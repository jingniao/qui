# 客户端策略 UI 对应后端开发规划

状态：规划，尚未实施。日期：2026-11-04（America/Los_Angeles）。

审查基线：`feat/v1.4-tunnels`，HEAD `8134314`，数据库迁移头 `0131`，包含本机尚未提交的客户端策略 UI 调整。以下描述区分现有实现和建议实现，不作为已交付能力说明。

依据：

- 当前 `ClientPolicyPage.vue`、`routing/api.ts` 与 `web/docs/client-policy-change-log.md`（截至轮次 33）。
- 本次对话中关于客户端运行参数、订阅和节点策略的约定。
- [Clash 配置内容简介](codeg://session/384) 中的三模块职责、用途组→地区组结构及页面联动约定。
- `internal/panel/subscription/`、`internal/panel/policy/` 和现行 AGENTS.md。

本次只做源码审查、前端静态检查和规划，不修改前后端功能。未执行已登录浏览器逐页走查；本规划不把其他会话中的概念示例当作当前后端能力。

## 1. 推荐的产品定义

保留当前“配置卡片 → 六个编辑模块”的 UI，定位为**客户端订阅配置模板管理**。允许保存多份配置，当前仍只有一份全局启用配置，不增加按用户分别绑定模板的功能。

六个模块为：基础设置、代理组、分流规则、DNS、规则集、YAML 预览。

三个主要模块的职责：

| 模块 | 管理内容 | 示例 |
| --- | --- | --- |
| 规则集 | 识别流量的域名、后缀和 IP 名单，或者外部 provider 描述 | OpenAI 规则集 |
| 分流规则 | 匹配条件、处理目标与执行顺序 | OpenAI 规则集 → AI 服务 |
| 代理组 | 候选节点/其他组，以及选择方式 | AI 服务 → 美国组 → 美国组内的授权端点 |

“用途组”和“地区组”均为既有 Clash proxy-groups 的管理分类，不新增一套分流引擎。组名不产生流量识别或地区过滤效果。必须有分流规则把流量送入用途组，地区组必须有真实的成员约束。

基础设置和 DNS 可以继续保留：它们控制用户客户端加载的配置。运行模式、混合端口、allow-lan、日志和 IPv6 均不是面板服务器或 Agent 的设置。使用 Rule 作为默认模式；若仍允许 Global/Direct，提示该模式下分流规则不参与正常规则匹配。客户端本地覆盖配置时以客户端最终采用的配置为准。

订阅模板保存/启用成功，表示后续订阅生成采用它；用户更新并加载订阅后才应用。UI 使用“已设为订阅模板，客户端更新订阅后生效”，不使用“已下发到所有客户端”。

## 2. 源码检查结果与优先级

| 优先级 | 当前 UI / 代码 | 实际缺口 | 处理方向 |
| --- | --- | --- | --- |
| P0 | 多配置卡片、新建、启用、删除 | `savedPolicies` 和 `activePolicyId` 仅存 localStorage；后端只有一个 client_rules 值 | 增加服务端配置库与全局启用指针 |
| P0 | 新配置从 version=0 保存，旧卡片带各自 version 启用 | 版本实际属于全局模板；与卡片版本混用会产生 stale_rules | 分离配置自身 revision 和全局 activeRevision |
| P0 | “美国节点”等地区组 | `addNewRegionGroup` 和标准拓扑仍写入 @all；组名并不约束导出成员 | 实际节点组引用、授权端点选择及持久化筛选 |
| P0 | “同步匹配节点” | 把服务器名称写入 members；后端校验不支持任意服务器名作为组成员 | 改为稳定端点引用，禁止直接持久化显示名称 |
| P0 | 延迟、刷新测速、最终节点、可用数量 | 名称哈希生成延迟；不存在节点时补虚构地区节点；数量/状态有固定推导 | 移除模拟值，展示配置候选与“由客户端运行时决定” |
| P0 | YAML 预览 | 请求全局启用模板，未传正在编辑的卡片/草稿；只使用响应 body，丢弃 warnings | 增加无副作用的草稿/指定配置预览，展示诊断 |
| P0 | 空地区组 | `rules.Expand` 及 Clash 导出器两处会退化到 DIRECT | 增加明确的空组处理策略；新增严格地区组默认 REJECT |
| P1 | 用途/地区分类、包含/排除关键词 | 分类靠名称猜测，筛选保存在以数组下标为键的临时状态中 | 持久化分类元数据和执行用筛选条件 |
| P1 | “编辑规则” | 一条关联规则直接开抽屉，多条才跳列表，零条直接新建 | 按关联会话统一进入规则模块并筛选该组，零条显示空态及添加入口 |
| P1 | 全局规则集编辑/克隆 | 已调用 service-sets API，即时保存到共享规则集；并非随配置弹窗一起保存 | 保留复用，明确共享作用域、保存时机和引用影响 |
| P1 | 保存失败/冲突 | 先改本地卡片再请求；stale_rules 后 load() 替换编辑内容；成功回执版本未统一同步所有副本 | 成功后更新视图；冲突保留草稿，显示服务端新版本 |
| P1 | 外部规则集 | 是客户端拉取的 provider，面板没有对应客户端拉取状态 | 显示来源、格式、间隔，不制造已更新/可用指标 |
| P2 | 节点、节点组、用户、规则集选择器 | 只取第一页（部分固定 100 条）；模板预览静默使用首个用户 | 正确分页/搜索；显式标注自动选择的预览用户 |

定位证据（行号仅对应本次基线，后续以函数名定位）：

- `ClientPolicyPage.vue`：`loadSavedPolicies`（2351）、`enablePolicy`（2412）、`saveAll`（4317）。
- 同文件：`getNodeSimulatedLatency`（2711）、`refreshDelays`（2734）、`resolveSymbolicNode`（2885）、`getRegionNodeStats`（2940）。
- 同文件：`getMatchedNodesForGroup`（3021）、`applyMatchedNodesToGroup`（3036）、`addNewRegionGroup`（3145）、`loadPreview`（4453 附近）。
- 关键词分隔表达式当前为 `/[,，s]+/`，会把字母 s 当分隔符；空包含条件还会提前返回并忽略排除条件。执行语义应统一由后端定义，前端仅辅助预览。
- `internal/panel/subscription/rules/validate.go` 的 `validateMember` 只接受内建动作、符号来源和其他代理组；`node:<id>` 当前是规则 target 语法，不是组成员语法。

## 3. 已有代码的复用地图

| 能力 | 复用入口 | 计划改动 |
| --- | --- | --- |
| 模板字段 | `subscription/rules/model.go` 的 Template/Client/DNS/Group/Provider/Rule | 保留现有结构，仅增加必要可选字段 |
| 全模板校验 | `rules.Validate`、`FieldError`、组环检测、`Ordered` | 扩展成员与筛选校验，沿用字段错误路径 |
| 单模板读写/CAS | `rules.Store.Read/Save` | 抽出事务内写入方法，供配置库复用 |
| 现有模板接口 | `subscription/rules_handler.go` 的 GET/PUT /subscription-rules、presets | 保持旧接口，统一接入配置服务 |
| 预设 | `rules.Presets` | 用于创建配置的初值；UI 不再拼第二套预设 |
| 用户授权与代理来源 | `subscription.Service.account/collect/Proxies`、source.Registry、published/importer | 为预览和成员展开复用，不复制授权 SQL |
| 组成员和规则展开 | `rules.Expand` | 在已授权端点上筛选，返回真实结果和诊断 |
| YAML | `format/clash` 的 Meta/Legacy、typed document、yaml.v3 | 共用渲染；前端不拼完整 YAML |
| 域名/IP 名单 | `policy.Repository` 的 Service/Services/SaveService/DeleteService | 继续使用 service_sets/service_set_entries |
| 面板 provider | `ClassicalRuleSet`、GET /sub/{token}/rules/{id} | 保留 Token 授权和路径，补引用诊断 |
| 前端 DTO 适配 | `routing/api.ts` 的 toConfig/fromConfig | 扩展完整往返映射，不静默丢字段 |
| 现有模拟器 | `routing/api.ts` 与 client-policy-selftest | 复用规则匹配，保留运行时无法确定的结果 |
| 权限/审计/存储 | auth.Require、CSRF、system 审计、SQLite、现有备份恢复框架 | 增加配置库操作的接线和验证 |

实现仍放在 `internal/panel/subscription/` 及其 `rules` 包，跨模块接口由 `internal/platform/server` 注入。无需新增独立服务、缓存系统、消息队列或 Agent 命令。

## 4. 多配置的存储与生效流程

### 4.1 最小新增存储

建议新增一张 `client_policy_profiles` 表，避免将全部配置塞进一个大 JSON 并共用同一个编辑锁：

```text
id                 稳定服务端 ID
name               配置名称
description        配置说明
template_json      既有 rules.Template（含必要的可选扩展）
ui_metadata_json   分组分类等管理元数据，不写入 Clash YAML
revision           本配置的乐观锁版本
created_at / created_by
updated_at / updated_by
```

继续保留 `system_settings.client_rules` 作为当前订阅使用的生效快照。另以一个 system_settings 元数据项保存 `activeProfileId` 与对应 `profileRevision`，不往旧 `rules.Stored` 中塞未定义字段。

配置库负责持久保存多份模板；生效快照是它的受控投影。所有写入都经过同一个配置服务，避免两份数据各自写入。

建议首版上限：50 份配置；名称 64 字、说明 500 字；每份模板沿用现有 64 组、2000 条规则、32 个 provider、每组 256 成员的限制。继续执行现有 HTTP 请求体大小限制，列表不默认返回整份模板。

### 4.2 保存、启用、删除

| 操作 | 建议行为 |
| --- | --- |
| 新建/保存未启用配置 | 校验完整模板，写配置库，不改变订阅 |
| 保存当前启用配置 | 同一事务更新配置和 client_rules 生效快照；符合当前 UI 的保存即更新模板语义 |
| 启用另一份配置 | 校验引用及指定版本，同一事务更新启用指针和生效快照 |
| 删除未启用配置 | 检查配置 revision 后删除 |
| 删除启用配置 | 返回 409，先启用其他配置 |
| 创建并启用 | 服务端提供原子动作；创建成功、启用失败不能返回一个模糊的成功状态 |

版本明确分成两种：

- `revision`：某份配置的编辑版本。
- `activeRevision`：复用当前 `client_rules.revision`，保护全局启用状态和订阅生效快照。

启用动作同时携带 `expectedRevision` 与 `expectedActiveRevision`。切换到不同配置时，即使模板内容相同，也推进 activeRevision，防止并发切换被当成同一次状态。普通内容未变化保存保留既有 no-op 语义。

保存当前启用配置也校验 activeRevision；若它已被另一位管理员切走，返回冲突，不猜测用户是否还想启用。配置自身的 CAS 不能用全局版本代替。

事务内复用存储校验和写入方法，不在事务中再次调用会创建事务的 Store.Save。成功回包后才替换前端卡片，卡片内容、revision、activeRevision、updatedAt 均使用服务端返回值。审计记录使用既有操作日志，确保事务结果和响应不矛盾。

### 4.3 兼容与数据迁移

- 当前迁移头为 0131；实施时新建下一个未占用迁移（若仍未占用则为 0132），不修改历史迁移。
- 旧 client_rules 内容保留；幂等初始化时复制为第一份已启用配置，保留其当前生效 revision。没有旧模板时使用服务端默认预设。
- 旧 GET /subscription-rules 继续返回当前生效模板；旧 PUT 通过同一服务更新对应的启用配置和生效快照，不能形成绕过配置库的第二条写路径。
- 浏览器里的 v2 卡片无法由数据库迁移直接读取。前端提供“一次性导入本机配置”，预览名称/数量后调用普通创建接口；逐份校验、返回成功/失败明细，默认全部作为未启用配置。
- 导入使用本地旧 ID 加内容摘要生成幂等导入标识，重试不重复创建。未确认导入成功前保留 localStorage；绝不让本机 activePolicyId 覆盖服务端启用状态。
- 新字段、表、启用指针与生效快照一起进入备份清单、恢复验证和一致性检查；恢复旧归档时初始化默认配置，恢复新归档时核对指针与快照，不自行生成新的启用行为。

## 5. 代理组与地区筛选

### 5.1 先复用现有成员表达

现有以下内容直接复用：

```text
DIRECT / REJECT
另一个客户端代理组名称
@all
@nodeGroup:<id>
@source:<id>
```

例如已有“美国入口”节点组时，地区组直接引用 `@nodeGroup:<id>`。实际展开仍取用户被授权的具体入站和导入成员；不会因引用服务器或节点组名称而增加授权。

代理组分类建议存在 profile 的 `ui_metadata_json` 中（service / region / other），不再每次按名称猜测。已有模板首次打开可推断初值，保存后以明确分类为准。首版不引入新的分组数据库表或双层专用执行模型。

### 5.2 补齐当前 UI 所需的端点选择与关键词筛选

服务器列表不是用户的可用代理端点列表：一台服务器可能有多个入站；还存在导入端点和已发布的中转规则端点。

新增只返回元数据的候选查询，复用现有订阅 source/proxy 流程，返回稳定引用、显示名称、协议、来源、所属节点组及诊断，不返回口令、URI 或 Token。

精确成员增加 `@endpoint:<opaqueRef>` 表达，引用由后端生成并校验：

- 自建入口：使用 inbound ID。
- 中转端点：使用 rule ID，和原始入口区分。
- 导入入口：使用 source ID + 源内 `name|protocol` 稳定键，不用刷新后可能变化的内容哈希 ID。
- 复用中立代理的 Origin 元数据；必要时在 Origin 中增补稳定引用字段，保持该包不依赖用户、数据库和 HTTP 模块。
- 客户端链的生成端点继续沿用现有支持；候选层必须明确区分可选入口与链内部克隆跳，不把后者当作普通入口。

Group 增加可选的 `includeKeywords`、`excludeKeywords` 字符串数组：

1. 先收集该用户已授权、已发布、通过协议过滤的代理。
2. 按成员来源收集候选，去重并保留确定性顺序。
3. 包含词为空表示不限制；非空按任一关键词匹配。
4. 排除词始终生效，优先排除命中项；首版使用规范化的不区分大小写字面匹配，避免任意正则。
5. 只对候选端点应用过滤，不用节点名称替代用户授权，也不靠组名自动添加隐含过滤。
6. 导出前仍执行格式能力过滤及最终名称消歧。

关键词条件需要进入 Template 并由后端 Expand 使用，不能只存在 UI 元数据里。新增可选字段同步更新严格解码、验证、前端 toConfig/fromConfig、备份检查与测试。

地区标签和名称匹配只能表达管理员组织的候选集，不能证明实际公网出口所在地。需要“AI 只用美国”时，使用明确的美国成员集合，且用途组不提供其他地区/DIRECT 备选。

### 5.3 空组与默认选择

当前实现会在授权展开和格式过滤后将空组改为 DIRECT。新增 `emptyPolicy: direct | reject`（缺省按旧行为 direct），新建的严格地区组默认 reject，并在两处空组处理均保留该策略和诊断。不可只在 Expand 改一处。

“首选出口”继续复用 members 顺序：select 组将首选放首位。该顺序用于配置建议，不代表客户端当前选中项；客户端可能保留本地选择。

url-test / fallback 实际用哪个节点由客户端运行时决定，不能递归取第一个成员就标成“最终节点”。面板可以展示“配置候选路径”“授权后成员数量”，实际状态默认未知。

本期不新建客户端远程控制通道。移除模拟测速和虚构地区节点；“刷新延迟”调整为“刷新候选”，或在没有客户端测量能力时明确不可用。既有节点 TCP 探测只能标为服务器视角，不能转成客户端延迟。

## 6. 分流规则、规则集与页面联动

### 6.1 分流规则

沿用 Rule 的 id/type/payload/target/priority/enabled/noResolve/description。客户端页面的顺序就是 priority 升序；稳定保留同优先级原顺序；已启用规则有且仅有一条最后的 MATCH。

- 拖动、复制、批量录入和上下移动继续在前端生成完整模板，不为每次拖动增加后端接口。
- 筛选状态禁止重排全局顺序；从用途组进入时显示目标筛选标签和“查看全部”。
- “编辑规则”始终进入该组的关联列表；新建规则预选 target。规则集的“用于分流”预选 RULE-SET 和 payload。
- 名称/备注不产生匹配效果。首版复用 description 作为可读标签，不为装饰性标题强行新增字段。
- group/provider 改名时，前端同步更新当前模板的引用，服务端校验完整模板；删除被引用对象返回字段级错误或先展示引用清单，不静默改为 DIRECT。
- 批量输入每项生成独立规则，统一去重/优先级整理，并确保不会越过 MATCH；修改已有规则时同样明确是否支持批量展开。

### 6.2 规则集

继续分成已有两类：

| 类型 | 存储 / 复用代码 | 更新方式 |
| --- | --- | --- |
| 面板内置/自建名单 | service_sets + service_set_entries，policyAPI、ClassicalRuleSet | 面板保存名单；客户端按 provider 更新周期获取。节点侧策略需另行发布 |
| 外部 provider | Template.Providers | 客户端按 URL 和 interval 拉取，面板保存描述 |

规则集内容本身不携带 DIRECT、REJECT 或用途组；这些动作放在引用它的 RULE-SET 规则。

共享名单编辑继续直接保存，但弹窗明确提示“修改共享规则集，会影响所有引用”。关闭配置编辑器或放弃模板草稿，不撤销已经保存的共享名单。外部 provider 的编辑则随当前配置保存。

增加引用查询：显示哪些客户端配置/规则和节点策略引用它，区分启用/未启用配置及规则开关。当前 DeleteService 只检查节点策略引用，需要经 platform 注入客户端引用查询接口，补齐删除/禁用保护，检查与写入在一致事务边界完成。

服务端全模板校验可报出缺失/已禁用 provider 对应服务集；订阅读取旧数据时必须给出诊断。现有禁用服务集下载返回 404，客户端可能保留缓存或报错，不能用状态标签宣称旧名单立刻停止生效。

外部 provider 首期不新增面板定时抓取。已有 subscription/importer 是代理节点来源，不能直接拿它解析 domain/classical/mrs 规则文件。若后续要显示外部规则条数和更新时间，再单列解析/缓存能力需求；当前显示“由客户端更新 / 面板未观测”。

Provider 配置弹窗继续展示 Token 占位符和用途说明，不将真实订阅 Token 存进浏览器方案库或变更日志。

## 7. YAML 预览与诊断

增加“对指定模板生成预览”的服务方法，例如 `PreviewTemplate`，让现有 Preview 与它共用 account → collect → Expand → exporter.Render 流程。预览中传入模板参数，禁止暂时修改 Service.Rules 全局字段或先保存再回滚。

支持两个输入来源：指定 profileId/revision，或者当前未保存 template；二选一。两者都不改变配置、启用指针、订阅 Token 或最近获取时间。

真实节点列表仍按具体用户授权生成。保留你去掉常驻用户下拉框的布局：可以自动选择一个可预览用户，但响应必须返回预览用户标识，页面显示“按用户 X 的授权生成”；需要核验其他用户时放入高级预览上下文，不静默把一个用户的结果说成所有用户的结果。

没有可预览用户时返回明确诊断，不生成虚构节点或伪装为完整可运行配置。结构校验仍可单独进行。

返回并展示：

```text
body / contentType / format
profileId / profileRevision（若有）
templateDigest / previewSource（draft 或 saved）
activeRevision（仅作为对照）
context（实际预览用户与自动选择说明）
warnings（阶段、引用对象、说明）
```

warnings 必须包括无授权成员、空组处理、格式省略、失效引用、运行时未知等情况。Owner 预览复用现有不签发 Token 的约定；面板 provider URL 保持占位符。草稿与正式订阅在相同模板/用户/格式下，除了 Token 链接等明确差异外应一致。

现有 GET /users/{id}/subscription-preview 保持其“当前生效订阅”语义，不用它伪装成任意草稿预览。

## 8. 接口规划

以下为建议的新接口，不是现有 API。都在 /api/v1 下；配置管理、用户上下文预览与候选诊断限 Owner，写操作沿用 CSRF。旧 subscription-rules 的既有读取权限保持兼容，不借此向 Viewer 扩大用户数据访问。

| 方法与路径 | 用途 | 复用点 |
| --- | --- | --- |
| GET /client-policy-profiles | 分页列表及全局 activeProfileId/activeRevision | 现有 HTTP 分页与配置服务 |
| POST /client-policy-profiles | 新建，可携带 activate=true 与 expectedActiveRevision | Presets、Validate、事务写入 |
| GET /client-policy-profiles/{id} | 完整模板、元数据与 revision | Template DTO |
| PUT /client-policy-profiles/{id} | 保存，携带 expectedRevision；启用配置同时校验 expectedActiveRevision | Store 事务内写入 |
| DELETE /client-policy-profiles/{id}?expectedRevision=N | 删除未启用配置 | CAS、Owner、审计 |
| POST /client-policy-profiles/{id}/activate | 切换全局启用配置，双版本校验 | client_rules 生效快照 |
| POST /client-policy-profiles/validate | 校验编辑草稿，无保存副作用 | rules.Validate |
| POST /client-policy-profiles/preview | 草稿或指定配置的完整预览 | Service.PreviewTemplate |
| POST /client-policy-profiles/resolve | 草稿在指定用户下的成员展开与路径诊断，返回无凭据元数据 | account、collect、Expand |
| GET /service-sets/{id}/references | 所有已保存模板及节点策略的引用 | 注入引用查询接口 |

resolve 同时服务地区成员预览与配置路径，不另建“最终节点”“即时测速”接口。页面尚未选择用户时可只展示符号来源；展示具体数量时必须标注所用授权上下文。

复用 `invalid_rules`、字段路径、已有统一错误包装。新配置冲突建议为 `stale_client_policy`，全局切换冲突沿用/明确映射 `stale_rules`；删除启用配置为 `client_policy_active`。冲突响应提供当前版本，客户端保留未保存草稿，禁止自动覆盖。

## 9. 分阶段实施顺序

| 阶段 | 工作 | 完成标准 |
| --- | --- | --- |
| A：状态与配置库 | 新表、初始化、配置 CRUD、启用事务、旧 API 兼容、本机卡片导入、基础备份接线 | 换浏览器仍看到相同配置和唯一启用状态；创建/启用/保存无版本串用 |
| B：真实组成员 | 复用 @nodeGroup/@source；补稳定端点、关键词筛选、分类元数据、空组策略；去掉模拟数据 | 美国组按成员约束生成；缺授权/缺节点按明确策略处理；不出现虚构状态 |
| C：预览与联动 | 草稿预览、成员 resolve、warnings、可见预览上下文、统一编辑规则跳转、失败保留草稿 | 编辑未启用配置时预览该配置；保存和预览不串模板 |
| D：共享规则集与收尾 | 跨模板引用、删除/禁用保护、全量选择器分页、恢复一致性验证、文档和回归 | 共享资源改动影响可见；备份恢复后启用关系、授权输出一致 |

每阶段交付都保持现有订阅路径可用。A 阶段可继续只用旧成员语法；B 阶段完成前，依赖新筛选/端点字段的控件应标明待接入，不能宣称已生效。无真实数据来源的指标在接线前就移除或显示未知。

不把客户端遥测/远程控制、每用户独立配置分配、通用 YAML 任意字段编辑、规则集远程内容缓存纳入本次首期。这些功能需要新的协议或产品行为，不能以“补 UI 接口”顺带实现。

## 10. 实施时的文件范围

- `internal/panel/subscription/rules/`：复用并扩展模型、校验、展开、事务存储；新增配置库实现。
- `internal/panel/subscription/`：配置接口、预览/resolve 服务方法、现有接口适配。
- `internal/panel/subscription/source/`、`importer/`、`proxy/`：补稳定端点元数据，保留授权路径和模块依赖边界。
- `internal/panel/subscription/format/clash/`：保持 YAML 结构，补空组策略传播和新字段渲染所需逻辑。
- `internal/platform/server/`：模块接线、跨模块引用查询和集成测试。
- `internal/panel/policy/`：复用服务集 CRUD，仅补跨模板引用/版本与一致性保护。
- `internal/panel/system/backup/`、必要的恢复协调代码、新迁移和迁移校验清单：配置库清单与兼容恢复。
- `web/src/modules/routing/api.ts`、`ClientPolicyPage.vue` 及按职责拆出的配置列表/组编辑等组件：接入 API、状态与字段往返。
- AGENTS.md、架构、协议矩阵、发布说明：实施完成后同步真实行为；本规划本身不提前改写现行规范。

## 11. 验收清单

1. 两个浏览器能看到相同配置列表；切换全局启用配置后都读到相同指针。
2. 两个 Owner 并发编辑同一配置、并发启用不同配置、编辑期间配置被切走，均有确定的 CAS 结果且不丢草稿。
3. 内容相同的不同卡片切换仍有全局版本变化；不修改内容重复保存不会制造无意义版本。
4. 新配置默认不影响订阅；启用配置保存成功后后续订阅采用新模板；失败事务无半写入。
5. 用途组→地区组嵌套正确，无自引用/循环；改名后的规则和组引用完整。
6. 两个授权不同的用户获得不同成员；单服务器多个入站、导入刷新、中转端点均不混淆；无授权用户不能靠 endpoint 引用获得节点。
7. 美国组过滤条件真正影响 YAML；排除词在无包含词时仍生效；无匹配成员和格式过滤后空组都按 emptyPolicy 处理。
8. UI 无名称哈希延迟、固定成功数量、虚构节点和伪实时选择；未知值保持未知。
9. 未启用配置和未保存草稿可预览；预览无写入/Token 签发，显示所用用户和全部 warnings。
10. 正式订阅和对应预览共用生成结果；mihomo YAML 使用真实核心校验，验证 rules、嵌套组、DIRECT/REJECT 和 provider。
11. provider 内容不包含转发动作；共享服务集引用可查；删除/禁用保护包含客户端与节点策略的引用。
12. URI/Base64/sing-box 片段保持既有范围；legacy Clash 继续省略不支持的项并报告，不把不支持内容静默改写。
13. 旧数据库、localStorage 重复导入、新旧备份恢复均保留正确生效配置；无未知字段静默丢失。
14. 修改记录按轮次追加，列出需求、全部文件、前后行为、接口/迁移变化、验证和未完成项，保留返工历史。

复用测试起点：`rules/rules_test.go`、`rules_handler_test.go`、`format/clash/clash_test.go` 与 routing_test.go、`platform/server/subscription_rules_test.go`、`web/scripts/client-policy-selftest.mjs`。新增测试针对并发、授权、真实渲染和恢复，不以源码字符串存在代替行为验证。

实施后按项目约定执行 Linux `scripts/check.sh` 和对应真实核心套件；本地前端检查不替代这些门禁。

## 12. 本次已执行的验证

- `npm.cmd --prefix web run typecheck`：通过。
- `node web/scripts/client-policy-selftest.mjs`：20 项通过，包含模板 DTO 往返与客户端运行时未知语义。
- 未运行生产构建、Go 测试、Linux 门禁、真实 Mihomo 套件或登录后的浏览器走查。
- 以上通过只能证明现有类型与已有自测通过，不能证明新增 UI 已有对应持久化、遥测或草稿预览能力。
