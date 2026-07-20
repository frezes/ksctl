# Repository Guidelines

## Project Structure & Module Organization

`cmd/ksctl` and `cmd/kubectl-ks` are the two executable entry points. Shared CLI wiring lives in `pkg/cmd`; authentication, configuration, token caching, and API clients are separated under `pkg/auth`, `pkg/config`, `pkg/cache`, and `pkg/client`. Keep implementation details that are not reusable outside this module in `internal/` (currently `internal/securefile`). Tests sit beside the code they exercise as `*_test.go`. Design notes and implementation plans live in `docs/superpowers/`. The `staging/` tree contains pinned upstream KubeSphere and kubectl source snapshots; avoid broad edits there unless updating those dependencies intentionally.

## Build, Test, and Development Commands

- `make build` builds both binaries into `bin/` with the configured version metadata.
- `make test` runs all Go tests once, without cached results.
- `make fmt-check` reports tracked Go files that need `gofmt`.
- `make mod-check` checks that `go.mod`/`go.sum` are tidy and verifies downloaded modules.
- `make verify` mirrors CI: formatting, modules, `go vet`, normal and race tests, then a build.
- `./bin/ksctl version` is a quick smoke test after building.

Go 1.26 or later is required. Use `make clean` to remove generated binaries.

## Coding Style & Naming Conventions

Format Go code with `gofmt`; do not hand-align indentation. Follow standard Go naming: short lowercase package names, exported identifiers in `PascalCase`, unexported identifiers in `camelCase`, and filenames in lowercase (use underscores only when they improve clarity). Keep Cobra commands thin and move reusable behavior into focused packages. Wrap errors with actionable context and preserve underlying errors with `%w` where appropriate.

## Testing Guidelines

Use Go's `testing` package and place tests next to their subject. Name tests `TestFunction_Scenario` or use table-driven subtests with descriptive case names. Cover success paths and boundary/error behavior, especially credential resolution, filesystem permissions, configuration merging, and CLI output. Run focused tests while developing (for example, `go test ./pkg/config -run TestLoad`) and `make verify` before opening a pull request. There is no fixed coverage threshold; new behavior should have regression tests.

## Commit & Pull Request Guidelines

Recent commits use concise, imperative summaries such as `add interactive login flow` and `fix version output`. Keep each commit scoped to one logical change. Pull requests should explain the user-visible effect, summarize implementation choices, link relevant issues or design notes, and list verification performed. Include terminal output examples when CLI behavior changes; screenshots are only useful for rendered documentation. Ensure `make verify` passes and never commit tokens, passwords, generated `bin/` artifacts, or unredacted configuration.
