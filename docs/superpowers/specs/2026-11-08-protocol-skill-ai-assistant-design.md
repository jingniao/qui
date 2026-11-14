# 协议 Skill 包与 AI 配置向导设计

状态：待 Owner 审批，暂不实施

日期：2026-11-08

## 1. 目标与背景

当前入站和出站表单已经能表达多种协议，但用户需要自行判断域名、SNI、TLS 证书、私钥、可信 CA、Reality 握手目标、Short ID、xhttp 路径和 QUIC 参数的含义。服务端有严格的最终校验，前端也已经有部分 Reality/xhttp 辅助逻辑，但通用表单的字段说明、证书检查和协议之间的依赖关系还不统一。

本项目增加一个“协议 Skill 包与 AI 配置向导”层：

1. 每个协议由版本化 Skill 包描述字段、依赖、说明、示例、校验和可调用动作。
2. 确定性校验仍是配置能否保存和发布的唯一依据。
3. AI 负责解释、补全建议、错误定位和受控动作编排，不直接生成并执行任意命令。
4. Owner 可以在后台配置 AI 提供商、模型和限额；没有 AI 配置时，向导和本地校验仍完整可用。
5. 证书支持导入、解析、匹配检查和登记过的 ACME/证书脚本动作，私钥始终由面板 vault 管理。

## 2. 范围

### 目标

- 在新建或编辑入站、出站、中转和隧道相关协议时，按照协议显示真正需要的字段。
- 给每个字段提供中文说明、示例、格式约束、依赖关系和“为什么要填”。
- 在浏览器端尽早提示错误，在服务端再次执行同等或更严格的校验。
- 粘贴证书后解析 SAN、主题、颁发者、有效期、用途和指纹。
- 粘贴证书和私钥后检查是否匹配，并明确说明 TLS 与 Reality 的区别。
- 支持受控动作：解析证书、生成 Reality 密钥、生成 Short ID、生成 xhttp 路径、检查入口可达性、申请/续期登记过的 ACME 证书。
- 让 AI 根据当前表单、协议 Skill 和节点能力给出结构化建议，并可请求执行白名单 action。
- 对 AI 请求、动作批准、结果和失败写入操作日志，但不记录私钥、密码、Token 或完整证书内容。

### 非目标

- 不允许 AI 执行任意 shell、SSH、远程脚本、文件路径或网络命令。
- 不让 AI 直接修改数据库、配置版本或已发布节点状态。
- 不把私钥、用户密码、订阅 Token、WireGuard 私钥或完整敏感凭据发送给外部 AI。
- 不把 Skill 包当成新的协议实现；协议渲染和最终约束仍由现有 `pkg/contracts/desiredstate` 与 Agent 核心适配器负责。
- 不在第一阶段自动替换现有协议表单，也不改变现有 API 的字段语义。
- 不在第一阶段支持任意第三方证书服务的自由脚本上传。

## 3. 已选方案与取舍

### 方案 A：固定向导和可选 AI

优点是范围小、行为可预测。缺点是协议知识和动作扩展需要改代码，不能满足后续增加协议 Skill 的目标。

### 方案 B：版本化协议 Skill 包运行时 + AI 受控动作编排（本方案）

Skill 包提供结构化 schema、文档、校验提示和 action 声明；面板提供统一的 Skill Registry、Action Registry 和 AI Broker。AI 只能选择 Registry 中已登记且经过权限检查的 action。这样可以复用现有协议校验，同时允许后续增加协议包和证书提供商。

代价是需要设计 Skill 包版本、兼容性、签名/信任边界、动作权限和审计；第一阶段必须限定为内置 Skill 包，不能直接加载任意用户上传的可执行包。

### 方案 C：AI 直接生成完整配置

开发较快，但无法可靠处理证书、协议组合、核心能力和敏感字段，不适合作为主路径。

已选择方案 B，原因是它保留现有确定性安全边界，同时提供用户要求的“协议知识 + 可用输入接口 + 证书动作 + 后台 API 配置”。

## 4. 核心架构

```text
Vue 协议向导
    │ 结构化表单上下文 / 脱敏问题
    ▼
Panel Skill Registry ── Protocol Validator ── desiredstate.ValidateSettings
    │
    ├─ Action Registry ── Certificate / Reality / Reachability adapters
    │
    └─ AI Broker ── Provider Adapter ── OpenAI-compatible / registered HTTP provider
                         │
                         └─ structured response only
```

### 组件职责

#### Skill Registry

加载内置、版本化、只读的协议 Skill 包，按 `skill_id` 和 `version` 查询。Registry 不执行 Skill 包中的代码，只读取 schema、文档、规则和 action 声明。第一阶段 Skill 包随面板二进制发布，后续如需在线更新，必须增加签名清单和回滚机制。

#### Protocol Validator

将 Skill 层的字段错误映射到现有协议模型，并调用 `pkg/contracts/desiredstate` 的最终校验。Skill 校验只能提前提示，不能放宽服务端约束。

#### Action Registry

只登记固定 Go 实现的动作。每个动作有输入 JSON schema、输出 JSON schema、敏感输入标记、所需角色、是否需要二次确认、超时和审计名称。AI 只能请求动作，不能提供实现代码。

#### AI Broker

统一管理 AI 提供商、模型、超时、重试、响应大小和用量限制。向提供商发送的上下文只包含协议 Skill 摘要、非敏感字段、节点能力摘要和已脱敏错误。AI 返回必须是结构化 JSON；无法解析或不符合 schema 时，当作普通提示失败，不改变配置。

#### Credential/Vault boundary

证书私钥和其他凭据继续通过现有 configuration vault 保存。证书解析动作可以在面板本地执行；私钥匹配动作只返回布尔值和摘要，不返回私钥。ACME 私钥由面板本地生成并写入 vault，AI 只看到状态和证书元数据。

## 5. Skill 包格式

Skill 包采用 JSON 或 YAML 静态资源，建议放在 `internal/panel/protocolskills/skills/<skill-id>/`，由 Go `embed.FS` 编译进面板。格式示例：

```yaml
id: vless-inbound
version: 1
kind: protocol-skill
protocol: vless
direction: inbound
title: VLESS 入站
summary: 用户接入协议，支持 TCP、WebSocket、gRPC、TLS、Reality 和 xhttp。
fields:
  - path: settings.security
    type: enum
    required: true
    values: [none, tls, reality]
    help: 安全方式决定是否使用普通 TLS 证书或 Reality 参数。
    sensitive: false
  - path: settings.certificate_pem
    type: pem-certificate
    required_when: settings.security == tls
    help: 填写服务端证书 PEM；Reality 不填写此字段。
    sensitive: true
  - path: settings.private_key_pem
    type: pem-private-key
    required_when: settings.security == tls
    help: 填写与证书匹配的私钥 PEM。
    sensitive: true
actions:
  - id: parse_certificate
    input_schema: certificate_input_v1
    confirmation: none
  - id: check_certificate_key_match
    input_schema: certificate_key_input_v1
    confirmation: none
  - id: generate_reality_keys
    input_schema: empty_v1
    confirmation: none
  - id: check_inbound_reachability
    input_schema: reachability_input_v1
    confirmation: none
warnings:
  - when: settings.security == reality && settings.certificate_pem != ''
    message: Reality 不使用普通 TLS 证书。
```

### Skill 包约束

- `id`、`version`、字段路径、action id 和错误代码必须稳定。
- 不允许 Skill 包包含 Go、shell、JavaScript、模板执行代码或远程 URL。
- 字段 schema 不能保存密钥值，只能声明 `sensitive`、`write_only` 和 `redact` 属性。
- Skill 包必须声明适用方向、协议、核心能力条件和最低 contract version。
- Skill 包内容进入版本控制，并由测试校验字段引用、action 引用和错误代码完整性。

## 6. 首批协议知识范围

第一阶段覆盖现有表单和服务端已支持的组合：

- SOCKS5、HTTP、Shadowsocks：地址、端口、认证、方法和 TCP/UDP 边界。
- VLESS：TCP、WebSocket、gRPC、TLS、Reality、xhttp、Vision 的依赖关系。
- Trojan：TLS 证书、私钥、SNI、传输方式和密码。
- Hysteria2、TUIC：原生 QUIC、TLS 证书、UDP 参数和可信 CA。
- WireGuard：本地私钥、地址、peer 公钥、endpoint 和允许地址。
- GOST forward/relay：转发目标、TLS relay 证书、客户端证书、CA 和服务器名称。

协议 Skill 只描述用户可填写的字段和安全语义；核心是否支持某组合仍以节点 Hello/Capabilities 和 `server_settings_combinations` 为准。

## 7. 表单设计

### 向导入口

在入站、出站和相关中转编辑器中增加“配置向导”区域。现有高级字段仍保留，但默认先显示协议向导视图，避免破坏已有配置。

向导步骤：

1. 选择协议、方向和用途。
2. 选择节点和核心，展示该节点实际支持的组合。
3. 选择传输和安全方式，只展示合法组合。
4. 填写身份、域名、证书和传输参数。
5. 执行本地检查：字段依赖、PEM、证书/私钥匹配、域名和端口格式。
6. 预览将保存的结构化配置和缺失项。
7. 保存草稿；发布仍走既有配置版本流程。

### 证书区域

- 支持粘贴 PEM、选择已登记证书、或调用 ACME action。
- 显示证书 Subject、SAN、颁发者、有效期、剩余天数、指纹和是否包含当前 SNI。
- 私钥输入只显示“已填写/未填写/与证书匹配/不匹配”，不回显内容。
- 编辑已有配置时，空私钥继续表示保留旧值，符合当前 `mergeSettings` 行为。
- Reality 选择后隐藏普通证书字段，改为握手目标、服务器名称、Short ID 和公钥；明确提示 Reality 不使用普通证书。
- Hysteria2/TUIC 选择后强制显示 QUIC/TLS 说明和证书要求。

### AI 辅助区域

提供“解释当前字段”“检查当前配置”“给出修复建议”三个只读入口，以及“请求执行动作”入口。AI 输出必须显示：

- 使用的 Skill id/version；
- 依据的节点核心能力；
- 建议修改的字段路径；
- 修改前后值的脱敏摘要；
- 是否需要 Owner 确认；
- 任何未确定项。

AI 建议默认只写入临时草稿，不直接保存、发布或申请证书。

## 8. Action 目录

首批只实现以下固定动作：

| Action | 作用 | 是否写入 | 确认 |
| --- | --- | --- | --- |
| `parse_certificate` | 解析 PEM 元数据 | 否 | 否 |
| `check_certificate_key_match` | 检查证书与私钥匹配 | 否 | 否 |
| `generate_reality_keys` | 生成 Reality 密钥对 | 草稿 | 否 |
| `generate_short_id` | 生成 Short ID | 草稿 | 否 |
| `generate_xhttp_path` | 生成安全路径 | 草稿 | 否 |
| `check_inbound_reachability` | 检查面板到入口的连通性 | 否 | 否 |
| `request_acme_certificate` | 按登记的 ACME provider 申请证书 | vault/任务 | 是 |
| `renew_acme_certificate` | 续期已登记证书 | vault/任务 | 是 |
| `list_certificate_providers` | 列出已登记证书来源 | 否 | 否 |

`request_acme_certificate` 和 `renew_acme_certificate` 只能调用面板内置 provider adapter。Provider 配置包含域名、验证方式、账号引用和存储位置，但不允许前端或 AI 传入任意命令、脚本路径或额外参数。

## 9. AI Provider 配置

系统设置增加 Owner 专用的“协议助手与 AI”区域，第一阶段支持：

- OpenAI-compatible Chat/Responses HTTP API；
- 自定义 HTTPS JSON provider，必须由 Owner 登记 base URL、认证方式、模型和响应 schema；
- 可选的本地/内网 provider，仍要求 HTTPS 或明确的受信本地地址策略。

配置字段：provider id、显示名称、base URL、模型、API key、超时、最大输出、每日请求上限、启用状态和允许的用途。API key 使用 configuration vault 加密保存，读取接口只返回是否已配置和末尾掩码。

安全限制：

- 只允许 HTTPS；开发环境可显式启用本地 HTTP provider，但必须显示警告。
- 默认不发送证书 PEM、私钥、密码、Token、WireGuard 私钥、完整订阅链接或完整配置快照。
- 所有外发字段经过敏感路径过滤和长度限制。
- 请求失败、超时、限流或返回非结构化内容时，不改变草稿和已发布配置。
- AI 请求和 action 结果记录摘要，不记录 prompt、私钥和完整响应正文。

## 10. 后端 API 草案

### Skill 查询

- `GET /api/v1/protocol-skills`
- `GET /api/v1/protocol-skills/{id}`
- `GET /api/v1/protocol-skills/{id}/actions`

Viewer 可读取不含敏感值的 Skill schema 和说明；只有 Owner 可执行 action 或读取 provider 状态。

### 本地校验和证书检查

- `POST /api/v1/protocol-assistant/validate`
- `POST /api/v1/protocol-assistant/certificate/parse`
- `POST /api/v1/protocol-assistant/certificate/match`
- `POST /api/v1/protocol-assistant/reality/keys`
- `POST /api/v1/protocol-assistant/reachability`

这些接口返回结构化错误：`path`、`code`、`message`、`severity` 和可选 `suggested_value`。敏感值不出现在响应和日志中。

### AI 会话和动作

- `POST /api/v1/protocol-assistant/chat`
- `POST /api/v1/protocol-assistant/action-preview`
- `POST /api/v1/protocol-assistant/action-execute`
- `GET /api/v1/protocol-assistant/operations/{id}`

`chat` 只能返回建议和结构化 patch；`action-preview` 只校验权限、参数和影响范围；需要写入、申请或续期的 action 必须由 Owner 明确确认后调用 `action-execute`。所有 action 使用幂等 request id，并有超时和取消状态。

### Provider 管理

- `GET /api/v1/system/ai-providers`
- `POST /api/v1/system/ai-providers`
- `PUT /api/v1/system/ai-providers/{id}`
- `DELETE /api/v1/system/ai-providers/{id}`
- `POST /api/v1/system/ai-providers/{id}/test`

Provider 的 API key 永不通过 GET 返回；删除和替换 provider 必须写操作日志。

## 11. 数据与迁移

建议新增迁移，保持现有 `system_settings` 与配置 vault 模式：

- `ai_providers`：非敏感 provider 元数据、状态、revision、限额；API key 加密列或 vault 引用。
- `protocol_assistant_operations`：请求 id、动作、Skill 版本、操作者、状态、错误代码、开始/结束时间、脱敏摘要和审计引用。
- `protocol_assistant_usage`：按 provider/model/日期记录请求数、失败数、输入输出估算和限流计数，不保存 prompt 正文。
- `certificate_orders`：ACME 申请/续期任务、域名摘要、provider、状态、证书指纹、有效期和失败代码；私钥不入普通列。

所有新表都必须加入备份/恢复校验、迁移 checksum 测试和降级兼容检查。Skill 包本身随二进制发布，不进入数据库。

## 12. 权限和审计

- Viewer：可读 Skill 文档、字段说明和非敏感协议能力；不能调用 AI、读取 provider 配置或执行 action。
- Owner：可配置 provider、使用 AI、执行无写入检查 action，并确认证书申请/续期等写入动作。
- 任何 action 都检查当前用户、节点、协议方向、核心能力和草稿上下文，不能只依赖前端传入的权限字段。
- 审计记录包括操作者、action、Skill 版本、provider/model 摘要、目标资源、结果和固定错误代码。
- 审计禁止包含 API key、私钥、密码、Token、完整 PEM、完整 URI 和完整 AI prompt/response。

## 13. 失败和回滚语义

- Skill 不存在、版本不兼容或 schema 无效：拒绝请求，不改变草稿。
- AI provider 不可用：保留本地校验和向导，返回固定错误码，不阻塞普通协议保存。
- AI 返回非法 patch：只显示解析错误，不应用任何字段。
- action 参数不合法：在 preview 阶段拒绝。
- 证书申请失败：保留旧证书和旧配置，任务记录失败原因，不能覆盖现有有效证书。
- 证书申请成功但写入 vault 或配置失败：任务进入恢复状态，恢复旧引用并提示 Owner。
- 证书续期成功后仍需通过入口校验和既有发布流程，不能自动发布到所有节点。
- AI 建议不推进配置 revision；只有 Owner 保存草稿和发布配置时才进入现有部署链路。

## 14. 分阶段实施计划（仅规划）

### Phase 0：契约和威胁模型

- 固定 Skill schema、Action schema、敏感字段标记和错误代码。
- 列出所有不可外发字段和所有允许的 provider 地址策略。
- 为现有协议模型建立字段路径映射。

### Phase 1：本地协议向导

- 内置 VLESS、TLS、Reality、xhttp、Trojan、Hysteria2/TUIC、WireGuard、GOST Skill 包。
- 接入现有 `protocol-support`、`desiredstate.ValidateSettings` 和节点能力。
- 前端重构证书区域、依赖提示、缺失项和结构化错误展示。
- 实现证书解析、证书/私钥匹配、Reality key/Short ID/xhttp path 生成。
- 不接入外部 AI，不申请真实证书。

### Phase 2：Provider 配置和只读 AI

- 增加 provider 配置、vault 保存、连通性测试和限额。
- 增加 AI Broker 和结构化 `chat`。
- AI 只解释当前表单、返回脱敏 patch 和校验建议。
- 增加请求日志摘要、限流和失败恢复。

### Phase 3：受控 Action

- 接入 action preview/execute、Owner 二次确认和幂等任务。
- 开放本地生成动作、可达性检查和证书元数据动作。
- 完成操作日志、备份恢复和取消/超时状态。

### Phase 4：ACME/证书 provider

- 先实现一个固定、可测试的 ACME provider adapter。
- 增加申请/续期任务、DNS/HTTP challenge 的明确配置模型。
- 证书成功后只更新 vault 和草稿引用，是否发布仍由 Owner 决定。
- 在隔离主机上完成续期失败、旧证书保留、恢复和回滚验收。

### Phase 5：真实核心与生产门禁

- 覆盖 Xray、官方 sing-box、qui sing-box 的协议向导输出。
- 覆盖 Reality/xhttp、QUIC、GOST TLS relay、证书轮换和节点能力不匹配。
- 增加全量 Go/web 检查、真实核心检查、备份恢复和敏感信息扫描。

## 15. 验收标准

### 功能

- 每个首批协议都能从 Skill schema 生成字段说明、依赖提示和校验结果。
- 普通 TLS、Reality、xhttp、Hysteria2/TUIC 的域名和证书填写路径不会混淆。
- 证书解析能准确显示 SAN、有效期和指纹；证书/私钥不匹配时不能保存为可发布配置。
- 节点核心能力不足时，向导说明原因并保留草稿，不伪造可用能力。
- AI 返回非法结构、provider 超时或达到限额时，不改变配置。
- 受控 action 只能执行已登记动作，任何写入动作都有 Owner 确认和审计记录。

### 安全

- API key、私钥、密码、Token、完整 PEM 和完整 prompt/response 不进入普通日志、操作日志、AI 请求或备份明文。
- 无任意 shell、SSH、URL 重定向脚本或任意文件路径执行通道。
- provider 只允许 HTTPS 或显式允许的本地地址。
- 证书申请失败和服务重启不会破坏旧证书或已发布配置。

### 兼容性

- 未配置 AI provider 时，旧表单和本地协议保存流程继续工作。
- 旧 API 字段、空私钥保留语义和现有配置发布/回滚行为保持不变。
- Skill 包升级不改变已保存配置；已保存的 Skill id/version 只用于解释和审计。
- 备份恢复后 provider 引用、证书元数据、操作状态和限额数据可校验；密钥仍由 vault 保护。

## 16. 待确认事项

1. 第一版是否只支持 OpenAI-compatible provider，还是同时允许自定义 HTTPS JSON provider？
2. ACME 第一版是否只支持 HTTP-01，还是需要 DNS-01？DNS-01 会影响 provider 凭据模型和权限边界。
3. AI 是否允许读取当前节点的公开能力、公开地址和公开证书元数据，还是只允许读取本地表单字段？
4. 是否需要把协议 Skill 包做成可在后台上传的扩展，还是先固定随版本发布？本规划建议先固定随版本发布。
5. AI action 是否默认只生成草稿 patch，还是允许 Owner 确认后直接保存草稿？本规划建议允许保存草稿，但始终禁止自动发布。

## 17. 实施前置条件

本文件获 Owner 审批后，下一步再编写分阶段实施计划，并按项目门禁先补契约测试和敏感信息测试。当前不创建迁移、不修改 Go/TypeScript、不接入外部 AI、不执行证书申请。
