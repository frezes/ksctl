package config

import "testing"

func TestRedactedCopyMasksSecretsWithoutMutatingSource(t *testing.T) {
	cfg := New()
	cfg.Fleets["prod"] = Fleet{
		Host: "https://prod.example.com",
		TLSClientConfig: TLSClientConfig{
			KeyData:    "private-key",
			CAData:     "public-ca",
			NextProtos: []string{"h2"},
		},
		Users: map[string]User{"admin": {
			BearerToken:     "token",
			BearerTokenFile: "/tokens/admin",
			Password:        "password",
		}},
	}

	redacted := RedactedCopy(cfg)
	fleet := redacted.Fleets["prod"]
	user := fleet.Users["admin"]
	if user.BearerToken != "<redacted>" || user.Password != "<redacted>" || fleet.TLSClientConfig.KeyData != "<redacted>" {
		t.Fatalf("redacted fleet = %#v", fleet)
	}
	if user.BearerTokenFile != "/tokens/admin" || fleet.TLSClientConfig.CAData != "public-ca" {
		t.Fatalf("non-secret fields changed: %#v", fleet)
	}

	fleet.TLSClientConfig.NextProtos[0] = "http/1.1"
	original := cfg.Fleets["prod"]
	if original.Users["admin"].BearerToken != "token" || original.Users["admin"].Password != "password" || original.TLSClientConfig.KeyData != "private-key" {
		t.Fatalf("source secrets mutated: %#v", original)
	}
	if original.TLSClientConfig.NextProtos[0] != "h2" {
		t.Fatalf("source slice mutated: %#v", original.TLSClientConfig.NextProtos)
	}
}

func TestRedactedCopyHandlesNil(t *testing.T) {
	if RedactedCopy(nil) != nil {
		t.Fatal("RedactedCopy(nil) is non-nil")
	}
}
