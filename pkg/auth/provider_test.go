package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tokencache "github.com/kubesphere/ksctl/pkg/cache/token"
)

func TestProviderUsesExplicitTokenWithoutReadingCache(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(tokencache.Path(cacheDir, "local"), []byte("not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	provider := NewProvider(ProviderOptions{CacheDir: cacheDir})
	got, err := provider.Token(context.Background(), Resolved{
		Context:       "local",
		ExplicitToken: "explicit-token",
	}, TokenOptions{})
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if got != "explicit-token" {
		t.Fatalf("Token() = %q, want explicit-token", got)
	}
}

func TestProviderUsesValidCacheBeforeLegacyTokens(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := tokencache.Save(cacheDir, "local", tokencache.NewEntry(tokencache.Response{
		AccessToken:  "cached-token",
		RefreshToken: "refresh-token",
		ExpiresIn:    7200,
	}, now)); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	provider := NewProvider(ProviderOptions{CacheDir: cacheDir, Now: func() time.Time { return now }})
	got, err := provider.Token(context.Background(), Resolved{
		Context:     "local",
		BearerToken: "legacy-token",
	}, TokenOptions{})
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if got != "cached-token" {
		t.Fatalf("Token() = %q, want cached-token", got)
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
		BearerTokenFile: tokenPath,
		BearerToken:     "legacy-token",
	}, TokenOptions{})
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if got != "file-token" {
		t.Fatalf("Token() = %q, want file-token", got)
	}

	got, err = provider.Token(context.Background(), Resolved{
		Context:     "local",
		BearerToken: "legacy-token",
	}, TokenOptions{})
	if err != nil {
		t.Fatalf("Token() bearer fallback error = %v", err)
	}
	if got != "legacy-token" {
		t.Fatalf("Token() = %q, want legacy-token", got)
	}
}

func TestProviderReturnsMalformedCacheError(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(tokencache.Path(cacheDir, "local"), []byte("not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	provider := NewProvider(ProviderOptions{CacheDir: cacheDir})

	_, err := provider.Token(context.Background(), Resolved{Context: "local"}, TokenOptions{})
	if err == nil || !strings.Contains(err.Error(), `load token cache for context "local"`) {
		t.Fatalf("Token() error = %v, want malformed cache error", err)
	}
}

func TestProviderRefreshesExpiredCache(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := tokencache.Save(cacheDir, "local", tokencache.NewEntry(tokencache.Response{
		AccessToken:  "expired-token",
		RefreshToken: "expired-refresh-token",
		ExpiresIn:    1,
	}, now.Add(-time.Hour))); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	refresher := &recordingTokenRefresher{response: tokencache.Response{
		AccessToken:  "refreshed-token",
		RefreshToken: "new-refresh-token",
		TokenType:    "bearer",
		ExpiresIn:    3600,
	}}
	provider := NewProvider(ProviderOptions{
		CacheDir:  cacheDir,
		Now:       func() time.Time { return now },
		Refresher: refresher,
	})

	got, err := provider.Token(context.Background(), Resolved{
		Endpoint: "https://ks.example.com",
		Context:  "local",
	}, TokenOptions{UserAgent: "ksctl/test"})
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if got != "refreshed-token" {
		t.Fatalf("Token() = %q, want refreshed-token", got)
	}
	if refresher.calls != 1 || refresher.options.Endpoint != "https://ks.example.com" || refresher.options.RefreshToken != "expired-refresh-token" || refresher.options.UserAgent != "ksctl/test" {
		t.Fatalf("Refresh() calls = %d, options = %#v", refresher.calls, refresher.options)
	}
	entry, err := tokencache.Load(cacheDir, "local")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if entry.AccessToken != "refreshed-token" || entry.RefreshToken != "new-refresh-token" {
		t.Fatalf("cached token = %#v", entry)
	}
}

func TestProviderDoesNotFallBackWhenRefreshFails(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := tokencache.Save(cacheDir, "local", tokencache.NewEntry(tokencache.Response{
		AccessToken:  "expired-token",
		RefreshToken: "expired-refresh-token",
		ExpiresIn:    1,
	}, now.Add(-time.Hour))); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	provider := NewProvider(ProviderOptions{
		CacheDir:  cacheDir,
		Now:       func() time.Time { return now },
		Refresher: &recordingTokenRefresher{err: errors.New("refresh denied")},
	})

	_, err := provider.Token(context.Background(), Resolved{
		Endpoint:    "https://ks.example.com",
		Context:     "local",
		BearerToken: "legacy-token",
	}, TokenOptions{})
	if err == nil || !strings.Contains(err.Error(), `login required for context "local"`) {
		t.Fatalf("Token() error = %v, want login required", err)
	}
}

func TestProviderRequiresLoginWhenExpiredCacheNeedsMissingRefresher(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := tokencache.Save(cacheDir, "local", tokencache.NewEntry(tokencache.Response{
		AccessToken:  "expired-token",
		RefreshToken: "expired-refresh-token",
		ExpiresIn:    1,
	}, now.Add(-time.Hour))); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	provider := NewProvider(ProviderOptions{CacheDir: cacheDir, Now: func() time.Time { return now }})

	_, err := provider.Token(context.Background(), Resolved{Context: "local"}, TokenOptions{})
	if err == nil || !strings.Contains(err.Error(), `login required for context "local"`) {
		t.Fatalf("Token() error = %v, want login required", err)
	}
}

func TestProviderFallsBackWhenExpiredCacheHasNoRefreshToken(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	cacheDir := filepath.Join(t.TempDir(), "tokens")
	if err := tokencache.Save(cacheDir, "local", tokencache.NewEntry(tokencache.Response{
		AccessToken: "expired-token",
		ExpiresIn:   1,
	}, now.Add(-time.Hour))); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	provider := NewProvider(ProviderOptions{CacheDir: cacheDir, Now: func() time.Time { return now }})

	got, err := provider.Token(context.Background(), Resolved{
		Context:     "local",
		BearerToken: "legacy-token",
	}, TokenOptions{})
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if got != "legacy-token" {
		t.Fatalf("Token() = %q, want legacy-token", got)
	}
}

func TestProviderRequiresLoginWhenNoTokenIsAvailable(t *testing.T) {
	provider := NewProvider(ProviderOptions{CacheDir: filepath.Join(t.TempDir(), "tokens")})
	_, err := provider.Token(context.Background(), Resolved{Context: "local"}, TokenOptions{})
	if err == nil || !strings.Contains(err.Error(), `login required for context "local"`) {
		t.Fatalf("Token() error = %v, want login required", err)
	}
}

type recordingTokenRefresher struct {
	response tokencache.Response
	err      error
	calls    int
	options  TokenRequestOptions
}

func (r *recordingTokenRefresher) Refresh(_ context.Context, options TokenRequestOptions) (tokencache.Response, error) {
	r.calls++
	r.options = options
	return r.response, r.err
}
