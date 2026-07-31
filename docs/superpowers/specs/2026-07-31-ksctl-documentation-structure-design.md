# ksctl Documentation Structure Design

## Context

Pull request #27 added the `ksctl kube` command suite and updated the public
documentation to describe its kubectl-compatible operations. The resulting
documentation gives too much space to individual kubectl commands and internal
assembly details. This changes the guides from concise ksctl documentation into
a partial kubectl reference.

The documentation before pull request #27 used a clearer structure: introduce
ksctl, show representative workflows, explain ksctl-specific scope and
connection behavior, and leave familiar upstream behavior implicit. The new
`kube` capability should fit that structure instead of replacing it.

## Goals

- Restore the concise, task-oriented structure used before pull request #27.
- Present `ksctl kube` as one core ksctl feature that provides nearly the full
  kubectl operation surface.
- Assume readers already know kubectl and avoid documenting its individual
  commands.
- Focus on ksctl-specific behavior: KubeSphere authentication, Context and
  Cluster selection, connection overrides, kubeconfig differences, and RBAC
  boundaries.
- Keep the English and Chinese guides structurally and factually aligned.

## Non-goals

- Changing commands, flags, help output, or runtime behavior.
- Replacing the kubectl documentation.
- Enumerating the `kube` command tree or grouping its subcommands by purpose.
- Recording kubectl constructors, factories, dependency versions, or transport
  protocol details.
- Changing the release history in `CHANGELOG.md`.

## Content principles

The public documentation should describe what is unique about ksctl. A reader
who needs syntax or behavior for a kubectl operation should use
`ksctl kube --help` and the upstream kubectl documentation.

`ksctl kube` should normally be described as a single capability:

> `ksctl kube` provides nearly the full kubectl operation surface through
> KubeSphere authentication and Cluster routing.

The wording may vary naturally between documents, but the emphasis must remain
on the capability boundary rather than its subcommands. Individual `kube`
commands may appear as isolated examples, but they must not become separate
reference sections.

## README structure

`README.md` keeps its pre-PR #27 section order:

1. Project summary
2. Highlights
3. Release installation
4. Source build
5. Quick start
6. Command overview
7. Scope and connection
8. Documentation
9. Development

The highlights contain one concise bullet for `kube`. The Quick start retains
representative examples across authentication, Context selection, resource
access, tenant management, Extension management, and raw API access. It uses no
more than one `ksctl kube` example.

The command overview gives `get` and `kube` separate rows. The `get` row
describes the concise, read-only resource entry point. The `kube` row describes
the kubectl-compatible operation surface without listing operations.

## CLI guide structure

`docs/cli.md` and `docs/cli_zh.md` keep their existing major section order:

1. Introduction
2. Command syntax
3. Command groups
4. Kubernetes resource management
5. Tenant management
6. Extension management
7. Other commands
8. Global options and environment variables
9. Common workflows
10. Troubleshooting

The Kubernetes resource section is compact. It explains:

- top-level `ksctl get` is a concise, read-only ksctl resource entry point;
- `ksctl kube` provides nearly the full kubectl operation surface;
- `kube` uses ksctl connection, authentication, Context, and Cluster routing;
- `kube` does not use the user's local kubeconfig;
- mutating operations are authorized by Kubernetes RBAC; and
- command-specific behavior is documented by command help and upstream kubectl
  documentation.

The section may retain a short scope example and a single representative
`kube` example. It must not contain separate explanations for `describe`,
`logs`, `top`, `apply`, or other kubectl subcommands. It must not contain a
subcommand inventory or an operation-category table.

The global options section documents the actual flag scopes without turning
them into kubectl instruction. The common workflows may use a `kube` command
where useful, but they do not explain that command's upstream behavior.

## Design document structure

`docs/design.md` and `docs/design_zh.md` return to their pre-PR #27 section
order:

1. Goals and boundaries
2. Architecture overview
3. Core design
   - Cross-Cluster resource access
   - Tenant pipeline
   - Extension management
   - Raw API requests
   - Authentication and configuration
4. Security and compatibility

There is no standalone Kubernetes command assembly section. The documents
describe `kube` only where it affects an architectural boundary:

- Goals and boundaries distinguish the read-only top-level `get` path from the
  general Kubernetes administration surface under `kube`.
- Cross-Cluster resource access states that Kubernetes operations use the same
  effective Cluster route as discovery and supporting requests.
- Security and compatibility state that `kube` may mutate the selected Cluster,
  relies on Kubernetes RBAC, does not use local kubeconfig, and does not fall
  back from a failed member-Cluster operation to the host Cluster.

The design documents do not name the aligned kubectl version, list included or
excluded subcommands, describe command constructors or factories, or enumerate
streaming protocols. Those details belong in code, tests, dependency metadata,
and implementation plans.

## Bilingual consistency

The English document is the editing baseline and the Chinese document is its
content mirror. Both versions must have the same heading hierarchy, paragraph
order, examples, tables, and behavioral claims. The Chinese text should read
naturally rather than follow English line wrapping or sentence structure
mechanically.

Terms already used as product concepts, including Context, Cluster, Workspace,
Namespace, Extension, Endpoint, and Token, retain the repository's established
capitalization.

## Scope of changes

Implementation changes only:

- `README.md`
- `docs/cli.md`
- `docs/cli_zh.md`
- `docs/design.md`
- `docs/design_zh.md`

`CHANGELOG.md`, command implementation, tests, and generated artifacts remain
unchanged.

## Validation

The implementation is complete when:

- no CLI guide section expands individual `kube` subcommands;
- no target document contains a complete kubectl command list;
- design documents contain no kubectl version, constructor, factory, or
  transport-protocol implementation detail;
- each target document clearly states the `get`/`kube` capability boundary;
- English and Chinese heading structures and claims match;
- documented command paths agree with current help output;
- repository-local Markdown links resolve; and
- `git diff --check` passes.
