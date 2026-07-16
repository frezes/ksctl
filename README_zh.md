# KubeSphere 命令行工具（ksctl）

[English](README.md) | 简体中文

`ksctl` 是用于操作 KubeSphere 4.x 资源以及通过 KubeSphere 暴露的
Kubernetes 资源的命令行工具。

当前命令主要用于查看资源。`get` 和 `describe` 命令复用了
[kubectl v0.36.2](https://kubernetes.io/zh-cn/docs/reference/kubectl/) 的资源发现、
REST 映射、输出、选择器、监听、内置 Describer、通用 Describe
回退和事件处理能力。

## 前提条件

- Go 1.26 或更高版本
- 可访问的 KubeSphere 4.x API 地址
- KubeSphere 账号或 Bearer Token

## 从源码构建

将 `ksctl` 构建到 `bin/ksctl`：

```bash
make build
```

检查构建后的二进制文件：

```bash
./bin/ksctl version
```

## 快速开始

登录 KubeSphere。密码只用于本次请求，不会写入配置文件。

```bash
export KS_PASSWORD='your-password'
./bin/ksctl auth login https://kubesphere.example.com \
  --username admin \
  --password "$KS_PASSWORD" \
  --context local
```

登录后新 Context 会被设为当前 Context，后续命令可以直接使用其中的 API
地址和缓存 Token：

```bash
./bin/ksctl get workspaces
./bin/ksctl get pods -A
```

## 命令语法

资源命令使用以下语法：

```text
ksctl [command] [TYPE] [NAME] [flags]
```

- `command` 是要执行的操作，例如 `get` 或 `describe`。
- `TYPE` 是服务端发现的资源类型。API 提供对应名称时，可以使用单数、复数或简称。
- `NAME` 是单个资源的名称；省略时对该类型的资源列表执行操作。
- `flags` 用于选择 Context 或作用域、过滤结果以及调整输出。

使用 `ksctl help`、`ksctl <command> --help` 或
`ksctl <command> <subcommand> --help` 查看具体命令的帮助信息。

## 命令

| 命令 | 说明 |
| --- | --- |
| `ksctl get TYPE [NAME]` | 显示一个或多个资源。 |
| `ksctl describe TYPE [NAME]` | 显示资源的详细状态及相关信息。 |
| `ksctl auth login ENDPOINT` | 使用用户名和密码认证，并保存 Context 和 Token 缓存。 |
| `ksctl auth logout [CONTEXT]` | 删除指定 Context 的缓存凭证。 |
| `ksctl config view` | 显示合并后的 ksctl 配置。 |
| `ksctl config current-context` | 显示当前 Context 名称。 |
| `ksctl config use-context NAME` | 选择已有 Context。 |
| `ksctl version` | 显示 ksctl、KubeSphere 和 Kubernetes 版本。 |

## 作用域和连接参数

通过作用域参数，可以使用相同的资源命令访问不同层级的 KubeSphere 和
Kubernetes 资源。

| 参数 | 说明 |
| --- | --- |
| `--context NAME` | 使用指定的 ksctl Context。 |
| `--cluster NAME` | 选择 KubeSphere 集群。 |
| `--workspace NAME` | 选择 KubeSphere Workspace。 |
| `-n, --namespace NAME` | 选择 Kubernetes Namespace 或 KubeSphere Project。 |
| `--endpoint URL` | 覆盖 KubeSphere API 地址。 |
| `--token TOKEN` | 覆盖 Bearer Token。 |
| `--request-timeout DURATION` | 设置单个服务端请求的超时时间。 |
| `--no-interactive` | 缺少输入时直接失败，不进行交互提示。 |
| `--insecure-skip-tls-verify` | 跳过服务端证书校验。 |

`KS_ENDPOINT` 和 `KS_TOKEN` 可分别提供 API 地址和 Token 的默认值。显式指定的
命令行参数优先级更高。

## 输出和过滤

`get` 默认输出服务端提供的表格。使用 `-o` 选择其他输出格式：

```bash
ksctl get pods
ksctl get pods -o wide
ksctl get pod web-0 -o yaml
ksctl get deployments -o json
ksctl get pod web-0 -o jsonpath='{.status.phase}'
```

使用与 kubectl 兼容的参数过滤、排序或监听资源：

```bash
ksctl get pods -l app=web
ksctl get pods --field-selector=status.phase=Running
ksctl get pods --sort-by=.metadata.name
ksctl get pods --watch
```

运行 `ksctl get --help` 查看所有支持的输出格式和筛选参数。

## 常用操作

查看 KubeSphere 资源：

```bash
ksctl get workspaces
ksctl describe workspace demo
ksctl get clusters
ksctl describe cluster member-1
```

通过 KubeSphere 查看 Kubernetes 资源：

```bash
ksctl get deployments,pods -n demo -l app=web -o wide
ksctl describe deployment web -n demo
ksctl get pods -A --cluster member-1
ksctl describe pod/web-0 -n demo --cluster member-1
```

不创建 Context，直接使用 API 地址和 Token：

```bash
ksctl get workspaces \
  --endpoint https://kubesphere.example.com \
  --token "$KS_TOKEN"
```

## 配置

ksctl 使用独立于 kubeconfig 的 `~/.ksctl/config.yaml`。设置 `KSCTL_CONFIG`
可以使用其他配置文件路径。

```yaml
apiVersion: ksctl.kubesphere.io/v1alpha1
kind: Config
currentContext: prod-admin
fleets:
  prod:
    host: https://prod.example.com
    users:
      admin:
        username: admin
        password: "<plaintext-password>"
  staging:
    host: https://staging.example.com
    users:
      admin:
        username: admin
        bearerToken: "<token>"
contexts:
  prod-admin:
    fleet: prod
    user: admin
    defaultCluster: ""
  staging-admin:
    fleet: staging
    user: admin
    defaultCluster: ""
```

新配置目录以 `0700` 权限创建，新配置文件以 `0600` 权限创建。省略 `username`
时，默认使用 User Map 中对应的 Key。User 归属于 Fleet，因此不同 Fleet 可以
同时包含名为 `admin` 的账户。User 可以配置 `bearerTokenFile`、`bearerToken`
或明文 `password`。其他可选空字段、空 User Map 及整个空 `tlsClientConfig`
块不会输出；`defaultCluster` 始终输出，默认值为空字符串。根级 `users` 不会被
读取或迁移。

使用配置命令查看或切换 Context：

```bash
ksctl config view
ksctl config current-context
ksctl config use-context prod-admin
```

## 认证

`ksctl auth login` 将非敏感的连接元数据写入配置文件，并将完整的 KubeSphere
`/oauth/token` 响应写入 `~/.ksctl/cache/tokens/<fleet>/<user>.json`。新 Token
缓存目录以 `0700` 权限创建，新缓存文件以 `0600` 权限创建。`auth login` 命令参数中的
密码不会落盘；用户显式写入 Config 的密码则以明文保留在 Config 文件中。

凭证按以下优先级解析：

```text
--token > KS_TOKEN > bearerTokenFile > bearerToken > token cache > password
```

配置了 Token File 或 Token 时会直接使用，并跳过缓存刷新和密码登录。Token File
读取失败、内容为空或 API 鉴权失败时直接返回错误，不尝试其他凭证。缓存 Access
Token 过期时会尽量使用 Refresh Token 自动刷新；缓存不可用且 Config 中配置了
密码时，只为当前命令请求 Access Token，不写入缓存。

`auth logout` 只删除登录缓存，不修改手工配置的凭证。引用同一 Fleet/User 的
多个 Context 共享 Token 缓存和退出状态。旧 Context 级缓存不会被读取或迁移。

登录时可通过 `--fleet` 指定 Fleet 名；省略时根据本次 Endpoint Host 生成。
省略 `--context` 时，Context 默认为 `<fleet>-<username>`，且不会从已有 Context
推断 Fleet：

```bash
ksctl auth login https://prod.example.com \
  --fleet prod --username admin --password '<password>'
```

删除当前或指定 Context 的登录缓存：

```bash
ksctl auth logout
ksctl auth logout local
```

## 开发

Makefile 提供三个开发目标：

```bash
make build
make test
make clean
```

- `build` 创建 `bin/ksctl`。
- `test` 运行全部 Go 测试。
- `clean` 删除 `bin/ksctl`。
