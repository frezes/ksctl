package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestAggregatingTransportKeepsSuccessfulClusterAdminRequest(t *testing.T) {
	var requests []string
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.RequestURI())
		return jsonResponse(request, http.StatusOK, podList("admin", "pod-a")), nil
	})
	resolver := &fakeNamespaceResolver{namespaces: []string{"tenant-a"}}
	state := &aggregationState{mode: aggregateOnForbidden}
	client := newAggregatingTestClient(t, base, resolver, state)

	response, err := client.Get(
		"https://example.test/proxy/clusters/member/api/v1/pods?labelSelector=app%3Dweb",
	)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got, want := requests, []string{
		"/proxy/clusters/member/api/v1/pods?labelSelector=app%3Dweb",
	}; !equalStrings(got, want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver calls = %d, want 0", resolver.calls)
	}
	if state.used.Load() {
		t.Fatal("aggregation state used = true, want false")
	}
}

func TestAggregatingTransportFallsBackAfterForbidden(t *testing.T) {
	var (
		requestsMu sync.Mutex
		requests   []string
	)
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requestsMu.Lock()
		requests = append(requests, request.URL.RequestURI())
		requestsMu.Unlock()
		switch request.URL.Path {
		case "/proxy/clusters/member/api/v1/pods":
			return jsonResponse(request, http.StatusForbidden, `{"kind":"Status"}`), nil
		case "/proxy/clusters/member/api/v1/namespaces/team-b/pods":
			return jsonResponse(request, http.StatusOK, podList("team-b", "pod-b")), nil
		case "/proxy/clusters/member/api/v1/namespaces/team-a/pods":
			return jsonResponse(request, http.StatusOK, podList("team-a", "pod-a")), nil
		default:
			t.Fatalf("unexpected request path %q", request.URL.Path)
			return nil, nil
		}
	})
	resolver := &fakeNamespaceResolver{namespaces: []string{"team-b", "team-a"}}
	state := &aggregationState{mode: aggregateOnForbidden}
	client := newAggregatingTestClient(t, base, resolver, state)

	response, err := client.Get(
		"https://example.test/proxy/clusters/member/api/v1/pods?labelSelector=app%3Dweb",
	)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	var list struct {
		Items []struct {
			Metadata struct {
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("items length = %d, want 2", len(list.Items))
	}
	if got, want := []string{
		list.Items[0].Metadata.Namespace + "/" + list.Items[0].Metadata.Name,
		list.Items[1].Metadata.Namespace + "/" + list.Items[1].Metadata.Name,
	}, []string{"team-b/pod-b", "team-a/pod-a"}; !equalStrings(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	requestsMu.Lock()
	defer requestsMu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("requests = %v, want 3 requests", requests)
	}
	slices.Sort(requests[1:])
	if got, want := requests, []string{
		"/proxy/clusters/member/api/v1/pods?labelSelector=app%3Dweb",
		"/proxy/clusters/member/api/v1/namespaces/team-a/pods?labelSelector=app%3Dweb",
		"/proxy/clusters/member/api/v1/namespaces/team-b/pods?labelSelector=app%3Dweb",
	}; !equalStrings(got, want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	if resolver.calls != 1 || resolver.workspace != "" {
		t.Fatalf(
			"resolver = calls %d workspace %q, want calls 1 workspace empty",
			resolver.calls,
			resolver.workspace,
		)
	}
	if !state.used.Load() {
		t.Fatal("aggregation state used = false, want true")
	}
}

func TestAggregatingTransportWorkspaceSkipsGlobalRequest(t *testing.T) {
	var requests []string
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.RequestURI())
		return jsonResponse(request, http.StatusOK, podList("team-a", "pod-a")), nil
	})
	resolver := &fakeNamespaceResolver{namespaces: []string{"team-a"}}
	state := &aggregationState{mode: aggregateWorkspace, workspace: "workspace-a"}
	client := newAggregatingTestClient(t, base, resolver, state)

	response, err := client.Get(
		"https://example.test/proxy/clusters/member/api/v1/pods?fieldSelector=status.phase%3DRunning",
	)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer response.Body.Close()

	if got, want := requests, []string{
		"/proxy/clusters/member/api/v1/namespaces/team-a/pods?fieldSelector=status.phase%3DRunning",
	}; !equalStrings(got, want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	if resolver.calls != 1 || resolver.workspace != "workspace-a" {
		t.Fatalf(
			"resolver = calls %d workspace %q, want calls 1 workspace %q",
			resolver.calls,
			resolver.workspace,
			"workspace-a",
		)
	}
	if !state.used.Load() {
		t.Fatal("aggregation state used = false, want true")
	}
}

func TestAggregatingTransportKeepsClusterScopedAndNamespacedRequestsDirect(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{
			name: "cluster scoped",
			url:  "https://example.test/proxy/clusters/member/api/v1/nodes",
		},
		{
			name: "explicit namespace",
			url: "https://example.test/proxy/clusters/member/api/v1/" +
				"namespaces/team-a/pods",
		},
		{
			name: "named resource",
			url:  "https://example.test/proxy/clusters/member/api/v1/pods/pod-a",
		},
		{
			name: "discovery",
			url:  "https://example.test/proxy/clusters/member/api/v1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requests []string
			base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				requests = append(requests, request.URL.RequestURI())
				return jsonResponse(request, http.StatusForbidden, `{"kind":"Status"}`), nil
			})
			resolver := &fakeNamespaceResolver{namespaces: []string{"team-a"}}
			state := &aggregationState{mode: aggregateOnForbidden}
			client := newAggregatingTestClient(t, base, resolver, state)

			response, err := client.Get(test.url)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			defer response.Body.Close()

			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusForbidden)
			}
			if len(requests) != 1 {
				t.Fatalf("requests = %v, want one direct request", requests)
			}
			if resolver.calls != 0 {
				t.Fatalf("resolver calls = %d, want 0", resolver.calls)
			}
			if state.used.Load() {
				t.Fatal("aggregation state used = true, want false")
			}
		})
	}
}

func TestAggregatingTransportDoesNotFallbackForNonForbiddenErrors(t *testing.T) {
	var requests int
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return jsonResponse(request, http.StatusInternalServerError, `{"kind":"Status"}`), nil
	})
	resolver := &fakeNamespaceResolver{namespaces: []string{"team-a"}}
	state := &aggregationState{mode: aggregateOnForbidden}
	client := newAggregatingTestClient(t, base, resolver, state)

	response, err := client.Get("https://example.test/proxy/clusters/member/api/v1/pods")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf(
			"status = %d, want %d",
			response.StatusCode,
			http.StatusInternalServerError,
		)
	}
	if requests != 1 || resolver.calls != 0 || state.used.Load() {
		t.Fatalf(
			"requests = %d, resolver calls = %d, used = %t; want 1, 0, false",
			requests,
			resolver.calls,
			state.used.Load(),
		)
	}
}

func TestAggregatingTransportKeepsSuccessfulAdministratorWatchNative(t *testing.T) {
	var requests int
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return jsonResponse(request, http.StatusOK, `{"type":"ADDED"}`), nil
	})
	resolver := &fakeNamespaceResolver{namespaces: []string{"team-a"}}
	state := &aggregationState{mode: aggregateOnForbidden}
	client := newAggregatingTestClient(t, base, resolver, state)

	response, err := client.Get(
		"https://example.test/proxy/clusters/member/api/v1/pods?watch=true",
	)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if requests != 1 || resolver.calls != 0 || state.used.Load() {
		t.Fatalf(
			"requests = %d, resolver calls = %d, used = %t; want 1, 0, false",
			requests,
			resolver.calls,
			state.used.Load(),
		)
	}
}

func TestAggregatingTransportLeavesNonGetRequestAlone(t *testing.T) {
	var requests int
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return jsonResponse(request, http.StatusForbidden, `{"kind":"Status"}`), nil
	})
	resolver := &fakeNamespaceResolver{namespaces: []string{"team-a"}}
	state := &aggregationState{mode: aggregateOnForbidden}
	client := newAggregatingTestClient(t, base, resolver, state)

	response, err := client.Post(
		"https://example.test/proxy/clusters/member/api/v1/pods",
		"application/json",
		strings.NewReader(`{}`),
	)
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}
	if requests != 1 || resolver.calls != 0 || state.used.Load() {
		t.Fatalf(
			"requests = %d, resolver calls = %d, used = %t; want 1, 0, false",
			requests,
			resolver.calls,
			state.used.Load(),
		)
	}
}

func TestAggregatingTransportRejectsWorkspaceRootResource(t *testing.T) {
	var requests int
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return jsonResponse(request, http.StatusOK, `{"kind":"NodeList","items":[]}`), nil
	})
	resolver := &fakeNamespaceResolver{namespaces: []string{"team-a"}}
	state := &aggregationState{mode: aggregateWorkspace, workspace: "platform"}
	client := newAggregatingTestClient(t, base, resolver, state)

	response, err := client.Get(
		"https://example.test/proxy/clusters/member/api/v1/nodes",
	)
	if err == nil || !strings.Contains(err.Error(), "cluster-scoped") {
		t.Fatalf("Get() error = %v, want cluster-scoped workspace error", err)
	}
	if response != nil {
		response.Body.Close()
		t.Fatalf("Get() response = %#v, want nil", response)
	}
	if requests != 0 || resolver.calls != 0 {
		t.Fatalf(
			"requests = %d, resolver calls = %d, want 0, 0",
			requests,
			resolver.calls,
		)
	}
}

func TestAggregatingTransportFailsWholeRequestWhenNamespaceFails(t *testing.T) {
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.Path == "/proxy/clusters/member/api/v1/pods":
			return jsonResponse(request, http.StatusForbidden, `{"kind":"Status"}`), nil
		case strings.Contains(request.URL.Path, "/team-a/"):
			return jsonResponse(request, http.StatusOK, podList("team-a", "pod-a")), nil
		default:
			return jsonResponse(request, http.StatusInternalServerError, `{"kind":"Status"}`), nil
		}
	})
	client := newAggregatingTestClient(
		t,
		base,
		&fakeNamespaceResolver{namespaces: []string{"team-a", "team-b"}},
		&aggregationState{mode: aggregateOnForbidden},
	)

	response, err := client.Get(
		"https://example.test/proxy/clusters/member/api/v1/pods",
	)
	if err == nil || !strings.Contains(err.Error(), `namespace "team-b"`) {
		t.Fatalf("Get() error = %v, want namespace failure", err)
	}
	if response != nil {
		response.Body.Close()
		t.Fatalf("Get() response = %#v, want nil", response)
	}
}

func TestAggregatingRESTClientGetterDelegatesDiscoveryMapperAndLoader(t *testing.T) {
	mapper := testRESTMapper()
	loader := clientcmd.NewDefaultClientConfig(
		clientcmdapi.Config{},
		&clientcmd.ConfigOverrides{},
	)
	delegate := &transportTestGetter{
		config: &rest.Config{Host: "https://example.test"},
		mapper: mapper,
		loader: loader,
	}
	getter := newAggregatingRESTClientGetter(
		delegate,
		&fakeNamespaceResolver{},
		&aggregationState{},
	)

	if got, err := getter.ToDiscoveryClient(); err != nil || got != nil {
		t.Fatalf("ToDiscoveryClient() = %#v, %v, want nil, nil", got, err)
	}
	if got, err := getter.ToRESTMapper(); err != nil || got != mapper {
		t.Fatalf("ToRESTMapper() = %#v, %v, want mapper, nil", got, err)
	}
	if got := getter.ToRawKubeConfigLoader(); got != loader {
		t.Fatalf("ToRawKubeConfigLoader() = %#v, want loader", got)
	}
}

func TestAggregatingTransportPaginatesEachNamespaceIndependently(t *testing.T) {
	var (
		mu       sync.Mutex
		requests = make(map[string][]string)
	)
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/proxy/clusters/member/api/v1/pods" {
			return jsonResponse(request, http.StatusForbidden, `{"kind":"Status"}`), nil
		}
		namespace := namespaceFromPath(request.URL.Path)
		token := request.URL.Query().Get("continue")
		mu.Lock()
		requests[namespace] = append(requests[namespace], token)
		mu.Unlock()
		switch namespace + "/" + token {
		case "team-a/":
			return jsonResponse(
				request,
				http.StatusOK,
				pagedPodList("team-a", "pod-a1", "a-next"),
			), nil
		case "team-a/a-next":
			return jsonResponse(
				request,
				http.StatusOK,
				pagedPodList("team-a", "pod-a2", ""),
			), nil
		case "team-b/":
			return jsonResponse(
				request,
				http.StatusOK,
				pagedPodList("team-b", "pod-b1", "b-next"),
			), nil
		case "team-b/b-next":
			return jsonResponse(
				request,
				http.StatusOK,
				pagedPodList("team-b", "pod-b2", ""),
			), nil
		default:
			return jsonResponse(request, http.StatusBadRequest, `{"kind":"Status"}`), nil
		}
	})
	client := newAggregatingTestClient(
		t,
		base,
		&fakeNamespaceResolver{namespaces: []string{"team-a", "team-b"}},
		&aggregationState{mode: aggregateOnForbidden},
	)

	response, err := client.Get(
		"https://example.test/proxy/clusters/member/api/v1/pods?limit=1",
	)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if got, want := podNames(t, body), []string{
		"team-a/pod-a1",
		"team-a/pod-a2",
		"team-b/pod-b1",
		"team-b/pod-b2",
	}; !equalStrings(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	if got, want := requests["team-a"], []string{"", "a-next"}; !equalStrings(got, want) {
		t.Fatalf("team-a continue tokens = %v, want %v", got, want)
	}
	if got, want := requests["team-b"], []string{"", "b-next"}; !equalStrings(got, want) {
		t.Fatalf("team-b continue tokens = %v, want %v", got, want)
	}
	var metadata struct {
		Metadata struct {
			Continue string `json:"continue"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &metadata); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if metadata.Metadata.Continue != "" {
		t.Fatalf("aggregate continue = %q, want empty", metadata.Metadata.Continue)
	}
}

func TestAggregatingTransportLimitsNamespaceConcurrency(t *testing.T) {
	namespaces := make([]string, 10)
	for index := range namespaces {
		namespaces[index] = "team-" + string(rune('a'+index))
	}
	var current atomic.Int32
	var maximum atomic.Int32
	started := make(chan struct{}, len(namespaces))
	release := make(chan struct{})
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/proxy/clusters/member/api/v1/pods" {
			return jsonResponse(request, http.StatusForbidden, `{"kind":"Status"}`), nil
		}
		inFlight := current.Add(1)
		for {
			observed := maximum.Load()
			if inFlight <= observed || maximum.CompareAndSwap(observed, inFlight) {
				break
			}
		}
		started <- struct{}{}
		<-release
		current.Add(-1)
		namespace := namespaceFromPath(request.URL.Path)
		return jsonResponse(request, http.StatusOK, podList(namespace, "pod")), nil
	})
	client := newAggregatingTestClient(
		t,
		base,
		&fakeNamespaceResolver{namespaces: namespaces},
		&aggregationState{mode: aggregateOnForbidden},
	)
	done := make(chan error, 1)
	go func() {
		response, err := client.Get(
			"https://example.test/proxy/clusters/member/api/v1/pods",
		)
		if response != nil {
			response.Body.Close()
		}
		done <- err
	}()

	startedCount := 0
	timer := time.NewTimer(time.Second)
	for startedCount < maxNamespaceConcurrency {
		select {
		case <-started:
			startedCount++
		case <-timer.C:
			close(release)
			if err := <-done; err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			t.Fatalf(
				"concurrent starts = %d, want %d",
				startedCount,
				maxNamespaceConcurrency,
			)
		}
	}
	if !timer.Stop() {
		<-timer.C
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := maximum.Load(); got <= 1 || got > maxNamespaceConcurrency {
		t.Fatalf(
			"maximum concurrency = %d, want > 1 and <= %d",
			got,
			maxNamespaceConcurrency,
		)
	}
}

func TestAggregatingTransportPreservesNamespaceOrderAcrossCompletionOrder(t *testing.T) {
	namespaces := []string{"team-a", "team-b", "team-c"}
	started := make(chan string, len(namespaces))
	gates := map[string]chan struct{}{
		"team-a": make(chan struct{}),
		"team-b": make(chan struct{}),
		"team-c": make(chan struct{}),
	}
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/proxy/clusters/member/api/v1/pods" {
			return jsonResponse(request, http.StatusForbidden, `{"kind":"Status"}`), nil
		}
		namespace := namespaceFromPath(request.URL.Path)
		started <- namespace
		<-gates[namespace]
		return jsonResponse(request, http.StatusOK, podList(namespace, "pod")), nil
	})
	client := newAggregatingTestClient(
		t,
		base,
		&fakeNamespaceResolver{namespaces: namespaces},
		&aggregationState{mode: aggregateOnForbidden},
	)
	type result struct {
		body []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		response, err := client.Get(
			"https://example.test/proxy/clusters/member/api/v1/pods",
		)
		if err != nil {
			done <- result{err: err}
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		done <- result{body: body, err: err}
	}()

	seen := make(map[string]bool, len(namespaces))
	timer := time.NewTimer(time.Second)
	for len(seen) < len(namespaces) {
		select {
		case namespace := <-started:
			seen[namespace] = true
		case <-timer.C:
			for _, gate := range gates {
				close(gate)
			}
			<-done
			t.Fatalf("started namespaces = %v, want all %v", seen, namespaces)
		}
	}
	if !timer.Stop() {
		<-timer.C
	}
	close(gates["team-c"])
	close(gates["team-b"])
	close(gates["team-a"])
	outcome := <-done
	if outcome.err != nil {
		t.Fatalf("Get() error = %v", outcome.err)
	}
	if got, want := podNames(t, outcome.body), []string{
		"team-a/pod",
		"team-b/pod",
		"team-c/pod",
	}; !equalStrings(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
}

func TestAggregatingTransportCancelsPeersAndClosesBodiesOnNamespaceError(t *testing.T) {
	blockedStarted := make(chan struct{})
	peerCancelled := make(chan struct{})
	forceRelease := make(chan struct{})
	var bodiesMu sync.Mutex
	var bodies []*trackingReadCloser
	newBody := func(value string) *trackingReadCloser {
		body := &trackingReadCloser{Reader: strings.NewReader(value)}
		bodiesMu.Lock()
		bodies = append(bodies, body)
		bodiesMu.Unlock()
		return body
	}
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/proxy/clusters/member/api/v1/pods" {
			response := jsonResponse(request, http.StatusForbidden, `{"kind":"Status"}`)
			response.Body = newBody(`{"kind":"Status"}`)
			return response, nil
		}
		switch namespaceFromPath(request.URL.Path) {
		case "team-a":
			close(blockedStarted)
			select {
			case <-request.Context().Done():
				close(peerCancelled)
				return nil, request.Context().Err()
			case <-forceRelease:
				return jsonResponse(request, http.StatusOK, podList("team-a", "pod-a")), nil
			}
		case "team-b":
			<-blockedStarted
			response := jsonResponse(
				request,
				http.StatusInternalServerError,
				`{"kind":"Status"}`,
			)
			response.Body = newBody(`{"kind":"Status"}`)
			return response, nil
		default:
			return nil, errors.New("unexpected namespace")
		}
	})
	state := &aggregationState{mode: aggregateOnForbidden}
	client := newAggregatingTestClient(
		t,
		base,
		&fakeNamespaceResolver{namespaces: []string{"team-a", "team-b"}},
		state,
	)
	done := make(chan error, 1)
	go func() {
		response, err := client.Get(
			"https://example.test/proxy/clusters/member/api/v1/pods",
		)
		if response != nil {
			response.Body.Close()
		}
		done <- err
	}()

	select {
	case <-peerCancelled:
	case <-time.After(time.Second):
		close(forceRelease)
		<-done
		t.Fatal("blocked namespace did not observe context cancellation")
	}
	err := <-done
	if err == nil || !strings.Contains(err.Error(), `namespace "team-b"`) {
		t.Fatalf("Get() error = %v, want team-b failure", err)
	}
	if !state.used.Load() {
		t.Fatal("aggregation state used = false, want true")
	}
	bodiesMu.Lock()
	defer bodiesMu.Unlock()
	for index, body := range bodies {
		if !body.closed.Load() {
			t.Fatalf("response body %d was not closed", index)
		}
	}
}

func TestAggregatingTransportReportsNamespaceErrorsAndClosesBodies(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		secondPage bool
	}{
		{name: "forbidden", status: http.StatusForbidden, body: `{"kind":"Status"}`},
		{name: "not found", status: http.StatusNotFound, body: `{"kind":"Status"}`},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{"kind":"Status"}`},
		{
			name:   "server error",
			status: http.StatusInternalServerError,
			body:   `{"kind":"Status"}`,
		},
		{name: "malformed JSON", status: http.StatusOK, body: `{`},
		{
			name:       "failing second page",
			status:     http.StatusInternalServerError,
			body:       `{"kind":"Status"}`,
			secondPage: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				mu     sync.Mutex
				bodies []*trackingReadCloser
			)
			responseWithTrackedBody := func(
				request *http.Request,
				status int,
				value string,
			) *http.Response {
				body := &trackingReadCloser{Reader: strings.NewReader(value)}
				mu.Lock()
				bodies = append(bodies, body)
				mu.Unlock()
				response := jsonResponse(request, status, value)
				response.Body = body
				return response
			}
			base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path == "/proxy/clusters/member/api/v1/pods" {
					return responseWithTrackedBody(
						request,
						http.StatusForbidden,
						`{"kind":"Status"}`,
					), nil
				}
				if test.secondPage && request.URL.Query().Get("continue") == "" {
					return responseWithTrackedBody(
						request,
						http.StatusOK,
						pagedPodList("team-a", "pod-a", "next"),
					), nil
				}
				return responseWithTrackedBody(
					request,
					test.status,
					test.body,
				), nil
			})
			state := &aggregationState{mode: aggregateOnForbidden}
			client := newAggregatingTestClient(
				t,
				base,
				&fakeNamespaceResolver{namespaces: []string{"team-a"}},
				state,
			)

			response, err := client.Get(
				"https://example.test/proxy/clusters/member/api/v1/pods",
			)
			if response != nil {
				response.Body.Close()
				t.Fatalf("Get() response = %#v, want nil", response)
			}
			if err == nil || !strings.Contains(err.Error(), `namespace "team-a"`) {
				t.Fatalf("Get() error = %v, want team-a failure", err)
			}
			if !state.used.Load() {
				t.Fatal("aggregation state used = false, want true")
			}
			mu.Lock()
			defer mu.Unlock()
			for index, body := range bodies {
				if !body.closed.Load() {
					t.Fatalf("response body %d was not closed", index)
				}
			}
		})
	}
}

func newAggregatingTestClient(
	t *testing.T,
	base http.RoundTripper,
	resolver namespaceResolver,
	state *aggregationState,
) *http.Client {
	t.Helper()
	getter := newAggregatingRESTClientGetter(
		&transportTestGetter{
			config: &rest.Config{
				Host:      "https://example.test/proxy/clusters/member",
				Transport: base,
			},
			mapper: testRESTMapper(),
		},
		resolver,
		state,
	)
	config, err := getter.ToRESTConfig()
	if err != nil {
		t.Fatalf("ToRESTConfig() error = %v", err)
	}
	client, err := rest.HTTPClientFor(config)
	if err != nil {
		t.Fatalf("HTTPClientFor() error = %v", err)
	}
	return client
}

func testRESTMapper() meta.RESTMapper {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(
		schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
		meta.RESTScopeNamespace,
	)
	mapper.Add(
		schema.GroupVersionKind{Version: "v1", Kind: "Node"},
		meta.RESTScopeRoot,
	)
	return mapper
}

type fakeNamespaceResolver struct {
	namespaces []string
	err        error
	calls      int
	workspace  string
}

func (r *fakeNamespaceResolver) Namespaces(
	_ context.Context,
	workspace string,
) ([]string, error) {
	r.calls++
	r.workspace = workspace
	return r.namespaces, r.err
}

type transportTestGetter struct {
	config    *rest.Config
	mapper    meta.RESTMapper
	loader    clientcmd.ClientConfig
	discovery discovery.CachedDiscoveryInterface
	err       error
}

func (g *transportTestGetter) ToRESTConfig() (*rest.Config, error) {
	if g.err != nil {
		return nil, g.err
	}
	return rest.CopyConfig(g.config), nil
}

func (g *transportTestGetter) ToDiscoveryClient() (
	discovery.CachedDiscoveryInterface,
	error,
) {
	return g.discovery, nil
}

func (g *transportTestGetter) ToRESTMapper() (meta.RESTMapper, error) {
	if g.err != nil {
		return nil, g.err
	}
	return g.mapper, nil
}

func (g *transportTestGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return g.loader
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func jsonResponse(
	request *http.Request,
	status int,
	body string,
) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:    io.NopCloser(strings.NewReader(body)),
		Request: request,
	}
}

func podList(namespace, name string) string {
	return pagedPodList(namespace, name, "")
}

func pagedPodList(namespace, name, token string) string {
	return `{
		"apiVersion":"v1",
		"kind":"PodList",
		"metadata":{"continue":` + quoteJSON(token) + `,"resourceVersion":"1"},
		"items":[{"apiVersion":"v1","kind":"Pod","metadata":{
			"namespace":` + quoteJSON(namespace) + `,
			"name":` + quoteJSON(name) + `
		}}]
	}`
}

func quoteJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func podNames(t *testing.T, body []byte) []string {
	t.Helper()
	var list struct {
		Items []struct {
			Metadata struct {
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	names := make([]string, len(list.Items))
	for index, item := range list.Items {
		names[index] = item.Metadata.Namespace + "/" + item.Metadata.Name
	}
	return names
}

func namespaceFromPath(path string) string {
	remaining := strings.SplitN(path, "/namespaces/", 2)
	if len(remaining) != 2 {
		return ""
	}
	return strings.SplitN(remaining[1], "/", 2)[0]
}

type trackingReadCloser struct {
	io.Reader
	closed atomic.Bool
}

func (r *trackingReadCloser) Close() error {
	r.closed.Store(true)
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
