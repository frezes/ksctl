package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tokencache "github.com/kubesphere/ksctl/pkg/cache/token"
	"github.com/kubesphere/ksctl/pkg/config"
)

const tokenSafetyWindow = 30 * time.Second

type TokenOptions struct {
	UserAgent             string
	Timeout               time.Duration
	InsecureSkipTLSVerify bool
	TLSClientConfig       config.TLSClientConfig
}

type TokenProvider interface {
	Token(context.Context, Resolved, TokenOptions) (string, error)
}

type TokenRefresher interface {
	Refresh(context.Context, TokenRequestOptions) (tokencache.Response, error)
}

type ProviderOptions struct {
	CacheDir  string
	Now       func() time.Time
	Refresher TokenRefresher
}

type Provider struct {
	cacheDir  string
	now       func() time.Time
	refresher TokenRefresher
}

func NewProvider(options ProviderOptions) TokenProvider {
	cacheDir := options.CacheDir
	if cacheDir == "" {
		cacheDir = tokencache.DefaultDir()
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Provider{cacheDir: cacheDir, now: now, refresher: options.Refresher}
}

func (p *Provider) Token(ctx context.Context, resolved Resolved, options TokenOptions) (string, error) {
	if resolved.ExplicitToken != "" {
		return resolved.ExplicitToken, nil
	}
	if resolved.Context == "" {
		return "", fmt.Errorf("error: login required")
	}

	entry, err := tokencache.Load(p.cacheDir, resolved.Context)
	if err == nil {
		now := p.now()
		if entry.ValidAt(now, tokenSafetyWindow) {
			return entry.AccessToken, nil
		}
		if entry.RefreshToken != "" {
			if p.refresher == nil {
				return "", loginRequiredError(resolved.Context)
			}
			response, refreshErr := p.refresher.Refresh(ctx, TokenRequestOptions{
				Endpoint:              resolved.Endpoint,
				RefreshToken:          entry.RefreshToken,
				UserAgent:             options.UserAgent,
				Timeout:               options.Timeout,
				InsecureSkipTLSVerify: options.InsecureSkipTLSVerify,
				TLSClientConfig:       options.TLSClientConfig,
			})
			if refreshErr != nil {
				return "", loginRequiredError(resolved.Context)
			}
			if err := tokencache.Save(p.cacheDir, resolved.Context, tokencache.NewEntry(response, now)); err != nil {
				return "", err
			}
			return response.AccessToken, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("load token cache for context %q: %w", resolved.Context, err)
	}

	if resolved.BearerTokenFile != "" {
		data, err := os.ReadFile(resolved.BearerTokenFile)
		if err != nil {
			return "", fmt.Errorf("read bearer token file %q: %w", resolved.BearerTokenFile, err)
		}
		if value := strings.TrimSpace(string(data)); value != "" {
			return value, nil
		}
	}
	if resolved.BearerToken != "" {
		return resolved.BearerToken, nil
	}
	return "", loginRequiredError(resolved.Context)
}

func loginRequiredError(contextName string) error {
	return fmt.Errorf("error: login required for context %q", contextName)
}
