# ksctl 设计

[English](design.md) | **简体中文**

本文档描述 ksctl 的当前架构。命令语法和工作流请参阅
[CLI 指南](cli_zh.md)。`docs/superpowers/` 下的历史规格记录各项决策和实施
阶段，但它们不是当前架构参考。

## 目标

- 提供一个 CLI，用于查看 KubeSphere 4.x 资源，以及通过 KubeSphere 暴露的
  Kubernetes 资源、日志和当前指标。
- 保留用户熟悉的 kubectl `get`、`describe`、`logs` 和 `top` 语法，以及
  发现、选择、打印、流式传输和错误行为。
- 保持由 kubectl 支持的通用资源界面只读。专用的扩展组件生命周期命令提供受控
  写操作，而 `api` 是一个显式的底层逃生口，用于发送由调用者选择的 KubeSphere
  API 请求。
- 支持交互式使用，以及显式、可预测的自动化。
- 使用 KubeSphere API 端点和凭据，而不读取或更改用户的 kubeconfig。
- 将配置、认证、API 客户端和命令装配保留在职责集中且边界可测试的包中。

## 非目标

- 为通用资源提供带类型的内置 create、update、edit、patch、delete、apply
  或其他变更命令。原始 `api` 传输逃生口不是带类型的资源管理工作流。
- 在本地重新实现或分叉 kubectl 的资源解析、打印器、watcher 或 Describers
- 读取、合并或写入 `~/.kube/config`
- 维护 KubeSphere 服务器可能暴露的资源的静态公共注册表
- 支持 KubeSphere 日志或监控扩展 API、历史可观测性查询，或跨 Cluster 的
  日志/指标聚合
- 审计可执行插件或为其提供沙箱
- 兼容 KubeSphere 3.x

可执行插件位于内置命令界面之外，可能实现非只读操作。它们在用户授权下作为
独立程序运行。

## 命令架构

可执行文件入口有意保持精简：

```text
cmd/ksctl/main.go -> NewRootCommandWithArgs
```

构造函数委托给 `pkg/cmd` 中共享的根命令构建器。

根命令拥有 KubeSphere 连接标志，并构造以下命令组：

- ksctl 拥有的 `api`、`auth`、`config`、`extension`、`plugin`、`tenant`
  和 `version` 命令；
- kubectl 拥有的 `get`、`describe`、`logs` 和 `top` 命令；以及
- Cobra 提供的帮助、补全和 shell 补全命令。

命令使用注入的输入、输出和错误流。可执行文件入口将这些流连接到进程标准流，
并在以非零状态退出前只打印一次返回的错误。根命令启用 `SilenceUsage` 和
`SilenceErrors`，因此执行失败时不会同时打印用法或重复打印错误。

在 Cobra 执行未知命令前，`WithArgs` 构造函数会让插件分派器尝试将其解析为
`ksctl-*` 可执行文件。

## 资源命令管线

ksctl 在 `pkg/cmd/resource_commands.go` 中构造 kubectl v0.36.2 的命令：

```text
Cobra command
  -> kubectl get, describe, logs, or top command/options
  -> kubectl Factory
  -> ksctl Kubernetes RESTClientGetter
  -> discovery and RESTMapper where required
  -> Kubernetes REST, Core, or Metrics client
  -> KubeSphere Endpoint
```

`get`、`describe` 和 `logs` 直接使用其上游构造函数。`top` 使用上游父命令、
子命令、选项、验证、客户端和打印器类型，并带有一个私有装配适配器：在上游
`Complete` 之后，ksctl 将 top 的原始 Clientset DiscoveryClient 替换为
`Factory.ToDiscoveryClient`。这在不分叉 top 行为的情况下保留了 ksctl 的
KubeSphere 发现回退机制。

递归示例规范化器会将上游 `kubectl` 示例改为当前命令的显示名称。

因此，资源参数、选择器、文件名输入、分页、watch、表格协商、打印器、内置
Describers、通用 describe 回退、Events、从工作负载解析 Pod 日志、日志流、
Metrics 客户端、资源计算和大多数错误行为都委托给固定版本的 kubectl 实现。
这也意味着，只有在有意升级相互对齐的 Kubernetes 依赖时，命令特定能力才会
随之演进。

### 原生租户管线

`pkg/cmd/tenant` 包独立于 kubectl 的发现驱动资源命令实现 `tenant get`。它使用
KubeSphere REST 客户端调用 `/kapis/tenant.kubesphere.io/v1beta1` 上的 KSE
租户 API，解码返回的对象或列表封套，并渲染默认的 kubectl 风格表格；对于
JSON 和 YAML 输出，则保留响应封套。

Workspace 和租户 Cluster 请求以 Fleet 为作用域，并忽略解析出的 Cluster。
Namespace 请求通过 KubeSphere REST 客户端的 Cluster 路由应用显式选择或
Context 默认的 Cluster，该路由会添加 `/clusters/<cluster>` 前缀。可选的
Workspace 会向 Namespace 和租户 Cluster 集合路由添加
`/workspaces/<workspace>`。

Factory 使用 `pkg/client/kubernetes.RESTClientGetter`，后者实现四个
cli-runtime 客户端接口：

```text
ToRESTConfig
ToDiscoveryClient
ToRESTMapper
ToRawKubeConfigLoader
```

连接解析是惰性的，因为 Cobra 在构造命令树之后才解析标志。每个 getter 在一次
命令调用的生命周期内缓存一个已解析连接、发现客户端和 RESTMapper。

### 发现兼容性

正常路径使用实时 KubeSphere 发现和内存发现缓存。延迟 RESTMapper 和快捷方式
扩展器据此派生 API 映射、单数名和复数名、短名称、组、版本及作用域。

某些 KubeSphere 部署会暴露各个发现端点，但聚合 API 组请求失败。回退发现客户
端通过探测从以下来源派生的候选组版本来处理这种情况：

- Kubernetes 客户端 scheme；
- 服务器返回的 CustomResourceDefinitions；以及
- Kubernetes 和 KubeSphere APIService 注册信息。

它只根据实际响应的组版本构造聚合发现视图。当 Cluster 作用域的 core-v1 发现
请求失败时，客户端可能通过无作用域的 KubeSphere API 端点查询该发现数据，
同时资源请求仍保持 Cluster 作用域。这是一条兼容路径，而不是面向用户的
KubeSphere 资源硬编码列表。

私有 top 适配器使用同一缓存发现界面。这一点很重要，因为 kubectl v0.36.2
在其他情况下会从新建的 Clientset 获取 top 发现信息，从而绕过
`RESTClientGetter.ToDiscoveryClient`。因此，当聚合发现根不可用时，可以通过
APIService 注册信息和具体的 `metrics.k8s.io/v1beta1` 端点恢复 Metrics API
发现。

## 客户端边界

### 共享选项

`pkg/client.Options` 是命令到客户端的输入模型。它携带 Endpoint、Token、
Context、Cluster、Namespace、请求超时、配置路径、用户代理和 TLS 验证设置。
Cobra 将公开的根标志绑定到两个客户端 getter 共享的一个 Options 值。交互式
输入不是客户端职责；它由 `auth login` 命令及其终端提示器负责。

### Kubernetes 客户端适配器

`pkg/client/kubernetes` 将已解析的 ksctl 状态适配到 Kubernetes `client-go`
和 `cli-runtime` 接口。它负责：

- 解析 Kubernetes 请求使用的 Token 和 TLS；
- 构造成员 Cluster API 端点；
- 解析请求超时；
- 提供 kubectl 处理 Namespace 所需的内存客户端配置；
- 提供带兼容性回退的缓存发现；以及
- 提供延迟发现 RESTMapper。

内存客户端配置仅包含 cli-runtime 所需的有效服务器、Token、TLS 设置、
Context 名称和 Namespace。它永远不会被写成 kubeconfig。

### KubeSphere REST 客户端

`pkg/client/kubesphere` 创建无版本 KubeSphere REST 客户端。它用于 OAuth、
版本查询和 kubeconfig 获取等 KubeSphere 原生操作，可以使用生成的 HTTP
客户端，也可以使用测试注入的客户端。

`pkg/client/kubesphere/connection` 解析 KubeSphere 原生 REST 配置、选中的
Cluster 和选中的用户名。与 Kubernetes getter 不同，它的基础 REST 配置保留
无作用域的 Fleet API 端点；当 API 支持时，KubeSphere 原生请求会显式添加
Cluster 作用域。

`internal/kubesphererest` 是从 ksctl TLS 配置到
`kubesphere.io/client-go/rest.TLSClientConfig` 的唯一适配器。OAuth 和
KubeSphere 连接 getter 都使用它。Kubernetes 适配器保持独立，因为它面向
`k8s.io/client-go/rest` 类型。

## 原始 KubeSphere API 请求

`api` 命令是一个底层的认证传输逃生口：

```text
ksctl api API_PATH [-X METHOD] [-d DATA]
```

`API_PATH` 必须是服务器相对路径，并以 `/` 开头。它可以包含查询字符串，但绝对
URL 和片段会被拒绝。该命令复用 ksctl 的 KubeSphere 连接、凭据、TLS、用户
代理和超时解析。

选中的 Cluster 和 Context 的 `defaultCluster` 不会改写请求路径。调用者若要
访问 Cluster 作用域的端点，必须在 `API_PATH` 中包含完整的
`/clusters/<cluster>` 前缀。

默认方法是 `GET`。在没有显式方法时提供 `--data` 会选择 `POST`；显式
`--method` 始终优先。提供的数据以原始字节发送，并带有
`Content-Type: application/json`。

响应正文原样复制到 stdout。对于 HTTP 错误响应，该命令既会写出收到的正文，
也会返回错误。它不执行资源类型识别、生命周期验证、响应格式化、脱敏或写操作
保护。因此，调用者需对所选路径、方法、请求正文和响应目标的影响及泄露风险
负责。

## 扩展组件管理

扩展组件管理是与除此之外只读的 kubectl 支持资源界面并列的、有意设计的专用
写入工作流。它仅拥有 `kubesphere.io/v1alpha1` Extension、ExtensionVersion
和 InstallPlan 工作流；不暴露通用变更动词。

职责在一条窄边界两侧拆分：

```text
pkg/cmd/extension
  Cobra flags, local file/stdin input, stable tables, lifecycle messages
  and command-specific scope rejection

internal/extension
  private wire models, host REST paths, catalog joins, dependency validation,
  guarded lifecycle writes, stale-safe waiting, and diagnosis
```

wire 模型有意保持私有，并保留每个完整的原始 JSON 文档。表格输出读取已知字段，
而 JSON 和 YAML 输出保留未知服务器字段，以实现向前兼容。

所有扩展组件目录和 InstallPlan 请求都使用主 KubeSphere API 端点。专用连接
getter 忽略 Context 的 `defaultCluster`；全局 `--cluster` 标志在连接解析前
被拒绝。扩展组件命令不定义资源命令本地的 `--namespace` 标志。多集群放置通过
InstallPlan 中的 `--clusters` 和覆盖项表达。`diagnose --target-cluster`
从主 InstallPlan 中选择成员状态，但所引用的 Namespace、Job 和 Pod 仍通过
主端的 `/api/v1` 和 `/apis/batch/v1` 路径读取。

安装操作创建启用的 InstallPlan，并设置 `upgradeStrategy: Manual`。升级和配置
操作发送由当前 `metadata.resourceVersion` 保护的最小 JSON Merge Patch；发生
冲突时直接返回错误，而不是重试已变化的意图。精确的 ExtensionVersion 值保持
不透明，但面向控制器的操作直接要求资源标识
`<extension>-<version>`。必需依赖只做验证，不会自动安装。

省略 `--version` 时，安装使用 Extension 当前的
`status.recommendedVersion`；升级仍要求显式提供精确版本。对于多集群安装，
`--all-clusters` 会列出主端 `Clusters`，选择当前就绪且可调度的集合，并将解析
出的合格快照写为显式放置，而不是动态选择器。当主 Cluster 满足相同条件时，
快照也会包含它；KubeSphere 控制器仍负责主端处理。

除非显式使用 `--wait`，生命周期写操作都是异步的。等待器使用已接受的创建或
补丁响应作为目标本地基线，因此不会把预先存在的陈旧终止状态归因于新操作。
主目标和成员目标独立推进；有效配置哈希与 KubeSphere 控制器的合并语义一致，
已移除成员的失败仍可采取行动。清除调度时使用显式空放置，以便控制器执行成员
清理。只有 InstallPlan 变为 NotFound 时，卸载才成功。已接受的规格和对象标识
均设有围栏，因此被丢弃的准入变更、并发修改、删除或同名重建都不会被报告为
原操作成功。

诊断会验证控制器的精确目标 ExtensionVersion，并报告控制器状态、条件、依赖、
Namespace、Job、Pod 终止情况、成员状态和有限的时间戳证据。已完成的重试 Job
和已恢复的容器历史会与当前失败区分开。默认情况下，完整健康结果是一行简洁的
健康信息；其他结果仅显示警告和错误检查以及状态计数，`--verbose` 则显示每个
已完成检查。诊断被中断时，会先报告已完成检查和未完成标记，再返回服务错误。
它会建议后续 `kubectl logs` 命令，但不会获取日志、Secret 或 Helm 值。

人类可读的扩展组件输出会转义终端控制数据。扩展组件命令还会拒绝 `--v=8` 及
更高的 REST 调试详细度，因为该客户端级别可能记录 InstallPlan 配置正文。

由于 `extension` 是内置命令，名为 `ksctl-extension` 或嵌套在该路径下的插件
可执行文件无法覆盖或扩展它。插件列表会将这些可执行文件报告为内置命令冲突。

## 配置模型

ksctl 将其状态存储在 `~/.ksctl/config.yaml`，或 `KSCTL_CONFIG` 选择的路径中。
它不使用 kubeconfig 作为持久化模型。

```text
Config
  currentContext -> Context name
  Fleets
    Fleet
      host
      TLS client settings
      Users
        User
          username
          bearerTokenFile | bearerToken | password
  Contexts
    Context
      Fleet reference
      User reference
      defaultCluster
```

Fleet 拥有 API 端点、TLS 客户端配置及其 Users。User 以 Fleet 为作用域，因此
同一个 User map key 可以存在于多个 Fleet 中。如果 User 的 `username` 为空，
则 User map key 成为 KubeSphere 用户名。

Context 选择一个 Fleet 和 User，并可设置默认 Cluster。当前 Context 是默认
选择。显式 `--context` 为一次调用选择另一个 Context；显式 `--cluster` 覆盖
其 `defaultCluster`。

缺失和空文件会加载为已初始化的空 Config。加载时会补齐缺失的 API version、
kind、Fleet map 和 Context map 默认值。非空文件进行严格解码：未知字段、重复
字段、类型不匹配、旧版根级 Users 或 Cluster 模型、不支持的 API version 和
不支持的 kind 都会报错。保存使用相同的类型契约验证，并拒绝改写二进制无法
理解的 Config version 或 kind。

## 认证模型

认证划分为三项职责：

1. `pkg/auth.Resolve` 将标志、环境变量、当前 Context、Fleet、User、默认
   Cluster 和 TLS 设置合并为连接身份。
2. `pkg/auth.Provider` 为该身份选择或获取 bearer Token。
3. `pkg/auth.OAuth` 针对 `/oauth/token` 执行密码授权和刷新授权。

API 端点选择顺序为：

```text
--endpoint > KS_ENDPOINT > selected Fleet host
```

由 `--endpoint` 或 `KS_ENDPOINT` 选择的 API 端点必须与由 `--token` 或
`KS_TOKEN` 选择的 Token 配对。配对不完整时，解析会在查询 Context 凭据前
失败。仅提供 `KS_TOKEN` 仍然有效：选中的 Fleet 提供其 API 端点和 TLS 设置。

凭据选择顺序为：

```text
--token > KS_TOKEN > bearerTokenFile > bearerToken > token cache > password
```

显式标志或环境变量 Token 会直接返回，而不读取已配置或缓存的凭据。已配置的
Token File 或 Token 同样具有权威性；文件读取错误或空文件会直接返回错误，
而不是继续回退。

Token 缓存以 Fleet 和 User 为键，位于
`~/.ksctl/cache/tokens/<fleet>/<user>.json`。有效的 Access Token 会被复用。
带 Refresh Token 的过期条目会尝试刷新，并在成功时原子替换缓存。如果无法刷新
或刷新失败，已配置的 Password 可以为当前命令获取 Access Token，但该响应不会
缓存。格式错误或因其他原因不可读的缓存数据会报错，而不是被忽略。

`auth login` 是显式创建缓存的路径。一个 Fleet 名称拥有一个规范化的 API 端点。
认证前，login 加载 Config；若现有 Fleet 的非空 Host 与登录 API 端点不同，则
拒绝登录。这可以防止同一个 Fleet/User 缓存坐标保存不同服务器的凭据。相同
API 端点的登录会合并 Fleet 现有的 TLS 设置、Users 和手动配置的凭据。

密码授权完成后，login 在内存中构建 Config 更新，捕获原 Fleet/User 缓存的精确
字节，原子保存新缓存，然后原子保存 Config。如果 Config 保存失败，幂等回滚会
恢复之前的缓存字节（包括格式错误的数据），或删除本次登录创建的缓存条目。
原始保存错误会与任何回滚错误合并。只有两次写入都完成后才打印成功信息。提供的
Password 永远不会持久化。

`auth logout` 请求 KubeSphere 撤销缓存的 Access Token，无论远程结果如何都会
删除 Fleet/User 缓存，并保留配置。

`auth whoami` 由服务器支持。它解析选中 Context 的 User 名称，构建经过认证的
Fleet 级 KubeSphere REST 客户端，并读取：

```text
/kapis/iam.kubesphere.io/v1beta1/users/<username>
```

该命令打印返回的 `metadata.name` 和
`metadata.annotations["iam.kubesphere.io/globalrole"]`。该请求验证解析出的
凭据能否访问选中的 User 资源，但此端点不是 OAuth Token 主体内省 API。
`auth logout` 读取缓存的 Fleet/User Access Token，并尽力向
`<fleet-endpoint>/oauth/logout` 发起无作用域请求。它忽略远程错误，并始终尝试
删除本地 Fleet/User 缓存。它不会解析或撤销已配置的静态凭据、执行刷新，或应用
成员 Cluster 路由。

## 跨集群路由

选中的 Cluster 会改变请求路由，但不会改变资源命令语法。

对于标准 Kubernetes 发现和资源请求，Kubernetes getter 构造的有效服务器为：

```text
<fleet-endpoint>/clusters/<cluster>
```

未选择 Cluster 时，它直接使用 Fleet API 端点。随后 kubectl 将其标准 `/api`
和 `/apis` 路径发送到该有效服务器。Cobra 命令不检查资源类型，也不改写单个
资源路径。

KubeSphere 原生 API 继续使用 `/kapis/...` 路径。接受 Cluster 作用域的原生
操作会通过 KubeSphere 请求构建器添加该作用域。例如，kubeconfig 获取使用基础
KubeSphere API 端点，并在调用用户 kubeconfig API 前将选中的 Cluster 应用于
请求。

原始 `api` 命令不使用自动 Cluster 路由。它原样使用调用者提供的路径，因此
Cluster 作用域请求必须自行包含 `/clusters/<cluster>` 前缀。

这条边界使命令解析不依赖 KubeSphere 代理拓扑：客户端层选择有效路由，而服务器
仍负责认证 KubeSphere Token，并提供目标 API 或将请求代理到目标 API。

Pod 日志子资源、Metrics API 和辅助 Core API 请求使用同一个有效服务器。例如：

```text
/clusters/<cluster>/api/v1/namespaces/<namespace>/pods/<pod>/log
/clusters/<cluster>/apis/metrics.k8s.io/v1beta1/namespaces/<namespace>/pods
/clusters/<cluster>/apis/metrics.k8s.io/v1beta1/nodes
```

这些命令查询一个选中的 Cluster，不会跨 Fleet 成员聚合。

## 生成的 kubeconfig

`config generate kubeconfig` 是一个 KubeSphere 原生请求，目标为：

```text
/kapis/resources.kubesphere.io/v1alpha2/users/<username>/kubeconfig
```

用户名始终来自选中的 Context；若为空，则回退到其 User map key。即使 API
端点或 Token 标志覆盖连接值，此身份要求仍然有效。显式 Cluster 标志优先于
Context 的默认 Cluster。

响应正文原样复制到 stdout。ksctl 不会解析、合并、存储它，也不会将其写入
`~/.kube/config`。调用者负责安全重定向和文件生命周期。

## 插件模型

在 Cobra 正常执行前，根命令会让插件分派器处理未知命令路径。分派器：

1. 忽略内置命令、帮助和 shell 补全请求；
2. 收集命令词，直到遇到第一个标志；
3. 将词中的连字符映射为下划线，用于查找可执行文件；
4. 首先尝试最长的 `ksctl-<words-joined-with-dashes>` 名称；
5. 将未匹配的词、剩余参数和继承的环境传递给可执行文件；并且
6. 在 Unix 上用插件替换当前进程，在 Windows 上则启动子进程。

由于查找只会在 Cobra 找不到内置命令后开始，插件无法替换或扩展内置命令路径。
插件名称前的标志会被拒绝，以免持久标志被误认为插件输入。内置 `logs` 和 `top`
路径同样不能被 `ksctl-logs` 或 `ksctl-top` 可执行文件替换。

`plugin list` 按顺序扫描唯一的 PATH 目录，并诊断候选项的可执行权限、PATH
遮蔽和与内置命令树的冲突。诊断返回非零状态，因此脚本可以将警告视为无效的
插件配置。

插件可执行文件不是注入 ksctl 进程的客户端。它们是任意外部程序，具有用户权限、
继承的环境，以及自己的标志和连接处理逻辑。

## 安全属性

- 交互式密码读取时不会回显。
- `auth login` 只将 Password 用于其请求，绝不持久化。
- 手动放入 Config 的 Password 保持明文，由用户负责。
- 写入 Config 和 Token 缓存时，以 `0700` 模式创建父目录，以 `0600` 模式
  写入临时文件，同步后将其原子重命名到目标位置。
- `config view` 默认对 Password、bearer Token 和 TLS 私钥数据脱敏；`--raw`
  是一个显式的敏感输出逃生口。
- 构造 OAuth 错误时不会嵌入请求凭据。
- 显式 API 端点覆盖不能借用选中 Context 的凭据。
- login 不能将 Fleet 名称重新绑定到另一个 API 端点。
- 返回登录持久化失败时，Config 保持不变，并补偿 Token 缓存写入。
- 生成的 kubeconfig 和原始配置输出可能包含凭据，调用者必须妥善保护。
- 原始 API 请求可能变更服务器状态，其请求或响应正文也可能包含敏感数据。ksctl
  不会检查或脱敏这些内容，也不会对其应用扩展组件生命周期保护措施。
- 容器日志写入 stdout，可能包含应用 Secret；ksctl 不检查或脱敏其内容。
- 插件不会被检查或置于沙箱中。信任插件等同于信任用户运行的任何其他可执行
  文件。

这些属性可以减少意外持久化和泄露，但不提供加密凭据存储、操作系统 keychain
集成或沙箱。

## 兼容性

- 模块要求 Go 1.26 或更高版本。
- 支持的服务器代际为 KubeSphere 4.x。
- `k8s.io/apimachinery`、`k8s.io/cli-runtime`、`k8s.io/client-go`、
  `k8s.io/kubectl` 和间接依赖 `k8s.io/metrics` 均对齐到 v0.36.2。
- 独立二进制文件和 kubectl 插件二进制文件由同一模块和命令包构建。

Kubernetes 模块必须保持在同一个对齐的次要版本，因为 kubectl 的命令、Factory、
Builder、打印器、发现、RESTMapper 和客户端接口会共同演进。

## 验证边界

架构由多个层面的验证保护：

- 命令测试验证显示名称传播、已注册命令和标志、版本行为、资源请求、成员
  Cluster 路由、递归 logs/top 示例、日志子资源流式传输、Metrics/Core 请求和
  Metrics API 发现回退；
- 配置测试验证默认值、严格 schema 和类型契约拒绝、序列化、脱敏、迁移边界和
  文件系统权限；
- 认证和缓存测试验证优先级、登录和刷新行为、错误泄露、Fleet/User 缓存身份、
  编码和权限；
- Kubernetes 客户端测试验证 TLS 和 Token 映射、Cluster API 端点构造、内存
  客户端配置、发现缓存和回退、API 路径保留及 RESTMapper 行为；
- KubeSphere 连接测试验证原生配置、用户名解析、Cluster 验证和注入的传输
  所有权；
- 原始 API 命令测试验证路径和方法验证、正文构造、查询和响应字节保持不变、
  不自动应用 Cluster 路由以及 HTTP 错误正文传播；
- 插件测试验证最长匹配、参数转发、连字符转换、内置命令保护和 PATH 诊断；以及
- 正常构建会编译 `cmd/ksctl`。

用户可见命令、配置、认证或插件发生变化时，必须更新 [CLI 指南](cli_zh.md)。
包边界、路由、依赖对齐、持久化或安全属性发生变化时，必须更新本文档。
