# 轻量多节点代理控制平台项目说明书

面向 1～10 名内部用户，自托管，用于自有或明确授权网络中的代理接入、节点管理、分流与域名级访问控制。本文规定当前产品行为、字段语义、安全边界与接口；源码为准，变更时同步修正文档。

## 当前状态

最新正式版 **v1.3.1**（2026-11-03，tag → `ebe8301`），无新增迁移。us2-node（面板＋Agent）、uk-node（Agent）、alpine-node（Alpine LXC/OpenRC Agent）均运行 1.3.1；迁移头为 0128。

v1.4 在 `feat/v1.4-tunnels` 开发，未发布；分支迁移头为 0132，正式版仍为 0128。范围见[v1.4 计划](docs/v1.4-plan.md)，发布说明见[1.4.0 草稿](docs/release-notes-1.4.0.md)。

正式版实机只验收 linux/amd64；arm64 只做 qemu-user 模拟，正式浏览器走查未执行。已验收内容、未验证项与限制见[验收矩阵](docs/v1-acceptance.md) §12 和[发布说明 1.3.1](docs/release-notes-1.3.1.md)。v1.4 的 A1 验收另行记录，不沿用正式版结论。

入口：[文档导航](docs/README.md)、[架构](docs/architecture.md)、[部署与开发门禁](docs/deployment.md)、[运维](docs/operations.md)、[协议矩阵](docs/protocol-support-matrix.md)。版本历史集中在[变更记录](docs/changelog.md)，完成的计划与报告在[归档](docs/archive/README.md)。

## 1. 文档目的

为产品、前后端、Agent、数据库、核心适配、测试与运维提供统一规范。需求变化须同步本文件与相关文档。

# 2. 项目定位

轻量多节点代理网络控制平台，提供用户访问管理、节点组订阅、分流与阻断、域名记录、协议转换中转和链式代理。不以商业计费、支付、多租户或大规模运营为目标。

保持部署简单、资源占用低、配置清晰、节点稳定，便于增加节点和协议；中央失联节点继续运行，错误配置自动回滚。可按域名控制流量经其他节点转发，只审计域名元数据。

# 3. 项目核心原则

## 3.1 轻量优先

中央端为 Go 单程序，嵌入前端并使用 SQLite。不强制引入 Kubernetes、Redis、Kafka、RabbitMQ、Elasticsearch、ClickHouse、Prometheus、独立配置中心/任务队列、微服务或服务网格。

## 3.2 模块化单体

用户、节点、订阅、代理、分流、发布、访问记录与系统模块边界清晰，通过 Go 接口或应用服务调用，不通过内部 HTTP 或消息队列通信。

## 3.3 控制面与数据面分离

中央面板负责用户、节点、策略、配置发布、状态、域名记录与订阅；用户代理流量不经过面板，由节点核心处理。GOST 只做转发与中继，WireGuard 按核心协议能力使用。

## 3.4 入站与出站解耦

接入与下一跳协议独立。例如用户以 VLESS 接入 A，A 终止会话后按规则以 Trojan 连接 B，再由 B Direct 出网。

## 3.5 节点独立运行

中央失联时节点保留最后成功配置、继续代理、本地缓存状态和记录，重连后补传。

## 3.6 最小化审计

只记录用户、时间、入口、域名、命中规则、允许/阻断与配置出口。不记录网页正文/标题、完整 HTTPS 路径、表单、密码、Cookie、聊天、上传下载内容或数据包正文；不做 HTTPS 中间人解密。出口证据口径见第 20 节。

# 4. 系统组成

## 4.1 Web 管理后台

Vue 3、TypeScript、Vite、Vue Router、轻量状态管理与 Fetch 请求封装。负责状态、用户凭据、节点组、入站/出站、出口组/代理链、策略、访问记录、发布/回滚与系统设置。构建后嵌入中央程序，生产环境不运行 Node.js 服务。

## 4.2 中央控制服务

Go 模块化单体，提供 REST、SSE 与 gRPC，使用 SQLite。负责管理员认证、用户隐藏接入身份与节点组授权、订阅、代理资源与策略、配置版本和发布、访问记录、流量、备份恢复。

## 4.3 Node Agent

Go 单文件程序，由 systemd 或 Alpine/OpenRC（含 LXC）管理；通过 gRPC 双向流与 mTLS 主动连接。负责注册/证书、能力与状态上报、配置编译/原生校验/应用/失败回滚、核心管理、流量及域名采集、本地持久队列和断线补传。

## 4.4 代理核心

`cores: [{name,path}]` 列启用核心，首项为兼容主核心（Xray 或 sing-box），旧标量映射单核心。生产节点均用 MultiRuntime 与 `runtime/<core>/`；节点绑定的认证清单 `cores.json.enc` 与 YAML 合并并持久覆盖禁用状态。安装/移除/回滚见第 22.2 节；Agent 不改安装器二进制/记录，职责见第 30 节。

Xray、sing-box 承载用户接入、身份、分流、Direct/Block、代理出站、中转/链与统计；协议按能力上报（第 11–13 节）。GOST 固定官方 3.3.2、仅 linux/amd64，只承载 forward（TCP/UDP）与加密中继，不承载用户身份、凭据、订阅、额度、门控、源观测、域名记录、出站资源、策略或出口组；转发按自身目标直连，仅要求启用的 direct 默认出站。

`desiredstate.Validate` 与 `ValidateCoreProtocol` 拒绝 GOST 的非 forward 入站；空 core 随主核心，是否可安装由 Hello.gost_core_supported 决定，不推断版本号。GOST 不可排 cores 首项，安装参数同样限制；必须保留至少一个 Xray/sing-box（last_core）。隧道管理要求各跳已启用 GOST，两节点契约至少 3，三节点至少 4；接收跳须公网地址或连接地址覆盖，见第 14 节。

# 5. 总体架构

管理员浏览器 → HTTPS/SSE → proxy-panel（嵌入 Web、REST、SQLite、管理与发布）→ gRPC/mTLS → 多台 Node Agent → 各节点核心（接入、中转、链与出口）。控制面只运行一个中央程序。

建议监听 443（Web/REST/SSE/订阅）、9443（Agent gRPC/mTLS）。用户数据不经过中央端。

# 6. 用户角色

后台有 Owner 与 Viewer 两种角色。

## 6.1 Owner

Owner 拥有全部权限，包括：

管理后台管理员；创建、修改和删除用户；管理用户凭据、共享订阅与门户登录；管理节点；管理节点组；管理用户节点组授权；管理入站和出站；管理出口组和代理链；管理分流规则；管理网站访问控制；查看访问记录；发布配置；回滚配置；管理 Agent 和代理核心版本；备份与恢复；修改系统设置。

## 6.2 Viewer

Viewer 只允许查看：

总览；节点状态；节点基础信息；流量统计。

Viewer 默认不能：

修改任何配置；查看敏感凭据；查看详细访问记录；发布或回滚配置；管理用户和管理员。

底层权限设计应保留细粒度权限点，以便后期增加 Operator 或 Auditor，页面不暴露复杂角色配置。

## 6.3 终端用户与门户

代理用户使用自己的用户名与 Owner 设置的密码登录 `/portal/login`；门户提供账号状态、订阅复制与二维码、授权节点、流量和修改密码。门户会话与管理员的表、Cookie、CSRF 状态和路由守卫独立，互相不能访问。禁用、到期或超额用户在登录开关开启时仍可登录查看原因，但不能取得订阅内容。Owner 的门户预览为只读，不签发 Token、不写数据；Viewer 使用后台只读视图。

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
├─ 节点列表（入站、出站、出口组；添加中转）
├─ 节点分组
├─ 隧道管理
│  ├─ 隧道
│  └─ 规则
└─ 中转方案（代理中转画布）

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

在线用户数；在线设备数；在线节点数；当前连接数；今日上传流量；今日下载流量；今日阻断次数；异常节点数；最近访问域名；最近阻断域名；简单节点拓扑；最近配置发布结果。

节点状态使用统一颜色：

- 绿色：正常
- 黄色：状态降级
- 红色：异常
- 灰色：离线
- 蓝色：配置发布或更新中

# 9. 用户管理模块

## 9.1 用户信息

字段：用户名、显示名称、启用、到期、`traffic_limit/used_traffic`、`device_limit`、估计在线设备数/执行范围、门户开关/密码/最后登录、重置周期/锚点、允许协议、授权节点组、备注。重置周期为不重置、每月或每 30 个本地日。

`device_limit` 为 0–100，0 不限；旧 `maxDevices/maxConnections` 不再是 API 字段，数据库旧列仅兼容保留。升级默认不限。

每用户只有一个跨节点额度，无每节点预算、预算转移或核内字节配额。面板累加 Agent 增量到 `used_traffic`，可关联隐藏身份时同步其用量。禁用、到期、超额、删除及提额、清零、续期、启用都触发只调整用户访问面的自动版本，移除或恢复凭据，无须手动发布；基线、范围、重试见第 22.1 节。

正常在线、采集/发布成功且无部署占用时，超额到切断最多一个上报周期（15 秒）＋最长 60 秒合并窗口＋发布耗时；每次重试另加窗口，期间流量仍放行并计量。Linux 官方 Xray 可 splice 的 TCP（如 SOCKS5/HTTP/forward 经 Direct、识别内层 TLS 后的 XTLS Vision）通常在连接关闭后才计下行，长连接因此延后额度判定。

账户额度只计 Xray 与带 `with_v2ray_api` 且上报 `singbox_user_stats` 的 sing-box；缺能力的官方 sing-box 只报节点总量，用户未知，页面说明未计范围。升级到有用户统计的 qui sing-box 会开始计入统一额度。

## 9.2 用户接入身份与设备限制

每用户一个隐藏接入身份，沿用 `DeviceID` 与旧 Agent 合同，历史记录保留，不提供设备管理。0113 保留主身份并撤销其他设备凭据/链接；升级前核对步骤见[部署](docs/deployment.md)。

跨入口节点按不同 IPv4、IPv6 /64 源地址去重，先到先得。活跃窗口 60 秒，随后保留名额 120 秒，再提升最早等待的活跃地址；Owner 可重置顺序。NAT 多端可能算一个，双栈/切网可能多算；这是源地址估计，不是 TCP 会话或物理设备数，缺测显示“未知”。

Agent 每 500 ms 采样，按凭据保留最近 20 秒来源。Xray 连接结束后仍短暂报告 IP；sing-box 只列活连接，短于采样间隔的连接可能漏观测。失败或节点/修订/核心纪元变化清空保留值，未测账户不继承旧来源。来源取自 Xray 在线 IP RPC，或 sing-box 用户日志与 Clash 连接联接。`ConnectionStatus.cores` 仅在协商成功会话接收，按（节点、核心）保存易失快照、合并后准入；旧 `snapshot_json` 仍兼容，部分核心缺测显示部分执行。

超限约一个上报周期＋投递延迟后生效：Xray 拒绝新连接、保留既有连接；sing-box 每约 2 秒关闭匹配连接。门控断联 3 分钟过期放行，旧 Agent/观测失败显示部分执行。WireGuard 一次一端，不计限制。

| 核心 | UDP 受限 | UDP 不支持设备限制（TCP 仍受限） |
| --- | --- | --- |
| Xray | VLESS、Trojan、Shadowsocks | SOCKS5 |
| sing-box | VLESS、Trojan | SOCKS5、Shadowsocks、Hysteria2、TUIC |

SOCKS5 UDP 无用户归属；sing-box 其余三种会为同一用户重建关闭的 UDP 会话。无法归属的 SOCKS5 UDP 不使整核观测失败。`node.DeviceGateUDPUnsupported` 统一判定；入站 API 给 `deviceGateCoverage=full/tcp_only/none`，用户/门户给 `deviceLimitUdpUnsupportedProtocols`。页面与编辑器说明“不支持”的 UDP 范围，与节点缺能力的“部分执行”区分（L8）。

面板完整源地址仅存内存，不入库/日志/备份；Owner/门户均掩码 IPv4 /24、IPv6 /48。Xray CLI 临时 JSON 权限 0600，调用返回即删除。

隧道/规则后的目标看到中继源地址，目标设备限制把同一中继后的客户端合为一个来源，不透传原始源地址。账户流量仍在目标用户入站计量；GOST 及隧道仅 linux/amd64（D10）。

## 9.3 凭据管理

凭据类型根据协议确定，包括：

用户名和密码；UUID；Shadowsocks 密钥；Trojan 密码；WireGuard 密钥；订阅 Token。

用户凭据与节点间中转凭据必须分开管理。

订阅 Token 以哈希查找，新签发 Token 另存 vault 加密副本，Owner 与本人可再次查看；升级前无加密副本的 Token 返回 `not_recoverable`，重置后才可查看。每用户一条共享订阅。用户可主动重置链接；链接重置撤销旧 Token，凭据重置则在自动版本应用后使旧代理设置失效。登录密码与代理凭据分开管理。

# 10. 用户订阅与节点组

## 10.1 节点组授权

Owner 在节点组页授权用户，原用户授权 API 保留。用户接入来自具体自建入站（`node_group_inbounds`）或导入（`node_group_imports`），不是服务器成员。导入 `proxy_key=''` 表示整个来源，否则用源内稳定键 `name|protocol`，不依赖刷新后的内容哈希 ID。

`node_group_servers` 保存显式服务器，`node_group_members` 为显式服务器与入站所属服务器的去重并集，用于节点策略、服务器筛选与批量操作。显式服务器不授予凭据、订阅或门户可见性。仓储写入与事务触发器在入站移动/删除时维护并集，删除服务器也删显式成员；新入站不自动入组。

0122 显式保留旧 relay/egress 全部服务器，以及 subscription/mixed 中无合格入站的服务器，保证迁移前后服务器集合不变。API 返回 `serverIds`（显式）和 `nodeIds`（并集）。`serverIds` 最多 100 个存在且未删除服务器；省略/null 保留，[] 清空。旧 `nodeIds` 在 subscription/mixed 且 `inboundIds` 缺省/null 时展开合格入站，并按选中服务器过滤旧显式成员；relay/egress 则设置显式成员，显式 `serverIds` 优先。旧批量请求保留省略的导入成员。

订阅按“用户 → 授权组 → 合格入站/导入 → 可用性与协议过滤”生成；组内新增可用接入后，用户刷新订阅即可获得，无须重新授权。

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
5. 展开启用、订阅可见且用途为 subscription/mixed 的授权分组，取得具体入站与导入成员。
6. 去除重复节点。
7. 过滤禁用节点。
8. 过滤维护中节点。
9. 过滤不允许订阅的节点。
10. 仅保留组内可用入站；入站非空 `allowedUserIds` 仍须包含本人，订阅还须核对已发布凭据。凭据渲染按入站授权，同服务器其他入站不自动获得用户凭据。
11. 过滤用户不允许使用的协议。
12. 输出订阅内容。

输出名称按节点与协议命名：同一节点同一协议有多个入站时追加端口（`节点 · 协议 · 端口`），只有一个时保持 `节点 · 协议`。

关联目标入站的中转规则按成功发布快照追加订阅端点；仅纳入已发布、启用且对用户可见的规则，仍须通过目标入站的授权、可用性、协议与已发布凭据检查。保留目标协议、凭据、传输与 TLS 等协议字段，仅把连接地址/端口换为入口地址/规则端口；原隐含 TLS server name 显式写为目标地址。名称为 `<目标节点> · <协议> · 经 <入口节点>`（目标协议原有端口消歧保留），直接端点保持自身可见性。Hysteria2/TUIC/WireGuard 按 UDP、其余按 TCP 与规则 network 过滤，各格式继续执行自身省略规则；不承诺额外 Reality 支持。

草稿修改与 D8 目标变化不推进订阅快照，pending_delete 仍保留上次快照，成功发布移除后才消失。0130 前无快照的规则须再次成功发布才进入订阅。节点中转 API 的 `shareLink` 仅 Owner 可得，使用首个有合格已发布凭据的用户生成预览；Viewer、无合格凭据或 URI 无法表达时为 null，草稿预览不代表已进入订阅。

订阅格式支持：

- 通用 URI 列表
- Base64 URI 列表
- sing-box JSON
- mihomo（`clash`，别名 `mihomo`、`clash-meta`）
- legacy Clash（`clash-legacy`，别名 `clash-premium`）

通用 URI 与 Base64 列表带不了固定证书，因此省略钉了 `trusted_ca_pem` 的 Hysteria2/TUIC 端点（不因此报错，其余链接照常输出）；这类端点由 sing-box 格式输出，并写入 `tls.certificate`。

URI、Base64 和 sing-box 保持旧输出兼容；sing-box 仍是出站片段，不是完整客户端配置。mihomo 支持全局客户端规则、provider 与客户端链；legacy Clash 过滤 VLESS、Hysteria2、TUIC、WireGuard、GEOSITE 与 RULE-SET/provider，并给 Owner 预览警告。mihomo 可用证书指纹表达固定证书的 QUIC 端点。

订阅路径是 Token → 用户 → 已发布节点及授权导入源 → 中立代理模型 → 格式导出器。`?format=` 优先，未知格式返回 400；其次为已验证 UA 表，再回退系统默认。UA 开关已实现，当前已验证 UA 匹配表为空，不能宣称自动识别已验收；一键导入协议也待核实，现提供复制与二维码。响应包含流量、到期、更新间隔与下载文件名等订阅头。

全局模板存在 `system_settings.client_rules`，有版本冲突检查与预设，按用户授权展开 `@all`、`@nodeGroup:<id>`、`@source:<id>`；服务端生成 YAML。客户端规则不替代节点策略。外部源仅 HTTPS，逐跳解析并固定经校验的公网 IP，限制 2 MiB / 15 秒；定时抓取加密缓存，订阅请求不抓取。导入节点仅用于客户端，不参与 Agent 发布、用户额度或设备门控。客户端链与格式边界见[协议矩阵](docs/protocol-support-matrix.md)。

0132 增加 `client_policy_profiles` 服务端配置库，最多 50 个配置；Owner 通过 `/api/v1/client-policy-profiles` 管理，名称最多 64 字符、描述最多 500 字符，`uiMetadata` 为最多 64 KiB 的 JSON 对象且不进入订阅。`system_settings.client_policy_active` 只保存 `{profileId}`，`client_rules` 继续是订阅读取的活动快照，其 revision 为独立的 activeRevision。编辑使用配置 revision，编辑活动配置与启用还须匹配 activeRevision；启用即使内容相同也推进 activeRevision，未修改保存不推进版本。旧 PUT 同步活动配置与快照，旧 GET 继续允许 Viewer。初始化与恢复在无配置时复制既有快照或服务端默认模板，保留 activeRevision；备份校验指针与模板一致性。浏览器旧卡片经预览后按内容摘要幂等导入为未启用配置，全部成功才清除本地副本，不采用浏览器旧启用选择。设为订阅模板，客户端更新订阅后生效。成员支持 `@endpoint:inbound:<id>`、`@endpoint:relay:<rule id>` 和 `@endpoint:import:<source id>:<name|protocol>`，只从该用户授权且已通过订阅来源检查的端点解析，客户端链克隆跳不可精确选择。`includeKeywords/excludeKeywords` 由服务器展开时对代理名称做 NFKC 规范化、不区分大小写的字面子串筛选，空包含列表不限制，排除优先，不过滤内置动作或其他组。`emptyPolicy=direct/reject` 统一作用于授权展开和格式过滤后的空组，省略兼容 DIRECT；新地区组使用 `@all` 加包含关键词并默认 REJECT。Owner-only `GET /api/v1/client-policy-candidates?userId=<id>` 复用订阅授权管线，只返回端点引用、名称、协议、来源种类/名称和节点组 ID；省略用户时按仓储顺序选择首个有节点组授权的有效用户并标明 autoSelected，无合格用户返回空列表与诊断。页面仅展示授权候选，不在浏览器重复执行筛选；分组类别按名称存入 `uiMetadata.groupCategories`，重命名同步移动类别键与组/规则引用。Owner-only `POST /api/v1/client-policy-profiles/preview` 预览草稿 template 或已保存配置（profileId + profileRevision），两者互斥；要求 CSRF，只读且不记审计。草稿复用保存校验，失败返回 invalid_rules 及字段路径；已保存版本过期返回 409 stale_client_policy 及当前 revision。与候选接口共享自动选用户，无合格用户仍校验并返回空正文与诊断。预览与真实订阅共用渲染和单次 Expand，返回用户上下文、解析分组、全部警告、规范化模板 SHA-256 和 activeRevision；不签发 Token、不标记抓取，provider URL 为占位。旧用户 GET 预览仍用活动模板。页面未保存时预览草稿，否则预览当前配置；地区组解析成员使用同一接口。分组编辑规则始终打开目标筛选列表，空状态可新增并预选该组，筛选时禁用全局拖拽排序；保存、启用或预览失败均保留草稿。共享规则集由 policy 模块持有，subscription/rules 唯一扫描全部已保存配置（含活动/未启用配置及启用/禁用规则），通过 platform/server 注入事务内读取。Owner-only `GET /api/v1/service-sets/{id}/references` 返回 nodePolicies 与 clientProfiles/rules；任何已保存引用均阻止删除（409 service_set_in_use，error 内附两类引用）。保存共享规则集立即写入，独立于配置草稿；禁用允许，保存响应附 references 与 warning。禁用后 `/sub/{token}/rules/{id}` 返回 404，客户端可能继续使用缓存或报告错误，不保证缓存规则立即停用。带 Lookup 保存时，启用的 RULE-SET 规则不能引用缺失或禁用的面板规则集；已保存模板读取只做结构校验，预览通过导出器警告路径提示缺失/禁用。节点、节点组和规则集选择项读取全部分页；预览用户仍由服务端分页选择。

# 11. 节点管理模块

## 11.1 节点列表

节点列表显示：

节点名称；在线状态；公网 IP；操作系统；CPU 使用率；内存使用率；磁盘使用率；当前连接数；今日流量；Agent 版本；代理核心；核心版本；当前配置版本；最后心跳时间；本月整机流量（单张网卡的本期整机用量与可选额度，见第 21 节）。

## 11.2 节点详情

展示基础信息、资源、能力、入站/出站、节点组、当前连接、今日代理流量、最近域名、当前/上一成功配置、发布记录、事件、出站网络与本月整机流量。出站源 IP/网卡、整机强制出站 IPv4/IPv6 未配置显示“—”，整机与代理流量分开。

`capabilities.cores` 列启用核心的版本、协议、用户统计、转发和门控能力；`heartbeat.cores` 列健康/错误/启动时间/统计源状态。`core_selectionBlocked` 列添加/移除中不可选组件。入站的 `core`、`userTrafficSupported`、`deviceGateSupported` 给所属与覆盖。`Welcome.multi_core_supported` 仅对声明支持的 Agent 为真；旧 Agent 按 `core_name` 单核心建模，缺 `Capabilities.cores` 时由旧标量合成。不得接受未上报启用核心；缺测保留 null，不编造 0/健康。GOST 仅 forward，无用户统计/门控/连接快照，coverage=none；编辑器只为 forward 提供已上报 GOST。端口唯一性见第 12 节。

`Capabilities.host` 刷新 CPU 型号/逻辑核、架构、内核、系统、内存/交换/状态目录文件系统总量。心跳提供 load1/5/15、processCount、tcpConnections、udpSockets、memoryUsedBytes、swapUsedBytes、diskUsedBytes、tuningCurrent；optional 缺测省略，实测 0 保留，uint64 JSON 为十进制字符串，旧 snake_case 字段保留。HostInfo 普通标量 0 无法区分缺测。`bootId/defaultRouteInterface` 缺测仅影响整机流量。

资源曲线每节点每 UTC 分钟保存最后一条新鲜心跳（消息/接收时间差 ≤45 秒），确认过的重放和过期消息不采样，随延迟调度清理、保留 7 天。查询 CPU/内存/磁盘百分比及字节、load1、进程/套接字/已测活连接、非 lo 接口速率合计；网卡合计可能重复计数，不等于代理量。缺测 null，不补 0。调优 GET 允许 Owner/Viewer，修改限 Owner＋CSRF＋匹配 confirmNodeId；期望与实际分列（第 22.0 节）。

## 11.3 节点注册与删除

Owner 建节点 → 一次性注册码 → 安装 Agent → 本地生成私钥并提交注册码/公钥/机器信息 → 中央签证并核销注册码 → mTLS 长连接。私钥不上传。

注册对话框提供 `deploy/linux/node-install.sh` 一键命令：下载面板同版本 GitHub Release、核对 SHA256SUMS，以公网证书或面板提供的固定 SHA-256 建立证书信任，再登记启动，操作见[部署](docs/deployment.md) §4。

未登记且无身份的节点取消登记时直接删除；已登记节点逻辑删除（`deleted_at`）、吊销证书、退出分组并从订阅/列表/选择器消失，保留流量与访问记录，不可重新启用，同服务器须新建节点登记。删除不停止远端 Agent/代理。

删除前清理入站、出站（含其他节点指向它的出站）、出口组、链、规则引用并等待批量发布结束，否则 409 `node_delete_blocked` 列阻塞项；`GET /api/v1/nodes/{id}/deletion-preview` 可预览。删除同时关闭本节点待发布、核心/Agent 更新及排队健康检查。操作见[运维](docs/operations.md) §15。

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

# 12. 入站代理模块

入站表示用户连接节点时使用的协议。

支持 HTTP CONNECT、SOCKS5、Shadowsocks、VLESS、Trojan、WireGuard（仅 Xray 入站）、Hysteria2/TUIC（仅 sing-box，需 QUIC 能力）与端口转发。VMess 与完整系统 TUN 不在当前支持表内；每核心范围以能力上报及[协议矩阵](docs/protocol-support-matrix.md)为准。

一个节点可同时有多个入站。

入站配置主要包括：

- 名称
- 所属节点
- 所属核心（`core`／`inbounds.core`；留空表示该节点当前报告的主核心，填写值必须是该 Agent 报告为启用的核心）
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

订阅隐藏（`subscriptionVisible=false`）的用户入站不会获得用户凭据，发布后渲染为停用，编辑器对这种组合给出提示。

隧道/规则生成入站返回 `managedBy:{kind,id,name}`，按 `tunnel_objects` 的 ID 归属管理；普通入站 PUT/DELETE 返回 409 `managed_by_tunnel`，须在隧道管理修改/删除并发布。目标入站被关联规则引用时，删除返回 409 `inbound_has_relays` 与 `rules:[{id,name}]`，包括尚待发布删除的规则。

由画布创建或接管的入站不能在入站编辑器中改名：面板按 `canvas_tunnel_objects` 的归属行判定（不按名称前缀），普通保存若改了名字会被拒绝（“该入站由画布隧道管理，不能在入站编辑器中修改名称。”），画布自身的生成路径仍可改名并由回滚撤销。未归属的入站可以自由改名，即使名字看起来像生成的。入站接口不额外暴露画布归属，页面因此不额外提示。

端口转发（`forward`）在 GOST 核心上另有可选字段（设置契约版本 2）：`targets[]`（1–32 个有序 `{host,port}`，与旧 `host`/`port` 互斥）、`strategy`（`round` 轮询／`fifo` 主备，默认 `round`）、`max_fails`（1–100，默认 1）、`fail_timeout`（1–3600 秒的整数秒，默认 `5s`）、`port_end`（含尾端，`Inbound.Port` 为起端，最多 256 个端口）、`bandwidth_in_bps`／`bandwidth_out_bps`（0 或 1024–1000000000000 字节/秒，0 为不限）与 `max_connections`（0–100000，仅 TCP 服务；UDP-only 转发拒绝该字段）。这些字段按“是否出现”判定：显式写 0 或默认值也算已设置，Xray／sing-box 转发携带它们会被共同校验、面板保存/发布与 Agent 渲染三处一致拒绝，不会被静默丢弃；选择非 GOST 核心前必须先清除。渲染时 qui 自己展开端口（GOST 的 `addr` 不支持范围），每个（端口、传输）一个服务，名字为 `fwd-<入站ID>-<端口>-<tcp|udp>`，每个服务都带完整有序目标池与独立的选择器与限速/连接限值，不会把第 n 个目标映射到第 n 个端口。每节点最多渲染 1024 个生产服务。

同一节点按数字端口跨核心检查，不区分 TCP/UDP，转发范围每个端口均占用。手动保存检查启用入站对启用入站的冲突；禁用入站保存只检查持久声明。`port_claims` 无论启停始终保留，发布变更/删除成功前旧声明也保留。自动分配跳过所有入站（含禁用及范围）与声明；`ports/check` 同样报告禁用入站供提示，故其占用提示不等于手动保存必然失败。共享检查按 ID 排除自身，Agent 校验为最终门禁。

协议、转发能力、WireGuard 与设备门控按入站所属核心的能力校验；组件正在被添加或移除（任务未落定）期间 `core` 不可选，保存草稿与发布都会拒绝，错误码 `core_pending`。

自建 VLESS 的 xhttp／Reality（v1.4 M1，仅契约，尚未渲染）已加入 `desiredstate.ProtocolSettings` 的类型化服务端私有字段：`reality{private_key,public_key,handshake_target,server_names[],short_ids[]}` 与 `xhttp{host,path,mode}`。两者只接受 VLESS 入站；不接受 JSON 透传、`extra`／`xmux` 或未知键（API 与 `desiredstate.Decode` 均拒绝未知字段）。Reality 私钥只进加密的 `encrypted_settings` 与加密修订，普通读取（`mask`）、修订差异和预览只暴露公钥、握手目标、server names 与 Short ID；省略私钥的编辑沿用已存值，普通保存不轮换密钥。校验口径：两把 X25519 键均为无填充 base64url、32 字节，且公钥必须由私钥导出（角色正确）；Short ID 为 1–16 个字符的偶数位十六进制；server names 1–16 个、每个 ≤253 且无控制字符；握手目标由 Owner 必填 `host:port`，面板只校验语法，不做默认站点或网络可达检查（Agent 原生校验是最终门禁）；Reality 与普通 TLS 证书材料互斥。xhttp 只接受 `auto`／`packet-up`／`stream-up`／`stream-one` 这一 TCP/H2 承载子集，`path` 归 xhttp 自己，`service_name` 必须为空；Vision（`xtls-rprx-vision`）只允许 TCP 上的 TLS 或 Reality，xhttp／gRPC 上的 Vision 在保存前拒绝。

能力门禁独立于协议标志与版本号：`desiredstate.CoreSettingsCapability{core,settings_contract_version,combinations[]}` 按（协议、方向、安全、传输、flow）逐项声明。版本 0、缺失、未知版本、其他核心的上报以及未列出的组合一律不授权；Agent 版本文本、VLESS 入站标志、`multi_core_supported` 与 GOST 契约位都不能替代它。M1 已把该上报落到 `agentpb`：`CoreCapability.server_settings_contract_version`（字段 10）与 `server_settings_combinations`（字段 11）为逐核声明，`Capabilities` 的同名字段（23／24）是单核 Agent 的标量形式，均由 `scripts/generate-proto.sh` 生成、只做加法，旧 Agent／旧面板各自忽略或保留未知字段。Agent 的 `MultiRuntime.DetectCapabilities` 按各适配器上报复制到对应核心，`control.reportCapabilities` 的旧标量装饰同样保留；面板 `node.SettingsContractCapability` 只把上报转换为共享契约、不推断来源，`node.EnabledCores` 的单核回退也保留这些字段。上报结构在 `validateMultiCapabilities` 里用共享组合校验一次：组合缺版本、方向／传输非法、重复项或超上限都拒绝整条上报；未知版本仍被接受（不因未来版本断连）但不授权。当前 Xray／sing-box 适配器都没有渲染器，因此生产 Agent 上报版本 0 与空列表，面板保存（`proxy.ValidateInboundServerSettings`）、配置快照（`SnapshotIntent`）与发布前（`deployment.validateStateCoresTx`）三处一致拒绝 Reality／xhttp 设置；合成上报可完整传输并放行对应组合，真实启用仍待渲染器（M2／M4）。

M1 不提供渲染：Xray 与 sing-box 适配器都没有 Reality／xhttp 渲染器，渲染入口（`desiredstate.ValidateServerSettingsRenderer`）对携带这些设置的入站或出站一律失败，避免结构校验通过后静默丢弃设置；已发布状态若意外带这些字段，`subscription/source/published` 也跳过该端点而不是导出缺公钥与 Short ID 的客户端条目，这只是渲染器缺席期间的临时保护，不代表客户端映射已完成。`/api/v1/protocol-support` 的 VLESS 行把 `security_reality`、`transport_xhttp` 列为 `pending`，选项、订阅映射与 sing-box 适配留待 M2–M4。

# 13. 出站代理模块

出站表示节点收到用户流量后，以什么方式继续访问。

支持的出站类型：

Direct；Block；HTTP；SOCKS5；Shadowsocks；VLESS；Trojan；WireGuard；远程节点；出口组；Proxy Chain。

出站主要字段：

名称；所属节点；出站类型；目标节点；目标地址；目标端口；使用协议；认证凭据；TCP 支持情况；UDP 支持情况；是否启用；健康状态；失败处理方式。

失败处理方式包括：

Block；Direct；备用出站；备用出口组；备用代理链。

对于要求固定出口的网站，默认失败策略建议为 Block 或备用出口，不自动 Direct。

## 13.1 节点出站网络

节点可以在“节点详情 → 节点设置”里指定出站网络。改动在下一次**发布配置**时生效；字段留空并保存表示清除。

- **核心出站源 IP / 核心出站网卡**：只影响本节点代理核心转发的流量。Xray 渲染为出站 `sendThrough` 与 `streamSettings.sockopt.interface`，sing-box 渲染为 `inet4/inet6_bind_address` 与 `bind_interface`；出口组成员同样固定，sing-box 的 `selector`/`urltest` 组本身不带拨号字段（上游不接受），入站 WireGuard 端点只监听、不改写。地址族按源地址固定（Xray `ForceIPv4`/`ForceIPv6`，sing-box `ipv4_only`/`ipv6_only`）；目标是另一族的字面量 IP 时连接失败。
- **服务器强制出站 IPv4 / IPv6**：整台服务器经该网卡新建的出站连接都改用此源地址（核心出站源 IP 除外）；仅 Linux，失败自动回滚。执行方式、双栈注意事项与排查命令见[运维手册](docs/operations.md)。

面板只校验语法（地址须为单播、`host_source_ipv4` 为 v4、`host_source_ipv6` 为 v6、网卡名符合 Linux 命名）；地址或网卡是否存在由节点在应用配置时判断，缺失时按临时失败码重试。

# 14. 协议转换中转

中转分为隧道管理、节点“添加中转”和代理中转画布。隧道转发字节，不终止目标用户协议；画布由节点终止协议 A、按策略以协议 B 建立下一跳，用户凭据与节点间凭据隔离。

隧道为 entry → [mid] → exit，最多三台不同服务器、两段中继；仅 GOST relay over TLS＋mTLS。两节点要求各跳契约 ≥3，三节点各跳 ≥4；接收跳使用公网地址或 `connectHost`。每个接收跳为同一隧道的规则共享一个监听，中间只知道一个下一跳。分段独立密封 CA/叶子证书，私钥不进 API、计划或日志。

规则指定入口监听、network=tcp/udp/both、可选隧道及最终目标；不选隧道即直接转发，优先用 GOST，否则主 Xray/sing-box。linked 关联用户入站，由目标服务器地址与入站端口派生，隧道出口就是目标服务器时用 127.0.0.1；manual 固定 host/port，不生成用户订阅。D8：目标公网地址或入站端口变化在同一事务递增草稿版本，已发布规则标为 needs_publish；读取派生新目标，上次成功发布快照保持不动。删除目标前须先发布移除关联规则。

节点“添加中转”可选择已有隧道，为本节点用户入站创建 linked 规则（隧道入口须为其他节点，ownerNodeId 为目标节点）；也可新建中转方案，以当前节点预选为代理入口。订阅与分享见第 10.3 节。

隧道/规则保存只写草稿；draft 从未发布，modified 为手动修改，needs_publish 为目标变化，published 为成功发布，pending_delete 为待发布移除且只读。未发布对象可直接删除；已发布对象保留行、旧监听声明及快照直到成功移除。隧道有规则引用时返回 tunnel_in_use（包括待删除规则）。

生成对象按 `tunnel_objects` 的 owner kind/ID 归属，不按名称认领；`port_claims` 保存节点级数字端口，改端口保留新旧声明直到成功发布。隧道和规则必须手填端口或从 `node_meta.port_range_start/end` 分配，无默认端口回退。自动建议不预留，保存时再查并声明，重发复用已保存端口；冲突语义见第 12 节。

中转方案画布只创建代理中转（proxy_chain/tunnel，tls 或 wireguard）；出口可为 qui 用户入站或外部 SOCKS5/HTTP/Shadowsocks/Trojan/VLESS 出站。qui 入站出口生成专用内部监听和独立中转凭据，不改变原用户入站；外部出站属于前一跳服务器，作为最终出站且按 TCP/UDP 能力校验。relay/exit 实例的 relayPortMode/relayPort 优先于旧 tunnelPort；自动分配复用稳定端口，无范围时画布仍保留 TLS 8443 / WireGuard 51820，范围耗尽则失败。

画布内部资源仍用 `canvas_tunnels/canvas_tunnel_objects` 的 ID 归属与共享依赖。旧 port_forward/gost 连接在迁移前只读保留，可删除，不可新增或改动；一次性转换及遗留原因见第 26.9、31 节。GOST 目标池、范围、限流与连接上限仍属普通 forward 入站（第 12 节），不由新画布创建。

# 15. 出口组模块

出口组用于系统内部对多个出口进行统一管理。

支持的选择方式（按上游能力）：

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

# 16. 代理链模块

隧道管理计划/发布接受 tunnelIds/ruleIds；选隧道纳入其规则，选规则纳入隧道，隧道有改动时一并纳入其他拨号规则。计划只读，展示服务器、监听、路由、变更、冲突和依赖；发布重新规划，复用部署批次按 exit → mid → entry 执行，失败反向回滚。仅全部成功推进版本及订阅快照；失败恢复生成对象、保留草稿与声明。未变发布为 unchanged，无修订或批次。

轮换隧道凭据仅生成新草稿，须再计划/发布才切换；接收监听先于拨号方，切换期间可短暂中断，不保留既有 TCP/UDP 会话。中间/出口 relay 健康仅指进程与监听，不代表目标可达。

画布代理中转仍按 `canvas_tunnels` 共享对象与依赖：相同 tunnelId 只生成一组对象，未变可无操作发布；不同方案对同一入口声明冲突路径则拒绝。删除方案不删仍共享对象，最后引用移除后须发布清理；在用画布隧道删除返回 409 tunnel_in_use 与方案列表。不同资源不自动接管监听。客户端方案自包含，拒绝 tunnelId，proxy_chain/tunnel 均表达 mihomo dialer-proxy 单路径。

画布发布先整体校验依赖，跳过仅为依赖且运行配置、启用意图未变的服务器（忽略修订号、用户访问按集合比较、空等于缺失，历史 NULL 启用意图不等于空集合）；跳过者无修订或回滚项。裁剪后按批外目标当前已发布状态校验；显式中继批次仍发布全体。新画布不接受端口转发/GOST 隧道连接。

代理链支持 A→B、A→B→C，各跳协议可不同，不支持无限多跳。

代理链包含：

链路名称；入口节点；中转节点；出口节点；每一跳协议；每一跳出站；最终出口方式；失败处理策略；健康状态。

代理链保存和发布前必须检查：

是否存在重复节点；是否存在路由环路；是否超过最大跳数；每一跳协议是否受支持；每一跳 TCP/UDP 能力是否匹配；端口是否冲突；内部入站是否存在；节点间凭据是否有效；最终出口是否存在。

按出口→中转→入口发布，按入口→中转→出口回滚。

# 17. 分流模块

分流模块统一处理：

网站分流；网站阻断；直接连接；指定节点出口；指定代理链；指定出口组。

规则可以匹配：

用户；入口节点；入口节点组；域名；域名后缀；网站服务分类；IP；CIDR；端口；TCP；UDP；时间段。

规则动作包括：

Direct；Block；指定 Outbound；指定出口组；指定 Proxy Chain。

规则按优先级从高到低执行，命中第一条有效规则后停止。

每个节点必须存在默认规则。

# 18. 服务规则模块

服务规则用于将常见网站的域名集合封装成易于选择的服务。

内置服务分类包括：

Netflix；Disney+；YouTube；OpenAI；Google；GitHub；Telegram。

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

# 19. 网站访问控制

网站访问控制与分流使用同一套策略引擎。

支持控制：

某个用户不能访问哪些网站；某个节点不能访问哪些网站；某个节点组不能访问哪些网站；某个网站必须从指定节点出去；某类网站只允许在指定时间访问。

访问控制支持：

精确域名；域名后缀；通配域名；自定义网站分类；内置服务规则；IP/CIDR。

# 20. 网站访问记录

页面、筛选、策略作用域与模拟器不再暴露设备字段；历史数据中的设备 ID 与隐藏身份仍保留以兼容统计和墓碑。按用户查询，不提供终端用户访问记录入口。

系统只记录域名级访问结果。

记录字段包括（设备仅是内部历史/隐藏身份字段，不展示）：

时间；用户；设备；入口节点；入站协议；访问域名；目标 IP；目标端口；命中规则；执行动作；出站协议；代理链；最终出口节点；允许或阻断；连接数量；上传流量；下载流量。

**字段口径**：

- **上传/下载流量是按域名的可空字段**。官方上游核无法提供每域名精确字节，未知时为 NULL，页面显示“—”，**不写零**。用户与节点级的总量仍来自核心统计。
- **最终出口节点记录的是“配置出口”**：该节点按策略解析出的出站或出口组所选成员。它表示配置，不证明跨节点链路实际到达了哪个出口节点；跨中转的实际出口无法确认，页面会分别标注“本节点直连”和“配置出口未确认”。
- 跨分钟仍存活的连接不会每分钟重复计为新连接；连接数不等于实时并发或请求次数。

阻断如果节点无法确定域名，则记录目标 IP，不根据网页内容猜测域名。

# 21. 访问记录轻量化处理

内部身份用于流量归属/补传，界面隐藏设备维度。不逐连接同步写中央库，按时间桶、用户、节点、域名、动作、出口聚合连接数；跨分钟存活连接不重复算新连接，域名字节可空。

| 项目 | 默认值/规则 |
| --- | --- |
| 允许/阻断记录 | 30/90 天 |
| 小时/日流量 | 小时 90 天；日长期保留 |
| 本地 spool | 100 MiB（`DefaultLimit`）；容量 pragma 对每条 SQLite 连接生效 |
| 采样/上报 | 每 5 秒采样写 spool；面板下发 HeartbeatInterval=15 秒并批量上传 |
| Xray 原始访问行 | `/var/lib/proxy-agent/runtime/xray/access.log`，约 100 ms 轮询，读后/停核清空，20 MiB 截断兜底，避免保留 HTTP 路径/查询串 |
| sing-box 访问行 | 核心日志 |

离线缓存，重连补传，收到 Ack 才删。满时先淘汰最旧允许记录，优先保留阻断与流量。同批流量被面板连续拒绝 3 次则隔离、保留且不淘汰，后续批次继续；1 小时后或 Agent 重启重试。删除用户/旧设备前产生的批次按其修订与凭据墓碑归属，只累加仍存在的用户/身份。

**代理月流量**：按面板时区半开月区间汇总保留样本，`scope=retained_account_samples`。HourlyDays 清理后不能还原完整历史月份（L9），不得拼累计心跳/UTC 日桶冒充完整月份。

**核心/节点总量**：Hello/Welcome 双方协商 `node_traffic_supported`（面板须有 AccessRecorder），Agent 额外上传每核心累计 `traffic.Counter{kind:"node"}`；未协商则整批 FailedPrecondition。校验 core=xray/sing-box/gost、credential_id=node、epoch 后缀 .node、当前已确认修订、字节非负且 ≤1 PiB。

按（节点、epoch、node）保存游标、计算增量写 `core_traffic_samples`，批次幂等，多核心 epoch 互不淘汰。`node_traffic_samples` 仅在同 epoch 无 node 游标时回退账户样本，避免重复；总量用于今日/月节点与访问审计节点汇总，绝不进入用户额度，用户维度只取账户计数。官方 sing-box 无 with_v2ray_api 时用户未知。

Xray 总量=账户计数＋已渲染 forward 的原生入站计数。渲染生成 tag 白名单，额外查询 `inbound>>>`（Reset_=false），仅接受白名单精确 uplink/downlink，忽略用户入站/管理 API/其他入站，避免重算。sing-box 取核心总量（含转发）；旧 Agent 缺失转发字节不补记。forward 无用户身份，不进额度、设备计数或域名归属。

**GOST 统计**：沿用采集周期及停核前采样，从 `runtime/gost/metrics.sock` 经 Unix DialContext 读取 Prometheus 文本，仅求和已渲染 `fwd-<入站ID>-<端口>-<tcp|udp>` 的 `gost_service_transfer_input_bytes_total/output_bytes_total`（上传/下载）。范围的各服务只计一次，TCP/UDP 都计；丢弃 client/host 标签，不作身份。负数、非有限、溢出、标签/行损坏使整次采样失败并标统计源失败，不补零。计数随进程累计，每次应用从零开始并换 epoch；不提供每转发历史表/API。

`relay-<入站ID>-<端口>` 重复搬运入口已计的转发载荷，**不计节点总量**；无用户额度、设备限制、域名归属。

隧道规则入口/直接规则的转发字节只进核心/节点总量；中间与出口 relay 服务继续排除，避免重复计量。中转用户的账户字节在目标用户入站计入原额度，不从隧道或规则字节推算用户用量。

**整机月流量**：独立于代理统计、用户额度和订阅。心跳上报非 lo 网卡累计字节、bootId、IPv4 默认路由网卡，面板在同一事务按（服务器、网卡、本地日期）写差值到 `node_machine_traffic_daily`。首次采样、重启、计数回退只建基线；同次启动断线量可补，跨日按时间比例分摊，跨重启缺口不能补。

长期保留各网卡日行，摘要只选一张：显式 interfaceName，否则最近新鲜心跳的默认路由网卡；不同网卡不合并/拼接，改网卡重算所选网卡本期。周期按面板时区重置日 1–31 起算半开区间，短月夹到月末；sum=入＋出，outbound=出，max=本期两方向合计较大者。无本期日行/无法解析网卡为 null（“—”）；有日行而缺新鲜采样为 stale（“截至上次采样”）。

整机含所有服务，不等于代理量/供应商账单；LXC 只见容器网卡。IPv6-only/策略路由须手选网卡。改时区只影响之后的日桶，历史不重分桶。额度仅展示，不限速、不停服；设置/接口见第 27.4 节，备份见第 31 节。

# 22. 配置发布与回滚

草稿不直接覆盖运行配置：保存业务配置 → 生成 Revision → 校验用户/节点/能力 → DesiredState → Agent 编译 → 原生校验 → 保存/切换 → 健康检查 → 成功或自动回滚。

一条 Revision 覆盖全部核心，`core.json.enc` 按核心名保存 JSON；恢复从校验过的 DesiredState 重建，兼容旧单核心修订。所有活跃核心先校验，再按配置顺序切换，失败逆序补偿。每核 `runtime/<core>/` 隔离标记、accounting key、访问日志与临时文件，无启用入站则停止；旧 core_healthy 是逐核 AND，supervisor 分核恢复/退避。

**发布仍重启目标节点全部核心**，即使某核配置未变，所有用户既有连接会中断；崩溃恢复只影响异常核心。编译/校验失败不改配置，启动/健康失败回滚；中央失联保留成功配置。Agent 启动时核心不能运行保留修订，核心停止但 Agent 继续连接、上报不健康并等待新版本。默认保留最近 3～5 个成功版本。

## 22.0 服务器调优应用流程

Owner＋CSRF 提交完整 settings/clear 和匹配 confirmNodeId → 校验白名单/边界 → 事务保存 node_tuning 唯一 request_id、期望 profile、pending、审计“修改服务器调优” → Connect 下发 TuningCommand。每节点最多一个 pending（409 tuning_busy）；缺能力 409 tuning_unsupported；禁用/离线/心跳过期 409 node_offline。

等待 helper 时仍收消息/Ack。TuningResult 须匹配认证 node_id/request_id，事务保存逐键结果再 Ack；相同结果幂等，冲突拒绝。断流/重连把 pending 改 unconfirmed，不重发或换 ID 重试；不确认不代表未执行。依据后续 tuningCurrent/运维检查，再由 Owner 发起新操作。恢复见第 31 节，续证见第 29.2 节。

## 22.1 自动额度版本

流量入库/定时 tick 判定用户（禁用 > 超限 > 到期），状态变化按节点排队，60 秒内合并；以**已发布状态**为基线只替换用户访问面，创建描述为 `auto: user <name> <reason>` 的 Revision。

- 唯一草稿例外是当前用户/入站绑定，未发布的绑定可能被带出；中转凭据、出站、出口组、链、规则均保留已发布状态。
- 启动先播种状态机，不因重启全量发布；随后对已有发布状态的节点对账，仅不一致者排队。读取失败每 tick 重试，直到完整读完。
- 忙碌/等 Agent 时等下窗口；读/建/部署失败或 Agent 失败/回滚时，下窗口按最新已发布状态重建。连续 3 次失败停止重试，以 system.alert 和“自动额度发布失败”日志告警。草稿不能渲染则回退基线、只移除不活跃用户，恢复访问须等草稿可渲染。
- 启用意图来自基线 `config_revisions.inbound_enabled_json`，不是草稿；无凭据渲染为 disabled。凭据恢复时只有基线意图含该入站才启用，Owner 已禁用或历史 NULL 均保持禁用直至手工发布；凭据仍可挂禁用入站。
- 目标为当前授权节点＋已发布状态仍持凭据的节点。删除用户/旧设备也排队（deleted/device deleted、user_access 告警），渲染不再带被删凭据，一致则不发版。
- 每次部署/回滚成功排一次对账，纠正旧修订带回禁用用户或漏掉已启用用户；同样合并 60 秒，一致不发版。
- 自动版本仍重启节点全部核心；中央离线继续旧配置直到重连。渲染使用同份草稿快照，入站 core 原样继承（空串随主核心），若核心不可用/添加移除中则失败（core selection pending）并按上述重试，不能静默搬移凭据。

## 22.2 核心更新与移除

核心更新独立于配置发布：Owner 提交组件、稳定版本、白名单 GitHub URL、归档 SHA-256 → 面板校验平台/版本/来源/摘要 → Agent 再校验 URL/重定向主机/摘要 → 解包（拒绝链接、穿越、多余候选）→ 原生版本/健康校验与切换，失败回滚。无可信摘要不安装。缺失组件走同一 schema-2 请求安装启用，空闲核心不启动；每组件有 worker、staging、认证 release hold 与独立历史，重新添加使用新历史命名空间并保留反回滚序号检查。

- Xray 只收官方前缀；sing-box 也收精确 `sing-box-<版本>-qui.<序号>-linux-<架构>.tar.gz` 的 qui 归档，面板/Agent 双检，旧 Agent 未声明能力不能排队 qui 来源。
- GOST 仅 linux/amd64、3.3.2、`v3.3.2/gost_3.3.2_linux_amd64.tar.gz`，前缀 `https://github.com/go-gost/gost/releases/download/`，固定**归档**摘要 `162feb43…a9d18274`（非二进制摘要）。下划线资产不套通用连字符匹配。共享规则只定义在 `pkg/contracts/release` 的 CoreAssetURL/CoreAssetMatches/CorePinnedTarget；改版本须审阅固定值并重跑真实核回归。安装缺失 GOST 另须当前 Hello.gost_core_supported；nodes 对应位恢复清零、重连刷新，多核心能力不能替代该位。
- Agent 自更新使用配置固定 Ed25519 公钥、签名清单与安装历史防重放，失败回滚确认镜像；核心不用签名目录，也无旧 receipt/outcome journal 路径。

任务按（节点、组件）唯一活跃，sequence 按各自历史递增；不同组件可同时排队。PendingCoreUpdate 先派 queued，再重发 running 同一身份，断线不换 ID。POST /core-updates 必填 component，回执信封 component 必须匹配，重复幂等、变更结果拒绝。卡住任务重连关闭，也可 Owner 清理。

更新回执在 worker **内存**队列按先后保留至 Ack，断流下会话重发。面板连续 3 次以 FailedPrecondition＋core update result rejected 永久拒绝同结果则丢弃并 Warn；网络/Unavailable/取消永不丢弃。Agent 重启丢失内存队列，由重连关闭/Owner 清理兜底。

success 仅证明镜像切换；须等 Capabilities.cores 出现组件才允许选择。进行中列入 core_selectionBlocked，保存/草稿渲染/发布拒绝 core_pending，不伪造能力。

移除使用 core_remove_tasks：Owner＋CSRF＋confirmNodeId＋当前确认修订 → POST /nodes/{id}/cores/{component}/remove，审计 remove_core → RemoveCoreCommand{id,component,revision} → 幂等保存 RemoveCoreResult/Ack。断线重发同身份，更晚修订已确认也接受迟到重放；移除回执**持久保存**至 Ack。

| 拒绝码 | 条件 |
| --- | --- |
| core_in_use | 草稿/已发布含该组件入站，禁用也算；返回 inboundIds；Agent 还检查中断待定修订 |
| core_pending | 组件添加/移除或部署占用 |
| last_core | 不得移除最后一个 Xray/sing-box；GOST 不可作主核心 |
| revision_mismatch | 提交修订不是当前已确认修订 |

移除顺序：确认退出 → 原子持久禁用清单 → 移除运行实例 → 删 runtime/<组件>/ 与自管镜像。标记/accounting key 是无残留进程的证据，ErrCoreStopUnconfirmed 时目录、清单、镜像全保留；启动恢复中断移除也须 Close 确认。`cores.json.enc`、更新历史、安装器外部二进制/摘要记录保留，Agent 不改安装器文件。重加组件重新启用并建新运行目录；操作后刷新能力，确认前仍显示组件。

回滚到已移除核心的历史修订在面板拒绝（`rollback refused: … core "sing-box" is unavailable`），先重装组件或发显式核心的新修订。

# 23. Node Agent 模块设计

模块：Identity（身份/证书）、Control（连接/心跳/重连/Ack）、Capability（系统/核心探测）、Reconciler（期望协调）、Compiler（Xray/sing-box/GOST）、Supervisor（进程/健康）、Collector（统计/域名）、Spool（持久缓存）、Updater（Agent/核心）。GOST 持久化 JSON，执行前生成 YAML。

更新方式见第 22.2 节；qui sing-box 仅为未修改上游加上游标签。受管 Agent 启动按安装历史完成中断切换，不使用旧候选确认恢复或 supervisor 巡检模式。

**禁止**任意 Shell、Web Terminal、任意命令、上传脚本执行与未定义远程操作。只允许：应用/回滚版本、重启/重载核心、健康检查、证书轮换、Agent/核心更新与核心移除，以及以下固定操作：

- 强制出站源地址：固定 nft 规则，独立 oneshot，仅 CAP_NET_ADMIN。
- 调优/清除：13 个白名单 sysctl 键及边界；root helper 直接写 procfs、读回校验、原子保存 99-proxy-tuning.conf，失败恢复原值，clear 恢复首次快照；无 shell/任意 sysctl（第 22.0、30 节）。
- TTL 设备门控：Xray 官方运行时规则 API / sing-box Clash 关连接，不发 Revision/不重启。
- TCP-connect：只接受已解析 IP、端口、次数、超时、截止时间，Agent 再检，最多 10 次、每次 3 秒，无 shell。
- 转发/中继：正常发布，旧能力拒绝；Xray dokodemo-door、sing-box direct override、GOST tcp/udp。无用户身份，不进订阅/额度/设备限制/域名归属。

GOST 目标按序写 forwarder.nodes（target-0…），selector={strategy,maxFails,failTimeout}；自行展开每端口/传输服务，addr 无范围。限速为 limiters[{name,limits:["$ <in> <out>"]}]＋service.limiter，连接上限为 climiters[{name,limits:["$ <n>"]}]＋service.climiter。ParseBase2Bytes 不收裸整数，速率写 `<整数字节>B`，唯 0 表不限；typed 输出校验引用/目标，避免未知 limiter 静默不限速。

GOST 出口为 handler:relay＋listener:tls＋客户端 CA；入口链挂 handler.chain，connector:relay＋dialer:tls（CA/服务器名）。证书材料按入站/半边写 `runtime/gost/tls/<入站>/<半边>/`，目录 0700、文件 0600，旧修订回滚不覆盖在用证书。

GOST 当前上报契约 4，允许中间 relay listener 携带一个 `gost_hops` 下一跳，链挂 handler.chain；前段监听与后段拨号证书分别保存，中间与出口 relay 不计节点总量。旧契约 3 的入口链（最多两个原生 hop）和出口渲染兼容。隧道管理限定入口→可选单个中间→出口（最多三台不同服务器），发布前按每跳上报能力校验两节点 ≥3、三节点 ≥4，不从二进制版本推断。TLS relay 无生产 UDP 监听，UDP 由 TLS/TCP 封装至出口再发往目标；健康只测进程与监听，不代表握手/目标可达。运行证据见 [X0 记录](docs/v1.4-x0-findings.md)，A1 结果待补。

**TCP 探测**只测建连耗时/失败率，不是带宽、ICMP、UDP/QUIC 可用性。手动历史每节点/目标最多 100 条；定时每分钟测启用未删除、心跳 ≤45 秒且支持的节点，每目标 3×3000 ms。默认目标用 probe_targets.scheduled（最多 5）；节点可自定义最多 5 个，custom=true＋空表=不测，false=清空自定义并继承。队列按 source 隔离、手动优先，在途结束或 2 分钟到期才派下一条。

定时结果 ValidateResult 后入独立采样表，保留 7 天。当前 Agent 仅 medianMs；原始 avg/min/max 为 null。≤12 小时返回原点，>12 小时按 Unix epoch 分桶，步长 ceil(小时×60/720)×60 秒，每目标约 720 点（边界可多 1）。桶 medianMs 为 sent>failed 且非 NULL 中位数的算术平均，min/max 是这些中位数极值，无值均 null，avgMs 恒 null；sent/failed 求和，samples 为样本数（原点 1）。删目标级联样本，逻辑删节点停止查询、等保留清理。API 见第 27.12 节。

**健康与恢复**：Health 每次即时查监听（心跳、手工检查、就绪、发布核对、更新激活不变）。巡检每 3 秒，仅 ErrListenerUnavailable 连续 3 次才恢复；退出、未确认停止、路由恢复失败或无原因不健康立即恢复。失败退避，成功/修订变化/组件更新重置计数。分核判断，全健康不再聚合二次采样，单次拨号失败不能重启仍服务的核心。

GOST 健康=确切子进程存活＋每个生产 TCP 可拨＋每个生产 UDP socket 属该进程；失败报 ErrListenerUnavailable 并列监听。指标失败只标统计源失败，目标可达性不属核心健康，须另做探测/真实流量验证。

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

不使用服务端渲染；生产环境不运行 Node.js；页面按路由懒加载；只保留少量全局状态；列表使用服务器分页；不一次加载全部访问记录；实时状态使用一条 SSE 连接；不为普通 CRUD 使用 WebSocket；通用表格、状态标签和确认弹窗统一复用。

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

`devices` 只保存每用户一个隐藏接入身份；不再提供设备管理。

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

协议配置使用密封 JSON（encrypted_settings），策略/能力使用结构化 JSON，例如：

```text
inbounds.encrypted_settings
outbounds.encrypted_settings
policies.scope_json
policies.match_json
policies.action_json
nodes.capabilities_json
```

JSON 字段必须在 Go 中解析为明确的数据结构，不允许无结构透传。

## 26.9 迁移与兼容规则

启动及恢复在 0124 认领后运行 0131 的一次性 `ReconcileCanvasForwards`，同一事务转换已发布且归属完整的画布端口转发。普通转发变规则，GOST 转发变两节点隧道及规则；原入站 ID、端口、设置与密封凭据不改，监听为 manual，订阅默认隐藏。目标公网地址与成功运行修订中的用户入站端口唯一匹配且与当前行一致才 linked，否则保留 manual。转换连接从布局移除；无法确定的连接保留，Owner 可读 migration-report 的中文原因。标记保存已迁移 ID，首次无修改发布保留旧物化形态；完成后不自动重试，事务失败可重试，不重新按名称认领。

共享隧道迁移（仅在升级/恢复数据库时应用）：

| 迁移 | 状态 |
| --- | --- |
| 0124 `canvas_tunnels` | canvas_tunnels（严格类型连接 JSON、端点引用、稳定端口、版本标记）、canvas_tunnel_objects（对象唯一归属）、canvas_tunnel_publications（待清理发布关联）、一次性 reconcile 标记；服务端布局增加 tunnelId，客户端保持内嵌；备份清单/验证与恢复覆盖，旧对象只按已发布、精确当前名称、无歧义规则认领 |
| 0125 `multi_core_panel` | `core_update_tasks.adding`（0007/0107 已按签名目录回填 component，本迁移不动既有行）；唯一索引由“每节点一个活跃任务”改为 `(node_id,component)`；`inbounds.core`（空/xray/sing-box，空＝当前主核心；gost 由 0127 扩展）并删除旧 `inbound_active_port`（端口唯一改由面板按 Agent 的节点级数字端口规则跨核心与转发校验）；`nodes.multi_core_supported/node_traffic_supported`；`node_health_checks.core`、`node_restart_requests.core`；`core_remove_tasks`（每（节点、组件）一个活跃移除任务、幂等回执）；`node_traffic_cursors`（每（节点、epoch、`node`）的累计游标）、`core_traffic_samples`（每核心观察增量）与去重视图 `node_traffic_samples` |

| 迁移 | 实际存储变化 |
| --- | --- |
| 0114 `end_user_accounts` | `users.password_hash/login_enabled/last_login_at`；独立 `user_sessions` 与过期索引；`subscription_tokens.encrypted_token` |
| 0115 `device_limit` | `users.device_limit`（0–100，默认 0）；源地址与门控租约不落库 |
| 0116 `subscription_sources` | `subscription_sources`、`imported_proxies`、`user_source_grants`；`canvas_schemes.client_published_layout` |
| 0117 `node_meta` | `node_meta`：地区、备注、续费日期/周期/价格/币种、提醒天数与每日提醒标记 |
| 0118 `traffic_reset` | `users.traffic_reset_cycle/traffic_reset_anchor/traffic_reset_at`，从旧备注迁移周期并去除元数据前缀 |
| 0119 `tcp_probes` | `nodes.tcp_probe_supported`；`probe_targets`、`node_tcp_probes` 与历史索引；转发复用入站设置，不新增转发表 |
| 0120 `latency_samples` | `probe_targets.scheduled`（默认关闭，触发器限制最多 5 个）；`node_tcp_probes.source/dispatched_at` 与单节点在途唯一索引；`node_latency_samples`（REAL 毫秒，含 median_ms；按节点/时间索引，保留 7 天） |
| 0121 `node_latency_targets` | 每节点目标关联表（节点、目标外键级联删除，插入及移动触发器限制每节点最多 5 个）；`nodes.latency_targets_custom`（0/1，默认 0） |
| 0122 `node_group_entries` | `node_group_inbounds`、`node_group_imports`；服务器成员转入站、旧来源授权转生成分组；`node_meta.port_range_start/end`（同时为空或 1–65535 的有序范围），迁移备注末尾有效 `[ports:A-B]` |
| 0123 `node_metrics_tuning` | `node_metric_samples`：可空分钟遥测、7 天保留、不进便携备份；`node_tuning`：完整期望 profile、request_id、状态与逐键回执，随备份保留，恢复以当前状态为准且不重发 |
| 0126 `node_machine_traffic` | `node_machine_traffic_settings`（每服务器 method/interface_name/可空 allowance_bytes 1–2^53-1/reset_day 1–31，默认 sum/''/NULL/1）、`node_machine_traffic_daily`（主键（服务器、网卡、本地日期），非负 rx/tx 字节，长期保留，非遥测）、`node_machine_traffic_cursors`（每（服务器、网卡）的 boot_id 与累计字节，用于增量）；无时区列，按写入时的面板时区分日（第 21 节） |
| 0127 `gost_core` | 五表核心 CHECK 接受 gost：inbounds.core（空/xray/sing-box/gost），core_update_tasks、core_remove_tasks、node_traffic_cursors、core_traffic_samples；保留 ID/外键/归属触发器、adding、活跃任务唯一索引和 core_traffic_time，node_traffic_samples 视图语义不变。nodes.gost_core_supported 为 0/1，默认 0，恢复清零/重连刷新 |
| 0128 `gost_tunnels` | `canvas_tunnels.gost_material`（可空密封列；每条隧道一套 CA ＋ 服务端/客户端叶子证书，CA 私钥签发后丢弃、有效期 10 年；轮换替换整列并更新 `updated_at`）。不新增表，隧道归属表、外键与触发器不变 |
| 0129 `tunnels` | `tunnels`（密封材料、草稿/发布标记）、有序 `tunnel_hops`、`tunnel_rules`、节点数字端口唯一的 `port_claims`、ID 归属 `tunnel_objects`；目标公网地址/入站端口变化触发关联规则草稿失效 |
| 0130 `relay_subscription_snapshot` | `tunnel_rules.published_snapshot`：无凭据的成功发布端点快照；旧发布不回填猜测地址，须再次成功发布 |
| 0131 `canvas_forward_migration` | `canvas_forward_reconcile` 单次标记及 migrated_ids、`tunnel_migration_report` 持久原因；启动/恢复事务转换合格画布转发，保留运行对象与凭据 |

备份校验、清单与恢复覆盖这些结构。恢复结束门户会话；当前安全状态、Token 与授权按模块的保守合并规则处理，不用旧备份恢复已撤销访问。排队探测在恢复时过期，运行能力待重新上报。

# 27. API

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
GET    /api/v1/nodes/{id}/metrics?hours=1..168
GET    /api/v1/nodes/{id}/tuning
PUT    /api/v1/nodes/{id}/tuning
GET    /api/v1/nodes/machine-traffic
GET    /api/v1/nodes/{id}/machine-traffic
GET    /api/v1/nodes/{id}/machine-traffic/settings
PUT    /api/v1/nodes/{id}/machine-traffic/settings
GET    /api/v1/nodes/{id}/deletion-preview
POST   /api/v1/nodes/enrollment-token
PUT    /api/v1/nodes/{id}
DELETE /api/v1/nodes/{id}
```

`GET /api/v1/nodes` 默认只返回未删除的节点；`?deleted=1` 只返回已删除的节点（Owner 与 Viewer 都可读）。`DELETE` 需要 Owner：未登记节点等同取消登记（记录移除），已登记节点执行逻辑删除，被引用时返回 409 `node_delete_blocked`，阻塞项（`blockers: [{kind,id,name}]`）位于错误体中（见第 11.3 节）。

`GET /install/ca.pem` 无需登录，返回面板 Web 证书链顶层的 PEM（`application/x-pem-file`，`Cache-Control: no-store`），供节点安装脚本建立对面板证书的信任（见第 11.3 节）；开发模式或证书不可读时返回 404。

metrics 与 tuning GET 均允许 Owner/Viewer；逻辑删除节点返回 404。metrics 默认 hours=24，合法范围 1–168；≤12 小时原始分钟点，>12 小时 epoch 对齐，intervalSeconds=ceil(hours×60/720)×60，sampleSeconds=60。响应 {intervalSeconds,sampleSeconds,points}；空 points 为 []，无测量字段为 null。桶内各字段对非 null 值取算术平均，计数/字节可能为小数，samples 为桶内行数。指标字段见第 11.2 节。

tuning PUT 请求 {settings:对象,clear:布尔,confirmNodeId:节点ID}，拒绝未知 JSON 字段；202 返回 {requestId,status:"pending"}。settings 为完整字符串 profile，空 profile 等同 clear；clear=true 不得带非空 settings。GET 返回 supported、desired、requestId、status、result、requestedAt、updatedAt、current；未操作时 ID/结果/时间为 null、期望为空对象、status=unconfirmed。current 是最后心跳实际读数，可能过期。PUT 权限、并发和错误码见第 22.0 节。

整机流量：GET settings 与两个摘要接口允许 Owner/Viewer；PUT settings 仅 Owner＋CSRF；逻辑删除节点返回 404，字段/设置非法返回 400 `invalid_request`。settings 为 `{method:"sum"|"outbound"|"max", interfaceName, allowanceBytes, resetDay}` 的完整替换，PUT 必须带全部四个字段（含显式 nullable allowance，1–9007199254740991 的整数字节或 null），未知字段拒绝，并写入操作日志“服务器流量设置”。摘要 `{nodeId, settings, interfaceName, timezone, periodStart, periodEnd, nextResetAt, rxBytes, txBytes, usedBytes, status, lastObservedAt}`，字节为十进制字符串，未知为 null，status 为 measured/stale/unknown；批量接口 `{items:[摘要],timezone}` 一次返回全部未删除节点，服务器列表只用它，详情用单节点接口。口径与限制见第 21 节。

## 27.5 节点组

Owner 专用，写操作要求 CSRF。列表及保存响应保留原字段，新增 `inboundIds: string[]`、`imports: [{sourceId,proxyKey}]`、`grantedUserIds: string[]`；`nodeIds` 是只读派生服务器列表。POST/PUT 接受 `inboundIds`、`imports`（最多 500 个入站、200 个导入成员）；仅传 `nodeIds` 而未传 `inboundIds` 时，兼容展开服务器当前订阅可见、非中转的入站。显式空入站数组优先，未知字段仍拒绝。

导入来源 API 继续接受但忽略 `userIds`；响应 `userIds` 为经有效分组可达该来源的去重用户集合（不代表其当前活跃或缓存可用）。`GET /imported-proxies` 项增加 `proxyKey: "name|protocol"`。`user_source_grants` 仅保留供旧备份兼容，不参与访问判断。

节点元数据 GET/PUT 增加可空整数 `portRangeStart`、`portRangeEnd`，须同时设置或同时为空；无效输入返回 400 `invalid_request`。画布未指定中转监听端口时优先在服务器范围内分配，按数字端口跳过该节点上任何已配置入站（含禁用）与本次计划已占用的端口，不区分 TCP/UDP（与 Agent 的节点级数字端口规则一致）；范围耗尽拒绝发布，未设置范围沿用原默认端口。

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

每节点、每核心操作：

```text
GET  /api/v1/nodes/{id}/restart-core            # 列表项含 core
POST /api/v1/nodes/{id}/restart-core            # {expectedRevision,confirmNodeId,core?}；core 留空表示整节点
GET  /api/v1/nodes/{id}/health-checks           # 列表项含 core
GET  /api/v1/core-updates                       # 任务列表，含 component 与 adding
POST /api/v1/core-updates                       # {nodeId,confirmNodeId,revision,component,version,url,sha256}
POST /api/v1/core-updates/{id}/clear
POST /api/v1/nodes/{id}/cores/{component}/remove    # Owner+CSRF；{confirmNodeId,revision}
GET  /api/v1/nodes/{id}/cores/{component}/removals  # 最近 100 个移除任务
```

`restart-core` 的 `core` 必须属于 `Capabilities.cores`，否则 409 `restart_conflict`（核心不可用）；`core` 留空仍是整节点重启，历史回滚也仍是整节点操作。回滚到使用已移除核心的修订同样返回 409，消息与补救方式见第 22.2 节。

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

节点状态变化；配置发布状态；总览统计变化；系统告警。

## 27.12 门户、订阅、观测与画布接口

- `GET /api/v1/service-sets/{id}/references`：Owner 读取共享规则集的节点策略与客户端配置/规则引用，包含启停及活动状态。

Owner 专用 GET/POST /canvas-tunnels、PUT/DELETE /canvas-tunnels/{id}、GET /canvas-tunnels/legacy-objects、POST /canvas-tunnels/{id}/adopt；写入要求 CSRF 并记录审计。服务端连接接受 tunnelId 引用，其余配置由资源读取；客户端拒绝该字段。PUT 隧道要求 updatedAt；端点修改在同一事务中重定向所有引用方案并更新方案令牌。旧内嵌写法按方案 ID + 连接 ID 创建/更新该方案最初创建的隧道，显式引用不隐式修改资源。规划响应含 tunnels:[{tunnelId,affectedSchemes:[{id,name}]}]，冲突增加 reason/objectId。

`POST /api/v1/canvas-tunnels/{id}/rotate-credentials`（Owner ＋ CSRF，体为 `{updatedAt}`，操作日志“轮换隧道凭据”）仅用于仍属旧画布的 GOST 资源，签发新一代材料并标 modified；只读视图只给代数、服务器名、到期与指纹。已迁移对象使用第 27.13 节隧道管理接口。

画布规划接口 `POST /api/v1/canvas-schemes/{id}/plan`（Owner，会话与 CSRF 同 publish）。无正文或 `{}` 规划已保存布局；`{"layout": <CanvasLayout>}` 规划未保存布局。返回 `ok`、按服务器的 `creates/updates/removes`、逐连接状态、监听端口冲突和警告；失败原因另有 `message`。接口仅读取，不写对象、方案、审计或 Revision。它复用发布的规划、生成对象识别、端口冲突与删除引用检查；客户端方案返回 `ok:false`，客户端已有 publish 路径不变。更新表示发布会执行同名对象的保存（不表示字段一定变化）；计划与执行之间状态变化仍可能导致发布失败。

延迟目标 GET 允许 Owner/Viewer，返回 `{custom,targetIds,defaultTargetIds}`；PUT 仅 Owner + CSRF，事务保存并记审计。超过 5 个返回 409 `node_target_limit`，未知目标返回 400 `unknown_target`，重复 ID 返回 400 `invalid_request`，已删除节点返回 404。延迟查询目标包含当前有效目标与查询范围内仍有采样的历史目标。

延迟查询返回 `{intervalSeconds,sampleSeconds,targets,series}`，series 项为 `{targetId,points}`，points 项为 `{at,avgMs,minMs,maxMs,medianMs,sent,failed,samples}`。sampleSeconds 为 60（原始检测周期）；intervalSeconds 表示返回点的桶步长，在不超过 12 小时时为 60，超过时为分桶步长（24 小时 120 秒，168 小时 840 秒）。at 为 UTC 原始时间或桶起点；桶统计含义见 §23，目标选择逻辑不变。

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
GET/POST /client-policy-profiles
GET/PUT/DELETE /client-policy-profiles/{id}
POST   /client-policy-profiles/{id}/activate
POST   /client-policy-profiles/preview
GET    /client-policy-candidates?userId={id}
GET/POST /subscription-sources
PUT/DELETE /subscription-sources/{id}
POST   /subscription-sources/{id}/refresh
GET    /imported-proxies
GET/PUT /nodes/{id}/meta
GET/POST /probe-targets
PUT/DELETE /probe-targets/{target}
GET/POST /nodes/{id}/tcp-probes
GET    /nodes/{id}/latency?hours=24
GET/PUT /nodes/{id}/latency-targets
POST   /inbounds/{id}/reachability
POST   /nodes/{id}/publish
GET    /revisions?nodeId={id}            (a node's revisions; there is no /nodes/{id}/revisions route)
GET/POST /canvas-tunnels
PUT/DELETE /canvas-tunnels/{id}
GET    /canvas-tunnels/legacy-objects
POST   /canvas-tunnels/{id}/adopt
POST   /canvas-tunnels/{id}/rotate-credentials
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

## 27.13 隧道管理与节点中转

以下路径均以 `/api/v1` 为前缀。GET 允许 Owner/Viewer，唯 migration-report 仅 Owner；全部 POST/PUT/DELETE（含只读 plan 和端口建议）要求 Owner＋CSRF，实际写操作记审计。门户会话不能访问。PUT 与轮换须带上次读取的 updatedAt，不匹配为 409 stale；DELETE 无该请求字段。请求/响应字段详见[v1.4 API 合同](docs/v1.4-plan.md#5-api-contract-fixed-before-u1a-backend-must-match)。

| 接口 | 合同 |
| --- | --- |
| `GET/POST /tunnels`、`PUT/DELETE /tunnels/{id}` | 列表 `{items,total}`；保存返回 Tunnel；删除 204，在用返回 409 tunnel_in_use 与 rules，已发布则 pending_delete |
| `POST /tunnels/{id}/rotate-credentials` | `{updatedAt}` → Tunnel，仅轮换草稿 |
| `GET/POST /tunnel-rules`、`PUT/DELETE /tunnel-rules/{id}` | GET 可按 tunnelId/nodeId 筛选；保存返回 TunnelRule；已发布删除待成功发布移除 |
| `GET /nodes/{id}/ports/check?port=` | `{nodeId,port,free,usedBy?}`，含禁用入站占用提示 |
| `POST /nodes/{id}/ports/allocate` | `{count?}` → `{ports:[]}`，只建议不预留；保存时重新查冲突 |
| `POST /tunnels/plan` | `{tunnelIds:[],ruleIds:[]}` → `{ok,servers,conflicts,warnings,message?}`，仅读取 |
| `POST /tunnels/publish` | 同一选择体 → `{batchId?,servers}`；逐节点 revision 可空，status 为 applied/unchanged/failed/rolled_back/skipped |
| `GET /tunnels/migration-report` | Owner 专用，`{items:[{tunnelId,name,reason}]}`；单次转换记录，不是实时问题列表 |
| `GET/POST /nodes/{id}/relays` | GET 返回 `{items:[{rule,shareLink}]}`；POST `{tunnelId,inboundId,portMode,listenPort?,subscriptionVisible,name?}` 创建本目标节点的 linked 规则，network=both；shareLink 权限见第 10.3 节 |

端口错误：409 port_in_use（含 usedBy）、400 port_out_of_range、409 no_free_port（无范围或耗尽）。auto 无 listenPort 则保存时分配；带建议端口则空闲时保留，后续重发固定。pending_delete 只读并继续保留声明/引用。

# 28. gRPC 节点通信

节点通信使用 Enroll 与双向流 Connect。

Agent 上报消息包括：

Hello；Heartbeat；Capabilities；ApplyResult；HealthResult；TrafficBatch；AccessBatch；UpdateResult；CertificateRequest。

Heartbeat 省略即缺测，实测 0 保留。v1.3 增加 `boot_id`（字段 34）与 `default_route_interface`（字段 35）供整机流量记账（第 21 节）：两者各自独立，读取失败只省略自己的字段，旧面板忽略未知字段。整机网卡的累计字节沿用既有 `interfaces`（字段 20）。

GOST 使用两个能力字段：`Hello.gost_core_supported`（字段 13，bool）表示该 Agent 懂 GOST 组件的安装与移除——即使 GOST 尚未安装；`CoreCapability.gost_contract_version`（字段 9，uint32）表示 GOST 设置契约版本（0 = 不支持，1 = 基础转发，2 = 池/范围/限流，3 = 加密中继，4 = 中间 relay 的下一跳链；开发分支 Agent 报告 4，正式版 1.3.1 为 3）。隧道发布按各跳能力要求两节点 ≥3、三节点 ≥4。GOST 设置随既有 `ApplyCommand.desired_state_json` 下发，不占用独立字段；没有新的 Welcome 标志，多核心与节点流量协商照旧。面板把 Hello 标志持久化在 `nodes.gost_core_supported`，恢复时清零、重连刷新，不用 Agent 版本号文本推断；旧 Agent 无法收到 GOST 分配。

中央端下发消息包括：

Welcome；DesiredState；ApplyCommand；RollbackCommand；HealthCommand；CertificateRotate；UpdateCommand；Ack。

每条消息包含：

节点 ID；消息 ID；顺序号；时间；配置 Revision；消息类型。

节点保存最后确认的消息顺序，避免断线重连后重复执行配置命令。

# 29. 安全设计

## 29.1 后台安全

终端用户认证与管理员认证隔离：门户 Cookie 为 `qui_portal_session`（Secure 时使用 `__Host-` 前缀），HttpOnly、SameSite=Strict、Path=/；会话表只存 Token 哈希。复用密码哈希、限流、同源 CSRF 与会话时长策略。禁用登录、改密码、撤销会话和删除用户立即结束相应会话；恢复备份结束门户会话。门户仅从会话取用户 ID，不接受请求指定其他用户；不展示域名访问记录。订阅链接不存浏览器 localStorage。

管理员密码安全哈希存储；HTTPS；安全 Cookie；登录失败限制；会话超时；CSRF 防护；API 请求大小限制；Owner 与 Viewer 权限隔离；敏感数据默认隐藏；管理操作日志。

## 29.2 节点安全

- Agent 主动连接中央端
- gRPC 使用 mTLS
- 每个节点独立证书
- 每个节点独立私钥
- 节点证书支持轮换和吊销
- 自动续证仅在控制连接内执行：NotBefore + 30 天或剩余不超过 14 天，原私钥不变；失败退避 1 小时，离线期间不执行，重连检查。90 天签发期不变。服务端准入须与之一致，当前如此：只在两个条件均未满足时拒绝自动申请，仍保留身份、原密钥、回执与确认检查。
- 一次性注册码注册
- 不使用 SSH 作为日常控制方式
- 不提供任意命令执行

## 29.3 凭据安全

- 用户凭据与节点间凭据分离、订阅 Token 的哈希查找与加密副本规则、节点私钥不上传中央端：见第 9.3 与 11.3 节。
- 节点间 GOST 凭据与用户凭据隔离，隧道管理存 `tunnels.gost_material`，旧画布材料存 `canvas_tunnels.gost_material`；三节点按段独立 CA 与服务端/客户端叶子证书。Vault 密封、CA 私钥签发后丢弃、有效期 10 年；API/计划/日志只露代数、服务器名、到期与指纹。轮换换整套草稿，发布才生效；删除不会撤销离线节点仍生效的凭据（见[运维手册](docs/operations.md)第 20–21 节）。
- 敏感配置写入节点时限制文件权限
- 配置和备份中的敏感字段加密保存

## 29.4 数据安全

只记录域名级信息；不保存内容数据；按保留周期自动清理；访问记录查询需要权限；SQLite 备份使用一致性快照；恢复前执行数据库完整性检查。

# 30. 部署目录

## 30.1 中央端与更新公共目录

| 路径 | 用途 |
| --- | --- |
| /usr/local/bin/proxy-panel、qui、qui-update | 面板、root 菜单、更新器 |
| /etc/proxy-panel/config.yaml、tls/ | 配置、Web TLS |
| /var/lib/proxy-panel/ | panel.db、identity/（节点 CA）、backups/、rulesets/、releases/ |
| /var/lib/proxycontrol-install/ | root 0700；安装记录、operation.lock、update.lock、更新中的 rollback.*/（文件快照、updater、service.sh、journal） |
| /usr/local/lib/proxycontrol/service.sh | systemd/OpenRC 共用服务封装 |
| /usr/local/lib/proxycontrol/update-recovery | 开机恢复；systemd 的 proxycontrol-update-recovery.service 或 OpenRC 同名单元，不记安装记录，排在 Agent/面板/强制出站之前 |

日志写 stdout → journald，OpenRC 经 logger → syslog，不写 /var/log 文件。更新前备份名为 pre-update-<版本>-<时间>.db，回滚保留 failed-update-<版本>-<时间>.db。

`qui-update --version X.Y.Z[-rc.N] --component panel|agent` 支持 RC 与组件范围（快照/日志/回滚），组合主机明确先 panel 后 agent；省略 component 的默认仍先 Agent 后面板。`--database` 仅面板，跨迁移降级须匹配备份。首次升级步骤见[部署](docs/deployment.md) §5.0。

## 30.2 节点端

| 路径 | 用途 |
| --- | --- |
| /usr/local/bin/proxy-agent、qui-update | Agent 引导镜像、更新器 |
| /etc/proxy-agent/config.yaml、tls/ca.crt | 配置、受信面板 CA |
| /usr/local/lib/proxy-agent/<name> | 安装器核心二进制 |
| /var/lib/proxy-agent/identity/ | 节点身份与证书 |
| /var/lib/proxy-agent/revisions/、spool/、releases/ | 修订、缓存、Agent/核心镜像 |
| /var/lib/proxy-agent/runtime/<core>/ | 逐核心运行状态、日志、临时文件 |
| /var/lib/proxy-agent/host-network/、tuning/ | Agent 的 desired.json 请求与 helper 的 status.json 回执 |

强制出站 helper：proxy-agent-host-network.service（oneshot，仅 CAP_NET_ADMIN）执行 `proxy-agent --apply-host-network /var/lib/proxy-agent/host-network`，.path 监视并开机重放，只维护 nft 表 inet proxy_agent_source；卸载删除表/单元。OpenRC 的 /etc/init.d/proxy-agent-host-network 监管 /usr/local/lib/proxy-agent/host-network-watch（busybox inotifyd）。Agent 本身由 systemd 或 /etc/init.d/proxy-agent 的 supervise-daemon 管理。

调优 helper：Agent 写 0600 请求；root oneshot proxy-agent-tuning.service/.path 执行 `--apply-tuning /var/lib/proxy-agent/tuning`。CAP_NET_ADMIN＋CAP_DAC_OVERRIDE（读写 Agent 0700/0600 目录），fs.file-max 靠 root 权限。systemd 仅开放固定 procfs 键、/etc/sysctl.d、请求目录；OpenRC 的 proxy-agent-tuning 监管 tuning-watch，无文件系统命名空间隔离。root 0600 的 /etc/sysctl.d/.proxy-tuning-snapshot.json 保存原值/管理键，flock 串行，按 request_id 回逐键结果，Agent 最多等 30 秒，超时未确认。clear/空配置删 99-proxy-tuning.conf 并恢复原值；卸载先恢复再移单元。写入拒绝即失败，不扩权/加载模块。主机信息随能力刷新，不可读心跳值未知；磁盘与 disk_percent 均指状态目录文件系统。

install.sh/node-install.sh 可重复 `--core xray --core sing-box --core gost`，新装缺省仅 Xray。显式安装将顶层规范为 cores 列表，保留首项 core_name/core_path 供旧 Agent，其余配置/身份不变；手工版本/URL/摘要紧跟对应核心。升级不带核心参数保留全部记录核心与 config 原文；显式参数只替换/追加，省略不删除。`--core-source qui|official` 仅 sing-box，维持架构/libc、官方 musl 与同版本 qui 选项。

GOST 固定官方 3.3.2 静态 linux/amd64（glibc/musl 同包），其他架构拒绝；不能独立或排首项，按已有顺序升级不受影响。Agent 托管 runtime/gost，无独立单元；service/enroll/续证/update/recovery/uninstall 的固定路径集合包含它。降级 Agent 到 1.3 以下前先移除 GOST。

安装记录 v2：1=proxycontrol-install-v2，2–4=组件/程序摘要/主服务摘要，5–6 空，7=installed，8–9=强制出站摘要，10=init（openrc 表 OpenRC），11–12=调优摘要，13 起 `core <name> <absolute-path> <二进制sha256>`。面板写 12 行无核心；兼容 v1 的 5–6 核心/摘要及可缺省 init/调优，升级迁至 v2；enroll/续证/CA 脚本兼容两版。

qui-update 快照/回滚全部核心路径、Agent config、记录、主服务/强制出站/调优文件，恢复依赖固定路径集合而非待替换记录。首次迁移操作见[部署](docs/deployment.md) §1、§5.0；v2 Agent 拒绝不支持 v2 的目标 installer 降级，失败仍可恢复原 v1 记录/config。uninstall 验证并删记录核心，保留配置/身份/状态，先清调优。验证范围见[验收矩阵](docs/v1-acceptance.md) §11–12。

# 31. 备份与恢复

v1.4 开发分支：`tunnels/tunnel_hops/tunnel_rules/port_claims/tunnel_objects`、0130 的 `published_snapshot` 和 0131 的迁移标记/报告随归档保留并校验。旧归档先升级再运行一次性转换；新归档保留原有表及凭据，恢复仍把已发布对象标为 modified，须 Owner 显式发布。校验拒绝孤立或重复归属、缺失/冲突端口声明、损坏发布快照和迁移标记，不自动重建可能改变监听的声明。

备份 SQLite、系统配置、中央证书、规则集、必要密钥加密副本；默认保留每日 7、每周 4、每月 3。恢复须停写并验证文件/数据库完整性；操作与恢复后核对见[运维](docs/operations.md) §2。

| 数据 | 备份/恢复合同 |
| --- | --- |
| client_policy_profiles、system_settings.client_policy_active/client_rules | 加密备份与恢复校验覆盖配置库、活动指针和模板快照；旧归档初始化配置库并保留 activeRevision，新归档校验指针与活动模板一致性 |
| node_latency_samples、定时探测队列、node_metric_samples | 不进便携备份：快照清空再 VACUUM，恢复候选再清空，不影响运行库样本；手动排队探测过期 |
| scheduled 默认目标、node_latency_targets、latency_targets_custom | 属配置；兼容 0121 前归档并默认继承。进程内恢复保留当前自定义（含空表），补其新目标但不提升为系统默认 |
| node_tuning | 备份/校验/清单包含；恢复以当前库为准，删归档独有行、保留当前期望/结果，pending→unconfirmed，不重发、不写宿主 sysctl；旧归档迁移后同处理，需核对 tuningCurrent |
| 多核心配置/任务 | inbounds.core、nodes 的 multi_core_supported/node_traffic_supported/gost_core_supported、health_checks/restart_requests.core、core_update_tasks.adding、core_remove_tasks 纳入备份；validateMultiCore 要求结果 JSON 与 id/component/revision/code/status 一致，不能同组件同时排添加/移除。移除表清空后复制归档，queued/running 更新/移除均变 unconfirmed、不重发；能力位待重连刷新 |
| node_traffic_cursors、core_traffic_samples | 随访问数据恢复，避免同段字节重复计量 |
| canvas_tunnels.gost_material | 随加密备份且纳入校验，原样恢复不重签；不自动发布，界面 modified，Owner 下次发布才下发 |
| tunnels、tunnel_hops、tunnel_rules（含 published_snapshot） | 加密备份/清单/校验覆盖；保留端口、版本与密封材料，不重签；恢复已发布对象标 modified，须 Owner 显式发布 |
| port_claims、tunnel_objects | 保留 ID 归属与历史声明；拒绝孤立/重复归属、缺失或冲突声明，不自动重分配 |
| canvas_forward_reconcile、tunnel_migration_report | 保留单次完成标记、已迁移 ID 与原因；0129 前归档先升级再转换，已完成的不自动重试；事务失败可重试，转换不改运行修订/入站/证书 |
| node_machine_traffic_settings/daily/cursors | 三表长期保留、全部加密备份，非 7 天遥测；validateMachineTraffic 校验设置/日期/字节/主外键，0126 前无表合法，清单有三项计数。冻结当前库已到 0126 则整组复制当前表（空表也权威），否则保留归档；不丢当前量、不重复/复活旧量 |

调度随停机/进程内恢复取消并等待退出，新实例仅一个循环。其他模块的会话结束、安全状态与撤销访问保守合并规则见第 26.9、29 节。

# 32. 开发流程

新增能力通过既有模块接口扩展，不修改已应用迁移。

每个变更在 Linux 测试机运行 `scripts/check.sh` 与真实核套件。GitHub `check` 每个发布提交手动派发，另有每周运行；`cores` 仅每周或派发 `cores=true`。`v*` 标签只构建发布，并要求同 SHA 已有成功 `check`。选包、执行命令与依赖见[部署与升级](docs/deployment.md) §2。

# 33. 测试要求

## 33.1 单元测试

重点测试：

用户节点组展开；订阅节点过滤；用户协议过滤；域名匹配；策略优先级；代理链环路检测；最大跳数检查；协议能力判断；配置编译；访问记录聚合；数据保留清理。

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

中央端离线；Agent 离线；下一跳节点离线；错误配置；节点端口占用；代理核心启动失败；节点本地缓存写满；节点证书过期；SQLite 锁等待；备份恢复。

# 34. 验收标准

验收须覆盖以下 25 条，按下方当前口径判定：

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

补充判定口径：

- \* 第 3 条：Xray 运行官方发布二进制；sing-box 可选官方或 qui 的未修改上游源码构建（上游标签），能力缺失明确降级。
- \* 第 7 条：设备限制按第 9.2 节的地址估计执行，缺能力显示部分执行；额度仍由面板自动版本软执行，正常路径窗口见第 9.1 节。
- \* 第 16 条：出口组按上游能力判定，权重策略已移除；固定出口失败不隐式 Direct。
- \* 第 19 条：每域名字节可以为空并显示“—”，出口记录为“配置出口”且不声称是跨节点实际出口。
- \* 第 7、19、24 条：有 `singbox_user_stats` 的 sing-box 流量进入每用户额度；官方无标签构建维持未知与节点总量。在线设备是源地址估计，账户字节与每域名字节分别判断。

L8（部分协议 UDP 不支持设备限制）、L9（保留样本不能还原清理后的完整月份）、L10（TCP 探测不能证明 UDP/QUIC 可用、转发无用户身份）仍成立；不得用本地测试通过替代实机或浏览器证据。

执行方式、证据要求和实机清单见[验收矩阵](docs/v1-acceptance.md)。

# 35. 明确不做

为保持轻量，不包含：

微服务；Kubernetes；Redis；Kafka；Elasticsearch；ClickHouse；高可用中央控制集群；商业套餐；支付；订单；工单；邀请返利；多租户；经销商；动态插件市场；无限多跳代理链；完整数据包记录；网页内容审计；HTTPS 中间人解密；Web Shell；任意远程命令执行；每条连接实时同步写数据库；大型日志分析平台。

# 36. 扩展边界

当前没有下一版本计划。完整系统 TUN、Passkey、OIDC、更细管理员权限、灰度升级及更大规模存储/多中央等仅为可能方向，不是交付承诺。新增能力须通过现有模块接口扩展，保持第 3、35 节的架构与安全边界；已支持协议见第 12、13 节。

# 37. 项目运行流程

用户取得按授权节点组生成的订阅 → 选择入口并以协议 A 接入 → 节点识别用户与域名、执行本地策略 → Block、Direct、协议 B 中转或 A→B→C 链 → 节点聚合域名/动作/配置出口记录并批量上报。

中央与节点职责见第 4、23 节；中央失联时按第 3.5 节继续运行。

# 38. 项目总结

以统一数据模型、Go 模块化单体、声明式节点发布与可替换核心适配层，完成少量内部用户的多节点、多协议访问管理，保持轻量部署和清晰的安全边界。
