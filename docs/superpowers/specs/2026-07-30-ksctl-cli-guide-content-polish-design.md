# ksctl CLI Guide Content Polish Design

## Goal

Refine the English and Simplified Chinese CLI guides so they explain ksctl's
cross-cluster and tenant-oriented value more clearly, assume kubectl
familiarity, and remove log-following guidance.

## Scope

Modify `docs/cli.md` and `docs/cli_zh.md` together. Preserve their mirrored
heading hierarchy, table order, examples, and behavioral constraints.

The change will:

- describe Kubernetes resource commands as supporting kubectl-compatible
  cross-cluster inspection through `--cluster`;
- describe tenant commands as showing how accessible resources are distributed
  across Workspaces, Namespaces, and Clusters;
- make the **Other** command group focus on authentication, direct API calls,
  and plugin support;
- organize resource scope around Context, Cluster, Namespace, and Workspace;
- keep resource-management explanations concise for kubectl users; and
- remove all `--follow` examples, log-following descriptions, and the related
  troubleshooting scenario.

The change will not alter CLI behavior, add commands, reorganize unrelated
sections, or remove extension status watching through `--watch`.

## Content Design

### Introduction

Replace the generic read-only command summary with a short explanation of the
two primary inspection paths:

- Kubernetes resource commands use kubectl-compatible syntax and `--cluster`
  for cross-cluster resource inspection.
- Tenant commands show the distribution of resources accessible to the current
  tenant across Workspaces, Namespaces, and Clusters.

Keep the existing prerequisite and extension-mutation notes.

### Command Groups

Keep the six-group table and all availability values. Revise the **Other** row
to describe authentication, direct API calls, and plugin support. The command
list remains `auth`, `config`, `api`, and `plugin` because configuration is part
of authentication and connection context.

### Resource Scope

Replace the current scope table and detailed override examples with four
concepts:

- Context selects the KubeSphere connection and identity.
- Cluster selects the target cluster, including per-command selection through
  `--cluster`.
- Namespace selects a Kubernetes Namespace or KubeSphere Project.
- Workspace represents the tenant scope used to understand accessible
  Namespaces and Clusters.

The section will avoid explaining familiar kubectl mechanics in detail.

### Log Guidance and Troubleshooting

Remove `--follow` from all log examples and remove wording that presents logs
as continuously followed. Delete the dedicated log-follow interruption
troubleshooting subsection. Retain concise security and routing notes for
ordinary log reads.

## Verification

Verify that:

- English and Chinese heading structures remain identical;
- ordered fenced command blocks remain synchronized;
- both introductions mention `--cluster` cross-cluster inspection and tenant
  resource distribution;
- both scope tables contain Context, Cluster, Namespace, and Workspace;
- the **Other** row focuses on authentication, APIs, and plugins;
- neither guide contains `--follow` or log-following descriptions;
- extension `--watch` documentation remains present; and
- `git diff --check` succeeds.
