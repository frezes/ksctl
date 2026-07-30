# Release-only unictl Entry Point Design

## Goal

Keep `ksctl` as the only entry point presented in current documentation,
normal development builds, and product-specific tests. Publish `unictl-ks` as
an additional release artifact whose user-facing command name is `unictl ks`.

## Scope

This change updates:

- the renamed executable entry point under `cmd/unictl-ks`;
- shared root-command construction in `pkg/cmd`;
- the Makefile's normal build, verification, and cleanup behavior;
- GoReleaser's release builds and archives;
- current user and architecture documentation;
- active tests that currently refer to the old companion entry point.

Historical specifications and plans under `docs/superpowers/` remain unchanged.
They continue to record the decisions that were current when they were written.

## Command architecture

`cmd/ksctl` remains the primary executable and continues to use the existing
standalone root-command constructor.

The release-only companion executable uses a generic alternate-entry-point
constructor from `pkg/cmd`. The constructor accepts a process name and a Cobra
display name instead of embedding a product-specific name in the shared
package. `cmd/unictl-ks` passes `unictl-ks` as the process name and `unictl ks`
as the display name.

Both entry points continue to share command registration, connection handling,
plugin dispatch, streams, and version information. The alternate display name
propagates through nested command help and examples.

The old product-specific companion constructors are removed. This prevents
stale names from remaining in active source APIs.

## Development and release behavior

`make build` builds only `bin/ksctl`. Because `make verify` delegates its final
build step to `make build`, normal CI verification also explicitly builds only
`ksctl`. `make clean` removes only the normal development artifact.

GoReleaser remains responsible for release-only compilation. Its configuration
defines two builds and two archives:

- the existing `ksctl` binary and archive; and
- the `unictl-ks` binary and archive built from `cmd/unictl-ks`.

The tag-triggered release workflow continues to run normal verification before
GoReleaser publishes both artifact sets.

## Documentation

Current documentation describes only `ksctl`. References to the previous
companion entry point are removed from:

- `README.md`;
- the English and Chinese CLI guides;
- `docs/design.md`; and
- `AGENTS.md`.

The release-only companion artifact is intentionally not documented in these
files. Historical specifications and plans are not rewritten.

## Testing

Active tests do not use either the old or new companion product name.

Tests that need to prove alternate-entry-point behavior use neutral fixture
names through the generic constructor. They continue to cover:

- recursive Cobra display-name propagation;
- shared command registration;
- entry-point-independent help generation; and
- plugin dispatch through an alternate entry point.

Tests that only duplicate standalone command registration are reduced to the
`ksctl` path. Release configuration is checked separately with GoReleaser.

## Verification

Implementation is complete when:

1. searches outside `docs/superpowers/` find no old companion name in current
   documentation, active tests, build configuration, or active source APIs;
2. the new name appears only where the release-only entry point requires it,
   including `cmd/unictl-ks` and `.goreleaser.yaml`;
3. focused alternate-entry-point tests pass with neutral fixture names;
4. `make verify` succeeds and produces only the normal `ksctl` development
   binary;
5. `goreleaser check` accepts the configuration; and
6. a GoReleaser snapshot build produces archives for both `ksctl` and
   `unictl-ks`.

## Non-goals

- Documenting installation or use of the release-only companion executable.
- Rewriting historical specifications or plans.
- Changing the `ksctl-*` executable plugin discovery convention.
- Changing the command set, API behavior, authentication, or configuration
  model shared by the two release entry points.
