# 协议与能力支持表（W12）

更新：2026-10-17（Phase 14 复核，原表 Phase 6.5）。性质：从当前源码与官方上游核提取的**结构化支持表**，不是验收报告，不含运行证据。本轮只做源码、必要编译和文档；未运行测试、核心、节点、浏览器或真实迁移。

本表只描述**官方上游核**（计划 D1）：未修改的 Xray 26.x 与 sing-box 1.14.x 官方发布二进制。fork 专有维度（`egress_proof`、`inbound_bytes`）已随 Phase 6.4 删除，受控核版本号与 `proxycontrol_*` 能力标记不再存在。

## 1. 依据与用法

本表由同一能力依据派生，避免网页选项、API 校验和服务端校验互相漂移：

- `pkg/contracts/desiredstate/support.go`：冻结的协议能力表，提供 `ProtocolSupportTable()`、`SupportsInbound`、`SupportsOutbound`、`SupportedInboundProtocols`、`SupportedOutboundProtocols`，以及每核行 `Cores`（`CoreSupportFor`、`CoreSupportsInbound`、`CoreSupportsOutbound`）。
- `desiredstate.State.Validate`（`state.go`）、`proxy.ValidateInbound`/`ValidateOutbound`（`internal/panel/proxy/model.go`）、`proxy.Service.SaveInbound`/`SaveOutbound`（`internal/panel/proxy/service.go`）调用该表：协议方向按 `SupportsInbound`/`SupportsOutbound` 判定；节点已上报核心名时再按 `CoreSupportsInbound`/`CoreSupportsOutbound` 拒绝该核不支持的方向。
- `GET /api/v1/protocol-support`（`internal/panel/proxy/handler_http.go`，Owner 与 Viewer 均可读）返回同一张表；前端 `web/src/modules/proxy/api.ts` 的 `loadProtocolSupport()` 读取后替换入站/出站协议下拉选项，失败时退回与表完全一致的静态清单。

状态标记沿用计划口径：

| 标记 | 含义 |
| --- | --- |
| `implemented_pending_acceptance` | 源码已实现，待本版运行验收；不等于运行通过 |
| `pending` | 尚未实现 |
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
| hysteria2 | 入/出 | udp | tls/quic | — | 入/出 tcp+udp | 仅 sing-box，且需 `with_quic`（官方 1.14.3 含该 tag）；`ValidateSettings` 原生 QUIC |
| tuic | 入/出 | udp | tls/quic | — | 入/出 tcp+udp | 同上，身份为 UUID |
| direct | 出 | tcp+udp | none | 出 tcp+udp | 出 tcp+udp | `ValidateSettings` 拒绝 direct/block 的传输层设置 |
| block | 出 | tcp+udp | none | 出 tcp+udp | 出 tcp+udp | 同上 |

说明：

- hysteria2/tuic 保持：官方 sing-box 1.14.3 的 `Tags:` 含 `with_quic`，`internal/agent/core/singbox` 的渲染与真实核测试（`quic_test.go`、`internal/platform/server/quic_agent_test.go`）均覆盖；Xray 不支持这两个协议。
- WireGuard：官方 sing-box 没有入站（`internal/agent/core/singbox/adapter.go` 只上报 `Inbound:false, Outbound:true`），面板按每核表拒绝给 sing-box 节点保存 WireGuard 入站；Xray 入站需要 major>=25。
- HTTP、SOCKS5、Shadowsocks、VLESS、Trojan、WireGuard 为原承诺基础协议，已接入的 Hysteria2/TUIC 保持；本表不静默减少任何已支持协议。
- **节点出站源地址（节点级，与协议无关）**：Xray 在每个拨号出站上渲染 `sendThrough` + `streamSettings.sockopt.interface`，地址族用 `freedom` 的 `settings.domainStrategy`、其他协议用 `sockopt.domainStrategy` 固定（`ForceIPv4`/`ForceIPv6`）；sing-box 渲染 `inet4/inet6_bind_address` + `bind_interface` 与限族的 `domain_resolver`（`ipv4_only`/`ipv6_only`）。`blackhole`/`block` 出站、sing-box 的 `selector`/`urltest` 组不渲染（组由上游在运行时选成员），sing-box 只固定出站 WireGuard 端点、入站端点仅监听。目标为另一族的字面量 IP 时连接失败。整机服务器强制出站是内核 SNAT，与协议无关，见[运维手册](operations.md)第 9 节。


## 3. 平台能力维度

表内每行的 `implemented` / `pending` / `not_applicable` 描述协议本身携带的平台能力：

| 维度 | 含义 |
| --- | --- |
| `user_identity` | 入站可识别用户/设备身份 |
| `account_metering` | 可计量账户流量（Xray 每用户；sing-box 仅节点总量，见第 4 节） |
| `quota` | 参与用户额度计算（面板软执行，D3） |
| `concurrency` | 并发/在线数为观测软限制（D2，面板只告警不断开） |
| `subscription` | 可进入用户订阅 |
| `udp` / `ipv6` / `tls` / `quic` / `transport_ws` / `transport_grpc` | 协议/传输族能力 |

已删除的 fork 维度：`egress_proof`（跨节点认证出口证明）与 `inbound_bytes`（入口逐连接 payload 字节）。两者只由受控核上报，官方核无法提供；Phase 7.3 已删除 `pkg/contracts/observation` 的关联/观测契约、面板的观测读侧与 `GET /access-records/egress-proofs` 页面（迁移 `0105_drop_fork_observation.sql`）。代码中剩余的 `observation` 字样属于无关命名（存储观测 `internal/panel/access/storage_observation.go`、账户流量采样）；`pkg/contracts/agentpb` 里仍留有 fork 时代的死字段，因为本机没有 `protoc`，它们既不被设置也不被读取。

## 4. 官方上游无法提供的部分（D4/D5）

- **每用户流量与在线数只有 Xray**：`StatsService` 的 `QueryStats`（`user>>>` 前缀，`reset:false` 读累计值，面板按游标差分）与 `GetStatsOnline`；在线数为去重客户端 IP 数，回环地址排除（`internal/agent/core/xray/online.go`）。
- **sing-box 节点只报节点总量**：Clash API `/connections` 的 `uploadTotal` / `downloadTotal` 累计值差分（核心重启时基线归零，不重复计）。`/connections` 元数据不含用户，因此 sing-box 的每用户流量与在线数为 unknown（D5）；每用户额度只统计 Xray 节点上看到的流量。
- **出口组**：Xray 的主备/延迟优先由 Agent 选择（核内没有“首健康”策略），轮询渲染 `routing.balancers` 的 `roundRobin` + `observatory` + `fallbackTag`；sing-box 的轮询与延迟优先都渲染 `urltest`，主备由 Agent 选择。权重策略已移除（迁移 `0104_egress_weighted_to_round_robin.sql` 把旧组映射为 `round_robin`）。
- **每域名精确字节**：不可得；访问记录字节列为空，UI 显示“—”（Phase 7.4 处理可空与显示）。
- **跨中转的实际出口节点**：只记录策略在该节点解析出的**配置出口**，标注“configured egress”。
- **逐目标 UDP 路由**：按上游行为。
- **SOCKS5 作用域 UDP 策略无法匹配身份**：sing-box 渲染直接拒绝该组合（`internal/agent/core/singbox/render.go` 的 `sing-box scoped SOCKS5 UDP identity requires native validation before publication`）。
- **WireGuard 每 peer 用户归属**：sing-box 官方核没有入站；能归属到用户的入站按上游能力提供，无法归属的入站不报每用户流量，UI 明示。

## 5. 仍未实现/待验收（不缩减）

- 规则真实匹配（用户/设备/节点/组、域名精确/后缀/通配、服务集、IP/CIDR、端口、TCP/UDP、时间范围）、默认优先级与 DST 边界：源码已有，运行待验收。
- 出口组主备/轮询/延迟优先与递归备用、健康失败不隐式 Direct：源码已有，运行待验收。
- A→B、A→B→C 环路/跳数/缺内部入口/协议-UDP 不匹配/端口冲突校验、先出口后入口发布、回滚反向：源码已有，运行待验收。
- 订阅授权组动态展开、隐藏中转、撤销凭据、未成功发布过滤与三种导出格式：源码已有，运行待验收。
- ~~Xray WireGuard TUN 入站 UDP 端到端、A→B/A→B→C 数据面：受控环境实测待做。~~ 已实测：全部协议的入站、出站、认证、UDP、分流与两条 A→B→C 链见[验收矩阵](v1-acceptance.md) 9.12（Xray 与 sing-box 都覆盖）。
- 完整系统 TUN 按 AGENTS 第 36 节延后，不冒充已交付。
- 域名级访问记录已改由上游输出生成（Xray access log、sing-box 核心日志）：Phase 7 完成，运行验收仍待做。

以上实测项需要 owner 的 Linux VM（含官方 Xray/sing-box 二进制）执行，属于[验收矩阵](v1-acceptance.md)的范围；本表不因源码存在而记为通过。

## 6. 核心版本与更新方式

- 节点运行官方发布二进制，路径由 Agent 配置的 `core_path` 指定（示例 `/usr/local/lib/proxy-agent/xray`）；`install.sh --core` 负责首装并校验归档摘要。
- 核心能力随版本不同：Agent 在运行时探测（Xray 读 `version` 并探测统计/在线统计；sing-box 读 `version` 的 `Tags:` 并探测 Clash API），缺失能力上报为不可用而不是拒绝运行（D1）。
- 后续升级由面板“系统 → 核心版本”录入 `组件 / 稳定版本号 / 官方 GitHub 资源 URL / 归档 SHA-256`（Phase 8，`CoreUpdateRequest` schema 2），节点侧再次校验摘要后切换，健康失败回滚；核心不再使用签名目录，签名目录只服务 Agent 自更新。
- 本表以 Xray 26.3.27 与 sing-box 1.14.3 为基准版本；换成其他稳定版本时需重新核对本表。
- Alpine（musl）节点装 sing-box 时 `install.sh` 与 `node-install.sh` 取官方的 `-musl` 归档（glibc 归档在那里起不来），摘要按架构与 libc 各自固定；Xray 只发静态二进制，不分 libc。
