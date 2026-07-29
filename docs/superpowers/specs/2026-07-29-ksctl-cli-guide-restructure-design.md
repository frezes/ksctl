# ksctl CLI Guide Restructure Design

## Goal

Turn `docs/cli.md` into a concise, task-oriented introduction for readers who
already know `kubectl`. A reader should be able to understand what ksctl is,
scan its command families, and find the commands for a common task without
first reading a complete reference manual.

## Scope

This change restructures and rewrites `docs/cli.md`. It does not change the CLI
command tree or split the guide into multiple files.

The guide will:

- introduce ksctl before presenting command details;
- organize commands by KubeSphere product capability;
- document available command families with focused examples;
- show the complete product taxonomy while marking unavailable families
  clearly;
- add tenant and administrator workflows; and
- retain only constraints that prevent common mistakes or unsafe use.

The guide will not:

- enumerate every flag or reproduce built-in help;
- explain general `kubectl` concepts such as label selectors or JSONPath;
- document `completion` or `version`; or
- create empty detail sections for unavailable command families.

## Audience and Tone

The primary reader understands Kubernetes and is familiar with `kubectl`.
Explanations should focus on ksctl-specific concepts: KubeSphere endpoints,
Contexts, member Clusters, tenant resources, and extension management.

The writing should be direct and compact. Each command-family section follows
the same rhythm:

1. a short statement of purpose;
2. a command summary table;
3. representative examples; and
4. brief notes for ksctl-specific constraints.

Built-in `--help` remains the exhaustive source for arguments and flags.

## Information Architecture

The guide will use the following top-level structure:

1. **Introduction**
   - ksctl's purpose and intended use;
   - the equivalent `ksctl` and `kubectl ks` entrypoints;
   - the boundary between read-only resource inspection and extension
     lifecycle mutations; and
   - minimal prerequisites.
2. **Command syntax**
   - common command forms; and
   - built-in help discovery.
3. **Command groups**
   - a compact navigation table containing all six product capability groups.
4. **Manage Kubernetes resources**
   - scope selection;
   - `get`, `describe`, `logs`, and `top`; and
   - focused filtering and output examples.
5. **Manage tenants**
   - the relationship between Workspaces, Namespaces, and Clusters; and
   - `tenant get workspace`, `tenant get namespace`, and
     `tenant get cluster`.
6. **Manage extensions**
   - discovery with `list`, `show`, `versions`, and `status`;
   - lifecycle operations with `install`, `upgrade`, `configure`, and
     `uninstall`; and
   - troubleshooting with `diagnose`.
7. **Other commands**
   - authentication and identity;
   - Context and configuration inspection;
   - kubeconfig generation;
   - direct KubeSphere API requests; and
   - executable plugins.
8. **Global options and environment variables**
9. **Common workflows**
   - tenant workflow; and
   - administrator workflow.
10. **Troubleshooting**
    - only common problems whose causes are not obvious from the error text.

## Command Classification

| Group | Commands | Detail section |
| --- | --- | --- |
| Kubernetes resource management | `get`, `describe`, `logs`, `top` | Yes |
| Cluster management | Not yet available | No |
| Tenant management | `tenant` | Yes |
| Extension management | `extension` | Yes |
| Application management | Not yet available | No |
| Other | `auth`, `config`, `api`, `plugin` | Yes |

`config generate kubeconfig` belongs to **Other** and is described alongside
the other configuration commands. `completion` and `version` are intentionally
omitted from both the classification table and the guide body.

## Content Boundaries

The rewrite will preserve concise explanations for behaviors that differ from
normal kubectl expectations or can cause user error:

- how a Context selects an Endpoint, User, and optional default Cluster;
- the pairing requirement for explicit Endpoint and Token overrides;
- the meaning of Namespace versus KubeSphere Project;
- extension operations being host-scoped rather than routed through the
  Context's default member Cluster;
- exact extension version selection where required;
- asynchronous extension lifecycle requests and the purpose of `--wait`; and
- safe handling of passwords, tokens, and unredacted configuration.

Long API-path descriptions, complete output-column inventories, exhaustive
flag interactions, and implementation details will be removed. Representative
examples should teach the normal path, while `COMMAND --help` provides the
complete contract.

The target size is approximately 300–400 lines. Clarity takes precedence over
hitting an exact line count.

## Role-Based Workflows

### Tenant workflow

The tenant workflow demonstrates how to:

1. log in and confirm the current identity;
2. list accessible Workspaces;
3. list Namespaces and Clusters associated with a Workspace; and
4. inspect workloads, logs, and resource usage in a selected Cluster and
   Namespace.

### Administrator workflow

The administrator workflow demonstrates how to:

1. log in and confirm an administrator identity;
2. inspect and switch Contexts;
3. inspect resources across Clusters and Namespaces;
4. inspect tenant resources;
5. discover, install, and diagnose extensions; and
6. call a KubeSphere API path when a purpose-built command is not available.

These workflows reuse commands already explained earlier and remain compact;
they are navigation aids, not separate tutorials.

## Verification

The completed rewrite will be checked against the source command tree and live
CLI help so that every documented command and example is valid. Verification
will include:

- confirming every available command in the classification table exists;
- confirming unavailable groups are labeled consistently and have no empty
  detail sections;
- checking that `completion` and `version` do not appear;
- checking heading order and link-free Markdown rendering;
- scanning examples for the correct `ksctl` command syntax;
- running a whitespace check with `git diff --check`; and
- reviewing the final diff for accidental loss of safety-critical guidance.
