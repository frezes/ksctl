package token

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCacheSaveLoadAndDelete(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tokens")
	now := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	entry := NewEntry(Response{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresIn:    7200,
	}, now)

	if err := Save(dir, "prod", "admin", entry); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat cache dir error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("cache dir mode = %v, want 0700", got)
	}

	fleetDir := filepath.Join(dir, "prod")
	info, err = os.Stat(fleetDir)
	if err != nil {
		t.Fatalf("Stat fleet cache dir error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("fleet cache dir mode = %v, want 0700", got)
	}

	path := Path(dir, "prod", "admin")
	if path != filepath.Join(dir, "prod", "admin.json") {
		t.Fatalf("Path() = %q", path)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("cache mode = %v, want 0600", got)
	}

	loaded, err := Load(dir, "prod", "admin")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.AccessToken != "access-token" || loaded.RefreshToken != "refresh-token" || loaded.TokenType != "Bearer" || loaded.ExpiresIn != 7200 {
		t.Fatalf("loaded token = %#v", loaded)
	}
	if !loaded.ObtainedAt.Equal(now) || !loaded.ExpiresAt.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("loaded times = %s / %s", loaded.ObtainedAt, loaded.ExpiresAt)
	}
	if !loaded.ValidAt(now.Add(time.Hour), 30*time.Second) {
		t.Fatal("ValidAt() = false, want true")
	}
	if loaded.ValidAt(now.Add(2*time.Hour-20*time.Second), 30*time.Second) {
		t.Fatal("ValidAt() = true inside safety window, want false")
	}

	if err := Delete(dir, "prod", "admin"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cache file still exists, err=%v", err)
	}
}

func TestCacheSeparatesSameNamedUsersByFleet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tokens")
	prod := NewEntry(Response{AccessToken: "prod-token", ExpiresIn: 3600}, time.Now())
	staging := NewEntry(Response{AccessToken: "staging-token", ExpiresIn: 3600}, time.Now())
	if err := Save(dir, "prod", "admin", prod); err != nil {
		t.Fatalf("Save(prod/admin) error = %v", err)
	}
	if err := Save(dir, "staging", "admin", staging); err != nil {
		t.Fatalf("Save(staging/admin) error = %v", err)
	}

	for _, test := range []struct{ fleet, token string }{{"prod", "prod-token"}, {"staging", "staging-token"}} {
		entry, err := Load(dir, test.fleet, "admin")
		if err != nil {
			t.Fatalf("Load(%s/admin) error = %v", test.fleet, err)
		}
		if entry.AccessToken != test.token {
			t.Fatalf("Load(%s/admin) token = %q", test.fleet, entry.AccessToken)
		}
	}

	if err := Delete(dir, "prod", "admin"); err != nil {
		t.Fatalf("Delete(prod/admin) error = %v", err)
	}
	if _, err := Load(dir, "staging", "admin"); err != nil {
		t.Fatalf("staging/admin removed with prod/admin: %v", err)
	}
}

func TestCacheDoesNotReadFlatContextCache(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tokens")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old-context.json"), []byte(`{"access_token":"old-token"}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Load(dir, "old-context", "admin"); !os.IsNotExist(err) {
		t.Fatalf("Load() error = %v, want not exist", err)
	}
}

func TestPathEncodesUnsafeComponentsWithoutCollisions(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		fleet string
		user  string
		want  string
	}{
		{fleet: "prod", user: "admin", want: filepath.Join(dir, "prod", "admin.json")},
		{fleet: ".", user: "..", want: filepath.Join(dir, "~2E", "~2E~2E.json")},
		{fleet: "a/b", user: "x:y", want: filepath.Join(dir, "a~2Fb", "x~3Ay.json")},
		{fleet: "a:b", user: "x/y", want: filepath.Join(dir, "a~3Ab", "x~2Fy.json")},
		{fleet: "", user: "~", want: filepath.Join(dir, "~", "~7E.json")},
	}
	seen := map[string]bool{}
	for _, test := range tests {
		got := Path(dir, test.fleet, test.user)
		if got != test.want {
			t.Errorf("Path(%q, %q) = %q, want %q", test.fleet, test.user, got, test.want)
		}
		if seen[got] {
			t.Errorf("Path() collision at %q", got)
		}
		seen[got] = true
		rel, err := filepath.Rel(dir, got)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("Path() escaped cache dir: rel=%q err=%v", rel, err)
		}
	}
}

func TestSaveReplacesBroadCachePermissions(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir, "prod", "admin")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := NewEntry(Response{AccessToken: "secret", ExpiresIn: 60}, time.Now())
	if err := Save(dir, "prod", "admin", entry); err != nil {
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
