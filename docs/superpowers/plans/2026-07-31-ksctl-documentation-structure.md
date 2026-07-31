# ksctl Documentation Structure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore the concise pre-PR #27 documentation structure while presenting `ksctl kube` as one core, nearly complete kubectl-compatible capability.

**Architecture:** Treat the README as the project overview, the CLI guides as ksctl-specific user guidance, and the design guides as stable architectural boundaries. Remove kubectl subcommand reference material and implementation details while retaining the actual `get`/`kube` behavior, KubeSphere authentication, Cluster routing, kubeconfig difference, and RBAC boundary.

**Tech Stack:** Markdown, Cobra-generated command help, Git

## Global Constraints

- Modify only `README.md`, `docs/cli.md`, `docs/cli_zh.md`, `docs/design.md`, and `docs/design_zh.md`.
- Do not change `CHANGELOG.md`, Go code, tests, dependencies, or generated artifacts.
- Assume readers already know kubectl; do not document individual kubectl subcommands.
- Describe `ksctl kube` as providing nearly the full kubectl operation surface through KubeSphere authentication and Cluster routing.
- Keep the English and Chinese guides structurally and factually aligned.
- Preserve established capitalization for Context, Cluster, Workspace, Namespace, Extension, Endpoint, and Token.
- Keep at most one representative `ksctl kube` example in the README and one in each CLI guide.
- Do not name a kubectl dependency version, constructors, factories, or transport protocols.

---

## File responsibility map

- `README.md`: concise project overview, representative Quick start, command
  groups, and connection concepts.
- `docs/cli.md`: English guide to ksctl-specific behavior and workflows.
- `docs/cli_zh.md`: natural Chinese mirror of `docs/cli.md`.
- `docs/design.md`: English architectural boundaries and data-flow decisions.
- `docs/design_zh.md`: natural Chinese mirror of `docs/design.md`.

### Task 1: Restore the README overview

**Files:**
- Modify: `README.md:3-107`

**Interfaces:**
- Consumes: the command surface implemented on `main`, especially top-level
  `get`, `kube`, connection flags, tenant commands, Extension commands, and
  `api`.
- Produces: the terminology and concise `get`/`kube` capability statement used
  as the baseline for both CLI guides.

- [ ] **Step 1: Restore representative Highlights and safety language**

Keep the existing section order. Replace the detailed `kube` highlight with one
capability statement equivalent to:

```markdown
- Use `kube` for nearly the full kubectl operation surface through KubeSphere
  authentication and member-Cluster routing.
```

Keep the separate top-level `get` highlight. Keep the short safety paragraph,
but describe `kube` as a general Kubernetes operation surface rather than
enumerating read, mutation, rollout, debugging, streaming, and
cluster-management categories.

- [ ] **Step 2: Restore the broad Quick start**

Replace the current sequence of `kube describe`, `kube logs`, and `kube top`
examples with the pre-PR #27 cross-feature examples:

```bash
ksctl auth login
ksctl auth whoami
ksctl config current-context

ksctl get pods -A
ksctl kube apply -f app.yaml --cluster member-1
ksctl tenant get workspace
ksctl extension list
ksctl api /kapis/version
```

Update the following paragraph so it explains the example names and the
mutating nature of the `kube apply` example. Remove the Metrics Server note,
because the Quick start no longer demonstrates `top`.

- [ ] **Step 3: Simplify the command overview**

Keep separate rows for `get` and `kube`. Use concise purposes:

```markdown
| `get` | Read Kubernetes and discovered KubeSphere resources. |
| `kube` | Run nearly the full kubectl operation surface through KubeSphere. |
```

Remove the sentence about the release companion rendering the same
`unictl ks kube ...` tree. Keep the general command-help and CLI-guide links.

- [ ] **Step 4: Preserve the real flag scopes**

Keep the Scope and connection table. Retain the distinction that top-level
`get` defines a local `--namespace`, while `kube` provides persistent
`--namespace` and `--request-timeout`. Do not explain upstream kubectl flag
behavior.

- [ ] **Step 5: Verify the README structure**

Run:

```bash
test "$(sed -n '/^## Quick start$/,/^## Command overview$/p' README.md | rg -c '^ksctl kube ')" -eq 1
rg -n '^ksctl auth whoami$|^ksctl config current-context$|^ksctl extension list$|^ksctl api /kapis/version$' README.md
! rg -n 'kube describe|kube logs|kube top|complete kubectl|rollout, debugging, streaming' README.md
git diff --check -- README.md
```

Expected: the count check succeeds; all four restored examples are found; the
forbidden-detail search returns no matches; the diff check exits zero.

- [ ] **Step 6: Commit the README**

```bash
git add README.md
git commit -m "streamline kube README coverage"
```

### Task 2: Collapse the bilingual CLI guides to ksctl-specific guidance

**Files:**
- Modify: `docs/cli.md:5-150`
- Modify: `docs/cli.md:336-414`
- Modify: `docs/cli_zh.md:5-139`
- Modify: `docs/cli_zh.md:293-360`

**Interfaces:**
- Consumes: the capability language established in `README.md` and current
  Cobra flag scopes.
- Produces: aligned English and Chinese user guidance referenced by the README
  and design documents.

- [ ] **Step 1: Simplify the introductions and prerequisites**

In both languages, state that `kube` provides nearly the full kubectl operation
surface through KubeSphere authentication and `--cluster` routing. Keep the
read-only boundary for top-level `get` and tenant inspection and the mutating
boundary for `kube` and Extension lifecycle operations.

Remove Metrics Server from the prerequisite list. The guide no longer teaches
the `top` subcommand.

- [ ] **Step 2: Keep command groups capability-oriented**

Keep separate command-group rows for top-level `get` and `kube`, but make the
`kube` row a single capability statement. Do not enumerate read, mutate, debug,
stream, rollout, or cluster-management categories.

English wording:

```markdown
| Kubernetes operations | Use nearly the full kubectl operation surface through KubeSphere. | `kube` | Available |
```

Chinese wording:

```markdown
| Kubernetes 操作 | 通过 KubeSphere 使用基本完整的 kubectl 操作能力。 | `kube` | 可用 |
```

- [ ] **Step 3: Replace the expanded Kubernetes sections**

Delete these English subsections and their content:

```text
### Inspect resources
### Read container logs
### View current resource usage
### Run the complete operation suite
```

Delete their Chinese counterparts:

```text
### 查看资源
### 读取容器日志
### 查看当前资源用量
### 使用完整操作集
```

Keep `### Select the resource scope` / `### 选择资源作用域` and its concept
table. Before and after the table, cover only:

- top-level `ksctl get` is the concise, read-only resource entry point;
- `ksctl kube` provides nearly the full kubectl operation surface;
- both use ksctl connection, authentication, Context, and Cluster selection;
- `kube` does not read or write `~/.kube/config`;
- `ksctl config generate kubeconfig` is the explicit kubeconfig workflow;
- mutating operations use Kubernetes RBAC and receive no extra confirmation;
  and
- syntax and upstream behavior come from `ksctl kube --help` and kubectl
  documentation.

Use one example block in each language:

```bash
ksctl get deployments,pods -n demo --cluster member-1
ksctl kube apply -f app.yaml --cluster member-1
```

Do not retain the PR #27 breaking-change callout. `CHANGELOG.md` is the release
history for moved commands and flags.

- [ ] **Step 4: Keep flag documentation accurate and compact**

In Global options and environment variables / 全局选项和环境变量:

- keep `--endpoint`, `--token`, `--context`, `--cluster`, and `-v` as root
  options;
- state that top-level `get` defines local `-n, --namespace`;
- state that `kube` defines persistent `-n, --namespace` and
  `--request-timeout`; and
- do not describe how individual kubectl subcommands use those flags.

- [ ] **Step 5: Remove subcommand-specific workflows and troubleshooting**

Replace any `kube logs` or `kube top` examples in Common workflows / 常用工作流
with top-level `get` or the one representative `kube apply` example, while
keeping no more than one `ksctl kube` example in the entire guide.

Remove `### Metrics API not available` / `### Metrics API 不可用` and their
content. Keep the remaining authentication, Context, resource-discovery,
Extension, and plugin troubleshooting guidance unchanged.

- [ ] **Step 6: Verify the bilingual CLI structure**

Run:

```bash
rg -n '^#{1,3} ' docs/cli.md
rg -n '^#{1,3} ' docs/cli_zh.md
test "$(rg -c '^ksctl kube ' docs/cli.md)" -eq 1
test "$(rg -c '^ksctl kube ' docs/cli_zh.md)" -eq 1
! rg -n 'v0\.36\.2|### Read container logs|### View current resource usage|### Run the complete operation suite|### 读取容器日志|### 查看当前资源用量|### 使用完整操作集' docs/cli.md docs/cli_zh.md
! rg -n 'certificate.*cluster-info|create.*expose.*run|SPDY|WebSocket|HTTP Upgrade' docs/cli.md docs/cli_zh.md
git diff --check -- docs/cli.md docs/cli_zh.md
```

Expected: the heading lists have matching hierarchy and order; each guide has
one `ksctl kube` command line; forbidden headings, command inventories,
versions, and transport details are absent; the diff check exits zero.

- [ ] **Step 7: Commit the CLI guides**

```bash
git add docs/cli.md docs/cli_zh.md
git commit -m "refocus CLI guides on ksctl behavior"
```

### Task 3: Restore the bilingual design boundaries

**Files:**
- Modify: `docs/design.md:8-106`
- Modify: `docs/design.md:188-213`
- Modify: `docs/design_zh.md:8-90`
- Modify: `docs/design_zh.md:154-173`

**Interfaces:**
- Consumes: the user-facing capability boundary established in the README and
  CLI guides.
- Produces: the stable architectural explanation for future maintainers.

- [ ] **Step 1: Compress Goals and boundaries**

Keep four safety boundaries:

1. top-level `get` and tenant inspection are read-only;
2. `kube` is the general Kubernetes operation surface using KubeSphere
   authentication and Cluster routing;
3. Extension lifecycle commands are purpose-built controlled writes; and
4. `api` is a raw authenticated escape hatch.

Replace the detailed multi-item kubectl non-goal list with a concise paragraph
covering only stable boundaries: ksctl does not use kubeconfig as its persistent
configuration model, aggregate resources across Clusters, audit or sandbox
plugins, or support KubeSphere 3.x.

- [ ] **Step 2: Remove Kubernetes command assembly**

Delete the entire `### Kubernetes command assembly` section from
`docs/design.md` and the entire `### Kubernetes 命令装配` section from
`docs/design_zh.md`, including:

- the command pipeline diagram;
- kubectl version references;
- included or excluded command details;
- constructor, Factory, and RESTClientGetter details;
- flag-ownership implementation details; and
- SPDY, WebSocket, HTTP Upgrade, DiscoveryClient, and `top` adapter details.

After deletion, Cross-Cluster resource access / 跨集群资源访问 is the first
subsection under Core design / 核心设计.

- [ ] **Step 3: Generalize the Cross-Cluster data flow**

Keep Context, explicit override, Fleet Endpoint, Namespace, discovery fallback,
and single-Cluster semantics. Replace lists of reads, mutations, streaming, and
metrics requests with the stable statement that Kubernetes operations and
their supporting requests share one effective Cluster route.

- [ ] **Step 4: Simplify Security and compatibility**

State that `kube` may mutate the selected Cluster and is authorized by
Kubernetes RBAC. State that it does not use local kubeconfig and does not retry
a failed member-Cluster operation against the host Cluster.

Remove named mutation-command examples. Replace the final dependency-detail
sentence with a concise compatibility statement that ksctl supports
KubeSphere 4.x and tracks upstream Kubernetes compatibility as one integration
surface.

- [ ] **Step 5: Verify the bilingual design structure**

Run:

```bash
rg -n '^#{1,3} ' docs/design.md
rg -n '^#{1,3} ' docs/design_zh.md
! rg -n 'Kubernetes command assembly|Kubernetes 命令装配|v0\.36\.2|RESTClientGetter|Factory|DiscoveryClient|SPDY|WebSocket|HTTP Upgrade' docs/design.md docs/design_zh.md
! rg -n 'apply, delete, drain|`apply`、`delete`、`drain`|config.*plugin.*version.*completion' docs/design.md docs/design_zh.md
git diff --check -- docs/design.md docs/design_zh.md
```

Expected: Cross-Cluster resource access is the first Core design subsection in
both languages; all implementation terms and command lists are absent; the
diff check exits zero.

- [ ] **Step 6: Commit the design guides**

```bash
git add docs/design.md docs/design_zh.md
git commit -m "simplify Kubernetes design coverage"
```

### Task 4: Cross-document verification

**Files:**
- Verify: `README.md`
- Verify: `docs/cli.md`
- Verify: `docs/cli_zh.md`
- Verify: `docs/design.md`
- Verify: `docs/design_zh.md`

**Interfaces:**
- Consumes: Tasks 1-3.
- Produces: a verified documentation-only change set ready for review.

- [ ] **Step 1: Confirm the scope**

Run:

```bash
git diff --name-only HEAD~3..HEAD
```

Expected: only the five target documents are listed across the three
implementation commits.

- [ ] **Step 2: Check the installed command paths**

Run:

```bash
make build
./bin/ksctl get --help
./bin/ksctl kube --help
./bin/ksctl config generate kubeconfig --help
```

Expected: build exits zero and all three documented command paths print help
without an unknown-command error.

- [ ] **Step 3: Check document links and forbidden detail**

Run:

```bash
test -f docs/cli.md
test -f docs/cli_zh.md
test -f docs/design.md
test -f docs/design_zh.md
! rg -n 'v0\.36\.2|RESTClientGetter|DiscoveryClient|SPDY|WebSocket|HTTP Upgrade' README.md docs/cli.md docs/cli_zh.md docs/design.md docs/design_zh.md
! rg -n '### Read container logs|### View current resource usage|### Run the complete operation suite|### 读取容器日志|### 查看当前资源用量|### 使用完整操作集' docs/cli.md docs/cli_zh.md
git diff --check HEAD~3..HEAD
```

Expected: all linked local documents exist; forbidden implementation and
subcommand-reference details are absent; the diff check exits zero.

- [ ] **Step 4: Review the final diff**

Run:

```bash
git diff --stat HEAD~3..HEAD
git diff HEAD~3..HEAD -- README.md docs/cli.md docs/cli_zh.md docs/design.md docs/design_zh.md
git status --short
```

Expected: the diff contains only the approved structural cleanup, no generated
`bin/` artifact is tracked, and the worktree is clean after the three task
commits.
