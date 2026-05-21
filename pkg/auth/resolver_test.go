package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kubesphere/ksctl/pkg/config"
)

func TestResolvePrefersFlagsOverEnvAndConfig(t *testing.T) {
	cfg := config.New()
	cfg.Clusters["prod"] = config.Cluster{Host: "https://config.example.com"}
	cfg.Users["admin"] = config.User{BearerToken: "config-token"}
	cfg.Contexts["prod"] = config.Context{Cluster: "prod", User: "admin"}
	cfg.CurrentContext = "prod"

	got, err := Resolve(ResolveInput{
		EndpointFlag: "https://flag.example.com",
		TokenFlag:    "flag-token",
		Env: map[string]string{
			"KS_ENDPOINT": "https://env.example.com",
			"KS_TOKEN":    "env-token",
		},
		Config: cfg,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Endpoint != "https://flag.example.com" || got.ExplicitToken != "flag-token" {
		t.Fatalf("resolved = %#v", got)
	}
}

func TestResolveUsesActiveContext(t *testing.T) {
	cfg := config.New()
	cfg.Clusters["prod"] = config.Cluster{
		Host: "https://config.example.com",
		TLSClientConfig: config.TLSClientConfig{
			Insecure:   true,
			ServerName: "ks.example.com",
			CAData:     "ca-data",
		},
	}
	cfg.Users["admin"] = config.User{
		Username:    "administrator",
		BearerToken: "config-token",
	}
	cfg.Contexts["prod"] = config.Context{Cluster: "prod", User: "admin", DefaultCluster: "host"}
	cfg.CurrentContext = "prod"

	got, err := Resolve(ResolveInput{
		Config: cfg,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Endpoint != "https://config.example.com" || got.Username != "administrator" || got.BearerToken != "config-token" || got.Cluster != "host" || !got.TLSClientConfig.Insecure || got.TLSClientConfig.ServerName != "ks.example.com" || got.TLSClientConfig.CAData != "ca-data" {
		t.Fatalf("resolved = %#v", got)
	}
}

func TestResolveCombinesEndpointFlagWithContextUser(t *testing.T) {
	cfg := config.New()
	cfg.Clusters["prod"] = config.Cluster{Host: "https://config.example.com"}
	cfg.Users["admin"] = config.User{}
	cfg.Contexts["prod"] = config.Context{Cluster: "prod", User: "admin"}
	cfg.CurrentContext = "prod"

	got, err := Resolve(ResolveInput{
		EndpointFlag:  "https://flag.example.com",
		NoInteractive: true,
		Config:        cfg,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Endpoint != "https://flag.example.com" || got.Username != "admin" {
		t.Fatalf("resolved = %#v", got)
	}
}

func TestResolvePrefersEnvironmentTokenOverConfiguredToken(t *testing.T) {
	cfg := config.New()
	cfg.Clusters["prod"] = config.Cluster{Host: "https://config.example.com"}
	cfg.Users["admin"] = config.User{BearerToken: "config-token"}
	cfg.Contexts["prod"] = config.Context{Cluster: "prod", User: "admin"}
	cfg.CurrentContext = "prod"

	got, err := Resolve(ResolveInput{
		Env:    map[string]string{"KS_TOKEN": "env-token"},
		Config: cfg,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.ExplicitToken != "env-token" {
		t.Fatalf("ExplicitToken = %q, want env-token", got.ExplicitToken)
	}
}

func TestResolvePreservesConfiguredTokenSources(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("file-token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg := config.New()
	cfg.Clusters["prod"] = config.Cluster{Host: "https://config.example.com"}
	cfg.Users["admin"] = config.User{
		BearerToken:     "config-token",
		BearerTokenFile: tokenPath,
	}
	cfg.Contexts["prod"] = config.Context{Cluster: "prod", User: "admin"}
	cfg.CurrentContext = "prod"

	got, err := Resolve(ResolveInput{Config: cfg})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.BearerTokenFile != tokenPath || got.BearerToken != "config-token" {
		t.Fatalf("resolved credentials = %#v", got)
	}
}
