# ksctl Documentation Design

## Goal

Make the repository documentation easier to enter and maintain by keeping the
English README concise, adding a complete CLI user guide, and adding a single
current architecture design document.

## Scope

This change:

- simplifies `README.md`;
- adds `docs/cli.md` for CLI users;
- adds `docs/design.md` for contributors and maintainers; and
- removes `README_zh.md` so the maintained project documentation is English
  only.

It does not change CLI behavior, Go code, dependencies, the Makefile, release
packaging, or the historical specifications and plans under
`docs/superpowers/`.

## Documentation Model

The documentation has three maintained entry points with distinct audiences:

| Document | Audience | Responsibility |
| --- | --- | --- |
| `README.md` | New users and contributors | Explain what ksctl is, how to install it, the shortest working example, where to find detailed documentation, and how to build and verify the repository. |
| `docs/cli.md` | CLI users and automation authors | Explain how to use every current command, flag group, configuration mechanism, authentication path, resource workflow, kubeconfig operation, and executable plugin feature. |
| `docs/design.md` | Contributors and maintainers | Explain the current goals, boundaries, architecture, configuration and authentication models, client pipeline, routing, plugin model, security properties, and compatibility constraints. |

The README links to both documents. The CLI guide may link to the design when
implementation context is useful, but the design does not serve as a usage
manual. Historical files under `docs/superpowers/` remain records of individual
decisions and implementation phases; `docs/design.md` is the authoritative
description of the current system.

## README Structure

`README.md` contains only:

1. the project name, purpose, and core capabilities;
2. release installation instructions for the standalone binary and kubectl
   plugin;
3. source build instructions;
4. a minimal quick start that logs in, queries one KubeSphere resource, and
   queries one Kubernetes resource;
5. links to the CLI guide and design document; and
6. the main development and verification commands.

The README does not contain a full command table, configuration YAML, detailed
credential precedence, a complete plugin specification, exhaustive resource
examples, or detailed security behavior. Those details move to `docs/cli.md`.
The language switcher and references to `README_zh.md` are removed.

## CLI Guide Structure

`docs/cli.md` is an English user guide organized around user workflows:

1. **Overview** — supported KubeSphere version, read-only resource scope, and
   the equivalent `ksctl` and `kubectl ks` entrypoints.
2. **Prerequisites and syntax** — required access and credentials, resource
   command grammar, and built-in help.
3. **Command reference** — one concise index of every current built-in command.
4. **Authentication** — interactive login, non-interactive login, derived Fleet
   and Context names, token caching, and logout behavior.
5. **Scope and connection selection** — the relationship among Fleet, User,
   Context, Cluster, Namespace, and KubeSphere Project, plus the relevant
   global flags.
6. **Configuration and credentials** — the config location and model, Context
   inspection and switching, redaction behavior, supported credential fields,
   and credential precedence and failure rules.
7. **Resource inspection** — `get` and `describe`, resource discovery, names,
   selectors, sorting, watching, namespaces, clusters, and output formats.
8. **Kubeconfig generation** — selection behavior, stdout-only output, and safe
   file redirection.
9. **Executable plugins** — naming, both entrypoints, longest-match dispatch,
   dash-to-underscore mapping, argument placement, listing and diagnostics,
   built-in conflicts, and the trust boundary.
10. **Environment variables and global flags** — connection overrides and the
    alternate config path.
11. **Common workflows and troubleshooting** — representative tasks and links
    to command-specific help.

Examples use `ksctl` by default. The equivalent `kubectl ks` form is explained
once rather than repeated throughout the document. Examples use placeholder
endpoints, Contexts, Clusters, Projects, and credentials; no live endpoint is
required to validate them.

## Design Document Structure

`docs/design.md` describes the current implementation and contains:

1. **Design goals** — one interface for KubeSphere and Kubernetes resources,
   kubectl-compatible resource inspection, human and automation use, and a
   read-only built-in resource surface.
2. **Non-goals** — resource mutation commands, a local reimplementation of
   kubectl resource behavior, use or mutation of the user's kubeconfig, and
   KubeSphere 3.x compatibility.
3. **Command architecture** — the shared Cobra command tree behind `ksctl` and
   `kubectl ks` and the difference in their displayed names.
4. **Resource command pipeline** — direct construction of kubectl `get` and
   `describe` commands and the roles of the Factory, discovery client,
   RESTMapper, and REST client getter.
5. **Client boundaries** — the responsibilities of `pkg/client/kubernetes`,
   `pkg/client/kubesphere`, and the KubeSphere connection package.
6. **Configuration model** — Fleet-owned Users, Context selection, and the
   optional default Cluster in the independent `~/.ksctl/config.yaml` file.
7. **Authentication model** — explicit token overrides, environment defaults,
   configured token sources, token cache and refresh, password fallback, and
   terminal failure behavior.
8. **Cross-cluster routing** — the boundary between KubeSphere API requests,
   standard Kubernetes API requests through KubeSphere, and member Cluster
   selection.
9. **Generated kubeconfig** — server retrieval and stdout-only delivery without
   merging into local kubeconfig.
10. **Plugin model** — executable discovery, longest-name matching, argument
    pass-through, and protection of built-in commands.
11. **Security properties** — password persistence rules, redacted config
    output, filesystem permissions, credential-bearing output, and the plugin
    execution trust boundary.
12. **Compatibility and validation** — the supported Go, Kubernetes, kubectl,
    and KubeSphere constraints and the tests that protect the architecture.

The design uses the current source tree as its source of truth. It does not
copy stale package names, removed flags, or superseded configuration examples
from historical design specifications.

## Content and Safety Rules

- Commands, arguments, flags, environment variables, paths, configuration
  fields, and precedence rules must match the current implementation.
- Security guidance for raw config output, bearer tokens, generated
  kubeconfig, cached credentials, and executable plugins must remain explicit.
- The documentation must not suggest that ksctl implements kubectl commands
  other than the commands registered in the current command tree.
- The design must distinguish built-in read-only resource commands from
  arbitrary executable plugins, which may perform other actions.
- Detailed behavior belongs in exactly one primary document. Other documents
  link to it or summarize it briefly.
- `docs/superpowers/` remains unchanged except for this specification and its
  implementation plan.

## Validation

Documentation validation includes:

1. build both CLI entrypoints with `make build`;
2. inspect root and nested `--help` output for the documented command surface
   and flags;
3. compare configuration, authentication, client, kubeconfig, and plugin
   claims with their current packages and tests;
4. verify all relative Markdown links from the README and new documents;
5. search tracked files for stale `README_zh.md` references;
6. check Markdown heading structure and fenced code blocks;
7. run `git diff --check`; and
8. review the final diff for accidental behavioral or generated-file changes.

The full Go test suite is not required because the implementation changes only
Markdown files. Building the binaries and inspecting their help output is the
runtime verification boundary for this documentation-only change.
