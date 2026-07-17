package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kubesphere/ksctl/pkg/config"
)

func TestResolvePrefersFlagsOverEnvAndConfig(t *testing.T) {
	cfg := config.New()
	cfg.Fleets["prod"] = config.Fleet{Host: "https://config.example.com", Users: map[string]config.User{"admin": {BearerToken: "config-token"}}}
	cfg.Contexts["prod"] = config.Context{Fleet: "prod", User: "admin"}
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
	cfg.Fleets["prod-fleet"] = config.Fleet{
		Host: "https://config.example.com",
		TLSClientConfig: config.TLSClientConfig{
			ServerName: "ks.example.com",
			CAData:     "ca-data",
		},
		Users: map[string]config.User{"admin": {
			Username: "administrator",
			Password: "configured-password",
		}},
	}
	cfg.Contexts["prod"] = config.Context{Fleet: "prod-fleet", User: "admin", DefaultCluster: "host"}
	cfg.CurrentContext = "prod"

	got, err := Resolve(ResolveInput{
		Config: cfg,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Endpoint != "https://config.example.com" || got.Username != "administrator" || got.Password != "configured-password" || got.Fleet != "prod-fleet" || got.User != "admin" || got.Cluster != "host" || got.TLSClientConfig.ServerName != "ks.example.com" || got.TLSClientConfig.CAData != "ca-data" {
		t.Fatalf("resolved = %#v", got)
	}
}

func TestResolveReturnsMissingFleetError(t *testing.T) {
	cfg := config.New()
	cfg.Contexts["prod"] = config.Context{Fleet: "missing", User: "admin"}
	cfg.CurrentContext = "prod"

	_, err := Resolve(ResolveInput{Config: cfg})
	if err == nil || !strings.Contains(err.Error(), `no fleet exists with the name: missing`) {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestResolveReturnsMissingFleetUserError(t *testing.T) {
	cfg := config.New()
	cfg.Fleets["prod"] = config.Fleet{Host: "https://prod.example.com"}
	cfg.Contexts["prod"] = config.Context{Fleet: "prod", User: "missing"}
	cfg.CurrentContext = "prod"

	_, err := Resolve(ResolveInput{Config: cfg})
	if err == nil || !strings.Contains(err.Error(), `no user exists with the name: missing in fleet: prod`) {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestResolveScopesSameNamedUsersByFleet(t *testing.T) {
	cfg := config.New()
	cfg.Fleets["prod"] = config.Fleet{Host: "https://prod.example.com", Users: map[string]config.User{"admin": {BearerToken: "prod-token"}}}
	cfg.Fleets["staging"] = config.Fleet{Host: "https://staging.example.com", Users: map[string]config.User{"admin": {BearerToken: "staging-token"}}}
	cfg.Contexts["prod-admin"] = config.Context{Fleet: "prod", User: "admin"}
	cfg.Contexts["staging-admin"] = config.Context{Fleet: "staging", User: "admin"}

	for _, test := range []struct {
		context, endpoint, token string
	}{
		{context: "prod-admin", endpoint: "https://prod.example.com", token: "prod-token"},
		{context: "staging-admin", endpoint: "https://staging.example.com", token: "staging-token"},
	} {
		got, err := Resolve(ResolveInput{ContextFlag: test.context, Config: cfg})
		if err != nil {
			t.Fatalf("Resolve(%q) error = %v", test.context, err)
		}
		if got.Endpoint != test.endpoint || got.BearerToken != test.token || got.User != "admin" {
			t.Fatalf("Resolve(%q) = %#v", test.context, got)
		}
	}
}

func TestResolveCombinesEndpointFlagWithContextUser(t *testing.T) {
	cfg := config.New()
	cfg.Fleets["prod"] = config.Fleet{Host: "https://config.example.com", Users: map[string]config.User{"admin": {}}}
	cfg.Contexts["prod"] = config.Context{Fleet: "prod", User: "admin"}
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
	cfg.Fleets["prod"] = config.Fleet{Host: "https://config.example.com", Users: map[string]config.User{"admin": {BearerToken: "config-token"}}}
	cfg.Contexts["prod"] = config.Context{Fleet: "prod", User: "admin"}
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
	cfg.Fleets["prod"] = config.Fleet{Host: "https://config.example.com", Users: map[string]config.User{"admin": {
		BearerToken: "config-token", BearerTokenFile: tokenPath,
	}}}
	cfg.Contexts["prod"] = config.Context{Fleet: "prod", User: "admin"}
	cfg.CurrentContext = "prod"

	got, err := Resolve(ResolveInput{Config: cfg})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.BearerTokenFile != tokenPath || got.BearerToken != "config-token" {
		t.Fatalf("resolved credentials = %#v", got)
	}
}
