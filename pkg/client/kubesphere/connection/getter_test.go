package connection

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kubesphere/ksctl/pkg/auth"
	clientoptions "github.com/kubesphere/ksctl/pkg/client"
	"github.com/kubesphere/ksctl/pkg/config"
)

func TestRESTClientGetterBuildsNativeKubeSphereConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Fleets["local"] = config.Fleet{
		Host: "https://ks.example.com/prefix",
		TLSClientConfig: config.TLSClientConfig{
			Insecure:   true,
			ServerName: "ks.internal",
			CAData:     "ca-data",
		},
		Users: map[string]config.User{"account": {Username: "alice", BearerToken: "secret"}},
	}
	cfg.Contexts["local"] = config.Context{Fleet: "local", User: "account", DefaultCluster: "member"}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	getter := NewRESTClientGetter(&clientoptions.Options{
		ConfigPath:     path,
		RequestTimeout: "15s",
		UserAgent:      "ksctl/test",
	}, Dependencies{})
	restConfig, err := getter.ToRESTConfig()
	if err != nil {
		t.Fatalf("ToRESTConfig() error = %v", err)
	}
	if restConfig.Host != "https://ks.example.com/prefix" || restConfig.BearerToken != "secret" {
		t.Fatalf("REST config = %#v", restConfig)
	}
	if !restConfig.Insecure || restConfig.ServerName != "ks.internal" || string(restConfig.CAData) != "ca-data" {
		t.Fatalf("TLS config = %#v", restConfig.TLSClientConfig)
	}
	if restConfig.Timeout != 15*time.Second || restConfig.UserAgent != "ksctl/test" {
		t.Fatalf("timeout/user agent = %v/%q", restConfig.Timeout, restConfig.UserAgent)
	}
	cluster, err := getter.KubeSphereCluster()
	if err != nil || cluster != "member" {
		t.Fatalf("KubeSphereCluster() = %q, %v", cluster, err)
	}
	username, err := getter.KubeSphereUsername()
	if err != nil || username != "alice" {
		t.Fatalf("KubeSphereUsername() = %q, %v", username, err)
	}
}

func TestRESTClientGetterUsernameUsesExplicitContextAndUserKeyFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Fleets["prod"] = config.Fleet{Users: map[string]config.User{
		"local-user": {Username: "alice"},
		"other-user": {},
	}}
	cfg.Contexts["local"] = config.Context{Fleet: "prod", User: "local-user"}
	cfg.Contexts["other"] = config.Context{Fleet: "prod", User: "other-user"}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	getter := NewRESTClientGetter(&clientoptions.Options{ConfigPath: path, Context: "other"}, Dependencies{})
	username, err := getter.KubeSphereUsername()
	if err != nil {
		t.Fatalf("KubeSphereUsername() error = %v", err)
	}
	if username != "other-user" {
		t.Fatalf("KubeSphereUsername() = %q, want other-user", username)
	}
}

func TestRESTClientGetterReturnsUsernameReferenceErrors(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*config.Config)
		want      string
	}{
		{name: "missing current context", want: "current-context is not set"},
		{name: "unknown context", want: "no context exists", configure: func(cfg *config.Config) {
			cfg.CurrentContext = "missing"
		}},
		{name: "missing fleet", want: "no fleet exists", configure: func(cfg *config.Config) {
			cfg.CurrentContext = "local"
			cfg.Contexts["local"] = config.Context{Fleet: "missing", User: "admin"}
		}},
		{name: "missing user", want: "no user exists", configure: func(cfg *config.Config) {
			cfg.CurrentContext = "local"
			cfg.Fleets["local"] = config.Fleet{Users: map[string]config.User{}}
			cfg.Contexts["local"] = config.Context{Fleet: "local", User: "missing"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			cfg := config.New()
			if test.configure != nil {
				test.configure(cfg)
			}
			if err := config.Save(path, cfg); err != nil {
				t.Fatalf("Save() error = %v", err)
			}
			getter := NewRESTClientGetter(&clientoptions.Options{ConfigPath: path}, Dependencies{})
			_, err := getter.KubeSphereUsername()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("KubeSphereUsername() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRESTClientGetterRejectsInvalidClusterBeforeResolvingToken(t *testing.T) {
	for _, test := range []struct {
		name      string
		options   clientoptions.Options
		configure func(*config.Config)
	}{
		{
			name:    "explicit cluster",
			options: clientoptions.Options{Endpoint: "https://ks.example.com", Token: "secret", Cluster: "team/member"},
		},
		{
			name: "context default cluster",
			configure: func(cfg *config.Config) {
				cfg.CurrentContext = "local"
				cfg.Fleets["local"] = config.Fleet{
					Host:  "https://ks.example.com",
					Users: map[string]config.User{"admin": {}},
				}
				cfg.Contexts["local"] = config.Context{
					Fleet: "local", User: "admin", DefaultCluster: "team/member",
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			cfg := config.New()
			if test.configure != nil {
				test.configure(cfg)
			}
			if err := config.Save(path, cfg); err != nil {
				t.Fatalf("Save() error = %v", err)
			}
			test.options.ConfigPath = path

			provider := &recordingTokenProvider{}
			getter := NewRESTClientGetter(&test.options, Dependencies{TokenProvider: provider})
			_, err := getter.ToRESTConfig()
			if err == nil || !strings.Contains(err.Error(), "invalid cluster") {
				t.Fatalf("ToRESTConfig() error = %v, want invalid cluster", err)
			}
			if provider.calls != 0 {
				t.Fatalf("Token() calls = %d, want 0", provider.calls)
			}
		})
	}
}

type recordingTokenProvider struct {
	calls int
}

func (p *recordingTokenProvider) Token(context.Context, auth.Resolved, auth.TokenOptions) (string, error) {
	p.calls++
	return "secret", nil
}
