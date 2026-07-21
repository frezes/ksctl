# ksctl Server-Side Logout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `ksctl auth logout` ask KubeSphere to revoke the cached access token before unconditionally deleting the local Fleet/User token cache.

**Architecture:** Add a focused `OAuth.Logout` request operation for `GET /oauth/logout`, then inject the existing OAuth client into the logout command. The command reads only the selected Fleet/User cache, makes the remote request on a best-effort basis, ignores its result, and preserves the existing authoritative local delete behavior.

**Tech Stack:** Go 1.26, Cobra, KubeSphere REST client, `net/http/httptest`, Go `testing`.

## Global Constraints

- Remote logout uses the cached access token only; it must not resolve static tokens, refresh tokens, or passwords.
- Remote logout targets the Fleet host directly and never applies member-cluster routing.
- Remote errors are ignored; the local Fleet/User cache is always deleted.
- Config, Context, Fleet, User, and static credentials remain unchanged.
- Missing, malformed, and unreadable cache entries do not block local deletion.
- Access tokens must not appear in command output, returned errors, or verbose REST logs.

---

### Task 1: KubeSphere logout request boundary

**Files:**
- Modify: `pkg/auth/oauth.go`
- Test: `pkg/auth/oauth_test.go`

**Interfaces:**
- Consumes: `OAuth.factory`, `config.TLSClientConfig`, and the existing KubeSphere REST client factory.
- Produces: `type LogoutOptions struct` and `func (*OAuth) Logout(context.Context, LogoutOptions) error`.

- [ ] **Step 1: Write the failing request-shape test**

Add this complete test in `pkg/auth/oauth_test.go`:

```go
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
```

- [ ] **Step 2: Write the failing error-redaction test**

Add this test:

```go
func TestLogoutFailureDoesNotExposeAccessToken(t *testing.T) {
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
	if strings.Contains(err.Error(), "cached-access-token") {
		t.Fatalf("Logout() error exposes access token: %v", err)
	}
}
```

- [ ] **Step 3: Run the focused tests and verify RED**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/auth -run 'TestLogout' -count=1 -v
```

Expected: build failure because `LogoutOptions` and `(*OAuth).Logout` do not exist.

- [ ] **Step 4: Implement the minimal logout client**

Add to `pkg/auth/oauth.go`:

```go
type LogoutOptions struct {
	Endpoint              string
	AccessToken           string
	UserAgent             string
	Timeout               time.Duration
	InsecureSkipTLSVerify bool
	TLSClientConfig       config.TLSClientConfig
}

func (o *OAuth) Logout(ctx context.Context, options LogoutOptions) error {
	if o == nil || o.factory == nil {
		return fmt.Errorf("KubeSphere REST client factory is required")
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
		return fmt.Errorf("create KubeSphere logout client: %w", err)
	}
	if err := client.Get().
		AbsPath("/oauth/logout").
		SetHeader("Authorization", "Bearer "+options.AccessToken).
		Do(ctx).
		Error(); err != nil {
		return fmt.Errorf("KubeSphere logout failed")
	}
	return nil
}
```

- [ ] **Step 5: Run package tests and verify GREEN**

Run `env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/auth -count=1`.

Expected: PASS with no credential values in output.

- [ ] **Step 6: Commit the request boundary**

```bash
git add pkg/auth/oauth.go pkg/auth/oauth_test.go
git commit -m "add KubeSphere logout request"
```

---

### Task 2: Best-effort remote logout command orchestration

**Files:**
- Modify: `pkg/cmd/auth.go`
- Test: `pkg/cmd/auth_test.go`

**Interfaces:**
- Consumes: `(*auth.OAuth).Logout(context.Context, auth.LogoutOptions) error`, `tokencache.Load`, and `tokencache.Delete`.
- Produces: `newLogoutCommand(userAgent string, oauth *auth.OAuth) *cobra.Command`, registered by `newAuthCommand`.

- [ ] **Step 1: Make the existing logout test require a remote call**

Update `TestLogoutDeletesTokenCacheAndPreservesConfiguredCredentials` to use an `httptest.Server` as `fleet.Host`. Assert exactly one `GET /oauth/logout` request with `Bearer cached-token`, then retain the existing assertions that the cache is absent and the Config is byte-for-byte unchanged.

- [ ] **Step 2: Add failing best-effort tests**

Add this helper and these complete tests to `pkg/cmd/auth_test.go`:

```go
func prepareLogoutTest(t *testing.T, host string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("KSCTL_CONFIG", configPath)
	cfg := config.New()
	cfg.CurrentContext = "local"
	cfg.Fleets["local"] = config.Fleet{Host: host, Users: map[string]config.User{"admin": {Username: "admin"}}}
	cfg.Contexts["local"] = config.Context{Fleet: "local", User: "admin"}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	return filepath.Join(home, ".ksctl", "cache", "tokens")
}

func TestLogoutIgnoresRemoteFailureAndDeletesCache(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "remote failure", http.StatusInternalServerError)
	}))
	defer server.Close()
	cacheDir := prepareLogoutTest(t, server.URL)
	if err := tokencache.Save(cacheDir, "local", "admin", tokencache.NewEntry(tokencache.Response{AccessToken: "cached-token", ExpiresIn: 7200}, time.Now())); err != nil {
		t.Fatalf("Save token error = %v", err)
	}

	cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"auth", "logout"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("logout requests = %d, want 1", requests)
	}
	if _, err := tokencache.Load(cacheDir, "local", "admin"); !os.IsNotExist(err) {
		t.Fatalf("Load token cache error = %v, want not exist", err)
	}
}

func TestLogoutWithoutCacheSkipsRemoteRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	prepareLogoutTest(t, server.URL)
	cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"auth", "logout"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("logout requests = %d, want 0", requests)
	}
}

func TestLogoutDeletesMalformedCacheWithoutRemoteRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	cacheDir := prepareLogoutTest(t, server.URL)
	path := tokencache.Path(cacheDir, "local", "admin")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cmd := NewRootCommand(IOStreams{Out: new(bytes.Buffer), ErrOut: new(bytes.Buffer)}, VersionInfo{Version: "dev"})
	cmd.SetArgs([]string{"auth", "logout"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("logout requests = %d, want 0", requests)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Stat token cache error = %v, want not exist", err)
	}
}
```

- [ ] **Step 3: Run focused tests and verify RED**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/cmd -run '^TestLogout' -count=1 -v
```

Expected: remote-call assertions fail because the current command only deletes the cache.

- [ ] **Step 4: Implement best-effort ordering**

Register `newLogoutCommand(userAgent, oauth)` from `newAuthCommand`. Change the logout constructor signature and insert this block after Context/Fleet/User validation and before the existing delete:

```go
entry, loadErr := tokencache.Load(tokencache.DefaultDir(), ctx.Fleet, ctx.User)
if loadErr == nil && entry.AccessToken != "" && oauth != nil {
	_ = oauth.Logout(cmd.Context(), auth.LogoutOptions{
		Endpoint:        fleet.Host,
		AccessToken:     entry.AccessToken,
		UserAgent:       userAgent,
		Timeout:         30 * time.Second,
		TLSClientConfig: fleet.TLSClientConfig,
	})
}
if err := tokencache.Delete(tokencache.DefaultDir(), ctx.Fleet, ctx.User); err != nil {
	return err
}
```

Preserve the existing success output and all broken-reference errors. Do not add a Provider call or use configured token fields.

- [ ] **Step 5: Run focused and package tests and verify GREEN**

Run:

```bash
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/cmd -run '^TestLogout' -count=1 -v
env GOCACHE=/private/tmp/ksctl-go-build-cache go test ./pkg/cmd ./pkg/auth -count=1
```

Expected: PASS; remote failure, missing cache, and malformed cache all end with a missing local cache file.

- [ ] **Step 6: Commit command behavior**

```bash
git add pkg/cmd/auth.go pkg/cmd/auth_test.go
git commit -m "revoke cached token on logout"
```

---

### Task 3: Documentation and full verification

**Files:**
- Modify: `docs/cli.md`
- Modify: `docs/design.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: the final best-effort behavior from Tasks 1 and 2.
- Produces: user-facing documentation that distinguishes remote access-token revocation from unconditional local cleanup.

- [ ] **Step 1: Update CLI documentation**

Replace the local-only paragraph in `docs/cli.md` with:

```markdown
Logout makes a best-effort request to the Fleet's `/oauth/logout` endpoint
using the cached Access Token, then removes the token cache for the selected
Fleet and User regardless of the remote result. It does not delete Contexts or
manually configured credentials. Contexts that select the same Fleet and User
share that cache and logout state. A configured `bearerToken` or
`bearerTokenFile` can still authenticate later commands.
```

- [ ] **Step 2: Update architecture documentation and changelog**

Replace the local-only sentences in `docs/design.md` with:

```markdown
`auth logout` reads the cached Fleet/User Access Token and makes a best-effort,
unscoped request to `<fleet-endpoint>/oauth/logout`. It ignores remote errors
and always attempts to delete the local Fleet/User cache. It does not resolve
or revoke configured static credentials, perform a refresh, or apply Member
Cluster routing.
```

Add under `CHANGELOG.md` Unreleased `### Added`:

```markdown
- Revoke cached Access Tokens through the KubeSphere logout endpoint on a
  best-effort basis before clearing local login state.
```

- [ ] **Step 3: Verify documentation consistency**

Run `rg -n 'logout|local-only|revok' docs/cli.md docs/design.md CHANGELOG.md` and `git diff --check`.

Expected: no remaining statement says logout is local-only; `git diff --check` prints no output.

- [ ] **Step 4: Run repository verification**

Run `make verify`.

Expected: formatting, module checks, vet, normal tests, race tests, and both binary builds pass.

- [ ] **Step 5: Commit documentation**

```bash
git add docs/cli.md docs/design.md CHANGELOG.md
git commit -m "document server-side logout"
```
