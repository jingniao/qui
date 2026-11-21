# 可选内核节点设计：仅 Agent 与纯 GOST 转发节点

状态：已实施，§7 阶段 5 的 Linux 真实核心验收通过，已随 **v1.9.0** 与自动续费（迁移 0160）发布。实机证据、失败定位、最终门禁与附件核验见[发布说明](release-notes-1.9.0.md)。

## 1. 目标

1. **仅安装 Agent**：安装时不装任何内核，登记后在「内核管理」按需添加。
2. **纯 GOST 转发节点**：只装 GOST，仅承担端口转发与隧道跳，不承载用户。
3. **已有节点可以转为纯转发**：允许移除最后一个 Xray／sing-box（取消 `last_core` 限制），前提是节点上已没有依赖用户内核的对象。

三者合并成一条规则：**节点可以没有「用户内核」（Xray／sing-box）**。没有用户内核的节点称为「无用户内核节点」，可能一个内核都没有，也可能只有 GOST。

## 2. 非目标

- 不让 GOST 承载用户、订阅、额度、设备门控或域名记录。
- 不新增数据库迁移：能力信息继续存于 `nodes.capabilities_json`。

## 3. 现状约束（实施时必须逐项改动）

| 位置 | 现状 | 本设计的改法 |
| --- | --- | --- |
| `deploy/linux/node-install.sh` | 未传 `--core` 时默认 `xray`；GOST 不得排首项 | 新增 `--no-core`；允许只传 `--core gost` |
| `deploy/linux/install.sh` | 首装且无 `--core` 时默认 `xray`；GOST 不得排首项 | 同上，安装记录允许为空列表或仅含 GOST |
| `internal/agent/configuration/config.go` | `cores` 必须 1～3 项，首项必须能承载用户；缺 `cores` 时默认 xray | 允许 `cores: []` 与 `[gost]`；缺 `cores` 字段仍按旧规则默认 xray（兼容旧配置） |
| `configuration/inventory.go` `Merge` | 结果为空时报错；动态追加的内核按字母序排列，`gost` 会排到 `sing-box`/`xray` 之前 | 允许结果为空；主内核固定为第一个用户内核（通过 `Inventory.Primary` 显式记录），GOST 永不成为主内核 |
| `internal/agent/control/control.go` 启动 | 用 `configuredCores[0]` 作主内核，并打开它的安装历史 | 没有用户内核时主内核为空，不打开主内核历史 |
| `internal/agent/core/multi.go` `NewMultiRuntime` | 空集合时报错；`Component("")` 默认返回主内核 | 允许空集合或仅 GOST；`Component("")` 在无主内核时返回 nil，所有调用方显式处理 |
| `control/core_installation.go` `coreComponent`、`coreHistoryPaths` | 主内核为空时当作 xray；`cores[0]` 直接取下标 | 去掉空主内核按 xray 处理的写法；空列表不再取 `cores[0]` |
| `control/core_membership.go` | Agent 侧拒绝移除最后一个用户内核（`last_core`） | 存储格式下限已抬升（§6）时允许 |
| `internal/agent/storage`、`pkg/contracts/agentstorage` | 配置格式 ReadMax 5 | 新增配置格式 6，表示「可能没有用户内核」 |
| `internal/panel/node/multi_core.go` `validateMultiCapabilities` | 多核 Agent 必须上报主内核 | 带新能力标志时允许主内核为空、列表为空或仅 GOST |
| `internal/panel/proxy/multi_core.go` `ValidateInboundCore` | `core_name` 为空时视为旧版未知上报并放行 | 带新能力标志且主内核为空时，拒绝未指定内核的入站 |
| `internal/panel/deployment/core_remove.go` | 最后一个用户内核返回 `last_core` | 改为检查依赖对象（§5.2）与 Agent 能力；`last_core` 只用于不支持本能力的 Agent |

## 4. 协议契约

- 新增能力位 `Capabilities.core_optional_supported = 34`，Agent 支持本设计时上报 true。
- 面板对 `core_name` 为空的处理分两种：
  - 能力位为 true：确认节点没有用户内核，执行 §5 的限制。
  - 能力位为 false：保留现有的「旧版未知上报」放行逻辑，不改变旧 Agent 的行为。
- `Capabilities.cores` 可以为空或只含 GOST；`core_name` 为空，或为某个已启用的用户内核。
- 心跳 `core_healthy` 在没有任何内核时不填写，节点状态按 `online` 计算，不显示「降级」。
- `pb` 由 `scripts/generate-proto.sh` 重新生成；只做加法，不改已有字段号。

## 5. 行为规则

### 5.1 Agent

- **启动**：没有内核时也照常完成登记、mTLS 连接、心跳、证书续期、Agent 网页升级、系统更新、调优、强制出站、延迟与 TCP 探测、整机流量采集。
- **应用配置**：
  - 不含入站的修订（包括升级前的基础修订）正常应用，不启动任何核心。
  - 含未指定内核入站的修订：无主内核时拒绝，错误码 `no_user_core`，保留上一份成功配置。
  - 指定 GOST 的 `forward` 入站与隧道：节点装了 GOST 时照常渲染。
  - 出站、分流、设备门控、访问日志、流量统计：没有用户内核时不渲染、不采集。
- **添加内核**（沿用现有「缺失组件走同一请求安装启用」路径）：
  - 第一个加入的 Xray／sing-box 写入 `Inventory.Primary`，成为主内核。
  - 安装历史使用动态追加的代际命名空间（`core-release-history/<core>/<generation>`），不占用 `release-history` 这个旧主内核路径。
  - 加 GOST 仍要求 `Hello.gost_core_supported`，且仅限 linux/amd64。
- **移除最后一个用户内核**：
  - 先完成 §6 的格式激活检查，再按现有移除流程处理：确认进程退出、更新 `cores.json.enc`、删除运行目录与自管镜像。
  - 移除后清空 `Inventory.Primary`。原主内核的安装历史作为证据保留，不再被选用。
  - 激活检查失败时拒绝，错误码 `fallback_incompatible`，节点保持原状。
- **supervisor**：没有运行时就不探测出站健康，也不做核心恢复。

### 5.2 面板

- **入站**：
  - 无用户内核节点只允许保存显式 `core=gost` 的 `forward` 入站（需已装 GOST）。
  - 其他入站一律 400 `no_user_core`，前端隐藏对应入口。
- **出站、代理链、出口组、分流与网站控制**：节点没有用户内核时拒绝保存，错误码 `no_user_core`。
- **中转线路、协议转换中转、画布方案**：候选节点列表排除无用户内核节点。
- **隧道**：照常要求各跳已启用 GOST。纯 GOST 节点可作任意跳，接收跳仍须有公网地址或连接地址覆盖。
- **节点组**：
  - 服务器成员照常可选，例如把纯转发机放进 `relay` 组统一管理。服务器成员本来就不影响订阅（§10.1）。
  - 无用户内核节点没有用户入站，自然不贡献订阅条目。
- **移除最后一个用户内核**：`POST /api/v1/nodes/{id}/cores/{component}/remove`，沿用 Owner、CSRF、`confirmNodeId` 和当前确认修订的要求。
  - Agent 未上报 `core_optional_supported` 时，仍返回 `last_core`。
  - 节点上有下列任一对象时返回 409 `user_core_in_use`，响应列出全部阻塞对象 ID：
    - 非 GOST 入站（草稿或已发布，启用或停用都算）；
    - 本节点出站、出口组、代理链跳；
    - 以本节点为中转或出口的中转线路与画布方案。
  - 其余拒绝码（`core_pending`、`revision_mismatch`）不变。
  - Agent 报告 `fallback_incompatible` 时，任务按失败结束，并显示原因。
- **Agent 降级保护**：节点能力位为 true 且没有用户内核时，拒绝把 Agent 升级或回退到不支持本能力的版本，错误码 `agent_version_unsupported`。

### 5.3 安装器

- `node-install.sh`：
  - 新增 `--no-core`，与 `--core` 互斥，同时给出时报错退出。
  - 允许只传 `--core gost`，仍仅限 linux/amd64。
  - 安装结束提示「未安装用户内核，可在面板 内核管理 添加」。
- `install.sh`：
  - 首装带 `--no-core` 时不下载核心，安装记录的核心列表为空。
  - Agent 配置写入显式 `cores: []`（或 `[gost]`），不写旧的 `core_name`/`core_path` 标量，并直接按配置格式 6 建立存储下限。
  - `qui-update` 升级沿用记录中的列表，不补默认 xray。
- 默认行为不变：不带 `--core` 也不带 `--no-core` 时仍装 xray，保证旧命令和文档继续有效。

### 5.4 界面

- **安装弹窗**（`ServerEnrollment.vue`）与待登记节点的「内核管理」（`NodeVersionPanel.vue`），共用 `coreInstallOptions.ts`：
  - 三个内核都可以不勾，此时说明为「仅安装 Agent，登记后在 内核管理 添加」，命令带 `--no-core`。
  - 只勾 GOST 时说明为「纯转发节点：只做端口转发与隧道，不承载用户（仅 linux/amd64）」。
  - 删除「需同时选择 Xray 或 sing-box」的校验与提示。
- **内核管理移除最后一个用户内核**：
  - 确认框写明「移除后这台机器不再承载用户，只能做 GOST 转发或不运行内核」。
  - 遇到 `user_core_in_use` 时列出阻塞对象，并给出跳转入口。
- **服务器卡片和详情**：运行内核处显示「未安装内核」或「仅 GOST（纯转发）」，不显示为错误或降级。
- **入站、出站相关入口**：对无用户内核节点隐藏，遵循隐藏不可用选项规则；在「内核管理」提示「添加 Xray 或 sing-box 后可创建用户入站」。

## 6. 兼容性与降级锁

- **新增配置格式 6**：`agentstorage.Current` 的配置格式 ReadMax 升到 6，意思是「读者能理解可能没有用户内核的配置与内核清单」。
  - v1.9.0 起的 Agent 都能读，但写入仍保持原格式。只有在下面两种情况才把 Write 抬到 6：
    1. 安装器写入空列表或仅 GOST 的配置；
    2. 移除最后一个用户内核前。
  - 抬升前沿用 `CheckMultiCoreActivation` 的做法：确认所有保留的回退镜像（受管回滚镜像与引导程序）都能读格式 6，否则拒绝（`fallback_incompatible`）。
  - 抬升后，旧版 Agent 启动时会因存储下限明确拒绝，不会误读成「默认 xray」或报「清单为空」。
- **最低兼容 v1.7.5 不变**：保留用户内核的节点配置、修订与能力上报都不变，也不抬升格式。
- **旧 Agent 配合新面板**：不带新能力位，面板保持现有逻辑，包括 `last_core`。
- **安装脚本版本**：命令下载与面板同版本的 `node-install.sh`，所以 `--no-core` 只在 v1.9.0 及以后可用。旧面板生成的命令不受影响。
- **重新加回用户内核**：无用户内核节点之后再添加 Xray／sing-box 完全支持。格式下限不回落，所以这台机器之后仍不能降回 v1.9.0 以前的 Agent。

## 7. 分阶段实施

1. **契约与 Agent**：
   - 能力位、配置格式 6 与激活检查、配置与 inventory 规则。
   - 启动路径、空运行时、所有依赖主内核的调用点（含 `multi_telemetry.go` 的 `Component("")`）。
   - `no_user_core` 拒绝、第一个用户内核成为主内核、移除最后一个用户内核。
2. **安装器**：`node-install.sh`、`install.sh`、`qui-update` 记录沿用，以及 shell 测试桩（需 Owner 同意新增测试）。
3. **面板**：能力校验、入站／出站／策略门禁、候选列表排除、移除规则与 `user_core_in_use`、Agent 降级保护；同步更新项目说明书 §4.4、§11、§12、§22.2、§27。
4. **界面**：安装弹窗、内核管理（含移除确认与阻塞列表）、服务器卡片与详情、入口隐藏。
5. **Linux 测试机实机验证**（真实核心）：
   - 零内核安装 → 登记 → 网页添加 Xray → 创建并发布用户入站 → 订阅可用。
   - 纯 GOST 安装 → 登记 → 作为隧道中间跳和入口跳转发成功。
   - 已有 Xray 节点：
     - 有入站时移除被 `user_core_in_use` 拦住；
     - 清空入站后移除成功，转为纯转发；
     - 再加回 Xray 正常工作。
   - 格式激活：保留旧版回退镜像时移除被 `fallback_incompatible` 拒绝；抬升后手工启动旧版 Agent 被存储下限明确拒绝。
   - 零内核节点执行 Agent 网页升级与系统更新。
   - 零内核节点上，含用户入站的修订被拒并保留原配置。
   - 保留用户内核的已有节点升级后，行为与黄金对照一致。

## 8. 待决问题

无。实施中发现新的边界时，先回报再扩大范围。
