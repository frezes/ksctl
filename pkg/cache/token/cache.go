package token

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var unsafePathChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

type Response struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

type Entry struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int64     `json:"expires_in"`
	ObtainedAt   time.Time `json:"obtained_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func NewEntry(response Response, obtainedAt time.Time) Entry {
	return Entry{
		AccessToken:  response.AccessToken,
		RefreshToken: response.RefreshToken,
		TokenType:    response.TokenType,
		ExpiresIn:    response.ExpiresIn,
		ObtainedAt:   obtainedAt,
		ExpiresAt:    obtainedAt.Add(time.Duration(response.ExpiresIn) * time.Second),
	}
}

func (e Entry) ValidAt(now time.Time, safetyWindow time.Duration) bool {
	if e.AccessToken == "" || e.ExpiresAt.IsZero() {
		return false
	}
	return now.Add(safetyWindow).Before(e.ExpiresAt)
}

func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".ksctl", "cache", "tokens")
	}
	return filepath.Join(home, ".ksctl", "cache", "tokens")
}

func Path(dir, fleet, user string) string {
	return filepath.Join(dir, SafeName(fleet), SafeName(user)+".json")
}

func SafeName(value string) string {
	name := unsafePathChars.ReplaceAllString(value, "-")
	if name == "" {
		return "default"
	}
	return name
}

func Load(dir, fleet, user string) (Entry, error) {
	data, err := os.ReadFile(Path(dir, fleet, user))
	if err != nil {
		return Entry{}, err
	}
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func Save(dir, fleet, user string, entry Entry) error {
	fleetDir := filepath.Join(dir, SafeName(fleet))
	if err := os.MkdirAll(fleetDir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(dir, fleet, user), data, 0o600)
}

func Delete(dir, fleet, user string) error {
	err := os.Remove(Path(dir, fleet, user))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
