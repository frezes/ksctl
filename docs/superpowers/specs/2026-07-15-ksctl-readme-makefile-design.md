# ksctl README and Makefile Design

## Goal

Provide concise, user-facing English and Chinese documentation for `ksctl`, and
add a minimal Makefile for the repository's current development workflow.

## Scope

This change updates `README.md`, adds `README_zh.md`, and adds `Makefile`.
It does not change command behavior, configuration formats, authentication,
release packaging, installation, linting, or CI.

The Makefile builds only the `ksctl` executable. The existing `kubectl-ks`
entrypoint remains outside the current build target.

## Documentation Structure

`README.md` is the English document and `README_zh.md` is its Chinese
counterpart. Each document links to the other at the top. Their section order,
commands, configuration examples, and behavioral claims remain synchronized.

The documents use the Kubernetes kubectl reference as an organizational model,
adapted to the smaller command surface currently implemented by `ksctl`:

1. Project overview and supported scope
2. Prerequisites and source build
3. Quick start
4. Command syntax
5. Supported commands
6. Scope and connection flags
7. Output formats and selectors
8. Common operation examples
9. Configuration and authentication
10. Development targets

The README files document only implemented commands: `get`, `describe`,
`auth`, `config`, and `version`. Resource examples cover both Kubernetes and
KubeSphere resources. The text explains that resource discovery, printing,
selectors, watching, and describe behavior are provided by the pinned kubectl
implementation without implying support for unrelated kubectl commands.

## Makefile

The Makefile exposes exactly three public targets:

- `build`: create `bin/ksctl` from `./cmd/ksctl`
- `test`: run all Go tests with `go test ./...`
- `clean`: remove `bin/ksctl`

`build` is the default target. The output directory is created when needed.
The Makefile does not install binaries, build `kubectl-ks`, inject release
metadata, run linters, download tools, or provide cross-compilation targets.

## Validation

Validation covers:

- `make clean`
- `make build`
- `./bin/ksctl version`
- `make test`
- confirmation that the English and Chinese README files use matching section
  structures and executable command examples
- `git diff --check`

No live KubeSphere endpoint is required because the change does not alter
runtime behavior and README examples use placeholders.
