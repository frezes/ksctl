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

type TokenRequester interface {
	Login(context.Context, TokenRequestOptions) (tokencache.Response, error)
	Refresh(context.Context, TokenRequestOptions) (tokencache.Response, error)
}

type ProviderOptions struct {
	CacheDir  string
	Now       func() time.Time
	Requester TokenRequester
}

type Provider struct {
	cacheDir  string
	now       func() time.Time
	requester TokenRequester
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
	return &Provider{cacheDir: cacheDir, now: now, requester: options.Requester}
}

func (p *Provider) Token(ctx context.Context, resolved Resolved, options TokenOptions) (string, error) {
	if resolved.ExplicitToken != "" {
		return resolved.ExplicitToken, nil
	}
	if resolved.Context == "" {
		return "", fmt.Errorf("error: login required")
	}
	if resolved.BearerTokenFile != "" {
		data, err := os.ReadFile(resolved.BearerTokenFile)
		if err != nil {
			return "", fmt.Errorf("read bearer token file %q: %w", resolved.BearerTokenFile, err)
		}
		value := strings.TrimSpace(string(data))
		if value == "" {
			return "", fmt.Errorf("bearer token file %q is empty", resolved.BearerTokenFile)
		}
		return value, nil
	}
	if resolved.BearerToken != "" {
		return resolved.BearerToken, nil
	}

	entry, err := tokencache.Load(p.cacheDir, resolved.Fleet, resolved.User)
	if err == nil {
		now := p.now()
		if entry.ValidAt(now, tokenSafetyWindow) {
			return entry.AccessToken, nil
		}
		if entry.RefreshToken != "" {
			if p.requester != nil {
				response, refreshErr := p.requester.Refresh(ctx, TokenRequestOptions{
					Endpoint:              resolved.Endpoint,
					RefreshToken:          entry.RefreshToken,
					UserAgent:             options.UserAgent,
					Timeout:               options.Timeout,
					InsecureSkipTLSVerify: options.InsecureSkipTLSVerify,
					TLSClientConfig:       options.TLSClientConfig,
				})
				if refreshErr == nil {
					if err := tokencache.Save(p.cacheDir, resolved.Fleet, resolved.User, tokencache.NewEntry(response, now)); err != nil {
						return "", err
					}
					return response.AccessToken, nil
				}
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("load token cache for context %q: %w", resolved.Context, err)
	}

	if resolved.Password != "" && p.requester != nil {
		response, err := p.requester.Login(ctx, TokenRequestOptions{
			Endpoint:              resolved.Endpoint,
			Username:              resolved.Username,
			Password:              resolved.Password,
			UserAgent:             options.UserAgent,
			Timeout:               options.Timeout,
			InsecureSkipTLSVerify: options.InsecureSkipTLSVerify,
			TLSClientConfig:       options.TLSClientConfig,
		})
		if err != nil {
			return "", err
		}
		return response.AccessToken, nil
	}
	return "", loginRequiredError(resolved.Context)
}

func loginRequiredError(contextName string) error {
	return fmt.Errorf("error: login required for context %q", contextName)
}
