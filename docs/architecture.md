# 架构总览

更新时间：2026-10-21（v1.0.0 发布后的同步）。性质：从当前源码提取的现状说明，不是验收报告。未运行测试、核心、节点或浏览器；结论均可按本文标注的源码位置复核。

产品定位与完整需求见 [项目说明书](../AGENTS.md)；本节只写“代码里现在是什么”。与源码冲突时以源码为准。

## 1. 组成

| 组件 | 入口 | 位置 | 职责 |
| --- | --- | --- | --- |
| 中央面板 | `proxy-panel` | `cmd/panel`、`internal/panel/*`、`internal/platform/*`、`web/` | 管理、策略、订阅、发布、访问记录、备份 |
| 节点 Agent | `proxy-agent` | `cmd/agent`、`internal/agent/*` | 注册、连接、编译、应用、采集、缓存、更新 |
| 代理核心 | 官方 `xray` / `sing-box` 二进制 | 节点本地 `/usr/local/lib/proxy-agent/<core>` | 入站接入、分流、出站、中转、代理链 |

中央端是一个 Go 程序加本地 SQLite，前端构建产物通过 `web.Files()` 嵌入同一程序（`cmd/panel/main.go:180`）。代理流量不经过中央端。

## 2. 监听与数据面

| 端口 | 用途 | 协议 |
| --- | --- | --- |
| `http_address`（示例 `0.0.0.0:443`） | Web 页面、REST `/api/v1`、SSE `/api/v1/events`、订阅 `/sub/{token}` | HTTPS |
| `grpc_address`（示例 `0.0.0.0:9443`） | 节点控制通道 | gRPC + 节点 mTLS |

节点侧没有入站端口约定：入站由配置版本决定，节点按渲染结果监听（`internal/agent/core/xray/render.go`、`internal/agent/core/singbox/render.go`）。

## 3. 面板模块

模块目录在 `internal/panel/`，跨模块编排与 HTTP 装配在 `internal/platform/server/`。

| 模块 | 目录 | 职责 |
| --- | --- | --- |
| auth | `internal/panel/auth` | 管理员、会话、CSRF、登录限速、首次初始化令牌 |
| user | `internal/panel/user` | 用户、设备、凭据、节点组授权、额度状态机、连接观测 |
| node | `internal/panel/node` | 节点、节点组、注册码、证书、心跳、能力、健康检查、逻辑删除（`deleted_at` 与阻塞项检查） |
| subscription | `internal/panel/subscription` | 按授权节点组展开并生成订阅 |
| proxy | `internal/panel/proxy` | 入站、出站、出口组、代理链、协议校验、健康 |
| policy | `internal/panel/policy` | 分流规则、服务集、网站控制、策略模拟 |
| deployment | `internal/panel/deployment` | 配置版本、差异、批量发布、回滚、核心与 Agent 更新任务 |
| access | `internal/panel/access` | 域名访问记录、流量汇总、保留清理 |
| dashboard | `internal/panel/dashboard` | 总览聚合（只读） |
| connections | `internal/panel/connections` | 面板内存中的实时连接快照（不落库） |
| system | `internal/panel/system` | 系统设置、备份、恢复、发布公钥、保留策略 |

约束：每个模块只通过自己的 Repository 修改所属数据；配置发布统一经 Deployment；Dashboard 只查询（见 AGENTS 第 24 节）。

## 4. Agent 模块

| 模块 | 目录 | 职责 |
| --- | --- | --- |
| control | `internal/agent/control` | gRPC 连接、心跳、重连、注册、证书操作、核心更新 worker |
| reconcile | `internal/agent/reconcile` | 期望状态与实际状态协调、时间窗重渲染、核心回滚修复 |
| supervisor | `internal/agent/supervisor` | 每 3 秒探测出站健康；核心不健康时恢复上一成功配置 |
| core/xray | `internal/agent/core/xray` | 官方 Xray 探测、渲染、启动、健康、账户统计、在线数、访问日志 |
| core/singbox | `internal/agent/core/singbox` | 官方 sing-box 探测、渲染、启动、健康、Clash API 节点总量 |
| collector | `internal/agent/collector` | 访问行解析、分钟桶聚合、隐私白名单 |
| spool | `internal/agent/spool` | 断线缓存、批量上传、ACK 删除、容量上限 |
| capability | `internal/agent/capability` | 系统资源采样（CPU、内存、磁盘）与心跳负载、采集/流量源状态 |
| hostnet | `internal/agent/hostnet` | 服务器强制出站源地址：校验目标状态、渲染 nft 表 `inet proxy_agent_source`、读写 helper 目录（`proxy-agent --apply-host-network`） |
| agenthost / updater / agentjobs | `internal/agent/agenthost` 等 | 受管 Agent 镜像、自更新与核心更新（URL + SHA-256）、任务日志与回滚 |
| lifecycle / storage / agentprocess | `internal/agent/lifecycle` 等 | 共享状态进程锁与属主、存储格式下限检查、受管 Agent 进程的安装事务 |
| identity | `internal/agent/identity` | 节点私钥、证书、ACK 位置、密钥轮换 |

Agent 只执行固定操作（应用/回滚配置版本、重启核心、健康检查、轮换证书、更新 Agent 或核心），不提供任意命令执行（AGENTS 第 23 节）。

## 5. 数据模型

SQLite，路径来自配置 `database`（示例 `/var/lib/proxy-panel/panel.db`）。迁移在 `migrations/`，按顺序执行到 `0109`（`0108_node_outbound_network.sql` 增加节点出站网络字段，`0109_node_deletion.sql` 增加 `nodes.deleted_at`），已应用迁移的校验和被 `migrations/checksums_test.go` 固定，不允许修改历史迁移。

| 分组 | 主要表 |
| --- | --- |
| 管理员 | `admins`、`sessions`、`admin_operations` |
| 用户与设备 | `users`、`devices`、`credentials`、`subscription_tokens` |
| 节点 | `nodes`、`node_identities`、`node_enrollment_tokens`、`node_certificate_renewals`、`node_groups`、`node_group_members`、`user_node_group_grants` |
| 代理配置 | `inbounds`、`outbounds`、`outbound_groups`、`outbound_group_members`、`proxy_chains`、`proxy_chain_hops` |
| 分流与网站控制 | `policies`、`service_sets`、`service_set_entries`、`custom_domain_entries` |
| 发布 | `config_revisions`、`deployment_targets` |
| 访问与流量 | `access_rollups`、`traffic_hourly`、`traffic_daily`、`node_status_snapshots` |
| 系统 | `system_settings`、`backups` |

协议特有字段保存在 JSON 列（如 `inbounds.settings_json`、`policies.match_json`、`nodes.capabilities_json`），Go 侧必须解析为明确结构，不做无结构透传（AGENTS 第 26 节）。

## 6. 关键流程

**节点接入**：面板创建节点并生成一次性注册码 → Agent 本地生成私钥并提交 CSR → 面板用节点 CA 签发证书（`internal/platform/pki`）→ Agent 以 mTLS 建立长连接。注册码只能使用一次，且要求节点记录为启用状态（`internal/panel/node/repository.go:106`、`control.go:136`）。

**配置发布与回滚**：Owner 保存业务配置 → 生成 Revision（`POST /api/v1/revisions`）→ 部署（`POST /api/v1/revisions/{id}/deploy`）→ Agent 渲染并校验核心原生配置 → 切换核心 → 健康检查 → 失败自动回滚到上一成功版本。代理链按“先出口、后中转、最后入口”发布，回滚反向（`internal/panel/deployment/relay_batches.go`）。

**自动额度版本**：流量入库后由 `internal/panel/user` 的额度状态机判断，`internal/platform/server/quota.go` 按节点合并（60 秒窗口）后创建并部署只调整用户访问面的自动 Revision；目标节点是该用户当前授权的节点加上**已发布**状态里仍持有其凭据的节点。自动版本以节点**已发布**状态为基线，除用户与入站的绑定取自当前草稿（这类未发布编辑可能被带出）外，不会带出未发布的 Owner 编辑。入站的启用状态也不取自草稿：每个版本另行记录 Owner 当时启用的入站集合（`config_revisions.inbound_enabled_json`，迁移 `0111_inbound_enabled_intent.sql`），自动版本原样继承，凭据回到已发布状态下被禁用的入站时只有该记录说 Owner 曾启用才重新启用；记录为 NULL 时保守保持禁用。单节点发布、用户接入以及代理链/关联节点批次都会记录这份意图（`internal/panel/proxy/snapshot.go` 的 `SnapshotIntent`、`ChainSnapshots`、`RelaySnapshots` 与 `internal/panel/deployment/batches.go` 的 `CreateBatch`），只有调用方直接提交 state 的原始版本（`POST /api/v1/revisions`）留为 NULL。

**订阅**：`GET /sub/{token}?format=uri|base64|sing-box`（省略时用系统设置里的默认格式，出厂默认 `uri`）按授权节点组展开、过滤不可用节点和协议后生成，Token 只保存哈希。URI 与 Base64 列表无法携带固定证书，因此省略钉了 `trusted_ca_pem` 的 Hysteria2/TUIC 端点并且不报错（其余链接照常输出）；这类端点用 sing-box 格式获取，该格式会写入 `tls.certificate`。

**节点出站网络**：Owner 在节点设置里填核心出站源 IP / 网卡和服务器强制出站 IPv4 / IPv6（迁移 `0108_node_outbound_network.sql` 的四个 `nodes` 列），改动在**发布配置后**生效。核心出站源在渲染时写入拨号出站（Xray `sendThrough` + `streamSettings.sockopt.interface`，sing-box `inet4/inet6_bind_address` + `bind_interface`）；整机强制出站走 helper：Agent 写 `/var/lib/proxy-agent/host-network/desired.json`，`proxy-agent-host-network.service`（oneshot，只带 `CAP_NET_ADMIN`）执行 `proxy-agent --apply-host-network`，由 `internal/agent/hostnet` 渲染并应用 nft 表 `inet proxy_agent_source`，结果写回 `status.json`。缺失网卡/源地址或 helper 失败是临时失败码，节点保留上一成功配置。

**节点删除**：`DELETE /api/v1/nodes/{id}` 先查引用（入站、出站、其他节点指向它的中转出站、出口组、代理链、分流规则、进行中的批量发布），有阻塞项时返回 409 `node_delete_blocked` 并带上 `blockers`（预览接口是 `GET /api/v1/nodes/{id}/deletion-preview`）。未登记且无身份的节点走“取消登记”，记录直接移除；已登记节点在同一事务里逻辑删除：写 `nodes.deleted_at`、`enabled=0`、`allow_subscription=0`、`status='disabled'`，把 `node_identities.revoked_at` 置位，删除注册码/续期/节点组成员，并用 `deployment.CloseNodeTasks` 关闭该节点自己的待发布配置、核心/Agent 更新任务与排队健康检查（`internal/panel/node/deletion.go`、`internal/panel/deployment/node_tasks.go`）。列表与选择器按 `deleted_at IS NULL` 过滤（`internal/panel/node/repository.go:300`），`?deleted=1` 只返回已删除节点；历史流量与访问记录保留。

**遥测与访问记录**：Agent 每 5 秒采样账户计数写入本地 spool；心跳（面板指定 15 秒）里上传流量与访问批次，收到 ACK 才删除。Xray 的访问行来自 Agent 运行时目录下的 `access.log`（Agent 自己 tail，读到即清空，20 MiB 兜底上限），sing-box 的访问行来自核心日志（`internal/agent/collector/singbox.go`）。面板把域名级记录按分钟桶聚合入库，不保存正文、路径或凭据。

**证书**：节点证书默认 90 天，到期前由 Agent 自动续期（`internal/agent/control/renewal.go`）；轮换与续期也可在节点停机时手工执行（见[运维手册](operations.md)）。

## 7. 部署路径

| 端 | 路径 | 内容 |
| --- | --- | --- |
| 中央 | `/usr/local/bin/proxy-panel` | 程序 |
| 中央 | `/usr/local/bin/qui` | root 终端菜单（改管理员口令、改 Web 端口、重启/启停、更新） |
| 两端 | `/usr/local/bin/qui-update` | 从 GitHub Release 下载并核对摘要后原地升级（面板先备份数据库） |
| 中央 | `/etc/proxy-panel/config.yaml`、`/etc/proxy-panel/tls/` | 配置与 Web TLS 材料 |
| 中央 | `/var/lib/proxy-panel/` | `panel.db`、`identity/`（节点 CA）、`backups/` |
| 节点 | `/usr/local/bin/proxy-agent` | 程序（引导镜像） |
| 节点 | `/etc/proxy-agent/config.yaml`、`/etc/proxy-agent/tls/ca.crt` | 配置与受信中央 CA（校验面板控制连接证书，即面板 Web 证书） |
| 节点 | `/var/lib/proxy-agent/` | `identity/`、`revisions/`、`spool/`、`runtime/`、`releases/` |
| 节点 | `/var/lib/proxy-agent/host-network/` | `desired.json`、`status.json`：服务器强制出站的 helper 目录 |
| 节点 | `/usr/local/lib/proxy-agent/<core>` | 安装脚本放置的官方核心 |
| 两端 | `/var/lib/proxycontrol-install/` | 安装记录与操作锁 |

日志：两个程序都写 stdout，由 systemd 收集到 journald（`internal/platform/logging`），不写 `/var/log` 下的文件。

## 8. 决策的架构后果（D1–D5）

| 决策 | 架构后果 |
| --- | --- |
| D1 只用官方核 | 无 `integrations/`、无定制构建；能力靠运行时探测，缺失能力降级为“不可用” |
| D2 并发不是硬限制 | 无核内准入与额度账本；面板内存快照（45 秒过期）只展示与告警 |
| D3 无每节点预算 | 只有一个用户额度；超限/到期/禁用由面板自动发布移除凭据的版本 |
| D4 上游缺失能力 | 每域名字节可空、只记录“配置出口”、UDP 按上游、权重策略删除 |
| D5 sing-box 每用户未知 | sing-box 只报节点总量，每用户流量与在线数为未知，额度只统计 Xray 流量 |

## 9. 不在本版范围

微服务、外部消息队列、容器编排、HTTPS 中间人解密、网页内容审计、任意远程命令、无限多跳代理链等明确不做项见 AGENTS 第 35 节。当前进度和剩余阶段见[轻量重构计划](lightweight-refactor-plan.md)。
