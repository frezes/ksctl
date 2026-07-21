package auth

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	clientkubesphere "github.com/kubesphere/ksctl/pkg/client/kubesphere"
	"github.com/kubesphere/ksctl/pkg/config"
	"k8s.io/klog/v2"
	kubesphererest "kubesphere.io/client-go/rest"
)

func TestLoginUsesKubeSphereOAuthTokenEndpoint(t *testing.T) {
	defer klog.CaptureState().Restore()
	var flags flag.FlagSet
	klog.InitFlags(&flags)
	if err := flags.Set("v", "8"); err != nil {
		t.Fatalf("set klog verbosity: %v", err)
	}
	var logs bytes.Buffer
	klog.SetOutput(&logs)
	klog.LogToStderr(false)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertTokenRequest(t, r, map[string]string{
			"grant_type":    "password",
			"client_id":     "kubesphere",
			"client_secret": "kubesphere",
			"username":      "admin",
			"password":      "temporary-password",
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"issued-token","refresh_token":"refresh-token","token_type":"bearer","expires_in":7200}`)
	}))
	defer server.Close()

	factory := &recordingRESTClientFactory{delegate: clientkubesphere.NewRESTClientFactory(&http.Client{})}
	response, err := NewOAuth(factory).Login(context.Background(), TokenRequestOptions{
		Endpoint:  server.URL,
		Username:  "admin",
		Password:  "temporary-password",
		UserAgent: "ksctl/test",
		Timeout:   15 * time.Second,
		TLSClientConfig: config.TLSClientConfig{
			ServerName: "ks.example.com",
			CAData:     "test-ca",
		},
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if response.AccessToken != "issued-token" || response.RefreshToken != "refresh-token" || response.TokenType != "bearer" || response.ExpiresIn != 7200 {
		t.Fatalf("response = %#v", response)
	}
	if factory.config == nil {
		t.Fatal("REST client factory was not called")
	}
	if factory.config.Host != server.URL || factory.config.APIPath != "" || factory.config.UserAgent != "ksctl/test" || factory.config.Timeout != 15*time.Second {
		t.Fatalf("REST config = %#v", factory.config)
	}
	if factory.config.ServerName != "ks.example.com" || string(factory.config.CAData) != "test-ca" {
		t.Fatalf("REST TLS config = %#v", factory.config.TLSClientConfig)
	}
	for _, secret := range []string{"temporary-password", "issued-token", "refresh-token"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("KubeSphere REST client logs expose credential %q:\n%s", secret, logs.String())
		}
	}
}

func TestRefreshUsesRefreshGrant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertTokenRequest(t, r, map[string]string{
			"grant_type":    "refresh_token",
			"client_id":     "kubesphere",
			"client_secret": "kubesphere",
			"refresh_token": "cached-refresh-token",
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"new-token","refresh_token":"new-refresh-token","token_type":"bearer","expires_in":3600}`)
	}))
	defer server.Close()

	response, err := newTestOAuth().Refresh(context.Background(), TokenRequestOptions{
		Endpoint:     server.URL,
		RefreshToken: "cached-refresh-token",
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if response.AccessToken != "new-token" || response.RefreshToken != "new-refresh-token" || response.ExpiresIn != 3600 {
		t.Fatalf("response = %#v", response)
	}
}

func TestLogoutRevokesBearerTokenAtKubeSphereEndpoint(t *testing.T) {
	defer klog.CaptureState().Restore()
	var flags flag.FlagSet
	klog.InitFlags(&flags)
	if err := flags.Set("v", "8"); err != nil {
		t.Fatalf("set klog verbosity: %v", err)
	}
	var logs bytes.Buffer
	klog.SetOutput(&logs)
	klog.LogToStderr(false)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/oauth/logout" {
			t.Errorf("request = %s %s, want GET /oauth/logout", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer cached-access-token" {
			t.Errorf("Authorization = %q, want cached bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer server.Close()

	factory := &recordingRESTClientFactory{delegate: clientkubesphere.NewRESTClientFactory(&http.Client{})}
	err := NewOAuth(factory).Logout(context.Background(), LogoutOptions{
		Endpoint:    server.URL,
		AccessToken: "cached-access-token",
		UserAgent:   "ksctl/test",
		Timeout:     15 * time.Second,
		TLSClientConfig: config.TLSClientConfig{
			ServerName: "ks.example.com",
			CAData:     "test-ca",
		},
	})
	if err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if factory.config.Host != server.URL || factory.config.UserAgent != "ksctl/test" || factory.config.Timeout != 15*time.Second {
		t.Fatalf("REST config = %#v", factory.config)
	}
	if factory.config.ServerName != "ks.example.com" || string(factory.config.CAData) != "test-ca" {
		t.Fatalf("REST TLS config = %#v", factory.config.TLSClientConfig)
	}
	if strings.Contains(logs.String(), "cached-access-token") {
		t.Fatalf("logout logs expose access token: %s", logs.String())
	}
}

func TestLogoutFailureDoesNotExposeAccessToken(t *testing.T) {
	defer klog.CaptureState().Restore()
	var flags flag.FlagSet
	klog.InitFlags(&flags)
	if err := flags.Set("v", "8"); err != nil {
		t.Fatalf("set klog verbosity: %v", err)
	}
	var logs bytes.Buffer
	klog.SetOutput(&logs)
	klog.LogToStderr(false)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "cached-access-token", http.StatusInternalServerError)
	}))
	defer server.Close()

	err := newTestOAuth().Logout(context.Background(), LogoutOptions{
		Endpoint: server.URL, AccessToken: "cached-access-token",
	})
	if err == nil {
		t.Fatal("Logout() error = nil, want logout error")
	}
	if strings.Contains(err.Error(), "cached-access-token") || strings.Contains(logs.String(), "cached-access-token") {
		t.Fatalf("Logout() exposes access token; error=%v logs=%q", err, logs.String())
	}
}

func TestTokenRequestFailureDoesNotExposeCredentials(t *testing.T) {
	defer klog.CaptureState().Restore()
	var flags flag.FlagSet
	klog.InitFlags(&flags)
	if err := flags.Set("v", "8"); err != nil {
		t.Fatalf("set klog verbosity: %v", err)
	}
	var logs bytes.Buffer
	klog.SetOutput(&logs)
	klog.LogToStderr(false)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "admin temporary-password", http.StatusUnauthorized)
	}))
	defer server.Close()

	oauth := NewOAuth(clientkubesphere.NewRESTClientFactory(&http.Client{}))
	_, err := oauth.Login(context.Background(), TokenRequestOptions{
		Endpoint: server.URL,
		Username: "admin",
		Password: "temporary-password",
	})
	if err == nil {
		t.Fatal("Login() error = nil, want login error")
	}
	for _, secret := range []string{"admin", "temporary-password"} {
		if strings.Contains(err.Error(), secret) || strings.Contains(logs.String(), secret) {
			t.Fatalf("OAuth failure exposes credential %q; error=%v logs=%q", secret, err, logs.String())
		}
	}
}

func TestOAuthRequiresRESTClientFactory(t *testing.T) {
	_, err := NewOAuth(nil).Login(context.Background(), TokenRequestOptions{Endpoint: "https://ks.example.com"})
	if err == nil || !strings.Contains(err.Error(), "REST client factory is required") {
		t.Fatalf("Login() error = %v, want REST client factory is required", err)
	}
}

func newTestOAuth() *OAuth {
	return NewOAuth(clientkubesphere.NewRESTClientFactory(nil))
}

type recordingRESTClientFactory struct {
	delegate *clientkubesphere.RESTClientFactory
	config   *kubesphererest.Config
}

func (f *recordingRESTClientFactory) ForConfig(config *kubesphererest.Config) (kubesphererest.Interface, error) {
	f.config = kubesphererest.CopyConfig(config)
	return f.delegate.ForConfig(config)
}

func assertTokenRequest(t *testing.T, r *http.Request, want map[string]string) {
	t.Helper()
	if r.Method != http.MethodPost {
		t.Errorf("Method = %q, want POST", r.Method)
	}
	if r.URL.Path != "/oauth/token" {
		t.Errorf("Path = %q, want /oauth/token", r.URL.Path)
	}
	if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/x-www-form-urlencoded") {
		t.Errorf("Content-Type = %q", got)
	}
	if err := r.ParseForm(); err != nil {
		t.Errorf("ParseForm() error = %v", err)
	}
	for key, value := range want {
		if got := r.Form.Get(key); got != value {
			t.Errorf("form[%q] = %q, want %q", key, got, value)
		}
	}
}
