package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	tokencache "github.com/kubesphere/ksctl/pkg/cache/token"
	"github.com/kubesphere/ksctl/pkg/config"
	kubesphererest "kubesphere.io/client-go/rest"
)

type TokenRequestOptions struct {
	Endpoint              string
	Username              string
	Password              string
	RefreshToken          string
	UserAgent             string
	Timeout               time.Duration
	InsecureSkipTLSVerify bool
	TLSClientConfig       config.TLSClientConfig
}

type LogoutOptions struct {
	Endpoint              string
	AccessToken           string
	UserAgent             string
	Timeout               time.Duration
	InsecureSkipTLSVerify bool
	TLSClientConfig       config.TLSClientConfig
}

type RESTClientFactory interface {
	ForConfig(*kubesphererest.Config) (kubesphererest.Interface, error)
}

type OAuth struct {
	factory RESTClientFactory
}

func NewOAuth(factory RESTClientFactory) *OAuth {
	return &OAuth{factory: factory}
}

func (o *OAuth) Login(ctx context.Context, options TokenRequestOptions) (tokencache.Response, error) {
	form := url.Values{
		"grant_type":    []string{"password"},
		"client_id":     []string{"kubesphere"},
		"client_secret": []string{"kubesphere"},
		"username":      []string{options.Username},
		"password":      []string{options.Password},
	}
	return o.requestToken(ctx, options, form, "KubeSphere login")
}

func (o *OAuth) Refresh(ctx context.Context, options TokenRequestOptions) (tokencache.Response, error) {
	form := url.Values{
		"grant_type":    []string{"refresh_token"},
		"client_id":     []string{"kubesphere"},
		"client_secret": []string{"kubesphere"},
		"refresh_token": []string{options.RefreshToken},
	}
	return o.requestToken(ctx, options, form, "KubeSphere token refresh")
}

func (o *OAuth) Logout(ctx context.Context, options LogoutOptions) error {
	if o == nil || o.factory == nil {
		return fmt.Errorf("KubeSphere REST client factory is required")
	}
	config := &kubesphererest.Config{
		Host:            options.Endpoint,
		UserAgent:       options.UserAgent,
		Timeout:         options.Timeout,
		WarningHandler:  kubesphererest.NoWarnings{},
		TLSClientConfig: toKubeSphereTLSClientConfig(options.TLSClientConfig, options.InsecureSkipTLSVerify),
	}
	config.Wrap(redactLogoutResponses)
	client, err := o.factory.ForConfig(config)
	if err != nil {
		return fmt.Errorf("create KubeSphere logout client: %w", err)
	}
	if err := client.Get().
		AbsPath("/oauth/logout").
		SetHeader("Authorization", "Bearer "+options.AccessToken).
		MaxRetries(0).
		Do(ctx).
		Error(); err != nil {
		return fmt.Errorf("KubeSphere logout failed")
	}
	return nil
}

func redactLogoutResponses(delegate http.RoundTripper) http.RoundTripper {
	return roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		response, err := delegate.RoundTrip(request)
		if err != nil || response == nil {
			return response, err
		}
		if response.Body != nil {
			_ = response.Body.Close()
		}
		response.Body = http.NoBody
		response.ContentLength = 0
		response.TransferEncoding = nil
		response.Header.Del("Content-Length")
		return response, nil
	})
}

func (o *OAuth) requestToken(ctx context.Context, options TokenRequestOptions, form url.Values, operation string) (tokencache.Response, error) {
	if o == nil || o.factory == nil {
		return tokencache.Response{}, fmt.Errorf("KubeSphere REST client factory is required")
	}
	config := &kubesphererest.Config{
		Host:            options.Endpoint,
		UserAgent:       options.UserAgent,
		Timeout:         options.Timeout,
		TLSClientConfig: toKubeSphereTLSClientConfig(options.TLSClientConfig, options.InsecureSkipTLSVerify),
	}
	config.Wrap(redactOAuthErrorResponses)
	client, err := o.factory.ForConfig(config)
	if err != nil {
		return tokencache.Response{}, fmt.Errorf("create %s client: %w", operation, err)
	}
	stream, err := client.Post().
		AbsPath("/oauth/token").
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		Body(strings.NewReader(form.Encode())).
		Stream(ctx)
	if err != nil {
		return tokencache.Response{}, fmt.Errorf("%s failed", operation)
	}
	defer stream.Close()
	raw, err := io.ReadAll(stream)
	if err != nil {
		return tokencache.Response{}, fmt.Errorf("read %s response: %w", operation, err)
	}

	var response tokencache.Response
	if err := json.Unmarshal(raw, &response); err != nil || response.AccessToken == "" {
		return tokencache.Response{}, fmt.Errorf("%s returned an invalid token response", operation)
	}
	return response, nil
}

func redactOAuthErrorResponses(delegate http.RoundTripper) http.RoundTripper {
	return roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		response, err := delegate.RoundTrip(request)
		if err != nil || response == nil || response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			return response, err
		}
		if response.Body != nil {
			_ = response.Body.Close()
		}
		response.Body = http.NoBody
		response.ContentLength = 0
		response.TransferEncoding = nil
		response.Header.Del("Content-Length")
		return response, nil
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func toKubeSphereTLSClientConfig(cfg config.TLSClientConfig, insecureOverride bool) kubesphererest.TLSClientConfig {
	return kubesphererest.TLSClientConfig{
		Insecure:   cfg.Insecure || insecureOverride,
		ServerName: cfg.ServerName,
		CertFile:   cfg.CertFile,
		KeyFile:    cfg.KeyFile,
		CAFile:     cfg.CAFile,
		CertData:   []byte(cfg.CertData),
		KeyData:    []byte(cfg.KeyData),
		CAData:     []byte(cfg.CAData),
		NextProtos: cfg.NextProtos,
	}
}
