# 架构总览

本文描述当前实现的模块与数据流；已完成设计见[归档](archive/README.md)。

产品定位与完整需求见 [项目说明书](project-spec.md)；本节只写“代码里现在是什么”。与源码冲突时以源码为准。

## 1. 组成

| 组件 | 入口 | 位置 | 职责 |
| --- | --- | --- | --- |
| 中央面板 | `proxy-panel` | `cmd/panel`、`internal/panel/*`、`internal/platform/*`、`web/` | 管理、策略、订阅、发布、访问记录、备份 |
| 节点 Agent | `proxy-agent` | `cmd/agent`、`internal/agent/*` | 注册、连接、编译、应用、采集、缓存、更新 |
| 代理核心 | 官方 Xray；官方或 qui 的未修改上游 sing-box 构建；固定 GOST 3.3.2 | 节点本地 `/usr/local/lib/proxy-agent/<core>` | 入站接入、分流、出站、中转、代理链、原生端口转发（GOST 仅转发/中继） |

中央端是一个 Go 程序加本地 SQLite，前端构建产物通过 `web.Files()` 嵌入同一程序（`cmd/panel/main.go`）。代理流量不经过中央端。

## 2. 监听与数据面

| 端口 | 用途 | 协议 |
| --- | --- | --- |
| `http_address`（默认 `0.0.0.0:22550`，示例配置同此值） | Web 页面、REST `/api/v1`、SSE `/api/v1/events`、订阅 `/sub/{token}` | HTTPS |
| `grpc_address`（默认 `0.0.0.0:18880`，示例配置同此值） | 节点控制通道 | gRPC + 节点 mTLS |

节点侧没有入站端口约定：入站由配置版本决定，节点按渲染结果监听（`internal/agent/core/xray/render.go`、`internal/agent/core/singbox/render.go`、`internal/agent/core/gost`）。

## 3. 面板模块

模块目录在 `internal/panel/`，跨模块编排与 HTTP 装配在 `internal/platform/server/`。

| 模块 | 目录 | 职责 |
| --- | --- | --- |
| auth | `internal/panel/auth` | 管理员、会话、CSRF、登录限速、首次初始化令牌 |
| user | `internal/panel/user` | 用户、隐藏接入身份、凭据、授权、额度、重置周期与设备观测 |
| portal | `internal/panel/portal` | 独立终端用户账号会话、本人数据、订阅与只读 Owner 预览 |
| node | `internal/panel/node` | 节点、节点组、注册码、证书、心跳、能力、健康检查、逻辑删除（`deleted_at` 与阻塞项检查） |
| subscription | `internal/panel/subscription` | 按授权节点组展开并生成订阅 |
| proxy | `internal/panel/proxy` | 入站、出站、出口组、代理链、协议校验、健康 |
| policy | `internal/panel/policy` | 分流规则、服务集、网站控制 |
| deployment | `internal/panel/deployment` | 配置版本、差异、批量发布、回滚、核心与 Agent 更新任务 |
| access | `internal/panel/access` | 域名访问记录、流量汇总、保留清理 |
| dashboard | `internal/panel/dashboard` | 总览聚合（只读） |
| statistics | `internal/panel/statistics` | 后台准备同一读取快照下的日／月／趋势／整机统计，供总览和服务器页面共享 |
| connections | `internal/panel/connections` | 面板内存中的实时连接快照（不落库） |
| system | `internal/panel/system` | 系统设置、备份、恢复、发布公钥、保留策略 |

约束：每个模块只通过自己的 Repository 修改所属数据；配置发布统一经 Deployment；Dashboard 只查询（见[项目说明书 §24](project-spec.md#24-中央端模块设计)）。

展示读取链路（见[实施决定](implementation-decisions.md#2026-11-19-prepared-display-data)）：运行状态以白名单快照和 SSE 更新，浏览器定期校准，恢复连接时保持已有卡片、确认真实离线后才显示离线。统计由后台每 30 秒准备，总览基础内容独立读取；统计错误和数据年龄显式返回，旧结果不参与额度判断。代理流量的 `traffic_sample_hours` 是可重建小时索引，完整小时走索引、边界走原始样本；未完成回填时仍执行原精确查询，原始账本、来源切换、去重语义与已压缩历史的精度限制不变。

## 4. Agent 模块

| 模块 | 目录 | 职责 |
| --- | --- | --- |
| control | `internal/agent/control` | gRPC 连接、心跳、重连、注册、证书操作、核心更新 worker |
| reconcile | `internal/agent/reconcile` | 期望状态与实际状态协调、时间窗重渲染、核心回滚修复 |
| supervisor | `internal/agent/supervisor` | 每 3 秒探测出站健康；核心不健康时恢复上一成功配置 |
| core/xray | `internal/agent/core/xray` | 官方 Xray 探测、渲染、启动、健康、账户统计、在线数、访问日志 |
| core/singbox | `internal/agent/core/singbox` | 探测、渲染、启动、健康；有标签时 v2ray API 每用户计数；Clash 源连接观测与关闭 |
| core/gost | `internal/agent/core/gost` | 固定 GOST 的转发/中继、监听健康与节点总量；不承载用户身份 |
| tuning | `internal/agent/tuning` | 白名单 sysctl 请求、独立 root helper 与结果确认 |
| collector | `internal/agent/collector` | 访问行解析、分钟桶聚合、隐私白名单 |
| spool | `internal/agent/spool` | 断线缓存、批量上传、ACK 删除、容量上限 |
| capability | `internal/agent/capability` | 系统资源采样（CPU、内存、磁盘）与心跳负载、采集/流量源状态 |
| hostnet | `internal/agent/hostnet` | 服务器强制出站源地址：校验目标状态、渲染 nft 表 `inet proxy_agent_source`、读写 helper 目录（`proxy-agent --apply-host-network`） |
| agenthost / updater / agentjobs | `internal/agent/agenthost` 等 | 受管 Agent 镜像、自更新与核心更新（URL + SHA-256）、任务日志与回滚 |
| lifecycle / storage / agentprocess | `internal/agent/lifecycle` 等 | 共享状态进程锁与属主、存储格式下限检查、受管 Agent 进程的安装事务 |
| handovercli | `internal/agent/handovercli` | root 本机固定命令：交接协议探测、授权、状态、撤销与停核屏障；供更新器和服务 post-stop 调用 |
| identity | `internal/agent/identity` | 节点私钥、证书、ACK 位置、密钥轮换 |

Agent 只执行固定操作（应用/回滚版本、重启核心、健康检查、证书与更新、TTL 设备门控、受限 TCP-connect 探测）。端口转发使用正常发布路径和各核心原生入站；没有任意命令执行或替代转发器。门控和 observer 在 supervisor 生命周期内运行，不写配置 Revision；Xray 使用官方 adrules/lsrules 并在非原子替换失败后恢复规则，sing-box 仅关闭用户与源都匹配的连接。

## 5. 数据模型

SQLite，路径来自配置 `database`（示例 `/var/lib/proxy-panel/panel.db`）。当前源码迁移头为 **`0163`**（`0163_traffic_minutes.sql`）；最新正式版 **v1.10.2** 迁移头为 **`0163`**；存储与生命周期见第 8.1 节。已应用迁移（`0152–0163`）的校验和由 `migrations/checksums_test.go` 固定，历史 `0001–0152` 校验和保存在 `internal/platform/db`，不允许修改历史迁移。

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

协议特有字段保存在 JSON 列（如密封 JSON `inbounds.encrypted_settings`、`policies.match_json`、`nodes.capabilities_json`），Go 侧必须解析为明确结构，不做无结构透传（见[项目说明书 §26](project-spec.md#26-数据库设计)）。

## 6. 关键流程

**节点接入**：面板创建节点并生成一次性注册码 → Agent 本地生成私钥并提交 CSR → 面板用节点 CA 签发证书（`internal/platform/pki`）→ Agent 以 mTLS 建立长连接。注册码只能使用一次，且要求节点记录为启用状态（`internal/panel/node/repository.go:106`、`control.go:136`）。

**配置发布与回滚**：Owner 保存业务配置 → 生成 Revision（`POST /api/v1/revisions`）→ 部署（`POST /api/v1/revisions/{id}/deploy`）→ Agent 渲染并校验核心原生配置 → 切换核心 → 健康检查 → 失败自动回滚到上一成功版本。代理链按“先出口、后中转、最后入口”发布，回滚反向（`internal/panel/deployment/relay_batches.go`）。

**自动额度版本**：`internal/platform/server/quota.go` 将用户访问变化按节点合并（60 秒），以已发布状态为基线替换用户凭据并创建 Revision。当前草稿的用户/入站绑定可以被带出；入站启用意图继承基线，不自动启用 Owner 禁用的入站。目标包括当前授权节点与已发布状态仍持有凭据的节点。完整重试、恢复和对账规则见[项目说明书](project-spec.md) §22.1。

**订阅**：Token → 用户 → 来源（已发布节点、授权导入缓存）→ 中立代理模型 → 注册导出器（uri/base64/sing-box/clash/clash-legacy）。一个用户一个隐藏身份与共享链接，新 Token 以哈希查找、vault 加密副本供再次查看；旧 Token 无副本需重置。显式 format 优先，其次客户端 UA 匹配表（尚未逐一完成真机验证），最后系统默认。sing-box 仍是片段；固定证书的 QUIC 在 URI/Base64 中省略，在 sing-box 中带证书、mihomo 中带指纹。全局规则模板按授权展开，不改节点策略；导入仅用于客户端，定时 HTTPS 固定公网 IP 抓取并加密缓存，不在订阅请求中联网。只有 mihomo 输出客户端链。

外部导入使用有界结构化结果与加密缓存，订阅请求不抓取来源。公开模型统一承载 Reality 公钥/Short ID、独立客户端 TLS 指纹与 xhttp host/path/mode；证书指纹不复用为客户端指纹。导出器按格式逐端点省略，复用链依赖清理与 emptyPolicy。自建服务端 Reality 私钥留在加密设置/修订中，经已安装核心原生探测和 Agent 精确能力契约进入发布；订阅只从成功修订映射公开字段，中转保留目标 SNI。

**入站出口 IP**：`inbounds.egress_source_ip` 默认空。Xray/sing-box 在组成员筛选前，按源地址分别编译出站、调度和路由；同地址共享上下文，Block 保持阻断，归属沿用原 ID。内部探测以（源地址、原出站 ID）隔离，面板仍只接收默认来源健康；未知或失败成员仍保留原始探测路径。保存、快照与发布检查实际渲染核心能力；Agent 应用前检查本地地址，整机 SNAT 额外豁免包含入站源地址。详见项目说明书 §13.1。

**节点出站网络**：Owner 在节点设置里填核心出站源 IP / 网卡和服务器强制出站 IPv4 / IPv6（迁移 `0108_node_outbound_network.sql` 的四个 `nodes` 列），改动在**发布配置后**生效。核心出站源在渲染时写入拨号出站（Xray `sendThrough` + `streamSettings.sockopt.interface`，sing-box `inet4/inet6_bind_address` + `bind_interface`）；整机强制出站走 helper：Agent 写 `/var/lib/proxy-agent/host-network/desired.json`，`proxy-agent-host-network.service`（oneshot，只带 `CAP_NET_ADMIN`）执行 `proxy-agent --apply-host-network`，由 `internal/agent/hostnet` 渲染并应用 nft 表 `inet proxy_agent_source`，结果写回 `status.json`。缺失网卡/源地址或 helper 失败是临时失败码，节点保留上一成功配置。

**节点删除**：`DELETE /api/v1/nodes/{id}` 先查引用（入站、出站、其他节点指向它的中转出站、出口组、代理链、分流规则、进行中的批量发布），有阻塞项时返回 409 `node_delete_blocked` 并带上 `blockers`（预览接口是 `GET /api/v1/nodes/{id}/deletion-preview`）。未登记且无身份的节点走“取消登记”，记录直接移除；已登记节点在同一事务里逻辑删除：写 `nodes.deleted_at`、`enabled=0`、`allow_subscription=0`、`status='disabled'`，把 `node_identities.revoked_at` 置位，删除注册码/续期/节点组成员，并用 `deployment.CloseNodeTasks` 关闭该节点自己的待发布配置、核心/Agent 更新任务与排队健康检查（`internal/panel/node/deletion.go`、`internal/panel/deployment/node_tasks.go`）。列表与选择器按 `deleted_at IS NULL` 过滤（`internal/panel/node/repository.go:300`），`?deleted=1` 只返回已删除节点；历史流量与访问记录保留。

**遥测与访问记录**：Agent 每 5 秒采样账户计数写入本地 spool；心跳（面板指定 15 秒）里上传流量与访问批次，收到 ACK 才删除。Xray 的访问行来自 `runtime/xray/access.log`，sing-box 从核心日志提取。Linux 两者使用共享 FileTail，只交付完整行，半行留在文件直到补全；认领从 inode/offset checkpoint 接读，追平后清空，20 MiB 隐私截断记录丢弃事实；Windows 保留原读取行为。三核 ProcessHandle 区分真实子进程与 Linux 认领句柄；加密 RuntimeMaterial 保留原配置、epoch/Revision、端口、计数源和交接 checkpoint，最终采样和认领首采均写原 spool。空窗内允许日志积压及少量访问记录重复或缺失；sing-box 不持久化来源映射，旧连接来源为 unavailable。核心在空窗内死亡的尾部流量只记本机 WARN `core_traffic_tail_unavailable`，不估算。面板把域名级记录按分钟桶聚合入库，不保存正文、路径或凭据。

**证书**：节点证书默认 90 天，到期前由 Agent 自动续期（`internal/agent/control/renewal.go`）；轮换与续期也可在节点停机时手工执行（见[运维手册](operations.md)）。

**多核心运行**：`cores: [{name,path}]` 的第一个用户内核（Xray 或 sing-box）由 `Inventory.Primary` 记录为主核心；GOST 不能是主核心，列表可为空或仅含 GOST，此时主核心为空。面板通过统一的 `node.CoreOptional` 结合 `core_optional_supported` 与启用列表判断，旧 Agent 的未知上报规则保持不变。无用户内核时只允许显式 GOST forward 和 GOST 隧道，不承载用户配置或订阅；没有任何内核时省略核心健康，不因缺主核心而降级。`cores.json.enc` 持久记录启用/禁用；每核使用 `runtime/<core>/`。一条 Revision 覆盖所有核心，先全部校验再依序切换，失败逆序补偿；发布仍重启该节点全部核心，supervisor 只恢复异常核心。移除须确认退出才删除运行目录与 Agent 自管镜像，安装器二进制与记录保留。旧 Agent 移除最后用户内核仍返回 `last_core`；支持可选内核时先检查全部用户内核依赖（409 `user_core_in_use`），再由 Agent 激活格式 6（失败 `fallback_incompatible`）。面板拒绝将无用户内核节点更新到低于 1.9.0 的 Agent（`agent_version_unsupported`）。

Linux Agent 更新时，仅有效本机更新授权允许 prepared→detached 退出交接，覆盖引导程序与受管 worker 两次切换；`handovercli` 协调更新事务，systemd/OpenRC post-stop 执行同一停核屏障。启动持有 lifecycle 锁，枚举固定三核目录（含禁用核），批量恢复私有端口及渲染输入，用新版本渲染并校验候选。配置、依赖、环境、核心镜像与账户归属全部一致才认领原 run/epoch/Revision；首采写原 spool 后写 adopted，并恢复 current/expected/history 与 renderedDigest。真实配置变化先建立旧统计上下文，再走全核发布。身份不明、损坏材料、部分认领失败只释放观察句柄，阻塞发布与监督恢复并保留在线控制面。认领及 Prepare 期间不触发 RestoreCore，认领后仍按单核恢复。普通 stop/restart、卸载、SIGINT、未授权 EOF 与非更新崩溃仍停核，遗留核心须先过停核屏障。pidfd 不可用或源/目标不支持协议时，明确提示后按停核更新；首次从旧 Agent 升级仍会断线。验收与未实测项见[交接设计 §9](core-handover-plan.md#9-实施与验收结果2026-11-22)。

**隧道管理（v1.4）**：`tunnels/tunnel_hops` 表达 entry → [mid] → exit，GOST TLS＋mTLS；接收跳各有一个共享监听，每节点只有一个下一跳。两节点要求契约 ≥3，三节点 ≥4，当前 Agent 上报 4。`tunnel_rules` 将入口监听指向隧道或直接目标；直接规则优先 GOST，否则主 Xray/sing-box。linked 目标随目标地址/入站端口变化派生并标 needs_publish，出口等于目标时用 loopback；manual 固定目标。`tunnel_objects` 按 ID 归属生成入站，普通入站编辑/删除返回 managed_by_tunnel。`port_claims` 持久预留，保存草稿不部署，删除已发布对象须再发布移除。

**隧道发布与订阅**：计划只读，发布重算依赖、按 exit → mid → entry 交给部署批次，失败反向回滚；未变返回 unchanged。只有全成功推进版本及 0130 的无凭据 published_snapshot。订阅先按目标用户入站授权/凭据过滤，再给可见 linked 规则生成中转端点，保留协议字段、显式原 TLS server name，仅换入口地址/规则端口。草稿与 needs_publish 不改已发布地址；目标设备限制只能看到中继源地址，账户计量仍在目标。

**代理中转画布**：保留 `canvas_tunnels/canvas_tunnel_objects` 内部 ID 归属和共享依赖；仅创建 proxy_chain/tunnel 的 tls/wireguard 代理中转。出口为 qui 入站（独立内部监听/凭据）或前一跳上的外部代理出站，后者决定 FinalOutboundID。relay/exit 的 relayPort 优先旧 tunnelPort；仅画布无范围时保留 8443/51820，新隧道/规则无默认端口回退。旧 port_forward/gost 连接只读至迁移。画布跳过未变依赖服务器，凭据、备份及轮换见说明书 §14、§16、§29、§31。

**统一端口检查**：`proxy/ports.go` 跨核心按数字端口覆盖入站范围、画布和持久声明。手动保存检查启用入站间冲突，声明始终占用；自动分配与 ports/check 包含禁用入站，建议不预留，保存再查。Agent 仍执行最终校验。

**三类流量**：账户字节进入用户额度；核心/节点总量含无身份转发（GOST relay 搬运不重复计入）；整机月流量来自单张网卡累计差值，含其他服务、只展示额度、不停止服务。整机日行与游标长期保留，资源/延迟遥测只保留 7 天。字段与未知值规则见说明书 §21，操作见[运维手册](operations.md) §18–20。

## 7. 部署路径

| 端 | 路径 | 内容 |
| --- | --- | --- |
| 中央 | `/usr/local/bin/proxy-panel`、`/usr/local/bin/qui` | 面板程序与 root 终端菜单 |
| 中央 | `/etc/proxy-panel/` | `config.yaml` 与 `tls/`（Web TLS 材料） |
| 中央 | `/var/lib/proxy-panel/` | `panel.db`、`identity/`（节点 CA）、`backups/` |
| 节点 | `/usr/local/bin/proxy-agent`、`/usr/local/lib/proxy-agent/<core>` | Agent 引导镜像与安装器放置的代理核心 |
| 节点 | `/etc/proxy-agent/` | `config.yaml` 与 `tls/ca.crt`（受信中央 CA） |
| 节点 | `/var/lib/proxy-agent/` | `identity/`、`revisions/`、`spool/`、`runtime/`、`releases/`、`host-network/` |
| 两端 | `/usr/local/bin/qui-update`、`/var/lib/proxycontrol-install/` | 原地升级脚本、安装记录与操作锁 |

完整路径、属主与权限见[部署与升级 §7](deployment.md#7-目录与权限)。日志由两个程序直接写 stdout，systemd 收集至 journald（`internal/platform/logging`），OpenRC 经 `logger` 写 syslog，不写 `/var/log` 下的独立文件。

## 8. 能力边界

| 决策 | 架构后果 |
| --- | --- |
| 核心来源 | Xray 官方；sing-box 官方或 qui 的未修改上游构建加上游标签，运行时探测 |
| 设备限制 | 取消设备管理；跨入口源地址估计与内存准入，TTL 门控，缺能力或租约过期放行 |
| 用户额度 | 只有一个用户额度；超限/到期/禁用由面板自动发布移除凭据的版本 |
| 上游边界 | 每域名字节可空、只记录“配置出口”、UDP 按上游、权重策略删除 |
| 账户计数 | `singbox_user_stats` 节点读取累计账户计数进入既有 spool/游标/额度；无能力的官方核仍节点总量、用户未知 |

## 8.1 存储与生命周期

隧道与转发规则由 `tunnels`、`tunnel_hops`、`tunnel_rules`（含无凭据 `published_snapshot`）、`tunnel_objects` 与 `port_claims` 持久化。备份与恢复共用校验并保留端口声明与密封材料；恢复后已发布对象标记为 `modified`，不自动下发。

客户端策略配置库（`client_policy_profiles`）以独立 profile revision 管理编辑，活动模板以 `activeRevision` 管理启用与订阅快照；`subscription_sources` 分开保存有界 `latest_result_json` 与 `last_success_at`，部分成功提交可用缓存，失败保留旧行但排除该来源，写入失败保留原状态。精确 `@endpoint` 引用、名称关键词与 `emptyPolicy` 在服务端按用户授权展开，预览与真实订阅共用同一展开和导出路径；`uiMetadata` 仅保存编辑器展示信息。共享规则集引用检查通过应用层注入仓储事务读取全部已保存配置（含未启用配置），不依赖浏览器筛选。协议公开字段与服务端私钥沿用既有加密备份与修订路径。

编辑器的高级预览上下文可选择有效且有授权的用户，自动选择仍为默认；同一上下文传给草稿／已保存预览、解析成员和候选接口，仅影响查看，不修改活动模板或用户授权。画布规划与发布前共用中转图检查，按实际出站保存顺序模拟新增／替换边，包含既有启用业务出站和尚未移除的旧归属边。

用户隐藏身份、门户、导入与设备限制之外，数据库还保存节点目标/指标/调优、多核心任务与计量、共享隧道及归属、整机流量和 GOST 密封材料。表结构与升级兼容规则由[项目说明书](project-spec.md) §26、§31 维护；各版本迁移变更见[变更记录](changelog.md)。恢复结束门户会话，探测任务过期，未确认的调优/组件操作不自动重发。

定时导入、自动续费、续费提醒、重置周期和额度对账均随面板停止/进程内恢复取消并等待结束。重置以面板时区本地日运算保留月锚点、夹紧短月；恢复保留当前重置边界。可选心跳增加宿主/核心 uptime、网卡计数及只读 sysctl，旧 Agent 缺字段显示未知。月节点流量由 access 统一合并本地月界内的 `node_traffic_samples` 去重视图与核心 UTC 小时历史；接口 scope 仍为 `retained_account_samples`，旧版已删账户回退数据仍无法补回（L9）。核心原始数据保留 7 天，旧小时原子汇总后删除，游标长期保留；历史非整小时边界返回精度错误。该代理样本口径与整机月流量不同；UDP 门控及无用户身份转发限制见[设备限制](device-limit.md)和[协议矩阵](protocol-support-matrix.md)。

## 9. 不在本版范围

微服务、外部消息队列、容器编排、HTTPS 中间人解密、网页内容审计、任意远程命令、无限多跳代理链等明确不做项见[项目说明书 §35](project-spec.md#35-明确不做)。当前状态与进度见[项目说明书](project-spec.md) 的“当前状态”。

## 流量回执与核心历史整理

access 拥有核心小时汇总、统一节点查询和回执检查点；Agent bound spool v2 认证队列身份、固定编号、原文及持久删除事实，control 只传交付身份与确认。完成、确认、封存使用独立于控制连接的连续编号；两端协商后切换，旧回执继续保留。候选双方确认段至少等待 24 小时后分批原子删除回执并推进封存进度，缺口和待续输入阻止跨越。队列表按节点和队列身份保留各代检查点，每节点唯一活动队列；只有显式身份恢复核销可关闭旧队列、授权新队列，普通握手拒绝换身份。恢复把统计、游标、回执及封存状态作为一组保留当前冻结库事实，旧冻结证据认证后迁移副本再读取，冲突暴露为恢复失败。调度复用既有访问清理生命周期，停止和恢复等待同一后台循环。

详见[实施决定](implementation-decisions.md#2026-11-18-traffic-storage-retirement)。
