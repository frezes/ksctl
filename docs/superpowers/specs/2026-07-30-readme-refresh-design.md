# README Refresh Design

## Goal

Refresh the English `README.md` so it accurately presents the current `ksctl`
command surface and works as a concise project landing page. Detailed command
reference material remains in the existing CLI guides.

## Scope

This change updates only `README.md`. It does not change CLI behavior, release
packaging, build targets, configuration formats, or the detailed documentation
under `docs/`.

The README remains English-only. It continues to link to both the English and
Simplified Chinese CLI and design documents.

## Audience and Positioning

The primary audience is a KubeSphere user or contributor encountering the
repository for the first time. The opening describes `ksctl` as a CLI for
KubeSphere 4.x and the Kubernetes resources exposed through KubeSphere.

The README emphasizes discovery, inspection, diagnostics, authentication,
context selection, and extension lifecycle management. It explicitly states
that the built-in Kubernetes-style resource commands are read-only so the
documentation does not imply unsupported mutation commands.

## Information Architecture

The refreshed README uses this order:

1. Project introduction and scope
2. Highlights grouped by user task
3. Release installation and source build
4. A short, connected quick-start workflow
5. A compact top-level command overview
6. Scope and connection guidance
7. Links to CLI and design documentation
8. Development commands

The command overview includes every current top-level command group:

- `auth`
- `config`
- `get`
- `describe`
- `logs`
- `top`
- `tenant`
- `extension`
- `api`
- `plugin`
- `version`

The README does not duplicate complete flag lists, environment-variable
precedence, troubleshooting material, or the extension lifecycle reference.
Those details remain in `docs/cli.md` and `docs/cli_zh.md`.

## Examples

The quick start demonstrates a coherent path:

1. log in;
2. inspect the active identity and Context;
3. inspect Kubernetes resources;
4. inspect tenant resources;
5. inspect or diagnose an Extension;
6. make a raw KubeSphere API request.

Examples use only commands and flags implemented by the current command tree.
They avoid requiring a specific local resource name when a discovery command
can demonstrate the feature. Release installation retains checksum
verification for both macOS and Linux.

## Accuracy Constraints

- Go requirements match `go.mod`.
- Development targets and their descriptions match `Makefile`.
- Release examples use the repository's existing archive naming convention.
- No badges, package-manager instructions, compatibility promises, or feature
  claims are added without a source in the repository.
- The release-only alternate entry point remains undocumented, in accordance
  with its existing design.

## Validation

Validation consists of:

1. comparing documented top-level commands with generated `ksctl --help`;
2. checking example flags against command help or existing CLI guides;
3. checking all relative documentation links resolve;
4. running `git diff --check`;
5. reviewing the final diff for duplication, unsupported claims, and accidental
   changes outside `README.md`.

No live KubeSphere endpoint is required because the change is documentation
only.
