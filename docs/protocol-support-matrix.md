# 协议与能力支持表

本表描述当前源码支持与未修改的官方上游能力（Xray 26.x 官方核、sing-box 1.14.x 默认 qui 构建及官方核、GOST 3.3.2），不替代运行证据。真实主机验收范围为 `linux/amd64`；`arm64` 仅做交叉构建或 qemu-user 模拟，未完成原生实机验收，不外推到未测客户端。

fork 专有维度（`egress_proof`、`inbound_bytes`）已删除，不使用补丁核；账户统计与门控的新能力按上报契约单独协商。

## 1. 依据与用法

本表由同一能力依据派生，避免网页选项、API 校验和服务端校验互相漂移：

- `pkg/contracts/desiredstate/support.go`：冻结的协议能力表，提供 `ProtocolSupportTable()`、`SupportsInbound`、`SupportsOutbound`、`SupportedInboundProtocols`、`SupportedOutboundProtocols`，以及每核行 `Cores`（`CoreSupportFor`、`CoreSupportsInbound`、`CoreSupportsOutbound`）。
- `desiredstate.State.Validate`（`state.go`）、`proxy.ValidateInbound`/`ValidateOutbound`（`internal/panel/proxy/model.go`）、`proxy.Service.SaveInbound`/`SaveOutbound`（`internal/panel/proxy/service.go`）调用该表：协议方向按 `SupportsInbound`/`SupportsOutbound` 判定；节点已上报核心名时再按 `CoreSupportsInbound`/`CoreSupportsOutbound` 拒绝该核不支持的方向。
- `GET /api/v1/protocol-support`（`internal/panel/proxy/handler_http.go`，Owner 与 Viewer 均可读）返回同一张表；前端 `web/src/modules/proxy/api.ts` 的 `loadProtocolSupport()` 读取后替换入站/出站协议下拉选项，失败时退回与表完全一致的静态清单。

API 保留以下兼容状态字符串，表示源码能力，不单独等同运行通过：

| 标记 | 含义 |
| --- | --- |
| `implemented_pending_acceptance` | 源码已实现；这是保留的 API 值，不表示当前发布尚未验收，也不单独证明运行通过 |
| `pending` | 全局行尚未声明；Reality/xhttp 的可用性另由逐核心设置契约判定 |
| `not_applicable` | 协议定义之外，不构成缺口 |

节点侧实际可用能力仍以 `pkg/contracts/agentpb` 的 `ProtocolCapability`（`protocol/inbound/outbound/tcp/udp`）为准：Agent 按官方核的 `version` 输出与运行时探测上报（Xray `Detect`；sing-box `version` 的 `Tags:`），节点未报告的能力在发布、订阅、出口组和代理链校验处拒绝，不静默忽略。

## 2. 每核协议支持表

| 协议 | 方向 | 传输 | 安全 | Xray 26.x | sing-box 1.14.x | 依据 |
| --- | --- | --- | --- | --- | --- | --- |
| http | 入/出 | tcp | none | 入/出 tcp | 入/出 tcp | 两核 detect `http udp=false`；`proxy/model.go` 拒绝 HTTP+UDP |
| socks5 | 入/出 | tcp+udp | none | 入/出 tcp+udp | 入/出 tcp+udp | 两核 detect `socks5 udp=true`；`ValidateSettings` |
| shadowsocks | 入/出 | tcp+udp | none | 入/出 tcp+udp | 入/出 tcp+udp | 两核 detect `shadowsocks udp=true`；加密白名单 |
| vless | 入/出 | tcp+udp | none/tls | 入/出 tcp+udp | 入/出 tcp+udp | 两核渲染 VLESS；真实核测试覆盖 ws/grpc 传输 |
| trojan | 入/出 | tcp+udp | tls | 入/出 tcp+udp | 入/出 tcp+udp | `ValidateSettings` 强制 TLS；两核渲染 ws/grpc |
| wireguard | 入/出 | tcp+udp | none | 入（major>=25）/出 tcp+udp | 仅出 tcp+udp | sing-box 官方核没有按 peer 归属用户（D4）；Xray 26.x 入站可用 |
| hysteria2 | 入/出 | udp | tls/quic | — | 入/出 tcp+udp | 仅 sing-box，且需 `with_quic`（默认 qui 1.14.4-qui.1 与官方 1.14.3 均含该 tag）；`ValidateSettings` 原生 QUIC |
| tuic | 入/出 | udp | tls/quic | — | 入/出 tcp+udp | 同上，身份为 UUID |
| direct | 出 | tcp+udp | none | 出 tcp+udp | 出 tcp+udp | `ValidateSettings` 拒绝 direct/block 的传输层设置 |
| block | 出 | tcp+udp | none | 出 tcp+udp | 出 tcp+udp | 同上 |

说明：

- hysteria2/tuic 保持：qui 1.14.4-qui.1 与官方 sing-box 1.14.3 的 `Tags:` 均含 `with_quic`，`internal/agent/core/singbox` 的渲染与真实核测试（`quic_test.go`、`internal/platform/server/quic_agent_test.go`）均覆盖；Xray 不支持这两个协议。
- WireGuard：官方 sing-box 没有入站（`internal/agent/core/singbox/adapter.go` 只上报 `Inbound:false, Outbound:true`），面板按每核表拒绝给 sing-box 节点保存 WireGuard 入站；Xray 入站需要 major>=25。
- HTTP、SOCKS5、Shadowsocks、VLESS、Trojan、WireGuard 为原承诺基础协议，已接入的 Hysteria2/TUIC 保持；本表不静默减少任何已支持协议。
- **入站出口 IP**：仅限上报 `inbound_egress_source_supported` 的 Xray/sing-box，GOST 拒绝。适用分流、默认出口、出口组与代理链均固定所选地址并清除网卡绑定；Block 不变。域名只解析所选地址族，无该族地址、另一族 IP 字面量和回环目标连接失败。空值沿用原行为，发布后生效。各出口 IP 在组成员筛选前分别探测、选路，保留原有策略；未知健康不借用默认来源结果。面板仅接收默认来源出站健康；动态组从对应出口 IP 探测成员（Xray observatory / sing-box `urltest`）。
- **节点出站源地址（节点级，与协议无关）**：Xray 在每个拨号出站上渲染 `sendThrough` + `streamSettings.sockopt.interface`，地址族用 `freedom` 的 `settings.domainStrategy`、其他协议用 `sockopt.domainStrategy` 固定（`ForceIPv4`/`ForceIPv6`）；sing-box 渲染 `inet4/inet6_bind_address` + `bind_interface` 与限族的 `domain_resolver`（`ipv4_only`/`ipv6_only`）。`blackhole`/`block` 出站、sing-box 的 `selector`/`urltest` 组不渲染（组由上游在运行时选成员），sing-box 只固定出站 WireGuard 端点、入站端点仅监听。目标为另一族的字面量 IP 时连接失败。整机服务器强制出站是内核 SNAT，与协议无关，见[运维手册](operations.md)第 9 节。

### 自建 VLESS Reality / xhttp（已发布，验收按具体组合记录）

| 用户入站组合 | Xray | sing-box |
| --- | --- | --- |
| Reality + TCP，无 flow / Vision | 已实现 | 已实现；单个具体 DNS SNI |
| Reality + gRPC，无 flow | 已实现 | 拒绝 |
| Reality / TLS + xhttp，auto / packet-up / stream-up / stream-one | 已实现 | 拒绝（固定版无 xhttp） |
| Vision + xhttp / gRPC | 拒绝 | 拒绝 |

设置契约按核心、方向、安全、传输、flow 精确上报；已安装核心的逐组合原生探测通过才声明支持。旧/未知契约、其他核心或未列组合不能授权，保存/快照/发布与投递均复核，本次连接须接受新的能力报告。通用 VLESS 标志或版本文本不能代替。全局协议行的 pending 是保留的 API 状态值，实际支持由逐核心独立契约授权；各客户端导出格式边界见第 7 节。

Reality 的 X25519 私钥只进入加密设置/修订与安全 Agent 配置，普通查询、预览与差异隐藏。普通编辑保持密钥，明确轮换先保存草稿，成功发布后客户端更新订阅；历史修订与回滚保持对应密钥。公开模型只映射成功修订的公钥、Short ID、具体 SNI 与客户端指纹；证书指纹是另一字段。内部代理中转创建继续使用 TCP/TLS。

### 出站客户端字段与导入节点出口（v1.6.0）

出站可带 ALPN、uTLS 客户端指纹、WebSocket Host、Reality 客户端半边与 xhttp，用于服务器方案以导入节点为最后一跳出口（说明书第 13.2 节）。这些字段只在核心上报对应 `direction=outbound` 组合时可用：

| 出站组合 | Xray | sing-box |
| --- | --- | --- |
| VLESS 无加密 + ws（Host） | 已实现 | 已实现 |
| VLESS TLS + tcp／Vision／ws／grpc（ALPN、指纹） | 已实现 | 已实现 |
| VLESS TLS + xhttp | 已实现 | 拒绝 |
| VLESS Reality + tcp／Vision | 已实现 | 已实现（不输出 SpiderX） |
| VLESS Reality + grpc／xhttp | 已实现 | 拒绝 |
| Trojan TLS + tcp／ws／grpc（ALPN、指纹、Host） | 已实现 | 已实现 |

Linux 真实核测试以面板渲染的出站连接自建 Reality（tcp／Vision／grpc／xhttp）与 TLS xhttp 监听并拒绝错误公钥（Xray 26.3.27、sing-box 1.14.3 Reality tcp／Vision）。不转换的导入类型：Shadowsocks 2022、证书指纹固定、跳过证书校验、Hysteria2 混淆、WireGuard 与其他传输。

### GOST（v1.3 G1）

`pkg/contracts/desiredstate/support.go` 只在 `forward` 行里加入 `gost` 条目；其他每一行都不含 `gost` 键，因此 `proxy.ValidateCoreProtocol` 对已知核心遇到没有该键的行直接拒绝。「GOST 仅承载转发」由这张共享表加 `desiredstate.State.Validate` 一处判定，不另加重复的 SQL 协议规则；Web 入站核心选择器同样只为 `forward` 提供 GOST，并要求 `Capabilities.cores` 已确认该核心启用。

| 协议 | 方向 | 传输 | 安全 | GOST 3.3.2 | 依据 |
| --- | --- | --- | --- | --- | --- |
| forward（GOST） | 入站 | tcp+udp | none | `implemented_pending_acceptance` | 官方 GOST `tcp`/`udp` 转发服务直连目标；没有用户身份、账户计量、额度、设备门控、订阅、源地址观测或域名记录，也没有出站、分流与出口组（唯一要求是启用中的 `direct` 默认出站） |

固定版本、安装方式与运行健康含义见[部署与升级](deployment.md)与[运维手册](operations.md)，契约与凭据规则见[项目说明书](project-spec.md) §12、§14、§28–29，G0 探测依据见[GOST 探测记录](archive/v1.3-g0-gost-findings.md)。

### GOST 转发的池、范围与限流（v1.3 G2）

`ForwardSettings` 的目标池、`strategy`/`max_fails`/`fail_timeout`、端口范围 `port_end`、双向字节限速与 TCP 连接上限属于 GOST 设置契约**版本 2**（`CoreCapability.gost_contract_version >= 2`，v1.4 起 Agent 上报 4，v1.3.1 为 3）。它们按“是否出现”判定：Xray 与 sing-box 转发携带任一字段时，`desiredstate.ValidateForwardCore`、面板保存/发布（`proxy.ValidateInboundForwardOptions`）与 Agent 解析主核心后的校验三处一致拒绝，不会静默丢弃；`State.Validate` 只对显式指定核心的入站判定，留空跟随主核心的入站在 `MultiRuntime` 解析后再判。

| 能力 | GOST 3.3.2 | 依据与边界 |
| --- | --- | --- |
| 目标池 `round`（轮询） | 支持 | `forwarder.nodes[]` + `selector.strategy=round`；每连接轮换，单个坏目标在该失败窗口内先失败一次 |
| 目标池 `fifo`（主备） | 支持 | `selector={strategy:fifo,maxFails,failTimeout}`；停在一个目标上，失败后切换，切换代价是该窗口内一次失败请求 |
| 端口范围 | 由 qui 展开 | GOST 配置的 `addr` **不支持范围**（`:20000-20003` 报 `unknown port`）；Agent 渲染为每个端口一个服务，每服务带完整池 |
| 字节限速（入/出） | 支持 | `limiters[{name,limits:["$ <in> <out>"]}]` + `service.limiter`；上游用 `ParseBase2Bytes` 解析，裸整数被拒绝，故写 `<整数字节>B`，`0` 表示该方向不限 |
| TCP 连接上限 | 支持 | `climiters[{name,limits:["$ <n>"]}]` + `service.climiter`；超限连接被接受后关闭；G0 未测量 UDP 并发，v1.3 拒绝 UDP-only 转发设置该字段 |
| UDP 限速 | 支持（待实机） | `tcp,udp` 转发的 UDP 服务同样渲染 limiter，真实 UDP 执行需实机门禁确认；UDP 目标失效后的故障切换不承诺（G0 未确认缺失 UDP 响应会产生拨号错误，`fifo` 的切换实测只在 TCP 上验证） |
| 每转发字节 | 节点总量 | 只进入 `gost` 节点计数（[项目说明书 §21](project-spec.md#21-访问记录轻量化处理)），没有用户额度、设备门控或域名归属，也不提供每转发的历史表/接口 |

### GOST 隧道与规则（v1.4）

隧道管理提供 GOST TLS＋mTLS 的 entry → [mid] → exit（最多三台不同服务器）；两节点各跳契约 ≥3，三节点各跳 ≥4。每接收跳一个共享监听，中间监听只有一个下一跳，分段材料独立密封。不选隧道的规则是直接转发，有 GOST 优先，否则主 Xray/sing-box。GOST 固定 3.3.2、仅 linux/amd64，能力从 Agent 上报契约判断。规则只支持单目标/单端口；目标池/范围/限流仍在普通 GOST forward 入站中配置。

画布只新增 proxy_chain/tunnel 的 tls/wireguard 代理中转，出口可为 qui 入站（独立内部凭据）或前一跳上的外部 SOCKS5/HTTP/Shadowsocks/Trojan/VLESS 出站，按 TCP/UDP 能力过滤；旧 port_forward/gost 连接只读至迁移。仅画布无范围时保留 8443/51820，新隧道/规则无默认端口回退。

| 能力 | GOST 3.3.2 | 依据与边界 |
| --- | --- | --- |
| 中继认证（mTLS） | 支持 | `handler: relay` + `listener: {type: tls, tls: {certFile,keyFile,caFile}}`，`caFile` 让客户端证书成为必需；监听类型 `mtls` 是**多路复用** TLS，不用于双向认证 |
| 入口链 | 支持 | 链挂在 `handler.chain`（不是 service 级）：`connector: relay` + `dialer: {type: tls, tls: {certFile,keyFile,caFile,serverName}}`；未知链名会被静默直连，所以 Agent 渲染后自检引用 |
| 中间 relay | 契约 4 | 监听认证前段，`handler.chain` 拨下一段；旧契约 3 不接受中间设置；三节点与恢复验收已于 v1.4 完成 |
| 客户端证书要求 | 强制 | 无证书或异构 CA 的客户端被拒；G0 §6.3 实测表现为“接受后空回复”，不是拒绝握手 |
| TCP/UDP 经中继 | 已实现 | UDP 经 TLS/TCP 到出口再发 UDP，可能队头阻塞；两节点与三节点主机及恢复验收已于 v1.4 完成 |
| 凭据轮换 | 有损 | 一次普通发布先出口后入口，两次部署之间隧道短暂中断；删除隧道不撤销离线节点上的旧凭据 |
| 计量 | 节点总量 | 中继服务不计入节点总量（只重复搬运已计字节），计入的是入口节点的转发服务；仍无用户额度、设备门控与域名归属 |

### 上游核心已知边界（S0 实测）

- **SS2022 SIP002 URI 导出**：必须输出百分号编码的明文 `method:password`（标准带填充 Base64 PSK，严格 16B / 32B）；Base64URL userinfo 仅作输入兼容，禁止用于输出。
- **SS2022 ChaCha20 多用户**：`2022-blake3-chacha20-poly1305` 的多用户身份（`serverPSK:userPSK`）会被 sing-box 服务端以 `invalid argument` 拒绝，单密钥可用。
- **Xray `run -test` 假通过**：`xray run -test` 会错误放行 `Vision + gRPC` 与 `Vision + xhttp`，随后在客户端 VLESS 出站运行时报错（`XTLS only supports TLS and REALITY directly for now`）；面板必须在发布前拒绝。
- **Xray xhttp `extra` 静默接受**：Xray xhttp 解析器静默接受未知 `extra` 键而不证明运行时生效；面板仅允许受控的类型化子集。
- **S0 探针实测注意事项**：测试框架中 mihomo Reality 单元首次尝试失败、重试后成功（原因未定）；Xray xhttp `stream-up` 首字节约 6 秒且断开时出现 `unexpected EOF`；H3、`xmux` 与 sing-box gRPC 仍未实测。

## 3. 平台能力维度

表内每行的 `implemented` / `pending` / `not_applicable` 描述协议本身携带的平台能力：

| 维度 | 含义 |
| --- | --- |
| `user_identity` | 入站可识别用户/设备身份 |
| `account_metering` | 可计量账户流量（Xray 与有标签/能力的 sing-box 每用户，无标签官方 sing-box 仅节点总量） |
| `quota` | 参与用户额度计算（面板软执行，D3） |
| `concurrency` | v1.1 源地址估计与运行时设备门控，按协商能力执行，见第 7 节 |
| `subscription` | 可进入用户订阅 |
| `udp` / `ipv6` / `tls` / `quic` / `transport_ws` / `transport_grpc` | 协议/传输族能力 |
| `security_reality` / `transport_xhttp` | 自建 VLESS 的 Reality 与 xhttp 独立设置契约；全局行保留 pending，实际选择与发布依赖已安装核心原生探测后的精确组合报告 |

fork 专有维度 `egress_proof` 与 `inbound_bytes` 无法由官方核提供，其契约、观测读侧与相关页面均已移除。

## 4. 官方上游无法提供的部分（D4/D5）

- **Xray 每用户统计**：`StatsService` 的 `QueryStats`（`user>>>` 前缀，`reset:false` 读累计值、按游标差分）和在线 IP RPC；回环排除，源地址不是 TCP 会话数。
- **sing-box 按能力区分**：有 `with_v2ray_api` 且 Agent 上报 `singbox_user_stats` 时读取账户累计字节并计入额度；无标签官方核仍用 Clash API 节点总量，不能编造用户用量。在线设备来自日志用户与 Clash 源地址联接，不能由节点总量推导。
- **出口组**：Xray 的主备/延迟优先由 Agent 选择（核内没有“首健康”策略），轮询渲染 `routing.balancers` 的 `roundRobin` + `observatory` + `fallbackTag`；sing-box 的轮询与延迟优先都渲染 `urltest`，主备由 Agent 选择。权重策略已移除（迁移 `0104_egress_weighted_to_round_robin.sql` 把旧组映射为 `round_robin`）。
- **每域名精确字节**：不可得；访问记录字节列为空，UI 显示“—”。
- **跨中转的实际出口节点**：只记录策略在该节点解析出的**配置出口**，标注“configured egress”。
- **逐目标 UDP 路由**：按上游行为。
- **SOCKS5 作用域 UDP 策略无法匹配身份**：sing-box 渲染直接拒绝该组合（`internal/agent/core/singbox/render.go` 的 `sing-box scoped SOCKS5 UDP identity requires native validation before publication`）。
- **WireGuard 每 peer 用户归属**：sing-box 官方核没有入站；能归属到用户的入站按上游能力提供，无法归属的入站不报每用户流量，UI 明示。

## 5. 验收范围与剩余边界

不能把“源码已实现”直接等同所有环境通过。完整系统 TUN 不在当前交付范围。GOST UDP 限速与 UDP 目标失效切换未完成专项主机实测，保留上表限制；TCP 双向限速、连接上限、IPv6 与中继 UDP 已有主机实测。

## 6. 核心版本与更新方式

| 核心 | 基准版本 | 来源与说明 |
| --- | --- | --- |
| Xray | `26.3.27` | 官方发布（静态二进制，不分 libc） |
| sing-box | `1.14.4-qui.1`（默认） / `1.14.3`（官方） | 默认同发布 qui 未修改上游静态构建（含 `with_v2ray_api`，架构内 glibc/musl 共用）；显式 `--core-source official` 使用官方归档（Alpine 用 `-musl`） |
| GOST | `3.3.2`（固定） | 官方发布，仅 `linux/amd64`，仅转发与中继 |

- 节点通过 `cores: [{name,path}]` 配置多核心；Agent 运行时按 `version` 输出与接口探测真实能力，缺失能力上报为不可用而非拒绝运行（D1）。两核原生 `forward` 支持 TCP/UDP，须声明独立能力。
- 核心安装、升级（`CoreUpdateRequest`，按 URL + SHA-256 校验并在健康失败时回滚）及离线归档细节见[部署与升级](deployment.md)。

## 7. 订阅格式、账户计数与门控

| 协议/功能 | URI / Base64 | sing-box 片段 | mihomo（clash） | legacy Clash |
| --- | --- | --- | --- | --- |
| HTTP、SOCKS5、Shadowsocks、Trojan | 支持 | 支持 | 支持 | 支持 |
| VLESS | 支持 | 支持 | 支持 | 省略并警告 |
| 导入 SS2022 | 支持；规范明文百分号编码 userinfo | AES-128/256 单钥或双段身份；不输出 chacha | AES-256 单钥/双段、AES-128 单钥、chacha 单钥；省略 AES-128 双段 | 省略并警告 |
| VLESS Reality TCP | 支持，无 flow / Vision | 仅 Vision | 无 flow / Vision | 省略并警告 |
| VLESS Reality gRPC，无 flow | 支持 | 省略并警告 | 支持 | 省略并警告 |
| VLESS xhttp 四种模式 | 支持；空或 TCP ALPN，仅 h2/http/1.1 | 省略并警告 | TLS / Reality，auto / packet-up / stream-up / stream-one；ALPN 为空、[h2]、[http/1.1] 或 [h2,http/1.1] 均原样输出（v1.5.9 起；mihomo 恰为 [http/1.1] 时用 HTTP/1.1，其余 h2） | 省略并警告 |
| Hysteria2、TUIC | 支持；固定 CA 时省略 | 支持，含固定证书 | 支持，固定证书转指纹 | 省略并警告 |
| WireGuard | 支持既有导出 | 支持既有导出 | 支持 | 省略并警告 |
| 全局规则模板与外部来源 | 不适用 | 不适用（不是完整配置） | 支持规则、内置服务集及外部 provider（文本/YAML/MRS 格式，追加 RULE-SET 行） | 受限，无 GEOSITE/RULE-SET/provider |
| 客户端链 | 省略依赖链并警告 | 省略依赖链并警告 | `dialer-proxy` | 省略依赖链并警告 |
| forward 入站 | 不进入订阅 | 不进入订阅 | 不进入订阅 | 不进入订阅 |

格式别名、预览警告与显式 `?format=` 优先规则见[项目说明书 §10.3](project-spec.md#103-订阅过滤)；v1.5.4 已内置客户端 UA 匹配表，但尚未逐一完成真机验证。每格式按自身能力过滤，不把不支持协议改写为另一协议。

上述 Reality/xhttp 行表示当前导出边界：SpiderX 不再导致节点省略，mihomo/sing-box 不输出它，URI/Base64 保留 `spx`。安全／传输省略警告按原因列节点名称，每组最多 10 个，超出汇总数量；链依赖与空组策略保持原有处理。TLS xhttp 的真实客户端连通验证仍 **待核实**。

自签名／固定 CA 的 TLS VLESS、Trojan、HTTP、SOCKS5 与 Hysteria2/TUIC 一样：mihomo 将第一张 PEM 证书 DER 的 SHA-256 写入 `fingerprint`，sing-box 写 `tls.certificate`，URI/Base64 省略端点并给预览警告。

外部导入新增的参数按输入格式区分：

| 参数 | URI | Clash YAML |
| --- | --- | --- |
| WebSocket Host | 支持 Host 映射 | 仅 `ws-opts.headers.Host/host`，两者不同值拒绝 |
| TUIC | `congestion_control`、`udp_relay_mode`、`alpn` | congestion-controller/udp-relay-mode 尚未接入，仍拒绝 |
| Hysteria2 跳跃与带宽 | `mport`，单段 start-end | `ports`、`hop-interval`、`up/down`；带宽为正整数或整数加 Mbps |
| Hysteria2 混淆 | `obfs=salamander` 与密码 | `obfs: salamander` 与 `obfs-password` |
| 可忽略参数 | `headerType=none`、insecure 别名值 0、TUIC `disable_sni=0`、`ed` | `tfo`、`ip-version`、`packet-encoding`；`udp-over-tcp:true` 仍拒绝 |

其他未知字段仍拒绝，不保存原配置透传。QUIC 输出 ALPN 固定 h3，不承诺按导入 ALPN 原样输出；URI 导出的 upmbps/downmbps 尚不能原样回导。混淆密码随模型加密保存，刷新诊断不回显，Owner 正文预览与链接包含客户端所需密码。

Hysteria2 端口跳跃（v1.5.0 §7）在自建入站设置了跳跃段时输出：mihomo（Clash Meta）写 `ports: "<start>-<end>"` 与 `hop-interval: <秒>`；sing-box 写 `server_ports: ["<start>:<end>"]` 与 `hop_interval: "<秒>s"`；URI 查询参数写 `mport=<start>-<end>`；legacy Clash 不输出 Hysteria2。自建入站间隔默认 30 秒；外部导入 mport 未给间隔时保留 0，mihomo 不写间隔、sing-box 写 0s，其跨客户端可用性 **待核实**。固定 CA 的跳跃端点仍不进 URI/Base64，sing-box 可输出。

linked 中转端点来自规则成功发布快照，且须启用/对用户可见、规则本身在用户的授权分组里（`node_group_relays`，不要求目标入站也在），并通过目标入站凭据检查；换入口地址（入口节点当前 `connectHost`，否则 `publicIp`）/规则端口、补显式原 TLS server name，并清空目标 QUIC 跳跃端口与间隔，只连接规则端口，其他协议字段保留，直接端点不变。命名取规则当前名称，重名按端口消歧；名称与入口地址改动不需重新发布。Hysteria2/TUIC/WireGuard 按 UDP，其余按 TCP 与规则 network 过滤；各格式继续执行上表省略规则。manual 不进订阅，草稿/needs_publish 不推进快照，pending_delete 保留快照直至移除成功。Reality/xhttp 中转保留目标公开参数与原 SNI；xhttp 限 TCP，并继续执行各格式省略边界。Owner shareLink 可含合格用户真实凭据，Viewer 恒 null。

xhttp 前置 CDN 时节点看到边缘地址，源地址设备估计可能合并或分散客户端。目标看到中继源地址，设备限制把同一中继后的客户端合为一个来源；账户额度仍在目标计量，规则转发只计节点总量，中间/出口 relay 服务排除。

| 核心/能力 | 每用户字节与额度 | 在线设备估计/门控 |
| --- | --- | --- |
| 官方 Xray | 累计 StatsService 计数，进入统一额度 | 在线 IP；运行时规则阻止新连接，既有连接保留 |
| qui sing-box，`with_v2ray_api` 且 Agent 声明 `singbox_user_stats` | 私有 loopback v2ray API 累计账户计数，游标/spool 去重，进入统一额度 | 用户日志与 Clash 源 IP/端口联接；周期关闭匹配连接，另需观测/门控能力协商 |
| 官方 sing-box 无用户计数标签 | 仍仅节点总量，每用户未知，不计入额度 | 单独按观测能力判断；缺能力明确部分执行，不能从字节标签推断门控支持 |
| 旧 Agent | 保持兼容路径，无新增 sing-box 用户统计 | 不发送门控，部分执行 |
| WireGuard | 按原核心可归属能力；共享密钥一次一端 | 不计入限制 |
| 原生 forward | 无用户身份，不计用户额度 | 不计入限制，无用户访问归属 |

设备限制是 IPv4 / IPv6 /64 跨入口去重估计，NAT/双栈/切网有误差，TTL 过期放行。Xray SOCKS5 UDP 无用户归属不能门控；Xray 无 Hysteria2/TUIC 入站。sing-box 的 SOCKS5、Shadowsocks、Hysteria2、TUIC UDP 均不支持设备限制，TCP 仍受限制；完整实测范围见[设备限制](device-limit.md)。每域名字节仍可空，账户统计不改变 D4。
