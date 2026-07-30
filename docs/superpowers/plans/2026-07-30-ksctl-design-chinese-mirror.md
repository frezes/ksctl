# ksctl Developer Design Documentation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the implementation-heavy architecture reference with concise, bilingual developer design documents centered on ksctl's five essential design flows.

**Architecture:** Keep `docs/design.md` as the canonical English source and rewrite it around goals, a conceptual architecture overview, one core-design section with five H3 capability subsections, and consolidated security and compatibility boundaries. Rewrite `docs/design_zh.md` as a complete Simplified Chinese mirror and retain the existing README and language navigation.

**Tech Stack:** Markdown, Git, `rg`, `awk`, `diff`, and the current Go source and tests as factual evidence only.

## Global Constraints

- The documents are developer design explanations, not source-code indexes, API references, command manuals, or test inventories.
- Do not change CLI behavior, Go code, dependencies, build configuration, or release packaging.
- Do not include fenced code blocks or text diagrams.
- Do not include package paths, source-file paths, Go interfaces, constructors, adapters, private types, method names, configuration field trees, or test inventories.
- Retain only the command names, domain objects, and routing semantics required to make design boundaries precise.
- Keep `docs/design.md` canonical and `docs/design_zh.md` structurally identical.
- Preserve the current language switchers, language-matched CLI links, and bilingual README links.
- Use the current source and tests to verify factual claims, but do not copy implementation details into the maintained design documents.

---

### Task 1: Rewrite the Canonical English Developer Design

**Files:**

- Modify: `docs/design.md`
- Inspect: `pkg/cmd/root.go`
- Inspect: `pkg/cmd/api.go`
- Inspect: `pkg/cmd/tenant/command.go`
- Inspect: `pkg/cmd/extension/command.go`
- Inspect: `pkg/auth/resolver.go`
- Inspect: `pkg/auth/provider.go`
- Inspect: `pkg/client/kubernetes/getter.go`
- Inspect: `pkg/client/kubesphere/connection/getter.go`

**Interfaces:**

- Consumes: the approved developer-documentation specification and current behavior of resource routing, tenant queries, Extension lifecycle management, raw API requests, and authentication.
- Produces: the final English section hierarchy and prose that Task 2 mirrors.

- [ ] **Step 1: Reconfirm the five design flows against current source**

Run:

```bash
rg -n 'AddCommand|Cluster|Namespace|Workspace|InstallPlan|RequestURI|Token|Password|Context|Fleet' pkg/cmd/root.go pkg/cmd/api.go pkg/cmd/tenant/command.go pkg/cmd/extension/command.go pkg/auth/resolver.go pkg/auth/provider.go pkg/client/kubernetes/getter.go pkg/client/kubesphere/connection/getter.go
```

Expected: evidence for the selected resource scope, Fleet/Cluster tenant
distinction, host-owned Extension lifecycle, caller-owned raw API path, and
Fleet/User/Context credential model.

- [ ] **Step 2: Replace the English heading structure**

Use `apply_patch` to rewrite `docs/design.md` with exactly this maintained
hierarchy:

```text
# ksctl Design
## Goals and boundaries
## Architecture overview
## Core design
### Cross-Cluster resource access
### Tenant pipeline
### Extension management
### Raw API requests
### Authentication and configuration
## Security and compatibility
```

Keep the existing English/Chinese language switcher and introductory link to
`cli.md`. Do not add any other H2 or H3 sections.

- [ ] **Step 3: Write goals, boundaries, and architecture overview**

Write concise prose that covers these exact points:

- ksctl provides KubeSphere and Kubernetes inspection through one CLI;
- kubectl-backed resource commands are read-only;
- Extension lifecycle commands are controlled write workflows;
- `api` is a raw authenticated escape hatch that may mutate server state;
- plugins execute outside built-in safeguards;
- ksctl does not provide generic typed mutation, kubeconfig persistence,
  cross-Cluster aggregation, plugin sandboxing, or KubeSphere 3.x support;
- the command layer captures intent and scope;
- connection and authentication resolution select one effective server,
  identity, and optional Cluster; and
- KubeSphere serves native APIs or proxies Kubernetes requests without command
  logic depending on proxy topology.

Do not name source packages, interfaces, constructors, methods, files, or test
cases.

- [ ] **Step 4: Write the five Core design subsections**

Under `## Core design`, write one concise H3 subsection per capability:

1. **Cross-Cluster resource access** — Context defaults, explicit scope
   overrides, shared routing for discovery/read/logs/metrics, Namespace as a
   namespaced-resource selector, compatibility discovery, and one-Cluster-only
   queries.
2. **Tenant pipeline** — Fleet-scoped Workspace and tenant Cluster reads,
   selected-Cluster Namespace reads, optional Workspace narrowing, stable
   tables, and response-preserving structured output.
3. **Extension management** — host control-plane ownership, explicit eligible
   Cluster placement, controlled lifecycle verbs, stale/conflict guards,
   independent asynchronous targets, safe waiting, and bounded diagnosis.
4. **Raw API requests** — normal connection and credential reuse,
   caller-controlled server-relative path/method/body, no automatic Cluster
   routing, byte-preserving output, HTTP error behavior, and no lifecycle or
   redaction safeguards.
5. **Authentication and configuration** — Fleet/User/Context ownership,
   explicit override behavior, explicit Endpoint/Token pairing, fail-closed
   authoritative Token sources, Token cache reuse and refresh, Password
   fallback, atomic login persistence, and best-effort logout revocation.

Each subsection explains responsibility, scope, request flow, and failure or
safety boundary in prose. Do not include command examples, exact request
paths, precedence expressions, schemas, cache paths, resource fields, package
names, or test details.

- [ ] **Step 5: Write consolidated security and compatibility boundaries**

Cover:

- non-echoed and non-persisted login Passwords;
- restricted and atomic Config/Token-cache writes;
- caller responsibility for sensitive output;
- raw API and plugin trust boundaries;
- KubeSphere 4.x support; and
- aligned Kubernetes dependency upgrades.

Do not list individual dependencies, versions, tests, build commands, or
implementation mechanisms.

- [ ] **Step 6: Verify the English document shape and content exclusions**

Run:

```bash
rg -n '^#{2,3} ' docs/design.md
```

Expected:

```text
## Goals and boundaries
## Architecture overview
## Core design
### Cross-Cluster resource access
### Tenant pipeline
### Extension management
### Raw API requests
### Authentication and configuration
## Security and compatibility
```

Run:

```bash
rg -n '```|pkg/|internal/|\.go\b|RESTClientGetter|ToREST|tests verify|test inventory' docs/design.md
```

Expected: exit status 1 with no matches.

Run:

```bash
git diff --check -- docs/design.md
```

Expected: exit status 0 with no output.

- [ ] **Step 7: Review and commit the canonical English rewrite**

Run:

```bash
git diff -- docs/design.md
```

Expected: the implementation-heavy reference is replaced by the approved
conceptual hierarchy without losing the five core design boundaries.

Commit:

```bash
git add docs/design.md
git commit -m "streamline developer design documentation"
```

---

### Task 2: Rewrite the Simplified Chinese Mirror

**Files:**

- Modify: `docs/design_zh.md`
- Verify: `README.md`
- Reference: `docs/design.md`
- Reference: `docs/cli.md`
- Reference: `docs/cli_zh.md`

**Interfaces:**

- Consumes: the final canonical English prose and hierarchy produced by Task 1.
- Produces: a complete Simplified Chinese mirror with identical design
  boundaries and maintained navigation.

- [ ] **Step 1: Replace the Chinese heading structure**

Use `apply_patch` to rewrite `docs/design_zh.md` with exactly this hierarchy:

```text
# ksctl 设计
## 设计目标与边界
## 整体架构
## 核心设计
### 跨集群资源访问
### 租户管线
### 扩展组件管理
### 原始 API 请求
### 认证与配置
## 安全与兼容性
```

Keep the existing language switcher and introductory link to `cli_zh.md`.

- [ ] **Step 2: Translate the complete English source**

Translate every paragraph, bullet, and numbered item from the final
`docs/design.md`. Preserve the same order and technical strength.

Use these terms consistently:

```text
Context
Fleet
User
Workspace
Cluster
Namespace
Endpoint
Token
InstallPlan
kubeconfig
```

Translate Extension as “扩展组件” in prose. Keep command names and technical
identifiers unchanged. Use natural, concise Chinese rather than word-for-word
syntax.

- [ ] **Step 3: Verify structural and technical mirroring**

Run:

```bash
diff -u <(rg -o '^#{2,3}' docs/design.md) <(rg -o '^#{2,3}' docs/design_zh.md)
```

Expected: exit status 0 with no output.

Run each command:

```bash
awk '/^- /{n++} END{print n+0}' docs/design.md
awk '/^- /{n++} END{print n+0}' docs/design_zh.md
awk '/^[0-9]+\. /{n++} END{print n+0}' docs/design.md
awk '/^[0-9]+\. /{n++} END{print n+0}' docs/design_zh.md
awk 'BEGIN{RS=""} END{print NR}' docs/design.md
awk 'BEGIN{RS=""} END{print NR}' docs/design_zh.md
```

Expected: English and Chinese counts match for bullets, numbered items, and
paragraph blocks.

Run:

```bash
diff -u <(rg -o '`[^`]+`' docs/design.md | sort -u) <(rg -o '`[^`]+`' docs/design_zh.md | sort -u)
```

Expected: exit status 0 with no output.

- [ ] **Step 4: Verify zero implementation-detail leakage**

Run:

```bash
rg -n '```|pkg/|internal/|\.go\b|RESTClientGetter|ToREST|测试验证|测试清单' docs/design.md docs/design_zh.md
```

Expected: exit status 1 with no matches.

Run:

```bash
rg -n 'design_zh.md|design.md|cli.md|cli_zh.md' README.md docs/design.md docs/design_zh.md
```

Expected: both language switchers, both language-matched CLI links, and both
README design links are present.

- [ ] **Step 5: Review and commit the Chinese rewrite**

Run:

```bash
git diff -- docs/design_zh.md
```

Expected: the Chinese implementation-heavy reference is replaced by a complete
mirror of the approved English conceptual design.

Run:

```bash
git diff --check
```

Expected: exit status 0 with no output.

Commit:

```bash
git add docs/design_zh.md
git commit -m "streamline Chinese developer design"
```

---

### Task 3: Perform Final Documentation Verification

**Files:**

- Verify: `README.md`
- Verify: `docs/design.md`
- Verify: `docs/design_zh.md`
- Verify: `docs/superpowers/specs/2026-07-30-ksctl-design-chinese-mirror-design.md`
- Verify: `docs/superpowers/plans/2026-07-30-ksctl-design-chinese-mirror.md`

**Interfaces:**

- Consumes: both committed conceptual design documents.
- Produces: evidence that the branch satisfies the revised approved
  specification without unrelated changes.

- [ ] **Step 1: Re-run structural mirroring checks**

Run:

```bash
diff -u <(rg -o '^#{2,3}' docs/design.md) <(rg -o '^#{2,3}' docs/design_zh.md)
```

Run:

```bash
diff -u <(rg -o '`[^`]+`' docs/design.md | sort -u) <(rg -o '`[^`]+`' docs/design_zh.md | sort -u)
```

Expected: both commands exit 0 with no output.

- [ ] **Step 2: Re-run content-exclusion checks**

Run:

```bash
rg -n '```|pkg/|internal/|\.go\b|RESTClientGetter|ToREST|tests verify|test inventory|测试验证|测试清单' docs/design.md docs/design_zh.md
```

Expected: exit status 1 with no matches.

- [ ] **Step 3: Scan for placeholders**

Run:

```bash
rg -n 'TODO|TBD|FIXME|XXX' README.md docs/design.md docs/design_zh.md
```

Expected: exit status 1 with no matches.

- [ ] **Step 4: Verify final changed-file scope**

Run:

```bash
git diff main...HEAD --name-only
```

Expected:

```text
README.md
docs/design.md
docs/design_zh.md
docs/superpowers/plans/2026-07-30-ksctl-design-chinese-mirror.md
docs/superpowers/specs/2026-07-30-ksctl-design-chinese-mirror-design.md
```

- [ ] **Step 5: Run final whitespace and worktree checks**

Run:

```bash
git diff main...HEAD --check
```

Expected: exit status 0 with no output.

Run:

```bash
git status --short
```

Expected: no output.
