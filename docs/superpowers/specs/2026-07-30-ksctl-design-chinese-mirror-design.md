# ksctl Design Document Chinese Mirror Design

## Goal

Bring the maintained architecture reference back in sync with the current
command surface and add a complete Simplified Chinese mirror without creating
a second information architecture.

## Scope

This change will:

- update `docs/design.md` where its architecture description differs from the
  current implementation;
- add `docs/design_zh.md` as a complete structural mirror of
  `docs/design.md`;
- add a language switcher near the title of both design documents; and
- update the `README.md` documentation section with separate English and
  Simplified Chinese design links.

The implementation review will use the current source and tests as the source
of truth. The known content gap is the root-level `api` command, which is
registered by the current command tree but is absent from the maintained
architecture reference.

This change will not:

- reorganize the design document around a new information architecture;
- change CLI behavior, Go code, dependencies, build configuration, or release
  packaging;
- translate historical specifications or plans under `docs/superpowers/`; or
- introduce architecture claims that are not supported by the current source
  or tests.

## English Design Updates

The English document remains the canonical architecture reference. Its
existing goals, non-goals, command pipeline, client boundaries, extension
management, configuration, authentication, routing, kubeconfig, plugin,
security, compatibility, and validation sections remain in their current
order unless a small local adjustment is required for accuracy.

The update will:

1. add the English/Chinese language switcher below the H1;
2. include `api` in the list of ksctl-owned root commands;
3. refine the read-only boundary so it applies to the kubectl-style generic
   resource commands rather than implying that every built-in command is
   read-only;
4. distinguish the purpose-built Extension lifecycle workflow from the raw
   `api` escape hatch, which may issue mutating requests under the user's
   authority;
5. add a focused section that explains the raw KubeSphere API request path;
6. record that `api` uses the selected KubeSphere connection and credential
   resolution but does not automatically apply selected Cluster scope;
7. explain that callers provide a server-relative path, including an explicit
   `/clusters/<cluster>` prefix when required;
8. describe byte-preserving response output and the behavior for HTTP error
   responses; and
9. add the raw API command's relevant disclosure and trust boundary to the
   existing security and validation descriptions.

Any other correction discovered during source comparison must be narrowly
scoped to a demonstrable mismatch. Editorial rewriting that does not improve
accuracy, navigation, or bilingual consistency is out of scope.

## Chinese Mirroring Contract

`docs/design_zh.md` will preserve the English document's:

- heading hierarchy and section order;
- paragraphs, lists, tables, and numbered-step order;
- architecture diagrams and text pipelines;
- commands, flags, environment variables, package names, types, field names,
  API paths, versions, file paths, and configuration keys;
- links to maintained repository documents; and
- technical constraints, warnings, and security strength.

Only prose, explanatory headings, list labels, and link labels are translated.
Code fences retain the same content and order. The Chinese copy should be
natural and concise rather than mechanically word-for-word, but it must not
omit qualifications or add behavior that is absent from the English source.

Use established project terminology consistently:

| English | Simplified Chinese |
| --- | --- |
| Context | Context |
| Fleet | Fleet |
| User | User |
| Workspace | Workspace |
| Cluster | Cluster |
| Namespace | Namespace |
| Extension | 扩展组件 |
| Endpoint | API 端点 |
| Token | 令牌 |
| discovery | 发现 |
| RESTMapper | RESTMapper |
| kubeconfig | kubeconfig |

Command names, flags, values, identifiers, and paths are never translated.

## Navigation

Immediately below each H1:

- `docs/design.md` shows **English** as the current language and links to
  `design_zh.md`;
- `docs/design_zh.md` links to `design.md` and shows **简体中文** as the current
  language.

The introductory link to the CLI guide follows the current document language:

- the English design links to `cli.md`;
- the Chinese design links to `cli_zh.md`.

The `README.md` documentation section exposes separate entries:

- Design (English) → `docs/design.md`;
- 设计文档（简体中文）→ `docs/design_zh.md`.

All navigation uses relative Markdown links so it works on GitHub and in local
rendering.

## Content Boundaries

The design documents remain contributor and maintainer references, not command
usage manuals. They explain ownership, data flow, routing, persistence,
security, and compatibility. Detailed command syntax and workflows remain in
the language-matched CLI guides.

The read-only guarantee applies to the kubectl-style `get`, `describe`, `logs`,
and `top` resource surface. Extension lifecycle commands remain constrained,
purpose-built write operations. The `api` command is a lower-level authenticated
transport escape hatch: the caller chooses the path, method, and optional body,
so ksctl does not promise that these requests are read-only or protect
resources with Extension-specific lifecycle checks.

Historical files under `docs/superpowers/` remain decision records. They may
be consulted while checking intent, but the current implementation and tests
determine what the maintained design documents claim.

## Validation

The documentation-only implementation will verify:

1. current root registration, `api` implementation, relevant client packages,
   and tests against every new English architecture claim;
2. identical English and Chinese H2/H3 structure and section order;
3. identical ordered fenced-code contents across both design documents;
4. presence of the same command names, flags, environment variables, API
   paths, package names, types, configuration keys, and versions where those
   tokens occur in the English source;
5. valid language switchers, language-matched CLI links, and README links;
6. paired Markdown fences and consistent heading hierarchy;
7. absence of `TODO`, `TBD`, placeholder prose, and unsupported claims;
8. `git diff --check`; and
9. a final diff containing only `README.md`, `docs/design.md`,
   `docs/design_zh.md`, and the approved specification and implementation plan.

Because the implementation changes only Markdown, the verification boundary
does not require the full Go test suite. Source and test inspection, structural
mirror checks, link checks, Markdown checks, and `git diff --check` are the
required evidence.
