# ksctl Module and Package Layout Refactor

Date: 2026-07-31

## Summary

Change the Go module path from `github.com/kubesphere/ksctl` to
`kubesphere.io/ksctl`, remove the repository's `internal` directory, and
organize KubeSphere-specific domain behavior under `pkg/kubesphere`.

The refactor preserves CLI behavior and existing public data formats. It
reuses `kubesphere.io/client-go/rest.Interface` as the generic KubeSphere REST
client abstraction instead of introducing a duplicate transport interface.

## Goals

- Declare the module as `kubesphere.io/ksctl`.
- Update every repository-owned Go import and linker symbol path to the new
  module path.
- Remove the top-level `internal` directory.
- Place KubeSphere extension domain behavior in
  `pkg/kubesphere/extension`.
- Keep KubeSphere REST client construction and connection handling in
  `pkg/client/kubesphere`.
- Move generic secure-file persistence to `pkg/securefile`.
- Preserve existing command behavior, configuration formats, cache formats,
  filesystem paths, API requests, and user-facing output.

## Non-Goals

- No KubeSphere API behavior changes.
- No CLI command, flag, argument, or output changes.
- No configuration or token-cache format migration.
- No compatibility packages at the old `github.com/kubesphere/ksctl` import
  paths.
- No compatibility packages under `internal`.
- No broad changes to the pinned `staging` source tree.
- No new generic REST client abstraction on top of
  `kubesphere.io/client-go/rest.Interface`.

## Target Repository Structure

```text
cmd/
  ksctl/
  unictl-ks/

pkg/
  auth/
  cache/
    token/
  client/
    kubernetes/
    kubesphere/
      client.go
      client_test.go
      tls.go
      tls_test.go
      connection/
  cmd/
  config/
  kubesphere/
    extension/
  securefile/
```

The top-level `internal/` directory no longer exists after the migration.

## Package Responsibilities

### `pkg/client/kubesphere`

This package remains the low-level KubeSphere client boundary. It owns:

- construction of `kubesphere.io/client-go/rest.Interface` values;
- optional injected HTTP client handling;
- KubeSphere REST transport wrapping;
- conversion from `pkg/config.TLSClientConfig` to the KubeSphere client-go
  TLS configuration;
- the existing `connection` subpackage that resolves authenticated
  KubeSphere REST configurations.

The TLS conversion currently in `internal/kubesphererest` moves into this
package. Consumers call the exported conversion helper rather than importing
a separate adapter package.

The package does not own extension models, extension lifecycle rules, or
extension API paths.

### `pkg/kubesphere/extension`

The complete contents of `internal/extension` move to this package. It owns:

- extension, extension-version, install-plan, Fleet cluster, namespace, job,
  and pod response models used by extension management;
- extension catalog, dependency, lifecycle, diagnosis, wait, and raw-config
  behavior;
- the narrow domain-level `APIClient` consumed by `Service`;
- the REST adapter that implements `APIClient`.

The REST adapter accepts `kubesphere.io/client-go/rest.Interface`. That
existing upstream interface already supplies the generic verbs and request
builder used by extension management, including `Get`, `Post`, `Patch`,
`Delete`, `AbsPath`, `Param`, `SetHeader`, `Body`, `Do`, `Raw`, and `Error`.
No ksctl-owned generic transport interface is added.

Keeping the narrow `APIClient` inside the extension package isolates service
tests from HTTP request construction. Keeping the REST adapter in the same
domain package allows it to use extension models without creating a
dependency from `pkg/client/kubesphere` back to a higher-level domain.

### `pkg/securefile`

The complete contents of `internal/securefile` move to this package. It
continues to own atomic private-file writes used by configuration and token
cache persistence.

The package remains domain-neutral and has no dependency on KubeSphere,
authentication, configuration models, or cache models.

## Dependency Direction

The relevant dependency direction is:

```text
pkg/cmd
  |-- pkg/kubesphere/extension
  |       `-- kubesphere.io/client-go/rest.Interface
  |-- pkg/client/kubesphere
  |       |-- pkg/config
  |       `-- kubesphere.io/client-go/rest
  |-- pkg/auth
  |       `-- pkg/client/kubesphere
  |-- pkg/cache/token
  `-- pkg/config

pkg/client/kubesphere/connection
  |-- pkg/auth
  `-- pkg/client/kubesphere

pkg/cache/token --> pkg/securefile
pkg/config      --> pkg/securefile
```

`pkg/client/kubesphere` does not import `pkg/kubesphere/extension`.
`pkg/kubesphere/extension` does not own client configuration or credential
resolution. This keeps the graph acyclic.

## Module-Path Migration

The first line of `go.mod` becomes:

```go
module kubesphere.io/ksctl
```

All Go source and test imports beginning with
`github.com/kubesphere/ksctl/` change to `kubesphere.io/ksctl/`.

The Makefile version linker flag changes from:

```text
-X github.com/kubesphere/ksctl/pkg/cmd.version=$(VERSION)
```

to:

```text
-X kubesphere.io/ksctl/pkg/cmd.version=$(VERSION)
```

Repository and release URLs such as `https://github.com/frezes/ksctl` are not
Go import paths and remain unchanged.

Historical design documents remain historical records and are not rewritten
solely to replace old package paths. The new design and implementation plan
describe the current layout.

## Migration Mapping

```text
internal/extension
  -> pkg/kubesphere/extension

internal/kubesphererest/tls.go
  -> pkg/client/kubesphere/tls.go

internal/securefile
  -> pkg/securefile
```

The production files and their colocated tests move together. Package names
remain `extension`, `kubesphere`, and `securefile` respectively.

Existing imports of `internal/extension` in `pkg/cmd` and
`pkg/cmd/extension` change to `kubesphere.io/ksctl/pkg/kubesphere/extension`.
Existing imports of `internal/securefile` in `pkg/config` and
`pkg/cache/token` change to `kubesphere.io/ksctl/pkg/securefile`.

Callers in `pkg/auth` and `pkg/client/kubesphere/connection` use the TLS
conversion exported by `pkg/client/kubesphere`.

## Behavior and Compatibility

This is a source-layout and module-identity change. Runtime behavior remains
unchanged:

- command names and flags are unchanged;
- extension API paths, selectors, request bodies, polling, and diagnostics
  are unchanged;
- authentication and token precedence are unchanged;
- TLS mapping and insecure-override behavior are unchanged;
- secure writes retain directory mode `0700` and file mode `0600`;
- config and token cache locations and serialization are unchanged.

The old Go import paths intentionally stop compiling. The module has not
published a compatibility promise for those paths, and retaining aliases
would conflict with removing `internal`.

## Testing and Verification

The migration relies primarily on existing behavioral tests, moved with their
packages. Because the requested change is structural, tests should prove that
behavior is preserved rather than assert source text or directory names.

Verification proceeds in increasing scope:

1. Run the moved `pkg/securefile` tests.
2. Run the `pkg/client/kubesphere` and
   `pkg/client/kubesphere/connection` tests, including every-field TLS
   conversion and insecure-override coverage.
3. Run the moved `pkg/kubesphere/extension` tests.
4. Run affected command, authentication, configuration, cache, and client
   package tests.
5. Run `go mod tidy` and confirm that `go.mod` and `go.sum` are consistent.
6. Run `make verify`.
7. Build the binaries and run `./bin/ksctl version` as a smoke test.

The final repository scan must find no production or test import beginning
with `github.com/kubesphere/ksctl`, no import containing `/internal/`, and no
top-level `internal` directory.

## Success Criteria

- `go.mod` declares `module kubesphere.io/ksctl`.
- All repository-owned Go imports use `kubesphere.io/ksctl`.
- The Makefile linker symbol uses `kubesphere.io/ksctl/pkg/cmd.version`.
- `internal/` is absent.
- Extension behavior is provided by `pkg/kubesphere/extension`.
- KubeSphere REST client construction and TLS conversion are provided by
  `pkg/client/kubesphere`.
- Secure-file writes are provided by `pkg/securefile`.
- `pkg/kubesphere/extension` reuses
  `kubesphere.io/client-go/rest.Interface`.
- `make verify` and the version smoke test pass.
