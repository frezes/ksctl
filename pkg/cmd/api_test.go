package cmd

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clientkubesphere "github.com/kubesphere/ksctl/pkg/client/kubesphere"
	kubesphererest "kubesphere.io/client-go/rest"
)

type apiRESTConfigGetterFunc func() (*kubesphererest.Config, error)

func (f apiRESTConfigGetterFunc) ToRESTConfig() (*kubesphererest.Config, error) {
	return f()
}

type apiRESTClientFactoryFunc func(*kubesphererest.Config) (kubesphererest.Interface, error)

func (f apiRESTClientFactoryFunc) ForConfig(config *kubesphererest.Config) (kubesphererest.Interface, error) {
	return f(config)
}

type recordedAPIRequest struct {
	method      string
	path        string
	query       string
	contentType string
	body        string
}

type apiErrorWriter struct {
	err error
}

func (w apiErrorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func TestAPICommandFlags(t *testing.T) {
	command := newAPICommand(
		apiRESTConfigGetterFunc(func() (*kubesphererest.Config, error) {
			return nil, errors.New("must not resolve configuration")
		}),
		clientkubesphere.NewRESTClientFactory(nil),
	)

	method := command.Flags().Lookup("method")
	if method == nil || method.Shorthand != "X" || method.DefValue != http.MethodGet {
		t.Fatalf("method flag = %#v", method)
	}
	data := command.Flags().Lookup("data")
	if data == nil || data.Shorthand != "d" {
		t.Fatalf("data flag = %#v", data)
	}
}

func TestAPICommandRequestVariants(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		wantMethod      string
		wantPath        string
		wantQuery       string
		wantContentType string
		wantBody        string
		status          int
	}{
		{
			name:       "default get preserves query",
			args:       []string{"/kapis/example.io/v1/items?limit=10&labelSelector=app%3Ddemo"},
			wantMethod: http.MethodGet,
			wantPath:   "/kapis/example.io/v1/items",
			wantQuery:  "labelSelector=app%3Ddemo&limit=10",
		},
		{
			name:            "data defaults to post",
			args:            []string{"/kapis/example.io/v1/items", "-d", `{"name":"demo"}`},
			wantMethod:      http.MethodPost,
			wantPath:        "/kapis/example.io/v1/items",
			wantContentType: "application/json",
			wantBody:        `{"name":"demo"}`,
		},
		{
			name:            "explicit method wins",
			args:            []string{"/kapis/example.io/v1/items/demo", "-X", "put", "-d", `{"enabled":true}`},
			wantMethod:      http.MethodPut,
			wantPath:        "/kapis/example.io/v1/items/demo",
			wantContentType: "application/json",
			wantBody:        `{"enabled":true}`,
		},
		{
			name:       "explicit delete",
			args:       []string{"/kapis/example.io/v1/items/demo", "-X", "delete"},
			wantMethod: http.MethodDelete,
			wantPath:   "/kapis/example.io/v1/items/demo",
		},
		{
			name:            "explicit empty data is still a post body",
			args:            []string{"/kapis/example.io/v1/items", "-d", ""},
			wantMethod:      http.MethodPost,
			wantPath:        "/kapis/example.io/v1/items",
			wantContentType: "application/json",
		},
		{
			name:            "at file syntax remains inline",
			args:            []string{"/kapis/example.io/v1/items", "-d", "@payload.json"},
			wantMethod:      http.MethodPost,
			wantPath:        "/kapis/example.io/v1/items",
			wantContentType: "application/json",
			wantBody:        "@payload.json",
		},
		{
			name:       "custom valid method",
			args:       []string{"/kapis/example.io/v1/items", "-X", "propfind"},
			wantMethod: "PROPFIND",
			wantPath:   "/kapis/example.io/v1/items",
		},
		{
			name:       "multi status is successful",
			args:       []string{"/kapis/example.io/v1/items", "-X", "propfind"},
			wantMethod: "PROPFIND",
			wantPath:   "/kapis/example.io/v1/items",
			status:     http.StatusMultiStatus,
		},
		{
			name:       "upper 2xx status is successful",
			args:       []string{"/kapis/example.io/v1/items"},
			wantMethod: http.MethodGet,
			wantPath:   "/kapis/example.io/v1/items",
			status:     299,
		},
	}

	response := []byte{0x00, 'o', 'k', '\n'}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorded := make(chan recordedAPIRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				body, _ := io.ReadAll(request.Body)
				recorded <- recordedAPIRequest{
					method:      request.Method,
					path:        request.URL.Path,
					query:       request.URL.RawQuery,
					contentType: request.Header.Get("Content-Type"),
					body:        string(body),
				}
				if test.status != 0 {
					w.WriteHeader(test.status)
				}
				_, _ = w.Write(response)
			}))
			t.Cleanup(server.Close)

			out := new(bytes.Buffer)
			command := newAPICommand(
				apiRESTConfigGetterFunc(func() (*kubesphererest.Config, error) {
					return &kubesphererest.Config{Host: server.URL}, nil
				}),
				clientkubesphere.NewRESTClientFactory(nil),
			)
			command.SetOut(out)
			command.SetErr(io.Discard)
			command.SetArgs(test.args)

			if err := command.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			got := <-recorded
			if got.method != test.wantMethod ||
				got.path != test.wantPath ||
				got.query != test.wantQuery ||
				got.contentType != test.wantContentType ||
				got.body != test.wantBody {
				t.Fatalf("request = %#v", got)
			}
			if !bytes.Equal(out.Bytes(), response) {
				t.Fatalf("output = %v, want %v", out.Bytes(), response)
			}
		})
	}
}

func TestAPICommandRejectsInvalidInputBeforeResolvingConnection(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing path", args: nil, want: "accepts 1 arg"},
		{name: "extra path", args: []string{"/kapis/one", "/kapis/two"}, want: "accepts 1 arg"},
		{name: "relative path", args: []string{"kapis/example"}, want: "must begin with /"},
		{name: "absolute URL", args: []string{"https://example.invalid/kapis/example"}, want: "server-relative"},
		{name: "authority path", args: []string{"//example.invalid/kapis/example"}, want: "server-relative"},
		{name: "fragment", args: []string{"/kapis/example#section"}, want: "invalid API path"},
		{name: "empty method", args: []string{"/kapis/example", "-X", "   "}, want: "HTTP method must not be empty"},
		{name: "invalid method", args: []string{"/kapis/example", "-X", "BAD METHOD"}, want: "invalid HTTP method"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			command := newAPICommand(
				apiRESTConfigGetterFunc(func() (*kubesphererest.Config, error) {
					calls++
					return nil, errors.New("connection must not be resolved")
				}),
				clientkubesphere.NewRESTClientFactory(nil),
			)
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			command.SetArgs(test.args)

			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Execute() error = %v, want %q", err, test.want)
			}
			if calls != 0 {
				t.Fatalf("connection getter calls = %d, want 0", calls)
			}
		})
	}
}

func TestAPICommandWritesHTTPErrorBodyAndReturnsError(t *testing.T) {
	response := []byte(`{"message":"invalid request"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write(response)
	}))
	t.Cleanup(server.Close)

	out := new(bytes.Buffer)
	command := newAPICommand(
		apiRESTConfigGetterFunc(func() (*kubesphererest.Config, error) {
			return &kubesphererest.Config{Host: server.URL}, nil
		}),
		clientkubesphere.NewRESTClientFactory(nil),
	)
	command.SilenceUsage = true
	command.SetOut(out)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"/kapis/example"})

	err := command.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want HTTP error")
	}
	if !bytes.Equal(out.Bytes(), response) {
		t.Fatalf("output = %q, want %q", out.Bytes(), response)
	}
}

func TestAPICommandJoinsHTTPAndOutputErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "server failed", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	command := newAPICommand(
		apiRESTConfigGetterFunc(func() (*kubesphererest.Config, error) {
			return &kubesphererest.Config{Host: server.URL}, nil
		}),
		clientkubesphere.NewRESTClientFactory(nil),
	)
	command.SilenceUsage = true
	command.SetOut(apiErrorWriter{err: errors.New("writer failed")})
	command.SetErr(io.Discard)
	command.SetArgs([]string{"/kapis/example"})

	err := command.Execute()
	if err == nil ||
		!strings.Contains(err.Error(), "request KubeSphere API") ||
		!strings.Contains(err.Error(), "write API response") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestAPICommandReturnsSuccessOutputWriteError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("success"))
	}))
	t.Cleanup(server.Close)

	command := newAPICommand(
		apiRESTConfigGetterFunc(func() (*kubesphererest.Config, error) {
			return &kubesphererest.Config{Host: server.URL}, nil
		}),
		clientkubesphere.NewRESTClientFactory(nil),
	)
	command.SilenceUsage = true
	command.SetOut(apiErrorWriter{err: errors.New("writer failed")})
	command.SetErr(io.Discard)
	command.SetArgs([]string{"/kapis/example"})

	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "write API response") {
		t.Fatalf("Execute() error = %v, want output error", err)
	}
	if strings.Contains(err.Error(), "request KubeSphere API") {
		t.Fatalf("Execute() error = %v, want only output error", err)
	}
}

func TestAPICommandPropagatesConnectionFactoryAndNetworkErrors(t *testing.T) {
	t.Run("connection", func(t *testing.T) {
		command := newAPICommand(
			apiRESTConfigGetterFunc(func() (*kubesphererest.Config, error) {
				return nil, errors.New("resolve failed")
			}),
			clientkubesphere.NewRESTClientFactory(nil),
		)
		command.SetOut(io.Discard)
		command.SetErr(io.Discard)
		command.SetArgs([]string{"/kapis/example"})

		err := command.Execute()
		if err == nil || !strings.Contains(err.Error(), "resolve API connection") {
			t.Fatalf("Execute() error = %v", err)
		}
	})

	t.Run("factory", func(t *testing.T) {
		command := newAPICommand(
			apiRESTConfigGetterFunc(func() (*kubesphererest.Config, error) {
				return &kubesphererest.Config{Host: "https://example.invalid"}, nil
			}),
			apiRESTClientFactoryFunc(func(*kubesphererest.Config) (kubesphererest.Interface, error) {
				return nil, errors.New("factory failed")
			}),
		)
		command.SetOut(io.Discard)
		command.SetErr(io.Discard)
		command.SetArgs([]string{"/kapis/example"})

		err := command.Execute()
		if err == nil || !strings.Contains(err.Error(), "create KubeSphere REST client") {
			t.Fatalf("Execute() error = %v", err)
		}
	})

	t.Run("network", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		host := server.URL
		server.Close()

		command := newAPICommand(
			apiRESTConfigGetterFunc(func() (*kubesphererest.Config, error) {
				return &kubesphererest.Config{Host: host}, nil
			}),
			clientkubesphere.NewRESTClientFactory(nil),
		)
		command.SetOut(io.Discard)
		command.SetErr(io.Discard)
		command.SetArgs([]string{"/kapis/example"})

		err := command.Execute()
		if err == nil || !strings.Contains(err.Error(), "request KubeSphere API") {
			t.Fatalf("Execute() error = %v", err)
		}
	})
}
