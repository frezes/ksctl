package tenant

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	kubesphererest "kubesphere.io/client-go/rest"
)

func TestCommandRoutesResourcesAndAliases(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		cluster  string
		wantPath string
		response string
		wantOut  string
	}{
		{
			name:     "workspaces plural ignores cluster",
			args:     []string{"get", "workspaces", "-o", "json"},
			cluster:  "ignored",
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates",
			response: `{"items":[{"metadata":{"name":"platform"}}],"total_count":1}`,
			wantOut:  `"name": "platform"`,
		},
		{
			name:     "named workspace ignores cluster",
			args:     []string{"get", "workspace", "platform", "-o", "json"},
			cluster:  "ignored",
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/workspacetemplates/platform",
			response: `{"metadata":{"name":"platform"}}`,
			wantOut:  `"name": "platform"`,
		},
		{
			name:     "namespace alias follows cluster",
			args:     []string{"get", "ns", "-o", "yaml"},
			cluster:  "member",
			wantPath: "/clusters/member/kapis/tenant.kubesphere.io/v1beta1/namespaces",
			response: `{"items":[{"metadata":{"name":"demo"}}],"totalItems":1}`,
			wantOut:  "name: demo",
		},
		{
			name:     "workspace namespace follows cluster",
			args:     []string{"get", "namespaces", "--workspace", "platform", "-o", "json"},
			cluster:  "member",
			wantPath: "/clusters/member/kapis/tenant.kubesphere.io/v1beta1/workspaces/platform/namespaces",
			response: `{"items":[{"metadata":{"name":"demo"}}],"totalItems":1}`,
			wantOut:  `"name": "demo"`,
		},
		{
			name:     "clusters plural ignores cluster",
			args:     []string{"get", "clusters", "-o", "json"},
			cluster:  "ignored",
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/clusters",
			response: `{"items":[{"metadata":{"name":"host"}}],"totalItems":1}`,
			wantOut:  `"name": "host"`,
		},
		{
			name:     "workspace cluster ignores cluster",
			args:     []string{"get", "cluster", "--workspace", "platform", "-o", "json"},
			cluster:  "ignored",
			wantPath: "/kapis/tenant.kubesphere.io/v1beta1/workspaces/platform/clusters",
			response: `{"items":[{"metadata":{"name":"host"}}],"totalItems":1}`,
			wantOut:  `"name": "host"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != test.wantPath {
					t.Errorf("path = %q, want %q", r.URL.Path, test.wantPath)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer secret" {
					t.Errorf("Authorization = %q, want Bearer secret", got)
				}
				if got := r.Header.Get("User-Agent"); got != "ksctl/test" {
					t.Errorf("User-Agent = %q, want ksctl/test", got)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, test.response)
			}))
			defer server.Close()

			out := new(bytes.Buffer)
			command := NewCommand(fakeRESTClientGetter{
				config:  &kubesphererest.Config{Host: server.URL, BearerToken: "secret", UserAgent: "ksctl/test"},
				cluster: test.cluster,
			})
			command.SetOut(out)
			command.SetErr(new(bytes.Buffer))
			command.SetArgs(test.args)
			if err := command.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !strings.Contains(out.String(), test.wantOut) {
				t.Fatalf("output missing %q:\n%s", test.wantOut, out.String())
			}
		})
	}
}

func TestCommandWorkspaceFlagsHaveNoShortForm(t *testing.T) {
	command := NewCommand(fakeRESTClientGetter{})
	get := findCommand(command, "get")
	if get == nil {
		t.Fatal("get command is missing")
	}
	for _, name := range []string{"namespace", "cluster"} {
		resource := findCommand(get, name)
		if resource == nil {
			t.Fatalf("%s command is missing", name)
		}
		flag := resource.Flags().Lookup("workspace")
		if flag == nil || flag.Shorthand != "" {
			t.Fatalf("%s workspace flag = %#v, want long form only", name, flag)
		}
	}
	if findCommand(get, "workspace").Flags().Lookup("workspace") != nil {
		t.Fatal("workspace command unexpectedly accepts --workspace")
	}
}

func TestCommandRejectsUnsupportedInputsBeforeConnection(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"get", "ws"}, want: "unknown command"},
		{args: []string{"get", "ns", "-w", "platform"}, want: "unknown shorthand flag"},
		{args: []string{"get", "cluster", "-w", "platform"}, want: "unknown shorthand flag"},
		{args: []string{"get", "workspace", "one", "two"}, want: "accepts at most 1 arg"},
		{args: []string{"get", "ns", "demo"}, want: `unknown command "demo"`},
		{args: []string{"get", "workspace", "-o", "wide"}, want: "unsupported output format"},
		{args: []string{"get", "ns", "--workspace", "team/member"}, want: "invalid workspace"},
	}
	for _, test := range tests {
		command := NewCommand(fakeRESTClientGetter{})
		command.SetOut(new(bytes.Buffer))
		command.SetErr(new(bytes.Buffer))
		command.SetArgs(test.args)
		err := command.Execute()
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("Execute(%v) error = %v, want %q", test.args, err, test.want)
		}
	}
}

func findCommand(command interface{ Commands() []*cobra.Command }, name string) *cobra.Command {
	for _, child := range command.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}

type fakeRESTClientGetter struct {
	config  *kubesphererest.Config
	cluster string
	err     error
}

func (g fakeRESTClientGetter) ToRESTConfig() (*kubesphererest.Config, error) {
	if g.err != nil {
		return nil, g.err
	}
	return g.config, nil
}

func (g fakeRESTClientGetter) KubeSphereCluster() (string, error) {
	if g.err != nil {
		return "", g.err
	}
	return g.cluster, nil
}
