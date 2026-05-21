package token

import (
	"os"
	"path/filepath"
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

	if err := Save(dir, "local/context", entry); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat cache dir error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("cache dir mode = %v, want 0700", got)
	}

	path := Path(dir, "local/context")
	if filepath.Base(path) != "local-context.json" {
		t.Fatalf("Path() = %q", path)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("cache mode = %v, want 0600", got)
	}

	loaded, err := Load(dir, "local/context")
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

	if err := Delete(dir, "local/context"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cache file still exists, err=%v", err)
	}
}
