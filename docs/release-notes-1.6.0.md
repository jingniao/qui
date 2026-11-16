# v1.6.0 发布说明

**已于 2026-11-16 发布，tag `v1.6.0` → `4f09e0a`。** 迁移 **0152**，迁移头 **0152**。生产本次未升级。范围见[规划](v1.6.0-plan.md)。下文时间均为 2026-11-16 UTC。

## 变更内容

- **服务器方案以导入节点为出口**：中转方案画布（服务器执行）可以把可转换的导入节点放在最后一跳。发布时面板在上一跳服务器生成一个出站，用导入节点保存的参数和凭据连接它；该出站归连接所属的画布隧道管理，撤除方案时一并删除，普通出站编辑器修改或删除它返回 409 `managed_by_tunnel`。导入节点刷新后内容变化时，方案状态「草稿」显示原因，下次发布更新该出站；内容未变时不重写。服务器方案的资源库只列能转换、且至少一台服务器能连接的导入节点。
- **出站客户端字段**：出站新增 ALPN、uTLS 客户端指纹、WebSocket Host、Reality 客户端参数与出站 xhttp。旧 Agent 会拒绝这些字段，所以面板在保存出站、生成快照与发布前要求该服务器全部启用的 Xray／sing-box 核心上报对应出站组合，否则返回 `outbound_client_unsupported` 并提示升级 Agent。Xray 支持 13 个组合（含 Reality tcp／Vision／gRPC／xhttp 与 TLS xhttp），sing-box 支持 10 个（无 xhttp，Reality 仅 tcp）。
- **导入节点的快速中转**：节点列表在导入节点上点「添加中转」打开快速流程。「纯转发」创建目标为该导入节点的隧道规则（新目标类型 `import`，迁移 0152），所有导入类型都可用、不需要升级 Agent；导入节点可转换且路线末端服务器支持时还可选「代理中转」。新入口或规则默认加入包含该导入节点的分组。隧道管理的规则编辑器同样可选导入节点为目标，节点分组的「隧道转发」成员包含这类规则。
- **导入目标规则的订阅与刷新**：订阅输出导入节点的原始参数，只把地址端口换成入口地址与规则端口，保留原 SNI，名称取规则名。订阅连接刷新后按「名称|协议」把规则改绑到新内容；地址或端口变化时已发布规则标为「需重新应用」，只换凭据不需要。导入节点被刷新删除时规则保留并显示「目标已删除」。被规则引用的导入节点不能删除（409 `import_in_use_by_rule`）。
- **系统 Reality 预设**：「系统设置」新增「Reality 预设」（最多 20 个，可设默认），每个预设包含握手目标、服务器名称与可选的传输／Flow，不含密钥与 Short ID。节点编辑器与中转入口的安全设置可一次填入；新建 VLESS Reality 入站自动带入默认预设，已有入站不会被改写。未设置预设时仍显示内置 5 个常用目标。

## 迁移与升级注意

0152 重建 `tunnel_rules`，增加目标类型 `import` 与导入节点引用列；重建时保留全部规则、分组成员与原有的两个失效触发器，升级后订阅内容不变。升级前保存并验证加密备份。

导入节点出口用到 Reality、xhttp、ALPN、客户端指纹或 WebSocket Host 时，上一跳服务器的 Agent 须为 1.6.0；只用经典参数（无加密或 TLS over tcp/ws/grpc）的导入节点不要求升级 Agent。导入节点纯转发不要求升级 Agent。

## 已知限制

- 不转换为服务器出站的导入类型：Shadowsocks 2022、固定证书指纹、跳过证书校验、Hysteria2 混淆、WireGuard 与其他未建模传输。它们仍可用于客户端方案和纯转发。
- Hysteria2 导入节点的跳跃端口不转发、不转换，只连主端口。sing-box 出站不输出 Reality SpiderX。
- 同一服务器同时启用 Xray 与 sing-box 时，需要 xhttp 的导入出口不可用（sing-box 不支持 xhttp）。
- 未做浏览器实机验收，未在测试面板上做导入节点出口的端到端连通。

## 发布验证

### 实现与审查

第 3 项（Reality 预设）与第 2.1 节后端由 Codex 实现、本人审查；Codex 之后连续无响应（含只要求回复一行的探测），其余部分由本人实现。审查中修正：导入刷新的故障注入测试改为注入到保留行的更新、路由覆盖清单补两条预设路由、服务器卡片只列关联规则（import 规则不再误挂在入口服务器下）、Hysteria2／TUIC 导入出站改为同时承载 TCP 与 UDP、经中间服务器的导入出口同样继承分组。

### 真实核测试（Linux 测试机，提交前）

- Xray 26.3.27：面板渲染的出站连接自建 Reality tcp／Vision／gRPC／xhttp 与 TLS xhttp 监听均取得源站响应，错误 Reality 公钥被拒。
- sing-box 1.14.3：面板渲染的 Reality tcp／Vision 出站认证成功，错误公钥被拒。
- 实际上报：Xray 5 个入站＋13 个出站组合，sing-box 2 个入站＋10 个出站组合。

### 本地完整 Linux 门禁

- 首个发布提交 `25621ba`：check 部分进行中即被取代；真实核部分 `internal/platform/server` 的 `TestRealUserCredentialSubscriptionAndRepublishedRevocation`（真实 Agent 20 秒内未完成修订 2）与 `internal/agent/core/singbox` 的 `TestOfficialXrayClientNativeSingboxUDPInteroperability/trojan`（Xray 启动失败）失败。渲染出的 Trojan 配置单独 `xray run -test` 通过；根因是 Xray 渲染每次都重新探测设置契约，v1.6.0 加入 13 个出站组合后每次发布约运行 24 次原生检查，负载下拖慢发布与启动。修正为按二进制（路径、版本、大小、修改时间）缓存完整探测结果，不完整的结果不缓存；两个失败测试与 sing-box 整包在测试机上通过后提交 `4f09e0a`，对新 SHA 重跑两条门禁。
- 发布提交 `4f09e0a`：`GATE 4f09e0a PASS total=1279s`，真实核部分 342 秒，一次通过。

### GitHub check

- 运行 43880358349（`25621ba`）在修正提交后取消。
- [运行 43883440815](https://github.com/jingniao/qui/actions/runs/43883440815)（`4f09e0a`，完整门禁＋cores）一次通过。

### 发布记录

- tag `v1.6.0` → `4f09e0a`，[Release 运行 43887293285](https://github.com/jingniao/qui/actions/runs/43887293285) 成功，14:44:29 UTC 发布，13 个资产齐全：两个平台的 `proxy-control`、`proxy-agent`（`.bin` 与签名清单）、`sing-box-1.14.4-qui.1`（含 provenance）、`SHA256SUMS`、`node-install.sh`、`panel-install.sh`。
- amd64 Agent 签名清单：版本 1.6.0，序号 **1006000**，签名存在。

## 生产

本次未升级生产（面板 1.5.8）。
