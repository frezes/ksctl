# ksctl Developer Design Documentation Design

## Goal

Replace the implementation-heavy architecture reference with a concise
developer design document that explains ksctl's essential responsibilities,
scope rules, request flows, and safety boundaries. Maintain a complete
Simplified Chinese mirror of the English source.

## Audience and Content Level

The maintained design documents are for developers who need to understand why
ksctl behaves as it does and where each responsibility belongs. They are not
source-code indexes, API references, command manuals, or test inventories.

Both documents will:

- explain concepts and data flow in prose;
- retain only the command names, domain objects, and routing semantics needed
  to make a design boundary precise;
- prefer responsibility and behavior over implementation mechanism; and
- link to the language-matched CLI guide for syntax and workflows.

Both documents will remove:

- fenced code blocks and text diagrams;
- package and source-file paths;
- Go interfaces, constructors, adapters, private types, and method names;
- configuration field trees and exact test coverage lists;
- detailed request-path examples that are not required to understand scope;
  and
- historical implementation narrative that does not affect the current
  design.

## Information Architecture

The English and Chinese documents use the same compact hierarchy:

1. **Goals and boundaries**
2. **Architecture overview**
3. **Core design**
   - Cross-Cluster resource access
   - Tenant pipeline
   - Extension management
   - Raw API requests
   - Authentication and configuration
4. **Security and compatibility**

The five capability-specific topics are H3 subsections under one H2
`Core design` section. This keeps the document readable as one system design
instead of presenting each capability as an independent implementation.

## Section Responsibilities

### Goals and boundaries

This section explains:

- ksctl is one CLI for KubeSphere and Kubernetes resource inspection;
- the kubectl-backed resource surface is read-only;
- Extension lifecycle commands are purpose-built, controlled write workflows;
- `api` is a raw authenticated escape hatch whose requests may mutate server
  state; and
- plugins run outside the built-in safety model.

Non-goals are limited to the boundaries developers need when extending the
system: no generic typed mutation surface, no kubeconfig persistence model, no
cross-Cluster aggregation, no plugin sandbox, and no KubeSphere 3.x support.

### Architecture overview

This section describes three conceptual layers without naming source packages
or interfaces:

1. the command layer captures user intent and explicit scope;
2. connection and authentication resolution produce one effective server,
   identity, and Cluster selection; and
3. KubeSphere serves native APIs or proxies Kubernetes requests to the selected
   Cluster.

It also explains that command construction is independent of proxy topology
and that each command invocation resolves and reuses one effective connection.

### Core design: Cross-Cluster resource access

This subsection explains:

- Context provides the default Fleet, User, and optional Cluster;
- explicit scope overrides Context defaults for one invocation;
- Kubernetes resource discovery, reads, logs, and metrics use the same selected
  Cluster route;
- namespace selection narrows namespaced requests but does not select a
  Cluster;
- compatibility discovery may recover server capabilities when aggregate
  discovery is incomplete; and
- resource commands query one Cluster and never aggregate Fleet members.

The explanation stays conceptual. It does not show request paths, client
interfaces, dependency versions, or kubectl constructor details.

### Core design: Tenant pipeline

This subsection distinguishes tenant scope from generic Kubernetes discovery:

- Workspace and tenant Cluster reads are Fleet-scoped;
- Namespace reads use the selected Cluster;
- an optional Workspace narrows Namespace and tenant Cluster collections; and
- tenant responses retain stable table output while structured output
  preserves the server response.

It does not identify source packages, wire types, or endpoint paths.

### Core design: Extension management

This subsection explains:

- Extension catalog and InstallPlan state belong to the host KubeSphere control
  plane;
- placement selects an explicit eligible set of member Clusters;
- install, upgrade, configure, uninstall, wait, and diagnose are controlled
  lifecycle operations rather than generic mutation verbs;
- write operations guard accepted intent against conflicting or stale state;
- asynchronous targets advance independently and waiting must not mistake old
  status for the new operation; and
- diagnosis summarizes controller, dependency, workload, and member status
  without retrieving secrets or application logs.

Detailed resource schemas, merge algorithms, object paths, field names, and
diagnostic implementation rules are omitted.

### Core design: Raw API requests

This subsection explains:

- `api` reuses normal connection, credential, TLS, timeout, and user-agent
  resolution;
- the caller owns the server-relative path, method, and optional body;
- selected Cluster scope is not added automatically, so Cluster routing must be
  explicit in the caller's path;
- response bytes are passed through unchanged and HTTP error bodies remain
  visible while the command returns a failure; and
- ksctl does not type, validate, redact, or protect raw API operations with
  Extension lifecycle safeguards.

The document does not show command examples, request paths, flags, or source
implementation.

### Core design: Authentication and configuration

This subsection explains:

- Fleet owns a KubeSphere Endpoint, TLS settings, and Fleet-scoped Users;
- Context selects one Fleet and User and may provide a default Cluster;
- explicit flags and environment values override configured connection state;
- explicit Endpoint overrides require an explicit Token and cannot borrow
  Context credentials;
- authoritative configured Token sources fail closed instead of silently
  falling through;
- valid cached Tokens are reused, refresh is attempted when possible, and a
  configured Password is the final command-local fallback;
- login authenticates, updates Config and Token cache atomically, and never
  persists the supplied Password; and
- logout makes a best-effort remote revocation, deletes the selected local
  cache, and preserves Config.

Exact precedence expressions, configuration schemas, cache paths, field names,
and OAuth endpoint paths are omitted.

### Security and compatibility

This section consolidates the guarantees and trust boundaries developers must
preserve:

- password input is not echoed or persisted by login;
- Config and Token caches use restricted, atomic writes;
- sensitive output remains the caller's responsibility;
- raw API requests and plugins may operate outside built-in read-only and
  lifecycle safeguards;
- KubeSphere 4.x is supported; and
- aligned Kubernetes dependencies must move together when upgraded.

It does not enumerate individual tests or restate build instructions.

## Bilingual Mirroring Contract

`docs/design.md` remains the canonical English source.
`docs/design_zh.md` is its complete Simplified Chinese mirror.

The mirror preserves:

- H2/H3 hierarchy and section order;
- paragraph, bullet, and numbered-list order;
- command names and domain identifiers;
- routing, failure, and security qualifications; and
- language-matched links to the CLI guides.

The Chinese prose should be concise and natural. Technical concepts such as
Context, Fleet, User, Workspace, Cluster, Namespace, Endpoint, Token,
InstallPlan, and kubeconfig remain recognizable and consistent. Command names
and identifiers are not translated.

Neither maintained design document contains fenced code blocks.

## Navigation

Both design documents retain the language switcher below the title. The
English introduction links to `cli.md`; the Chinese introduction links to
`cli_zh.md`. `README.md` continues to expose separate English and Simplified
Chinese design links.

## Validation

The rewritten documents will verify:

1. identical English and Chinese H2/H3 level sequences;
2. identical paragraph-block, bullet, and numbered-list counts;
3. identical inline technical-token sets where code spans are retained;
4. zero fenced code blocks in either design document;
5. no package paths, source-file paths, Go interface names, or test inventory;
6. valid language switchers, CLI-guide links, and README links;
7. no placeholders or unsupported architecture claims;
8. `git diff --check`; and
9. a final implementation diff limited to `README.md`, `docs/design.md`, and
   `docs/design_zh.md`, plus the updated approved specification and plan.

The change is Markdown-only. Source and test inspection support factual
review, but the full Go test suite is not required to validate the rewrite.
