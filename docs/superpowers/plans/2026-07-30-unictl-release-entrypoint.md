# Release-only unictl Entry Point Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep `ksctl` as the only documented and normally built executable while publishing `unictl-ks` as a second release artifact that displays `unictl ks`.

**Architecture:** `pkg/cmd` will expose one name-neutral constructor for alternate executable entry points, while `cmd/unictl-ks` supplies its release-specific process and display names. Normal Make targets will build only `ksctl`; GoReleaser will own compilation and packaging of both release binaries. Active tests will exercise the generic alternate-entry-point behavior with fixture names.

**Tech Stack:** Go 1.26, Cobra, GNU Make, GoReleaser v2.17.0, GitHub Actions, Markdown.

## Global Constraints

- `make build`, `make verify`, and `make clean` explicitly handle only `ksctl`.
- A tagged release publishes both `ksctl` and `unictl-ks`.
- The release-only executable displays `unictl ks` in help, examples, and errors.
- Current documentation and active tests mention neither the old nor the new companion product name.
- Historical specifications and plans under `docs/superpowers/` remain unchanged.
- Preserve the existing `ksctl-*` executable plugin discovery convention.
- Preserve the user's current deletion of `cmd/kubectl-ks/main.go` and addition of `cmd/unictl-ks/main.go`.

## File map

- `pkg/cmd/plugin_dispatch.go`: provide the generic alternate-entry-point constructor and retain shared plugin dispatch.
- `pkg/cmd/root.go`: remove the obsolete product-specific no-argument constructor.
- `cmd/unictl-ks/main.go`: configure the release-only process and display names.
- `pkg/cmd/root_test.go`: cover display-name propagation with neutral fixture names and remove duplicate product-specific registration checks.
- `pkg/cmd/plugin_dispatch_test.go`: cover alternate-entry-point plugin execution with neutral fixture names.
- `pkg/cmd/extension_integration_test.go`: retain one alternate-entry-point integration path without a product name.
- `pkg/cmd/extension/command_test.go`: verify parent display-name propagation with a neutral fixture.
- `Makefile`: build and clean only the normal `ksctl` artifact.
- `.goreleaser.yaml`: build and archive `ksctl` plus the renamed release-only executable.
- `README.md`: document only `ksctl` installation, build, and development behavior.
- `docs/cli.md`: remove the secondary entry point from the current English guide.
- `docs/cli_zh.md`: remove the secondary entry point from the current Chinese guide.
- `docs/design.md`: describe the current `ksctl` command architecture and validation boundary without the release-only product name.
- `AGENTS.md`: align repository structure and build guidance with the normal `ksctl` workflow.

---

### Task 1: Introduce a generic alternate entry point

**Files:**

- Delete: `cmd/kubectl-ks/main.go`
- Modify: `cmd/unictl-ks/main.go`
- Modify: `pkg/cmd/root.go:33-41`
- Modify: `pkg/cmd/plugin_dispatch.go:34-56`
- Test: `pkg/cmd/root_test.go`
- Test: `pkg/cmd/plugin_dispatch_test.go`
- Test: `pkg/cmd/extension_integration_test.go`
- Test: `pkg/cmd/extension/command_test.go`

**Interfaces:**

- Consumes: `newRootCommandWithArgs(use, displayName string, streams IOStreams, info VersionInfo, arguments []string, handler pluginHandler) (*cobra.Command, error)`.
- Produces: `NewEntrypointCommandWithArgs(use, displayName string, streams IOStreams, info VersionInfo, arguments []string) (*cobra.Command, error)`.
- Produces: `cmd/unictl-ks` passes process name `unictl-ks` and display name `unictl ks` to the generic constructor.

- [ ] **Step 1: Rewrite the alternate-entry-point tests before production code**

In `pkg/cmd/root_test.go`, add a test helper that calls the not-yet-defined
generic constructor with fixture names:

```go
const (
	testEntrypointUse         = "fixture-entrypoint"
	testEntrypointDisplayName = "fixture entrypoint"
)

func newTestEntrypointCommand(
	t *testing.T,
	streams IOStreams,
	info VersionInfo,
) *cobra.Command {
	t.Helper()
	command, err := NewEntrypointCommandWithArgs(
		testEntrypointUse,
		testEntrypointDisplayName,
		streams,
		info,
		[]string{testEntrypointUse},
	)
	if err != nil {
		t.Fatalf("NewEntrypointCommandWithArgs() error = %v", err)
	}
	return command
}
```

Rename the three recursive help tests to use `newTestEntrypointCommand` and
assert literal fixture output. For example, the `get` test must include:

```go
if !strings.Contains(help, "Usage:\n  fixture entrypoint get") {
	t.Fatalf("entrypoint help = %q", help)
}
if !strings.Contains(help, "fixture entrypoint get pods") {
	t.Fatalf("entrypoint examples = %q", help)
}
```

Keep the standalone and alternate rows only where display-name propagation is
the behavior under test:

```go
{
	name: "alternate entrypoint",
	root: newTestEntrypointCommand(
		t,
		IOStreams{},
		VersionInfo{Version: "dev"},
	),
	want: "fixture entrypoint extension install",
},
```

For command-registration tests, remove the duplicated alternate row and assert
the shared command tree through `NewRootCommand` once.

In `pkg/cmd/plugin_dispatch_test.go`, rename
`TestDefaultPluginHandlerExecutesForBothEntrypoints` to
`TestDefaultPluginHandlerExecutesForRootAndAlternateEntrypoints`. Use
`"alternate"` as the helper-process selector and construct that branch with:

```go
root, err = NewEntrypointCommandWithArgs(
	"fixture-entrypoint",
	"fixture entrypoint",
	streams,
	VersionInfo{Version: "test"},
	arguments,
)
```

In `pkg/cmd/extension_integration_test.go`, rename the boolean parameter
`kubectlPlugin` to `alternateEntrypoint`, rename the product-specific subtest
to `"alternate entrypoint status uses host"`, and use the same fixture names
when the boolean is true.

In `pkg/cmd/extension/command_test.go`, change the parent display-name fixtures
to:

```go
for _, parent := range []string{"ksctl", "fixture entrypoint"} {
```

The production mutation these tests catch is ignoring the supplied display
name or failing to wire plugin dispatch for an alternate executable. The
expected strings are literals derived independently from the implementation.

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
go test ./pkg/cmd ./pkg/cmd/extension -run 'Test(Entrypoint|DefaultPluginHandler|NewCommandHelp|ExtensionHelp|PluginHelp)' -count=1
```

Expected: compilation fails because `NewEntrypointCommandWithArgs` is
undefined. This proves the tests require the new generic public boundary.

- [ ] **Step 3: Add the generic constructor**

Remove `NewKubectlPluginCommand` from `pkg/cmd/root.go`.

Replace `NewKubectlPluginCommandWithArgs` in `pkg/cmd/plugin_dispatch.go` with:

```go
func NewEntrypointCommandWithArgs(
	use, displayName string,
	streams IOStreams,
	info VersionInfo,
	arguments []string,
) (*cobra.Command, error) {
	return newRootCommandWithArgs(
		use,
		displayName,
		streams,
		info,
		arguments,
		newDefaultPluginHandler(pluginFilenamePrefixes),
	)
}
```

This function validates no new policy. It supplies the shared production
boundary used by the release-only `main` package and by behavior tests.

- [ ] **Step 4: Wire the renamed executable**

Change `cmd/unictl-ks/main.go` to call:

```go
cmd, err := kscmd.NewEntrypointCommandWithArgs(
	"unictl-ks",
	"unictl ks",
	kscmd.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr},
	kscmd.DefaultVersionInfo(),
	os.Args,
)
```

Do not add release-specific command logic to this file.

- [ ] **Step 5: Format and verify GREEN**

Run:

```bash
gofmt -w cmd/unictl-ks/main.go pkg/cmd/root.go pkg/cmd/plugin_dispatch.go pkg/cmd/root_test.go pkg/cmd/plugin_dispatch_test.go pkg/cmd/extension_integration_test.go pkg/cmd/extension/command_test.go
go test ./pkg/cmd ./pkg/cmd/extension -count=1
go test ./cmd/unictl-ks -count=1
```

Expected: all packages pass. Mentally mutating `displayName` to `use` must
break the recursive help tests, and bypassing `newRootCommandWithArgs` must
break the alternate plugin-dispatch test.

- [ ] **Step 6: Confirm active tests contain no companion product name**

Run:

```bash
rg -n 'kubectl-ks|kubectl ks|unictl-ks|unictl ks' --glob '*_test.go' --glob '!staging/**' .
```

Expected: no output.

- [ ] **Step 7: Commit the entry-point behavior**

```bash
git add cmd/kubectl-ks/main.go cmd/unictl-ks/main.go pkg/cmd/root.go pkg/cmd/plugin_dispatch.go pkg/cmd/root_test.go pkg/cmd/plugin_dispatch_test.go pkg/cmd/extension_integration_test.go pkg/cmd/extension/command_test.go
git commit -m "rename release companion entrypoint"
```

---

### Task 2: Separate normal builds from release packaging

**Files:**

- Modify: `Makefile:7-10`
- Modify: `Makefile:23-30`
- Modify: `.goreleaser.yaml:16-35`

**Interfaces:**

- Consumes: `cmd/ksctl` and the `cmd/unictl-ks` entry point from Task 1.
- Produces: `make build` creates only `bin/ksctl`.
- Produces: GoReleaser build IDs and archive IDs named `ksctl` and `unictl-ks`.

- [ ] **Step 1: Verify the current normal build is RED**

Run:

```bash
make clean
make build
```

Expected: `make build` fails while attempting to compile the removed
`./cmd/kubectl-ks` path. This is the incorrect normal-build dependency being
removed.

- [ ] **Step 2: Limit normal build and cleanup to ksctl**

Change the Makefile targets to:

```make
build:
	@mkdir -p bin
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/ksctl ./cmd/ksctl

clean:
	rm -f bin/ksctl
```

Keep `verify` delegating to `$(MAKE) build`; do not add a second development or
CI build target.

- [ ] **Step 3: Verify the normal build boundary is GREEN**

Run:

```bash
make clean
make build
test -x bin/ksctl
test ! -e bin/unictl-ks
test ! -e bin/kubectl-ks
./bin/ksctl version
```

Expected: every command exits zero; version output begins with
`ksctl Version: dev`.

- [ ] **Step 4: Rename the release build and archive**

Replace the second GoReleaser build with:

```yaml
  - id: unictl-ks
    main: ./cmd/unictl-ks
    binary: unictl-ks
    env:
      - CGO_ENABLED=0
    goos: [linux, darwin]
    goarch: [amd64, arm64]
    flags: [-trimpath]
    ldflags:
      - -s -w -X github.com/kubesphere/ksctl/pkg/cmd.version=v{{ .Version }}
```

Replace the second archive with:

```yaml
  - id: unictl-ks
    ids: [unictl-ks]
    formats: [tar.gz]
    name_template: unictl-ks_{{ .Version }}_{{ .Os }}_{{ .Arch }}
```

- [ ] **Step 5: Validate and smoke-test the release-only executable**

Run:

```bash
go run github.com/goreleaser/goreleaser/v2@v2.17.0 check
go run github.com/goreleaser/goreleaser/v2@v2.17.0 build --snapshot --clean --single-target --id unictl-ks
find dist -type f -name unictl-ks -perm -111 -exec {} get --help \; | rg -F 'unictl ks get'
```

Expected: configuration validation succeeds, the host-target release binary
builds, and its help includes `unictl ks get`.

- [ ] **Step 6: Build the complete release snapshot**

Run:

```bash
go run github.com/goreleaser/goreleaser/v2@v2.17.0 release --snapshot --clean
find dist -type f -name 'ksctl_*.tar.gz' | sort
find dist -type f -name 'unictl-ks_*.tar.gz' | sort
```

Expected: each `find` prints four archives covering Linux and macOS on amd64
and arm64. Snapshot mode does not publish a GitHub release.

- [ ] **Step 7: Commit build and release configuration**

```bash
git add Makefile .goreleaser.yaml
git commit -m "build companion binary only for releases"
```

---

### Task 3: Make current documentation ksctl-only

**Files:**

- Modify: `README.md:3-6`
- Modify: `README.md:21-49`
- Modify: `README.md:78-91`
- Modify: `docs/cli.md:5-30`
- Modify: `docs/cli_zh.md:5-25`
- Modify: `docs/design.md:8-22`
- Modify: `docs/design.md:41-68`
- Modify: `docs/design.md:92-93`
- Modify: `docs/design.md:518-532`
- Modify: `AGENTS.md:3-16`

**Interfaces:**

- Consumes: the normal-build and release boundaries established by Tasks 1–2.
- Produces: current user, contributor, and architecture documentation that
  describes only the supported `ksctl` development workflow.

- [ ] **Step 1: Update the README**

Make the introduction describe `ksctl` without a second invocation form:

```markdown
`ksctl` is a command-line client for inspecting KubeSphere 4.x resources and
the Kubernetes resources exposed through KubeSphere.
```

Describe only the `ksctl_VERSION_OS_ARCH.tar.gz` release archive and remove the
secondary installation paragraph. Change the source-build sentence to:

```markdown
Go 1.26 or later is required. Build `ksctl` into `bin/`:
```

Update the Development bullets so `build` creates `bin/ksctl`, `verify` ends
with the `ksctl` build, and `clean` removes that generated binary.

- [ ] **Step 2: Update both CLI guides**

Remove the second-entry-point examples and replacement guidance from
`docs/cli.md` and `docs/cli_zh.md`.

The English prerequisite becomes:

```markdown
- The `ksctl` executable.
```

The Chinese prerequisite becomes:

```markdown
- `ksctl` 可执行文件。
```

Do not change the command descriptions or kubectl-compatibility explanations.

- [ ] **Step 3: Update the current architecture document**

Remove the goal that promises two named entry points. Replace the command
architecture opening with:

````markdown
The executable entry point is intentionally small:

```text
cmd/ksctl/main.go -> NewRootCommandWithArgs
```

The constructor delegates to the shared root-command builder in `pkg/cmd`.
````

Describe recursive example normalization as using the active command display
name. Change the validation boundary to say plugin tests cover longest
matching, argument forwarding, dash conversion, built-in protection, and PATH
diagnostics, and that the normal build compiles `cmd/ksctl`.

- [ ] **Step 4: Update contributor instructions**

Change `AGENTS.md` to identify `cmd/ksctl` as the normal executable entry
point and `pkg/cmd` as shared CLI wiring. Change the build command description
to:

```markdown
- `make build` builds `ksctl` into `bin/` with the configured version metadata.
```

Leave test, formatting, module, verification, and release-safety guidance
unchanged.

- [ ] **Step 5: Check the documentation boundary**

Run:

```bash
rg -n 'kubectl-ks|kubectl ks|unictl-ks|unictl ks' README.md AGENTS.md docs/cli.md docs/cli_zh.md docs/design.md
```

Expected: no output. Human-facing prose is verified by review and this scope
check; it does not receive source-text unit tests.

- [ ] **Step 6: Review the documentation diff**

Run:

```bash
git diff --check
git diff -- README.md AGENTS.md docs/cli.md docs/cli_zh.md docs/design.md
```

Expected: no whitespace errors; the diff removes only the secondary
entry-point claims and accurately describes the `ksctl` workflow.

- [ ] **Step 7: Commit current documentation**

```bash
git add README.md AGENTS.md docs/cli.md docs/cli_zh.md docs/design.md
git commit -m "document ksctl as the primary entrypoint"
```

---

## Final verification

- [ ] **Step 1: Verify naming boundaries**

Run:

```bash
rg -n 'kubectl-ks|kubectl ks' --glob '!docs/superpowers/**' --glob '!staging/**' .
rg -n 'unictl-ks|unictl ks' --glob '!docs/superpowers/**' --glob '!staging/**' .
rg -n 'kubectl-ks|kubectl ks|unictl-ks|unictl ks' --glob '*_test.go' --glob '!staging/**' .
```

Expected: the old-name and active-test searches produce no output. The new-name
search reports only release wiring in `cmd/unictl-ks/main.go` and
`.goreleaser.yaml`.

- [ ] **Step 2: Run repository verification**

Run:

```bash
make verify
```

Expected: formatting, module checks, `go vet`, normal tests, race tests, and the
single normal `ksctl` build all pass.

- [ ] **Step 3: Revalidate release packaging**

Run:

```bash
go run github.com/goreleaser/goreleaser/v2@v2.17.0 check
go run github.com/goreleaser/goreleaser/v2@v2.17.0 release --snapshot --clean
find dist -type f -name 'ksctl_*.tar.gz' | sort
find dist -type f -name 'unictl-ks_*.tar.gz' | sort
```

Expected: GoReleaser succeeds and both archive searches list four platform
archives.

- [ ] **Step 4: Confirm repository state**

Run:

```bash
git status --short
git log -4 --oneline
```

Expected: no uncommitted source changes remain. The recent history contains
separate entry-point behavior, build/release configuration, and documentation
commits after the design and plan commits.
