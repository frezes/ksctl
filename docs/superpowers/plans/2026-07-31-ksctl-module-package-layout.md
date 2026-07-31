# ksctl Module and Package Layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Change the module identity to `kubesphere.io/ksctl`, remove the top-level `internal` directory, and place KubeSphere extension behavior under `pkg/kubesphere` without changing runtime behavior.

**Architecture:** Reuse `kubesphere.io/client-go/rest.Interface` as the low-level KubeSphere REST abstraction. Keep REST construction, authenticated connection configuration, and TLS conversion in `pkg/client/kubesphere`; keep extension models and behavior in `pkg/kubesphere/extension`; keep atomic private-file writes in the domain-neutral `pkg/securefile` package.

**Tech Stack:** Go 1.26, Cobra, KubeSphere client-go REST, Kubernetes client-go, Go `testing`, Make.

## Global Constraints

- Go 1.26 or later is required.
- Preserve all CLI commands, flags, arguments, output, config formats, cache formats, filesystem paths, API requests, and authentication behavior.
- Reuse `kubesphere.io/client-go/rest.Interface`; do not add another generic REST transport interface.
- Do not add compatibility packages for `github.com/kubesphere/ksctl` or any old `internal` path.
- Do not rewrite historical design documents solely to replace old import paths.
- Do not make broad changes under `staging/`.
- Move production files and their colocated behavioral tests together.
- Use `gofmt` for every changed Go file.

---

### Task 1: Move secure-file persistence into `pkg/securefile`

**Files:**
- Move: `internal/securefile/securefile_test.go` → `pkg/securefile/securefile_test.go`
- Move: `internal/securefile/securefile.go` → `pkg/securefile/securefile.go`
- Modify: `pkg/config/loader.go:9`
- Modify: `pkg/cache/token/cache.go:13`

**Interfaces:**
- Consumes: Go filesystem APIs only.
- Produces: `func securefile.Write(path string, data []byte) error`.

- [ ] **Step 1: Move the behavioral test before the implementation**

Use an `apply_patch` move so the existing tests appear at the desired package
path while `Write` is still absent:

```text
*** Update File: internal/securefile/securefile_test.go
*** Move to: pkg/securefile/securefile_test.go
```

Keep all three existing contracts unchanged:

```go
func TestWriteCreatesFileWithPrivateMode(t *testing.T)
func TestWriteReplacesFileWithPrivateMode(t *testing.T)
func TestWriteCleansTemporaryFileAfterRenameFailure(t *testing.T)
```

- [ ] **Step 2: Run the moved test and verify the expected failure**

Run:

```bash
go test ./pkg/securefile -count=1
```

Expected: FAIL at compile time because `Write` is undefined in
`pkg/securefile`.

- [ ] **Step 3: Move the implementation and update both consumers**

Move the implementation without changing its behavior:

```text
*** Update File: internal/securefile/securefile.go
*** Move to: pkg/securefile/securefile.go
```

Update both imports to:

```go
import "github.com/kubesphere/ksctl/pkg/securefile"
```

Do not change directory mode `0700`, temporary file mode `0600`, the
write-sync-close-rename order, or cleanup behavior.

- [ ] **Step 4: Run focused persistence tests**

Run:

```bash
go test ./pkg/securefile ./pkg/config ./pkg/cache/token -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the secure-file move**

```bash
git add -A internal/securefile pkg/securefile pkg/config/loader.go pkg/cache/token/cache.go
git commit -m "move secure file persistence to pkg"
```

---

### Task 2: Consolidate KubeSphere TLS conversion into the client package

**Files:**
- Move and modify: `internal/kubesphererest/tls_test.go` → `pkg/client/kubesphere/tls_test.go`
- Move and modify: `internal/kubesphererest/tls.go` → `pkg/client/kubesphere/tls.go`
- Modify: `pkg/auth/oauth.go:13,81,124`
- Modify: `pkg/client/kubesphere/connection/getter.go:10,174`

**Interfaces:**
- Consumes: `config.TLSClientConfig` and
  `kubesphere.io/client-go/rest.TLSClientConfig`.
- Produces:
  `func kubesphere.TLSClientConfig(config.TLSClientConfig, bool) rest.TLSClientConfig`.

- [ ] **Step 1: Move and retarget the TLS tests before the implementation**

Move `tls_test.go` into `pkg/client/kubesphere` and change only its package
declaration:

```diff
-package kubesphererest
+package kubesphere
```

Retain both independent contracts:

```go
func TestTLSClientConfigMapsEveryField(t *testing.T)
func TestTLSClientConfigAppliesInsecureOverride(t *testing.T)
```

The first test must continue to prove that `NextProtos` does not alias the
source slice.

- [ ] **Step 2: Run the TLS tests and verify the expected failure**

Run:

```bash
go test ./pkg/client/kubesphere -run '^TestTLSClientConfig' -count=1
```

Expected: FAIL at compile time because `TLSClientConfig` is undefined in
package `kubesphere`.

- [ ] **Step 3: Move the TLS implementation into `pkg/client/kubesphere`**

Move `tls.go` and change its package declaration:

```diff
-package kubesphererest
+package kubesphere
```

Keep the exact exported signature:

```go
func TLSClientConfig(
	cfg config.TLSClientConfig,
	insecureOverride bool,
) kubesphererest.TLSClientConfig
```

Keep every field mapping and the cloned `NextProtos` slice unchanged.

- [ ] **Step 4: Point authentication and connection code at the client helper**

In `pkg/auth/oauth.go`, replace the internal adapter import with:

```go
clientkubesphere "github.com/kubesphere/ksctl/pkg/client/kubesphere"
```

Use:

```go
TLSClientConfig: clientkubesphere.TLSClientConfig(
	options.TLSClientConfig,
	options.InsecureSkipTLSVerify,
),
```

in both logout and token request configurations.

In `pkg/client/kubesphere/connection/getter.go`, import the parent client
package:

```go
clientkubesphere "github.com/kubesphere/ksctl/pkg/client/kubesphere"
```

Use:

```go
g.restConfig.TLSClientConfig = clientkubesphere.TLSClientConfig(
	g.resolved.TLSClientConfig,
	g.options.InsecureSkipTLSVerify,
)
```

- [ ] **Step 5: Format and run the affected tests**

Run:

```bash
gofmt -w pkg/client/kubesphere/tls.go pkg/client/kubesphere/tls_test.go pkg/auth/oauth.go pkg/client/kubesphere/connection/getter.go
go test ./pkg/client/kubesphere ./pkg/client/kubesphere/connection ./pkg/auth -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the TLS adapter move**

```bash
git add -A internal/kubesphererest pkg/client/kubesphere pkg/auth/oauth.go
git commit -m "move KubeSphere TLS mapping into client"
```

---

### Task 3: Move extension domain behavior into `pkg/kubesphere/extension`

**Files:**
- Move production:
  - `internal/extension/catalog.go`
  - `internal/extension/changes.go`
  - `internal/extension/dependencies.go`
  - `internal/extension/diagnose.go`
  - `internal/extension/lifecycle.go`
  - `internal/extension/raw.go`
  - `internal/extension/rest_client.go`
  - `internal/extension/service.go`
  - `internal/extension/types.go`
  - `internal/extension/wait.go`
- Move tests:
  - `internal/extension/catalog_test.go`
  - `internal/extension/changes_test.go`
  - `internal/extension/dependencies_test.go`
  - `internal/extension/diagnose_test.go`
  - `internal/extension/fake_client_test.go`
  - `internal/extension/lifecycle_test.go`
  - `internal/extension/raw_test.go`
  - `internal/extension/rest_client_test.go`
  - `internal/extension/wait_test.go`
- Modify imports and aliases:
  - `pkg/cmd/extension.go`
  - `pkg/cmd/extension/command.go`
  - `pkg/cmd/extension/diagnose.go`
  - `pkg/cmd/extension/diagnose_test.go`
  - `pkg/cmd/extension/fake_service_test.go`
  - `pkg/cmd/extension/input.go`
  - `pkg/cmd/extension/input_test.go`
  - `pkg/cmd/extension/mutation.go`
  - `pkg/cmd/extension/mutation_test.go`
  - `pkg/cmd/extension/output.go`
  - `pkg/cmd/extension/output_test.go`
  - `pkg/cmd/extension/query.go`
  - `pkg/cmd/extension/query_test.go`

**Interfaces:**
- Consumes: `kubesphere.io/client-go/rest.Interface`.
- Produces:
  - `type APIClient interface { ... }` with the existing extension API
    methods.
  - `func NewRESTClient(rest.Interface) APIClient`.
  - `func NewService(APIClient) *Service`.
  - All existing extension result, option, operation, and error types.

- [ ] **Step 1: Move the extension tests before production files**

Move every `internal/extension/*_test.go` listed above to the corresponding
path under `pkg/kubesphere/extension/`. Do not change package declarations or
behavioral assertions. The REST tests must continue to build real clients
through:

```go
rest, err := clientkubesphere.NewRESTClientFactory(nil).ForConfig(config)
```

- [ ] **Step 2: Run the moved suite and verify the expected failure**

Run:

```bash
go test ./pkg/kubesphere/extension -count=1
```

Expected: FAIL at compile time because production types such as `APIClient`,
`Service`, `Extension`, and `InstallPlan` do not yet exist at the new package
path.

- [ ] **Step 3: Move all extension production files**

Move every production file listed above from `internal/extension/` to
`pkg/kubesphere/extension/` with no package-name or behavior changes.

Preserve the boundary in `rest_client.go`:

```go
type restClient struct {
	client kubesphererest.Interface
}

func NewRESTClient(client kubesphererest.Interface) APIClient {
	return &restClient{client: client}
}
```

Do not introduce a ksctl-owned generic transport interface.

- [ ] **Step 4: Run the relocated extension suite**

Run:

```bash
go test ./pkg/kubesphere/extension -count=1
```

Expected: PASS.

- [ ] **Step 5: Update command packages to use the new domain path**

In every command file listed above, replace:

```go
internalextension "github.com/kubesphere/ksctl/internal/extension"
```

with:

```go
kubesphereextension "github.com/kubesphere/ksctl/pkg/kubesphere/extension"
```

Replace every `internalextension.` selector with
`kubesphereextension.`. Do not rename command-package service interfaces or
change their method signatures.

- [ ] **Step 6: Format and run extension command tests**

Run:

```bash
gofmt -w $(rg --files pkg/kubesphere/extension pkg/cmd/extension --glob '*.go') pkg/cmd/extension.go
go test ./pkg/kubesphere/extension ./pkg/cmd ./pkg/cmd/extension -count=1
```

Expected: PASS.

- [ ] **Step 7: Confirm that `internal` has no remaining files**

Run:

```bash
find internal -type f -print
```

Expected: no output. Remove the now-empty `internal/extension`,
`internal/kubesphererest`, `internal/securefile`, and `internal` directories.

- [ ] **Step 8: Commit the extension domain move**

```bash
git add -A internal pkg/kubesphere/extension pkg/cmd/extension.go pkg/cmd/extension
git commit -m "move KubeSphere extension domain to pkg"
```

---

### Task 4: Change the Go module identity and repository-owned imports

**Files:**
- Modify: `go.mod:1`
- Modify: `Makefile:5`
- Modify: every Go source or test file under `cmd/` and `pkg/` that imports
  `github.com/kubesphere/ksctl/...`
- Verify unchanged: historical files under `docs/superpowers/specs/`
- Verify unchanged: repository and release URLs in `README.md` and
  `CHANGELOG.md`

**Interfaces:**
- Consumes: package APIs produced by Tasks 1–3.
- Produces: module path `kubesphere.io/ksctl` and linker symbol
  `kubesphere.io/ksctl/pkg/cmd.version`.

- [ ] **Step 1: Change only the module declaration**

Apply:

```diff
-module github.com/kubesphere/ksctl
+module kubesphere.io/ksctl
```

- [ ] **Step 2: Verify that old repository-owned imports now fail**

Run:

```bash
go test ./cmd/... ./pkg/... -run '^$'
```

Expected: FAIL because source files still import packages under
`github.com/kubesphere/ksctl`.

- [ ] **Step 3: Rewrite Go imports and the linker symbol**

Perform the mechanical replacement only in the active source tree and build
configuration:

```bash
rg -l 'github\.com/kubesphere/ksctl' go.mod Makefile cmd pkg \
  | xargs perl -pi -e 's#github\.com/kubesphere/ksctl#kubesphere.io/ksctl#g'
```

The resulting Makefile line must be:

```make
LDFLAGS := -s -w -X kubesphere.io/ksctl/pkg/cmd.version=$(VERSION)
```

Do not replace GitHub release URLs or historical design text.

- [ ] **Step 4: Format, tidy, and compile every active package**

Run:

```bash
gofmt -w $(rg --files cmd pkg --glob '*.go')
go mod tidy
go test ./cmd/... ./pkg/... -run '^$'
```

Expected: PASS with no compile errors and no module changes beyond the
intended module identity and any normalization required by `go mod tidy`.

- [ ] **Step 5: Run all active package tests**

Run:

```bash
go test ./cmd/... ./pkg/... -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the module migration**

```bash
git add go.mod go.sum Makefile cmd pkg
git commit -m "change module path to kubesphere.io/ksctl"
```

---

### Task 5: Verify the complete repository refactor

**Files:**
- Verify: `go.mod`
- Verify: `Makefile`
- Verify: all active Go files under `cmd/` and `pkg/`
- Verify absent: `internal/`
- Modify only if verification reveals a regression in a file already in
  scope.

**Interfaces:**
- Consumes: the final repository layout and module path.
- Produces: verified CLI binaries with unchanged runtime behavior.

- [ ] **Step 1: Scan the active tree for stale paths**

Run:

```bash
test ! -d internal
test -z "$(rg -l 'github\.com/kubesphere/ksctl|/internal/' go.mod Makefile cmd pkg)"
```

Expected: both commands exit successfully with no output.

- [ ] **Step 2: Check formatting and patch hygiene**

Run:

```bash
make fmt-check
git diff --check
```

Expected: PASS with no output indicating formatting or whitespace errors.

- [ ] **Step 3: Run the CI-equivalent verification**

Run:

```bash
make verify
```

Expected: formatting, module checks, `go vet`, normal tests, race tests, and
the build all pass.

- [ ] **Step 4: Smoke-test the built executable**

Run:

```bash
./bin/ksctl version
```

Expected: exit status 0 and valid client version output.

- [ ] **Step 5: Review the final diff and commit any verification-only fixes**

Run:

```bash
git status --short
git diff --stat
git diff --check
```

Expected: no uncommitted changes. If an in-scope correction was required,
stage only that correction and commit it with a concise imperative message
that names the corrected package.
