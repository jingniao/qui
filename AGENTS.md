# 轻量多节点代理控制平台项目说明书

**文档版本：** V1.2  
**编制日期：** 2026 年 8 月 20 日  
**本次修订：** 2026-10-28（v1.1 Phase 11，文档与本地回归；尚未发布 v1.1.0）  
**项目状态：** 核心业务已实现，按轻量重构计划收敛完成；v1.0.0 已于 2026-10 作为 GitHub 发布出版，v1.0.1（节点 Agent 支持 Alpine/OpenRC 与实机验收修复）于 2026-10-23 发布，一键安装与 qui-update 的失败处理与自动回滚于 2026-10-23 作为 v1.0.2 发布，qui-update 的中断自动恢复（含断电）与面板跨迁移降级于 2026-10-23 作为 v1.0.3 发布，强制出站 helper 连续失败后不再执行新配置的问题于 2026-10-23 作为 v1.0.4 发布，Alpine 节点的 sing-box（musl 构建）与钉证书 QUIC 端点的 URI 订阅于 2026-10-24 作为 v1.0.5 发布，核心运行不了保留修订时 Agent 反复重启、连不上面板的问题于 2026-10-24 作为 v1.0.6 发布，删除用户后流量上传卡死、回滚恢复已禁用用户、快捷用户接入的凭据被自动修订删除、重连后核心更新结果丢失与 supervisor 绕过恢复退避等问题于 2026-10-25 作为 v1.0.7 发布，删除用户或设备后自动撤销凭据、恢复备份保留凭据墓碑、核心更新回执与结果重发、备份校验与入站启用意图等修复于 2026-10-26 作为 v1.0.8 发布，面板停止服务（含进程内恢复备份）时停掉自动额度版本的定时器、Agent 日志 `retry_after` 改为可读时长于 2026-10-26 作为 v1.0.9 发布；功能与交付验收阶段（状态核对：2026-10-26；三台主机实机验收进行中，arm64 未验证，V1 未完成）  
**目标用户规模：** 1～10 名内部用户  
**部署方式：** 自托管、轻量化部署  
**适用场景：** 自有或明确授权网络中的代理接入、节点管理、访问分流与域名级访问控制

当前工作入口见 [v1.1 计划](docs/v1.1-plan.md) 与 [进度记录](docs/v1.1-progress.md)，历史轻量化决策见 [轻量重构计划](docs/lightweight-refactor-plan.md)。系统组成见 [架构总览](docs/architecture.md)，部署与运维见 [部署与升级](docs/deployment.md) 和 [运维手册](docs/operations.md)，完成标准见 [验收矩阵](docs/v1-acceptance.md)。v1.1 的 R1–R9 与 D-a 至 D-e 优先于历史 D1、D2、D5 的对应口径；实现已完成本地审查，原生核心 CI、面板主机 -race、RC 与发布仍待验收，不表示 v1.1.0 已发布。

## 变更记录

**V1.2（2026-10-28）**：按 v1.1 计划同步已实现功能，发布说明仍为草稿。

- L4：qui-update 支持 `X.Y.Z-rc.N`、semver 优先级与 `--component panel|agent`；本地修复完成，Linux/root 与主机验收待执行，RC 未运行。

- R1：订阅拆为中立代理模型、来源与格式注册表；增加 mihomo、legacy Clash 及一个全局客户端规则模板。
- R2：外部订阅导入已实现，按用户授权、加密缓存，仅用于客户端；客户端链仅 mihomo 输出。
- R3：Xray 保持官方发布核；sing-box 可用未修改上游源码的 qui 可复现构建，增加上游 `with_v2ray_api` 标签，官方核仍可用。
- R4、R5：独立终端用户账号和门户；每用户一条订阅，多端共享，新 Token 同时存哈希与加密副本，可再次查看；旧 Token 需重置后才能查看。
- R6：权限、额度、到期和凭据统一按用户管理，取消设备管理；每用户保留一个隐藏接入身份兼容 Agent，设备 API 返回 410。
- R7：并发设备限制以跨入口节点的源 IPv4 / IPv6 /64 估计，运行时门控；升级默认 0（不限），缺能力节点明确显示部分执行。
- R8、R9：单一 v1.1.0 发布目标，先面板后 Agent；保留 Owner 的页面布局，补齐实际接口、元数据、提醒、探测、端口转发与流量重置周期，未知值显示“—”。
- D-a：WireGuard 共享密钥只支持一次一个设备，不计入设备限制。D-b：活跃窗口 60 秒、释放窗口 120 秒、门控 TTL 3 分钟。
- D-c：用户可主动重置自己的订阅链接并撤销旧链接，操作记审计。D-d：Owner 与门户均只显示掩码地址（IPv4 /24、IPv6 /48）。D-e：移除无法按用户执行的上下行限速与备注中的旧限速元数据。
- 迁移 0114–0119 覆盖门户会话、设备限制、导入、节点元数据、重置周期和 TCP 探测。§34 与验收矩阵逐项记录待 CI/Owner 验收；不把跳过的原生核心测试、L8 UDP、L9 完整月流量或 RC 当作通过。

**V1.1（2026-10-17）**：按轻量重构计划决策 D1–D5 修订。

- 第 9.1 节：最大并发连接数改为观测软限制（D2），流量额度由面板软执行并自动发布（D3）。
- 第 15 节：出口组选择方式改为上游能力口径，权重策略移除（D4）。
- 第 20、21 节：每域名字节可空并显示“—”，出口记录为“配置出口”，补充上报周期与本地缓存上限的实际取值。
- 第 22 节：增加自动额度版本与核心更新流程。
- 第 23 节：更新方式明确为核心用官方 URL + SHA-256、Agent 用签名清单，并说明已删除的 receipt/journal 机制。
- 第 30 节：补充身份目录与日志走 journald 的说明。
- 第 11、13、23、30.2 节：节点可配置核心出站源 IP/网卡与服务器强制出站源地址（固定 nft 规则、独立 oneshot 单元）。
- 第 32 节阶段 4：改为适配官方 Xray 发布二进制（D1）。
- 第 34 节：验收标准按 D1–D5 补充观测口径。
- 第 9.1、22.1 节：自动额度版本失败重试、启动对账与连接数口径修正。
- 第 21、22.1 节：访问日志读后即清空；自动额度版本覆盖仍持有凭据的节点、启动对账失败重试、核心重启影响与草稿绑定例外。
- 第 30.1 节：面板主机增加 root 终端菜单 `/usr/local/bin/qui`（改管理员凭据、改 Web 端口、重启/启动/停止）。
- 第 30 节：面板与节点主机增加 `/usr/local/bin/qui-update`（从 GitHub Release 下载、校验 SHA256SUMS 后用 install.sh 原地升级，面板升级前备份数据库），`qui` 菜单增加“更新”。
- 第 11.3 节：注册节点后面板显示一键安装命令（`node-install.sh`：从 GitHub Release 下载与面板同版本的归档并校验，按公网证书或固定 SHA-256 建立对面板证书的信任，登记并启动 Agent）；新增无需登录的 `GET /install/ca.pem`。
- 文档版本号由 V1.0 升为 V1.1；文档中“第 5.5 步”的表述改为“Phase 14”。
- 第 4.3、30.2 节（2026-10-22）：节点 Agent 除 systemd 外正式支持 OpenRC（Alpine 3.19+，OpenRC ≥ 0.45，含无 systemd 的 LXC 容器）。脚本自动识别初始化系统，服务定义在 `deploy/openrc/`。强制出站 helper 在 OpenRC 上由 `inotifyd` watcher 服务触发；代理核心以 `Pdeathsig` 启动，Agent 被杀时核心随之退出；缺少 `/etc/machine-id` 时由安装脚本创建。面板仍只支持 systemd。
- 第 30 节（2026-10-23，v1.0.2）：`qui-update` 在替换前把会被替换的文件快照到 `/var/lib/proxycontrol-install/rollback.*`，更新后要求服务 10 秒内不重启；`install.sh` 失败、版本不符或新版本没有持续运行时自动恢复快照（面板还恢复更新前的数据库）并启动旧版本。新增离线选项 `--bundle/--sums`。`install.sh` 失败时不再留下 `.installing` 临时文件与暂存目录，systemd 主机缺少 `unzip` 时自动安装；面板的一键安装命令在 Debian/Ubuntu 缺少 curl 时先用 apt 安装。
- 第 30 节（2026-10-23，v1.0.3）：`qui-update` 在快照里写入进度日志（`prepared`/`started`/`committed`/`restored`，原子写入并落盘）与它自己的副本；Ctrl-C、SIGTERM、SSH 断线时按日志自动回滚，`kill -9`、断电后由开机单元 `proxycontrol-update-recovery`（systemd oneshot / OpenRC 初始化脚本，排在 Agent、面板与强制出站单元之前）完成回滚，下一次运行 `qui-update` 也会先完成未结束的恢复；同时只允许一个 `qui-update`。面板数据库备份先写 `.partial`、校验后改名；回滚覆盖数据库前把新版本的数据库保留为 `failed-update-*.db`。面板新增只读的 `--check-database FILE`（退出码 0/3/4/5），`qui-update` 用目标版本预检数据库：降级跨过迁移时不做任何替换并列出可用备份；新增 `--database FILE`，把面板降级与对应的 `pre-update-*` 数据库一起安装（第 30.1、31 节）。
- 第 30.2 节（2026-10-23，v1.0.4）：`proxy-agent-host-network.service` 设置 `StartLimitIntervalSec=0`，helper 连续失败不再让 systemd 停用 path 单元；Agent 只有在 `status.json` 确认同一请求成功后才把保留的“清除”请求视为已生效，否则重新发布；`install.sh` 发现 oneshot 单元处于 `failed` 时重放一次保留的请求。新增回归测试 `deploy/linux/tests/host-network-test.sh`。
- 第 10.3、30.2 节（2026-10-24，v1.0.5）：`install.sh` 与 `node-install.sh` 在 musl 主机（Alpine）上安装 sing-box 的官方 `-musl` 归档，按架构与 libc 固定摘要；新增 `deploy/linux/tests/core-asset-test.sh`。通用 URI 与 Base64 订阅省略钉了 `trusted_ca_pem` 的 Hysteria2/TUIC 端点，其余链接照常输出，不再整体返回 400。
- 第 22 节（2026-10-24，v1.0.6）：Agent 启动时如果已安装的核心编译或启动不了保留的成功修订（例如手工换成不支持 Hysteria2/TUIC 的 Xray），不再退出并被反复重启，而是让核心保持停止、照常连上面板并上报核心不健康，收到新修订后正常应用；supervisor 的重试间隔从 3 秒倍增到 1 分钟。重新应用保留修订失败时不再删除或改写它的修订目录。修订完整性错误（校验和、身份、回执、解密）仍然退出；核心更新中断后的修复路径也仍然退出，由重启重试。
- 第 9.3、21、22、22.1、22.2 节（2026-10-25，v1.0.7，见[验收矩阵](docs/v1-acceptance.md) 9.15、9.16）：删除用户或设备时记录凭据墓碑（迁移 `0110_credential_tombstones.sql`），删除前产生的流量按批次所属修订照常入账，不再让节点的流量与访问记录上传卡死；Agent 对同一流量批次连续被拒 3 次后隔离它（保留在磁盘上，1 小时后或重启时重试）。每次部署或回滚成功后为该节点排队一次额度对账，回滚到旧修订不再恢复已禁用用户的凭据。“用户接入”快捷功能改为写入真实入站并按正常路径渲染；原始配置版本不再接受用户凭据（`unmanaged_user_credentials`）。核心更新结果在重连后不再被静默丢弃，Agent 在核心更新后上报新的能力。supervisor 只在核心健康时刷新运行时状态，核心不健康时只走退避后的恢复。
- 第 9.1、22.1 节（2026-10-26，v1.0.8，见[验收矩阵](docs/v1-acceptance.md) 9.17–9.19）：删除用户或设备后自动发布移除其凭据的版本（`auto: user <name> deleted` / `device deleted`），不再需要 Owner 重新发布；此前被删除的凭据在下一次发布前仍可使用（验收矩阵 9.16）。草稿渲染不了时退回已发布状态的自动版本同样移除已删除设备的凭据。恢复较旧的备份时合并当前数据库的凭据墓碑（第 21 节）；核心更新成功后重连、任务已按上报版本标为成功时，面板仍接受随后到达的成功回执，不再断开控制连接（第 22.2 节）。Owner 清除一个已保存节点结果的核心更新任务时不再改写其状态；此前这样的任务（状态 `cleared`、结果 `failed_closed`）会让之后的每次备份都校验失败，备份校验现在接受已存在的这类记录（第 31 节）。自动版本不再启用 Owner 手动禁用、但未发布启用的入站：每次发布额外记下 Owner 当时启用的入站集合（迁移 `0111_inbound_enabled_intent.sql`，`config_revisions.inbound_enabled_json`），自动版本原样继承基线版本的记录，凭据回到已禁用入站时只有记录说 Owner 曾启用它才重新启用；记录为 NULL（本列之前发布的版本）时保守保持禁用，等 Owner 下一次发布。Agent 发核心更新结果时如果控制连接在等 Ack 时断开，结果不再随会话丢失，而是留在 core-update worker 里由下一个会话重发，直到面板确认；面板以 `FailedPrecondition` + `core update result rejected` 永久拒绝同一结果（例如任务已被 Owner 清除）连续 3 次后 Agent 丢弃并记 Warn，其他错误永不丢弃（第 22.2 节）。
- 第 22.1 节（2026-10-26，v1.0.9，见[验收矩阵](docs/v1-acceptance.md) 9.18 问题 C、9.20）：面板停止服务（包括进程内恢复备份前）时，自动额度版本的合并窗口定时器全部停止，进行中的对账被取消并在服务返回前结束；停止过程中被打断的对账不计入 3 次失败、不告警，由下一次启动对账重新处理。此前旧实例会在恢复后继续对已关闭的数据库重试并记 `database is closed`。Agent 日志的 `retry_after` 改为可读时长。

**V1.0（2026-08-20）**：初版，完整需求与架构约束。

---

## 1. 文档目的

本文档用于明确轻量多节点代理控制平台的产品目标、功能范围、技术架构、模块边界、数据模型、节点程序设计、开发顺序、测试要求和验收标准。

本文档可作为以下工作的统一依据：

- 项目立项
- 产品设计
- 前端页面设计
- 后端接口设计
- Node Agent 开发
- 数据库设计
- 代理核心适配
- 测试和验收
- 部署与后续维护

项目开发过程中如出现需求变更，应同步更新本文档版本，避免前端、后端、节点端和代理核心实现之间出现理解偏差。

---

# 2. 项目定位

本项目不是单纯的代理账号管理面板，而是一套：

> **多节点代理网络控制平台 + 用户访问管理 + 智能分流 + 网站访问策略 + 域名访问记录 + 协议转换中转 + 链式代理**

系统主要面向 1～10 名内部用户使用，不以大规模商业运营、计费、支付或多租户为目标。

第一阶段重点是：

- 功能完整
- 部署简单
- 资源占用低
- 配置清晰
- 节点稳定
- 中央服务失联时节点继续运行
- 错误配置自动回滚
- 容易增加新节点
- 容易增加新协议
- 用户订阅通过节点组统一管理
- 支持协议 A 接入、协议 B 中转
- 支持节点 A 将指定网站流量转发到节点 B
- 支持节点端链式代理
- 可以控制用户或节点不能访问哪些网站
- 可以记录用户访问过哪些域名
- 不审计网页正文和其他页面内容

---

# 3. 项目核心原则

## 3.1 轻量优先

系统在满足功能的前提下，优先减少依赖、进程数量、资源占用和运维复杂度。

第一版不强制使用：

- Kubernetes
- Redis
- Kafka
- RabbitMQ
- Elasticsearch
- ClickHouse
- Prometheus
- 独立配置中心
- 独立任务队列
- 微服务架构
- 服务网格

中央端以一个 Go 程序运行，前端编译后嵌入中央程序，数据库使用 SQLite。

## 3.2 模块化单体

系统采用模块化开发，但不拆分成多个微服务。

模块之间需要有清晰边界，包括：

- 用户模块
- 节点模块
- 订阅模块
- 代理模块
- 分流模块
- 配置发布模块
- 访问记录模块
- 系统模块

模块之间通过 Go 接口或应用层服务调用，不通过内部 HTTP 或消息队列通信。

## 3.3 控制面与数据面分离

中央面板只负责：

- 用户管理
- 节点管理
- 策略管理
- 配置发布
- 状态查看
- 域名访问记录
- 订阅生成

用户实际代理流量不经过中央面板。

数据转发由节点上的 Xray、sing-box 或 WireGuard 完成。

## 3.4 入站与出站解耦

用户接入节点使用的协议，与节点向下一跳转发使用的协议互相独立。

例如：

```text
用户
 ↓ VLESS
节点 A
 ↓ Trojan
节点 B
 ↓ Direct
Internet
```

节点 A 负责终止 VLESS 入站连接，根据规则建立 Trojan 出站连接。

## 3.5 节点独立运行

节点失去中央服务连接后：

- 保持最后一个成功配置
- 继续提供代理服务
- 本地缓存状态和访问记录
- 中央服务恢复后重新连接并补传数据

中央服务不可用不应直接导致节点代理停止。

## 3.6 最小化审计

系统只记录：

- 谁访问了网站
- 什么时间访问
- 从哪个节点进入
- 访问了哪个域名
- 命中了哪条规则
- 是否允许或阻断
- 最终从哪个节点或出口出去

系统不记录：

- 网页正文
- 页面标题
- 完整 HTTPS 路径
- 表单内容
- 账号密码
- Cookie
- 聊天内容
- 上传或下载文件内容
- 数据包正文

系统不做 HTTPS 中间人解密。

---

# 4. 系统组成

整个项目由以下四个主要部分组成。

## 4.1 Web 管理后台

技术方案：

- Vue 3
- TypeScript
- Vite
- Vue Router
- 轻量状态管理
- 浏览器原生 Fetch 或轻量请求封装

主要职责：

- 展示系统状态
- 管理用户和设备
- 管理节点和节点组
- 管理入站和出站
- 管理代理链
- 管理网站分流和阻断规则
- 查看访问记录
- 发布和回滚配置
- 管理系统设置

生产环境中，前端编译后的静态文件嵌入 Go 中央程序，不单独运行 Node.js 服务。

## 4.2 中央控制服务

技术方案：

- Go
- REST API
- SSE
- gRPC
- SQLite
- 模块化单体

主要职责：

- 管理员认证
- 用户与设备管理
- 节点和节点组管理
- 用户节点组授权
- 订阅生成
- 入站、出站和代理链管理
- 分流和网站控制
- 配置版本生成
- 节点配置发布
- 访问记录查询
- 流量统计
- 备份恢复

## 4.3 Node Agent

技术方案：

- Go 单文件程序
- systemd 或 OpenRC（Alpine，含 LXC）管理
- gRPC 双向连接
- mTLS 节点身份认证
- 本地轻量数据库或持久化队列

主要职责：

- 节点注册
- 节点证书管理
- 主动连接中央控制端
- 上报节点状态和协议能力
- 接收目标配置
- 编译代理核心配置
- 校验并应用配置
- 启动和管理代理核心
- 配置失败自动回滚
- 采集流量和域名访问记录
- 中央端断线时本地缓存记录

## 4.4 代理核心

第一阶段优先支持：

- Xray

后续可选支持：

- sing-box

按需使用：

- WireGuard

代理核心负责：

- 用户入站接入
- 用户身份识别
- 域名和 IP 分流
- Direct 出站
- Block 出站
- 协议代理出站
- 节点中转
- 链式代理
- 流量统计

---

# 5. 总体架构

```text
                        管理员浏览器
                             │
                        HTTPS / SSE
                             │
                ┌────────────▼────────────┐
                │      proxy-panel        │
                │      Go 单体程序         │
                │                         │
                │ Web 静态页面             │
                │ REST API                │
                │ 用户 / 节点 / 策略       │
                │ 订阅 / 发布 / 访问记录   │
                │ SQLite                  │
                └───────────┬─────────────┘
                            │
                       gRPC + mTLS
                            │
          ┌─────────────────┼─────────────────┐
          │                 │                 │
    ┌─────▼─────┐     ┌─────▼─────┐     ┌─────▼─────┐
    │ Node A    │     │ Node B    │     │ Node C    │
    │ Agent     │     │ Agent     │     │ Agent     │
    └─────┬─────┘     └─────┬─────┘     └─────┬─────┘
          │                 │                 │
     Xray/sing-box     Xray/sing-box     Xray/sing-box
          │                 │                 │
          └──────协议中转 / 代理链 / 出口──────┘
```

中央服务建议使用两个监听端口：

```text
443   Web、REST、SSE、用户订阅
9443  Node Agent gRPC + mTLS
```

仍然只运行一个中央程序。

---

# 6. 用户角色

第一版只保留两个后台角色。

## 6.1 Owner

Owner 拥有全部权限，包括：

- 管理后台管理员
- 创建、修改和删除用户
- 管理用户凭据、共享订阅与门户登录
- 管理节点
- 管理节点组
- 管理用户节点组授权
- 管理入站和出站
- 管理出口组和代理链
- 管理分流规则
- 管理网站访问控制
- 查看访问记录
- 发布配置
- 回滚配置
- 管理 Agent 和代理核心版本
- 备份与恢复
- 修改系统设置

## 6.2 Viewer

Viewer 只允许查看：

- 总览
- 节点状态
- 节点基础信息
- 流量统计

Viewer 默认不能：

- 修改任何配置
- 查看敏感凭据
- 查看详细访问记录
- 发布或回滚配置
- 管理用户和管理员

底层权限设计应保留细粒度权限点，以便后期增加 Operator 或 Auditor，但第一版不在页面中暴露复杂角色配置。

## 6.3 终端用户与门户

代理用户使用自己的用户名与 Owner 设置的密码登录 `/portal/login`；门户提供账号状态、订阅复制与二维码、授权节点、流量和修改密码。门户会话与管理员的表、Cookie、CSRF 状态和路由守卫独立，互相不能访问。禁用、到期或超额用户在登录开关开启时仍可登录查看原因，但不能取得订阅内容。Owner 的门户预览为只读，不签发 Token、不写数据；Viewer 使用后台只读视图。

---

# 7. 网站功能模块

网站沿用 Owner 的布局，当前入口如下（设备与订阅旧路由跳转到用户管理）：

```text
系统总览

用户管理（用户详情内管理订阅、登录与凭据）

服务器
├─ 服务器列表
├─ Agent 管理
├─ 代理核心
├─ 节点组
└─ 节点拓扑

节点配置
├─ 节点列表（入站、出站、出口组）
└─ 中转搭建（服务端与客户端方案）

分流控制
├─ 客户端策略（全局订阅规则模板）
├─ 节点策略
├─ 规则集
└─ 策略模拟

访问审计
├─ 全部访问记录
├─ 已阻断拦截
└─ 域名资产统计

系统管理
├─ 管理员
├─ 备份恢复
├─ 操作日志
└─ 系统设置
```

---

配置版本、发布、差异与选定版本回滚在节点详情内操作。终端用户另有门户入口：概览、订阅、节点、流量、账号。

# 8. 总览模块

总览页面用于快速了解系统状态，不做复杂的大屏监控。

主要展示：

- 在线用户数
- 在线设备数
- 在线节点数
- 当前连接数
- 今日上传流量
- 今日下载流量
- 今日阻断次数
- 异常节点数
- 最近访问域名
- 最近阻断域名
- 简单节点拓扑
- 最近配置发布结果

节点状态使用统一颜色：

- 绿色：正常
- 黄色：状态降级
- 红色：异常
- 灰色：离线
- 蓝色：配置发布或更新中

---

# 9. 用户管理模块

## 9.1 用户信息

每个代理用户包含：

- 用户名
- 显示名称
- 启用状态
- 到期时间
- 流量额度
- 当前已使用流量
- 并发设备限制（`device_limit`，0 表示不限）
- 估计在线设备数与执行范围
- 门户登录开关、密码与最后登录时间
- 流量重置周期及锚点（不重置、每月、每 30 个本地日）
- 允许使用的协议
- 授权节点组
- 备注

**并发设备限制（R7，取代 D2 的对应口径）**：旧 `maxDevices`、`maxConnections` 不再作为 API 字段，旧数据库列为兼容保留；新限制升级默认不限。Xray 通过在线 IP RPC、sing-box 通过用户日志与 Clash 连接源地址联接上报，按第 9.2 节准入。这里的设备是源地址估计，读不到显示“未知”，不等同 TCP 会话数，也不等同真实物理设备数。

**流量额度（D3）**：每用户只有**一个**额度（`traffic_limit` / `used_traffic`），跨全部节点累计；不存在每节点预算、预算转移或核内字节配额。面板按下列方式**软执行**：

1. Agent 每 5 秒采样账户计数写入本地队列，并在心跳中批量上报（面板当前指定 15 秒心跳）。
2. 面板把增量累加到 `used_traffic`（凭据能对应到设备时同时累加到设备用量）。
3. 用户处于超限、到期或禁用状态时，面板自动创建并部署一个**只调整用户访问面**的配置版本，把该用户的凭据从节点配置中移除；提额、清零、续期或启用同样触发一次自动版本；删除用户或设备同样自动移除其凭据，无需重新发布。自动版本以节点**已发布**状态为基线，除用户与入站的绑定外（见 22.1）不会带出未发布的 Owner 编辑。
4. **超额窗口**：在**正常路径**上（节点在线、读取与发布都成功、没有其他部署占用该节点），从超限到被切断最多经过一个上报周期（当前 15 秒）＋最长 60 秒的合并窗口＋发布耗时；每次重试再加一个窗口。这段时间的流量仍会放行并计入用量。这是被接受的取舍。
5. **每用户额度统计有账户计数能力的节点**：Xray 及携带 `with_v2ray_api`、并上报 `singbox_user_stats` 的 sing-box。官方 sing-box 无此能力时仍只报节点总量，每用户未知，页面说明未计入范围；qui sing-box 的用户流量开始计入统一额度是升级行为变化。协议载荷对比与原生核心验收仍待 CI/Owner，不能以本地假服务测试代替。

## 9.2 用户接入身份与设备限制

取消用户可管理的设备对象；每用户只有一个隐藏接入身份，沿用 `DeviceID` 与旧 Agent 合同，历史记录不删除。0113 迁移保留主身份，其他设备凭据撤销、链接失效；升级前必须检查备份与用户影响。

`device_limit` 为 0–100，0 表示不限。按跨入口节点的不同 IPv4、IPv6 /64 活跃源统计，按首次出现顺序保留名额；活跃窗口 60 秒，随后保留 120 秒，再提升最早等待的地址。Owner 可重置准入顺序。面板源地址只保存在内存中，两个视图均掩码，不入库、不写日志、不进备份；Xray 官方 CLI 所需的节点侧临时 JSON 为 0600，调用返回即删除。

NAT 下多端只算一个，双栈与切网可能多算；超限检测约有一个上报周期与投递延迟。Xray 阻止后续新连接，既有连接继续；sing-box 每约 2 秒关闭匹配连接。断联 3 分钟后门控过期放行，旧 Agent 或观测失败明确显示部分执行。WireGuard 一次一个设备且不计入限制；Xray SOCKS5 UDP 无用户归属，不能门控。Hysteria2/TUIC 的 UDP 数据报门控尚待专项验收（L8），QUIC 上的 TCP 流测试不代表数据报通过。

## 9.3 凭据管理

凭据类型根据协议确定，包括：

- 用户名和密码
- UUID
- Shadowsocks 密钥
- Trojan 密码
- WireGuard 密钥
- 订阅 Token

用户凭据与节点间中转凭据必须分开管理。

订阅 Token 以哈希查找，新签发 Token 另存 vault 加密副本，Owner 与本人可再次查看；升级前无加密副本的 Token 返回 `not_recoverable`，重置后才可查看。每用户一条共享订阅。用户可主动重置链接（D-c）；链接重置撤销旧 Token，凭据重置则在自动版本应用后使旧代理设置失效。登录密码与代理凭据分开管理。

---

# 10. 用户订阅与节点组

## 10.1 节点组授权

用户不直接绑定单个节点，而是绑定节点组。

```text
用户
 ↓
授权节点组
 ↓
展开组内节点
 ↓
过滤不可用节点和协议
 ↓
生成订阅
```

例如：

```text
User01

授权节点组：
- 日本节点
- 香港节点
```

日本节点组包含：

```text
Tokyo-01
Tokyo-02
Osaka-01
```

当管理员把 `Tokyo-03` 加入日本节点组后，User01 不需要重新授权，刷新订阅即可获得该节点。

## 10.2 节点组类型

节点组支持以下用途：

- `subscription`：用户订阅入口节点
- `relay`：节点间中转节点
- `egress`：系统出口节点
- `mixed`：混合用途

纯中转或纯出口节点可以设置为不在用户订阅中显示。

## 10.3 订阅过滤

订阅生成时需要执行：

1. 验证用户状态。
2. 验证用户是否到期。
3. 验证用户订阅 Token。
4. 获取用户授权节点组。
5. 展开组内节点。
6. 去除重复节点。
7. 过滤禁用节点。
8. 过滤维护中节点。
9. 过滤不允许订阅的节点。
10. 获取节点可用入站。
11. 过滤用户不允许使用的协议。
12. 输出订阅内容。

v1.1 订阅格式支持：

- 通用 URI 列表
- Base64 URI 列表
- sing-box JSON
- mihomo（`clash`，别名 `mihomo`、`clash-meta`）
- legacy Clash（`clash-legacy`，别名 `clash-premium`）

通用 URI 与 Base64 列表带不了固定证书，因此省略钉了 `trusted_ca_pem` 的 Hysteria2/TUIC 端点（不因此报错，其余链接照常输出）；这类端点由 sing-box 格式输出，并写入 `tls.certificate`。

URI、Base64 和 sing-box 保持旧输出兼容；sing-box 仍是出站片段，不是完整客户端配置。mihomo 支持全局客户端规则、provider 与客户端链；legacy Clash 过滤 VLESS、Hysteria2、TUIC、WireGuard、GEOSITE 与 RULE-SET/provider，并给 Owner 预览警告。mihomo 可用证书指纹表达固定证书的 QUIC 端点。

订阅路径是 Token → 用户 → 已发布节点及授权导入源 → 中立代理模型 → 格式导出器。`?format=` 优先，未知格式返回 400；其次为已验证 UA 表，再回退系统默认。UA 开关已实现，但 Phase 0 A.7 无已验证客户端字符串，当前匹配表为空，不能宣称自动识别已验收；一键导入协议也待核实，现提供复制与二维码。响应包含流量、到期、更新间隔与下载文件名等订阅头。

全局模板存在 `system_settings.client_rules`，有版本冲突检查与预设，按用户授权展开 `@all`、`@nodeGroup:<id>`、`@source:<id>`；服务端生成 YAML。客户端规则不替代节点策略。外部源仅 HTTPS，逐跳解析并固定经校验的公网 IP，限制 2 MiB / 15 秒；定时抓取加密缓存，订阅请求不抓取。导入节点仅用于客户端，不参与 Agent 发布、用户额度或设备门控。支持范围及客户端链限制见计划 L5–L6。

---

# 11. 节点管理模块

## 11.1 节点列表

节点列表显示：

- 节点名称
- 在线状态
- 公网 IP
- 操作系统
- CPU 使用率
- 内存使用率
- 磁盘使用率
- 当前连接数
- 今日流量
- Agent 版本
- 代理核心
- 核心版本
- 当前配置版本
- 最后心跳时间

## 11.2 节点详情

节点详情包含：

- 基础信息
- 系统资源
- 协议能力
- 入站服务
- 出站服务
- 所属节点组
- 当前连接
- 今日流量
- 最近访问域名
- 当前配置版本
- 上一成功配置
- 发布记录
- 节点事件
- 出站网络（核心出站源 IP/网卡、服务器强制出站 IPv4/IPv6，未配置显示“—”）

## 11.3 节点注册

节点注册流程：

```text
Owner 创建节点
       ↓
生成一次性注册码
       ↓
节点安装 proxy-agent
       ↓
Agent 本地生成私钥
       ↓
提交注册码、公钥和机器信息
       ↓
中央端签发节点证书
       ↓
注册码立即失效
       ↓
Agent 使用 mTLS 建立长期连接
```

节点私钥只保存在节点本地，不上传中央服务。

注册节点成功后，面板对话框同时显示一键安装命令（`deploy/linux/node-install.sh`）：从 GitHub Release 下载与面板同版本的归档并校验 `SHA256SUMS`，按公网证书或面板显示的固定 SHA-256 建立对面板证书的信任，然后登记并启动 Agent（见[部署与升级](docs/deployment.md)第 4 节）。

**删除节点**：尚未登记且没有身份的节点走“取消登记”，记录直接移除；已登记的节点是**逻辑删除**——写入 `nodes.deleted_at`、吊销证书、退出所有节点组并从订阅与各列表（含选择器）中消失，流量与访问记录保留，且不能再次启用；同一台服务器要重新接入时请作为新节点登记。删除前必须先处理仍引用它的入站、出站（含其他节点指向它的中转出站）、出口组、代理链与分流规则，并等进行中的批量发布结束，否则返回 409 `node_delete_blocked` 并列出阻塞项（`GET /api/v1/nodes/{id}/deletion-preview` 可先查看）。面板无法停止服务器上的 Agent，删除不会结束远端代理；删除还会关闭该节点自己的待发布配置、核心/Agent 更新任务与排队中的健康检查。运维步骤见[运维手册](docs/operations.md)第 15 节。

## 11.4 节点状态

节点状态统一为：

- `pending`
- `online`
- `degraded`
- `offline`
- `applying`
- `rollback`
- `disabled`
- `error`

---

# 12. 入站代理模块

入站表示用户连接节点时使用的协议。

第一阶段计划支持：

- HTTP CONNECT
- SOCKS5
- Shadowsocks
- VLESS
- Trojan
- WireGuard

后续可扩展：

- Hysteria2
- TUIC
- VMess
- TUN

一个节点可以同时存在多个入站，例如：

```text
Node A

VLESS       :443
SOCKS5      :1080
Shadowsocks :8388
```

入站配置主要包括：

- 名称
- 所属节点
- 协议
- 监听地址
- 监听端口
- 传输方式
- 安全方式
- 允许用户
- 是否显示在订阅中
- 是否启用
- 默认出站

用于节点间中转的内部入站，应设置为不在用户订阅中显示。

---

# 13. 出站代理模块

出站表示节点收到用户流量后，以什么方式继续访问。

支持的出站类型：

- Direct
- Block
- HTTP
- SOCKS5
- Shadowsocks
- VLESS
- Trojan
- WireGuard
- 远程节点
- 出口组
- Proxy Chain

出站主要字段：

- 名称
- 所属节点
- 出站类型
- 目标节点
- 目标地址
- 目标端口
- 使用协议
- 认证凭据
- TCP 支持情况
- UDP 支持情况
- 是否启用
- 健康状态
- 失败处理方式

失败处理方式包括：

- Block
- Direct
- 备用出站
- 备用出口组
- 备用代理链

对于要求固定出口的网站，默认失败策略建议为 Block 或备用出口，不自动 Direct。

## 13.1 节点出站网络

节点可以在“节点详情 → 节点设置”里指定出站网络。改动在下一次**发布配置**时生效；字段留空并保存表示清除。

- **核心出站源 IP / 核心出站网卡**：只影响本节点代理核心转发的流量。Xray 渲染为出站 `sendThrough` 与 `streamSettings.sockopt.interface`，sing-box 渲染为 `inet4/inet6_bind_address` 与 `bind_interface`；出口组成员同样固定，sing-box 的 `selector`/`urltest` 组本身不带拨号字段（上游不接受），入站 WireGuard 端点只监听、不改写。地址族按源地址固定（Xray `ForceIPv4`/`ForceIPv6`，sing-box `ipv4_only`/`ipv6_only`）；目标是另一族的字面量 IP 时连接失败。
- **服务器强制出站 IPv4 / IPv6**：整台服务器经该网卡新建的出站连接都改用此源地址（核心出站源 IP 除外）；仅 Linux，失败自动回滚。执行方式、双栈注意事项与排查命令见[运维手册](docs/operations.md)。

面板只校验语法（地址须为单播、`host_source_ipv4` 为 v4、`host_source_ipv6` 为 v6、网卡名符合 Linux 命名）；地址或网卡是否存在由节点在应用配置时判断，缺失时按临时失败码重试。

---

# 14. 协议转换中转

系统必须支持以下场景：

> 用户使用协议 A 连接节点 A，节点 A 再使用协议 B 将流量转发到节点 B。

完整流程：

```text
用户
 ↓ 协议 A
节点 A
 ↓ 协议 B
节点 B
 ↓ Direct
Internet
```

节点 A 内部处理过程：

```text
终止协议 A 入站会话
       ↓
取得逻辑 TCP/UDP 连接
       ↓
识别用户和目标
       ↓
匹配本地分流规则
       ↓
建立协议 B 出站会话
       ↓
转发到下一节点
```

示例：

```text
用户
 ↓ VLESS
Node A
 ↓ Trojan
Node B
 ↓ Direct
Netflix
```

用户不需要知道 Node B 的地址和凭据，Node B 只作为系统内部中转或出口节点。

节点间凭据由中央端生成和维护，与用户凭据完全分离。

---

# 15. 出口组模块

出口组用于系统内部对多个出口进行统一管理。

例如：

```text
US-Exit
├─ LA-01
└─ LA-02
```

第一版支持的选择方式（按上游能力，D4）：

| 策略 | Xray | sing-box |
| --- | --- | --- |
| 主备（`primary`） | 由 Agent 在节点上选择当前成员，只渲染选中的成员（上游没有“首健康”策略） | 同左 |
| 简单轮询（`round_robin`） | 渲染 `routing.balancers` 的 `roundRobin` + `observatory` + `fallbackTag` | 渲染 `urltest` |
| 延迟优先（`latency`） | 由 Agent 在节点上选择（leastPing 语义） | 渲染 `urltest` |
| 权重（`weighted`） | 已移除 | 已移除 |

权重策略在上游没有对应实现，已从校验、API 与页面移除；迁移 `0104_egress_weighted_to_round_robin.sql` 把既有权重出口组改写为 `round_robin`，读取旧配置版本时同样按 `round_robin` 解释。

出口组与用户订阅节点组必须分开：

- 用户节点组决定用户客户端可以连接哪些入口节点。
- 出口组决定节点内部流量最终从哪里出去。

出口组的备用策略按配置执行，**固定出口失败不会隐式 Direct**（见第 13 节失败处理）。

---

# 16. 代理链模块

代理链支持节点之间的多跳转发。

第一版支持：

```text
A → B
```

以及：

```text
A → B → C
```

第一版不支持无限多跳。

每一跳可以使用不同协议，例如：

```text
用户
 ↓ Shadowsocks
Node A
 ↓ Trojan
Node B
 ↓ WireGuard
Node C
 ↓ Direct
Internet
```

代理链包含：

- 链路名称
- 入口节点
- 中转节点
- 出口节点
- 每一跳协议
- 每一跳出站
- 最终出口方式
- 失败处理策略
- 健康状态

代理链保存和发布前必须检查：

- 是否存在重复节点
- 是否存在路由环路
- 是否超过最大跳数
- 每一跳协议是否受支持
- 每一跳 TCP/UDP 能力是否匹配
- 端口是否冲突
- 内部入站是否存在
- 节点间凭据是否有效
- 最终出口是否存在

代理链配置发布顺序：

```text
先发布出口节点
再发布中转节点
最后发布入口节点
```

回滚顺序相反：

```text
先回滚入口节点
再回滚中转节点
最后回滚出口节点
```

---

# 17. 分流模块

分流模块统一处理：

- 网站分流
- 网站阻断
- 直接连接
- 指定节点出口
- 指定代理链
- 指定出口组

规则可以匹配：

- 用户
- 入口节点
- 入口节点组
- 域名
- 域名后缀
- 网站服务分类
- IP
- CIDR
- 端口
- TCP
- UDP
- 时间段

规则动作包括：

- Direct
- Block
- 指定 Outbound
- 指定出口组
- 指定 Proxy Chain

规则按优先级从高到低执行，命中第一条有效规则后停止。

每个节点必须存在默认规则。

示例：

```text
规则 10：TikTok
动作：Block

规则 20：Netflix
动作：Trojan-To-Node-B

规则 30：OpenAI
动作：SG-Exit

默认规则：
动作：Direct
```

---

# 18. 服务规则模块

服务规则用于将常见网站的域名集合封装成易于选择的服务。

第一版可提供：

- Netflix
- Disney+
- YouTube
- OpenAI
- Google
- GitHub
- Telegram

管理员不需要手工维护大量域名，只需要设置：

```text
Netflix → Node B
OpenAI → SG-Exit
YouTube → Direct
```

系统内部将服务规则编译为域名或 IP 匹配规则。

系统也支持创建自定义服务分类，例如：

```text
公司开发服务
├─ git.example.com
├─ registry.example.com
└─ api.example.com
```

服务规则更新必须经过配置版本发布，不直接在线修改节点运行规则。

---

# 19. 网站访问控制

网站访问控制与分流使用同一套策略引擎。

支持控制：

- 某个用户不能访问哪些网站
- 某个节点不能访问哪些网站
- 某个节点组不能访问哪些网站
- 某个网站必须从指定节点出去
- 某类网站只允许在指定时间访问

示例：

```text
Node A

Netflix
→ Node B

TikTok
→ Block

GitHub
→ Direct

其他
→ Direct
```

访问控制支持：

- 精确域名
- 域名后缀
- 通配域名
- 自定义网站分类
- 内置服务规则
- IP/CIDR

---

# 20. 网站访问记录

v1.1 页面、筛选、策略作用域与模拟器不再暴露设备字段；历史数据中的设备 ID 与隐藏身份仍保留以兼容统计和墓碑。按用户查询，不提供终端用户访问记录入口。

系统只记录域名级访问结果。

记录字段包括（设备仅是内部历史/隐藏身份字段，不展示）：

- 时间
- 用户
- 设备
- 入口节点
- 入站协议
- 访问域名
- 目标 IP
- 目标端口
- 命中规则
- 执行动作
- 出站协议
- 代理链
- 最终出口节点
- 允许或阻断
- 连接数量
- 上传流量
- 下载流量

**字段口径（D4）**：

- **上传/下载流量是按域名的可空字段**。官方上游核无法提供每域名精确字节，未知时为 NULL，页面显示“—”，**不写零**。用户与节点级的总量仍来自核心统计。
- **最终出口节点记录的是“配置出口”**：该节点按策略解析出的出站或出口组所选成员。它表示配置，不证明跨节点链路实际到达了哪个出口节点；跨中转的实际出口无法确认，页面会分别标注“本节点直连”和“配置出口未确认”。
- 跨分钟仍存活的连接不会每分钟重复计为新连接；连接数不等于实时并发或请求次数。

示例：

```text
时间：15:20
用户：User01
入口节点：Node A
入站协议：VLESS
访问域名：netflix.com
命中规则：Netflix-US
出站协议：Trojan
最终出口：Node B
结果：Allowed
```

阻断示例：

```text
时间：15:22
用户：User01
入口节点：Node A
访问域名：tiktok.com
命中规则：TikTok-Block
结果：Blocked
```

如果节点无法确定域名，则记录目标 IP，不根据网页内容猜测域名。

---

# 21. 访问记录轻量化处理

v1.1 内部身份字段继续用于流量归属与补传，界面隐藏设备维度。月度节点流量按面板时区的半开区间汇总保留的 `account_traffic_samples`，API 标记 `scope=retained_account_samples`；样本被 HourlyDays 清理后不能还原完整历史月份（L9），不把累计心跳或 UTC 日桶拼成完整月流量。

系统不应为每一个 TCP 连接立即向中央数据库写入一行记录。

节点端按时间窗口聚合：

```text
时间桶
用户
节点
域名
动作
出口
```

例如一分钟内访问 Netflix 建立 50 个连接，节点只需上传一条聚合记录：

```text
时间：15:20
用户：User01
节点：Node A
域名：netflix.com
连接数：50
动作：Proxy
出口：Node B
上传：2 MB
下载：90 MB
```

实际默认值（以源码为准）：

- 允许访问记录保留 30 天
- 阻断记录保留 90 天
- 小时流量保留 90 天
- 日流量长期保留
- 节点本地缓存上限 100 MiB（`internal/agent/spool` 的 `DefaultLimit`）
- 上报周期 15 秒（面板下发的 `HeartbeatInterval`）；节点每 5 秒把核心统计采样写入本地队列，心跳时批量上传
- Xray 访问日志位于节点运行时目录 `/var/lib/proxy-agent/runtime/access.log`：Agent 读完即清空（约 100 ms 一次轮询），核心停止时再清空，因为明文 HTTP 行会带上 URL 路径与查询串；超过 20 MiB 仍会截断作为兜底；sing-box 的访问行来自核心日志

节点离线时，数据先保存在本地队列；恢复连接后批量补传，收到确认后才删除。缓存写满时优先淘汰“允许访问”的最旧记录，阻断记录与流量记录优先保留。面板拒绝的流量批次（同一批次连续 3 次）进入隔离：仍保留在本地、不被淘汰，后面的批次照常上传，隔离 1 小时后或 Agent 重启时重新尝试。删除用户或设备前产生、尚未上传的流量按批次所属修订照常入账（凭据墓碑），只计入仍存在的用户或设备。

**每域名字节**：见第 20 节，官方核无法提供时列为空并显示“—”。

---

# 22. 配置发布与回滚

后台配置修改后，不直接覆盖节点当前运行配置。

发布流程：

```text
管理员修改配置
       ↓
保存业务配置
       ↓
生成配置 Revision
       ↓
校验用户、节点和协议能力
       ↓
生成节点 DesiredState
       ↓
发送到 Node Agent
       ↓
Agent 编译核心配置
       ↓
执行核心原生配置校验
       ↓
保存新版本
       ↓
切换并启动核心
       ↓
执行健康检查
       ↓
成功或自动回滚
```

失败处理：

- 编译失败：不修改当前配置
- 配置校验失败：不修改当前配置
- 核心启动失败：恢复上一成功版本
- 健康检查失败：自动回滚
- 中央服务失联：保持当前成功版本
- Agent 启动时已安装的核心运行不了保留的成功版本：核心保持停止，Agent 照常连接中央端并上报核心不健康，等待新版本

每个节点默认保留最近 3～5 个成功配置版本。

## 22.1 自动额度版本

除 Owner 手工发布外，面板还会自动发布一种**只调整用户访问面**的版本（D3）：

```text
流量入库 / 定时 tick
       ↓
判定用户状态（禁用 > 超限 > 到期）
       ↓
状态发生变化时按节点排队（同一节点 60 秒内合并）
       ↓
以节点「已发布」状态为基线，只替换用户与凭据
       ↓
创建并部署自动 Revision（描述形如 auto: user <name> <reason>）
```

- 除用户与入站的绑定外（见下条），自动版本不会带出未发布的 Owner 编辑（基线是已发布状态）。
- 面板重启不会引发发布风暴：状态机启动时先从数据库播种，只有状态**变化**才触发；随后对每个有已发布状态的节点对账一次，只为「用户访问面与节点正在运行的配置不一致」的节点创建版本（例如面板重启期间超额的用户，或合并窗口里丢失的工作）；启动对账读取状态失败时，每个 tick 重跑一次，直到一次完整读完为止。
- 节点繁忙或等待 Agent 结果时等下一个窗口；读取、创建、部署失败或 Agent 上报失败/回滚时，在下一个窗口用**当前已发布状态**重新生成版本重试；连续 3 次失败后停止重试并告警 Owner（`system.alert` ＋ 操作日志「自动额度发布失败」）。Owner 已保存的草稿无法渲染时，自动版本退回按已发布状态生成、只移除不活跃用户，恢复类转换要等草稿能再次渲染。
- 用户与入站的绑定取自当前草稿，因此「把某个用户的凭据绑定到某个入站」这类未发布编辑可能被自动版本带出；中转凭据、出站、出口组、代理链与分流规则始终按已发布状态。
- 入站的启用状态不取自草稿：渲染器把「没有用户凭据」的入站写成 `enabled=false`，所以每个版本另记 Owner 当时启用的入站集合（`config_revisions.inbound_enabled_json`），自动版本原样继承基线版本的记录。凭据回到已发布状态下被禁用的入站时，只有记录说 Owner 曾启用它才重新启用，否则保持禁用（凭据仍可挂在禁用的入站上）；记录为 NULL（本列之前发布的版本）时同样保守保持禁用，直到 Owner 下一次发布。Owner 手动禁用的入站因此永远不会被自动版本启用。
- 目标节点是该用户**当前授权**的节点，加上**已发布状态里仍持有该用户凭据**的节点（例如授权已移除但尚未发布），所以禁用也能到达后者。
- 自动额度版本和其他版本一样会在该节点重启核心（先停旧核心再起新核心），该节点上**所有用户**的既有连接都会短暂中断；60 秒合并窗口限制了重启频率。
- 中央离线时节点继续运行旧配置，直到重新连接并收到版本。
- 每次部署或回滚成功后，面板为该节点排队一次对账（同样受 60 秒合并窗口约束）：回滚或部署旧修订重新带回已禁用用户、或漏掉已重新启用用户时，由一个自动版本纠正；节点已一致时不产生版本。
- 删除用户或设备后，面板为已发布状态里仍持有该用户凭据的节点排队一次（原因 `deleted` 或 `device deleted`，同样发出 `user_access` 告警），自动版本按当前数据渲染、不再含被删除的凭据；节点已一致时不产生版本。

## 22.2 核心更新流程

核心升级与配置发布是两条独立路径：

```text
Owner 录入 组件 / 稳定版本号 / 允许的 GitHub 资源 URL / 归档 SHA-256
       ↓
面板校验组件、版本号、摘要、URL 前缀与平台匹配，排队
       ↓
Agent 校验 URL 前缀与重定向主机，校验归档 SHA-256
       ↓
解包（拒绝符号链接、路径穿越与多余候选）并切换镜像
       ↓
版本校验与健康检查
       ↓
成功；失败则回滚到上一镜像
```

- Xray 只接受官方发布前缀；sing-box 还接受严格匹配的 qui 发布归档 `sing-box-<版本>-qui.<序号>-linux-<架构>.tar.gz`。面板与 Agent 都检查来源；只有声明新能力的 Agent 才能排队 qui 来源，旧 Agent 明确拒绝。没有可信摘要不安装。
- 卡住的任务在节点重连时关闭，Owner 也可以显式清理。
- Agent 的核心更新结果在收到面板 Ack 前一直保留在 core-update worker 的内存队列里（先发最早的一个）：连接在等 Ack 时断开就在下一个会话重发，面板对相同结果幂等并确认；面板以 `FailedPrecondition` + `core update result rejected` 永久拒绝同一结果（例如任务已被 Owner 清除）连续 3 次后 Agent 丢弃并记 Warn，其他错误（网络、`Unavailable`、上下文取消）永不丢弃。该队列只在内存里，Agent 重启会丢失，由上面的重连关闭与 Owner 清理兜底。
- **Agent 自身的更新仍使用签名清单**（Ed25519，公钥固定在配置里，按安装历史防重放），不使用 URL + SHA-256。
- 早期版本中的 receipt/outcome 更新日志（journal）与受控核签名目录路径已删除；核心更新不再经过签名目录，签名目录只剩 Agent 自更新在用。

---

# 23. Node Agent 模块设计

```text
proxy-agent
│
├─ Identity
│   └─ 节点 ID、私钥、证书
│
├─ Control
│   └─ gRPC、心跳、重连、消息确认
│
├─ Capability
│   └─ 系统、核心和协议能力检测
│
├─ Reconciler
│   └─ 期望状态与实际状态协调
│
├─ Compiler
│   ├─ Xray 配置编译器
│   └─ sing-box 配置编译器
│
├─ Core Supervisor
│   └─ 启动、停止、重载、健康检查
│
├─ Collector
│   └─ 流量、状态和域名记录采集
│
├─ Spool
│   └─ 断线缓存和批量补传
│
└─ Updater
    └─ Agent 和核心更新
```

更新方式（V1.2）：

- **代理核心**：允许的官方 / qui GitHub 发布资源 URL + 归档 SHA-256，来源限制见 22.2；qui sing-box 只对未修改的上游源码增加上游构建标签，不是补丁核。Agent 再次校验后切换，健康失败回滚。
- **Agent 自身**：Ed25519 签名清单（公钥固定在配置里，按安装历史防重放），失败回滚到已确认镜像。
- 早期版本的 receipt/outcome 更新日志（journal）、候选镜像确认恢复模式与 supervisor 巡检模式已删除；受管 Agent 启动时按安装历史自行完成中断的切换。

Node Agent 不允许提供：

- 任意 Shell
- Web Terminal
- 任意系统命令执行
- 上传脚本并执行
- 未定义的远程操作

只允许执行固定操作：

- 应用配置版本
- 回滚配置版本
- 重启代理核心
- 重新加载核心
- 执行健康检查
- 轮换节点证书
- 更新 Agent
- 更新代理核心
- 应用服务器强制出站源地址（固定 nft 规则，独立 oneshot 单元，仅 CAP_NET_ADMIN）
- 应用有 TTL 的设备门控（Xray 官方运行时规则 API / sing-box Clash 连接关闭），不走配置 Revision，不重启核心
- TCP-connect 探测（已解析 IP、端口、次数、超时与截止时间；Agent 再校验，最多 10 次、每次 3 秒，无 shell）
- 应用端口转发入站（Xray `dokodemo-door` / sing-box `direct` override，按能力拒绝旧 Agent；经正常配置发布）

TCP 探测只测建连耗时与失败比例，不是带宽、ICMP 或 UDP 测试。手动历史每节点/目标最多保留 100 条。定时检测每 5 分钟针对启用、未删除、45 秒内有心跳且支持探测的节点执行，每目标 3 次、每次 3000 ms，系统最多选择 5 个定时目标。队列按 source 隔离，手动优先，已发出的探测结束或 2 分钟期限到达后才派发下一条。定时结果经 ValidateResult 校验后写入独立采样表，保留 7 天；当前 Agent 只提供 medianMs，avgMs/minMs/maxMs 返回 null。删除目标级联删除采样；逻辑删除节点不再提供采样查询，记录等待保留期清理。端口转发没有用户身份，不进入订阅、用户额度、设备限制或用户域名归属。服务器调优仍只读，新增特权操作未实现（L2）。

---

# 24. 中央端模块设计

中央控制端后端模块如下：

```text
auth          管理员登录和权限
user          用户、隐藏接入身份和凭据
portal        终端用户登录、独立会话和本人数据
node          节点、节点组和节点身份
subscription  用户订阅生成
proxy         入站、出站、出口组和代理链
policy        分流、服务规则和网站控制
deployment    配置编译、发布和回滚
access        域名访问记录和流量统计
dashboard     总览聚合查询
connections   面板内存中的实时连接快照（不落库）
system        设置、备份、版本和操作日志
```

推荐模块目录结构：

```text
internal/panel/user/
├─ model.go
├─ service.go
├─ repository.go
├─ repository_sqlite.go
├─ handler_http.go
├─ dto.go
└─ errors.go
```

模块化开发约束：

1. 每个模块只能通过自己的 Repository 修改所属数据。
2. 其他模块不能直接修改该模块的数据表。
3. 跨模块操作通过接口或应用编排服务完成。
4. 协议配置必须经过类型化结构校验。
5. 不允许直接把未经校验的 JSON 下发给节点。
6. Dashboard 只负责查询和聚合，不直接修改业务数据。
7. 配置发布必须统一经过 Deployment 模块。

---

# 25. 前端模块设计

前端按照业务模块组织：

```text
web/src/
├─ app/
│  ├─ router.ts
│  ├─ auth.ts
│  └─ api.ts
│
├─ modules/
│  ├─ auth/
│  ├─ dashboard/
│  ├─ users/
│  ├─ nodes/
│  ├─ proxy/
│  ├─ routing/
│  ├─ access/
│  └─ system/
│
├─ components/
├─ layouts/
├─ types/
└─ styles/
```

每个业务模块包含：

```text
pages/         页面组件
components/    模块内组件（按需）
api.ts         模块接口封装
```

路由统一在 `web/src/app/router.ts` 集中注册，共享类型在 `web/src/types/index.ts`。

前端轻量化要求：

- 不使用服务端渲染
- 生产环境不运行 Node.js
- 页面按路由懒加载
- 只保留少量全局状态
- 列表使用服务器分页
- 不一次加载全部访问记录
- 实时状态使用一条 SSE 连接
- 不为普通 CRUD 使用 WebSocket
- 通用表格、状态标签和确认弹窗统一复用

---

# 26. 数据库设计

中央数据库使用 SQLite。

建议数据库路径：

```text
/var/lib/proxy-panel/panel.db
```

SQLite 仅运行在中央服务器本地磁盘，不放在网络共享目录。

主要数据表如下。

## 26.1 管理员

```text
admins
sessions
admin_operations
```

## 26.2 用户与设备

v1.1 的 `devices` 只保存每用户一个隐藏接入身份；不再提供设备管理。

```text
users
devices
credentials
subscription_tokens
```

## 26.3 节点与节点组

```text
nodes
node_identities
node_groups
node_group_members
user_node_group_grants
```

## 26.4 代理配置

```text
inbounds
outbounds
outbound_groups
outbound_group_members
proxy_chains
proxy_chain_hops
```

## 26.5 分流与网站控制

```text
policies
service_sets
service_set_entries
custom_domain_entries
```

## 26.6 配置发布

```text
config_revisions
deployment_targets
```

## 26.7 访问记录和流量

```text
access_rollups
traffic_hourly
traffic_daily
node_status_snapshots
```

## 26.8 系统

```text
system_settings
backups
```

协议特有配置可以使用 JSON 字段，例如：

```text
inbounds.settings_json
outbounds.settings_json
policies.scope_json
policies.match_json
policies.action_json
nodes.capabilities_json
```

JSON 字段必须在 Go 中解析为明确的数据结构，不允许无结构透传。

## 26.9 v1.1 新增迁移

| 迁移 | 实际存储变化 |
| --- | --- |
| 0114 `end_user_accounts` | `users.password_hash/login_enabled/last_login_at`；独立 `user_sessions` 与过期索引；`subscription_tokens.encrypted_token` |
| 0115 `device_limit` | `users.device_limit`（0–100，默认 0）；源地址与门控租约不落库 |
| 0116 `subscription_sources` | `subscription_sources`、`imported_proxies`、`user_source_grants`；`canvas_schemes.client_published_layout` |
| 0117 `node_meta` | `node_meta`：地区、备注、续费日期/周期/价格/币种、提醒天数与每日提醒标记 |
| 0118 `traffic_reset` | `users.traffic_reset_cycle/traffic_reset_anchor/traffic_reset_at`，从旧备注迁移周期并去除元数据前缀 |
| 0119 `tcp_probes` | `nodes.tcp_probe_supported`；`probe_targets`、`node_tcp_probes` 与历史索引；转发复用入站设置，不新增转发表 |
| 0120 `latency_samples` | `probe_targets.scheduled`（默认关闭，触发器限制最多 5 个）；`node_tcp_probes.source/dispatched_at` 与单节点在途唯一索引；`node_latency_samples`（REAL 毫秒，含 median_ms；按节点/时间索引，保留 7 天） |

备份校验、清单与恢复覆盖这些结构。恢复结束门户会话；当前安全状态、Token 与授权按模块的保守合并规则处理，不用旧备份恢复已撤销访问。排队探测在恢复时过期，运行能力待重新上报。

---

# 27. API 规划

API 统一使用：

```text
/api/v1
```

## 27.1 身份认证

```text
POST /api/v1/auth/login
POST /api/v1/auth/logout
GET  /api/v1/auth/session
PUT  /api/v1/auth/password
```

## 27.2 用户

```text
GET    /api/v1/users
POST   /api/v1/users
GET    /api/v1/users/{id}
PUT    /api/v1/users/{id}
DELETE /api/v1/users/{id}
```

## 27.3 节点组授权与移除的设备 API

```text
POST   /api/v1/users/{id}/node-groups/{groupId}
DELETE /api/v1/users/{id}/node-groups/{groupId}
```

旧设备路由返回 410 `devices_removed`；订阅 Token 请求携带 `deviceId` 同样拒绝。用户 API 不接受旧的最大设备数/最大连接数字段，使用 `deviceLimit`。

## 27.4 节点

```text
GET    /api/v1/nodes
GET    /api/v1/nodes?deleted=1
GET    /api/v1/nodes/{id}
GET    /api/v1/nodes/{id}/deletion-preview
POST   /api/v1/nodes/enrollment-token
PUT    /api/v1/nodes/{id}
DELETE /api/v1/nodes/{id}
```

`GET /api/v1/nodes` 默认只返回未删除的节点；`?deleted=1` 只返回已删除的节点（Owner 与 Viewer 都可读）。`DELETE` 需要 Owner：未登记节点等同取消登记（记录移除），已登记节点执行逻辑删除，被引用时返回 409 `node_delete_blocked`，阻塞项（`blockers: [{kind,id,name}]`）位于错误体中（见第 11.3 节）。

`GET /install/ca.pem` 无需登录，返回面板 Web 证书链顶层的 PEM（`application/x-pem-file`，`Cache-Control: no-store`），供节点安装脚本建立对面板证书的信任（见第 11.3 节）；开发模式或证书不可读时返回 404。

## 27.5 节点组

```text
GET    /api/v1/node-groups
POST   /api/v1/node-groups
PUT    /api/v1/node-groups/{id}
DELETE /api/v1/node-groups/{id}
```

## 27.6 代理配置

```text
GET/POST/PUT/DELETE /api/v1/inbounds
GET/POST/PUT/DELETE /api/v1/outbounds
GET/POST/PUT/DELETE /api/v1/outbound-groups
GET/POST/PUT/DELETE /api/v1/proxy-chains
```

## 27.7 分流

```text
GET/POST/PUT/DELETE /api/v1/policies
GET                    /api/v1/service-sets
POST                   /api/v1/policies/simulate
```

## 27.8 访问记录

```text
GET /api/v1/access-records
GET /api/v1/access-records/blocked
GET /api/v1/access-records/domains
GET /api/v1/traffic/summary
```

## 27.9 配置版本

```text
GET  /api/v1/revisions
POST /api/v1/revisions
POST /api/v1/revisions/{id}/deploy
POST /api/v1/nodes/{id}/rollback
```

## 27.10 用户订阅

```text
GET /sub/{token}
GET /sub/{token}?format=base64
GET /sub/{token}?format=sing-box
```

## 27.11 实时状态

```text
GET /api/v1/events
```

SSE 只推送：

- 节点状态变化
- 配置发布状态
- 总览统计变化
- 系统告警

## 27.12 v1.1 接口补充

以下路径均以 `/api/v1` 为前缀；写操作需要相应 CSRF。Owner 接口与门户接口互相拒绝对方会话。

```text
PUT    /users/{id}/login
DELETE /users/{id}/sessions
POST   /users/{id}/credentials/reset
GET    /users/{id}/subscription-link
GET    /users/{id}/subscription-preview
GET    /users/{id}/portal-preview
GET    /users/{id}/devices-online
POST   /users/{id}/devices-online/reset
GET    /subscription-formats
GET/PUT /subscription-rules
GET    /subscription-rules/presets
GET/POST /subscription-sources
PUT/DELETE /subscription-sources/{id}
POST   /subscription-sources/{id}/refresh
GET    /imported-proxies
GET/PUT /nodes/{id}/meta
GET/POST /probe-targets
PUT/DELETE /probe-targets/{target}
GET/POST /nodes/{id}/tcp-probes
GET    /nodes/{id}/latency?hours=24
POST   /inbounds/{id}/reachability
POST   /nodes/{id}/publish
GET    /nodes/{id}/revisions
GET/POST /canvas-schemes
PUT/DELETE /canvas-schemes/{id}
POST   /canvas-schemes/{id}/publish
POST   /portal/auth/login
POST   /portal/auth/logout
GET    /portal/auth/session
PUT    /portal/auth/password
GET    /portal/me
GET    /portal/traffic
GET    /portal/nodes
GET    /portal/devices-online
GET    /portal/subscription
POST   /portal/subscription/reset
```

`GET /sub/{token}/rules/{id}` 用订阅 Token 授权，输出客户端 classical provider 文本，撤销 Token 后返回 403；不使用管理员 Cookie。格式与协议矩阵见相应文档。

---

# 28. gRPC 节点通信

节点通信建议只设计两个主要方法：

```text
Enroll
Connect
```

其中 `Connect` 为双向流。

Agent 上报消息包括：

- Hello
- Heartbeat
- Capabilities
- ApplyResult
- HealthResult
- TrafficBatch
- AccessBatch
- UpdateResult
- CertificateRequest

中央端下发消息包括：

- Welcome
- DesiredState
- ApplyCommand
- RollbackCommand
- HealthCommand
- CertificateRotate
- UpdateCommand
- Ack

每条消息包含：

- 节点 ID
- 消息 ID
- 顺序号
- 时间
- 配置 Revision
- 消息类型

节点保存最后确认的消息顺序，避免断线重连后重复执行配置命令。

---

# 29. 安全设计

## 29.1 后台安全

终端用户认证与管理员认证隔离：门户 Cookie 为 `qui_portal_session`（Secure 时使用 `__Host-` 前缀），HttpOnly、SameSite=Strict、Path=/；会话表只存 Token 哈希。复用密码哈希、限流、同源 CSRF 与会话时长策略。禁用登录、改密码、撤销会话和删除用户立即结束相应会话；恢复备份结束门户会话。门户仅从会话取用户 ID，不接受请求指定其他用户；不展示域名访问记录。订阅链接不存浏览器 localStorage。

- 管理员密码安全哈希存储
- HTTPS
- 安全 Cookie
- 登录失败限制
- 会话超时
- CSRF 防护
- API 请求大小限制
- Owner 与 Viewer 权限隔离
- 敏感数据默认隐藏
- 管理操作日志

## 29.2 节点安全

- Agent 主动连接中央端
- gRPC 使用 mTLS
- 每个节点独立证书
- 每个节点独立私钥
- 节点证书支持轮换和吊销
- 一次性注册码注册
- 不使用 SSH 作为日常控制方式
- 不提供任意命令执行

## 29.3 凭据安全

- 用户凭据与节点间凭据分离
- 订阅 Token 只保存哈希
- 节点私钥不上传中央端
- 敏感配置写入节点时限制文件权限
- 配置和备份中的敏感字段加密保存

## 29.4 数据安全

- 只记录域名级信息
- 不保存内容数据
- 按保留周期自动清理
- 访问记录查询需要权限
- SQLite 备份使用一致性快照
- 恢复前执行数据库完整性检查

---

# 30. 部署目录

## 30.1 中央端

```text
/usr/local/bin/proxy-panel
/usr/local/bin/qui
/usr/local/bin/qui-update

/etc/proxy-panel/
├─ config.yaml
└─ tls/

/var/lib/proxy-panel/
├─ panel.db
├─ backups/
├─ rulesets/
└─ releases/

/var/log/proxy-panel/
└─ panel.log
```

说明：`/var/lib/proxy-panel/` 下实际还有面板自己的节点 CA 目录 `identity/`（`node_identity_directory`）。日志不写文件：两个程序都写 stdout，由 systemd 收进 journald（`journalctl -u proxy-panel`）。

更新用法（L4）：新版 `qui-update --version X.Y.Z-rc.N --component panel|agent` 支持 RC；组合主机按 v1.1 顺序先 panel 再 agent，RC1 仅 panel、Agent 保持 1.0.9。不指定组件仍先 Agent 后面板。旧更新器首次使用须从已审核并校验的归档运行新版 `deploy/linux/update.sh`，见[部署与升级](docs/deployment.md)第 5.0 节；组件选择限定快照、日志与回滚范围，`--database` 不用于 agent。

更新相关（面板与节点主机共用）：`backups/` 里有 `qui-update` 的更新前备份 `pre-update-<版本>-<时间>.db` 与回滚时保留的 `failed-update-<版本>-<时间>.db`；`/var/lib/proxycontrol-install/`（root 0700）保存安装记录、`operation.lock`、`update.lock`，更新进行中时还有快照 `rollback.*/`（被替换的文件、`updater`、`service.sh`、进度日志 `journal`）。开机恢复由 `/usr/local/lib/proxycontrol/update-recovery` 与 `proxycontrol-update-recovery.service`（OpenRC：`/etc/init.d/proxycontrol-update-recovery`）完成，它们排在 Agent、面板与强制出站单元之前，不记入安装记录。跨迁移降级面板用 `qui-update --version X.Y.Z --database <pre-update-X.Y.Z-*.db>`（见[部署与升级](docs/deployment.md)第 5.1 节）。

## 30.2 节点端

```text
/usr/local/bin/proxy-agent
/usr/local/bin/qui-update

/etc/proxy-agent/
├─ config.yaml
└─ identity/

/var/lib/proxy-agent/
├─ agent.db
├─ revisions/
├─ spool/
├─ releases/
└─ host-network/
    ├─ desired.json
    └─ status.json

/var/log/proxy-agent/
├─ agent.log
└─ core.log
```

`host-network/` 是服务器强制出站的 helper 目录：Agent 写 `desired.json`，`proxy-agent-host-network.service`（oneshot，`proxy-agent --apply-host-network /var/lib/proxy-agent/host-network`，只带 `CAP_NET_ADMIN`）读它、写 `status.json`，由 `proxy-agent-host-network.path` 监视并在开机时重放。helper 只维护一个 nft 表 `inet proxy_agent_source`，卸载脚本会删除该表与这两个单元。

中央端使用 systemd 管理。节点端使用 systemd，或在 Alpine 上使用 OpenRC：
- `/etc/init.d/proxy-agent` 由 `supervise-daemon` 监管。
- `/etc/init.d/proxy-agent-host-network` 监管 `/usr/local/lib/proxy-agent/host-network-watch`（busybox `inotifyd`），代替上面的 path 单元与 oneshot 单元。
- 日志经 `logger` 写入 syslog。

两种初始化系统共用 `/usr/local/lib/proxycontrol/service.sh`，安装记录第 10 行为 `openrc` 时表示 OpenRC 安装。

---

# 31. 备份与恢复

延迟采样属于遥测，不进入便携备份：生成快照后清空 `node_latency_samples` 与定时探测队列，再压实快照；目标的 scheduled 设置保留。恢复时再次清空采样与定时任务，手动排队任务过期。调度循环随面板停机及进程内恢复停止并等待退出，新实例只启动一个循环。

中央端需要备份：

- SQLite 数据库
- 系统配置
- 中央证书材料
- 规则集
- 必要密钥的加密副本

默认备份策略：

- 保留 7 个每日备份
- 保留 4 个每周备份
- 保留 3 个每月备份

恢复流程：

```text
选择备份
 ↓
停止写入
 ↓
验证备份文件
 ↓
执行数据库完整性检查
 ↓
替换数据库和配置
 ↓
启动服务
 ↓
验证用户、节点组和策略
```

---

# 32. 开发阶段

## 阶段 1：项目基础骨架

实现：

- Go 项目结构
- Vue 项目结构
- SQLite 初始化
- 数据库迁移
- HTTP 服务
- gRPC 服务
- 日志
- 配置加载
- 健康检查
- 前端嵌入 Go 程序

验收：

- 一个中央程序可以启动
- 自动创建数据库
- 可以访问登录页面
- HTTP 和 gRPC 正常监听

## 阶段 2：管理员与后台框架

实现：

- 首次创建 Owner
- Owner 登录
- Viewer 管理
- 会话
- 权限控制
- 主页面布局
- 左侧导航
- 操作日志

验收：

- Owner 可以管理系统
- Viewer 只能查看
- 未登录不能访问后台 API

## 阶段 3：节点注册与控制连接

实现：

- 一次性注册码
- Agent 密钥生成
- 节点证书签发
- mTLS
- Agent 心跳
- 节点在线状态
- 能力上报
- 自动重连

验收：

- 节点无需日常 SSH 即可接入
- 中央端重启后 Agent 自动重连
- 非法证书不能连接

## 阶段 4：Xray 核心适配

实现（D1：只适配官方 Xray 发布二进制，不含任何 fork 或定制构建）：

- Xray 路径和版本检测（读取官方 `version` 输出，运行时探测统计与在线统计能力）
- 配置编译
- 原生配置校验
- 启动和停止
- 健康检查
- 配置版本
- 自动回滚

首个场景：

```text
用户
 ↓ SOCKS5
Node A
 ↓ Direct
Internet
```

## 阶段 5：用户、设备、节点组和订阅

实现：

- 用户管理
- 设备管理
- 凭据
- 节点组
- 用户节点组授权
- 订阅 Token
- 订阅格式生成

验收：

- 用户绑定日本节点组
- 日本组新增节点后，用户刷新订阅自动获得新节点
- 中转节点不会进入用户订阅

## 阶段 6：入站、出站与协议转换

实现：

- VLESS 入站
- Trojan 入站
- Shadowsocks 入站
- HTTP/SOCKS5 入站
- Direct 和 Block 出站
- Trojan、VLESS、Shadowsocks 和 SOCKS5 出站
- 节点间内部凭据

验收场景：

```text
用户
 ↓ VLESS
Node A
 ↓ Trojan
Node B
 ↓ Direct
Internet
```

## 阶段 7：分流和网站控制

实现：

- 域名规则
- 域名后缀
- IP/CIDR
- 用户范围
- 节点范围
- 服务规则
- Direct
- Block
- 指定出站
- 默认规则
- 策略模拟

验收：

```text
Netflix → Node B
TikTok → Block
GitHub → Direct
其他 → Direct
```

## 阶段 8：出口组和代理链

实现：

- 出口组
- 主备选择
- 简单健康检查
- A → B
- A → B → C
- 代理链能力校验
- 失败策略
- 反向发布

## 阶段 9：域名访问记录与流量统计

实现：

- Agent 域名记录
- 分钟级聚合
- 本地缓存
- 批量上传
- 全部访问页面
- 已阻断页面
- 用户、节点和出口流量

## 阶段 10：配置版本、备份与恢复

实现：

- 配置 Revision
- 发布状态
- Diff 摘要
- 自动回滚
- 手动回滚
- SQLite 在线备份
- 恢复和完整性检查

## 阶段 11：sing-box 扩展

在统一业务模型不变的情况下增加：

- sing-box 配置编译器
- Hysteria2
- TUIC
- TUN
- 后续扩展协议

## 阶段 12：系统加固

完成：

- 权限收敛
- 证书轮换
- 敏感字段保护
- 登录限速
- CSRF 防护
- 节点断网测试
- 中央端断网测试
- 核心崩溃测试
- 数据库恢复测试
- 安装和卸载脚本
- 运维文档

---

# 33. 测试要求

## 33.1 单元测试

重点测试：

- 用户节点组展开
- 订阅节点过滤
- 用户协议过滤
- 域名匹配
- 策略优先级
- 代理链环路检测
- 最大跳数检查
- 协议能力判断
- 配置编译
- 访问记录聚合
- 数据保留清理

## 33.2 集成测试

至少覆盖：

```text
SOCKS5 → Direct
VLESS → Direct
VLESS → Trojan → Node B
Shadowsocks → SOCKS5 → Node B
Netflix → Node B
TikTok → Block
A → B → C
```

## 33.3 故障测试

必须验证：

- 中央端离线
- Agent 离线
- 下一跳节点离线
- 错误配置
- 节点端口占用
- 代理核心启动失败
- 节点本地缓存写满
- 节点证书过期
- SQLite 锁等待
- 备份恢复

---

# 34. 第一版验收标准

V1 达到以下条件后可以视为完成（带 * 的条目按 D1–D5 的口径判定）：

1. 一个中央面板可以管理至少三个节点。
2. 节点通过 Agent 主动连接中央端。
3. 节点使用独立证书进行 mTLS 认证。
4. 中央端使用 Go 单体服务和 SQLite。
5. 前端静态资源嵌入中央程序。
6. 可以创建 Owner 和 Viewer。
7. 可以创建代理用户，维护共享订阅、凭据与独立门户账号。
8. 用户可以绑定一个或多个节点组。
9. 用户订阅根据节点组自动生成。
10. 新节点加入节点组后，用户无需重新授权。
11. 中转节点可以不显示在用户订阅中。
12. 节点可以同时运行多个入站。
13. 支持协议 A 入站、协议 B 出站。
14. 支持 Node A 使用另一种协议中转到 Node B。
15. 支持 `A → B` 和 `A → B → C` 代理链。
16. 支持 Direct、Block、指定 Outbound 和代理链。
17. 支持 Netflix 等服务分流到指定节点。
18. 支持网站域名阻断。
19. 可以查看访问过的域名。
20. 不记录网页正文、密码或聊天内容。
21. 配置发布前能够执行校验。
22. 配置失败能够自动回滚。
23. 中央端离线后节点继续运行。
24. 节点访问记录可以断线缓存和恢复补传。
25. 可以备份并恢复中央数据库。

D1–D5 的判定口径（补充，不替代上面 25 条）：

- \* 第 3 条：Xray 运行官方发布二进制；sing-box 可选官方或 qui 的未修改上游源码构建（上游标签），能力缺失明确降级（v1.1 R3 取代 D1 对应部分）。
- \* 第 7 条：设备限制按第 9.2 节的地址估计执行，缺能力显示部分执行；额度仍由面板自动版本软执行，正常路径窗口为上报周期＋60 秒合并窗口＋发布耗时（R7、D3）。
- \* 第 16 条：出口组按上游能力判定，权重策略已移除；固定出口失败不隐式 Direct（D4）。
- \* 第 19 条：每域名字节可以为空并显示“—”，出口记录为“配置出口”且不声称是跨节点实际出口（D4）。
- \* 第 7、19、24 条：有 `singbox_user_stats` 的 sing-box 流量进入每用户额度；官方无标签构建维持未知与节点总量。在线设备是源地址估计，账户字节与每域名字节分别判断（R3 取代 D5 对应部分）。

v1.1 的本地测试与证据见验收矩阵新增部分；尚待原生核心 CI、主机 -race、arm64/qemu、备份 B3、浏览器、RC1/RC2 与发布批准。Hysteria2/TUIC UDP 数据报门控未验收（L8）；保留样本月汇总不保证清理后的完整历史月份（L9）；TCP 探测不能证明 UDP/QUIC 可用，转发不带用户身份（L10）。这些限制与 L1–L7 不得被本地全量测试通过替代。

执行方式、证据要求和实机清单见[验收矩阵](docs/v1-acceptance.md)。

---

# 35. 第一版明确不做

为保证轻量和开发可控，V1 不包含：

- 微服务
- Kubernetes
- Redis
- Kafka
- Elasticsearch
- ClickHouse
- 高可用中央控制集群
- 商业套餐
- 支付
- 订单
- 工单
- 邀请返利
- 多租户
- 经销商
- 动态插件市场
- 无限多跳代理链
- 一个节点同时常驻多个代理核心
- 完整数据包记录
- 网页内容审计
- HTTPS 中间人解密
- Web Shell
- 任意远程命令执行
- 每条连接实时同步写数据库
- 大型日志分析平台

---

# 36. 后续扩展方向

V1 稳定后，可根据实际需要扩展：

- sing-box 完整支持
- Hysteria2
- TUIC
- TUN
- 更多订阅格式
- Passkey
- OIDC
- 更细的管理员权限
- 自动出口健康切换
- 规则集自动更新
- 节点版本灰度升级
- PostgreSQL 数据库迁移
- 更大规模访问记录存储
- 多中央控制节点

这些功能不应影响 V1 的核心架构，新增能力应通过现有模块接口扩展。

---

# 37. 项目最终运行流程

```text
用户获得订阅
       ↓
订阅根据授权节点组生成
       ↓
用户选择入口节点
       ↓
使用协议 A 连接节点
       ↓
节点识别用户和目标域名
       ↓
执行本地分流和访问控制
       ↓
┌──────────┬──────────┬────────────────┐
│          │          │                │
Block    Direct   协议 B → Node B   Proxy Chain
                                    A → B → C
       ↓
    Internet
       ↓
节点聚合域名、动作和出口记录
       ↓
批量上传中央面板
```

中央控制平台负责：

- 管理用户
- 管理用户节点组
- 生成订阅
- 管理节点
- 管理入站和出站
- 管理协议转换中转
- 管理代理链
- 管理网站分流
- 管理网站阻断
- 查看域名访问记录
- 发布配置
- 自动回滚
- 备份和恢复

节点负责：

- 运行代理核心
- 执行本地策略
- 进行实际流量转发
- 进行协议转换
- 执行链式代理
- 收集域名级访问记录
- 保持离线独立运行

---

# 38. 项目总结

本项目最终定位为：

> **一套面向少量内部用户、以轻量部署为核心、支持多节点、多协议、节点组订阅、协议转换中转、链式代理、网站分流和域名级访问控制的自托管代理控制平台。**

系统通过一个中央 Go 单体程序、一个轻量 Node Agent、SQLite 数据库和可替换的代理核心完成主要能力。

架构重点不是增加大量服务，而是保持以下四个部分清晰可靠：

1. 统一的数据模型。
2. 模块化的中央控制端。
3. 声明式的节点配置发布。
4. 可替换的代理核心适配层。

在此基础上，系统能够以较低资源占用完成用户、节点、协议、订阅、分流、中转、代理链和访问记录管理，并为后续增加新协议和新节点保留扩展空间。
