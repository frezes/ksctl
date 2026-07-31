# ksctl CLI 指南

[English](cli.md) | **简体中文**

## 简介

`ksctl` 是 KubeSphere 4.x 的命令行客户端。它连接到 KubeSphere API
端点，用于查看资源和管理 KubeSphere 扩展组件。`kube` 命令通过 KubeSphere
认证提供基本完整的 kubectl 操作能力，并支持通过 `--cluster` 跨 Cluster
路由；租户命令用于查看当前租户可访问的资源在 Workspace、Namespace 和
Cluster 中的分布。

顶层 `get` 和租户查看为只读。`kube` 下的命令以及 `install`、
`configure` 和 `uninstall` 等扩展组件生命周期命令可能更改服务器状态。

开始前，你需要：

- 可访问的 KubeSphere 4.x API 端点。
- KubeSphere 账户或持有者令牌。
- `ksctl` 可执行文件。

## 命令语法

命令通常采用以下形式：

```text
ksctl COMMAND [TYPE] [NAME] [flags]
```

内置帮助是已安装版本的完整参考：

```bash
ksctl help
ksctl COMMAND --help
```

## 命令分类

| 分类 | 用途 | 命令 | 可用性 |
| --- | --- | --- | --- |
| Kubernetes 操作 | 通过 KubeSphere 使用基本完整的 kubectl 操作能力。 | `kube` | 可用 |
| 租户管理 | 查看 Workspace 及其 Namespace 和 Cluster。 | `tenant` | 可用 |
| 扩展组件管理 | 查看、安装、配置、诊断和移除扩展组件。 | `extension` | 可用 |
| 应用管理 | 管理 KubeSphere 应用。 | — | 暂未提供 |
| 其他 | 认证、调用 API 和使用插件。 | `auth`、`config`、`api`、`plugin` | 可用 |

## Kubernetes 资源管理

顶层 `ksctl get` 是读取 Kubernetes 资源和已发现 KubeSphere 资源的简洁只读
入口。`ksctl kube` 使用 ksctl 的连接、认证、Context 和 Cluster 选择，并提供
基本完整的 kubectl 操作能力。

### 选择资源作用域

| 概念 | 含义 |
| --- | --- |
| Context | 选择 KubeSphere 连接和身份。 |
| Cluster | 选择目标 Cluster；可通过 `--cluster` 为单个命令指定。 |
| Namespace | 选择 Kubernetes Namespace 或 KubeSphere 项目。 |
| Workspace | 表示租户作用域，用于查看可访问的 Namespace 和 Cluster。 |

使用已保存的 Context，或为单次命令覆盖其作用域：

```bash
ksctl get deployments,pods -n demo --cluster member-1
ksctl kube apply -f app.yaml --cluster member-1
```

`kube` 不读取或写入 `~/.kube/config`；明确需要 kubeconfig 时，请使用
`ksctl config generate kubeconfig`。变更操作受解析出的 KubeSphere 凭据所具备
的 Kubernetes RBAC 权限约束，ksctl 不会额外要求确认。操作语法和行为请参考
`ksctl kube --help` 与上游 kubectl 文档。

## 租户管理

Workspace 将租户对 Namespace 和 Cluster 的访问进行分组。`tenant get` 从 KubeSphere 租户 API 读取这些关系，而不是通过 Kubernetes 资源发现读取。

| 命令 | 用途 |
| --- | --- |
| `ksctl tenant get workspace [NAME]` | 列出可访问的 Workspace 或显示一个 Workspace。 |
| `ksctl tenant get namespace` | 列出 Namespace，可按 Workspace 过滤。 |
| `ksctl tenant get cluster` | 列出 Cluster，可按 Workspace 过滤。 |

列出租户资源并按 Workspace 过滤：

```bash
ksctl tenant get workspace
ksctl tenant get workspace platform
ksctl tenant get ns --workspace platform
ksctl tenant get ns --workspace platform --cluster member-1
ksctl tenant get cluster --workspace platform
```

可接受的名称包括 `workspace`/`workspaces`、`namespace`/`namespaces`/`ns` 和 `cluster`/`clusters`。

`--workspace` 过滤 Namespace 和 Cluster 结果。`--cluster` 将 Namespace 请求路由到所选成员 Cluster，但不会更改 Workspace 或租户 Cluster 请求。输出可为 `table`、`json` 或 `yaml`。

## 扩展组件管理

扩展组件资源始终在 KubeSphere host Cluster 上管理。Context 的默认 Cluster
会被忽略，而显式传递全局 `--cluster` 参数会返回错误。扩展组件命令不定义资源
命令本地的 `--namespace` 参数。请使用扩展组件部署位置参数选择成员 Cluster，
或使用 `diagnose --target-cluster` 查看成员状态。

| 命令 | 用途 |
| --- | --- |
| `ksctl extension list` | 列出并过滤可用扩展组件。 |
| `ksctl extension show NAME` | 显示扩展组件或指定版本的详细信息。 |
| `ksctl extension versions NAME` | 列出可用版本。 |
| `ksctl extension status [NAME]` | 显示或持续观察安装状态。 |
| `ksctl extension install NAME` | 安装扩展组件。 |
| `ksctl extension upgrade NAME --version VERSION` | 升级到指定版本。 |
| `ksctl extension configure NAME` | 更新配置或部署位置。 |
| `ksctl extension uninstall NAME` | 移除扩展组件。 |
| `ksctl extension diagnose NAME` | 诊断扩展组件控制器状态。 |

### 查看扩展组件

查看目录、可用版本和安装状态：

```bash
ksctl extension list --installed
ksctl extension list --category observability
ksctl extension show logging
ksctl extension versions logging
```

在支持的地方可使用 `-o table`、`-o wide`、`-o json` 或 `-o yaml`。JSON 和 YAML 会保留完整的服务器对象。

### 管理生命周期

省略 `--version` 时，安装会使用扩展组件的推荐版本。升级始终需要指定确切版本：

```bash
ksctl extension install logging
ksctl extension install logging --version 1.2.1 --wait
ksctl extension upgrade logging --version 1.3.0 --wait
ksctl extension configure logging --config ./logging-values.yaml
ksctl extension uninstall logging --wait
```

生命周期请求默认异步执行，并会在 API 接受请求后返回。添加 `--wait` 可轮询完成状态。卸载会删除 InstallPlan，且不进行交互式确认。

配置输入必须是非空 YAML 映射。使用 `--config FILE`，或使用 `--config -` 从标准输入读取。

### 配置部署位置和诊断

选择成员 Cluster 并添加每个 Cluster 的覆盖项：

```bash
ksctl extension install logging --clusters member-a,member-b
ksctl extension configure logging --override member-a=./member-a.yaml
ksctl extension diagnose logging --target-cluster member-a
```

`--all-clusters` 解析当前符合条件的 Cluster 集合并保存该快照；它不是动态选择器。诊断会检查控制器状态，但不会检索日志、Secret 或渲染后的 Helm 值。

扩展组件命令请使用不高于 7 的详细级别。更高的 REST 调试详细级别可能会泄露扩展组件配置。

## 其他命令

### 认证并选择 Context

| 命令 | 用途 |
| --- | --- |
| `ksctl auth login [ENDPOINT]` | 登录并选择已保存的 Context。 |
| `ksctl auth whoami` | 验证凭据并显示服务器端身份。 |
| `ksctl auth logout [CONTEXT]` | 移除缓存的登录凭据。 |
| `ksctl config current-context` | 显示已选择的 Context。 |
| `ksctl config use-context NAME` | 选择其他 Context。 |
| `ksctl config view` | 显示合并后的已脱敏配置。 |

在终端中省略必需的登录值会启动引导流程：

```bash
ksctl auth login
```

Fleet 将 API 端点、TLS 设置和其用户分组。登录时，Fleet 名称默认为 API 端点主机名，Context 名称默认为 `<fleet>-<username>`。新的 Context 会成为当前 Context。

引导流程输入的密码不会被持久化。对于非交互式登录，请避免通过 shell 历史记录、日志或进程检查暴露 `--password`。`config view` 默认会脱敏凭据；不要共享 `config view --raw` 输出。登出会清除缓存的登录凭据，但不会删除 Context。

### 覆盖连接设置

无需创建 Context，即可使用 API 端点和持有者令牌：

```bash
ksctl get workspaces \
  --endpoint https://kubesphere.example.com \
  --token "$KS_TOKEN"
```

显式 `--endpoint` 或 `KS_ENDPOINT` 必须与显式 `--token` 或 `KS_TOKEN` 配对。ksctl 绝不会将被覆盖的 API 端点与所选 Context 的凭据组合使用。

### 生成 kubeconfig

为所选用户和 Cluster 生成 kubeconfig：

```bash
umask 077
ksctl config generate kubeconfig --cluster member-1 > member-1.kubeconfig
```

服务器响应会原样写入标准输出，且不会合并到 `~/.kube/config`。输出包含凭据，因此请以严格的权限保存。

### 调用 API 或使用插件

向相对于服务器的 KubeSphere API 路径发送已认证请求：

```bash
ksctl api /kapis/version
```

`api` 默认使用 GET。未指定 `--method` 而提供 `--data` 时，默认方法变为 POST，响应正文会原样写入。发送会更改状态的请求前，请运行 `ksctl api --help`。

`API_PATH` 会原样发送。`--cluster`、`--namespace` 和 Context 的默认 Cluster 都不会自动添加作用域。调用成员 Cluster 时，必须在 `API_PATH` 中包含 `/clusters/CLUSTER/...`。

名为 `ksctl-foo` 的可执行文件提供外部命令 `ksctl foo`：

```bash
ksctl plugin list
ksctl foo --context prod
```

插件参数必须位于插件名称之后。插件以你的用户权限运行，ksctl 不会审核或沙箱化插件；仅安装和运行你信任的插件。

## 全局选项和环境变量

| 参数 | 用途 |
| --- | --- |
| `--endpoint URL` | 覆盖 KubeSphere API 端点。 |
| `--token TOKEN` | 覆盖 KubeSphere 持有者令牌。 |
| `--context NAME` | 使用具名 ksctl Context。 |
| `--cluster NAME` | 选择 KubeSphere 成员 Cluster。 |
| `-v, --v LEVEL` | 设置日志详细级别。 |

顶层 `get` 在命令本地定义 `-n, --namespace NAME`。`ksctl kube` 持久定义
`-n, --namespace NAME` 和 `--request-timeout DURATION`（`0` 表示不限制），
并由其操作命令继承。这两个参数都不是根连接参数。

| 环境变量 | 用途 |
| --- | --- |
| `KSCTL_CONFIG` | 选择其他 ksctl 配置文件。 |
| `KS_ENDPOINT` | 提供默认 KubeSphere API 端点。 |
| `KS_TOKEN` | 提供默认 KubeSphere 持有者令牌。 |

显式参数优先于环境变量，环境变量优先于 Context 默认值（在支持该设置的位置）。子命令可定义额外参数，或为参数赋予特定于命令的含义。

## 常用工作流

### 租户视角

租户可发现其被分配的作用域，然后查看所选 Namespace 和 Cluster 中的工作负载：

```bash
ksctl auth login
ksctl auth whoami
ksctl tenant get workspace
ksctl tenant get ns --workspace demo
ksctl tenant get cluster --workspace demo
ksctl get deployments,pods -n demo --cluster member-1
```

### 管理员视角

管理员可确认当前环境、查看平台状态并管理扩展组件：

```bash
ksctl auth login
ksctl auth whoami
ksctl config current-context
ksctl config use-context prod-admin
ksctl get pods -A --cluster member-1
ksctl tenant get workspace
ksctl extension list --installed
ksctl extension install logging --version 1.2.1 --wait
ksctl extension diagnose logging
ksctl api /kapis/version
```

## 故障排查

- 运行 `ksctl help` 或 `ksctl COMMAND --help`，确认已安装版本的语法。
- 运行 `ksctl config current-context`，确认已选择的 Context。
- 运行 `ksctl config view`，查看已脱敏的连接和作用域设置。
- 未知资源类型通常表示所选服务器或 Cluster 未通过资源发现公布该类型。
- 要求登录的错误表示未找到适用于所选连接的可用凭据。
- 日常故障排查时避免使用 `ksctl config view --raw`，因为它可能泄露凭据和 TLS 私钥数据。
