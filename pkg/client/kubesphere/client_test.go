package kubesphere

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kubesphererest "kubesphere.io/client-go/rest"
)

func TestRESTClientFactoryUsesInjectedHTTPClientWithoutMutatingConfig(t *testing.T) {
	var request *http.Request
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		request = req
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    req,
		}, nil
	})}
	config := &kubesphererest.Config{Host: "https://ks.example.com"}

	client, err := NewRESTClientFactory(httpClient).ForConfig(config)
	if err != nil {
		t.Fatalf("ForConfig() error = %v", err)
	}
	raw, err := client.Get().AbsPath("/readyz").DoRaw(context.Background())
	if err != nil {
		t.Fatalf("GET /readyz error = %v", err)
	}
	if string(raw) != "ok" {
		t.Fatalf("response = %q, want ok", raw)
	}
	if request == nil || request.URL.Path != "/readyz" {
		t.Fatalf("request = %#v, want path /readyz", request)
	}
	if config.NegotiatedSerializer != nil {
		t.Fatal("ForConfig() mutated the caller's NegotiatedSerializer")
	}
}

func TestRESTClientFactoryBuildsDefaultHTTPClientFromConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			t.Errorf("path = %q, want /readyz", r.URL.Path)
		}
		_, _ = io.WriteString(w, "ready")
	}))
	defer server.Close()

	client, err := NewRESTClientFactory(nil).ForConfig(&kubesphererest.Config{Host: server.URL})
	if err != nil {
		t.Fatalf("ForConfig() error = %v", err)
	}
	raw, err := client.Get().AbsPath("/readyz").DoRaw(context.Background())
	if err != nil {
		t.Fatalf("GET /readyz error = %v", err)
	}
	if string(raw) != "ready" {
		t.Fatalf("response = %q, want ready", raw)
	}
}

func TestRESTClientFactoryAppliesConfigTransportWrapperToInjectedHTTPClient(t *testing.T) {
	wrapped := false
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    req,
		}, nil
	})}
	config := &kubesphererest.Config{Host: "https://ks.example.com"}
	config.Wrap(func(delegate http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(request *http.Request) (*http.Response, error) {
			wrapped = true
			return delegate.RoundTrip(request)
		})
	})

	client, err := NewRESTClientFactory(httpClient).ForConfig(config)
	if err != nil {
		t.Fatalf("ForConfig() error = %v", err)
	}
	if _, err := client.Get().AbsPath("/readyz").DoRaw(context.Background()); err != nil {
		t.Fatalf("GET /readyz error = %v", err)
	}
	if !wrapped {
		t.Fatal("REST config transport wrapper was not applied")
	}
	if httpClient.Transport == nil {
		t.Fatal("ForConfig() mutated the injected HTTP client")
	}
}

func TestRESTClientFactoryRejectsNilConfig(t *testing.T) {
	_, err := NewRESTClientFactory(nil).ForConfig(nil)
	if err == nil || !strings.Contains(err.Error(), "config is required") {
		t.Fatalf("ForConfig(nil) error = %v, want config is required", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
