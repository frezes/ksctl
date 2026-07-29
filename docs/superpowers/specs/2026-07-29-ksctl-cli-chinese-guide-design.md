# ksctl Chinese CLI Guide Design

## Goal

Add a Simplified Chinese version of the concise ksctl CLI guide without
creating a second information architecture. Readers should be able to switch
languages from either guide or discover both versions from `README.md`.

## Scope

This change will:

- add `docs/cli_zh.md` as a structural mirror of `docs/cli.md`;
- add a language switcher near the title of both guides; and
- update the `README.md` documentation section with separate English and
  Simplified Chinese links.

It will not:

- reorganize or expand the English guide;
- introduce content that exists in only one language;
- combine both languages in one file; or
- document the `completion` or `version` commands.

## Mirroring Contract

The Chinese guide will preserve the English guide's:

- heading hierarchy and section order;
- command classification and availability;
- command tables and row order;
- shell commands, arguments, flags, resource names, paths, and environment
  variables;
- warnings and behavioral constraints; and
- tenant and administrator workflows.

Only prose, table descriptions, labels, and explanatory headings are
translated. Code fences remain byte-for-byte equivalent except for surrounding
descriptive text.

Cluster management and application management remain visible only in the
classification table as unavailable capabilities. Neither guide gains an
empty detail section for them.

## Navigation

Immediately below each H1:

- `docs/cli.md` shows **English** as the current language and links to
  `cli_zh.md`;
- `docs/cli_zh.md` links to `cli.md` and shows **简体中文** as the current
  language.

The navigation uses relative links so it works both on GitHub and in local
Markdown rendering.

The `README.md` documentation section will expose two separate entries:

- CLI guide (English) → `docs/cli.md`;
- CLI 指南（简体中文）→ `docs/cli_zh.md`.

## Terminology

CLI tokens and KubeSphere resource concepts stay visually recognizable across
both guides. Use the following translations consistently:

| English | Simplified Chinese |
| --- | --- |
| Context | Context |
| Fleet | Fleet |
| Workspace | Workspace |
| Cluster | Cluster |
| Namespace | Namespace |
| Extension | 扩展组件 |
| Endpoint | API 端点 |
| Token | 令牌 |
| Kubernetes resource management | Kubernetes 资源管理 |
| Cluster management | 集群管理 |
| Tenant management | 租户管理 |
| Extension management | 扩展组件管理 |
| Application management | 应用管理 |
| Other | 其他 |
| Available | 可用 |
| Not yet available | 暂未提供 |

Command names, flags, values, file paths, API paths, output formats, and
environment variables are never translated.

## Writing Style

The Chinese copy should be concise and natural rather than mechanically
word-for-word. It should preserve the English sentence's meaning and safety
level while avoiding additional background material for concepts already
familiar to kubectl users.

Use Chinese punctuation in prose. Keep Markdown code spans around CLI tokens
and product concepts where the English guide uses them.

## Verification

The completed change will verify:

- both language switchers link to existing files;
- the Chinese H2 and H3 counts match the English guide;
- the ordered fenced code blocks contain identical command content;
- the same command names, flags, environment variables, and API paths appear
  in both guides;
- the two unavailable groups appear only in the classification tables;
- neither guide documents `ksctl completion` or `ksctl version`;
- `README.md` links to both guides;
- Markdown fences are paired and `git diff --check` succeeds; and
- the Git diff contains only `docs/cli.md`, `docs/cli_zh.md`, and `README.md`
  apart from this approved design record.
