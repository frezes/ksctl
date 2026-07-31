# ksctl 设计

[English](design.md) | **简体中文**

本文档面向开发人员说明 ksctl 的核心设计。命令语法和工作流请参阅
[CLI 指南](cli_zh.md)。

## 设计目标与边界

ksctl 通过一个命令行界面访问 KubeSphere 4.x，并经 KubeSphere 提供基本完整的
kubectl 操作能力。它保留上游 kubectl 行为，同时让各命令以一致的方式处理连接、
认证和作用域选择。

命令界面具有四条不同的安全边界：

- 顶层 `get` 和租户查看只执行读取操作；
- 通用 Kubernetes 操作位于 `kube` 下，并使用 KubeSphere 认证和 Cluster 路由；
- 扩展组件生命周期命令提供专用且受控的写入操作；以及
- `api` 提供直接发送认证请求的底层入口，调用者选择的请求可能变更服务器状态。

可执行插件在用户授权下作为独立程序运行，不受这些内置保护措施约束。

ksctl 不将 kubeconfig 用作持久化配置模型，不跨 Cluster 聚合资源，不审计插件或
为其提供沙箱，也不支持 KubeSphere 3.x。

## 整体架构

整体设计将用户意图、连接解析和服务器行为相互分离。命令层获取请求的操作和
显式作用域；连接与认证解析将标志、环境值和选中的 Context 转换为一个有效
Endpoint、身份和可选 Cluster；随后由 KubeSphere 提供原生 API，或将 Kubernetes
请求代理到该 Cluster。

因此，命令不需要理解 KubeSphere 的代理拓扑。命令运行时解析有效连接，并在整个
调用期间复用该结果，使 Kubernetes 操作、辅助请求和错误报告使用一致的身份与
作用域。

## 核心设计

### 跨集群资源访问

Context 提供默认 Fleet、User 和可选 Cluster。显式连接值或作用域值会在一次
调用中覆盖这些默认值。选中 Cluster 时，Kubernetes 发现和资源请求经 KubeSphere
访问该 Cluster；否则直接使用 Fleet Endpoint。

Kubernetes 操作及其辅助请求共用同一个有效 Cluster 路由。Namespace 只缩小
命名空间资源请求的范围，不负责选择 Cluster。这样既能使命令语法独立于路由，
也能避免同一操作内的不同请求偏移到其他目标。

某些 KubeSphere 部署即使聚合发现不完整，也会暴露各个发现端点。ksctl 可以根据
服务器实际暴露的能力（包括已注册的扩展 API）恢复聚合视图，而无需维护静态的
公共资源列表。

每个 Kubernetes 操作只查询一个选中的 Cluster。跨 Cluster 聚合明确不属于命令
模型。

### 租户管线

租户资源使用 KubeSphere 原生 API，而不使用通用 Kubernetes 发现管线。因此，
即使存在同名 Kubernetes 资源，其作用域仍然明确。

Workspace 和租户 Cluster 读取以 Fleet 为作用域，并忽略选中的资源 Cluster。
Namespace 读取使用选中的 Cluster，因为 Namespace 的归属与 Cluster 相关。
可选 Workspace 会缩小 Namespace 和租户 Cluster 集合的范围，但不会改变拥有
该请求的控制平面。

人类可读输出使用稳定的 kubectl 风格表格。结构化输出保留服务器响应，避免客户端
丢失 ksctl 尚不了解的字段。

### 扩展组件管理

扩展组件目录和 InstallPlan 状态归属于主 KubeSphere 控制平面。因此，扩展组件
命令不会继承 Context 的默认资源 Cluster；多集群部署通过显式 InstallPlan
放置单独表达。

放置会记录为当前操作选择的合格 Cluster 集合，而不是将成员关系保留为动态查询。
KubeSphere 控制器负责在主端和成员上执行，ksctl 则负责输入验证、已接受意图、
进度报告和可操作的失败信息。

安装、升级、配置和卸载是专用的生命周期写操作，而不是通用变更动词。更新会保护
已接受意图，避免冲突或陈旧状态影响结果。依赖只做验证，不会隐式安装。

除非请求等待，否则生命周期操作异步执行。等待从已接受的操作开始，不会信任较早
的终止状态；主目标和成员目标独立跟踪；删除或同名重建会被视为操作已改变。诊断
汇总控制器、依赖、工作负载和成员状态，但不获取 Secret、配置值或应用日志。

### 原始 API 请求

`api` 命令复用常规 KubeSphere 连接、凭据、TLS、超时和用户代理解析，但不会增加
资源语义。调用者负责提供服务器相对路径、方法和可选请求正文。

选中的 Cluster 作用域不会自动添加。需要访问 Cluster 作用域 API 时，调用者必须
在提供的路径中明确表达该路由。这样可以让原始传输保持可预测，避免静默改写任意
路径。

响应字节原样传递。HTTP 错误正文仍对调用者可见，同时命令会报告失败。ksctl 不会
对原始 API 操作进行类型识别、格式化或脱敏，也不会应用扩展组件生命周期保护，
因此调用者需承担变更和泄露风险。

### 认证与配置

Fleet 拥有一个 KubeSphere Endpoint、对应 TLS 设置和 Fleet 作用域的 Users。
Context 选择一个 Fleet 和 User，并可提供默认 Cluster。这样既能让凭据与服务器
绑定，又能支持多个可复用的连接选择。

显式标志优先于环境值，环境值优先于配置的连接状态。显式 Endpoint 必须与显式
Token 配对，不能借用选中 Context 的凭据。具有权威性的配置 Token 文件和 Token
不可用时会直接失败，不会静默回退到安全性更弱的凭据。

处理完显式和配置的 Token 后，认证可以复用有效的缓存 Token，或刷新已过期的
Token。配置的 Password 是最后的命令本地回退，其成功响应不会被隐式缓存。无法
读取或格式错误的缓存数据会被报告，而不是忽略。

登录是显式创建缓存的工作流。它先进行认证，再以一个可恢复操作更新 Config 和
选中的 Fleet/User Token 缓存。持久化失败时会恢复先前缓存状态，提供的 Password
永远不会被存储。登出会尽力执行远程撤销，始终尝试删除选中的本地缓存，并保留
Config。

## 安全与兼容性

顶层 `get` 和租户查看不会变更资源。`kube` 可能变更选中的 Cluster，并由解析出的
KubeSphere 凭据所具备的 Kubernetes RBAC 权限授权。它不使用本地 kubeconfig，
也不会在成员 Cluster 操作失败后改为操作 host Cluster。

交互式 Password 输入不会回显，登录也不会持久化提供的 Password。Config 和
Token 缓存更新使用受限权限和原子替换。手动放入 Config 的 Password 仍为明文，
由用户负责。

原始配置、生成的 kubeconfig、原始 API 响应和容器日志可能包含凭据或应用 Secret。
ksctl 不检查或脱敏这些输出；调用者负责其目标位置和生命周期。

扩展组件生命周期写入保留其专用验证和等待语义。原始 API 请求可能变更服务器
状态，插件则以用户权限和继承的环境运行。两者都不具备扩展组件命令界面的
生命周期保护。

ksctl 支持 KubeSphere 4.x，并将上游 Kubernetes 兼容性作为一个整体集成面进行
跟踪。
