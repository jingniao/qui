# 架构总览

本文概述当前实现、代码入口与关键数据流。产品字段、接口和恢复规则见[项目说明书](project-spec.md)，操作步骤见[部署与升级](deployment.md)和[运维手册](operations.md)，设计约束与验收边界见[实施决定](implementation-decisions.md)。与源码冲突时以源码为准。

## 1. 组成

| 组件 | 代码入口 | 职责 |
| --- | --- | --- |
| 中央面板 `proxy-panel` | `cmd/panel`、`internal/panel`、`internal/platform`、`web` | 管理、订阅、策略、发布、统计与备份恢复 |
| 节点 `proxy-agent` | `cmd/agent`、`internal/agent` | 注册、配置应用、核心管理、采集、断线缓存与更新 |
| 代理核心 | `internal/agent/core/{xray,singbox,gost}` | Xray、sing-box 处理用户代理；GOST 3.3.2 处理转发与加密中继 |
| 更新工具 | `cmd/qui-update-db`、`cmd/qui-update-state`、`deploy/linux/update.sh` | Bash 编排服务与锁，Go 处理数据库文件事务、journal、快照丢弃和事件编码 |

面板是 Go 单体与本地 SQLite，Vue 构建产物由 `web.Files()` 嵌入。Agent 主动连接面板，用户代理流量由节点核心处理。Xray 使用官方构建，sing-box 使用官方或 qui 的未修改上游构建。

## 2. 监听与数据面

| 配置与默认监听 | 用途 | 协议 |
| --- | --- | --- |
| `http_address`：`0.0.0.0:22550` | Web、REST `/api/v1`、SSE `/api/v1/events`、订阅 `/sub/{token}` | HTTPS |
| `grpc_address`：`0.0.0.0:18880` | 节点控制通道 | gRPC + mTLS |

节点入站端口由发布配置决定。管理会话与用户门户会话分别认证；管理员入口由系统设置维护，不写入文档。

## 3. 面板模块

业务模块位于 `internal/panel/`，跨模块编排和 HTTP/gRPC 装配位于 `internal/platform/server/`。

| 模块 | 职责 |
| --- | --- |
| `auth`、`portal` | 管理员认证、独立用户门户、本人数据与 Owner 只读预览 |
| `user` | 用户隐藏接入身份、凭据、授权、额度、重置与设备观测 |
| `node` | 节点、节点组、登记、证书、心跳、能力与生命周期 |
| `subscription` | 授权来源、导入缓存、中立代理模型与格式导出 |
| `proxy` | 入站、出站、出口组、代理链、隧道和中转画布 |
| `policy` | 分流、服务集与网站控制 |
| `deployment` | 不可变配置版本、批量发布、回滚、核心及 Agent 更新任务 |
| `access` | 域名访问聚合、流量账本、历史查询与保留清理 |
| `dashboard`、`statistics` | 只读总览及后台共享统计快照 |
| `connections` | 内存中的实时连接快照，不落库 |
| `certificate`、`notification` | Web/入站证书与 DNS-01、通知规则及发送 |
| `system` | 设置、备份恢复、发布信任与保留策略 |

每个模块通过自己的 Repository 修改所属数据，配置发布统一经 Deployment。展示查询使用独立只读池；认证、额度、凭据生成、发布和恢复写决策保留原事务链路。前端按 `web/src/modules/` 分业务，门户在 `web/src/portal/`，路由和 SSE 在 `web/src/app/`。

## 4. Agent 模块

| 目录（相对 `internal/agent/`） | 职责 |
| --- | --- |
| `control`、`identity` | gRPC 连接、心跳、登记、证书续期与密钥轮换 |
| `reconcile`、`supervisor` | 期望状态协调、配置应用与上一成功配置恢复 |
| `core/xray`、`core/singbox`、`core/gost` | 原生配置渲染、校验、进程、健康与核心能力 |
| `collector`、`spool`、`capability` | 域名采集、持久交付队列、资源和计数源采样 |
| `hostnet`、`tuning` | 固定网络与 sysctl 请求，通过独立 helper 执行 |
| `agenthost`、`updater`、`agentjobs` | 受管镜像、自更新、核心更新与任务恢复 |
| `lifecycle`、`storage`、`agentprocess` | 共享状态锁、存储兼容检查与安装事务 |
| `handovercli` | 本机交接协议、授权、状态、撤销与停核屏障 |

Agent 只执行固定操作，不提供任意远程命令。设备门控与连接观测随 supervisor 运行，不写配置 Revision；端口转发使用核心原生能力。具体能力与限制见[协议矩阵](protocol-support-matrix.md)和[设备限制](device-limit.md)。

## 5. 数据模型

SQLite 路径来自配置 `database`。当前迁移头为 **0165**（`0165_reinstall_superseded_updates.sql`），最低兼容 v1.7.5。`migrations/` 嵌入 SQL，`migrations/checksums_test.go` 固定已发布迁移校验和，历史 `0001–0152` 校验和保存在 `internal/platform/db`；历史迁移不可修改。

| 数据分组 | 主要表 |
| --- | --- |
| 身份与用户 | `admins`、`sessions`、`users`、`credentials`、`subscription_tokens` |
| 节点与授权 | `nodes`、`node_identities`、`node_groups`、`user_node_group_grants` |
| 代理与隧道 | `inbounds`、`outbounds`、`outbound_groups`、`proxy_chains`、`tunnels`、`tunnel_rules`、`port_claims` |
| 分流与客户端策略 | `policies`、`service_sets`、`client_policy_profiles`、`subscription_sources` |
| 配置与更新 | `config_revisions`、`deployment_targets`、`agent_system_update_tasks`、`agent_update_tasks`、`core_update_tasks` |
| 遥测与流量 | `access_rollups`、`node_traffic_samples`、`core_traffic_samples`、`core_traffic_hourly`、`traffic_sample_hours` |
| 系统 | `system_settings`、`backups`、`certificates`、`notification_rules` |

协议特有字段使用明确 Go 结构解析 JSON；敏感配置由 vault 密封。完整模型与恢复规则见[说明书 §26、§31](project-spec.md)。

## 6. 关键流程

| 流程 | 主链路与边界 |
| --- | --- |
| 接入与证书 | 一次性注册码 → Agent 本地私钥/CSR → 面板节点 CA 签证 → mTLS 长连接；证书默认 90 天，Agent 自动续期 |
| 节点存活 | `node.Liveness` 独立记录连接、接收及待确认消息，RuntimeSnapshots 经读池发 SSE；写库等待单独展示，数据库提交后才 ACK |
| 发布与回滚 | 保存草稿 → 创建 Revision → 部署 → Agent 原生渲染校验 → 切换 → 健康检查；失败回滚。多跳先出口、后中转、最后入口，回滚反向 |
| 自动额度 | `server/quota.go` 合并访问变化，以成功发布配置为基线更新凭据；禁用、到期、超额统一经配置发布执行 |
| 订阅 | Token → 用户授权 → 成功发布节点/导入缓存 → 中立模型 → 格式导出；请求时不抓取外部来源，维护或确认离线的整条线路排除 |
| 出站源地址 | 核心渲染绑定源 IP/网卡；入站出口 IP 分别编译、探测和选路；整机强制出站经受限 nft helper，发布后生效 |
| 遥测 | 采样写本地 spool → 控制通道批量上传 → 面板持久入账后 ACK → Agent 删除；只聚合域名元数据，不保存正文或凭据 |
| 多核心 | 一条 Revision 覆盖全部核心，全部校验后依序切换，失败逆序补偿；支持空核心或纯 GOST 节点时不承载用户配置与订阅 |
| 隧道与画布 | 保存草稿与端口预留不部署，发布重算依赖并按出口至入口执行；订阅使用成功快照，画布仅创建受支持的代理中转 |
| 删除与重装 | 删除先检查依赖，已登记节点撤销身份并保留历史流量。重装成功核销恢复码时，同事务将未结束升级标为 `superseded`，保留执行证据并释放配置恢复占用 |

详细字段、准入条件与接口见[说明书 §11–22、§27](project-spec.md)。订阅来源和客户端格式差异见[协议矩阵](protocol-support-matrix.md)。

## 7. 进程、更新与部署目录

Linux 使用 systemd；Agent 也支持 Alpine/OpenRC。主服务以专用非 root 账号运行，提权操作通过固定 helper 执行。

Agent 更新由监督进程串行处理；终端与网页完整更新共用 `qui-update`，分别确认引导程序与实际 worker。支持交接时，更新前授权并保存校验材料，新进程核对配置、依赖和核心摘要后认领原核心；普通停止、未授权退出和卸载仍执行停核屏障。材料损坏或身份不明时保留现场并阻塞发布。

更新器快照保存自身工具与摘要，恢复先验证再操作服务；旧快照交给自己的 updater。Bash 保留下载、服务编排、锁和恢复入口，`hostupdatedb` 与 `hostupdatestate` 承担文件事务和状态编码。升级失败、断电恢复及首次跨版本边界见[部署与升级 §5](deployment.md#5-升级)。

面板配置与状态分别位于 `/etc/proxy-panel/`、`/var/lib/proxy-panel/`；Agent 对应 `/etc/proxy-agent/`、`/var/lib/proxy-agent/`；共享安装记录与更新快照在 `/var/lib/proxycontrol-install/`。完整路径和权限见[部署与升级 §7](deployment.md#7-目录与权限)。

面板日志由服务管理器收集；Agent 运行日志写入 `/var/lib/proxy-agent/logs/agent.log`，当前文件加 9 个归档总上限 100 MiB，启动和文件写入错误由服务管理器收集。独立 root 升级任务输出不纳入该轮转。

## 8. 能力边界

- Xray 与具备对应能力的 sing-box 提供账户计量、源观测和门控；无用户计数标签的官方 sing-box 仅有节点总量，用户用量为未知。GOST 不提供用户身份、额度或订阅。
- 展示快照、统计缓存与真实额度账本分离；旧统计和存活证据不参与写决策，也不证明代理核心或数据面健康。
- 核心原生能力、Agent 精确能力契约和订阅导出能力分别检查；不能从版本号或计数标签推断全部支持。

### 8.1 存储与生命周期

后台统计按同一读取快照准备，SSE 配合定期校准；查询错误和数据年龄显式返回。`traffic_sample_hours` 是可重建索引，完整小时读索引、边界读原始样本，未回填时查询精确来源。

流量原始、分钟和小时层级按保留策略整理，累计游标与账本保持；已压缩历史无法精确覆盖查询边界时返回精度错误。代理样本与整机月流量采用各自口径，不混算。

编号队列通过认证身份、连续交付编号和双向检查点去重；双方确认前缀满 24 小时后才可淘汰回执，缺口或 pending 阻止跨越。旧无编号回执只在接收超过 7 天、记账完成且无同批 pending 时分批删除，窗口外重发可能重复计入。

流量整理与空闲页回收独立运行；新空库默认 incremental，已有 NONE/FULL 库不自动转换，不执行全库 VACUUM。迁移 0164 仅失效需重算的派生小时索引，原始数据和账本保持。

恢复将流量、游标、回执及队列检查点作为一组保留当前冻结库事实，冲突明确失败。恢复结束门户会话，未确认的调优与组件操作不自动重发；定时导入、续费、重置、额度及清理任务受停止/恢复生命周期统一管理。

设计来源见[流量存储决定](implementation-decisions.md#2026-11-18-traffic-storage-retirement)、[展示数据决定](implementation-decisions.md#2026-11-19-prepared-display-data)及 [v1.11.0 发布记录](changelog.md)。

## 9. 范围与进一步阅读

明确不做项见[说明书 §35](project-spec.md#35-明确不做)。实机验收限制见[实施决定](implementation-decisions.md)，历史设计与底层调查见[归档](archive/README.md)，生产状态只查[运维登记](operations.md#当前生产环境)。
