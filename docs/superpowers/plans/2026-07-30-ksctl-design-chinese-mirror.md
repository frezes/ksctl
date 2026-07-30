# ksctl Design Document Chinese Mirror Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Correct the maintained architecture reference for the current `api` command and add a complete Simplified Chinese mirror with bilingual navigation.

**Architecture:** Keep `docs/design.md` as the canonical architecture reference and make only source-supported corrections. Generate `docs/design_zh.md` as a section-for-section translation whose technical tokens and fenced blocks remain aligned with the English source, then expose both documents from `README.md`.

**Tech Stack:** Markdown, Git, `rg`, `awk`, `diff`, and the current Go source and tests as documentation evidence.

## Global Constraints

- Do not change CLI behavior, Go code, dependencies, build configuration, or release packaging.
- Do not reorganize the existing design document or translate historical files under `docs/superpowers/`.
- Treat current source and tests as the source of truth.
- Keep `docs/design.md` canonical and `docs/design_zh.md` structurally identical.
- Do not translate commands, flags, environment variables, package names, types, field names, API paths, versions, file paths, configuration keys, or fenced code.
- Preserve the technical strength of warnings and security qualifications in both languages.
- Limit the implementation diff to `README.md`, `docs/design.md`, and `docs/design_zh.md`; this plan and its approved specification are separate planning records.

---

### Task 1: Correct the English Architecture Reference

**Files:**

- Modify: `docs/design.md:1-38`
- Modify: `docs/design.md:39-193`
- Modify: `docs/design.md:382-419`
- Modify: `docs/design.md:466-525`
- Inspect: `pkg/cmd/root.go`
- Inspect: `pkg/cmd/api.go`
- Inspect: `pkg/cmd/api_test.go`
- Inspect: `pkg/client/kubesphere/connection/getter.go`

**Interfaces:**

- Consumes: the root command registration, `newAPICommand` request behavior, and KubeSphere connection getter semantics.
- Produces: the canonical English headings and prose that Task 2 mirrors exactly.

- [ ] **Step 1: Reconfirm the raw API behavior from source and tests**

Run:

```bash
rg -n 'newAPICommand|Use:|methodSet|dataSet|RequestURI|ContentType|DoRaw|Cluster' pkg/cmd/root.go pkg/cmd/api.go pkg/cmd/api_test.go pkg/client/kubesphere/connection/getter.go
```

Expected: the root registers `api`; the command accepts one server-relative
path, optional method and data, uses `RequestURI`, writes raw response bytes,
and does not automatically add selected Cluster scope.

- [ ] **Step 2: Correct the read-only boundary and root command inventory**

Use `apply_patch` to make these exact conceptual changes in
`docs/design.md`:

```markdown
- Keep the kubectl-backed generic resource surface read-only. Purpose-built
  Extension lifecycle commands provide controlled writes, while `api` is an
  explicit low-level escape hatch that sends caller-selected KubeSphere API
  requests.
```

Replace the generic-mutation non-goal with:

```markdown
- Typed built-in create, update, edit, patch, delete, apply, or other generic
  resource mutation commands. The raw `api` transport escape hatch is not a
  typed resource-management workflow.
```

Add `api` to the ksctl-owned root command list:

```markdown
- ksctl-owned `api`, `auth`, `config`, `extension`, `plugin`, `tenant`, and
  `version` commands;
```

Expected: the document no longer implies that every built-in command is
read-only or that Extension lifecycle is the only possible source of a write
request.

- [ ] **Step 3: Add the raw KubeSphere API request architecture**

Insert a new `## Raw KubeSphere API requests` section after
`### KubeSphere REST clients`. Cover all of these exact behaviors:

```text
ksctl api API_PATH [-X METHOD] [-d DATA]
```

- `API_PATH` is server-relative, starts with `/`, may contain a query, and
  rejects absolute URLs and fragments.
- The command reuses KubeSphere connection, credential, TLS, user-agent, and
  timeout resolution.
- The selected Cluster and a Context's `defaultCluster` do not rewrite the
  path; callers include `/clusters/<cluster>` explicitly when required.
- `GET` is the default; explicitly supplied data selects `POST` unless an
  explicit method wins.
- Supplied data is sent as raw bytes with `Content-Type: application/json`.
- Response bytes are copied unchanged to stdout.
- HTTP error responses write the received body and also return an error.
- The command performs no resource typing, lifecycle validation, response
  formatting, redaction, or write guard.

Expected: contributors can distinguish this raw transport path from the
kubectl Factory pipeline, native tenant requests, and controlled Extension
lifecycle services.

- [ ] **Step 4: Align routing, security, and validation descriptions**

Use `apply_patch` to add three narrowly scoped statements:

```markdown
The raw `api` command is excluded from automatic Cluster routing. It uses the
caller-provided path unchanged, so a Cluster-scoped request must contain its
own `/clusters/<cluster>` prefix.
```

```markdown
- Raw API requests may mutate server state, and their request or response
  bodies may contain sensitive data. ksctl does not inspect, redact, or apply
  Extension lifecycle safeguards to them.
```

```markdown
- raw API command tests verify path and method validation, body construction,
  unchanged query and response bytes, the absence of automatic Cluster
  routing, and HTTP-error body propagation;
```

Place them in `Cross-cluster routing`, `Security properties`, and
`Validation boundaries`, respectively.

- [ ] **Step 5: Review the English diff for unsupported expansion**

Run:

```bash
git diff -- docs/design.md
```

Expected: changes are limited to the raw API/read-only corrections described
above; existing accurate architecture sections are not rewritten.

- [ ] **Step 6: Check Markdown whitespace**

Run:

```bash
git diff --check -- docs/design.md
```

Expected: exit status 0 with no output.

- [ ] **Step 7: Commit the canonical English update**

```bash
git add docs/design.md
git commit -m "document raw API architecture"
```

Expected: one commit containing only `docs/design.md`.

---

### Task 2: Add the Simplified Chinese Mirror and Navigation

**Files:**

- Modify: `docs/design.md:1-8`
- Create: `docs/design_zh.md`
- Modify: `README.md:66-72`
- Reference: `docs/cli.md:1-5`
- Reference: `docs/cli_zh.md:1-5`

**Interfaces:**

- Consumes: the final canonical `docs/design.md` produced by Task 1.
- Produces: `docs/design_zh.md` with the same section structure and technical content, plus bidirectional language navigation and README entry points.

- [ ] **Step 1: Add bidirectional language navigation**

Use `apply_patch` to add immediately below each H1:

```markdown
**English** | [简体中文](design_zh.md)
```

and:

```markdown
[English](design.md) | **简体中文**
```

Expected: each design document identifies the current language and links to
the other using a relative path.

- [ ] **Step 2: Create the Chinese mirror in canonical section order**

Create `docs/design_zh.md` with `apply_patch`. Translate every prose paragraph,
heading, list label, and explanatory table cell from the final
`docs/design.md`. Preserve every fenced block byte-for-byte and keep all lists,
tables, and paragraphs in the same order.

Use these exact translations consistently:

```text
Goals -> 目标
Non-goals -> 非目标
Command architecture -> 命令架构
Resource command pipeline -> 资源命令管线
Native tenant pipeline -> 原生租户管线
Discovery compatibility -> 发现兼容性
Client boundaries -> 客户端边界
Shared options -> 共享选项
Kubernetes client adapter -> Kubernetes 客户端适配器
KubeSphere REST clients -> KubeSphere REST 客户端
Raw KubeSphere API requests -> 原始 KubeSphere API 请求
Extension management -> 扩展组件管理
Configuration model -> 配置模型
Authentication model -> 认证模型
Cross-cluster routing -> 跨集群路由
Generated kubeconfig -> 生成的 kubeconfig
Plugin model -> 插件模型
Security properties -> 安全属性
Compatibility -> 兼容性
Validation boundaries -> 验证边界
```

Keep `Context`, `Fleet`, `User`, `Workspace`, `Cluster`, `Namespace`,
`Endpoint`, `Token`, `RESTMapper`, `kubeconfig`, commands, flags, identifiers,
and paths in their original technical form. Translate `Extension` as
“扩展组件” in prose without changing API kinds or code spans.

Expected: the Chinese document contains all English qualifications, including
the raw API write and sensitive-data warnings.

- [ ] **Step 3: Point the Chinese introduction to the Chinese CLI guide**

Use this Chinese introduction, retaining the historical-specification
boundary:

```markdown
本文档描述 ksctl 的当前架构。命令语法和工作流请参阅
[CLI 指南](cli_zh.md)。`docs/superpowers/` 下的历史规格记录各项决策和实施
阶段，但它们不是当前架构参考。
```

Expected: English design links to `cli.md`; Chinese design links to
`cli_zh.md`.

- [ ] **Step 4: Expose both design documents from the README**

Replace the single Design entry in `README.md` with:

```markdown
- [Design (English)](docs/design.md) — architecture, client boundaries,
  routing, persistence, security properties, and compatibility.
- [设计文档（简体中文）](docs/design_zh.md) — ksctl 架构、客户端边界、路由、
  持久化、安全属性和兼容性。
```

Expected: the documentation index exposes both language variants without
adding a second README.

- [ ] **Step 5: Verify heading-level and fenced-code structural parity**

Run:

```bash
diff -u <(rg -o '^#{2,3}' docs/design.md) <(rg -o '^#{2,3}' docs/design_zh.md)
```

Expected: exit status 0 with no output.

Run:

```bash
diff -u <(awk '/^```/{inside=!inside; print; next} inside {print}' docs/design.md) <(awk '/^```/{inside=!inside; print; next} inside {print}' docs/design_zh.md)
```

Expected: exit status 0 with no output.

- [ ] **Step 6: Verify technical-token parity**

Run:

```bash
diff -u <(rg -o '`[^`]+`' docs/design.md | sort -u) <(rg -o '`[^`]+`' docs/design_zh.md | sort -u)
```

Expected: exit status 0 with no output. If translated prose requires an inline
code token already present in English, retain the exact English token rather
than adding or removing one.

- [ ] **Step 7: Verify local links and Markdown fences**

Run each command separately:

```bash
test -f docs/design.md
test -f docs/design_zh.md
test -f docs/cli.md
test -f docs/cli_zh.md
```

Expected: every command exits 0.

Run:

```bash
awk '/^```/{n++} END{exit n%2}' docs/design.md
```

Expected: exit status 0.

Run:

```bash
awk '/^```/{n++} END{exit n%2}' docs/design_zh.md
```

Expected: exit status 0.

- [ ] **Step 8: Review bilingual changes and whitespace**

Run:

```bash
git diff -- README.md docs/design.md docs/design_zh.md
```

Expected: only the language switchers, complete Chinese mirror, and bilingual
README entries are present in this task's diff.

Run:

```bash
git diff --check
```

Expected: exit status 0 with no output.

- [ ] **Step 9: Commit the Chinese mirror and navigation**

```bash
git add README.md docs/design.md docs/design_zh.md
git commit -m "add Chinese architecture documentation"
```

Expected: one commit containing the Chinese mirror and bilingual navigation.

---

### Task 3: Perform Final Documentation Verification

**Files:**

- Verify: `README.md`
- Verify: `docs/design.md`
- Verify: `docs/design_zh.md`
- Compare: `pkg/cmd/root.go`
- Compare: `pkg/cmd/api.go`
- Compare: `pkg/cmd/api_test.go`

**Interfaces:**

- Consumes: the committed English correction and Chinese mirror from Tasks 1
  and 2.
- Produces: final evidence that the implementation matches the approved
  specification without unrelated changes.

- [ ] **Step 1: Re-run all structural parity checks**

Run:

```bash
diff -u <(rg -o '^#{2,3}' docs/design.md) <(rg -o '^#{2,3}' docs/design_zh.md)
```

Run:

```bash
diff -u <(awk '/^```/{inside=!inside; print; next} inside {print}' docs/design.md) <(awk '/^```/{inside=!inside; print; next} inside {print}' docs/design_zh.md)
```

Run:

```bash
diff -u <(rg -o '`[^`]+`' docs/design.md | sort -u) <(rg -o '`[^`]+`' docs/design_zh.md | sort -u)
```

Expected: all three commands exit 0 with no output.

- [ ] **Step 2: Scan maintained documents for placeholders**

Run:

```bash
rg -n 'TODO|TBD|FIXME|XXX' README.md docs/design.md docs/design_zh.md
```

Expected: exit status 1 with no matches.

- [ ] **Step 3: Verify raw API claims against implementation**

Run:

```bash
rg -n 'api|API_PATH|RequestURI|ContentType|DoRaw|methodSet|dataSet|clusters/' docs/design.md docs/design_zh.md pkg/cmd/api.go pkg/cmd/api_test.go
```

Expected: both documents describe path validation, method/body behavior,
unchanged response output, and explicit Cluster path handling covered by the
implementation and tests.

- [ ] **Step 4: Verify the final changed-file scope**

Run:

```bash
git diff HEAD~2 --name-only
```

Expected:

```text
README.md
docs/design.md
docs/design_zh.md
```

The previously committed specification and implementation plan are planning
records and are not part of these two implementation commits.

- [ ] **Step 5: Run the final whitespace check**

Run:

```bash
git diff HEAD~2 --check
```

Expected: exit status 0 with no output.

- [ ] **Step 6: Confirm a clean worktree**

Run:

```bash
git status --short
```

Expected: no output.
