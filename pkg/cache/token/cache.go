package token

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kubesphere/ksctl/internal/securefile"
)

var unsafePathChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

const upperHex = "0123456789ABCDEF"

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
	return filepath.Join(dir, encodePathSegment(fleet), encodePathSegment(user)+".json")
}

func encodePathSegment(value string) string {
	if value == "" {
		return "~"
	}
	var encoded strings.Builder
	for _, char := range []byte(value) {
		if char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' ||
			char >= '0' && char <= '9' || char == '_' || char == '-' || char == '.' {
			encoded.WriteByte(char)
			continue
		}
		encoded.WriteByte('~')
		encoded.WriteByte(upperHex[char>>4])
		encoded.WriteByte(upperHex[char&0x0f])
	}
	name := encoded.String()
	if name == "." {
		return "~2E"
	}
	if name == ".." {
		return "~2E~2E"
	}
	return name
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
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	return securefile.Write(Path(dir, fleet, user), data)
}

func SaveWithRollback(dir, fleet, user string, entry Entry) (func() error, error) {
	path := Path(dir, fleet, user)
	previous, readErr := os.ReadFile(path)
	existed := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	if err := Save(dir, fleet, user, entry); err != nil {
		return nil, err
	}

	var once sync.Once
	var rollbackErr error
	return func() error {
		once.Do(func() {
			if existed {
				rollbackErr = securefile.Write(path, previous)
				return
			}
			rollbackErr = Delete(dir, fleet, user)
		})
		return rollbackErr
	}, nil
}

func Delete(dir, fleet, user string) error {
	err := os.Remove(Path(dir, fleet, user))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
