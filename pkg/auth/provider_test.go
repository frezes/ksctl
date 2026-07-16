package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tokencache "github.com/kubesphere/ksctl/pkg/cache/token"
	"github.com/kubesphere/ksctl/pkg/config"
)

func TestProviderUsesExplicitTokenWithoutReadingCache(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(cacheDir, "prod"), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(tokencache.Path(cacheDir, "prod", "admin"), []byte("not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	provider := NewProvider(ProviderOptions{CacheDir: cacheDir})
	got, err := provider.Token(context.Background(), Resolved{
		Context:       "local",
		Fleet:         "prod",
		User:          "admin",
		ExplicitToken: "explicit-token",
	}, TokenOptions{})
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if got != "explicit-token" {
		t.Fatalf("Token() = %q, want explicit-token", got)
	}
}

func TestProviderUsesConfiguredTokenBeforeValidCache(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := tokencache.Save(cacheDir, "prod", "admin", tokencache.NewEntry(tokencache.Response{
		AccessToken:  "cached-token",
		RefreshToken: "refresh-token",
		ExpiresIn:    7200,
	}, now)); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	requester := &recordingTokenRequester{}
	provider := NewProvider(ProviderOptions{CacheDir: cacheDir, Now: func() time.Time { return now }, Requester: requester})
	got, err := provider.Token(context.Background(), Resolved{
		Context:     "local",
		Fleet:       "prod",
		User:        "admin",
		BearerToken: "config-token",
		Password:    "configured-password",
	}, TokenOptions{})
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if got != "config-token" {
		t.Fatalf("Token() = %q, want config-token", got)
	}
	if requester.loginCalls != 0 || requester.refreshCalls != 0 {
		t.Fatalf("requester calls = login %d, refresh %d", requester.loginCalls, requester.refreshCalls)
	}
}

func TestProviderUsesConfiguredTokenFileBeforeExpiredCache(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := tokencache.Save(cacheDir, "prod", "admin", tokencache.NewEntry(tokencache.Response{
		AccessToken: "expired-token", RefreshToken: "refresh-token", ExpiresIn: 1,
	}, now.Add(-time.Hour))); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("file-token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	requester := &recordingTokenRequester{}
	provider := NewProvider(ProviderOptions{CacheDir: cacheDir, Now: func() time.Time { return now }, Requester: requester})

	got, err := provider.Token(context.Background(), Resolved{
		Context: "local", Fleet: "prod", User: "admin", BearerTokenFile: tokenPath, BearerToken: "config-token", Password: "configured-password",
	}, TokenOptions{})
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if got != "file-token" {
		t.Fatalf("Token() = %q, want file-token", got)
	}
	if requester.loginCalls != 0 || requester.refreshCalls != 0 {
		t.Fatalf("requester calls = login %d, refresh %d", requester.loginCalls, requester.refreshCalls)
	}
}

func TestProviderFallsBackToConfiguredTokensWhenCacheIsMissing(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("file-token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	provider := NewProvider(ProviderOptions{CacheDir: filepath.Join(t.TempDir(), "tokens")})

	got, err := provider.Token(context.Background(), Resolved{
		Context:         "local",
		Fleet:           "prod",
		User:            "admin",
		BearerTokenFile: tokenPath,
		BearerToken:     "config-token",
	}, TokenOptions{})
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if got != "file-token" {
		t.Fatalf("Token() = %q, want file-token", got)
	}

	got, err = provider.Token(context.Background(), Resolved{
		Context:     "local",
		Fleet:       "prod",
		User:        "admin",
		BearerToken: "config-token",
	}, TokenOptions{})
	if err != nil {
		t.Fatalf("Token() bearer fallback error = %v", err)
	}
	if got != "config-token" {
		t.Fatalf("Token() = %q, want config-token", got)
	}
}

func TestProviderReturnsEmptyConfiguredTokenFileError(t *testing.T) {
	emptyPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(emptyPath, []byte(" \n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	requester := &recordingTokenRequester{}
	provider := NewProvider(ProviderOptions{CacheDir: filepath.Join(t.TempDir(), "tokens"), Requester: requester})

	_, err := provider.Token(context.Background(), Resolved{
		Context: "local", Fleet: "prod", User: "admin", BearerTokenFile: emptyPath, BearerToken: "fallback-token", Password: "fallback-password",
	}, TokenOptions{})
	if err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("Token() error = %v, want empty token file error", err)
	}
	if requester.loginCalls != 0 || requester.refreshCalls != 0 {
		t.Fatalf("requester calls = login %d, refresh %d", requester.loginCalls, requester.refreshCalls)
	}
}

func TestProviderUsesConfiguredPasswordWithoutSavingCache(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	requester := &recordingTokenRequester{loginResponse: tokencache.Response{AccessToken: "issued-token"}}
	provider := NewProvider(ProviderOptions{CacheDir: cacheDir, Requester: requester})
	tlsConfig := config.TLSClientConfig{ServerName: "ks.example.com", CAData: "ca-data"}

	got, err := provider.Token(context.Background(), Resolved{
		Endpoint: "https://ks.example.com", Context: "local", Fleet: "prod", User: "admin", Username: "admin", Password: "configured-password",
	}, TokenOptions{UserAgent: "ksctl/test", Timeout: time.Minute, TLSClientConfig: tlsConfig})
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if got != "issued-token" {
		t.Fatalf("Token() = %q, want issued-token", got)
	}
	if requester.loginCalls != 1 || requester.loginOptions.Endpoint != "https://ks.example.com" || requester.loginOptions.Username != "admin" || requester.loginOptions.Password != "configured-password" || requester.loginOptions.UserAgent != "ksctl/test" || requester.loginOptions.Timeout != time.Minute || !reflect.DeepEqual(requester.loginOptions.TLSClientConfig, tlsConfig) {
		t.Fatalf("Login() calls = %d, options = %#v", requester.loginCalls, requester.loginOptions)
	}
	if _, err := tokencache.Load(cacheDir, "prod", "admin"); !os.IsNotExist(err) {
		t.Fatalf("Load() error = %v, want not exist", err)
	}
}

func TestProviderReturnsMalformedCacheError(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(cacheDir, "prod"), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(tokencache.Path(cacheDir, "prod", "admin"), []byte("not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	provider := NewProvider(ProviderOptions{CacheDir: cacheDir})

	_, err := provider.Token(context.Background(), Resolved{Context: "local", Fleet: "prod", User: "admin"}, TokenOptions{})
	if err == nil || !strings.Contains(err.Error(), `load token cache for context "local"`) {
		t.Fatalf("Token() error = %v, want malformed cache error", err)
	}
}

func TestProviderRefreshesExpiredCache(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := tokencache.Save(cacheDir, "prod", "admin", tokencache.NewEntry(tokencache.Response{
		AccessToken:  "expired-token",
		RefreshToken: "expired-refresh-token",
		ExpiresIn:    1,
	}, now.Add(-time.Hour))); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	requester := &recordingTokenRequester{refreshResponse: tokencache.Response{
		AccessToken:  "refreshed-token",
		RefreshToken: "new-refresh-token",
		TokenType:    "bearer",
		ExpiresIn:    3600,
	}}
	provider := NewProvider(ProviderOptions{
		CacheDir:  cacheDir,
		Now:       func() time.Time { return now },
		Requester: requester,
	})

	got, err := provider.Token(context.Background(), Resolved{
		Endpoint: "https://ks.example.com",
		Context:  "local",
		Fleet:    "prod",
		User:     "admin",
	}, TokenOptions{UserAgent: "ksctl/test"})
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if got != "refreshed-token" {
		t.Fatalf("Token() = %q, want refreshed-token", got)
	}
	if requester.refreshCalls != 1 || requester.refreshOptions.Endpoint != "https://ks.example.com" || requester.refreshOptions.RefreshToken != "expired-refresh-token" || requester.refreshOptions.UserAgent != "ksctl/test" {
		t.Fatalf("Refresh() calls = %d, options = %#v", requester.refreshCalls, requester.refreshOptions)
	}
	entry, err := tokencache.Load(cacheDir, "prod", "admin")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if entry.AccessToken != "refreshed-token" || entry.RefreshToken != "new-refresh-token" {
		t.Fatalf("cached token = %#v", entry)
	}
}

func TestProviderUsesConfiguredPasswordWhenRefreshFails(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := tokencache.Save(cacheDir, "prod", "admin", tokencache.NewEntry(tokencache.Response{
		AccessToken:  "expired-token",
		RefreshToken: "expired-refresh-token",
		ExpiresIn:    1,
	}, now.Add(-time.Hour))); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	requester := &recordingTokenRequester{
		refreshErr:    errors.New("refresh denied"),
		loginResponse: tokencache.Response{AccessToken: "issued-token"},
	}
	provider := NewProvider(ProviderOptions{
		CacheDir:  cacheDir,
		Now:       func() time.Time { return now },
		Requester: requester,
	})

	got, err := provider.Token(context.Background(), Resolved{
		Endpoint: "https://ks.example.com",
		Context:  "local",
		Fleet:    "prod",
		User:     "admin",
		Username: "admin",
		Password: "configured-password",
	}, TokenOptions{})
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if got != "issued-token" || requester.refreshCalls != 1 || requester.loginCalls != 1 {
		t.Fatalf("Token() = %q, requester = %#v", got, requester)
	}
	entry, err := tokencache.Load(cacheDir, "prod", "admin")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if entry.AccessToken != "expired-token" {
		t.Fatalf("cache was replaced: %#v", entry)
	}
}

func TestProviderRequiresLoginWhenExpiredCacheNeedsMissingRefresher(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := tokencache.Save(cacheDir, "prod", "admin", tokencache.NewEntry(tokencache.Response{
		AccessToken:  "expired-token",
		RefreshToken: "expired-refresh-token",
		ExpiresIn:    1,
	}, now.Add(-time.Hour))); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	provider := NewProvider(ProviderOptions{CacheDir: cacheDir, Now: func() time.Time { return now }})

	_, err := provider.Token(context.Background(), Resolved{Context: "local", Fleet: "prod", User: "admin"}, TokenOptions{})
	if err == nil || !strings.Contains(err.Error(), `login required for context "local"`) {
		t.Fatalf("Token() error = %v, want login required", err)
	}
}

func TestProviderFallsBackWhenExpiredCacheHasNoRefreshToken(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := tokencache.Save(cacheDir, "prod", "admin", tokencache.NewEntry(tokencache.Response{
		AccessToken: "expired-token",
		ExpiresIn:   1,
	}, now.Add(-time.Hour))); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	requester := &recordingTokenRequester{loginResponse: tokencache.Response{AccessToken: "issued-token"}}
	provider := NewProvider(ProviderOptions{CacheDir: cacheDir, Now: func() time.Time { return now }, Requester: requester})

	got, err := provider.Token(context.Background(), Resolved{
		Endpoint: "https://ks.example.com",
		Context:  "local",
		Fleet:    "prod",
		User:     "admin",
		Username: "admin",
		Password: "configured-password",
	}, TokenOptions{})
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if got != "issued-token" || requester.loginCalls != 1 {
		t.Fatalf("Token() = %q, login calls = %d", got, requester.loginCalls)
	}
}

func TestProviderRequiresLoginWhenNoTokenIsAvailable(t *testing.T) {
	provider := NewProvider(ProviderOptions{CacheDir: filepath.Join(t.TempDir(), "tokens")})
	_, err := provider.Token(context.Background(), Resolved{Context: "local", Fleet: "prod", User: "admin"}, TokenOptions{})
	if err == nil || !strings.Contains(err.Error(), `login required for context "local"`) {
		t.Fatalf("Token() error = %v, want login required", err)
	}
}

func TestProviderSharesCacheAcrossContextsAndSeparatesFleets(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := tokencache.Save(cacheDir, "prod", "admin", tokencache.NewEntry(tokencache.Response{AccessToken: "prod-token", ExpiresIn: 3600}, now)); err != nil {
		t.Fatalf("Save(prod/admin) error = %v", err)
	}
	if err := tokencache.Save(cacheDir, "staging", "admin", tokencache.NewEntry(tokencache.Response{AccessToken: "staging-token", ExpiresIn: 3600}, now)); err != nil {
		t.Fatalf("Save(staging/admin) error = %v", err)
	}
	provider := NewProvider(ProviderOptions{CacheDir: cacheDir, Now: func() time.Time { return now }})

	for _, test := range []struct {
		context, fleet, token string
	}{
		{context: "prod-admin", fleet: "prod", token: "prod-token"},
		{context: "prod-admin-cluster-a", fleet: "prod", token: "prod-token"},
		{context: "staging-admin", fleet: "staging", token: "staging-token"},
	} {
		got, err := provider.Token(context.Background(), Resolved{Context: test.context, Fleet: test.fleet, User: "admin"}, TokenOptions{})
		if err != nil {
			t.Fatalf("Token(%q) error = %v", test.context, err)
		}
		if got != test.token {
			t.Fatalf("Token(%q) = %q, want %q", test.context, got, test.token)
		}
	}
}

type recordingTokenRequester struct {
	loginResponse   tokencache.Response
	refreshResponse tokencache.Response
	loginErr        error
	refreshErr      error
	loginCalls      int
	refreshCalls    int
	loginOptions    TokenRequestOptions
	refreshOptions  TokenRequestOptions
}

func (r *recordingTokenRequester) Login(_ context.Context, options TokenRequestOptions) (tokencache.Response, error) {
	r.loginCalls++
	r.loginOptions = options
	return r.loginResponse, r.loginErr
}

func (r *recordingTokenRequester) Refresh(_ context.Context, options TokenRequestOptions) (tokencache.Response, error) {
	r.refreshCalls++
	r.refreshOptions = options
	return r.refreshResponse, r.refreshErr
}
