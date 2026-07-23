package config

import (
	"errors"
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
		Fleets: map[string]Fleet{"prod": {
			Host: "https://ks.example.com",
			TLSClientConfig: TLSClientConfig{
				ServerName: "ks.example.com",
				CAData:     "ca-data",
			},
			Users: map[string]User{"admin": {
				Username:        "administrator",
				BearerToken:     "config-token",
				BearerTokenFile: "/tmp/ks-token",
				Password:        "plaintext-password",
			}},
		}},
		Contexts: map[string]Context{"prod": {Fleet: "prod", User: "admin"}},
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
		"fleets:",
		"host: https://ks.example.com",
		"tlsClientConfig:",
		"serverName: ks.example.com",
		"caData: ca-data",
		"username: administrator",
		"bearerToken: config-token",
		"bearerTokenFile: /tmp/ks-token",
		"password: plaintext-password",
		"fleet: prod",
		`defaultCluster: ""`,
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
	fleet := loaded.Fleets["prod"]
	if loaded.CurrentContext != "prod" || fleet.Host != "https://ks.example.com" || fleet.TLSClientConfig.ServerName != "ks.example.com" || fleet.TLSClientConfig.CAData != "ca-data" {
		t.Fatalf("loaded config mismatch: %#v", loaded)
	}
	if got := fleet.Users["admin"]; got.Username != "administrator" || got.BearerToken != "config-token" || got.BearerTokenFile != "/tmp/ks-token" || got.Password != "plaintext-password" {
		t.Fatalf("loaded user = %#v", got)
	}
}

func TestMarshalOmitsEmptyTLSClientConfig(t *testing.T) {
	cfg := New()
	cfg.Fleets["prod"] = Fleet{Host: "http://ks.example.com"}
	cfg.Contexts["prod"] = Context{Fleet: "prod", User: "admin"}

	data, err := Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.Contains(string(data), "tlsClientConfig") {
		t.Fatalf("Marshal() emitted empty TLS config:\n%s", data)
	}
	if !strings.Contains(string(data), `defaultCluster: ""`) {
		t.Fatalf("Marshal() omitted defaultCluster:\n%s", data)
	}
}

func TestLoadRejectsUnsupportedOrAmbiguousDocuments(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{
			name: "legacy clusters",
			data: "clusters:\n  old:\n    host: https://old.example.com\n",
		},
		{
			name: "legacy root users",
			data: "fleets:\n  prod:\n    host: https://prod.example.com\nusers:\n  admin:\n    bearerToken: old-token\n",
		},
		{
			name: "unknown field",
			data: "fleets:\n  prod:\n    host: https://prod.example.com\n    unexpected: true\n",
		},
		{
			name: "duplicate field",
			data: "currentContext: prod\ncurrentContext: staging\n",
		},
		{
			name: "unsupported api version",
			data: "apiVersion: ksctl.kubesphere.io/v2\nkind: Config\n",
		},
		{
			name: "unsupported kind",
			data: "apiVersion: ksctl.kubesphere.io/v1alpha1\nkind: LegacyConfig\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(test.data), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("Load() error = nil, want document rejection")
			}
		})
	}
}

func TestLoadDefaultsMissingTypeMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("currentContext: prod\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.APIVersion != ConfigAPIVersion || cfg.Kind != ConfigKind {
		t.Fatalf("type metadata = %q, %q", cfg.APIVersion, cfg.Kind)
	}
	if cfg.Fleets == nil || cfg.Contexts == nil {
		t.Fatalf("maps were not initialized: %#v", cfg)
	}
}

func TestSaveRejectsUnsupportedTypeMetadataWithoutCreatingFile(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Config)
	}{
		{
			name: "unsupported api version",
			configure: func(cfg *Config) {
				cfg.APIVersion = "ksctl.kubesphere.io/v2"
			},
		},
		{
			name: "unsupported kind",
			configure: func(cfg *Config) {
				cfg.Kind = "LegacyConfig"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			cfg := New()
			test.configure(cfg)

			if err := Save(path, cfg); err == nil {
				t.Fatal("Save() error = nil, want type metadata rejection")
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Stat() error = %v, want file not created", err)
			}
		})
	}
}

func TestSaveReplacesBroadConfigPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, New()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}
