# 协议支持补齐规划：外部订阅、自建节点与失败诊断

状态（2026-11-05）：S1–S4 与 M1–M4 已实现并整合到本地 `compat-integration`（`3e449e7`，迁移头 0133），已通过个人复核与针对性 Windows 检查；不代表 S5、Linux、浏览器或主机验收完成。S0 与原 M1 阶段门禁通过；S1 `ccf23e4` 门禁仅因 0133 缺校验和固定失败（check 1972s、cores 560s、合计 2533s，END 2026-11-05T15:01:31Z），修正 `4ded28f` 门禁运行中，未记通过。最新用户指令以针对性验证和一次最终整合 Linux 门禁替代逐阶段重复门禁，不扩大测试或增加防御逻辑；浏览器走查仍待执行。当前边界与证据以[英文版 §14.8](subscription-import-compatibility-plan.en.md#148-implementation-status-2026-11-05)为准，全部仍属未发布的 v1.4.0。

S0 状态（2026-11-05，rev 3）：证据与净化夹具清单已采集；首轮与第二轮审阅均未通过，已按意见修正（rev 2：补跑两格、能力按客户端逐格、URI 编码按 SIP002/SIP022 原文；rev 3：`S0-GR-02` 改为第二条**有效**的 no-flow gRPC 代表条目，Vision+grpc 只作为负例 `S0-NEG-VISION-GRPC`；两处 mihomo 改为“首次失败、后续成功、原因未确定”；sing-box xhttp 明确为原生配置拒绝且未发起 HTTP）。**已通过主助手本人审查（仅证据与夹具范围，不是完整协议验收）；提交 `9045e1b` 的 Linux 门禁已通过（`check` 1995s、`cores` 574s，`GATE 9045e1b PASS total=2569s`，START 2026-11-05T10:00:34Z，END 10:43:23Z），S0 文档与夹具与该提交逐字节一致**；16 条代表性条目（3 SS2022 AES-256 + 11 Reality+xhttp + 2 Reality+grpc）全部有效。详见[英文版 §10 的 S0 状态段](subscription-import-compatibility-plan.en.md#10-implementation-sequence-and-deliverables)、[S0 证据](v1.4-s0-evidence.md)、[S0 夹具](v1.4-s0-fixtures.md)与[开发记录](subscription-import-compatibility-change-log.md)末两条。要点：SS2022 的规范导出是百分号编码的明文 userinfo（SIP002 规定 AEAD-2022 禁 Base64URL），Base64URL 仅作输入兼容，且无任何探测把 URI 文本交给客户端；能力结论按（组合、客户端）逐格给出——mihomo 只测过 xhttp `auto`，Reality+TCP 无 Vision 与 Reality+grpc 没有 mihomo/sing-box 实测，AES-128 身份形态只有 sing-box，chacha 单钥只有 mihomo；首次遗留的两格已补跑并入 35 条矩阵；mihomo Reality 首次拨号失败原因**未确定**，其正例标为“后续尝试成功”；Vision+grpc/xhttp 的失败发生在客户端 VLESS 出站且源站命中为 0。S0 只改文档与采集证据，不改生产代码、迁移、API、UI；此段保留 S0 当时证据范围；当前实现和门禁状态见顶部及英文版 §14.8。

本次更新：2026-11-04T23:15:43-08:00。自建节点 xhttp / Reality 已纳入正文范围、实施阶段与验收，不再仅作为文末补充。保留当前文件路径，避免已有链接失效。

基线：`feat/v1.4-tunnels`，HEAD `c708c35`，迁移头 `0132`。该基线为原规划背景；当前实现见顶部。本文不刷新来源或调整运行节点、客户端。

## 1. 目标与范围

本规划统一覆盖以下三条链路，尽量复用既有模型、校验、加密、发布与订阅代码：

| 方向 | 本次规划内容 | 最终效果 |
| --- | --- | --- |
| 外部订阅 | Shadowsocks 2022、VLESS Reality、VLESS xhttp 的解析、加密保存与各格式导出 | 支持项可完整导入并供兼容客户端使用，不支持项有准确原因 |
| 自建节点 | 优先补齐 Xray VLESS 的 xhttp / Reality；sing-box 按实际版本能力独立适配 | 节点编辑器可配置，后端可保存，Agent 可渲染/应用，用户可取得匹配的已发布订阅 |
| 失败诊断 | 外部抓取/解析/导出，以及自建参数校验/能力/发布错误 | 区分失败发生在哪一层，不把保存成功或写状态成功当成连接成功 |

外部订阅链路为 URI/Base64/Clash 输入 → 类型化解析 → 加密存储 → 各格式导出。自建节点链路为编辑器 → 后端配置/校验 → DesiredState → Agent 原生渲染/校验/应用 → 已发布订阅。两者共用客户端参数映射与导出规则，自建服务端配置按第 13 节单独建模。

上轮只读检查记录：来源返回 HTTP 200；Base64 解码得到 16 个节点，其中 3 个使用 `2022-blake3-aes-256-gcm`，13 个 VLESS 使用 Reality，传输分别为 11 个 xhttp 和 2 个 grpc。此统计是检查当时的结果，不保证外部来源之后返回相同内容；本轮不重新获取真实来源。

首期验收应使用脱敏、人工构造的等价样例覆盖这 16 类条目，不把真实订阅地址、Token、UUID、密码或完整返回内容写入代码、测试夹具、日志和本规划。

产品边界：

- 本次增加外部导入兼容及自建 VLESS 的 xhttp / Reality 支持；自建链路必须同时补齐后端、DesiredState、核心能力、Agent 和前端，不能仅增加下拉选项。
- 自建首期以 Xray VLESS 入站、成功发布及对应用户订阅为闭环；自建出站仅对已验证并纳入同一设置契约的组合开放，画布/内部中转不自动改用新组合。
- 自建 Shadowsocks 2022 涉及服务端多用户密钥和隐藏身份管理，本规划首期仅补外部 SS2022；其自建能力单独立项，不因复用算法验证而默认开启。
- 导入能力和目标格式/客户端能力分开：完整保存的节点可以只对支持的格式导出，其他格式报告明确省略原因。
- 保留导入不参与 Agent 发布、账户计量和设备门控的现有边界。
- 本文只授权范围规划，不执行生产变更；不因兼容失败更换用户节点、降低传输安全、自动升级生产核心或启用不安全 TLS。

## 2. 现有阻塞点及可复用代码

| 层级 | 现状 | 复用与扩展 |
| --- | --- | --- |
| 来源抓取 | `importer/fetch.go` 把 DNS、HTTP、TLS、超时等归为 ErrFetch | 保留 HTTPS、公网校验、逐跳固定 IP、2 MiB/15 秒限制，补类型化错误 |
| 输入解析 | `importer/parse.go` 严格白名单；未知参数直接跳过；SS2022 不在算法列表 | 继续使用 URI/Base64/Clash 三个解析器，增加类型化字段和逐项诊断 |
| 中立模型 | `subscription/proxy` 缺 Reality、xhttp、客户端 TLS 指纹等字段 | 增加可选结构，不新增一套不透明原始配置透传 |
| 加密存储 | `importer.Store.replace` 对中立模型 JSON 加密入库 | 沿用 Vault、事务替换和既有节点来源授权 |
| URI 导出 | `format/uri` 不接受 reality，且无对应参数输出 | 补完整参数映射和新 SS URI 编码规则 |
| Mihomo 导出 | `format/clash` 按协议名过滤，模型缺 Reality/uTLS/xhttp 字段 | 扩展组合能力校验、类型化 YAML 和结构化省略原因 |
| sing-box 导出 | 当前模型只处理普通 TLS 和部分传输；部分不支持项会终止整份渲染 | 补实际支持字段；单项不能表达时按既有 Exporter 契约省略并警告 |
| 刷新状态 | 拉取/解析失败写固定 last_error，随后返回 writeErr，可能被视为成功 | 返回业务结果，区分成功、部分成功、失败和数据库写入错误 |
| 前端 | refresh 返回 204 后显示“刷新完成”；节点数只有缓存条数 | 读取业务结果，展示识别/导入/跳过数量和详细原因 |
| 自建节点编辑 | `NodeProtocolFields.vue` 只提供 tcp/ws/grpc、none/tls | 按目标核心真实能力展示 xhttp/Reality 和相应字段 |
| 共享配置/能力 | ProtocolSettings、ValidateSettings、协议矩阵均无新组合 | 新增类型化服务端/客户端设置及能力契约，复用现有共同校验 |
| Agent 渲染/应用 | Xray/sing-box 适配器未接入新配置 | 复用编译、原生校验、多核应用、健康检查、版本和回滚 |
| 备份与恢复 | 已有 `backup/validateImports` 与加密载荷校验 | 复用并扩展新模型和诊断字段验证 |

外部导入仍位于既有 subscription/importer、proxy、format 模块。自建节点扩展既有 panel/proxy、desiredstate、agent/core 和 node-editor，不新建平行发布服务。共用字段规范、编解码和不含服务端私有材料的验证逻辑；角色不同的结构按第 13 节区分。

## 3. 阶段 0：先确认目标核心和协议组合

当前项目真实核心套件以 Mihomo `1.19.33`、sing-box `1.14.3`、Xray `26.3.27` 为参考。实施时锁定实际测试二进制、版本、构建标签和 SHA256，不使用模糊的“最新版支持”。

本轮已有静态证据：

- `docs/archive/v1.1-phase0-findings.md` 记录 Mihomo 1.19.33 对 SS2022 AES-128 配置校验通过；该记录不能替代本次 AES-256、Reality 或实际连接验收。
- 本地缓存的 sing-box **1.14.2** 源码含 Reality/uTLS 和 SS2022 模型，其 V2Ray transport 枚举未包含 xhttp。该缓存版本不等于项目固定的 1.14.3，因此只作为需要验证的线索。
- Xray 26.3.27 的本地源码可用于核对 Reality 和 xhttp 参数，但服务端存在实现，不代表每一种客户端组合都有效。

按以下矩阵逐格取得证据：

| 配置 | URI/Base64 | Mihomo YAML | sing-box 片段 | legacy Clash |
| --- | --- | --- | --- | --- |
| SS2022 AES-256 | 目标支持，验证 SIP002 和密钥 | 目标支持，原生校验及连接验证 | 目标支持，原生校验及连接验证 | 默认视为未验证，按锁定版本核实；不能沿用普通 SS 的布尔标记直接放行 |
| SS2022 AES-128 | 随 AES-256 一并实现和验证 | 同左 | 同左 | 同上 |
| SS2022 ChaCha20 | 核对实际客户端支持后纳入 | 按版本判定 | 按版本判定 | 未验证则省略 |
| VLESS Reality + TCP | 目标保真 | 按确切字段和指纹验证 | 按 Reality/uTLS 字段验证 | 保持现有 VLESS 省略 |
| VLESS Reality + grpc | 目标保真，核对组合是否合法 | 单独验证，不从 TCP 推断 | 单独验证，不从 TCP 推断 | 省略 |
| VLESS Reality + xhttp | 目标保真，并用支持该组合的客户端验证 | 先确认固定版本是否支持，缺能力则给出格式省略诊断 | 先确认固定版本是否支持，缺能力则给出格式省略诊断 | 省略 |

协议组合必须联合校验 protocol/security/transport/flow/cipher，不能只判断 VLESS 或 Shadowsocks 协议名。若固定客户端无 xhttp，仍应完成中立模型与 URI 保真；若需要在该客户端使用全部 xhttp 节点，先明确升级版本的实际支持和验收，不把 xhttp 转换成 ws/grpc 或删除必要字段。

阶段 0 交付一份版本/字段/配置校验/连接结果矩阵。只有取得对应证据的格子才标为“已支持”。

自建部分另外建立“核心 × 版本/设置契约 × 入站或出站 × security/transport/flow”矩阵，至少逐项核验 Xray VLESS Reality+TCP、Reality+xhttp、TLS+xhttp 和适用的 Vision 组合。客户端能识别分享链接不能替代服务端组合验证；旧 Agent 的未知能力不得按已支持处理。

## 4. 协议模型与解析设计

### 4.1 中立模型

优先在 `internal/panel/subscription/proxy` 中增加可选字段：

```text
Security = none | tls | reality

TLS
  保留现有 ServerName / ALPN / PinnedCA 等
  ClientFingerprint       客户端 TLS/uTLS 指纹

RealityOptions
  PublicKey
  ShortID
  SpiderX                 仅实际支持该语义的格式可使用

Transport
  保留 Type / Path / ServiceName
  增加受支持的 Host 语义
  XHTTPOptions            类型化的 mode、host、path 及已验证扩展
```

现有 Mihomo `fingerprint` 输出用于证书固定；URI `fp`/Mihomo `client-fingerprint` 表示 TLS 客户端指纹。两者不能混用，也不把 Reality 公钥当成证书指纹。

Reality 和 xhttp 结构的字段边界由阶段 0 的目标格式映射确定。所有影响连接的已识别参数必须可往返保存；尚未建模且影响连接的扩展，返回明确的不支持诊断，不保留任意 map 并静默透传。

### 4.2 VLESS / Reality

- 正确解析 UUID、flow、security、SNI 和客户端指纹。
- 处理 `pbk`、`sid`、`spx`、`fp`，校验公钥编码/长度、Short ID 格式/长度、指纹枚举及协议组合。
- `sni` 和 `servername` 作为已知别名归一化；二者值冲突时报告字段冲突，不能随机选一个。
- `encryption=none` 作为 VLESS 已知合法字段处理；其他值必须验证，不直接忽略。
- 空值与字段缺省区分，例如空 Short ID 是否合法由固定核心规则确定。
- grpc 的 `serviceName`、可能的 `mode` 别名与 xhttp 的 `mode` 分协议解析，避免把同名字段跨传输误用。
- Reality+grpc、Reality+xhttp 以及 flow 组合都单独验证；不可用组合仍报告失败，而不是为追求导入数量强行接受。

### 4.3 xhttp

- 类型化解析 host/path/mode，核对 auto、packet-up、stream-up、stream-one 等值在目标版本中的实际支持范围。
- 将传输 host 与 TLS server name 分开保存；正确处理 URL 编码、空 path、IPv6 地址和参数重复。
- 对 `extra` 或其他高级参数逐项建立支持清单、大小和嵌套深度限制；首期不能完整表示的项明确拒绝该节点，保留具体原因。
- 输入能够存储，不意味着每个导出器都能表达；在导出兼容矩阵中体现差异。

### 4.4 Shadowsocks 2022

- 优先实现本次出现的 `2022-blake3-aes-256-gcm`，同时补齐可共享验证逻辑的 AES-128；ChaCha20 依版本证据开放。
- 密钥按实际算法校验 Base64 编码和解码长度：AES-128 为 16 字节，AES-256 为 32 字节；其他算法按其规范处理，不能仅检查密码非空。
- 如支持多用户 identity-key 链，逐段校验结构和长度；未完成该能力时返回明确诊断，不能截取最后一段冒充完整支持。
- URI 输入兼容已验证的 Base64 userinfo 与明文/百分号编码形式；特别处理密码内 `:`、`+`、`/`、`=` 和 URL 转义。Base64URL userinfo 仅作为**输入**兼容路径，SS2022 不得据此输出。
- SS2022 导出采用 SIP002 的**明文**形式：`ss://` + 百分号编码的 `method:password` + `@host:port`（tag 单独转义）。SIP002 规定 AEAD-2022 的 userinfo *“MUST NOT be encoded with Base64URL”*，且*“method and password MUST be percent encoded”*；password 本身是 SIP022 规定的标准带填充 Base64 PSK。S0 已固定该条文并抄录在[证据报告](v1.4-s0-evidence.md) §7；identity 分隔符的百分号编码是 S2 须用会解析 URI 的客户端确认的决定。非 SS2022 条目保持既有 SIP002 形式。
- 标准化后仍保留正确算法名，不降级为普通 Shadowsocks 算法。

### 4.5 严格且可解释的未知字段策略

已知别名归一化；有明确无运行影响的元数据可以在限定清单内忽略并记录；未知的安全/传输参数拒绝该节点并标出字段名称。

每个输入条目产生独立解析结果。Base64 解码后复用同一 URI 解析路径；Clash 输入新增 reality-opts、client-fingerprint、已支持的 xhttp 配置结构，继续执行类型、数量和大小限制。

为单节点粘贴、机场订阅和 Clash 文档复用同一套规范化/验证函数；禁止只修改某一个入口的白名单。

## 5. 格式导出与整体可用性

1. `format/uri`：完整导出 VLESS Reality/xhttp/grpc 字段及 SS2022；Base64 仅包装同一 URI 输出。
2. `format/clash`：按固定 Mihomo 版本增加 reality-opts、client-fingerprint 和实际支持的 transport 字段；legacy 独立判断 cipher/security/transport，不把普通 SS 支持扩大为所有 SS 算法。
3. `format/singbox`：补齐已证实的 Reality、uTLS 和 SS2022；不支持的 xhttp 返回单节点不可表达诊断。
4. 复用已有 `format.Output.Warnings`、中立模型和订阅 Registry；补统一的协议组合能力判断辅助方法，先不引入复杂插件系统。
5. 不可表达的合法节点按项目 Exporter 契约省略并警告；某个节点不支持不能让其他可用节点的整份订阅失败。内部数据损坏仍作为错误处理，不伪装为正常省略。
6. 节点省略后继续复用组成员过滤、客户端链依赖清理和 emptyPolicy；禁止留下不存在的代理名称，或把已配置的 REJECT 空组策略改为 DIRECT。
7. 在 Owner 的预览中区分“已导入 N 个”和“当前格式可输出 M 个”。导入成功数不等于所有客户端格式的节点数。

当某格式完全无法表达一个来源时，Owner 明确看到原因和兼容格式提示；普通订阅请求保持既有授权和响应合同，不把带敏感内容的诊断插进原始 URI/YAML 文本。

## 6. 结构化失败诊断

### 6.1 区分各链路的结果

导入刷新结果负责描述抓取、解码、解析与缓存提交。格式预览结果负责描述当前客户端格式能否表达已保存节点。两者不能共用一句“协议不支持”。

自建节点另沿用既有字段校验和发布 ApplyResult：保存草稿成功、发布应用成功、客户端真实连接成功是不同状态。不将节点发布错误写成来源刷新失败，也不将人工连接测试结果伪装成 Agent 普通健康状态。

建议刷新结果结构：

```json
{
  "sourceId": "opaque-id",
  "outcome": "partial",
  "stage": "parse",
  "code": "import_partial",
  "counts": {
    "total": 16,
    "accepted": 5,
    "rejected": 10,
    "duplicates": 1,
    "cached": 5,
    "usable": 5
  },
  "attemptedAt": "UTC timestamp",
  "lastSuccessAt": "UTC timestamp or null",
  "diagnostics": [
    {
      "entryIndex": 3,
      "protocol": "vless",
      "stage": "parse",
      "code": "unsupported_transport_option",
      "field": "transport.mode",
      "message": "该传输参数尚未支持"
    }
  ],
  "diagnosticsTruncated": false
}
```

上例是结构示意，不是当前来源的处理结果。建议固定：total = accepted + rejected + duplicates；accepted 为去重后本次成功保留的节点数。每项可附多个原因，但统计按条目计算，不能因一个节点同时命中 Reality/xhttp 两个原因而把失败总数翻倍。

cache 数量、上次成功缓存数量、本次识别数量及当前可用数量分别定义。下载未完成时 total 为 null/未知，不写成“来源没有节点”。

### 6.2 错误分类

| 阶段 | 典型错误码 | 用户应看到的说明 |
| --- | --- | --- |
| 输入 | invalid_source_url | 来源必须是合法的 HTTPS 订阅地址 |
| DNS | source_dns_failed | 来源域名无法解析 |
| 地址/重定向 | source_address_rejected / source_redirect_rejected | 来源或跳转地址不符合公网访问要求 |
| 建连/TLS | source_connect_failed / source_tls_failed | 无法连接来源 / 证书校验失败 |
| 时间限制 | source_timeout | 获取订阅超时，本次未更新缓存 |
| HTTP | source_http_error | 来源返回 HTTP 状态码，例如 401、403、429、5xx |
| 大小限制 | source_body_too_large | 内容超过允许大小 |
| 解码/格式 | invalid_base64 / unsupported_document / invalid_document | Base64 无效、返回页面或格式无法识别 |
| 节点字段 | invalid_uuid / invalid_ss2022_key / invalid_reality_key / conflicting_alias | 指出无效字段，不输出字段原始值 |
| 节点能力 | unsupported_protocol / unsupported_security / unsupported_transport / unsupported_parameter | 指出尚未支持的协议、传输或参数名称 |
| 全部被跳过 | no_supported_entries | 已识别若干节点，但没有可导入节点，提供分类数量 |
| 存储 | import_persist_failed | 缓存保存失败；未把半完成结果设为可用 |
| 格式导出 | format_option_unsupported | 节点已保存，但当前订阅格式无法表达该配置 |
| 自建参数校验 | invalid_reality_settings / invalid_xhttp_settings / invalid_protocol_combination | 指出无效字段、互斥参数或不合法组合，保留编辑输入 |
| 自建能力 | core_feature_unsupported / agent_contract_unsupported | 所选核心/Agent 未声明该配置能力，暂不可保存或发布 |
| 自建发布 | 沿用原生校验、应用、健康检查与回滚结果码 | 明确是配置校验、启动、健康或回滚失败，不显示“节点已搭建成功” |

Fetcher 和 Parser 内保留类型化原因；只在 Owner API/日志边界转换成固定模板、安全错误码和允许的数字信息。禁止直接输出底层 err.Error()、原始 URL、Location、请求响应正文或 HTTP 库可能携带的 Token。

诊断默认只展示条目序号和协议类型；需要名称时必须清理控制字符、限制长度且不把不可信名称原样写入日志。公钥等参数也不必完整回显。

每来源只保存最近一次结果，限制诊断条数和总字节（建议 50 项/32 KiB），另保留聚合数量及截断标识。未来若需完整历史再明确新增保留策略，不无限积累原始失败内容。

## 7. 刷新 API、状态与缓存语义

### 7.1 先修正成功/失败结果

把现有 Refresh 的“只返回 error”改为业务结果加 error，或新增兼容包装方法，手动刷新和定时任务共用同一实现。写入失败状态成功不等于刷新成功。

- 完全成功：返回 HTTP 200 + outcome=ok。
- 部分成功：返回 HTTP 200 + outcome=partial 和明确导入/跳过数量。
- 抓取失败：按固定映射返回 502；超时返回 504，响应附安全结果摘要。
- 内容已获取但格式无效/没有合格节点：返回 422 和可读诊断。
- 输入校验返回 400，内部持久化失败返回 500；不存在来源沿用明确的 not_found 语义。
- 用户取消/请求 context 结束不应被错误标记为协议不支持；当前任务若未提交，保留已提交状态。
- 现有前端 refresh 返回类型由 void 改为结构化结果；APIError.details 中保留失败结果，用于显示诊断。GET sources 同时返回最近一次结果，以支持定时任务后的页面刷新。

沿用原有 POST /subscription-sources/{id}/refresh，不增加会修改状态的 GET。来源创建/编辑仍可先保存，再请求刷新；若后者失败，提示“来源已保存，获取/解析失败”，保留来源 ID 和输入上下文，不提示“来源未保存”或“首次拉取完成”。

### 7.2 兼容现有 last_status 门控

当前 `Proxies` 要求 last_status=ok，且前端把任意 lastError 当成拉取异常。不要直接新写 last_status=partial，否则部分成功的节点会全部被现有代码停用。

首版建议：

- 有 accepted 节点并成功提交：last_status=ok，last_error 清空；新结果字段 outcome 可为 ok/partial，前端依 outcome 展示警告。
- accepted=0 或抓取失败：last_status=failed，last_error 写安全摘要；新结果保存具体失败分类。
- last_fetch_at 保持“最近尝试时间”兼容，UI 改为“最近尝试”；新增 last_success_at 表示最近成功刷新时间。

### 7.3 缓存处理

保留项目当前失败来源不参与订阅的保守策略，不在本次顺带改为继续使用旧节点。

- 完全成功/部分成功：在同一事务替换为本次 accepted 节点，并更新状态与诊断。
- 全失败：保留上次成功缓存的存储行供诊断，但来源状态 failed，订阅不使用这些行；没有历史缓存就明确显示“无可用缓存”。
- 失败标记与诊断持久化一致；数据库提交失败不得产生半新半旧缓存。
- URL 修改后 pending 不得继续使用旧 URL 的缓存冒充新来源结果。
- 保持订阅请求只读加密缓存，抓取仍由显式刷新/既有定时调度完成。
- 不自动调整刷新间隔或增加无限重试，沿用现有调度生命周期与取消处理。

## 8. UI 提示与交互

来源列表分别展示：来源已保存、最近尝试、最近成功、本次导入/跳过、缓存数量、当前可用数量。

| 情况 | 建议文案 |
| --- | --- |
| 保存成功，尚未拉取 | 来源已保存，等待获取节点 |
| 成功 | 已更新：导入 16 个节点 |
| 部分成功 | 导入 5 个，跳过 10 个，重复 1 个；查看原因 |
| HTTP 成功但无合格条目 | 已获取订阅，共识别 16 项，0 项可导入；查看兼容性原因 |
| 网络超时且有旧缓存 | 获取超时；保留旧缓存 8 个，当前暂停用于订阅 |
| 失败且无旧缓存 | 刷新失败；当前无可用节点 |
| 某格式不支持 | 已导入；当前 sing-box 格式省略 11 个 xhttp 节点，查看支持格式 |

上述数量均为说明示例，实际必须来自服务端统计。“已加密保存”只作为链接存储说明，不代表节点已导入。用户授权数量只说明授权关系，不代表可用节点数量。

“查看原因”弹窗按网络/解析/目标格式分组，显示聚合数量和条目序号、错误码及修复方向；不展示原始敏感 URL。Toast 以业务 outcome 为准，不再仅因 HTTP 返回成功就写“刷新完成”。

现有单独导入节点使用同样诊断；部分成功需要明确保留了哪些支持项。没有成功项时保留编辑内容，允许修改后重试，不清空表单。

自建节点编辑器联动要求：

- 选择 VLESS 后，按所属核心展示已支持的传输和安全方式；不适用或能力未知的项标明原因，并由后端再次校验。
- 选择 xhttp 显示其 host/path/mode；选择 Reality 显示允许的 SNI、握手目标、Short IDs 和密钥状态。Reality 不沿用普通 TLS 的“必须上传证书”校验路径。
- 公钥可以复制用于客户端；私钥默认隐藏且不从常规查询、版本差异或预览中返回。保存普通参数不自动轮换密钥。
- 下拉组合变更应处理依赖和互斥字段，保留用户可恢复的输入，不静默切换成其他安全或传输方案。
- 保存后显示“草稿已保存，发布后生效”；成功发布后提示客户端更新订阅。不得把发布完成表述为所有客户端已经更新或握手成功。

## 9. 数据兼容与文件范围

建议给 subscription_sources 增加最近结果 JSON 和 last_success_at 等必要字段。当前迁移头为 0132，实施时使用下一个未占用迁移号，不修改历史迁移。

旧 last_status/last_error/last_fetch_at 继续可读。历史没有诊断的失败只能显示“历史刷新失败，原因未记录”，不能回填为 Reality 不支持或网络失败。迁移不自动拉取真实订阅。

新可选协议结构保存在现有加密 payload 中，不把密码拆成明文列。同步修改缓存验证、备份载荷检查和恢复兼容。旧程序无法表示的新字段不能静默丢弃；最低兼容版本与降级需使用匹配备份的规则写入发布说明。

自建设置沿用入站/出站加密配置列和加密 Revision，不新增明文 Reality 私钥列。保存、版本差异、备份及恢复分别验证服务端私有材料和客户端公共参数的边界；旧 Agent/旧恢复工具的兼容检查必须明确拒绝无法理解的新设置，不能退回 none/tcp。新增能力字段如涉及 protobuf，使用可兼容的新增字段并重新生成契约代码；数据库迁移只在确有新列/状态需求时新增。

建议文件：

- `subscription/proxy/`：Reality/xhttp/客户端指纹及共享参数校验。
- `subscription/importer/parse.go`、`fetch.go`：协议兼容、归一化、类型化失败和逐项结果。
- `subscription/importer/store.go`、`handler.go`：刷新业务结果、诊断持久化和 API 映射。
- `subscription/format/uri`、`clash`、`singbox`：完整导出、组合能力判断、单项省略诊断。
- `subscription/service.go`、预览处理器、客户端策略候选/链和分组处理：传播诊断，保持授权与依赖正确。
- `panel/system/backup/imports.go` 与相关恢复验证：检查新载荷和结果。
- `web/src/modules/proxy/importApi.ts`、`proxyNodes.ts`、`ProxyNodeListPage.vue`：类型、来源管理状态与详情。
- 订阅/客户端策略预览页面：显示格式能力和 warnings；复用既有组件，不另建一套来源管理。
- `pkg/contracts/desiredstate/`、必要的 `proto/agent.proto` 与生成代码：自建参数和能力/设置契约，保留历史配置的读取能力。
- `internal/panel/proxy/`、`internal/panel/node/`、`internal/platform/server/`：保存/发布门禁、核心能力和接线，复用 Snapshot/Deployment，不走第二条下发路径。
- `internal/agent/core/xray/`、`singbox/`、`capability/` 及必要的协调层：按核心配置渲染与能力上报，沿用现有应用和回滚。
- `web/src/modules/proxy/components/node-editor/`、协议类型/API 和能力展示：自建节点参数表单、联动、校验、错误提示和确认摘要。
- 入站/出站、Revision、密钥和节点配置的备份恢复测试：自建新结构与发布历史的一致性。
- 新迁移、测试、AGENTS.md、协议矩阵、运维、版本草稿与开发记录。

已有客户端策略 Phase B 的稳定导入引用（source ID + name|protocol）继续复用；新参数变化不能意外授予用户节点或使客户端链在无提示下改指向。保留来源授权、稳定身份和内容变更检查。

## 10. 实施顺序与交付

| 阶段 | 内容 | 交付门槛 |
| --- | --- | --- |
| S0：版本与样例核验 | 核对目标核心、URI 规范、实际组合、脱敏夹具 | 明确每种配置在哪些输出格式可用，不凭协议名推断 |
| S1：先修诊断 | 类型化错误、业务结果、统计、迁移、缓存语义与前端提示 | 现有不支持项可准确解释，失败不再显示成功 |
| S2：SS2022 | 密钥/URI 编码、存储、格式导出、部分成功 | 本次 3 类 AES-256 示例可完整往返并通过实际连接验证 |
| S3：Reality | 安全字段、指纹、别名、TCP/grpc 合法组合、导出 | 受支持组合无参数丢失，非法组合给出精确原因 |
| S4：xhttp | 类型化参数、组合能力、URI 保真及支持格式输出 | 11 类 xhttp 示例可保真导入；不支持格式明确省略；至少支持该组合的客户端完成实际连接验证 |
| M1：自建契约与能力 | 服务端 Reality/xhttp 模型、共享校验、方向/版本/组合能力及旧 Agent 门禁 | 支持矩阵与保存/发布一致；未知能力不能误放行 |
| M2：Xray 自建闭环 | 密钥、Xray 渲染、原生校验、发布/健康/回滚、已发布订阅映射 | 临时自建 VLESS 节点可真实连接，回滚与凭据边界正确 |
| M3：编辑器和协议错误 | 接入 xhttp/Reality 选项、参数、密钥状态、组合联动及分层错误 | 不只是选项可见，而是可保存、可发布、可取得正确订阅 |
| M4：sing-box 适配 | 在已核验的固定版本中接入支持的 Reality/传输组合 | 每个开放组合都有本核心的原生校验和连接证据；无能力的 xhttp 保持不可选 |
| S5：联合回归与文档 | 导入、自建、授权、链依赖、空组、新旧 Agent、备份恢复、真实核心和文档 | 两条链路均达到所声明能力，门禁与证据齐全 |

依赖关系：S0 为所有协议开发前置，S1 可先独立交付；S2—S4 完成客户端模型和格式能力；M1→M2→M3 接通自建 Xray，其中 M2/M3 依赖相应协议的客户端导出工作。M4 复用 M1 框架，但不得用 Xray 的结果代替 sing-box 验收。S5 在所列交付范围全部完成后统一执行，不以外部导入完成替代自建交付。

每阶段追加开发记录：需求、文件、行为前后、字段/迁移变化、测试命令和结果、未完成项及返工。只在对应能力真正通过后更新“已支持”矩阵。

## 11. 验收标准

1. 使用人工构造的 3 个 SS2022 AES-256、11 个 Reality+xhttp、2 个 Reality+grpc 样例，逐项记录解析、存储、输出与目标核心结果；有效组合应全部保留必要参数，不合法组合准确说明。
2. 导入数量、重复数量和拒绝数量相互一致；空文档、HTML 返回页、Base64 错误与全不支持均有区别。
3. 正常/错误/空密钥、identity-key 链、Reality 参数冲突、重复参数、未知安全字段、过大 extra 均有明确结果。
4. URI → 中立模型 → 加密入库 → 读取 → URI/Base64/支持的 YAML/JSON 能语义往返；旧节点输出保持既有兼容。
5. 一项格式不支持不会使其他项整份订阅失败；链、分组和 emptyPolicy 无悬空引用和意外直连。
6. HTTP 401/403/429/5xx、DNS、TLS、超时、大小限制、重定向和私网拒绝均能区分；保留既有 SSRF 测试，异常中不出现敏感 URL。
7. 全成功、部分成功、全失败、历史缓存、首次拉取和 URL 修改后的结果与 UI 一致，失败无成功 Toast。
8. 后台定时刷新与手动刷新使用相同诊断；中断/取消/并发不破坏缓存，失败状态不是数据库写入成功的别名。
9. 日志/API 诊断/测试快照不泄漏 Token、密码、UUID、密钥链、完整 URI 和响应正文；诊断大小与条目数有界。
10. 新旧数据库与备份恢复后，新协议字段、来源状态、授权和稳定引用均保留；无自动刷新或自动启用副作用。
11. 用固定版本 Xray/sing-box/Mihomo 完成原生配置检查；用本地临时凭据与受控服务器验证实际握手和 HTTP 请求，不使用真实机场凭据作为持续集成夹具。
12. 区分“配置检查通过”和“真实连接通过”：语法通过不能替代 Reality 握手、SS2022 密钥和 xhttp 请求的实际验证。
13. 按 AGENTS.md 执行 Linux `scripts/check.sh` 和对应真实核心套件，浏览器走查来源管理和格式预览，保存证据后再宣称完成。
14. 自建节点选项、ProtocolSettings、协议矩阵、Agent 上报/渲染和已发布订阅对同一组合给出一致结果；旧 Agent 及未知能力不能保存/发布新配置。
15. 自建 Reality 私钥安全生成并加密保存，普通保存不换钥；显式轮换、重新发布和版本回滚的行为明确，私钥不进入客户端订阅、日志或预览。
16. Xray 的已声明 Reality/xhttp 组合在真实临时节点完成用户认证和 HTTP 请求；错误参数、端口冲突、核心校验/健康失败执行既有失败/回滚路径，不留下伪成功状态。
17. sing-box 独立验收所开放的组合；能力不足时前后端均拒绝，不自动换成其他核心、普通 TLS 或 TCP。
18. 自建草稿不提前进入订阅，成功发布后用户获得与监听一致的公钥/SNI/传输/凭据；备份恢复和历史版本读取保留私有材料且不自动发布。

复用测试位置：`importer/parse_test.go`、`fetch_test.go`、`store_test.go`、handler 测试、`format/*` 测试、`platform/server` 集成测试及 `backup/imports_test.go`。只为新增协议和业务语义补有意义的行为测试，不用源码匹配断言代替功能验收。

自建部分复用 desiredstate 协议校验测试、panel/proxy 保存/快照测试、Agent Xray/sing-box 渲染与真实核心测试、多核心应用/协调回滚测试和现有节点编辑器自测。新增真实连接夹具使用受控目标和临时密钥，不影响生产节点。

## 12. 本轮工作记录

以下保留首次规划的工作范围；当前整体范围以第 1、10、11、13 节及配套开发记录的最新更新为准。

- 阅读当前 importer、neutral proxy、URI/Mihomo/sing-box 导出、错误提示、API、备份验证和版本证据。
- 核对当前分支已提交客户端策略 Phase B，迁移头已是 0132；本规划复用该基线，不沿用早期未提交版本的假设。
- 新建本规划与配套开发记录；未修改功能、迁移、数据库或运行配置。
- 本轮没有重新请求真实订阅，没有运行测试、构建或真实核心验证。阶段 0 中的版本支持仍须按表补充证据。

## 13. 自建节点 xhttp / Reality 的实现设计

自建节点已纳入本规划 M1—M4，并与外部导入 S0—S4 一起接受 S5 联合验收。本节细化其服务端模型、能力门禁、私有材料和发布路径，所有能力仍待实施。

### 13.1 当前缺口

- `web/src/modules/proxy/components/node-editor/NodeProtocolFields.vue` 的传输选项只有 tcp/ws/grpc，安全方式只有 none/tls。
- `pkg/contracts/desiredstate/ProtocolSettings` 没有 Reality 服务端配置或 xhttp 结构；`ValidateSettings` 明确拒绝其他传输和安全方式，Vision 也只允许 TCP TLS。
- `/protocol-support` 对 VLESS 当前仅声明 none/tls；核心上报的 VLESS 支持不能证明其所有安全/传输组合都可用。
- Agent 的 Xray/sing-box 适配器尚未渲染相应字段。因此直接在前端增加选项会在保存、发布或 Agent 应用时失败。

### 13.2 两条开发链路的关系

```text
外部节点：订阅解析 → 客户端模型 → 加密缓存 → 客户端订阅导出

自建节点：编辑器 → 后端入站模型/校验 → DesiredState
          → Agent 核心渲染/原生校验/应用 → 用户订阅导出
```

两条链路可以复用 Reality/xhttp 的参数规范、组合验证和客户端导出；自建节点还必须增加服务端模型、密钥管理、能力协商与发布回滚。仅完成导入链路，不会让节点搭建页面自动出现可用选项。

### 13.3 推荐实施次序

1. **先做 Xray 自建 VLESS。** 按固定版本分别验证 Reality+TCP、Reality+xhttp、TLS+xhttp，以及实际支持的其他组合；不把外部订阅出现的任意组合直接作为有效服务端配置。
2. **补共享契约和校验。** 为 ProtocolSettings 增加类型化可选结构，明确 xhttp host/path/mode、Reality 握手目标/允许的 server names/Short IDs/密钥等字段，校验 flow、TLS、传输之间的组合和互斥。复用已有协议验证、Vault 和配置版本，不增加不透明 JSON 透传。
3. **补按核心、方向和版本区分的能力。** 扩展现有协议能力矩阵/上报模型，明确哪些入站和出站组合受支持。旧 Agent 缺少相应设置契约时不可选、不可发布；不能仅根据“支持 VLESS”或版本文本推断。
4. **补密钥与渲染。** Reality 使用 X25519 密钥对。面板若负责生成，使用服务端密码学随机源并加密持久化；列表、日志、预览和计划不返回私钥。Agent 接收发布所需的私有材料，沿用安全文件权限、原生校验、健康检查和回滚流程。订阅只携带客户端所需公钥/Short ID/SNI/指纹，不泄漏服务端私钥或完整内部配置。
5. **再接编辑器。** VLESS 下按选中核心能力显示传输和安全选项，提供 Reality 参数和生成密钥入口、xhttp 参数；不支持时说明原因。切换组合时明确处理互斥字段，不静默丢弃用户输入或提交隐藏残留参数。
6. **最后验证用户订阅。** 把自建节点成功发布的参数映射到客户端中立模型，复用 URI/Mihomo/sing-box 导出；继续要求用户授权和已发布凭据，不让草稿配置提前进入订阅。

sing-box 的自建 Reality 作为同一能力框架下的独立适配任务，按固定核心的实际能力验收；xhttp 只有在对应核心确有支持时才开放。GOST 不承载这些用户协议设置。自建 Shadowsocks 2022 若另行纳入，还涉及服务端用户密钥与隐藏身份模型，不能仅复用外部导入的算法白名单便宣称完成。

结构复用边界：DesiredState 的入站设置包含服务端私有字段，节点出站和 subscription/proxy 只包含连接所需客户端字段。可复用类型中的公共部分、验证函数和编码器，但不能把整个服务端结构直接序列化到分享链接。Reality 的握手目标、server names 和 Short IDs 与客户端 SNI/public key/short ID 分别映射；普通 TLS 证书、Reality 密钥和 Vision flow 依已验证组合校验，不能沿用旧 TLS 分支强行要求所有 Reality 配置上传证书。

密钥生命周期建议：首次明确创建或点击生成时生成，普通编辑不重新生成；显式轮换作为单独用户操作，提示客户端订阅需要更新，保存草稿后通过既有发布生效。发布失败恢复上一个成功版本及其密钥，不能用最新草稿密钥覆盖旧 Revision。沿用现有受权凭据和 Snapshot 逻辑，不改变用户隐藏身份。

### 13.4 自建节点完成标准

- UI、服务端校验、能力矩阵、Agent、客户端订阅对每种组合的支持一致。
- 不支持的新设置不会被发给旧 Agent；错误配置原生校验失败并可回滚。
- 临时测试节点能生成合法密钥、发布、建立真实客户端连接，并验证用户认证与已有统计范围。
- 修改 xhttp/Reality 参数后的重新发布、回滚、备份恢复保留正确配置和私有材料。
- URI/YAML/JSON 与服务端实际监听配置匹配；支持差异和省略原因清楚可见。
- 使用真实核心、受控测试服务和临时凭据验证，不能只测试下拉选项存在。

本文档更新仅修改规划与记录，没有修改节点编辑器、后端、Agent、生产节点或数据库。
