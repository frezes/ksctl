package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingConfigReturnsEmptyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.APIVersion != ConfigAPIVersion || cfg.Kind != ConfigKind {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &Config{
		APIVersion:     ConfigAPIVersion,
		Kind:           ConfigKind,
		CurrentContext: "prod",
		Clusters: map[string]Cluster{"prod": {
			Host: "https://ks.example.com",
			TLSClientConfig: TLSClientConfig{
				Insecure:   true,
				ServerName: "ks.example.com",
				CAData:     "ca-data",
			},
		}},
		Users: map[string]User{
			"admin": {
				Username:        "administrator",
				BearerToken:     "config-token",
				BearerTokenFile: "/tmp/ks-token",
			},
		},
		Contexts: map[string]Context{"prod": {Cluster: "prod", User: "admin", DefaultCluster: "host"}},
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %v, want 0600", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	contents := string(data)
	for _, want := range []string{
		"host: https://ks.example.com",
		"tlsClientConfig:",
		"insecure: true",
		"serverName: ks.example.com",
		"caData: ca-data",
		"username: administrator",
		"bearerToken: config-token",
		"bearerTokenFile: /tmp/ks-token",
	} {
		if !strings.Contains(contents, want) {
			t.Fatalf("saved config does not contain %q:\n%s", want, contents)
		}
	}
	for _, unwanted := range []string{"server:", "tls:", "insecureSkipVerify:", "token:", "credentialRef:"} {
		if strings.Contains(contents, unwanted) {
			t.Fatalf("saved config contains %q:\n%s", unwanted, contents)
		}
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cluster := loaded.Clusters["prod"]
	if loaded.CurrentContext != "prod" || cluster.Host != "https://ks.example.com" || !cluster.TLSClientConfig.Insecure || cluster.TLSClientConfig.ServerName != "ks.example.com" || cluster.TLSClientConfig.CAData != "ca-data" {
		t.Fatalf("loaded config mismatch: %#v", loaded)
	}
	if got := loaded.Users["admin"]; got.Username != "administrator" || got.BearerToken != "config-token" || got.BearerTokenFile != "/tmp/ks-token" {
		t.Fatalf("loaded user = %#v", got)
	}
}
