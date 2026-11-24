# 轻量多节点代理控制平台项目说明书

面向 1～10 名内部用户，自托管，用于自有或明确授权网络中的代理接入、节点管理、分流与域名级访问控制。本文规定当前产品行为、字段语义、安全边界与接口；源码为准，变更时同步修正文档。

## 当前状态

当前正式版为 **v1.10.3**（tag → `c910e89`，迁移头 **0163**）；上一正式版为 v1.10.2（tag → `7a6add0`，迁移头 0163）。v1.10.3 减少面板更新停服期间的磁盘工作，修复快照及预检目录残留，统一心跳新鲜度常量并整理文档入口；本轮未连接或升级生产；Release 见 https://github.com/jingniao/qui/releases/tag/v1.10.3。最低兼容版本为 **v1.7.5**，其有效面板数据与在途任务继续保留。

生产状态见[运维手册「当前生产环境」](operations.md#当前生产环境)（管理员登录入口不写入文档）。生产操作前须重新实时核实实际版本与节点状态；逐版本发布与部署记录见[变更记录](changelog.md)。

## 1. 文档目的

为产品、前后端、Agent、数据库、核心适配、测试与运维提供统一规范。需求变化须同步本文件与相关文档。

# 2. 项目定位

轻量多节点代理网络控制平台，提供用户访问管理、节点组订阅、分流与阻断、域名记录、协议转换中转和链式代理。不以商业计费、支付、多租户或大规模运营为目标。

保持部署简单、资源占用低、配置清晰、节点稳定，便于增加节点和协议；中央失联节点继续运行，错误配置自动回滚。可按域名控制流量经其他节点转发，只审计域名元数据。

# 3. 项目核心原则

## 3.1 轻量优先

中央端为 Go 单程序，嵌入前端并使用 SQLite。不强制引入 Kubernetes、Redis、Kafka、RabbitMQ、Elasticsearch、ClickHouse、Prometheus、独立配置中心/任务队列、微服务或服务网格。

## 3.2 模块化单体

用户、节点、订阅、代理、分流、发布、访问记录、统计与系统模块边界清晰，通过 Go 接口或应用服务调用，不通过内部 HTTP 或消息队列通信。

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

Vue 3、TypeScript、Vite、Vue Router、轻量状态管理与 Fetch 请求封装，构建后嵌入中央程序，生产不运行 Node.js。负责状态、用户凭据、节点组、入站/出站、出口组/代理链、策略、访问记录、发布/回滚与系统设置。

视觉规范、排版层级与配色统一遵循[设计系统](design-system.md)：代理协议徽标复用 `web/src/components/ProtocolBadge.vue` 与 `web/src/components/protocolColors.ts`，同一协议在全站保持同一颜色，不得按页面或方向另配颜色；正文与标准控件保持 14px，不以缩小文字解决布局拥挤。

## 4.2 中央控制服务

Go 模块化单体，提供 REST、SSE 与 gRPC，使用 SQLite。负责管理员认证、用户隐藏接入身份与节点组授权、订阅、代理资源与策略、配置版本和发布、访问记录、流量、统计与备份恢复。

## 4.3 Node Agent

Go 单文件程序，由 systemd 或 Alpine/OpenRC（含 LXC）管理；通过 gRPC 双向流与 mTLS 主动连接。负责注册/证书、能力与状态上报、配置编译/原生校验/应用/失败回滚、核心管理、流量及域名采集、本地持久队列和断线补传。

## 4.4 代理核心

`cores: [{name,path}]` 列出启用核心，主核心为第一个用户内核（Xray 或 sing-box），由 `Inventory.Primary` 显式记录；允许空列表或仅 GOST，此时主核心为空。节点均使用 MultiRuntime 与 `runtime/<core>/`；节点绑定的认证清单 `cores.json.enc` 与 YAML 合并并持久覆盖禁用状态。安装/移除/回滚见第 22.2 节；Agent 不改安装器二进制与安装记录。

Xray、sing-box 承载用户接入、身份、分流、Direct/Block、代理出站、中转/链与统计，协议按能力上报（第 11–13 节）。GOST 固定官方 3.3.2（仅 `linux/amd64`），只承载 `forward`（TCP/UDP）与加密中继，不承载用户身份、凭据、订阅、额度、门控、源观测、域名记录、出站资源、策略或出口组；转发按自身目标直连，仅要求启用的 direct 默认出站。

`desiredstate.Validate` 与 `ValidateCoreProtocol` 拒绝 GOST 的非 forward 入站；空 `core` 随主核心；能否安装 GOST 由 `Hello.gost_core_supported` 决定，不从版本号推断。GOST 永不成为主核心；支持 `core_optional_supported=true` 的 Agent 可没有用户内核（Xray/sing-box），旧 Agent 仍保留最后一个用户内核（`last_core`）。隧道管理要求各跳已启用 GOST，两节点契约 ≥3、三节点 ≥4，接收跳须公网地址或连接地址覆盖（第 14 节）。

# 5. 总体架构

管理员浏览器 → HTTPS/SSE → `proxy-panel`（嵌入 Web、REST、SQLite、管理与发布）→ gRPC/mTLS → 多台 Node Agent → 各节点核心（接入、中转、链与出口）。控制面只运行一个中央程序。

建议监听 `22550`（Web/REST/SSE/订阅）、`18880`（Agent gRPC/mTLS），两者均可修改；已有安装保持原端口。用户数据不经过中央端。

# 6. 用户角色

后台有 Owner 与 Viewer 两种角色。

## 6.1 Owner

拥有全部权限：管理后台管理员；管理用户、凭据、共享订阅与门户登录；管理节点、节点组与用户授权；管理入站、出站、出口组、代理链、隧道与画布；管理分流规则与网站访问控制；查看访问记录；发布与回滚配置；管理 Agent 与核心版本；备份与恢复；修改系统设置。

## 6.2 Viewer

只允许查看总览、节点状态、节点基础信息、流量统计与监控类通知（`node_offline`/`cpu`/`memory`/`disk`）。默认不能修改配置、查看敏感凭据、查看详细访问记录、发布或回滚配置、管理用户和管理员。底层权限保留细粒度权限点以便扩展。

## 6.3 终端用户与门户

代理用户在根地址 `/` 使用用户名与 Owner 设置的门户密码登录；管理员登录页位于自定义入口（默认 `/login`，第 27.18、29.1 节），未登录访问后台一律回到 `/`。门户提供账号状态（流量进度、到期天数、下次重置日期）、按客户端列出的订阅链接与二维码、授权节点及本人开放协议、流量图表与改密；登录频控 429 提示 15 分钟后再试。

门户会话与管理员的表、Cookie、CSRF 与路由守卫完全隔离。禁用、到期或超额用户在登录开启时仍可登录查看原因，但不能获取订阅内容。Owner 门户预览为只读，不签发 Token、不写数据。

# 7. 网站功能模块

```text
系统总览

用户管理（用户详情内管理订阅、登录与凭据）

服务器
├─ 服务器列表
├─ 版本与更新（Agent 与代理核心合并管理）
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
└─ 规则集

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

配置版本、发布、差异与回滚在节点详情内操作。终端用户另有门户入口：概览、订阅、节点、流量、账号。

# 8. 总览模块

总览用于快速了解系统状态，不做复杂大屏监控；响应式网格布局、卡片样式与悬停交互见[设计系统](design-system.md)。Viewer 不展示近期阻断。

主要展示：在线用户数、在线设备数、在线节点数、当前连接数、今日上传/下载流量、今日阻断次数、异常节点数、服务器剩余价值、待处理问题、节点流量（今日/本月排行）、用户用量与到期、近期阻断、简单拓扑与最近发布结果。

节点状态统一语义色：绿色（正常）、黄色（状态降级）、红色（异常）、灰色（离线）、蓝色（发布或更新中）。

**服务器剩余价值与汇率**：统计全部未删除服务器（含离线/维护/停用），展示总价值、月均支出与剩余价值，默认 CNY，支持切换 CNY/USD/EUR/HKD/GBP/JPY/TWD/SGD/AUD/CAD/NZD/CHF/KRW/MYR/THB/PHP/IDR/VND/INR/RUB/TRY/AED/BRL（前端 `CURRENCY_LIST` 与后端 `supportedCurrencies` 须一致）。未填价格不计入，零价按零处理，一次性费用不计月均与剩余价值，已到期周期费用剩余价值为零，缺日期或不支持周期标注未计入台数。

- `GET /api/v1/dashboard/exchange-rates`（Owner/Viewer）固定请求 `https://open.er-api.com/v6/latest/CNY`（无密钥、不携带用户/服务器数据），返回 `{base,updatedAt,nextUpdateAt,source,sourceUrl,rates}`（1 CNY 兑换全部支持币种的金额）。面板在 `nextUpdateAt` 前复用内存结果；校验 `updatedAt > 0` 且不晚于当前时间、`nextUpdateAt > updatedAt` 及基准/全部支持币种数值有效。失败返回 502 `exchange_rates_unavailable` 与原始原因，不回用旧缓存、不使用固定汇率。
- 前端统一按 `原金额 ÷ 原币种汇率 × 展示币种汇率` 换算；财务数据按面板时区每日读取一次缓存在浏览器内存，增改删服务器续费后即时更新，失败不缓存。

**节点流量与运行计数**：`GET /api/v1/dashboard` 的 `nodeTraffic` 按未删除节点返回 `{nodeId,todayUpload,todayDownload,monthUpload,monthDownload,measured,historyError?}`，由 `access` 与 `statistics` 统一读取 `node_traffic_samples` 与核心小时汇总（面板时区当日/自然月半开区间，口径见第 21 节），不读取心跳累计字节。本月无样本 `measured=false` 显示「—」；月边界切入已合并小时带 `historyError`，该月显示不可用而今日仍可读。`counts.activeConnections/onlineUsers/todayBlocked` 中任一启用节点未上报时前两项显示「—」并提示部分节点未测。待处理问题的核心异常仅检查在线且已有配置修订的节点，优先读 `heartbeat.cores`，未发布配置不计核心异常。

# 9. 用户管理模块

## 9.1 用户信息

字段：用户名、显示名称、启用、到期、`traffic_limit`/`used_traffic`、`device_limit`、估计在线设备数/执行范围、门户开关/密码/最后登录、重置周期（不重置/每月/每 30 本地日）/锚点、授权节点组、备注。每用户自动拥有全部受支持协议，`allowedProtocols` 仅作兼容字段不再限制。

`device_limit` 范围 0–100（0 不限）；旧 `maxDevices`/`maxConnections` 已从 API 移除，数据库旧列 `max_devices`/`max_connections` 仅为 v1.7.5 数据兼容保留。

每用户唯一跨节点额度，无每节点预算、预算转移或核内字节配额（历史修订中的 `budget` 字段仅兼容读取）。面板累加 Agent 增量至 `used_traffic` 并同步隐藏身份用量。禁用、到期、超额、删除及提额、清零、续期、启用均触发自动额度版本（第 22.1 节）移除或恢复凭据。正常路径超额切断延迟最多为上报周期（15 秒）+ 合并窗口（60 秒）+ 发布耗时；Linux Xray 可 splice 的 TCP（Direct 转发、识别内层 TLS 后的 XTLS Vision 等）在连接关闭后才计下行，长连接相应延后。账户额度只计 Xray 与上报 `singbox_user_stats` 的 sing-box；无统计标签的官方 sing-box 仅报节点总量，用户未知。

## 9.2 用户接入身份与设备限制

每用户唯一隐藏接入身份，沿用 `DeviceID` 与旧 Agent 合同并保留历史记录，系统不再提供设备管理界面或 API。

设备限制为跨入口节点的源地址估计（IPv4 单地址、IPv6 `/64` 去重，先到先得），面板完整源地址仅存内存、不入库/日志/备份，页面统一掩码显示（IPv4 `/24`、IPv6 `/48`）。超限通过 TTL 运行时门控生效，不触发配置修订；WireGuard 一次一端不计限制，前置 CDN 或中继隧道会合并来源，部分协议的 UDP 不支持门控（`deviceGateCoverage=full/tcp_only/none`），缺测或核心无能力时明确显示「未知」或「部分执行」。

完整的活跃/保留窗口、采样间隔、门控租约、各核心协议 UDP 覆盖表与边界见[设备限制](device-limit.md)。

## 9.3 凭据管理

按协议管理用户名/密码、UUID、Shadowsocks 密钥、Trojan 密码、WireGuard 密钥与订阅 Token；用户凭据与节点间中转凭据严格分离。

订阅 Token 以哈希查找，新签发 Token 另存 vault 加密副本供 Owner 与本人查看；无加密副本的旧 Token 返回 `not_recoverable`，重置后可查看。每用户一条共享订阅；重置链接立即撤销旧 Token，重置凭据在自动版本生效后使旧代理设置失效。门户密码与代理凭据分离。

# 10. 用户订阅与节点组

## 10.1 节点组授权

Owner 在节点组页维护授权账号与成员（原用户授权 API 保留）；仅 `subscription` 与 `mixed` 用途可新增授权，其他用途仅允许撤销已有授权。

用户接入来自三类具体成员：自建入站（`node_group_inbounds`）、关联目标入站或导入节点的隧道规则（`node_group_relays`）及导入成员（`node_group_imports`），不再由服务器成员隐式授权。
- 隧道规则成员与入站直连成员互不连带，可分别授权；仅获隧道规则的用户也会在目标自建入站获得凭据（订阅不显示直连但不阻止自行直连）。新建规则不自动入组，删除规则随外键移出分组。目标为导入节点的 `import` 规则不贡献凭据下发节点。
- 导入成员上限 1000：单独导入（`Kind=content`）的 `proxy_key` 为稳定 `id:<id>`，拒绝整源成员（允许历史合法悬空精确成员）；订阅连接（`Kind=url`）支持 `''`（整个来源）与 `name|protocol`（单节点）。
- `node_group_servers` 保存显式服务器（最多 100 个存在且未删除服务器；省略/null 保留，`[]` 清空），`node_group_members` 为显式服务器与入站所属服务器的只读去重并集（响应字段 `serverIds` 与只读派生 `nodeIds`），用于节点策略、筛选与批量操作，不授予凭据或订阅可见性。保存请求不接受 `nodeIds`。

订阅按「用户 → 授权组 → 合格入站/规则/导入 → 可用性与协议过滤」生成；组变更后触发自动额度版本（第 22.1 节），仅访问面变化的节点生成修订。

## 10.2 节点组类型

- `subscription`：用户订阅入口节点
- `relay`：节点间中转节点
- `egress`：系统出口节点
- `mixed`：混合用途

纯中转或纯出口节点可设为不在用户订阅中显示。

## 10.3 订阅过滤

订阅生成依次执行：
1. 验证用户启用状态、到期状态与订阅 Token。
2. 展开启用、订阅可见且用途为 `subscription`/`mixed` 的授权节点组，取得入站、规则与导入成员并去重。
3. 过滤禁用与不允许订阅的节点。
4. 按成功发布的完整线路依赖索引（`subscription_route_dependencies`），过滤任一受管跳（入口、中间、出口及固定链）处于禁用、维护或确认离线的整条线路；离线沿用第 27.11 节持续时间，等待恢复期间保留。纯导入端点无受管机器身份不按 IP 推断状态；依赖材料缺失或损坏明确报错，不返回假成功空订阅。此过滤仅影响订阅与预览，不改凭据或节点运行配置。
5. 仅保留组内可用入站；非空 `allowedUserIds` 须含本人，且核对已发布凭据（按入站授权，同服务器其他入站不自动获得凭据）。
6. 仅保留有客户端连接地址（`connectHost`，否则 `publicIp`，第 11.1 节）的入站；缺失地址节点不生成端点，订阅预览明确给出警告。
7. 输出订阅内容。同节点同协议多入站命名为 `节点 · 协议 · 端口`，单个为 `节点 · 协议`；汇总遇重名保留首个端点并警告。

**隧道规则订阅端点**：
- 关联自建目标的 `linked` 规则按成功发布快照（`published_snapshot`）追加端点：需已发布、启用、订阅可见且本人授权组含该规则，目标入站须通过可用性与已发布凭据检查（不要求目标入站本身在用户分组中）。保留目标协议、凭据、传输与 TLS 参数，连接地址/端口替换为入口客户端连接地址（空时回退快照地址）与规则端口，原隐含 SNI 显式写为目标地址，名称取规则名（重名按端口消歧，地址与名称变更免重发生效）。QUIC 规则清空跳跃端口与间隔；Hysteria2/TUIC/WireGuard 按 UDP、其余按 TCP 匹配规则 `network`。
- 目标为导入节点的 `import` 规则同理按发布快照与授权筛选，取导入节点当前公开参数与凭据（凭据刷新即生效），替换入口地址/规则端口、显式原 SNI、清空 Hysteria2 跳跃端口，引用为 `relay:<规则 ID>`。
- 草稿改动或目标变化不推进快照，`pending_delete` 保留旧快照直至发布移除。节点中转 API 的 `shareLink` 仅 Owner 可得，取首个有合格已发布凭据的用户生成预览。

**格式导出与协议边界**：支持通用 URI 列表、Base64 URI 列表、sing-box JSON（出站片段）、mihomo（`clash`，别名 `mihomo`/`clash-meta`）与 legacy Clash（`clash-legacy`，别名 `clash-premium`）。格式选择：`?format=` 优先（未知 400）→ 客户端 User-Agent 匹配（不区分大小写、较长优先：`clash`/`mihomo`/`clash.meta`/`clash-verge`/`clashmetaforandroid`/`flclash`/`clashx meta` → `clash`；`clashforwindows`/`clashx/` → `clash-legacy`；`sing-box`/`sfa/`/`sfi/`/`sfm/`/`sft/` → `sing-box`；`v2rayn`/`shadowrocket`/`nekobox`/`nekoray` → `base64`）→ 系统默认。
- 通用 URI/Base64 省略钉了 `trusted_ca_pem` 的端点；sing-box 写 `tls.certificate`，mihomo 写第一张证书 DER 的 SHA-256 `fingerprint`。
- legacy Clash 过滤 VLESS、Hysteria2、TUIC、WireGuard、GEOSITE 与 RULE-SET/provider 并向预览警告；sing-box 仅输出 Reality TCP Vision，不输出 Reality gRPC/xhttp 或客户端链（省略并警告）。mihomo 支持 Reality TCP（无 flow/Vision）、Reality gRPC、TLS/Reality xhttp（`auto`/`packet-up`/`stream-up`/`stream-one`，ALPN 允许空、`[h2]`、`[http/1.1]`、`[h2,http/1.1]`）与客户端链（链克隆保留 `chain-<方案>-<实例>`）。详见[协议矩阵](protocol-support-matrix.md)。
- 面板规则集引用地址优先取「节点安装连接地址」（第 27.14 节），未设置则按订阅请求拼出；响应包含流量、到期、更新间隔与下载文件名头。

**外部导入来源与刷新合同**：外部源仅 HTTPS，逐跳固定经校验公网 IP，限制 2 MiB / 15 秒，定时抓取加密缓存（订阅请求不联网抓取）；失败按固定类型（`input`/`dns`/`address`/`redirect`/`tls`/`connect`/`deadline`/`http`/`size`）返回固定代码与中文说明，不回显链接、Location、响应体或凭据。支持 SS2022、VLESS Reality TCP/gRPC、xhttp、WS Host、TUIC 参数与 Hysteria2 `mport`/混淆等公开字段，统一模型区分证书指纹 `TLS.Fingerprint` 与客户端指纹 `TLS.ClientFingerprint`。导入节点不参与用户额度或设备门控，可作为服务器方案最终出口（第 13.2、14 节）或纯转发规则目标。
- 刷新返回结构化结果：`outcome`（`ok`/`partial`/`failed`）、`counts`（`total=accepted+rejected+duplicates`、`accepted`、`rejected`、`duplicates`、`cached`、`usable`），逐条诊断最多 50 条且结果 ≤32 KiB（超界置 `diagnosticsTruncated`），不含名称、凭据或链接。完整/部分成功均 200 并提交缓存（`last_status=ok`）；失败保留旧缓存但 `last_status=failed`、`usable=0` 且排除出订阅；抓取取消返回 503 且保持原状。更换 URL 重置为 `pending`；同源 `name|protocol` 冲突按文档顺序保留首条（`duplicate_endpoint_key`），完全相同计 `duplicate_endpoint_content`。
- 单独导入支持改名（1–200 UTF-8 字节、无控制字符、不重名、`expectedRevision` 防并发，保持 ID 与显式引用不变）与预览删除（画布草稿/客户端发布快照引用阻止，分组/客户端策略精确引用确认移除且不得删空策略组，删空最后节点连同删空来源）；订阅连接节点不可单独改名或删除。

**客户端策略配置库**：`client_policy_profiles` 最多保存 50 个配置（名称 ≤64 字符、描述 ≤500 字符、`uiMetadata` ≤64 KiB JSON 对象不入订阅），`system_settings.client_policy_active` 存 `{profileId}`，`client_rules` 为活动快照（独立 `activeRevision`）。旧 `PUT /subscription-rules` 与浏览器旧卡片导入已撤除，创建配置保留 `importKey` 服务端幂等去重；`GET /api/v1/subscription-rules` 仍允许 Viewer。
- 内置预设（`GET /api/v1/subscription-rules/presets`）含经典分流、基础代理、分流+去广告、白名单与「精简/标准/完整分流 · 屏蔽回国」（`meta-lite`/`meta-standard`/`meta-full`，MetaCubeX mrs 规则集，国内域名与 IP 直接 REJECT）。单模板最多 256 个规则集，请求体上限 1 MiB。DNS 模板支持 `defaultNameservers`、`proxyServerNameservers`、`fakeIpFilter`、`fallbackFilter`（mihomo 输出）。
- 成员支持 `@all`、`@nodeGroup:<id>`、`@source:<id>` 及精确引用 `@endpoint:inbound:<id>`、`@endpoint:relay:<rule id>`、`@endpoint:import:<src>:id:<id>`（单独导入）或 `@endpoint:import:<src>:<name|protocol>`（订阅连接）；每组最多 256 个 `excludeEndpoints`（仅排除动态展开）。`includeKeywords`/`excludeKeywords` 服务端 NFKC 不区分大小写字面子串筛选（排除优先）；`emptyPolicy=direct/reject` 统一作用于授权与格式过滤后的空组（新地区组默认 REJECT）。
- `GET /api/v1/client-policy-candidates?userId=<id>` 与 `POST /api/v1/client-policy-profiles/preview`（Owner＋CSRF，草稿 `template` 与 `profileId+profileRevision` 互斥，版本过期 409 `stale_client_policy`）复用真实订阅展开管线；省略 `userId` 时按仓储顺序自动选取首个有授权的有效用户。
- 共享规则集引用通过事务扫描全部已保存配置（含未启用）：`GET /api/v1/service-sets/{id}/references` 返回 `nodePolicies` 与 `clientProfiles/rules`，任一引用阻止删除（409 `service_set_in_use`）；启用的 RULE-SET 不得引用缺失或禁用的面板规则集，禁用后 `/sub/{token}/rules/{id}` 返回 404。

# 11. 节点管理模块

## 11.1 节点列表

列表展示节点名称、在线状态、公网 IP、操作系统、CPU/内存/磁盘使用率、当前连接数、今日流量、Agent 版本、代理核心及版本、当前配置版本、最后心跳时间与本月整机流量（第 21 节）。

- **版本与更新页**：合并管理 Agent 与代理核心，展示各节点 Agent 版本、已启用核心版本/健康/协议及升级/编辑/添加入口；顶部提供「升级全部核心」与「升级全部 Agent」。「升级全部 Agent」直接按最新可用正式版下发任务，不再打开版本确认框；逐台升级仍可选版本，失败显示错误提示，执行结果在任务记录中查看。具体徽标配色、按钮样式、卡片排版与视觉细节统一遵循[设计系统](design-system.md)。
- **服务器视图与表单**：支持卡片/列表切换、按搜索词与服务器分组标签筛选，监听节点状态 SSE（5 秒节流静默刷新，可见时 30 秒刷新，切回前台立即刷新）。离线卡片去色并在延迟区域提示「已离线」，保留点击与操作。新增与编辑服务器共用表单，提供名称、地区、分组标签、备注、客户端连接地址、默认端口范围（`start-end`，1–65535）、续费信息（含自动续费）、整机流量设置（编辑弹窗按 GB 展示，保留原始精确字节，未修改不重写，读取失败阻止保存）与维护模式开关。
- **自动续费**：`node_meta.auto_renew`（`PUT /nodes/{id}/meta` 字段 `autoRenew`，省略保持原值）只在已填续费到期日且周期为 `/月`、`/季`、`/半年`、`/年`、`/2年`、`/3年` 时可开启，否则 400；表单仅在满足条件时显示勾选框，改为一次性或清空日期即关闭。面板每分钟按面板时区检查：到期日早于今天（当天不算过期）时按周期整月顺延，直到不早于今天；每次从原日期计算，月末日期夹紧到短月末日（01-31 → 02-28）。续费提醒先按原日期照常判断与发送（开启自动续费不影响提醒），随后顺延并写入审计 `system`「自动续费 旧日期 → 新日期」。
- **分组标签与排序**：服务器分组存于 `node_meta.tags_json`，每台最多 10 个标签（1–20 字符、大小写不敏感去重、禁逗号与控制字符），与订阅节点组独立。Owner 拖动卡片调整全局展示顺序，存于 `system_settings.server_order`，「版本与更新」复用同一顺序。标签与顺序不影响授权、订阅或发布。
- **离线地区识别**：面板内嵌离线 IP→国家表（`internal/panel/geoip`，DB-IP Lite CC BY 4.0，不向外部发请求），按 `publicIp` 推导 `countryCode`（不入库）。显示优先级：管理员指定地区 ＞ `countryCode` ＞ 名称/主机名关键词 ＞ 🌐。
- **双地址模型**：
  - `publicIp`（节点间连接与公网标识）：Agent 心跳字段 36（`public_addresses`，最多 16 个全局单播地址）写入 `public_addresses_json` 供候选；来源 `publicIpSource` 为 `manual`（手填）、`selected`（从候选选定 `publicIpSelected`，若候选消失标记 `publicIpSelectionMissing=true`）或 `auto`（默认路由 IPv4 ＞ 首个公网 IPv6 ＞ 控制连接对端公网 IP）。
  - `connectHost`（可选客户端连接域名或 IP，无协议/端口/方括号）：客户端连接地址优先取 `connectHost`，否则 `publicIp`；订阅、预览、分享链接与隧道规则入口地址均按此顺序，节点间中转与隧道仍用 `publicIp`。两者皆空时不进订阅且预览给出警告。

## 11.2 节点详情

展示基础信息、资源、能力、入站/出站、节点组、当前连接、今日代理流量、最近域名、当前/上一成功配置、发布记录、事件、出站网络与本月整机流量。

- **多核心能力与健康**：`capabilities.cores` 列出启用核心的版本、协议、用户统计、转发与门控能力；`heartbeat.cores` 列出逐核健康/错误/启动时间/统计源状态；`core_selectionBlocked` 列出添加/移除中不可选组件。缺测保留 `null`，不编造 0 或健康；不接受未上报启用核心。
- **主机观测与资源曲线**：`Capabilities.host` 上报 CPU、架构、内核、系统、内存/交换/状态目录磁盘总量、虚拟化类型（`virtualization`）、19 个调优键可写状态（`tuning_keys`）、拥塞/排队可用及可加载算法（`congestion_available/loadable`、`qdisc_available/loadable`）、进程句柄上限（`fd_limits`，systemd 与 OpenRC 对齐 524288）及 `/etc/sysctl.d/` 配置源（`sysctl_sources`）。心跳提供负载、进程/连接/套接字数、内存/交换/磁盘占用、`tuningCurrent` 及 `tcp_out_segs`/`tcp_retrans_segs`（计算分钟 `tcpRetransPercent`）。资源曲线每节点每 UTC 分钟保存最后一条新鲜心跳（时间差 ≤45 秒），保留 7 天。
- **逐入站发布状态**：`GET /api/v1/nodes/{id}/inbound-states` 比较草稿入站与节点当前运行修订（忽略修订号、用户凭据与 WireGuard 对端），返回 `effective`/`modified`/`pending`/`unpublished`（失败带 `code` 与 `revision`），只读不写库。

## 11.3 节点注册与删除

Owner 创建节点并签发一次性注册码 → 节点运行 `deploy/linux/node-install.sh` 下载同版本发布包、校验 `SHA256SUMS`、建立面板证书信任 → Agent 本地生成私钥并提交 CSR → 面板签证并核销注册码 → 建立 mTLS 长连接（私钥永不上传）。安装命令默认填充「节点安装连接地址」（第 27.14 节），对话框「启用网页升级」默认勾选，命令带 `--web-upgrade`（脚本本身默认不启用）。

- **原位身份恢复**：主机重装但保留节点身份时，Owner 签发绑定当前指纹的一次性恢复码（300–3600 秒，默认 900 秒，可选立即吊销旧证书；存在未完成任务时返回 409 `node_recovery_blocked` 与 `blockers`）。`node-install.sh --replace-agent` 在同机校验发行包、确认旧服务退出与最终补传后才清理固定数据目录并重新登记。新 Agent 经 `Enroll` 核销恢复码后保留节点 ID 与全部业务配置，旧指纹永久写入 `node_identity_retirements`（备份恢复亦不复活），并在首次能力上报后自动下发当前已发布配置。操作见[运维手册](operations.md#51-原位恢复推荐)。
- **删除与连带清理**：空白待登记节点取消登记时直接删除；已登记节点逻辑删除（`deleted_at`）、吊销证书、移出分组并关闭本节点待发布/更新/健康检查任务，保留历史流量与访问记录且不可重新启用（不停止远端进程）。删除连带移除该节点全部入站及涉及它的方案、隧道与规则；外部手工出站、出口组、代理链、策略或未收尾发布批次以 409 `node_delete_blocked` 返回 `error.blockers`。预览接口 `GET /api/v1/nodes/{id}/deletion-preview` 列出待删对象与阻塞项；删除成功返回 200 `{pendingNodes:[{nodeId,name,reason}]}`，详见[运维手册](operations.md)。

## 11.4 节点状态

节点状态统一为：`pending`、`online`、`degraded`、`offline`、`applying`、`rollback`、`disabled`、`error`。带 `core_optional_supported=true` 且启用列表没有 Xray/sing-box 的节点称为无用户内核节点；允许 `core_name` 为空、`cores` 为空或仅含 GOST，非空主核心须为已启用的用户内核。没有任何内核时心跳省略 `core_healthy`，照常 online，不因缺少主核心而降级；GOST 仍按实测健康显示。旧 Agent 不带能力位时，空主核心沿用未知上报规则。节点组服务器成员照常可选；订阅明确排除无用户内核节点。

# 12. 入站代理模块

无用户内核节点只允许显式 `core=gost` 的 `forward` 入站，且须已启用 GOST；其他入站在保存、画布规划与发布时返回 400 `no_user_core`。空 `core` 不会自动选 GOST。入站定义用户接入节点或节点间中转的监听协议。单节点支持多入站；内部中转入站须设为订阅隐藏（`subscriptionVisible=false`），隐藏的用户入站不分配用户凭据并渲染为停用。

主要字段：名称、所属节点、所属核心（`core`，留空随主核心，填写值须为已上报启用核心且未处于添加/移除 `core_pending`）、协议、监听地址/端口、传输方式、安全方式、允许用户、是否订阅可见、是否启用、默认出站。

**支持协议与传输边界**：
- 支持 HTTP CONNECT、SOCKS5、Shadowsocks（空 `method` 默认 `chacha20-ietf-poly1305`）、VLESS、Trojan、WireGuard（仅 Xray）、Hysteria2/TUIC（仅 sing-box 需 QUIC 能力）与端口转发（`forward`）；不支持 VMess 与系统 TUN。完整范围见[协议矩阵](protocol-support-matrix.md)。
- 传输限制（`desiredstate.ValidateInboundTransport`，在保存、`SnapshotIntent` 与发布前三处生效）：WebSocket、gRPC 仅限 VLESS/Trojan；xhttp 仅限 VLESS；HTTP/SOCKS5/Shadowsocks 仅允许 TCP。历史修订与备份仍可读旧组合。
- **VLESS Reality 与 xhttp**：服务端设置使用类型化 `reality{private_key,public_key,handshake_target,server_names[],short_ids[]}` 与 `xhttp{host,path,mode}`，拒绝未知字段与 `extra`/`xmux`。Xray 支持 Reality TCP（无 flow/Vision）、Reality gRPC（无 flow）与 Reality/TLS xhttp（`auto`/`packet-up`/`stream-up`/`stream-one`）；sing-box 支持 Reality TCP（无 flow/Vision，仅单个具体 DNS SNI）。Reality 私钥仅存加密设置与修订，普通读取/差异/预览隐藏，普通编辑不轮换；X25519 密钥对须无填充 base64url 32 字节且公私钥匹配；Short ID 为 1–16 字符偶数十六进制；`server_names` 为 1–16 个具体主机名/IP（不接受通配符）；Vision 仅限 TCP + TLS/Reality。编辑器支持一键填入系统「Reality 预设」（第 27.19 节）。
- **逐核设置契约门禁**：`desiredstate.CoreSettingsCapability{core,settings_contract_version,combinations[]}` 按（协议、方向、安全、传输、flow）逐项声明。版本 0、缺失、未知版本（接受上报但永不授权）或未列组合在保存、快照、发布与投递/回滚前一致拒绝；携带新设置的命令须本次连接已接收新序号能力报告，支持撤回时不破坏健康控制流。

**TLS 证书来源**（`inbounds.tls_source`，面板元数据不进 `desiredstate`）：
- `paste`：粘贴 PEM 材料（历史空值有证书时按 `paste` 读取）。
- `self_signed`：面板自签（内部中转 1 年，用户入站 10 年，Subject/SAN 取节点 `ConnectAddress()`，无地址返回 400）；编辑保留已有证书，仅 `regenerateCertificate: true` 或切换来源时重签。
- `file`：节点本机路径（`certificate_path`/`private_key_path` 须同时填写、绝对规范、≤256 字节、位于 `/etc/proxy-agent/certs/` 下、不含 `..`，出站禁用）。Agent 渲染时解析符号链接确认不越界、加载密钥对并检查未过期（宽于 0640 记 WARN），受 `tls_certificate_files_supported` 门禁。
- `acme`：引用证书库 `certificate_id`（第 27.16 节），入站不存 PEM 副本，快照与预览实时读取证书库；必须引用已签发证书，`server_name` 为空时取首个非通配域名，无 `TrustedCAPEM`，按公网可信证书导出。
- 编辑器支持按域名调用 `GET /api/v1/nodes/{id}/certificate-matches?domain=` 自动排序匹配节点证书文件、证书库证书或直接发起 ACME 申请；读取接口提供只读 `certificateInfo`（含覆盖提示，`file` 来源为 `null`）。

**Hysteria2 端口跳跃**：仅 sing-box Hysteria2 入站支持 `settings.quic.{hop_port_start,hop_port_end,hop_interval_seconds}`（1–65535、`end > start`、≤10000 端口、不含监听端口，间隔 5–300 秒、0 默认 30）。受 `Capabilities.port_hop_supported` 三处门禁（`port_hop_unsupported`）；由 host-network helper 维护 `inet proxy_agent_hop` nft 规则，端口冲突校验包含监听端口与跳跃段。

**GOST 端口转发扩展（契约 ≥2）**：`forward` 在 GOST 核心上支持 `targets[]`（1–32 个有序 `{host,port}`，与旧 `host`/`port` 互斥）、`strategy`（`round`/`fifo`）、`max_fails`（1–100）、`fail_timeout`（1–3600 秒）、`port_end`（最多 256 端口）、`bandwidth_in_bps`/`bandwidth_out_bps`（0 或 1024–10¹² B/s）与 `max_connections`（0–100000，仅 TCP）。这些字段在非 GOST 核心上三处一致拒绝；渲染按（端口、传输）展开为 `fwd-<入站ID>-<端口>-<tcp|udp>`，单节点上限 1024 个生产服务。

**端口唯一性与资源归属**：
- 同一节点跨核心按数字端口检查（不区分 TCP/UDP，含转发范围与跳跃段）：手动保存检查启用入站冲突与 `port_claims` 持久声明；自动分配与 `ports/check` 计入禁用入站；Agent 校验为最终门禁。
- 隧道/规则生成的入站返回 `managedBy:{kind,id,name}`，普通 PUT/DELETE 返回 409 `managed_by_tunnel`。画布创建的入站由 `canvas_tunnel_objects` 归属，禁止在入站编辑器改名；已绑定入口（`canvas_entry_bindings`）不得作为画布 inbound 实例或 `linked` 规则目标（400 `inbound_is_scheme_entry`）。
- 删除入站连带删除引用它的方案、入口方案、`linked` 规则及失去全部规则的隧道；存在手工中转出站、代理链或未收尾发布批次时返回 409 `inbound_delete_blocked` 与 `error.blockers`，成功返回 200 `{pendingNodes}`。

# 13. 出站代理模块

无用户内核节点禁止保存本节点出站（含 Direct/Block 资源）、出口组、代理链跳，以及显式归属该节点的分流与网站控制策略，返回 400 `no_user_core`。中转线路、协议转换中转、画布方案与代理链不能选择这类节点作为中转或出口；GOST 隧道照常。全局/节点组策略保留共享定义，在无用户内核节点上不渲染。出站定义节点收到流量后的下一跳方式。支持 Direct、Block、HTTP、SOCKS5、Shadowsocks、VLESS、Trojan、WireGuard、远程节点、出口组与 Proxy Chain。主要字段含名称、所属节点、类型、目标节点/地址/端口、协议、凭据、TCP/UDP 支持、启用、健康状态与失败策略（Block/Direct/备用出站/备用出口组/备用代理链；固定出口建议 Block 或备用出口，不隐式 Direct）。

## 13.1 节点出站网络

**入站出口 IP**：高级选项 `egressSourceIp`（期望状态 `egress_source_ip,omitempty`）为空时沿用原行为；非空时规范化为本地单播地址，覆盖服务器和出站绑定、清除网卡绑定。仅支持明确上报 `inbound_egress_source_supported` 的 Xray/sing-box，GOST 返回 `inbound_egress_unsupported_core`，旧 Agent 返回 `inbound_egress_unsupported`。所有适用分流目标和默认目标使用按地址排序编号的 `<origTag>~egress~<k>` 克隆；Block 保持阻断，入站规则先于通用规则。访问、统计归属原 ID，面板仍只接收默认来源的原出站健康。各出口 IP 在组成员筛选前分别建立探测和选路上下文，保留原有主备、延迟、轮询与失败策略；缓存和探测指纹区分源地址、目标、有效绑定及关联凭据，不能复用其他源的健康结果。未知健康沿用组的失败策略，但完整原始成员的探测入口始终保留以便恢复。原生动态组继续从该出口 IP 探测成员（Xray observatory / sing-box `urltest`）。域名只解析指定地址族；另一族 IP 字面量和回环目标连接失败。发布后生效；地址不在本机时返回 `network_unavailable` 并保留旧配置。整机强制出站的额外豁免也包含该地址，仍需 helper 能力，每族最多 16 个。

节点设置可配置出站网络，下一次发布配置生效，留空清除：
- **核心出站源 IP / 网卡**：作用于代理核心拨号出站（Xray `sendThrough`/`sockopt.interface`，sing-box `inet4/inet6_bind_address`/`bind_interface`；出口组成员继承，sing-box 组节点与 WireGuard 入站不写）；指定源 IP 时固定地址族。
- **服务器强制出站 IPv4 / IPv6**（仅 Linux）：通过 host-network helper 维护 nft 表 `inet proxy_agent_source` 对整机新建出站做 SNAT（核心出站源 IP 自动豁免），失败自动回滚。详见[运维手册](operations.md)。
- **逐出站源绑定**：非 Block 出站可单独设置 `sourceIp`/`sourceInterface`，优先级「出站非空 ＞ 服务器级核心设置 ＞ 不绑定」，需全部启用 Xray/sing-box 核心上报 `outbound_source_supported`（否则拒绝 `outbound_source_unsupported`）。与服务器强制出站并用时，启用出站的额外源 IP 自动汇总为 `ExtraHostExemptions` 传入 helper（需 `host_source_extra_exempt_supported`，否则 `host_source_exempt_unsupported`）。Agent 应用前校验可达出站的有效源 IP/网卡在本机存在且匹配，失败返回 `network_unavailable` 并保留旧配置。

## 13.2 出站客户端字段与导入节点转换（v1.6.0）

出站支持客户端专用字段：`alpn`（VLESS/Trojan TLS/Reality，仅 `h2`/`http/1.1`，≤3 项）、`client_fingerprint`（uTLS）、`ws_host`（仅 ws）、`reality_client{public_key,short_id,spider_x}`（仅 VLESS Reality，指纹必填）与 `xhttp{host,path,mode}`（仅 TLS/Reality）。入站带客户端字段或出站带服务端私有 `reality` 均拒绝。携带上述字段的出站（含禁用）在保存、快照与发布前均要求启用 Xray/sing-box 核心上报对应 `direction=outbound` 设置组合（缺能力返回 `outbound_client_unsupported`）；经典组合无需上报。Xray 探测上报 13 个组合，sing-box 上报 10 个（无 xhttp、Reality 仅 TCP、不输出 SpiderX）。

导入节点转换（`proxy.ImportOutbound`）仅做无损映射：SOCKS5、HTTP、Shadowsocks（四种 AEAD）、VLESS、Trojan、Hysteria2、TUIC，保留 SNI、固定 CA、ALPN（仅 `h2`/`http/1.1`）、客户端指纹、Reality（缺指纹默认 `chrome`）与 ws/grpc/xhttp（Hysteria2 丢弃跳跃端口连主端口）。SS2022、证书指纹固定、跳过证书校验、Hysteria2 混淆、WireGuard 及其他传输拒绝转换并返回固定原因码与中文说明。

# 14. 协议转换中转

中转分为**隧道管理**（转发字节，不终止用户协议）、**节点「添加中转」**（快速入口）与**代理中转画布**（终止协议 A 并按策略以协议 B 建立下一跳，用户与节点间凭据隔离）。

**隧道与规则**：
- 隧道拓扑为 `entry → [mid] → exit`（最多 3 台不同服务器、2 段中继），仅使用 GOST `relay` over TLS+mTLS；两节点各跳契约 ≥3、三节点 ≥4，接收跳使用公网地址或 `connectHost`，每接收跳共享监听，各段独立密封 CA/叶子证书（私钥不进 API/计划/日志）。
- 规则定义入口监听、`network=tcp/udp/both`、可选隧道（无隧道为直接转发，优先 GOST，否则主 Xray/sing-box）与目标：
  - `linked`：关联自建用户入站，目标地址/端口动态派生（出口即目标服务器时用 `127.0.0.1`）；目标地址或端口变化时在同事务递增草稿版本并将已发布规则标 `needs_publish`，旧快照保持不变。
  - `manual`：固定 `host`/`port`，不进订阅。
  - `import`：指向导入节点 `importId`（`network` 默认 `both`，QUIC 走主端口）；订阅连接刷新按 `name|protocol` 改绑新 ID，地址/端口变化标 `needs_publish`（仅换凭据免重发）；导入节点删除时外键置空、标 `needs_publish`，计划/发布返回 409 `import_target_deleted`。
- 隧道页与节点「添加中转」保存、修改、删除与轮换凭据后立即执行计划与发布（API 仍为先草稿再发布），失败保留草稿供重试。对象状态分 `draft`/`modified`/`needs_publish`/`published`/`pending_delete`；已发布删除保留行、端口声明与快照直至成功移除，隧道有规则引用返回 `tunnel_in_use`。
- 发布前若节点未装 GOST 但具备安装能力，发布 API 返回 409 `gost_install_required`，前端自动逐台安装固定 GOST 3.3.2（每台最多等 5 分钟，无确认修订先发基础配置）后再发布；发布后对不再被引用但已装 GOST 的节点通过 `gostRemovable` 提示可手动移除。
- 生成对象按 `tunnel_objects` 归属，`port_claims` 持久预留节点级端口；隧道与规则须手填端口或从 `node_meta.port_range_start/end` 分配（无默认端口回退）。

**代理中转画布与快速创建**：
- 画布仅创建代理中转（`proxy_chain`/`tunnel`，协议 `tls` 或 `wireguard`）；出口可为 qui 用户入站（生成独立内部监听与凭据）、前一跳上的外部出站或可转换的导入节点（仅作最后一跳，发布时在上一跳生成归隧道所有的出站，普通编辑器修改/删除返回 409 `managed_by_tunnel`，候选见 `GET /api/v1/canvas-import-exits`）。画布无端口范围时保留 TLS `8443` / WireGuard `51820` 回退。
- 旧 `port_forward`/GOST 画布连接以只读保留（可删除、不可新增或修改）。
- 三步快速创建（`POST /relay-schemes/quick/plan`、`/relay-schemes/quick`）最多一个中间服务器，内部通道统一为 Trojan over TLS/TCP（1 年期自签证书与独立中转凭据），`requestId` 经绑定表防重。
- 入口绑定 `entryBinding.mode` 仅接受 `create`（新建独立入口）。VLESS 新入口可选 Reality（支持系统预设）、自签 TLS 或不加密，及 TCP/TCP Vision/xhttp；发布后安全设置在方案内只读，须在节点列表编辑入站（方案重发不覆盖安全设置）。
- **撤除与后台待清理**：`POST /api/v1/canvas-schemes/{id}/remove`（与 `DELETE /api/v1/canvas-schemes/{id}` 同义，体须带 `updatedAt`）在事务中立即删除方案、入口、专用内部监听、中转出站、规则及无引用通道，返回 `{schemeId,removed:true,pendingNodes}`。受影响节点写入 `relay_cleanup_nodes`，仅以已发布状态减去已删对象生成 `auto: relay cleanup` 修订（不夹带草稿）：在线立即下发，离线重连下发，失败按 1/5/30 分钟及之后每 30 分钟重试，启动与每分钟扫描；逻辑删除节点清除自身待清理行。
- **展示与状态**：节点列表隐藏内部监听与 `managedBy.kind=tunnel` 监听，方案入口与 `linked` 规则收进出口节点「中转线路」；维护或离线服务器上的自建节点不展示为独立条目。`GET /canvas-schemes/{id}/status` 返回六项状态（`draft`/`batch`/`nodes`/`entries`/`subscription`/`probe`），其中 TCP 探测不代表代理认证或全路径连通；只读 `routeSummary` 来自草稿，异常标 `pathIncomplete`。

# 15. 出口组模块

出口组用于节点内部统一管理多个出口，与用户订阅节点组完全独立。

| 策略 | Xray | sing-box |
| --- | --- | --- |
| 主备（`primary`） | Agent 本地选择当前成员并仅渲染所选成员 | 同左 |
| 简单轮询（`round_robin`） | `routing.balancers` (`roundRobin`) + `observatory` + `fallbackTag` | `urltest` |
| 延迟优先（`latency`） | Agent 本地选择（leastPing 语义） | `urltest` |
| 权重（`weighted`） | 已移除 | 已移除 |

权重策略已从校验、API 与页面移除，读取历史配置修订中的 `weighted` 仍兼容解释为 `round_robin`。固定出口失败不隐式 Direct（第 13 节）。

# 16. 代理链模块

代理链支持 `A→B` 与 `A→B→C`（最多三跳，各跳协议可不同，不支持无限多跳），字段含名称、入口/中转/出口节点、各跳协议与出站、最终出口、失败策略与健康状态。保存与发布前检查重复节点、环路、最大跳数、协议与 TCP/UDP 能力匹配、端口冲突、内部入站、中转凭据与最终出口。

- **发布与回滚顺序**：按「出口 → 中转 → 入口」发布，失败按「入口 → 中转 → 出口」反向回滚。
- **隧道与画布发布**：隧道计划只读，发布重算依赖并交给部署批次，全成功才推进版本与订阅快照，未变返回 `unchanged`；轮换凭据仅改草稿，发布切换期间可短暂中断。画布按 `canvas_tunnels` 共享对象，冲突声明拒绝，在用隧道删除返回 409 `tunnel_in_use`；客户端方案自包含、拒绝 `tunnelId`，输出 mihomo `dialer-proxy` 单路径。
- **可达依赖校验与裁剪**：发布依赖仅检查从启用非 GOST 入站可达的中转出站（无凭据停用入口不作起点），可达但目标监听不匹配返回 409 `dependency_not_ready`（含出站 ID、目标节点 ID 与原因码，不含凭据）。画布发布先整体校验，跳过仅为依赖且运行配置与启用意图未变的服务器（忽略修订号、按用户集合比较，历史 NULL 启用意图不等于空集）；显式中继批次仍发布全体。

# 17. 分流模块

统一处理网站分流、阻断、Direct、指定 Outbound、出口组与代理链。支持按用户、入口节点/节点组、域名、域名后缀、域名关键字、服务分类、IP/CIDR、端口、TCP/UDP 与时间段匹配；规则按优先级从高到低命中首条停止，每节点必有默认规则。

- **展开上限**：单条策略手写条目 ≤2000 条；引用规则集展开后单条 ≤50000 条（超过 2000 条需逐核上报 `expanded_policy_supported`，否则快照与发布前拒绝）；整份期望状态上限 2 MiB。内置默认禁用的 `policy-block-china`（匹配 `service-cn` → Block）为普通策略，可编辑或删除。
- **节点原生规则集**：规则集外部来源支持 `native` 格式，同时提供 sing-box `.srs` 与 Xray geosite/geoip 地址及标签（白名单限于 `testingcf.jsdelivr.net`/`cdn.jsdelivr.net`/`fastly.jsdelivr.net` 上的 MetaCubeX `meta-rules-dat`，面板与 Agent 双检）。节点所有启用 Xray/sing-box 核心均上报 `native_rule_sets_supported` 时，在期望状态 `match.rule_sets` 下发引用并跳过 yaml/text 展开（不计条数上限）；否则维持展开。Agent 在独立准备任务中下载候选文件至 `runtime/<core>/rulesets/`（单文件 ≤32 MiB、3 分钟、下载期间不阻塞心跳），失败沿用旧文件（首次缺失报 `rule_set_unavailable` 回滚），替换保留 `.prev` 以便校验或健康失败恢复，成功清理无引用旧文件。Xray 开启访问记录时仅对 WireGuard 与 HTTP/SOCKS5 按凭据复制策略规则，VLESS/Trojan/Shadowsocks 由访问日志 email 归属账户。

# 18. 服务规则模块

将常见网站域名集合封装为内置或自定义服务分类（如 Netflix、YouTube、OpenAI、Google、GitHub、Telegram、广告拦截、国内、私有网络等 25 个内置集合，挂接 MetaCubeX `meta-rules-dat` yaml 来源，内置 `service-cn` 另含 geosite/geoip `cn` 原生来源）。面板仅定时刷新被至少一条已启用节点策略引用的启用集合（`autoRefresh`），手动刷新不受限；客户端规则引用不触发面板抓取。服务规则变更须经配置版本发布生效。

# 19. 网站访问控制

与分流共用同一策略引擎，支持按用户、节点、节点组与时间段控制精确域名、后缀、关键字（`domain_keyword`/`keyword`）、通配域名、自定义分类、内置服务与 IP/CIDR 的阻断或定向出口。

# 20. 网站访问记录

页面、筛选与策略作用域均不暴露设备维度，仅按用户查询，不提供终端用户访问记录入口（数据库保留内部隐藏身份用于统计与墓碑）。

记录域名级元数据：时间、用户、（内部设备 ID）、入口节点、入站协议、访问域名、目标 IP/端口、命中规则、执行动作、出站协议、代理链、最终出口节点、允许/阻断、连接数、上传/下载流量。

**口径边界**：
- **域名字节可空**：上游核心无法提供每域名精确字节时存 `NULL`、页面显示「—」，不写 0；用户与节点总量取自核心统计。
- **配置出口**：最终出口节点为本节点策略解析出的配置出口，不证明跨节点中转的实际出网节点，页面明确区分「本节点直连」与「配置出口未确认」。
- **连接去重**：跨分钟存活连接不重复计为新连接；阻断无法识别域名时记录目标 IP，不猜测域名。

# 21. 访问记录轻量化处理

不逐连接写中央库，按时间桶、用户、节点、域名、动作与出口聚合。

| 项目 | 默认值 / 规则 |
| --- | --- |
| 允许 / 阻断记录 | 30 / 90 天 |
| 小时 / 日流量 | 小时 90 天；日长期保留 |
| 本地 spool | 100 MiB（`DefaultLimit`），容量 pragma 对每条 SQLite 连接生效 |
| 采样 / 上报 | 每 5 秒采样写 spool；心跳间隔 15 秒批量上传 |
| Xray / sing-box 访问行 | Xray tail `runtime/xray/access.log`；sing-box 从核心日志提取访问行（Linux 为文件 tail）。Linux 两者约 100 ms 轮询，只交出完整行，半行留在文件直到补全，追平后清空；20 MiB 隐私截断记录结构化丢弃事实。Windows 保留原读取行为 |

Linux Xray、sing-box、GOST 已接入更新时的核心交接：认领沿用原 epoch/Revision 和累计计数，恢复已见计数源集合，来源消失时报记账失败；Xray/sing-box 共享文件 tail 从原 inode/完整行 checkpoint 接读，错误阻止交接就绪。Prepare 经原 TrafficSink 持久化最终采样并保存 checkpoint，Xray 还须证明静态路由 gate-free；Detach 仅释放句柄与 tail，Close 仍停止核心。认领 attach 期间保留文件，20 MiB 隐私上限仍生效并记录丢弃；认领成功后恢复追平清理。更新空窗内允许访问日志在文件积压，访问记录允许少量重复或缺失，不承诺恰好一次。sing-box 来源映射不持久化，交接前已建立连接的来源标记为 unavailable，不阻塞交接。“不漏计”仅承诺正常存活交接；核心在空窗内死亡后无法取得的尾部流量只记本机结构化 WARN `core_traffic_tail_unavailable`，不估算、不补零，不新增面板告警。验收范围见[交接设计 §9](archive/core-handover-plan.md#9-实施与验收结果2026-11-22)。

断线缓存、重连补传、Ack 后删；满时先淘汰最旧允许记录、保留阻断与流量。同一流量或访问批次连续被面板拒绝 3 次则隔离且不淘汰（1 小时后或重启重试），面板记限频 WARN「agent batch rejected」（不含用户名或域名）。删除用户前产生的批次按修订与凭据墓碑归属，仅累加仍存在用户。

**三类流量口径**：
1. **代理账户与核心/节点总量**：Hello/Welcome 协商 `node_traffic_supported`，Agent 上报每核心累计 `traffic.Counter{kind:"node"}`（校验核心名、`credential_id=node`、`.node` epoch、已确认修订、非负 ≤1 PiB），按（节点、epoch、`node`）存游标写 `core_traffic_samples`。`node_traffic_samples` 仅在同 epoch 无 node 游标时回退账户样本去重。
   - Xray 总量 = 账户计数 + 已渲染 `forward` 入站白名单计数；sing-box 取核心总量；GOST 从 `runtime/gost/metrics.sock` 仅求和 `fwd-<入站ID>-<端口>-<tcp|udp>` 字节（异常整次采样标统计源失败不补零），**`relay-*` 服务不计节点总量**。无身份转发只计节点总量、不扣用户额度，停核前保留最后采样。
   - 代理月流量按面板时区半开月区间合并 `node_traffic_samples` 与核心小时历史（保留 `scope=retained_account_samples`），月边界切入已合并 UTC 小时返回 422 `traffic_history_resolution`；历史清理过的账户回退样本不能补回（L9），禁止拼接累计心跳冒充完整月。展示查询加速使用可重建索引 `traffic_sample_hours`（迁移 0155），未回填完整时回退精确查询。
2. **流量存储整理（v1.7.0 起，迁移 0154）**：核心明细保留至少 7 天，完整过期小时按批 500 行在同事务合并入 `core_traffic_hourly` 并删除明细；迟到增量下轮补入。Hello/Welcome 协商 `traffic_retirement_supported`，Agent bound spool v2 认证固定 `queue_id`、连续编号、队列原文与删除进度（控制消息序号不作交付编号）。面板记录连续完成 `C`、节点持久删除确认 `A`、已淘汰 `R`（`R ≤ A ≤ C`），冻结候选至少 24 小时后每批 ≤500 条淘汰，缺口或 pending 阻止跨越；旧无编号回执保留且不按时间淘汰，切换后拒绝旧协议。仅显式身份恢复核销可在同事务封存旧队列并授权新队列；`seq ≤ R` 返回 `traffic_delivery_retired`，身份或进度冲突返回 `traffic_delivery_conflict`。整理随现有生命周期停止/恢复，不自动 VACUUM。

   **v1.10.2 监控整合调度**：监控整合每 5 秒调度，单轮预算 2 秒（含数据库连接排队），每种监控最多处理 8 个五分钟桶。过期清理分表提交，每个桶的汇总与原样本删除单独事务提交，桶间释放唯一写连接；后续失败保留前面已提交进度并返回原始错误。选桶使用现有时间索引读取最早样本，继续保留 24 小时原始、7 天五分钟监控及加权字段、计数器前驱规则。节点离线判定时限不变。

   **v1.10.2 Agent 旧状态换代顺序**：安装器发布完成后异步排队独立重置服务，服务按 `update.lock` → `operation.lock` 顺序同时持锁；其启动不阻挡 Agent 或网络/调优 helper，OpenRC 启动异步返回，避免升级持锁等待 helper、helper 又等待重置。发现尚未收尾的更新或核心交接 journal 时明确失败并保留数据，等待恢复结束。停旧 Agent 和 helper 后才封存身份及删除重建；停服或封存失败不删除。重建后恢复服务账号权限与目录监听，再启动新 Agent 并确认；OpenRC 新进程不继承重置锁。仅有旧请求、尚未准备 journal 时使用当前二进制摘要准备；`prepared`/`sealed`/`rebuilt` 继续使用原 reset ID 和身份绑定，`rebuilt` 重试不再封存或删除，`complete` 后后续升级不重复清理。
3. **整机月流量**：独立于代理统计、用户额度与订阅。心跳上报非 lo 网卡累计字节、`bootId` 与 IPv4 默认路由网卡，面板同事务推进游标并按（服务器、网卡、面板时区本地日）写入 `node_machine_traffic_daily`（跨日按真实时长比例分摊且末份补足整数，首次/重启/回退仅建基线，改时区不重分桶）。单张网卡计算（显式 `interfaceName` 否则最近新鲜心跳默认路由网卡，不跨网卡拼接），重置日 1–31 短月夹月末，支持 `sum`/`outbound`/`max`；额度仅展示，不限速不停服。

# 22. 配置发布与回滚

草稿不直接覆盖运行配置：保存业务配置 → 生成 Revision → 校验用户/节点/能力 → DesiredState → Agent 编译 → 原生校验 → 切换核心 → 健康检查 → 成功或自动回滚。已终结目标收到迟到结果确认忽略并记 WARN；本节点曾下发过的修订其流量、访问与设备观测照常归属。心跳回报成功修订等于待确认修订且核心健康时按成功确认（INFO `deployment confirmed by heartbeat`），失败与回滚码仍仅来自应用结果。

单条 Revision 覆盖全部核心（`core.json.enc` 按核心名存储），活跃核心先全部校验再依序切换、失败逆序补偿，无启用入站的核心停止。**发布重启目标节点全部核心**（中断既有连接），崩溃恢复仅影响异常核心。启动时保留修订不可运行则核心停止但 Agent 保持在线待新版本。默认保留最近 3–5 个成功版本。续期证书自动发布仅在 Owner 设定的低峰窗口内执行（第 27.16 节）。

Linux Agent 更新仅有效本机更新授权允许保留核心：旧 Agent 最终采样写原 spool，写 prepared 后脱离并写 detached；新 Agent 在 lifecycle 独占锁内检查固定三核目录（含禁用核），先恢复全节点私有端口与渲染输入，再用当前版本渲染、原生校验，比较配置字节、依赖、环境、核心镜像摘要及账户归属。全部一致才沿用原 run、epoch、Revision 认领；首采持久化后写 adopted，首次 Refresh 不重启。真实配置变化先建立旧配置管理及采样上下文，再按全核发布重启。材料损坏、身份不明或部分认领失败保留错误与现场，不以补偿重启健康同伴，控制连接保持在线。

交接同时覆盖引导程序与受管 worker 更新。Linux 须支持并允许使用 `pidfd_open`/`pidfd_send_signal`；不可用时不交接，明确提示后按停核更新，不用裸 PID 替代。源或目标不支持交接协议时提示“本次更新会中断连接”；第一次升级到本版本由旧 Agent 执行，仍会断线。普通 stop/restart、卸载和非更新崩溃仍停核，服务 post-stop 屏障须确认遗留核心退出；失败回滚先撤销交接授权，再确认停核，屏障失败保留现场。

## 22.0 服务器调优与推荐应用流程

调优白名单固定为 19 个 sysctl 键（`pkg/contracts/tuning`），禁止任意 shell 或 `modprobe`。root helper 直接写 procfs 并原子更新 `/etc/sysctl.d/99-proxy-tuning.conf`，首次应用前在 `/etc/sysctl.d/.proxy-tuning-snapshot.json` 保存基线，失败回滚、清空恢复基线，回执 `initial` 返回完整 19 键初始快照。

- **按需补装模块（迁移 0156）**：Owner 可为单次 BBR/fq 请求勾选 `installMissingModules: true`（仅上报 `host.tuning_module_install_supported` 且目标键可写的 Ubuntu/systemd 节点接受，自动推荐默认 `false`），支持范围与排错见[运维手册](operations.md#18-主机观测续证与调优-helper)。
- **规则建议与自动应用**：规则引擎（`internal/panel/node/tuningadvice`，`RulesVersion = "2026.11-1"`）综合虚拟化、可用/可加载算法、内存、线路带宽（`node_meta.bandwidth_mbps`）、24 小时 RTT 中位数、QUIC 入站与中继角色给出 `suggested`/`optional` 建议。支持 `GET /nodes/{id}/tuning/recommendations`、`POST .../dismiss`（键推荐值变化时重新展示）及 `GET/PUT /system/tuning-auto-apply`（新节点首次上报能力且无调优记录时以 `auto` 下发）。
- **下发与状态机**：Owner＋CSRF 提交完整 `settings`/`clear` 与匹配 `confirmNodeId` → 同事务存 `node_tuning` 唯一 `request_id` 与 `pending` → 下发 `TuningCommand`（并发 409 `tuning_busy`、缺能力 409 `tuning_unsupported`、离线 409 `node_offline`）。`TuningResult` 幂等落库后 Ack，冲突拒绝；断流或重连将 `pending` 置 `unconfirmed`，不自动重发。

## 22.1 自动额度版本

流量入库或定时 tick 判定用户状态（禁用 ＞ 超限 ＞ 到期），变化按节点排队并在 60 秒内合并，以**已发布状态**为基线仅替换用户访问面，生成 `auto: user <name> <reason>` 修订：
- 仅带出当前草稿的用户/入站绑定；中转凭据、出站、出口组、链与规则保留已发布状态；入站启用意图继承基线 `config_revisions.inbound_enabled_json`（无凭据渲染为 disabled，历史 NULL 或已禁用入站保持禁用直至手工发布）。
- 启动先播种状态机再对已有发布状态的节点对账，仅不一致者排队；每次部署/回滚成功后排一次对账（一致不发版）。
- 目标为当前授权节点 ∪ 已发布状态仍持凭据节点；删除用户亦排队清理凭据。
- 忙碌等下窗口，失败按最新已发布状态重建，连续 3 次失败停止重试并触发 `system.alert` 与日志告警；草稿不可渲染时回退基线仅移除不活跃用户；组件添加/移除中返回 `core_pending` 不静默搬移凭据。自动版本仍重启节点全部核心。

## 22.2 核心更新与移除

**核心更新**：Owner 提交组件、稳定版本、白名单 GitHub URL 与厂商公布归档 SHA-256 → 面板与 Agent 双检平台/版本/来源/摘要 → 解包（拒绝符号链接、穿越、多余文件）→ 原生版本与健康校验切换，失败回滚。缺失组件走同一请求安装启用。
- **发布信息自动填写**：`GET /api/v1/core-releases`（Owner，缓存 10 分钟，`?refresh=1` 跳过，失败不缓存且返回 502 `core_release_lookup_failed` 不回显上游）按（组件、来源）列最近 5 个正式版与厂商公布摘要（Xray `.dgst`、官方 sing-box GitHub 摘要、qui `SHA256SUMS`、GOST 本地固定值），面板从不自行算摘要；按节点架构与 libc（Alpine 选 `-musl`）解析，手改后不覆盖。
- **来源约束**：Xray 仅官方；sing-box 支持官方与精确 `sing-box-<版本>-qui.<序号>-linux-<架构>.tar.gz`（需 Agent 能力）；GOST 固定 `linux/amd64` 3.3.2（`go-gost/gost` 归档摘要固定于 `pkg/contracts/release`，需 `Hello.gost_core_supported`）。
- **任务与回执**：按（节点、组件）唯一活跃，同节点不同组件可并发。断线重发同身份；worker 内存队列保留回执至 Ack（连续 3 次永久拒绝则丢弃并 Warn），重启丢失由重连关闭或 Owner 清理兜底。升级成功须等 `Capabilities.cores` 上报后可选，进行中列入 `core_selectionBlocked`（操作拒绝 `core_pending`）。
- **无修订节点基础配置**：从未发布配置的在线节点升级 Agent 或核心前，调用 `POST /api/v1/nodes/{id}/baseline` 发布仅含节点级必需设置的基础修订（`auto: baseline before upgrade`，无入站不启核），等待确认（最多 3 分钟）后再排队升级。

**核心移除**：`POST /api/v1/nodes/{id}/cores/{component}/remove`（Owner＋CSRF＋`confirmNodeId`＋当前确认修订）创建 `core_remove_tasks`，下发 `RemoveCoreCommand`，移除回执**持久保存**至 Ack。拒绝码：`core_in_use`（草稿/已发布含该核入站，含禁用）、`core_pending`、`last_core`（仅不支持可选用户内核的旧 Agent 不可移除最后 Xray/sing-box）、`revision_mismatch`。支持新能力的 Agent 移除最后用户内核前，面板检查全部依赖，任一阻塞返回 409 `user_core_in_use`：非 GOST 入站（草稿及已发布、启用及停用）、本节点出站/出口组/代理链跳、以本节点为中转或出口的中转线路与画布方案；GOST 隧道不阻塞。Agent 先检查回退镜像并激活配置格式 6，失败以 `fallback_incompatible` 结束移除任务，错误码原样保留。移除须确认进程退出（`ErrCoreStopUnconfirmed` 保留全部现场），随后更新 `cores.json.enc`、删 `runtime/<core>/` 与自管镜像（保留安装器文件）。回滚到已移除核心的历史修订在面板明确拒绝，须重装组件或发布新修订。

# 23. Node Agent 模块设计

受支持代码以 **v1.7.5** 为下限：低版本、dev 或缺版本 Agent 在 `Enroll` 与 `Hello` 阶段拒绝（登记检查先于签证与 token 消耗）。安装与恢复前的镜像探测必须同时通过 v2 storage 与 control-only 合同，缺能力或多余输出一律拒绝。目录沿用 1.7.5 布局；保留对 1.7.5 有效安装记录 v1 的兼容读取。

核心模块：`Identity`（身份/证书）、`Control`（连接/心跳/重连/Ack）、`Capability`（系统/核心探测）、`Reconciler`（期望协调）、`Compiler`（Xray/sing-box/GOST）、`Supervisor`（进程/健康）、`Collector`（统计/域名）、`Spool`（持久缓存）、`Updater`/`agenthost`/`agentjobs`（Agent 与核心更新）。GOST 持久化 JSON、运行前生成 YAML。

**安全与固定操作白名单**：**严禁**任意 Shell、Web Terminal、任意命令或上传脚本执行。仅允许应用/回滚版本、重启/重载核心、健康检查、证书轮换、Agent/核心更新与移除，以及以下固定操作：
- **强制出站源地址与端口跳跃**：独立 oneshot `host-network` helper（仅 `CAP_NET_ADMIN`）维护 nft 表 `inet proxy_agent_source`（含 `extra_exempt_ipv4/ipv6`，每族最多 16 项，由回执 `features:["extra_exempt"]` 声明能力）与 `inet proxy_agent_hop`。
- **白名单调优**：19 键 sysctl root helper（第 22.0 节）。
- **TTL 设备门控**：Xray 运行时规则 API / sing-box Clash 关连接，不发修订不重启。
- **受限 TCP-connect 探测**：仅接受已解析 IP、端口、次数（≤10）、超时（≤3 秒）与截止时间。定时每分钟探测在线节点（每目标 3×3000 ms，默认及每节点自定义各最多 5 目标，手动优先），采样表保留 7 天，≤12 小时返回原点，>12 小时按 epoch 分桶（每目标约 720 点）。
- **转发与 GOST 中继**：GOST 按序渲染 `forwarder.nodes`、限速（`<整数字节>B`）与连接上限；证书写入 `runtime/gost/tls/<入站>/<半边>/`（0700/0600）。GOST 上报契约 **4**（支持中间 relay 下一跳链，兼容两节点 ≥3；UDP 由 TLS/TCP 封装至出口转发）。

**健康与恢复**：即时健康检查验证核心进程与本机监听（GOST 含 TCP 可拨；Linux UDP 按端口占用判断，不验证 socket 的 PID 归属）；巡检每 3 秒，仅 `ErrListenerUnavailable` 连续 3 次触发恢复，进程退出等立即恢复并退避，分核判断不因单次拨号失败重启健康核心。认领另行验证原核心进程身份，不能用监听可达代替。目标可达性不属核心健康。

认领与 Prepare 期间暂停监督恢复；认领后单核崩溃仍只恢复该核。只有有效 armed 更新请求允许退出交接，授权窗口为 300 秒 CLOCK_BOOTTIME；受管 worker 接受 bootstrap 与 worker 两种作用域。SIGINT、普通 stop/restart、卸载、未授权 EOF 和非更新异常退出仍停核；崩溃遗留核心由停核屏障确认后正常恢复。

**Agent 网页升级**：v1.7.5 网页升级统一调用节点本机 `/usr/local/bin/qui-update --component agent --version X.Y.Z --yes`（新装可在安装对话框勾选 `--web-upgrade`，已有节点运行一次 `qui-update --component agent` 启用），详见第 27.9 节与[部署与升级](deployment.md#5-升级)。

# 24. 中央端模块设计

中央控制端后端模块（`internal/panel/`，跨模块编排在 `internal/platform/server/`）：

```text
auth          管理员登录、会话、CSRF、限流与权限
user          用户、隐藏接入身份、凭据、额度与设备观测
portal        终端用户登录、独立会话和本人数据
node          节点、节点组、注册/恢复、证书、心跳、能力与逻辑删除
subscription  用户订阅生成、外部导入源与客户端策略配置库
proxy         入站、出站、出口组、代理链、隧道与画布资源
policy        分流、服务规则、规则集与网站控制
deployment    配置编译、发布、回滚、核心与 Agent 更新任务
access        域名访问记录、流量统计、核心历史整理与保留清理
dashboard     总览聚合查询（只读）
statistics    后台准备同一读取快照下的日／月／趋势／整机统计，供总览和服务器页面共享
connections   面板内存中的实时连接快照（不落库）
system        系统设置、备份恢复、证书库、通知与操作日志
```

模块目录结构遵循 `model.go` / `service.go` / `repository.go` / `repository_sqlite.go` / `handler_http.go` / `dto.go` / `errors.go`。约束：
1. 每模块仅经自身 Repository 修改所属表，跨模块经接口或应用编排服务调用。
2. 协议配置经类型化结构校验，禁止无结构 JSON 透传下发节点。
3. `dashboard` 只读查询；配置发布统一经 `deployment`。

# 25. 前端模块设计

目录按业务模块组织（`web/src/app/` 放 `router.ts`/`auth.ts`/`api.ts`，`web/src/modules/` 按 `auth`/`dashboard`/`users`/`nodes`/`proxy`/`routing`/`access`/`system` 划分 `pages/`、`components/`、`api.ts`，共享组件与类型在 `components/`、`layouts/`、`types/`、`styles/`）。

轻量化约束：无 SSR、生产无 Node.js、路由懒加载、极少全局状态、列表服务端分页、单条 SSE 推送实时状态（普通 CRUD 不用 WebSocket）、统一复用基础组件。

# 26. 数据库设计

中央数据库使用本地 SQLite（建议路径 `/var/lib/proxy-panel/panel.db`，不放网络共享目录）。

## 26.1 管理员

`admins`、`sessions`、`admin_operations`

## 26.2 用户与设备

`devices` 仅保存每用户一个隐藏接入身份，不再提供设备管理。

`users`、`devices`、`credentials`、`subscription_tokens`、`user_sessions`

## 26.3 节点与节点组

`nodes`、`node_identities`、`node_enrollment_tokens`、`node_identity_recoveries`、`node_identity_retirements`、`node_meta`、`node_groups`、`node_group_servers`、`node_group_members`、`node_group_inbounds`、`node_group_relays`、`node_group_imports`、`user_node_group_grants`、`node_tuning`、`node_tuning_dismissals`、`probe_targets`、`node_tcp_probes`、`node_latency_targets`、`node_latency_samples`、`node_metric_samples`

## 26.4 代理配置

`inbounds`、`outbounds`、`outbound_groups`、`outbound_group_members`、`proxy_chains`、`proxy_chain_hops`、`tunnels`、`tunnel_hops`、`tunnel_rules`、`tunnel_objects`、`port_claims`、`canvas_schemes`、`canvas_tunnels`、`canvas_tunnel_objects`、`canvas_tunnel_publications`、`canvas_entry_bindings`、`relay_cleanup_nodes`、`subscription_sources`、`imported_proxies`、`client_policy_profiles`、`subscription_route_dependencies`

## 26.5 分流与网站控制

`policies`、`service_sets`、`service_set_entries`、`service_set_sources`、`custom_domain_entries`

## 26.6 配置发布

`config_revisions`、`deployment_targets`、`core_update_tasks`、`core_remove_tasks`、`agent_system_update_tasks`、`agent_system_update_attempts`、`agent_system_update_logs`

## 26.7 访问记录和流量

`access_rollups`、`traffic_hourly`、`traffic_daily`、`node_status_snapshots`、`node_traffic_cursors`、`core_traffic_samples`、`core_traffic_hourly`、`traffic_sample_hours`、`traffic_storage_job`、`traffic_delivery_queues`、`node_machine_traffic_settings`、`node_machine_traffic_daily`、`node_machine_traffic_cursors`

## 26.8 系统

`system_settings`、`backups`、`certificates`、`notification_rules`、`notification_channels`、`notifications`、`notification_state`

协议配置存于密封 JSON（`inbounds.encrypted_settings`、`outbounds.encrypted_settings` 等），策略与能力存于结构化 JSON（`policies.scope_json`/`match_json`/`action_json`、`nodes.capabilities_json`），均须在 Go 解析为明确结构，禁止无结构透传。

## 26.9 迁移与兼容规则

**基线与不可变规则**：
- 现有数据库与备份归档**最低兼容版本为 v1.7.5（迁移头 `0158`）**；早于 `0158` 或记录不完整的数据库拒绝打开（`ErrPredatesBaseline`，`--check-database` 报 `outdated`），须先经 v1.7.5 完成升级。全新空库正常初始化。
- 历史 0001–0152 迁移已合并为 `migrations/0152_baseline.sql`，旧文件名与校验和保留于 `internal/platform/db/legacy.go`（校验和由 `migrations/checksums_test.go` 固定）。已应用迁移不可修改；记录完整 152 条旧迁移且校验和一致的库视为已在基线，原样保留不补写基线行。

当前 `migrations/` 文件清单：

| 迁移文件 | 作用 |
| --- | --- |
| `0152_baseline.sql` | 新库建出 0001–0152 全量结构与内置数据（含节点/用户/隧道/画布/证书/通知/多核心/规则集/整机流量等基线表） |
| `0153_drop_one_shot_markers.sql` | 清除历史一次性转换标记表与未使用的 `app_metadata` |
| `0154_traffic_storage_retirement.sql` | 新增 `core_traffic_hourly`、`traffic_storage_job`、`traffic_delivery_queues` 及回执/pending 固定交付身份（`queue_id`/`delivery_seq`） |
| `0155_traffic_display_hours.sql` | 新增可重建展示读索引 `traffic_sample_hours` |
| `0156_tuning_module_intent.sql` | `node_tuning` 新增 `install_missing_modules` 标记 |
| `0157_agent_system_updates.sql` | `node_update_capabilities` 新增系统更新探测列；新增 `agent_system_update_tasks`/`attempts`/`logs` |
| `0158_subscription_route_dependencies.sql` | 新增成功发布线路依赖表 `subscription_route_dependencies` |
| `0159_inbound_egress_source.sql` | 入站新增 `egress_source_ip`，默认空字符串 |
| `0160_node_meta_auto_renew.sql` | `node_meta` 新增 `auto_renew`（0/1，默认 0） |
| `0161_agent_state_reset.sql` | 旧 Agent 一次性状态换代、身份封存与回执代次隔离 |
| `0162_telemetry_tiers.sql` | 监控指标五分钟汇总与延迟分层存储 |
| `0163_traffic_minutes.sql` | 核心与账户流量 UTC 分钟汇总表 |

**持续生效的运行时与数据兼容合同（v1.7.5 基线）**：
- **隧道发布收尾（基线 0149 规则）**：部署批次含 Vault 密封收尾 journal 与完成标记，单节点发布亦使用持久批次；请求断开不取消发布，超 300 秒返回 202/`publishing`；终态、周期扫描与重启统一幂等收尾。**启动收尾失败只记日志，不阻止面板启动；未收尾批次继续占用其节点。**
- **v1.7.5 有效历史数据保留**：保留安装记录 v1、旧迁移 checksum 链、历史修订中的 `budget`/`weighted` 与 `legacyRelayNodes`、只读 `port_forward`/GOST 画布连接、`user_source_grants` 兼容表、`users.max_devices`/`max_connections` 旧列、`canvas_entry_bindings` 创建基线双列同时 `NULL` 的旧行，以及历史 `null`/缺失画布数组的可读性（新写入严格校验数组）。
- **导入与策略身份**：`content` 来源允许合法悬空精确成员，URL 整源成员仍合法，历史无法冻结引用保留诊断不自动重绑；客户端策略配置保留 `importKey` 服务端幂等去重。
- **更新任务与流量事实**：既有 worker 更新任务与系统更新任务遵守真实占用、尝试历史与回执合同（不伪造终态、不清除排队恢复）；队列身份、未确认日志、最终补传证据与恢复 journal 不按年龄清理；旧无编号流量回执不按时间淘汰，新协议须满足 `R ≤ A ≤ C` 且 ≥24 小时方可淘汰。

# 27. API

统一前缀 `/api/v1`。除特别说明外，写操作（含只读 POST 预览/规划）须校验同源 CSRF 并记审计日志；Owner 与 Viewer 权限严格隔离，门户会话与管理会话互斥。

## 27.1 身份认证

```text
POST /api/v1/auth/login
POST /api/v1/auth/logout
GET  /api/v1/auth/session
PUT  /api/v1/auth/password
```

## 27.2 用户

```text
GET/POST       /api/v1/users
GET/PUT/DELETE /api/v1/users/{id}
```

## 27.3 节点组授权与移除的设备 API

```text
POST   /api/v1/users/{id}/node-groups/{groupId}
DELETE /api/v1/users/{id}/node-groups/{groupId}
```

旧设备路由已删除（无 410 兼容）；订阅 Token 请求携带 `deviceId` 拒绝；用户 API 拒绝 `maxDevices`/`maxConnections`，统一使用 `deviceLimit`。

## 27.4 节点

```text
GET    /api/v1/nodes                         # 默认未删除，?deleted=1 仅已删除（Owner/Viewer）
GET/PUT/DELETE /api/v1/nodes/{id}
GET    /api/v1/nodes/{id}/deletion-preview
GET    /api/v1/nodes/{id}/metrics?hours=1..168
GET/PUT /api/v1/nodes/{id}/meta
GET/PUT /api/v1/nodes/{id}/tuning
GET    /api/v1/nodes/{id}/tuning/recommendations
POST   /api/v1/nodes/{id}/tuning/recommendations/dismiss
GET/PUT /api/v1/system/tuning-auto-apply
GET/PUT /api/v1/system/server-order
GET    /api/v1/nodes/machine-traffic
GET    /api/v1/nodes/{id}/machine-traffic
GET/PUT /api/v1/nodes/{id}/machine-traffic/settings
POST   /api/v1/nodes/enrollment-token
POST   /api/v1/nodes/{id}/recovery-token
GET    /api/v1/nodes/{id}/recovery-status
GET    /api/v1/nodes/{id}/certificate-matches?domain=<域名>
GET    /install/ca.pem                       # 无需登录，返回面板顶层 CA PEM（no-store）
```

关键合同：
- 节点对象含 `tags`、`gostCoreSupported`、`relayCleanupPending`/`relayCleanupCode`、`publicIp`、`publicIpSource`、`publicAddresses`、`publicIpSelected`、`publicIpSelectionMissing`、`connectHost` 与只读 `countryCode`。`PUT /nodes/{id}` 与 `enrollment-token` 校验 `connectHost`（`netaddr.ValidHostName`，非法 400）。
- `DELETE /nodes/{id}`（Owner）执行第 11.3 节连带删除，成功 200 `{pendingNodes}`，阻塞 409 `node_delete_blocked` 与 `error.blockers`。
- `server-order` GET 允许 Owner/Viewer，PUT 仅 Owner＋CSRF（`{ids,expectedRevision}`，≤1000 个存在未删除 ID，冲突 409 `settings_conflict`）。
- 原位恢复：`recovery-token` 201 返回一次性 `enrollmentToken`（`purpose:"recover"`，300–3600 秒）；未登记/禁用/已删 409 `node_not_enrolled`，存在未完成任务 409 `node_recovery_blocked` 与 `blockers`。
- `metrics`（Owner/Viewer）：默认 24 小时，≤12 小时原始分钟点，>12 小时 epoch 对齐分桶（`intervalSeconds=ceil(hours×60/720)×60`），缺测为 `null`。
- `tuning` PUT（Owner＋CSRF）：`{settings,clear,confirmNodeId,installMissingModules?}`，拒绝未知字段，202 `{requestId,status:"pending"}`；并发/能力/离线见第 22.0 节。`meta` 支持可选 `bandwidthMbps`（1–100000）与 `portRangeStart`/`portRangeEnd`（须同时设置或清空）。
- 整机流量：GET 允许 Owner/Viewer，PUT settings 仅 Owner＋CSRF，须提交完整 `{method,interfaceName,allowanceBytes,resetDay}`（拒绝未知字段，字节为十进制字符串，`status` 为 `measured`/`stale`/`unknown`）。

可选用户内核错误合同（Owner 写操作仍要求 CSRF）：
- 入站、出站、出口组、代理链、节点级分流/网站控制、中转线路与方案的保存及发布门禁返回 **400 `no_user_core`**；显式 GOST forward 与 GOST 隧道按原能力检查。
- `POST /nodes/{id}/cores/{component}/remove` 移除最后用户内核存在依赖时返回 **409 `user_core_in_use`**。响应为 `{"error":{"code":"user_core_in_use","message":"Core removal refused","inboundIds":[],"outboundIds":[],"outboundGroupIds":[],"proxyChainIds":[],"relayIds":[],"schemeIds":[]}}`，数组始终存在、去重并按 ID 排序；`proxyChainIds` 按涉及本节点的代理链归组（跳无独立 ID），`relayIds` 是 `canvas_tunnels` 中转线路 ID，`schemeIds` 是服务端画布方案 ID。草稿、停用对象同样阻塞。
- 无用户内核节点的 Agent 单台/批量升级（均走 `/agent-updates`）、重试及历史受管恢复拒绝低于 **1.9.0** 的目标，返回 **400 `agent_version_unsupported`**。配置格式 6 激活后，即使重新添加用户内核，Agent 存储下限仍阻止旧镜像启动。
- 移除回执 `fallback_incompatible` 保持原码，任务状态为 `rejected`；`core_pending`、`revision_mismatch` 及旧 Agent 的 `last_core` 不变。

## 27.5 节点组

```text
GET/POST       /api/v1/node-groups
PUT/DELETE     /api/v1/node-groups/{id}
```

Owner 专用。请求接受显式 `serverIds`（≤100）、`inboundIds`（≤500）、`relayIds`（≤500，关联目标或导入目标规则，省略/null 保留、`[]` 清空）与 `imports`（≤1000）；拒绝未知字段与只读派生字段 `nodeIds`。响应返回上述字段及只读 `nodeIds`、`relayNodeIds`、`grantedUserIds`。导入来源 API 忽略请求 `userIds`，响应返回经有效分组可达的去重用户集合；`user_source_grants` 仅保留数据兼容，不参与访问判断。

## 27.6 代理配置

```text
GET/POST/PUT/DELETE /api/v1/inbounds
GET/POST/PUT/DELETE /api/v1/outbounds
GET/POST/PUT/DELETE /api/v1/outbound-groups
GET/POST/PUT/DELETE /api/v1/proxy-chains
```

入站 API（列表、读取、创建、更新）包含 `egressSourceIp`：空字符串清除，非空只接受有效单播 IP，保存为规范形式。

## 27.7 分流

```text
GET/POST/PUT/DELETE /api/v1/policies
GET/POST/PUT/DELETE /api/v1/service-sets
GET                 /api/v1/service-sets/{id}/references
POST                /api/v1/service-sets/{id}/sources/{sourceId}/refresh
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
POST /api/v1/nodes/{id}/baseline                  # Owner+CSRF，{confirmNodeId}，无修订节点发布升级基础配置
GET/POST /api/v1/nodes/{id}/restart-core          # {expectedRevision,confirmNodeId,core?}，空 core 为整节点
GET  /api/v1/nodes/{id}/health-checks
GET  /api/v1/nodes/{id}/inbound-states            # Owner/Viewer，逐入站发布状态
GET  /api/v1/core-releases                        # Owner，?component=&source=[&refresh=1]
GET/POST /api/v1/core-updates
POST /api/v1/core-updates/{id}/clear
POST /api/v1/nodes/{id}/cores/{component}/remove  # Owner+CSRF，{confirmNodeId,revision}
GET  /api/v1/nodes/{id}/cores/{component}/removals
GET  /api/v1/agent-releases                       # Owner，?refresh=1，GitHub 最新官方签名正式版
POST /api/v1/agent-updates                        # Owner+CSRF，{nodeId,confirmNodeId,revision,targetVersion}，新建 v1.7.5 完整系统更新
GET  /api/v1/agent-updates                        # Owner，含 system 与历史 worker 任务（mode 区分）
GET  /api/v1/agent-updates/{id}/attempts          # Owner，各次尝试真实结果
GET  /api/v1/agent-updates/{id}/logs              # Owner，attemptId/offset 分页，download=1 下载
POST /api/v1/agent-updates/{id}/retry             # Owner+CSRF，仅明确失败的完整系统升级可重试
POST /api/v1/agent-updates/{id}/recover           # Owner+CSRF，仅已有在途 worker 任务恢复
```

历史面板托管 Agent 目录路由（**仅用于读取与恢复 v1.7.5 已存在的在途 worker 任务；新建任务一律走 v1.7.5 完整系统更新，不再创建新 worker 任务**）：

```text
POST /api/v1/agent-releases/{version}/import      # 旧托管目录导入（仅供已有 worker 任务兼容）
GET  /api/v1/releases                             # 旧已导入签名 Agent 版本（只读）
GET  /agent-catalogue/...                         # 旧只读签名清单与归档目录（严格白名单防穿越，仅供在途 worker 任务）
```

合同要点：
- `restart-core` 指定 `core` 须属于 `Capabilities.cores`，否则 409 `restart_conflict`；回滚到已移除核心的修订返回 409。
- `POST /api/v1/agent-updates` 固定调用节点本机 `/usr/local/bin/qui-update --component agent --version X.Y.Z --yes`，完整沿用终端下载、信任与安装/恢复流程，不导入面板 Agent 文件。节点上报 `agentSystemUpdateSupported`、`agentSystemUpdateError`（原文保留换行/路径/退出码）与 `agentBootstrapVersion`，未就绪时提示终端迁移命令。
- 既有 v1.7.5 worker 任务继续按原签名命令派发、重发、接收真实结果与保留恢复入口；`queued`/`running`/`failed_closed` 及排队恢复均持续占用节点直至真实结果释放，迟到原回执不可结束当前恢复尝试；日志分块持久化，未知结果保持待确认不自动重试。详见[v1.7.5 发布说明](releases/release-notes-1.7.5.md)。

## 27.10 用户订阅

```text
GET /sub/{token}
GET /sub/{token}?format=base64
GET /sub/{token}?format=sing-box
GET /sub/{token}/rules/{id}   # 订阅 Token 授权，输出客户端 classical provider，撤销返回 403
```

## 27.11 实时状态

```text
GET /api/v1/events
```

SSE 推送节点状态、配置发布、总览统计与系统告警；按管理员角色过滤（Viewer 仅收节点/发布/总览及 `node_offline`/`cpu`/`memory`/`disk` 四类监控通知，每 15 秒重认证同步角色）。关闭 `node_offline` 规则只停止通知，界面与订阅仍读取该规则的离线时限。通知中心使用不透明背景、320px 宽度（不超出视口），每次打开默认仅显示未读，可切换全部。

展示层统一状态优先级：删除 ＞ 停用 ＞ 维护 ＞ 待登记 ＞ 连接状态（等待恢复、离线、核心异常、降级）。管理后台共用通知规则 `node_offline.durationMinutes`（默认 3 分钟，0 表示断线/心跳过期即离线）判定展示离线，超过 45 秒心跳有效期而未达该时长显示「连接中断，等待恢复」；节点读取与 `node_runtime` 快照提供只读 `offlineAfterSeconds`，写操作准入与指标新鲜度仍按真实时钟 45 秒规则执行。节点同时提供原始持久化状态 `persistedStatus` 和只读 `liveness`（评估时间/45 秒有效期、会话号、连接、最后真实接收、在途数量/起点、处理阶段与断开时间）。单实例已认证、已完成握手、非 replacement drain 的当前会话收到有效消息后，在进入 SQL 前登记在途；在途未完成时仍视为连接存活，10 秒后页面提示「面板处理延迟」，日志每 60 秒重复。Welcome/数据库提交后的 Ack 成功发送才移除相应在途；没有在途时按数据库心跳与最后真实接收的较新者计算原有时限，只有连接不能无限保活。断流/取消立即撤销在途覆盖，禁用、删除及身份失效在提交后清理并断开；保留业务降级/异常、核心失败、指标年龄和真实数据库心跳。列表、总览、门户、订阅、离线及恢复通知使用同一解释，控制准入继续只用持久化事实。恢复通知可明确显示数据库处理仍有延迟，不声称数据追平。浏览器证据过期时显示「面板存活状态未更新，等待校准」，不依据旧 connected 宣称在线或离线。前端按 generation/revision 接受 `node_runtime` 增量，可见时每 30 秒及重连/切回前台校准，断连保留最后确认状态并冻结展示时钟。门户节点接口增加明确 `status`（保留 `online`），仅在节点页可见时 30 秒轮询，不接入管理 SSE。

## 27.12 门户、订阅、观测与画布接口

均以 `/api/v1` 为前缀；Owner 与门户会话互相拒绝：

```text
PUT    /users/{id}/login                       DELETE /users/{id}/sessions
POST   /users/{id}/credentials/reset           GET    /users/{id}/subscription-link
GET    /users/{id}/subscription-preview        GET    /users/{id}/portal-preview
GET    /users/{id}/devices-online              POST   /users/{id}/devices-online/reset
GET    /subscription-formats                   GET    /subscription-rules
GET    /subscription-rules/presets
GET/POST       /client-policy-profiles         GET/PUT/DELETE /client-policy-profiles/{id}
POST   /client-policy-profiles/{id}/activate   POST   /client-policy-profiles/preview
GET    /client-policy-candidates?userId={id}
GET/POST       /subscription-sources           PUT/DELETE     /subscription-sources/{id}
POST   /subscription-sources/{id}/refresh      GET    /imported-proxies
PUT/DELETE     /imported-proxies/{id}          GET    /imported-proxies/{id}/deletion-preview
GET/POST       /probe-targets                  PUT/DELETE     /probe-targets/{target}
GET/POST       /nodes/{id}/tcp-probes          GET    /nodes/{id}/latency?hours=24
GET/PUT        /nodes/{id}/latency-targets     POST   /inbounds/{id}/reachability
POST   /nodes/{id}/publish                     GET    /revisions?nodeId={id}
GET/POST       /canvas-tunnels                 PUT/DELETE     /canvas-tunnels/{id}
GET/POST       /canvas-schemes                 PUT/DELETE     /canvas-schemes/{id}
POST   /canvas-schemes/{id}/plan               POST   /canvas-schemes/{id}/publish
POST   /canvas-schemes/{id}/remove             GET    /canvas-schemes/{id}/status
GET    /canvas-import-exits                    POST   /relay-schemes/quick/plan
POST   /relay-schemes/quick                    GET    /inbounds/{id}/deletion-preview
POST   /portal/auth/login                      POST   /portal/auth/logout
GET    /portal/auth/session                    PUT    /portal/auth/password
GET    /portal/me                              GET    /portal/traffic
GET    /portal/nodes                           GET    /portal/devices-online
GET    /portal/subscription                    POST   /portal/subscription/reset
```

主要错误与语义：
- `/canvas-tunnels` 写操作要求 CSRF（PUT 须带 `updatedAt`）；旧画布 GOST 只读视图仅露代数、服务器名、到期与指纹。
- `/canvas-schemes/{id}/plan` 仅读取，支持规划已保存或未保存 `{"layout":...}`；客户端方案返回 `ok:false`。
- `/canvas-schemes/{id}/remove` 与 `DELETE /canvas-schemes/{id}` 均为完整撤除（体须含 `updatedAt`，过期 409 `stale_scheme`）。
- `/relay-schemes/quick` 202 返回 `{schemeId,publication,warnings}`，出口为 `exitInboundId` 或 `exitImportId` 之一。
- `/imported-proxies/{id}` 改名与删除校验 `expectedRevision`，删除被规则/画布引用分别返回 409 `import_in_use_by_rule`/`import_in_use_by_canvas`/`import_referenced`。
- `/nodes/{id}/latency` 返回 `{intervalSeconds,sampleSeconds,targets,series}`（`sampleSeconds=60`，≤12 小时 `intervalSeconds=60`，>12 小时分桶）；`/latency-targets` 上限 5 个（409 `node_target_limit`）。

## 27.13 隧道管理与节点中转

均以 `/api/v1` 为前缀。GET 允许 Owner/Viewer；POST/PUT/DELETE（含只读 plan/allocate）要求 Owner＋CSRF；PUT 与轮换须带 `updatedAt`（不匹配 409 `stale`）。

| 接口 | 合同 |
| --- | --- |
| `GET/POST /tunnels`、`PUT/DELETE /tunnels/{id}` | 列表 `{items,total}`；在用删返回 409 `tunnel_in_use`，已发布转 `pending_delete` |
| `POST /tunnels/{id}/rotate-credentials` | `{updatedAt}` → 仅轮换草稿凭据 |
| `GET/POST /tunnel-rules`、`PUT/DELETE /tunnel-rules/{id}` | 支持 `tunnelId`/`nodeId` 筛选；已发布删待发布移除 |
| `GET /nodes/{id}/ports/check?port=` | `{nodeId,port,free,usedBy?}`（含禁用入站提示） |
| `POST /nodes/{id}/ports/allocate` | `{count?}` → `{ports:[]}`，仅建议不预留 |
| `POST /tunnels/plan` | `{tunnelIds,ruleIds}` → `{ok,servers,conflicts,warnings,message?}`，只读 |
| `POST /tunnels/publish` | 同一选择体 → `{batchId?,servers}`（`applied`/`unchanged`/`failed`/`rolled_back`/`skipped`） |
| `GET/POST /nodes/{id}/relays` | GET 返回 `{items:[{rule,shareLink}]}`；POST 创建本节点 `linked` 规则 |
| `GET /nodes/relay-rules?nodeIds=id1,id2` | 批量按序读取中转规则，不生成 `shareLink` |

端口错误码：`409 port_in_use`（含 `usedBy`）、`400 port_out_of_range`、`409 no_free_port`。`pending_delete` 只读并保留声明与引用。

## 27.14 系统设置：节点安装连接地址

存于 `system_settings` 键 `install_endpoint`（`{host,httpPort,grpcHost,grpcPort,revision,updatedAt,updatedBy}`）。Owner 专用 `GET/PUT /api/v1/system/install-endpoint`（PUT 须 CSRF 与 `expectedRevision`，冲突 409 `settings_conflict`）。

`host`/`grpcHost` 为空或匹配 `^[A-Za-z0-9.-]+$`（≤253、每段 ≤63，不接受裸 IPv6、协议或端口）；两端口各为 0 或 1–65535 且非 0 时不可相同。新建服务器、原位恢复与添加内核命令对话框共用该设置。

## 27.15 系统管理：面板网页更新

Owner 专用 `GET/POST /api/v1/system/panel-update`（查询 GitHub 最新正式版缓存 10 分钟，`?refresh=1` 强刷；不支持开发版 `dev_build` 或缺 helper `helper_missing`；仅允许升级最新正式版，不支持降级或 RC）。POST 校验版本严格较新后 202 返回 `{requestId,version}`（冲突码 `panel_update_unsupported`/`panel_update_busy`/`panel_update_stale`，查询失败 502 `panel_update_check_failed`）。

固定文件合同：面板仅原子写 `/var/lib/proxy-panel/panel-update/request.json`，由 systemd `proxy-panel-update.path` 触发 root oneshot `proxy-panel-update.service` 运行 `/usr/local/lib/proxycontrol/panel-update-request`，严格校验后执行 `/usr/local/bin/qui-update --component panel --version X.Y.Z --yes` 并将固定状态码写回 `status.json`（输出仅追加至 root 0600 `/var/lib/proxycontrol-install/panel-update.log`）。同一 `requestId` 仅执行一次；`pending` 超 10 分钟为 `stalled`、`running` 超 35 分钟为 `interrupted`，均解除占用。仅更新面板，不更新节点 Agent。

面板更新必须保留持久化回滚快照、停服后的最终主库与 WAL 独立备份及完整性、迁移校验；备份须逐字节比较并 fsync，预检成功才允许写 `prepared` 并安装，启动后须通过 10 秒 PID 稳定检查，失败自动回滚。`--check-database` 只接受离线输入，通过只读硬链接与私有 WAL 索引避免再次复制整库，不修改原库、WAL 或 SHM；更新器的检查材料限于 root 登记目录，root 不在面板账号目录内创建或删除检查材料，检查材料准备或删除失败必须拒绝更新。快照时机、检查目录与信号清理（含 SIGHUP）、中断恢复、文件同步及旧更新器首次升级的生效边界见[部署与升级 §5](deployment.md#5-升级)。

## 27.16 系统管理：证书与 Cloudflare DNS-01

面板自管 ACME 证书申请与续期（Cloudflare Token 仅留面板不发节点）。全部接口 Owner 专用，写操作须 CSRF 并记日志：
- `GET/PUT /api/v1/system/certificate-settings`：维护 `email`、`directory`（`production`/`staging`）、自动发布窗口 `deployStartHour`/`deployEndHour`（0–23 整数且不相等，默认 3–6 点，结束小时不含）与 `cloudflareToken`（非空值先调 Cloudflare `tokens/verify` 校验，永不回显明文；`expectedRevision` 防冲突）。
- `GET/POST /api/v1/certificates`、`POST /api/v1/certificates/{id}/renew`、`DELETE /api/v1/certificates/{id}`：域名 1–10 个（小写、可带前导 `*.`、禁 IP 与重复），可选 `autoDns` 与 `dnsNodeId`；被入站引用删除返回 409 `certificate_in_use`。

单 worker 串行签发：确保 ACME 账户 → 可选写 A/AAAA（`proxied=false`）指向节点 `public_ip` → 写 `_acme-challenge` TXT 并直查权威 NS（每 5 秒、上限 180 秒）→ 签发 ECDSA P-256 证书并密封保存，无论成败均清理 TXT。失败写固定错误码并按 1h → 6h → 24h 退避（有效证书续期失败保持 `valid`，余期不足 7 天发 `system.alert`）；启动时残留 `issuing` 置 `failed`/`interrupted`。

**续期自动发布**：比较证书库与节点**已发布状态**，仅当入站 `certificate_id` 对应证书内容变更且 **DNS 名称集合完全相同**时，在低峰窗口内以已发布状态为基线仅替换证书/私钥创建 `auto: certificate <域名> renewed` 修订部署；节点忙不计失败，单节点连续 3 次失败停止并告警。

## 27.17 通知

```text
GET  /api/v1/notifications?unread=1&page=1&pageSize=20   # Owner 全部；Viewer 仅 4 类监控
POST /api/v1/notifications/read-all                      # Owner+CSRF
GET/PUT /api/v1/notification-rules                       # Owner；7 类规则完整替换
GET/POST /api/v1/notification-channels                   # Owner；仅返非敏感 format 与 last4
PUT/DELETE /api/v1/notification-channels/{id}
POST /api/v1/notification-channels/{id}/test
```

规则：`node_offline` 跳过待登记、禁用与维护节点；维护期间保留已有离线事件状态，退出维护后继续评估。面板通知 worker 每次启动后重新观察一个配置离线时限（至少 45 秒，覆盖 0 分钟规则的心跳窗口），期间不创建新的离线事件，避免将面板停机当成全部节点故障；已有离线事件保留，恢复通知照常发送。每轮离线通知一次、恢复通知一次（跨重启保留状态），其余规则按冷却时间去重；历史通知保留 90 天。渠道支持 `telegram` 与 `webhook`（HTTPS，`format` 支持 `generic`/`wecom`/`dingtalk`/`feishu`，`secret` 仅钉钉/飞书 ≤1024 字节，PUT 空串保留原值、`clearSecret: true` 清除）。三家机器人均须 HTTP 2xx 且业务码为 0，否则报 `channel_rejected`，响应读上限 64 KiB、不回显上游或凭据。

## 27.18 系统设置：管理员登录地址与门户字段（v1.5.4）

管理员登录路径存于 `system_settings.admin_entry`（默认 `/login`），`GET/PUT /api/v1/system/admin-entry`（Owner＋CSRF＋`expectedRevision`）：路径须单段 `/` 开头、4–64 字符（小写字母数字 `-`/`_`）、不可占用面板保留段（`system.ReservedAdminEntrySegments`）。面板仅在精确命中该路径时向 `index.html` 注入 `<meta name="qui-entry" content="admin">`，其余未知路径返回 404，公开接口不泄露该路径；遗忘时可在服务器停服后用 `qui` 或 `proxy-panel --admin-entry show|reset` 重置。这仅是隐藏入口，不改变 `/api/v1` 认证与 CSRF 边界。

门户补充字段：`GET /portal/me` 含 `trafficResetCycle` 与 `nextTrafficResetAt`；`GET /portal/nodes` 每项含本人可见 `protocols`；`GET /portal/subscription` 含 `defaultFormat`。

## 27.19 系统设置：Reality 预设（v1.6.0）

存于 `system_settings.reality_presets`，`GET/PUT /api/v1/system/reality-presets`（Owner＋CSRF＋`expectedRevision`）。最多 20 项，仅存公开参数 `{id,name,handshakeTarget,serverNames,transport,flow}` 与 `defaultId`（不含私钥或 Short ID）；名称 1–64 字符唯一，`serverNames` 为 1–16 个具体 DNS 主机名，`transport` 为 `''`/`tcp`/`xhttp`，Vision 仅配 `tcp`。编辑器仅复制参数，不绑定预设 ID。

# 28. gRPC 节点通信

节点通信使用 `Enroll` 与双向流 `Connect`（每消息含节点 ID、消息 ID、顺序号、时间、修订号与类型，节点保存最后确认顺序防重放）。

- **Agent 上报**：`Hello`、`Heartbeat`、`Capabilities`、`ApplyResult`、`HealthResult`、`TrafficBatch`、`AccessBatch`、`UpdateResult`、`CertificateRequest`。
- **面板下发**：`Welcome`、`DesiredState`、`ApplyCommand`、`RollbackCommand`、`HealthCommand`、`CertificateRotate`、`UpdateCommand`、`Ack`。

关键字段与能力协商：
- `Heartbeat`：省略即缺测、实测 0 保留；`public_ip`（9）由默认路由网卡按公网 IPv4 ＞ 公网 IPv6 选出（绝不发私网/回环/CGNAT）；`interfaces`（20）上报网卡累计字节；`boot_id`（34）与 `default_route_interface`（35）用于整机流量；`public_addresses`（36）上报最多 16 个全局单播候选；`tcp_out_segs`（37）与 `tcp_retrans_segs`（38）用于计算 TCP 重传率。
- **GOST 能力**：`Hello.gost_core_supported`（13）声明可安装/移除 GOST（落库 `nodes.gost_core_supported`，恢复清零重连刷新）；`CoreCapability.gost_contract_version`（9）声明契约版本（0–4，当前 Agent 上报 **4**，隧道两节点 ≥3、三节点 ≥4）。
- **逐核与主机能力**：逐核声明 `tls_certificate_files_supported`（12）、`domain_keyword_supported`（13）、`expanded_policy_supported`（14）、`native_rule_sets_supported`（15）、`outbound_source_supported`（16）及 `server_settings_contract_version`/`combinations`（10/11）；节点级声明 `panel_catalogue_supported`（26）、`port_hop_supported`（28）、`host_source_extra_exempt_supported`（32）。`HostInfo` 上报 `/etc/proxy-agent/certs/` 只读证书清单（9/10，永不读私钥，5 分钟重扫）及虚拟化、19 键调优状态、可用/可加载算法、句柄上限与 sysctl 来源（11–18）。`TuningResult.initial`（4）回传 19 键基线快照；`Welcome.agent_catalogue_port`（22）告知只读目录端口。

# 29. 安全设计

## 29.1 后台安全

登录会话默认 1 天（1440 分钟），Owner 可在 15–1440 分钟调整（管理员与门户共用，仅影响后续新登录）。门户会话与管理员完全隔离：门户 Cookie `qui_portal_session`（HTTPS 下 `__Host-` 前缀，HttpOnly、SameSite=Strict、Path=/），会话表仅存 Token 哈希；禁用登录、改密、撤销会话、删用户与恢复备份均立即终止门户会话；订阅链接不存 `localStorage`。

管理员密码安全哈希、HTTPS、登录限流、CSRF 防护、API 请求大小限制、Owner/Viewer 隔离、敏感数据默认隐藏、操作审计日志与自定义隐藏登录入口（第 27.18 节）。面板网页更新仅写版本号请求文件，由固定 root helper 调用 `qui-update`（第 27.15 节），不引入任意命令执行。

## 29.2 节点安全

- Agent 主动 mTLS 连接中央端，每节点独立私钥（不上传）与 90 天证书，支持轮换、吊销与一次性注册/恢复码（旧指纹永久作废）。
- 自动续证仅在控制连接内执行（签发满 30 天或余期 ≤14 天，沿用原私钥，失败退避 1 小时，服务端准入一致）。
- 不使用 SSH 作为日常控制通道，不提供任意命令执行。
- 网页升级须先建立受管安装历史；旧托管目录 `GET /agent-catalogue/` 仅开放严格正则白名单路径，底层 `os.OpenRoot` 防路径穿越，且 Agent 仅信控制连接同主机与 Welcome 端口。

## 29.3 凭据安全

- 用户凭据与节点间中转凭据隔离；订阅 Token 哈希查找＋vault 加密副本（第 9.3 节）。
- 节点间 GOST 证书材料（`tunnels.gost_material`/`canvas_tunnels.gost_material`）、通知渠道密钥（`notification_channels.encrypted_config`）、Cloudflare Token、ACME 私钥与签发证书（`certificates.encrypted_material`）均以 `configuration.key` AES-256-GCM 密封；CA 私钥签发后丢弃，API/日志永不回显明文。
- 节点敏感文件严格限制权限（0700/0600），配置与备份敏感字段加密保存。

## 29.4 数据安全

仅记录域名级元数据，不存正文内容；按保留周期自动清理；访问查询需权限；SQLite 一致性快照备份；恢复前校验文件与数据库完整性。

# 30. 部署目录

完整目录清单、权限、安装/升级/降级/卸载命令与 systemd/OpenRC 单元细节由部署文档维护，见[部署与升级](deployment.md#7-目录与权限)和[部署与升级](deployment.md#5-升级)；彻底卸载与故障处理见[运维手册](operations.md#彻底卸载qui-菜单uninstallsh---purge)。

## 30.1 中央端与更新公共目录

- 核心路径：程序 `/usr/local/bin/{proxy-panel,qui,qui-update}`，配置与 TLS `/etc/proxy-panel/`，数据与备份 `/var/lib/proxy-panel/`（`panel.db`、`identity/`、`backups/`、`rulesets/`、`releases/`、`panel-update/`），安装记录与操作锁 `/var/lib/proxycontrol-install/`（root 0700），服务与恢复/更新 helper `/usr/local/lib/proxycontrol/`。
- 数据库路径以 `/var/lib/proxycontrol-install/panel.database` 为准且须位于 `/var/lib/proxy-panel/` 内归面板账号所有；更新临时文件同文件系统校验后原子发布。面板日志走 stdout → journald（面板网页更新日志例外，追加至 `/var/lib/proxycontrol-install/panel-update.log` root 0600）。
- 自动申请证书需面板出站 HTTPS 访问 `api.cloudflare.com` 与 Let's Encrypt ACME 目录，及出站 UDP 53 查询权威 NS（无需开放入站 80 端口）。
- `qui-update` 支持 `--component panel|agent` 与 `--version`；Agent 禁止降级，仅面板支持配合 `--database` 备份回退。

## 30.2 节点端

- 核心路径：引导程序 `/usr/local/bin/{proxy-agent,qui,qui-update}`，安装器核心 `/usr/local/lib/proxy-agent/<core>`，配置/受信 CA/入站证书文件目录 `/etc/proxy-agent/`（`certs/` root:proxy-agent 0750），状态目录 `/var/lib/proxy-agent/`（`identity/`、`revisions/`、`spool/`、`releases/`、`runtime/<core>/`、`host-network/`、`tuning/`、`logs/`）。
- **Agent 日志（v1.7.3 起）**：运行日志统一写入状态目录 `logs/agent.log` 及 `.1`–`.9`（单文件 ≤10 MiB、总计 ≤100 MiB），监督进程接管工作进程 stderr；日志故障报至服务日志并继续排空输出以保护代理运行（流量队列与旧系统日志不在清理范围，见[运维手册](operations.md)）。
- **固定权限 helper**：`host-network`（仅 `CAP_NET_ADMIN`，维护 `inet proxy_agent_source`/`hop`）与 `tuning`（`CAP_NET_ADMIN`+`CAP_DAC_OVERRIDE`、禁用 `@module` 系统调用，独立 probe 核对二进制摘要与当前 boot）均支持 systemd 与 OpenRC。
- **受管 Agent 升级与安装记录**：终端 `qui`/`qui-update --component agent` 与网页完整升级复用同一本地监督流程、加密任务日志、签名校验与失败回滚（本地 `agent-local.sock` 0600，不提供命令执行）；安装记录采用 v2 格式并兼容读取有效 v1 记录，具体路径与升级步骤见[部署与升级](deployment.md#5-升级)和[部署与升级](deployment.md#7-目录与权限)。

# 31. 备份与恢复

备份涵盖 SQLite 数据库、系统配置、中央证书、规则集与密钥加密副本（默认保留每日 7、每周 4、每月 3）；恢复须停写并校验文件与数据库完整性，操作见[运维手册](operations.md)。

**基线准入**：当前仅接受已完成迁移 **0158（v1.7.5）** 及以后的归档，更早或缺失 0158 所需结构的归档在迁移校验阶段拒绝（需先用 v1.7.5 升级并重新备份）。恢复旧 1.7.5 快照时先认证并迁移副本，再与当前冻结库事实核对合并，不改写原归档；调度循环随停机/恢复取消并等待退出，恢复结束全部门户会话，排队探测过期，运行能力位清零待节点重连刷新，**不自动发布配置或触发外部抓取/证书申请**。

| 数据域 | 备份校验与恢复合并合同（v1.7.5 基线） |
| --- | --- |
| 系统设置（`client_rules`、`client_policy_active`、`install_endpoint`、`reality_presets`、`server_order`、`tuning_auto_apply`、`admin_entry`、证书/ACME/Cloudflare 密封值） | 随整库快照备份与往返；`client_policy_profiles` 校验活动指针与模板一致；`install_endpoint`/`reality_presets` 损坏在读取时报 `invalid_settings` |
| 节点地址、标签与元数据（`public_addresses_json`、`public_ip_selected`、`tags_json`、`bandwidth_mbps`） | 校验公网单播列表 ≤16 项、标签规范数组与带宽范围（1–100000），原样保留归档值 |
| 出站源绑定、隧道、画布与清理队列（`outbounds.source_*`、`tunnels*`、`port_claims`、`tunnel_objects`、`canvas_*`、`canvas_entry_bindings`、`relay_cleanup_nodes`） | 校验语法、ID 唯一归属、端口声明完整无冲突与快照合法，拒绝孤立/重复归属且不自动重建声明；密封 GOST 证书原样恢复不重签；历史 `canvas_entry_bindings` 双列同时 `NULL` 及旧 null/缺失画布数组合法可读；已发布隧道/画布对象恢复后标 `modified` 须手工发布；保留 `relay_cleanup_nodes` 交重试器继续处理 |
| 分组成员与导入节点（`node_group_relays`、`imported_proxies`、`subscription_sources`、`user_source_grants`） | 校验 `node_group_relays` 每组 ≤500 且目标合法、`imported_proxies.revision ≥ 1` 且明文与密封一致、`latest_result_json` 为固定码且无自由文本；`content` 拒绝整源但允许合法悬空精确成员，URL 整源仍合法；成员、授权与导入节点按当前库保守权威规则合并，不复活已撤销访问、不按名配对、不重抓 |
| 节点身份恢复与作废（`node_identity_retirements`、`node_identity_recoveries`） | 作废指纹只并入不复活，永不覆盖当前身份；恢复记录以当前库为准，`issued` 统一置过期，不重发恢复码、不重签证书 |
| 多核心任务与 Agent 更新任务（`core_update_tasks`、`core_remove_tasks`、`agent_system_update_*`、既有 worker 任务） | 校验组件/结果一致且同组件无添加与移除冲突；核心 `queued`/`running` 置 `unconfirmed` 不重发。既有 worker 任务与系统更新任务严格按真实终态、节点、原命令与修订核对合并：当前仍有未收尾任务时明确拒绝恢复，绝不清除排队恢复或伪造终态 |
| 调优与建议（`node_tuning`、`node_tuning_dismissals`） | 校验白名单键与值长度 ≤256；恢复以当前库为准，删归档独有行，将 `pending` 置 `unconfirmed`，不重发也不写宿主 sysctl |
| 流量账本、核心小时与回执封存（`node_traffic_cursors`、`core_traffic_samples`、`core_traffic_hourly`、`traffic_storage_job`、`traffic_delivery_queues`、回执与 pending） | 作为整组事实保护：有当前冻结库时先认证并迁移证据副本，统计、游标、回执完成 `C`/确认 `A`/封存 `R` 以当前库为准，pending 并集冲突拒绝恢复，绝不复活 `R` 以内数据；旧无编号回执不按时间淘汰 |
| 整机月流量（`node_machine_traffic_settings`/`daily`/`cursors`） | 三表长期保留、全部校验；有当前冻结库时整组保留当前表（空表亦权威），避免丢失当前量或复活旧量 |
| 证书库、通知与外部规则集（`certificates`、`notification_*`、`service_set_sources`） | 校验密封材料可解密、域名/规则/format/behavior 合法及 `native` 行无缓存；恢复将 `issuing` 证书置 `failed`/`interrupted` 按退避重试，保留通知（90 天清理）与规则集缓存不触发抓取 |
| 短期遥测与探测队列（`node_latency_samples`、`node_metric_samples`、定时探测队列） | 不进便携备份（快照清空再 VACUUM，恢复候选清空），手动排队探测过期；`node_latency_targets` 与自定义开关属配置，进程内恢复保留当前自定义 |

# 32. 开发流程

新增能力通过既有模块接口扩展，不修改已应用迁移（含 `0152_baseline.sql`）。

日常增量检查（`scripts/quick-check.sh`）、发布前快速检查（`--release`）、完整 Linux 门禁与 GitHub `check` 并行校验、纯前端补丁作用域判断及打 tag 发布流程，统一遵循[发布手册](release-runbook.md)。

# 33. 测试要求

## 33.1 单元测试

重点覆盖：用户节点组展开、订阅过滤、域名匹配、策略优先级、代理链环路与最大跳数检查、协议与逐核设置能力判断、配置编译、访问记录聚合与数据保留清理。

## 33.2 集成测试

至少覆盖：`SOCKS5 → Direct`、`VLESS → Direct`、`VLESS → Trojan → Node B`、`Shadowsocks → SOCKS5 → Node B`、`Netflix → Node B`、`TikTok → Block`、`A → B → C`。

## 33.3 故障测试

必须验证：中央端离线、Agent 离线、下一跳节点离线、错误配置、端口占用、代理核心启动失败、本地缓存写满、节点证书过期、SQLite 锁等待与备份恢复。

# 34. 验收标准

验收须覆盖以下 25 条（当前实测证据与限制见[v1.7.5 验证记录](v1.7.5-verification.md)）：

1. 一个中央面板管理至少三个节点。
2. 节点通过 Agent 主动连接中央端。
3. 节点使用独立证书进行 mTLS 认证（Xray 用官方二进制；sing-box 用官方或 qui 未修改上游构建，缺能力明确降级）。
4. 中央端为 Go 单体服务与 SQLite。
5. 前端静态资源嵌入中央程序。
6. 支持创建 Owner 与 Viewer。
7. 支持创建代理用户，维护共享订阅、凭据、独立门户、软执行额度（第 9.1 节）与源地址设备限制（第 9.2 节；有 `singbox_user_stats` 计入用户额度，官方无标签维持未知与节点总量）。
8. 用户可绑定一个或多个节点组。
9. 用户订阅按授权节点组自动生成。
10. 新接入加入节点组后用户无须重新授权。
11. 中转节点可隐藏于用户订阅之外。
12. 单节点支持同时运行多个入站。
13. 支持协议 A 入站、协议 B 出站。
14. 支持 Node A 经另一协议中转至 Node B。
15. 支持 `A → B` 与 `A → B → C` 代理链。
16. 支持 Direct、Block、指定 Outbound、出口组（按上游能力，权重已移除，固定出口失败不隐式 Direct）与代理链。
17. 支持按服务分类（如 Netflix）分流至指定节点。
18. 支持网站域名阻断。
19. 可查看访问过的域名（每域名字节可空显示「—」，出口为「配置出口」）。
20. 不记录网页正文、密码或聊天内容。
21. 配置发布前执行校验。
22. 配置失败自动回滚。
23. 中央端离线后节点继续运行。
24. 节点访问记录与流量支持断线缓存和恢复补传。
25. 支持备份并恢复中央数据库。

已知边界持续成立：L8（部分协议 UDP 不支持设备限制）、L9（已清理历史样本不可还原完整月）、L10（TCP 探测不证明 UDP/QUIC 可用，转发无用户身份）；不得以本地单测替代实机或浏览器证据。

# 35. 明确不做

微服务；Kubernetes；Redis；Kafka；Elasticsearch；ClickHouse；高可用中央控制集群；商业套餐；支付；订单；工单；邀请返利；多租户；经销商；动态插件市场；无限多跳代理链；完整数据包记录；网页内容审计；HTTPS 中间人解密；Web Shell；任意远程命令执行；每条连接实时同步写数据库；大型日志分析平台。

# 36. 扩展边界

完整系统 TUN、Passkey、OIDC、更细管理员权限、灰度升级及更大规模存储/多中央等仅为可能方向，不是交付承诺。新增能力须通过现有模块接口扩展并保持第 3、35 节架构与安全边界；历史演进见[变更记录](changelog.md)。

# 37. 项目运行流程

用户取得按授权节点组生成的订阅 → 选择入口并以协议 A 接入 → 节点识别用户与域名、执行本地策略 → Block、Direct、协议 B 中转或 `A→B→C` 链 → 节点聚合域名/动作/配置出口记录并批量上报。中央与节点职责见第 4、23 节；中央失联按第 3.5 节继续运行。

# 38. 项目总结

以统一数据模型、Go 模块化单体、声明式节点发布与可替换核心适配层，完成少量内部用户的多节点、多协议访问管理，保持轻量部署和清晰的安全边界。
