# 协议支持补齐与失败诊断开发记录

## 2026-11-04：规划与源码核对

- 需求：补齐本次来源使用的协议配置及对应失败提示，先提供开发规划。
- 基线：`feat/v1.4-tunnels`，HEAD `c708c35`，迁移头 `0132`。
- 新增文件：`docs/subscription-import-compatibility-plan.md`、本记录文件。
- 阅读范围：订阅导入解析、来源抓取/刷新/状态、Vault 缓存、中立模型、URI/Base64/Mihomo/sing-box 导出、来源管理 UI、备份验证、历史版本证据及本地上游源码。
- 规划内容：固定版本和组合验证、SS2022 密钥/URI 语义、Reality/uTLS、xhttp 参数、结构化诊断、刷新 API 业务结果、缓存状态、前端提示、迁移/备份兼容和分阶段验收。
- 复用约定：继续使用既有来源/授权/加密/导出链路及已提交的客户端策略 Phase B，不扩大 Agent、自建节点或画布协议支持范围。
- 本轮行为变化：无；未修改功能代码、历史迁移、数据库、来源、运行配置，未调用真实订阅刷新。
- 验证：检查规划的章节结构、代码围栏及工作区改动范围；未运行构建、测试或真实核心验证。
- 未完成项：规划全部待实施；按文档 S0—S5 执行，并在每阶段追加请求、全部文件、行为变化、验证和返工记录。

## 补充：自建节点选项缺失与独立实施链路

- 需求上下文：用户指出节点搭建页面没有 xhttp、Reality 选择。
- 核对结果：前端只提供 tcp/ws/grpc 和 none/tls；共享模型/校验、协议能力矩阵和 Agent 核心适配也尚未接入，不能只新增前端选项。
- 修改文件：`docs/subscription-import-compatibility-plan.md`、本记录文件。
- 规划变化：追加第 13 节，区分外部导入和自建节点，列出 Xray 优先、类型化设置、能力协商、Reality 密钥、渲染/发布/回滚、前端表单和订阅映射的实施顺序；sing-box 按实际核心能力单独验收。
- 复用要求：复用参数验证、客户端导出、Vault、已有发布和备份恢复，不将服务端私钥输出给客户端。
- 验证：只读检查对应前端、desiredstate、协议矩阵和 Agent 渲染源码；未运行测试或真实核心验证。
- 行为变化：无。未修改功能代码、数据库、节点配置或运行服务。

## 2026-11-04T23:15:43-08:00：合并外部订阅与自建节点的整体规划

- 原始需求：更新上面的文档，把节点搭建中的 xhttp、Reality 缺口纳入整体规划。
- 修改文件：`docs/subscription-import-compatibility-plan.md`、本记录文件；保留原文件路径和历史记录。
- 修改前：标题、目标、文件范围、阶段 S0—S5 和主要验收仍以外部订阅为主，自建节点仅在文末追加。
- 修改后：
  - 标题和首节明确三条工作线：外部导入、自建 VLESS、分层失败诊断。
  - 自建 Xray 优先纳入 M1—M3；sing-box 按固定核心能力在 M4 独立验收，S5 改为联合回归。
  - 增补 DesiredState/能力契约、Agent 渲染/发布/回滚、前端表单和对应文件范围。
  - 明确 Reality 服务端私钥与客户端公共参数的边界、普通保存不轮换、显式轮换与历史版本回滚规则。
  - 把自建校验/能力/发布错误加入诊断分类，补齐草稿保存和已发布订阅的 UI 提示。
  - 主验收清单纳入新旧 Agent、真实自建连接、私钥、备份和已发布订阅一致性。
  - 自建 Shadowsocks 2022 和画布新组合不随本次自动开放，防止把客户端导入验证误用为服务器多用户支持。
- 复用：沿用现有协议验证、Vault、Snapshot/Deployment、核心适配、订阅导出和备份恢复，不新建平行执行链路。
- 验证：文档章节、表格和代码围栏检查，以及范围/阶段/验收的一致性审查；未运行功能测试、构建或真实核心验证。
- 功能/API/数据库实际变化：无。全部新增能力仍处于规划状态。

## 2026-11-04T23:33:26-08:00: English translation of the integrated plan

- Request: Provide an English version of the updated protocol-support plan.
- Files: added `docs/subscription-import-compatibility-plan.en.md`; appended this entry to `docs/subscription-import-compatibility-change-log.md`.
- Content: translated all 13 sections, including external SS2022/Reality/xhttp imports, typed diagnostics, refresh and cache semantics, self-hosted Xray/sing-box capabilities, key lifecycle, deployment/rollback, phases S0-S5 and M1-M4, and all 18 acceptance criteria.
- Scope: preserved the source plan's existing/proposed distinction, version-specific support limits, and separation of client-public parameters from server-private material. No additional implementation commitments were added.
- Verification: compared numbered section/subsection coverage, phase identifiers, error codes, acceptance-criterion counts, and Markdown code fences with the Chinese plan.
- Functional changes: none. No source refresh, database/runtime changes, implementation, builds, or functional/core tests were performed.

## 2026-11-05: Review for approval

- Request: review the English plan for approval; revise the document only, no implementation.
- Baseline re-checked at `5387802`: the §2/§13.1 gap descriptions still hold. Found that `Store.Refresh` returns the failure-status write result, so a failed refresh reaches the UI as 204 success.
- Files: `subscription-import-compatibility-plan.en.md` (status line, new §14), `subscription-import-compatibility-plan.md` (status line points to §14), this log.
- §14 amendments: separate import and self-hosted deliverables; unique `name|protocol` per source (`duplicate_endpoint_key`); per-entry omission on all exporter paths reusing `rules.EmptyGroup`; v1.4 relay endpoints carry Reality/xhttp fields and correct network class; CDN effect on the device estimate; no xhttp patching of qui sing-box; Owner-entered Reality target; work starts after v1.4.0 on a new branch; per-phase gate process.
- Open Owner decisions: R1–R4 in §14.3.
- Functional changes: none. No tests, builds, or source refreshes run.

## 2026-11-05: Owner decisions recorded (R1–R4)

- Request: record the Owner's decisions in the English plan, this log, the v1.4.0 release notes and the handoff; documentation only, no implementation.
- Decisions (plan `.en.md` §14.3–§14.7):
  - R1: the import track and the self-hosted track run in parallel. The only shared step is the client-side Reality/xhttp fields and codecs, defined once in S3/S4.
  - R2: the refresh false-success fix ships in v1.4.0 as the first step (scope in §14.5).
  - R3: confirmed. Within one source, the first entry for a `name|protocol` key is kept and later ones are rejected with `duplicate_endpoint_key`. This is a behavior change for sources that import duplicates today.
  - R4: all work in this plan ships in v1.4.0 (changed from v1.5.0). The v1.4.0 RC and A1 wait until both tracks pass S5. Migrations continue after 0132 in merge order.
- Files: `subscription-import-compatibility-plan.en.md` (status, §14.1 item 8, §14.3–§14.7), this log, `docs/release-notes-1.4.0.md`, `docs/changelog.md`, `docs/handoff.md`.
- Not updated: the Chinese plan. Its status line still points to the English §14, which is authoritative.
- Functional changes: none. No code, tests, builds, or source refreshes.

## 2026-11-05: Minimal refresh false-success fix (§14.5), pending independent review

- Request: implement only §14.5 directly on `feat/v1.4-tunnels`, stop before local commit for independent review, and defer the Linux gate. S1 diagnostics/counts/schema and the remaining import/self-hosted phases are outside this change.
- Actual baseline: `84e8725` on `feat/v1.4-tunnels`; `569b1c8` and `84e8725` are the documentation-only commits following the requested `5387802`. Preserved the branch history and all three untracked compatibility documents.
- Implementation files:
  - `internal/panel/subscription/importer/store.go`: after successfully recording a failed status, return the original fetch/parse error; return the status-write error when persistence fails. Scheduled refresh still ignores the returned error.
  - `internal/panel/subscription/importer/handler.go`: refresh maps `ErrFetch`/`ErrParse` to HTTP 400 `import_refresh_failed` with fixed text `订阅来源抓取或解析失败，该来源的节点已从订阅中排除`. Other errors retain HTTP 400 `import_unavailable`; no upstream body, source URL or database details enter the response.
  - `web/src/modules/proxy/pages/ProxyNodeListPage.vue`: the create, edit-with-new-URL and manual-refresh paths share a refresh/reload helper. A refresh failure reloads the list and keeps the original error toast even if the reload also fails, without a success toast. Existing edit drafts stay open; after a successful create followed by a failed first fetch, the reloaded saved source opens for editing and the create fields remain cleared to avoid a duplicate create. No template/layout changes.
- Test files:
  - `internal/panel/subscription/importer/store_test.go`: the existing failed-refresh assertion now expects `ErrFetch` instead of nil.
  - `internal/panel/subscription/importer/refresh_test.go`: local TLS fixtures cover HTTP 502 and invalid-body parse failures, persisted failed status, exclusion of retained cached nodes, SQLite-trigger status-write failures taking precedence, fixed safe handler responses, and successful refresh recovery returning 204.
  - `web/scripts/import-refresh-selftest.mjs`: executes the page's actual action functions with injected state/API fixtures; checks all three failure paths, reload failure preserving the original error, saved-source editing context, edit draft retention, busy-state reset, absence of success toasts on failure, and normal success behavior.
- Verification on Windows (all passed):
  - With `GOTOOLCHAIN=local`, absolute `GOPATH=C:\Users\luanf\qui\.tools\gopath` and `GOCACHE=C:\Users\luanf\qui\.tools\gocache`: `.tools/go/bin/go.exe test -p 1 ./internal/panel/subscription/importer` — PASS (`3.223s` package execution).
  - `npm.cmd --prefix web run typecheck` — PASS.
  - `node web/scripts/import-refresh-selftest.mjs` — PASS.
  - `node web/scripts/ui-regression-selftest.mjs` — PASS.
  - Go formatting applied with the local `gofmt.exe`; `git diff --check` — PASS.
- Issues/limits: no functional test failures. Node printed the existing `NO_COLOR`/`FORCE_COLOR` environment warning; Git printed an inaccessible user-global ignore-file warning on status reads. Neither prevented verification. UI action tests are fixture-based, not a browser walkthrough. If the list reload itself fails, the UI keeps the original refresh error and cannot display a newly retrieved status until a later successful reload.
- Not performed: local commit/staging, push, release, production changes, real subscription refetch, full Linux gate or real-core probes. The Linux gate remains required on the eventual reviewed commit; this Windows run is not Linux/host acceptance evidence. Applied migrations, schemas, typed causes, counts and partial-success semantics are unchanged.

## 2026-11-05：刷新修复接入 Linux 门禁自测与 Windows 复验（未提交）

- 请求：把 `web/scripts/import-refresh-selftest.mjs` 加入 `scripts/check.sh` 的 frontend self-tests，使该自测在 Linux 门禁中执行；运行针对性的 importer Go 测试、新前端自测、UI 回归自测与 web 类型检查/构建，并如实追加结果。本轮不开始 S1，不提交、不跑 Linux 门禁，不推送/打标签/发布，不改迁移，不抓取真实订阅。
- 基线：`feat/v1.4-tunnels`，HEAD `84e8725`。保留 Codex 已完成的未提交最小修复（`importer/handler.go`、`importer/store.go`、`importer/store_test.go`、`ProxyNodeListPage.vue`）、未跟踪的 `importer/refresh_test.go`、`web/scripts/import-refresh-selftest.mjs` 与三份兼容性文档；未改动迁移、备份合同或已应用结构。
- 本轮改动文件：
  - `scripts/check.sh`：frontend self-tests 列表在 `node-editor-selftest.mjs` 之后新增 `node web/scripts/import-refresh-selftest.mjs`；该步骤上方注释补一句说明（自测用注入的 API/响应式状态夹具执行订阅页真实的刷新动作）。其余步骤、顺序、root/sudo 要求与 `npm --prefix web ci` 先行安装不变，文件保持 LF。
  - 本记录文件。
- 依赖条件：门禁先运行 `npm --prefix web ci` 与 `npm --prefix web run build`，脚本从 `web/node_modules` 解析 `typescript`，与本次本机运行方式一致；未新增依赖。
- 验证（Windows 本机；不是 Linux 或实机验收证据）：
  - `GOTOOLCHAIN=local`、绝对 `GOPATH=C:\Users\luanf\qui\.tools\gopath`、`GOCACHE=C:\Users\luanf\qui\.tools\gocache`、`-p 1`：`.tools/go/bin/go.exe test -p 1 -count=1 ./internal/panel/subscription/importer` → `ok proxycontrol/internal/panel/subscription/importer 3.704s`。
  - `.tools/go/bin/go.exe vet ./internal/panel/subscription/...` → 退出码 0。
  - `.tools/go/bin/gofmt.exe -l internal/panel/subscription/importer` → 无输出；`git diff --check` → 无空白问题。
  - `node web/scripts/import-refresh-selftest.mjs` → PASS（三个调用方在刷新失败时都重载列表、保留原始错误与编辑上下文，成功路径行为不变）。
  - `node web/scripts/ui-regression-selftest.mjs` → PASS（`All UI regression self-tests passed`，含 F2/F3、U1b、walkthrough A/E、review 1–4 与第三轮子项）。
  - `web/scripts` 下全部 13 个自测逐个用 `node` 运行 → 0 失败；`qr-selftest.mjs` 与 `client-policy-selftest.mjs` 另行单独运行也 PASS。
  - `npm.cmd --prefix web run typecheck` → 退出码 0；`npm.cmd --prefix web run build` → 退出码 0（`✓ built in 4.58s`）；`npm.cmd run test:time` → 退出码 0（fail 0）。
- 未执行与限制：
  - 本机无 `bash`，`bash -n scripts/check.sh` 未运行；新增行只是普通命令调用，仍需 Linux 门禁实际解析并执行确认。
  - 未运行 Linux 门禁，未提交/暂存，未推送、未打标签、未发布、未改生产，未真实抓取订阅，未开始 S1。
  - 前端自测为夹具级，不等于浏览器走查或实机证据；节点上「刷新失败后的列表状态」仍以真实面板 + 真实来源为准。
  - 计划 §14.5 的状态行仍写 `Not implemented yet`，待该修复审阅通过并提交/门禁时随文档更新。

## 2026-11-05：§14.5 刷新修复通过独立审阅并本地提交（Linux 门禁待跑）

- 请求：按英文版 §14.5 及计划流程，在独立审阅通过后做本阶段的本地提交，并把英文版/中文版计划里 §14.5 的状态由“未实现”改为“已实现，待 Linux 门禁”；不得写成已验证或已完成，只保留最小范围修复。
- 审阅结论：审阅方独立核对当前代码、测试与 `scripts/check.sh` 变更后，批准本阶段本地提交并运行 Linux 门禁。
- 本阶段修改文件：
  - `docs/subscription-import-compatibility-plan.en.md`：状态行与 §14.5 改为“已实现、待该提交的 Linux 门禁”，并列出实际落地的 1–5 项与新增覆盖。
  - `docs/subscription-import-compatibility-plan.md`：状态行注明 §14.5 已实现、待门禁，其余仍未实施（该文件没有独立的 §14 章节，状态以英文版 §14.5 为准）。
  - 本记录文件。
- 提交范围（显式暂存，共 10 个文件）：`internal/panel/subscription/importer/handler.go`、`store.go`、`store_test.go`、`refresh_test.go`、`web/src/modules/proxy/pages/ProxyNodeListPage.vue`、`web/scripts/import-refresh-selftest.mjs`、`scripts/check.sh`，以及本计划三份文档。不含迁移、其他模块或无关未跟踪文件。
- 提交信息：`fix(subscription): report failed source refreshes accurately`，结尾为 `Co-Authored-By: Claude Code <noreply@anthropic.com>`；未推送、未打标签、未发布。
- 验证：沿用上一轮 Windows 本机结果（importer 包测试、新增前端自测、UI 回归自测、typecheck/build 均通过）。Linux 门禁在该提交上运行 `scripts/check.sh` 与真实核心套件，结果另记；门禁通过前不声称已验证。
- 未完成与限制：Linux 门禁与真实核心结果、浏览器走查仍未完成；S1 及后续阶段未开始；未抓取真实订阅，未改生产。

## 2026-11-05：S0 版本/字段/格式证据（未提交、未审阅、未跑门禁）

- 请求：只实施计划 §3、§10、§14 的 S0（版本与夹具证据），在 `feat/v1.4-tunnels` 的短生命周期分支与独立 worktree 中产出证据报告与净化夹具清单；不改生产解析/模型，不提交，不跑 Linux 门禁，不推送/打标签/发布，不开始 S1 或 M1。另一任务负责的主干刷新修复与其串行门禁不触碰（未改 `scripts/check.sh`、`web/scripts/*`、等待器或门禁脚本）。
- 基线：worktree `C:/Users/luanf/qui-wt/s0-import`，分支 `feat/v1.4-import-s0`，取自 `feat/v1.4-tunnels` HEAD `650b349`；主检出未切换，工作树干净。
- 新增文件（仅文档）：
  - `docs/v1.4-s0-evidence.md`：S0 证据报告（固定二进制与摘要、隔离方法、逐格结果与按客户端的 35 条 manifest、原生校验与真实连接的区别、首次尝试限制、xhttp 参数处理、`ss://` 导出编码、未验证项与限制、证据位置与复现/清理命令）。
  - `docs/v1.4-s0-fixtures.md`：净化合成夹具清单，**16 条代表条目**（3 个 SS2022 AES-256、11 个 Reality+xhttp、2 个 Reality+grpc），逐条按客户端给出实测状态与各格式语义；否定、输入兼容、替代与重复键用例在独立小节单列，不占代表条目名额。
- 固定二进制（实测，非推断）：Xray 26.3.27 `8255dd93…c46845ed`；sing-box 1.14.3 `768d31fa…23815f93`（`with_quic/with_utls/with_clash_api`，**无** `with_v2ray_api`）；mihomo 1.19.33 `817f50b3…632b620f`（`vcs.modified=true`，不是可复现的干净构建）。
- 探测方法：每次运行新建网络命名空间（仅 `lo`，非 `lo` 链路即中止）、回环 HTTP 源站（随机 nonce 路径回显）、回环 TLS 伪装目标（`openssl` 自签，ALPN h2）、逐格临时凭据、`ss -ltnH` 判就绪（不建连）、以服务端 accepted/inbound 日志相对基线的增量作为“真实认证”证据、结束与异常都清理命名空间并复核；脚本置于运行目录之外，`python3 -I` 运行，输出脱敏。证据保存在测试机 `/root/s0-import/evidence/20261105T090351Z`（**35 条记录**：首轮 33 条，加 rev 2 补跑后追加的 2 条；补跑运行根为 `20261105T093755Z-recovery`），权限 0700/0600。
- 主要结论（按客户端逐格，rev 2 已按审阅意见收紧）：SS2022 AES-256 单钥与身份形态在 mihomo、sing-box 均通过原生校验与认证连接；AES-128 单钥两客户端均通过，身份形态只有 sing-box；chacha20 只有单钥 mihomo 可用，身份形态被 sing-box 拒绝（`initialize inbound[0]: invalid argument`）；PSK 必须为标准带填充 Base64 且长度精确（未填充、Base64URL、错误长度均被拒）。Reality+TCP 无 flow 仅 Xray 实测；TCP+Vision 三客户端均通过（mihomo 为第二次尝试成功）；Reality+grpc 无 flow 仅 Xray 实测，sing-box 的 grpc 仍属未验证线索；Reality+xhttp 四种模式只有 Xray 全部实测通过，mihomo 只测过 `auto`（`alpn: [h2]`）；Reality 与 grpc/xhttp 搭配 Vision 时原生校验通过、由**客户端 VLESS 出站**在请求时拒绝（`XTLS only supports TLS and REALITY directly for now.`），服务端 Freedom 从未建连、源站命中为 0；sing-box 无 xhttp 传输（`unknown transport type: xhttp`）；`mode=invalid` 原生拒绝，未知 `extra` 与 `xmux` 被原生**接受**但运行时效果未测，故严格子集须由面板校验。
- 对既有线索的核对与更正：早前 Codex 结论整体可复现，但有两点更正——(1) chacha 拒绝属服务端身份形态限制而非“链式”本身；(2) 早前“Mihomo Reality 通过”的失败样本只说明**后续尝试能成功**，首次拨号失败的原因**未确定**（无隔离实验），现矩阵只重试一次并记录 `http_attempt_count`，mihomo 正例一律标为“后续尝试成功”，不提供首次即成功的保证。`/tmp/qui-s0-*` 旧目录仅作线索：其保存配置缺少访问日志且 Reality 键写作 `target`，重放无法证明任一结论。
- 未复现项：早前“整条 URI Base64 经 DIRECT 回落仍 HTTP 200”的具体现象未复现（三个固定客户端都不消费 URI 文本），改用显式对照格 `net-control-direct`：HTTP 200 但服务端认证连接数为 0，证明“HTTP 成功≠支持”。
- 验证：本节所有数字均来自测试机实测运行（`/root/s0-import/probe/s0probe.py`、`extra.py`、`extra2.py`、`extra3.py`、`extra-cells.py` 与 `summary.jsonl`）；Windows 本机未运行任何 Go/前端测试，因为本轮不涉及源码改动。
- 未完成与限制：未做独立审阅（首轮审阅随后未通过，见下条），未提交/暂存，未跑 Linux 门禁（门禁由另一任务串行占用），未推送/打标签/发布；未开始 S1、M1；未改生产代码、迁移、API、UI；不含 UDP、计量、设备门控、Agent 验收；legacy Clash 输出与 xhttp 的 H3/高级 tuning 未验证；无任何探测把 URI 文本交给客户端，URI 编码结论随后改为按 SIP002/SIP022 原文引用（见下条）。

## 2026-11-05：S0 审阅第一轮意见修正（未提交、未审阅通过、未跑门禁）

- 请求：只修正首轮审阅提出的 8 项问题；不实施 S1 或任何协议代码，不提交、不跑门禁、不合并；保留全部安全边界（不接触真实订阅/生产、不打印或复制密钥、Python-I 探测脚本留在产物目录之外、命名空间隔离并清理、不放宽 TLS）。
- 基线：同一 worktree `C:/Users/luanf/qui-wt/s0-import`，分支 `feat/v1.4-import-s0`，HEAD 仍为 `650b349`；本轮只改 5 份文档，未暂存、未提交、未切换分支。
- 修改文件：`docs/v1.4-s0-evidence.md`（rev 2）、`docs/v1.4-s0-fixtures.md`（重写为 16 条代表条目 + 独立否定小节）、`docs/subscription-import-compatibility-plan.md`、`docs/subscription-import-compatibility-plan.en.md`（§4.4、§10 S0 状态段）、本记录。
- 逐项对应：
  1. **SS2022 导出编码**：抓取并引用 <https://shadowsocks.org/doc/sip002.html> 与 <https://shadowsocks.org/doc/sip022.html> 原文——AEAD-2022 的 userinfo *“MUST NOT be encoded with Base64URL”*，未编码时 *“method and password MUST be percent encoded”*。规范导出改为 `ss://` + 百分号编码的明文 `method:password`；Base64URL 降为**输入兼容**（`S0-COMPAT-SS-B64URL`）；同时撤回“两种客户端都接受百分号形”的说法：没有任何探测把 URI 文本交给客户端，探测写入的是已解码凭据。§7、夹具 §1、计划 §4.4、本条与上条记录同步修正。
  2. **代表性 manifest**：11 条 xhttp 全部改为有效 Reality 条目（auto×4、packet-up×3、stream-up×2、stream-one×2），不再用 TLS/非法/重复用例占位；`S0-ALT-TLS-XHTTP`、`S0-NEG-*`、`S0-COMPAT-SS-B64URL` 与 `S0-DUP-01` 移入独立小节并使用唯一名称，重复键测试单独一条，避免去重掩盖校验结果。
  3. **能力结论按客户端**：证据 §8 改为（组合 × 客户端）矩阵——AES-256 单钥与身份两客户端、AES-128 单钥两客户端但身份仅 sing-box、chacha 单钥仅 mihomo、Reality TCP 无 flow 仅 Xray、TCP Vision 三客户端、grpc 无 flow 仅 Xray、xhttp auto 为 Xray + mihomo（H2）且其余模式仅 Xray；夹具 `S0-GR-01` 的 sing-box 由“支持”改为未验证。
  4. **两格补跑**：stream-up 与 packet-up+Vision 在独立回环命名空间（`qui-s0-recovery-*`，仅 `lo`）单独重跑，结果写入新运行根 `20261105T093755Z-recovery` 并追加进 `20261105T090351Z/summary.jsonl`（33 → 35 条）；首轮遗留目录移入 `.attempt1/` 且注明不是证据；证据 §9 增补 manifest 表、驱动脚本摘要（含新 `extra-cells.py` `005ea82f…6a738e`）与清理命令，时间线注明补跑时刻 2026-11-05T09:38Z。stream-up 首发起效（1 次源站命中）但客户端在响应后记录 `failed to transfer response payload > unexpected EOF`；packet-up+Vision 两次均只到传输层、源站命中 0。
  5. **Mihomo 首次拨号**：删除“探测伪影/竞态”因果结论与“No verdict rests on retry”；改为“原因未确定”，明确重试只可能把失败变成功、mihomo 正例一律为后续尝试成功、首次即成功无保证；并说明 `http_probe()` 内联注释的竞态说法不是本报告结论。计划 §10 与上条记录同步删除竞态表述。
  6. **Vision 失败归属**：三处 Vision 负例统一表述为“原生校验通过、HTTP 检查失败（源站命中 0）、**客户端 `proxy/vless/outbound` 拒绝**、服务端 Freedom 从未建连”，不再写成服务端拒绝；面板文案要求说明是客户端侧限制。
  7. **复现完整性**：复现块补 `extra3.py` 与 `extra-cells.py`，注明 `s0probe.py` 截断而各驱动追加的顺序要求；新增独立清理命令（`ip netns pids|kill -9`、`ip netns del`、`ip netns list | grep -c qui-s0`、`pgrep -af`）与 35 条精确 manifest（§9.1）。
  8. **§8 过度概括**：删除“Xray 与 mihomo 四种 xhttp 模式”和“三客户端 TCP 含 Vision/无 flow”等宽表述，改为逐客户端组合并列明未测项（mihomo 其它 xhttp 模式、mihomo/sing-box 的 TCP 无 flow 与 grpc、sing-box grpc、Xray 的 SS2022、AES-128 身份 mihomo）。
- 额外更正（审阅未提但影响证据）：矩阵中 `native-xhttp-mode-invalid` 与 `native-xhttp-extra-unknown-params` 两格因使用占位 Reality 私钥而失败（`invalid "privateKey"`），§6 与 manifest 标为 void，结论只依据 `native2-*`；`xmux`/未知 `extra` 的表述由“静默忽略”改为“解析器接受、运行时效果未测”。
- 验证：本轮仅有文档改动，未运行 Go/前端测试（无需重跑）。远端复核使用只读命令：读取 `summary.jsonl` 计数与字段、`sha256sum` 驱动脚本、检查 `ip netns list` 与残留进程；补跑结束后命名空间已删除、无 `s0probe.py`/`extra-cells.py` 残留。
- 门禁状态：另一任务在 `650b349` 上的门禁已于 2026-11-05T09:30:12Z 完成——`check` 1980s、`cores` 572s，`GATE 650b349 PASS total=2552s`（本轮只读查看日志，**没有新起门禁**）；该结果属于刷新修复提交，S0 文档仍未提交，因此不据此声称 S0 已验证。
- 未完成与限制：仍**未通过审阅**、未提交、未跑门禁；未开始 S1/M1；未改生产代码、迁移、API、UI；无 URI 文本级探测，identity 分隔符的百分号编码仍待 S2 用会解析 URI 的客户端确认；未新增 mihomo/sing-box 的 grpc、TCP 无 flow、其它 xhttp 模式等单元格（本轮只被授权补跑两格）。
- 后续提交约定：提交信息结尾使用 `Co-Authored-By: Claude Code <noreply@anthropic.com>`；本轮无提交。

## 2026-11-05：S0 最终审阅意见修正（rev 3，未提交、未审阅通过、未跑门禁）

- 请求：按最终 S0 审阅意见只改文档；不提交、不暂存、不跑门禁，不改生产代码/迁移/API/UI，不做生产或网络测试，不新起门禁，不在本轮声称 S1 完成。
- 基线：同一 worktree `C:/Users/luanf/qui-wt/s0-import`，分支 `feat/v1.4-import-s0`，HEAD 仍为 `650b349`；本轮只改 5 份文档。
- 修改文件：`docs/v1.4-s0-fixtures.md`、`docs/v1.4-s0-evidence.md`（rev 3）、`docs/subscription-import-compatibility-plan.en.md`、`docs/subscription-import-compatibility-plan.md`、本记录。
- 逐项对应：
  1. **GR02 改为第二条有效代表条目**：夹具 §3 现明确两条都是无 `flow` 的有效 Reality+grpc 代表条目；`S0-GR-02` 使用独立 UUID、独立端口 `127.0.0.1:22002` 与需要转义的 `serviceName=s0 grpc`（URI 写 `serviceName=s0%20grpc`），Mihomo/sing-box 一侧保持未转义字面量，作为后续 S3/S4 codec 的转义用例；Vision+grpc 的无效用例只保留为 §4 的 `S0-NEG-VISION-GRPC`。16 条代表条目仍为 3 SS2022 AES-256 + 11 Reality+xhttp + 2 Reality+grpc，且全部有效。
  2. **形状级实测与实例级未实测分开**：夹具 §3 区分 `vless-reality-grpc-empty-xray` 提供的“形状级、仅 Xray 客户端”支持，与本轮两条合成实例（各自 UUID、SNI、端口与 `serviceName` 转义）未被重放；`serviceName` 的百分号编码按“待 S2/S3 用会解析 URI 的客户端确认”处理，与 SS2022 identity 分隔符同级，不写成实测结果。
  3. **证据两处 mihomo 行（原 156/164 行）**：改为“首次尝试失败、后续尝试成功、原因未确定”，与证据 §5 一致；§9 manifest 第 10、18 行同步注明首次失败。
  4. **证据 sing-box xhttp 表述（原 235 行）**：不再与三处 Vision 负例并列为“两次都失败”。三处 Vision 是运行时负例（两次都只到传输层、源站命中 0、由客户端 VLESS 出站拒绝）；sing-box xhttp 是**原生配置拒绝**（`decode config at …: outbounds[0].transport: unknown transport type: xhttp`），未发起任何 HTTP 请求，也不是重试用例。§4 增补区分段，§5 拆成两条，§8 的 `a2` 定义改为“首次失败、第二次成功、原因未确定”。
  5. **状态刷新（5 份文档）**：删除“S1/M1 未开始”的现行口径，统一为“S0 只改文档与采集证据、不改生产代码；S1 已在独立 worktree 进行中，本轮不声称 S1 完成；S2–S4、M1–M4 未开始”。更早的条目保留各自日期的历史口径（其“未开始 S1/M1”不是现状），现行口径以本条和 5 份文档的状态段为准。
  6. **刷新修复门禁状态**：英文版顶部与 §14.5（以及中文版顶部、英文版 §14.7 分支说明）改为 `650b349` 已提交并通过 Linux 门禁，且经独立核对：`check` 1980s、`cores` 572s、`GATE 650b349 PASS total=2552s`，END `2026-11-05T09:30:12Z`；本轮只读核对日志，**没有新起门禁**；仍不据此声称 S0 已验证或 A1 完成（真实失败来源的浏览器走查仍缺）。
- 校验：本轮无源码改动，未运行 Go/前端/真实核心测试。`git diff --check` 按基线对比处理：改动前的基线输出只涉及本记录文件——该文件全篇 CRLF 且 `core.autocrlf=false`，因此其每条新增行都会报 `trailing whitespace`，并另有既存的行尾空行提示；改动后再次运行，违规仍只出现在该文件，其余 4 份 LF 文档没有新增空白问题，新增行除 CRLF 外没有行尾空格或制表符。未用“CRLF 错误既存”作未经基线比对的结论。
- 未完成与限制：仍未通过独立审阅、未提交/暂存、未推送/打标签/发布；S0 文档未跑 Linux 门禁；能力矩阵仍按（组合、客户端）逐格保守给出，未新增 mihomo/sing-box 的 grpc、TCP 无 flow、其它 xhttp 模式单元格。
- 后续提交约定：提交信息结尾使用 `Co-Authored-By: Claude Code <noreply@anthropic.com>`；本轮无提交。

## 2026-11-05：S0 交付（已审阅通过，提交与自身 Linux 门禁）

- 请求：S0 的证据与夹具范围已通过独立审阅（**仅证据/清单范围，不是完整协议验收**）；本轮只做 S0 交付——把状态刷新为“已审阅通过、待自身提交门禁”，把发布说明的刷新修复行对齐到 `650b349` 的门禁结论，然后只在 `feat/v1.4-import-s0` 上做本地提交，并对**该提交本身**串行运行 `scripts/check.sh` 与真实核心套件。不推送、不打标签、不发布、不碰生产，不合并主干或其他 worktree，不编辑 S1 文件（S1 在自己的 worktree 继续），不新增任何协议支持结论。
- 基线：同一 worktree `C:/Users/luanf/qui-wt/s0-import`，分支 `feat/v1.4-import-s0`，HEAD `650b349`；主检出 `C:/Users/luanf/qui` 未切换分支。
- 审阅确认项（本轮新增）：代表性 manifest 的 16 条**全部有效**（含 `S0-GR-02` 为第二条无 `flow` 的 gRPC 有效条目）；按（组合、客户端）的限制成立；SS2022 规范导出按 SIP002 原文经独立讨论核对；final 汇总的恢复单元格复现一致——stream-up 认证成功、源站命中 1、`attempt_count=1`；packet-up+Vision `http=false`、源站命中 0、`attempt_count=2`。
- 修改文件（6 份，全部文档）：`docs/v1.4-s0-evidence.md`、`docs/v1.4-s0-fixtures.md`、`docs/subscription-import-compatibility-plan.md`、`docs/subscription-import-compatibility-plan.en.md`、`docs/release-notes-1.4.0.md`、本记录。
- 状态口径：4 份 S0 文档的状态段由“未通过审阅、未提交、未跑门禁”改为“已通过独立审阅（证据/夹具范围）、已本地提交、该提交的 Linux 门禁待执行”。首轮与第二轮审阅未通过的记录**按原日期保留**，只作历史，不是现状；本条与 4 份文档的状态段为现行口径。不声称整体计划完成：S1 在独立 worktree 进行中（本轮不声称完成），S2–S4、M1–M4 未开始，其他协议工作线未验收。
- 发布说明对齐：`docs/release-notes-1.4.0.md` 的刷新误报行改为“已实施、`650b349` 门禁通过（`check` 1980s、`cores` 572s、`GATE 650b349 PASS total=2552s`、END 2026-11-05T09:30:12Z）”；外部订阅导入行标注 S0 已审阅通过但该提交门禁待执行，并注明独立审阅通过不等于任何协议支持被验收。
- 提交与门禁流程：本地提交只含上述 6 份文档；门禁使用 `~/.qui-local/gate.sh` 并传**显式提交 SHA**（辅助脚本默认从主检出 `/c/Users/luanf/qui` 归档，S0 提交对象经共享对象库可达，不能误门禁 `650b349`）；启动前核对没有其他门禁运行器在跑（锚定计数），只跑一次，不重启、不删除匹配日志，不用 `nohup`；结果与日志路径另记，不预写结论。
- 空白与行尾校验：`git -c core.whitespace=cr-at-eol diff --check` 无输出；本记录与夹具全篇 CRLF 保持不变，新增行同为 CRLF，其余文档保持 LF；没有新增只含空白的行。
- 未完成与限制：S0 的 Linux 门禁结果与来源管理/格式预览的浏览器走查仍缺；S1–S4、M1–M4 未完成；本轮不新增协议支持或验收结论。
- 后续提交约定：提交信息结尾使用 `Co-Authored-By: Claude Code <noreply@anthropic.com>`。

## 2026-11-05：S1 结构化诊断实现（当时待主助手本人审阅）

- 请求：在前置提交 `650b349`（Linux 门禁 `GATE 650b349 PASS total=2552s`，check/cores 均 EXIT 0，结束 `2026-11-05T09:30:12Z`）之上，按计划 §6–§10 与 §14 修正实现 S1：类型化抓取失败、逐条诊断与计数、结构化刷新结果与 HTTP 映射、缓存/状态语义、前端业务提示，并保持既有 SSRF、事务、取消与并发约束。工作分支 `feat/v1.4-import-s1`，工作树 `C:/Users/luanf/qui-wt/s1-diagnostics`，基于 `650b349`。
- 修改前（S0/前置状态）：`Fetcher` 把所有失败折叠为 `ErrFetch`；解析器静默跳过不支持条目且 `replace` 只按内容哈希去重；刷新只返回 error，部分成功无法表达；`subscription_sources` 无结果列；页面提示基于错误文本，无计数与原因。
- 修改后：
  - 抓取：新增 `FetchError{stage,code,status}`，覆盖 input/dns/address/redirect/tls/connect/deadline/http/size，`errors.Is(err, ErrFetch)` 保持兼容；错误文本只含固定代码，`Message()` 为固定中文，不含链接、Location、响应体或凭据。HTTPS、逐跳公网 IP 固定、2 MiB、15 秒上限与重定向重校验未放宽。
  - 解析：`Parse` 改为 `Analyze` 的兼容适配（同一实现两种入口）；逐条记录 `parse`/`duplicate` 原因，`total = accepted + rejected + duplicates`，条目多问题时只计一次；完全相同条目为 `duplicate_endpoint_content`，同 `name|protocol` 但参数不同为 `duplicate_endpoint_key`（附保留条目序号），保留文档顺序第一条；文档级失败（空、无法识别、Base64 失败）计数为 null。
  - 存储：`Store.Refresh` 返回 `(Result, error)`；部分成功提交缓存且 `last_status=ok`/空 `last_error`，失败保留旧行但置 `failed` 并写固定 `last_error`；提交失败回滚且不改状态（返回 `import_commit_failed` 与持久化错误）；取消不写失败。新增 `last_success_at` 与 `latest_result_json`（迁移 `0133_subscription_source_results`），更换 URL 清空结果回 `pending`；`List` 返回 `lastSuccessAt`/`latestResult`，历史失败无结果显示“未记录原因”。
  - API：刷新 200（ok/partial）、422、502、504、500、404、400（内容来源无链接）分别映射，错误体 `details.result` 携带同一结果；`GET /subscription-sources` 附带最近结果。
  - 备份/恢复：`validateImports` 在应用 0133 时校验结果 JSON（固定阶段/代码、counts 一致、诊断文本必须等于代码固定文本），0133 前归档无该列仍合法；`PrepareRestoreTx` 按归档列集复制，旧归档保留“未记录原因”，不自动抓取。
  - 前端：`importApi.ts`/`proxyNodes.ts` 增加结果类型、`describeImportResult`、`importCountsLine`、`failedImportResult`；来源卡片显示状态（正常/部分导入/拉取异常/等待抓取）、本次导入/跳过/重复/缓存/可用于订阅、上次尝试与上次成功；新增有界“导入结果”对话框；三个刷新入口与粘贴导入按业务结果显示成功/警告/错误；失败保留已保存 ID 与编辑内容。
- 新增/修改测试：`fetch_test.go`（九类抓取错误与既有 SSRF 用例）、`report_test.go`（计数、两类重复、边界、文档级失败、结果校验、固定文案）、`refresh_test.go`（HTTP 映射、失败保留缓存、提交失败回滚、URL 变更清空）、`store_test.go`（适配新签名）、`imports_test.go`（结果校验与 0133 前归档）、`recovery/imports_test.go`（恢复保留结果）、`web/scripts/import-refresh-selftest.mjs`（三入口 + 粘贴导入的业务提示与失败保留）。
- 验证：见本轮交付说明中的命令与结果；Linux 门禁、真实来源抓取、浏览器走查均未执行。
- 未完成与限制：SS2022/Reality/xhttp（S2–S4、M1–M4）仍未实现；未抓取真实订阅、未改生产、未提交/推送/打标签。

## 2026-11-05：S1 首轮审查阻塞项修正（当时待主助手本人审阅）

- 请求：只修正既有未提交 S1 分支上的 5 项审查阻塞项，不提交、不跑完整门禁、不合并、不推送；由执行者实现，审阅由主助手本人完成（不委派）。不新增 SS2022/Reality/xhttp，不改动安全边界与 SSRF 限制，不改已应用迁移（0133 仍为最新）。
- 基线：`feat/v1.4-import-s1` 未提交工作树；S0 提交 `9045e1b` 的门禁仍在运行，本轮未启动竞争门禁、未触碰 S0。
- 逐项修正：
  1. 粘贴导入结果（对应阻塞 1）：`Store.Save` 改为返回 `(id, Result, error)`，粘贴内容走与抓取相同的 `Analyze` 管线——200 返回 `{id, result}`，无条目可导入 422（`details.result` 携带同一结果），输入无效 400（未发生尝试、无结果），持久化失败 500，来源不存在 404。`saveManualNodeModal` 改用共享 `showImportResult`：`ok` 成功、部分导入警告、失败用面板固定文案；被拒的粘贴保留草稿与对话框，并直接打开有界原因对话框（内容来源没有卡片可点）。
  2. 上游状态（阻塞 2）：`Result.Status`（`upstreamStatus`）持久化上游 HTTP 状态，`resultMessage(code, status)` 成为唯一固定文案格式化入口（`FetchError.Message`、API、卡片、对话框共用）；仅 `source_http_status` 可带状态且必须在 100–599，其余代码带状态即被校验拒绝；`handler.refresh` 不再传 `0`，403 显示为“HTTP 403”，永不出现“HTTP 0”。
  3. 持久化失败（阻塞 3）：`Save`/`Refresh` 在尝试前一次性捕获已提交状态（`last_status`、`last_success_at`、缓存条数）；任何持久化分支（封存、开始事务、替换缓存、写状态、提交、写失败记录本身）都返回 `stage=commit`、`outcome=failed`、`code=import_commit_failed`，并报告回滚后仍保留的上次成功时间与 `cached`/`usable`（原先可用则仍可用）；不再把本次分析的条目说成已保留的 `accepted`；响应体不含存储错误文本。
  4. 归档校验（阻塞 4）：`ValidateResult` 增加语义校验（代码→阶段/结果关系、固定文案、RFC3339 时间、counts 不变式、诊断阶段/字段/索引/保留索引、未知 JSON 字段拒绝），新增 `ValidateStoredResult` 在备份边界把结果与其行核对（来源 ID、`last_fetch_at`/`last_success_at`、`last_status` 一致）；迁移 0133 之前的归档与 null 未知计数仍兼容。
  5. Base64 可达性（阻塞 5）：`Analyze` 在没有任何格式命中时，仅对“像 Base64 但无法解码”的文档走 `Base64` 分析器（阶段 `decode`、代码 `invalid_base64`），文本/HTML/普通单词仍为 `import_unrecognized`；解码失败分支因此可达且仍只有一份实现。
- 新增/修改测试：
  - `internal/panel/subscription/importer/save_test.go`（新增）：混合粘贴返回 200+结果、干净粘贴、仅链接无结果、全不支持粘贴 422+结果且不落库、Base64 粘贴分类、输入无效 400（无结果）、未知来源 404、粘贴持久化失败 500（保留旧数据、无存储文本泄漏）。
  - `refresh_test.go`：403 全链路（结果/落库行/路由响应均含 403，无 “HTTP 0”）、提交失败保持上次成功与可用缓存（旧状态 ok、可用 1，新抓取 2 条）、替换缓存失败回滚、失败记录写入失败保持可用、开始事务失败（占住唯一写连接、经读池读取）报告 commit 状态。
  - `report_test.go`：Base64 可达性、全部结果类型 round trip、18 项伪造结果逐项拒绝（阶段/结果/代码/文案/时间/状态/计数/诊断字段与索引/保留索引/未知字段/尾随 JSON/非法 JSON）、`ValidateStoredResult` 行匹配。
  - `internal/panel/system/backup/imports_test.go`：ok/partial/failed 三类结果均通过，逐项篡改（来源、时间、成功时间、计数、诊断字段与文案、状态矛盾）均被拒，恢复原值后再次通过。
  - `web/scripts/import-refresh-selftest.mjs`：三入口失败显示真实 403、混合粘贴为警告并打开原因、全不支持粘贴保留草稿并显示逐条原因、保存成功但重载失败不重复保存也不报错、未知计数显示“—”。
- 本地验证（Windows，仅针对性测试，未跑 Linux 门禁）：`go test -count=1 ./internal/panel/subscription/importer`（ok）、`./internal/panel/subscription/...`（全部 ok）、`./internal/panel/system/backup`（ok 88.6s）、`./internal/platform/recovery`（ok 61.3s）、`./internal/platform/server -run TestImport|TestSubscription|TestSources`（ok）、`node web/scripts/import-refresh-selftest.mjs`（PASS）、`vue-tsc --noEmit`（`EXIT=0`）、`vite build`（built）、`gofmt -l`（无输出）。
- 未完成与限制：Linux 门禁、真实订阅抓取、浏览器走查均未执行；未提交、未推送、未打标签；SS2022/Reality/xhttp 仍未实现。审查结论以主助手本人复核为准。

## 2026-11-05：S1 第二轮审查阻塞项修正（当时待主助手本人审阅）

- 请求：只修正主助手本人第二轮审查发现的 5 项具体阻塞项；不提交、不合并不跑完整门禁，由执行者实现、主助手本人审阅（不委派）。不改迁移（0133 仍为最新）、不新增协议、不放宽 SSRF 与加密边界。
- 基线：`feat/v1.4-import-s1` 未提交工作树。S0 `9045e1b` 门禁 PASS（total=2569s）已合入 `feat/v1.4-tunnels`，M1 契约分支另开；本轮未 rebase、未触碰其他工作树、未暂存任何文件。
- 逐项修正：
  1. 诊断字段白名单（阻塞 1）：`parse.go` 为每个实际发出的字段路径定义常量（新增 `entry`、`ws-opts`、`grpc-opts`、`network`，原有 `""`/`credential`/`tls.insecure`/`method`/`security`/`type`/`query` 同样常量化，解析处只传常量），`report.go` 的 `diagnosticFields` 由同一组常量构成。此前 Clash 的 `unsupported_option` 与传输类原因会让 `ValidateResult` 拒绝面板自己写入的结果：`List` 读回 nil、备份拒绝自身数据库。
  2. 尾随 JSON（阻塞 2）：`ValidateResult` 的第二次 `Decode` 要求 `io.EOF`，因此 `validJSON + " !garbage"` 这类以语法错误出现的尾随内容不再被接受；只有空白尾随通过。
  3. 粘贴拒绝语义（阻塞 3）：`Store.Save` 在写入前失败时返回 disposition 代码——新来源 `import_paste_rejected`（未创建来源）、已有来源 `import_paste_unchanged`（未保存任何更改，现有来源状态保持不变）。两者保留逐条原因与 `total/rejected/duplicates`；已有来源沿用上次成功时间与旧 `cached`/`usable`（原先可用则仍可用），不再声称“已抓取”或“已从订阅中排除”。`unchangedCounts` 成为“未改变状态”的唯一计数来源，与提交失败、存储失败共用。
  4. 可达性不变式（阻塞 4）：共享校验边界新增「成功（`ok`/`partial`）结果的 `lastSuccessAt` 必须等于 `attemptedAt`」「`ok` 不得带 rejected/duplicates」「`partial` 必须有跳过」；`ValidateStoredResult` 只接受面板实际落库的代码（存储失败 `import_commit_failed`、粘贴拒绝两码、`invalid_source_url` 永不落库），并要求已提交缓存计数已知（`cached`/`usable` 非 null）。
  5. 本地存储失败（阻塞 5）：`Refresh` 打开来源自身密封数据失败不再是 `ErrFetch`，改为 `ErrStore` + 新代码 `import_source_unreadable`（阶段 `store`）：HTTP 500、固定文案、状态不变、报告旧 `cached`/`usable`、不写失败记录、不回显 Vault 错误或凭据。
- 新增/修改测试：
  - `internal/panel/subscription/importer/fields_test.go`（新增）：字段常量全部被 `validField` 接受、任意路径被拒绝；Clash 部分文档（未知选项、缺 `ws-opts`、缺 `grpc-opts`、非法 `network`）经粘贴与抓取两条路径后，`List` 仍带 `latestResult` 且 `ValidateStoredResult` 通过。
  - `report_test.go`：尾随垃圾 / 第二个 JSON / 纯空白尾随；8 项不可达状态（缺成功时间、成功时间不符、`ok` 带跳过、`partial` 无跳过、存储失败落库、粘贴拒绝落库、失败缺缓存）与自身结果通过。
  - `save_test.go`：全不支持粘贴 → 422 `import_paste_rejected`（无来源创建、消息不含“已抓取/已排除”）；已有内容来源与 URL 来源（各 1 条已缓存且可用）→ 422 `import_paste_unchanged`、`cached`/`usable`=1、行/缓存/订阅端点与上次成功时间不变。
  - `refresh_test.go`：合成密封数据损坏（`url_encrypted=x'00'`，不打印任何密钥或明文）→ `ErrStore`、`import_source_unreadable`、500 固定文案、旧计数与状态不变、响应不含 vault/加密错误与来源机密。
  - `internal/panel/system/backup/imports_test.go`：失败行改用真实 `Refresh` 记录（连接被拒）以获得可落库结果；新增 Clash 未知选项来源的真实备份创建与 `Examine` 往返。
  - `web/scripts/import-refresh-selftest.mjs`：粘贴拒绝固定文案改为“未保存任何来源”，计数、原因与草稿保留断言不变。
- 本地验证（Windows，仅针对性测试，未跑 Linux 门禁）：`go test -count=1 ./internal/panel/subscription/importer/`（ok）、`./internal/panel/system/backup/`（ok 89.2s）、`./internal/platform/recovery/`（ok 56.0s）、`./internal/panel/subscription/...`、`go build ./...`、`go vet`（importer、backup）、`node web/scripts/import-refresh-selftest.mjs`（PASS）、`gofmt -l`（无输出）。
- 未完成与限制：Linux 门禁、真实订阅抓取、浏览器走查均未执行；未提交、未推送、未打标签；SS2022/Reality/xhttp 仍未实现。审查结论以主助手本人复核为准。

## 2026-11-05：S1 取消请求的错误体修正（主助手本人第三轮审查，当时仍未提交）

- 请求：上一轮 5 项修正经主助手本人复核通过；仅修正随后发现的取消语义回归，不提交、不合并不跑完整门禁，审阅仍由主助手本人执行（不委派）。不改迁移（0133 仍为最新）、不改存储的状态/结果/缓存语义、不新增取消阶段或通用结果框架、不推送/打标签/改生产/抓取真实来源。
- 缺陷：`Store.Refresh` 在抓取前先建立临时结果（`Result{SourceID, LastSuccessAt}` 并在 `Fetch` 前填入 `AttemptedAt`），请求在抓取期间被取消（`ctx.Err() != nil`）时原样返回；`Handler.failure` 仅按 `AttemptedAt != ""` 判断就挂上 `details.result`。于是 503 携带一个未完成结果：`outcome`/`stage`/`code`/`message` 为空、`counts` 全 null、`diagnostics` 为 `null`。页面 `failedImportResult` 接受任意 `attemptedAt`，`describeImportResult` 随即读取 `result.diagnostics.length` 抛 `TypeError`；即使不崩溃，也会用空摘要替换真实的中文取消提示。来源行、缓存与已发布端点原本就未被写入（行为正确，保持不变）。
- 修正（仅 `handler.go`）：新增 `classified(result)`——结果必须同时带 `outcome`、`stage`、`code` 与固定 `message`（面板所有业务结果的实际形态）才作为 `details.result` 携带；未完成的尝试改为返回原有 503 固定外层文案“刷新已取消，来源状态保持不变”，不带任何部分细节。同一判定顺带覆盖“内容来源没有链接”（`ErrContentSource`）此前会携带空结果的相同缺陷。`store.go` 的取消路径、`last_status`/`latest_result_json`/缓存写入逻辑与 `RecordFailure` 语义均未改动。
- 新增/修改测试：
  - `refresh_test.go` 新增 `TestRefreshCancelledDuringFetchKeepsTheSourceAndSendsNoResult`：先成功抓取一次（缓存 1 条、`import_ok`）并记录整行，然后把 `Fetcher.DialContext` 换成阻塞到调用方 context 结束的假拨号，经真实刷新路由在**抓取进行中**取消请求。断言 HTTP 503、`code=import_unavailable` 与固定文案、错误体不含 `result`/`attemptedAt`/`diagnostics`/`counts`、不含来源机密；并逐字段比对状态、`lastFetchAt`、`lastSuccessAt`、`latestResult.code` 与缓存行、订阅可用端点均未变化。已验证该用例在旧实现（`AttemptedAt != ""`）下失败、在新实现下通过。
  - `web/scripts/import-refresh-selftest.mjs` 新增“503 无 `details.result`”fixture：三个刷新入口都显示“刷新已取消，来源状态保持不变”（error），不打开原因对话框、不产生空计数摘要，并保留草稿与可编辑状态；既有“无结果失败保留面板文案”用例不变。
- 本地验证（Windows，仅针对性测试，未跑 Linux 门禁）：`go test -count=1 ./internal/panel/subscription/importer/`（ok）、新增用例单独 `-run`（PASS；旧实现下 FAIL）、`go vet ./internal/panel/subscription/importer/`（通过）、`node web/scripts/import-refresh-selftest.mjs`（PASS）、`gofmt -l`（无输出）。前端类型检查与构建未在本轮重跑。
- 未完成与限制：Linux 门禁、真实订阅抓取、浏览器走查、`vue-tsc`/`vite build` 本轮均未执行；工作树仍未提交、未暂存、未推送、未打标签；SS2022/Reality/xhttp 仍未实现。审查结论以主助手本人复核为准。

## 2026-11-05：S1 基线整合到 `9045e1b` 与 flaky 断言修正（当时未提交）

- 请求：主助手本人已复核并接受两轮修正与取消回归的代码修改；进入阶段提交前，把未提交的 S1 工作整合到主开发线当前提交 `9045e1b`（S0 已门禁/已整合）。须保留 S0 的审阅证据、夹具与状态、`650b349` 与 `9045e1b` 的门禁记录，以及 S1 的实现与修正记录；不提交、不合并主干、不跑完整 Linux 门禁，S1 当时仍待最终审阅与阶段提交门禁；不改已应用迁移（0133 仍为 S1 保留），不改 Owner 既有页面布局。
- 基线：worktree `C:/Users/luanf/qui-wt/s1-diagnostics`，分支 `feat/v1.4-import-s1` 由 `650b349` 快进到 `9045e1b`；主检出 `C:/Users/luanf/qui` 保持干净并仍指向 `9045e1b`；M1 worktree 未触碰。
- 做法：先把已跟踪改动导出为补丁、另存 `git stash create` 提交对象（引用 `refs/s1-backup/pre-rebase`）并复制全部未跟踪文件，再清理已跟踪改动、快进分支，最后以 `git apply --3way` 重新应用补丁。未跟踪文件（`fields_test.go`、`report.go`、`report_test.go`、`save_test.go`、`migrations/0133_subscription_source_results.sql`）全程保留。没有 `reset --hard`、`stash drop` 或其他数据删除。
- 冲突与解决（4 份共享文档，均为 S0 与 S1 同时修改）：`docs/release-notes-1.4.0.md`（保留 S0 的刷新修复行、S0 两行与 S0 声明，插入 S1 行并更新 S1/S0 行的现状）、`docs/subscription-import-compatibility-plan.md` 与 `.en.md`（保留 S0 状态段原文，只在状态行追加 S1 现状；§14.5 补 `PART check EXIT 0`/`PART cores EXIT 0` 原文）、`docs/subscription-import-compatibility-change-log.md`（整文件冲突源于行尾：库内该文件为 CRLF，S1 副本为 LF；按“S0 全文 + S1 追加 65 行”重建并以 CRLF 写回，diff 变为纯新增、无行尾噪声）。S0 的文档内容与 S1 的实现记录均未回退，未新增任何 S1 Linux 门禁、浏览器走查或协议支持结论。
- flaky 测试修正（`save_test.go`）：原断言要求 `result.AttemptedAt != before.LastSuccess`，即“本次尝试时间必须不同于上次成功时间”；两次正确的调用可以落在同一时钟分辨率内，该不等式不是拒绝粘贴的有效判据。改为 `lastSuccessAt` 逐字符不变 + `attemptedAt` 通过 `validStamp`，不加 sleep、不改生产逻辑，行、缓存与订阅可用端点断言不变。`-run TestSaveRejectedPasteKeepsExistingSource -count=20`（40 个子用例）在整合前（`650b349` 基线）与整合后（`9045e1b` 基线）各跑一次，均全部通过。
- 本地验证（Windows，仅针对性测试，未跑 Linux 门禁）：`go test -count=1 ./internal/panel/subscription/...`（ok）、`./internal/panel/system/backup/...`（ok 98.6s）、`./internal/platform/recovery/...`（ok 65.3s）、`./internal/platform/server/...`（ok 225.5s）；`npm.cmd --prefix web run typecheck`（exit 0）与 `run build`（exit 0，vite 6.14s）；`node web/scripts/import-refresh-selftest.mjs`（PASS）；`gofmt -l`（24 个改动 Go 文件无输出）；`go vet`（importer、backup 通过）；`git -c core.whitespace=cr-at-eol diff --check`（无输出）。
- 未完成与限制：工作树仍未提交、未暂存、未推送、未打标签、未发布；Linux 门禁、真实订阅抓取与浏览器走查仍未执行，Windows 结果不替代 Linux/主机/浏览器证据；S1 待主助手本人最终审阅与阶段提交门禁；S2–S4、M2–M4 未开始（M1 在并列分支单独进行）。
- 后续提交约定：提交信息结尾使用 `Co-Authored-By: Claude Code <noreply@anthropic.com>`。

## 2026-11-05：S1 审查归属与状态更正、阶段提交（本地，Linux 门禁待执行）

- 请求：主助手本人最终审查已通过 S1 的代码与测试（基线 `9045e1b`），本轮只做窄范围文档更正后提交阶段工作：更正审查归属（所有审查一直由主助手本人完成，Owner 不承担验收工作）、把 S0 提交 `9045e1b` 的门禁记为已通过、把计划 §14.8 的基线改为 `9045e1b` 并补上 M1 状态、把措辞改为提交就绪；不新增实现改动、不合并主干、不推送、不打标签、不发布。
- 更正内容：
  - 审查归属：本记录此前把主助手的审查写成“Owner 本人审阅/复核”，现统一改为“主助手本人审查/复核”；Owner 对产品范围的决策、授权与真实操作仍记为 Owner。历史顺序更正为：主助手本人首轮 5 项阻塞 → 第二轮 5 项阻塞 → 1 项取消回归（此前“5 项 + 1 项”的表述不准确）。更早的 S0 与刷新修复条目中的“独立审阅/审阅方独立核对”指的同是主助手本人的审查，那些条目按原日期保留原措辞，不重写历史。
  - S0 门禁：`9045e1b` 的 Linux 门禁已由主助手本人核对通过（`check` 1995s、`cores` 574s，`GATE 9045e1b PASS total=2569s`，START 2026-11-05T10:00:34Z，END 10:43:23Z）；文档中“该提交门禁待执行”的说法已全部替换，S0 文档与夹具仍与 `9045e1b` 逐字节一致。
  - 计划与状态：英文版 §14.8 的 “based on `650b349`” 改为 `9045e1b`，状态行去掉“其余全部未实施”的错误表述（刷新修复与 S0 已交付、S1 已实现）；M1 记为并列分支 `feat/v1.4-selfhost-m1` 的阶段提交 `3565153`，其 Linux 门禁运行中（未整合、未门禁通过、未完成功能验收）；迁移编号起点改为 0133 之后（0133 属 S1，M1 无需迁移）。
  - 交接文档：三份计划文件自 `84e8725` 起已跟踪，不再写作“untracked”；C6 记为 Owner 已批准实施、工作正在进行。
- 阶段提交：只包含 S1 交付的 36 个文件（31 个已跟踪修改 + 5 个新增：`fields_test.go`、`report.go`、`report_test.go`、`save_test.go`、`migrations/0133_subscription_source_results.sql`）；没有临时备份或草稿文件入库，没有实现改动，未合并主干。
- 未完成与限制：阶段提交自身的 Linux 门禁尚未执行；M1 的阶段门禁仍在测试机上运行，S1 门禁按串行规则等其结束后再用同一脚本跑一次；真实订阅抓取、浏览器走查与协议支持（S2–S4、M2–M4）仍未执行或未实现，Windows 结果不替代 Linux/主机/浏览器证据。
- 后续提交约定：提交信息结尾使用 `Co-Authored-By: Claude Code <noreply@anthropic.com>`。

## 2026-11-05: S1 Linux gate correction — migration checksum

- The gate for `ccf23e4` failed `TestAppliedMigrationsAreImmutable`: the new `0133_subscription_source_results.sql` was not pinned in the checksum map. The migration itself is unchanged.
- Added its SHA-256 (`5745d8bfd47ddf9fe905434598c389650eb6941a0076b5b53945b01dec3c2d50`) to `migrations/checksums_test.go`.
- Windows targeted `go test ./migrations -run TestAppliedMigrationsAreImmutable -count=1` passed. A Linux gate on this correction commit is required; no passing result is claimed here. The original gate's real-core part was still running when this correction was prepared.

## 2026-11-05：M1 自建契约接入真实能力上报（未提交，待个人复核与 Linux 门禁）

- 请求：主检阅认为 M1 不能以“能力契约留到 M2”结项——原 `pkg/contracts/desiredstate/settings_contract.go` 与 `internal/panel/node/settings_contract.go` 明写 M1 不加字段、`SettingsContractCapability` 恒为零报告，而计划 §10 M1 与 §13.3 第 3 条要求现在就具备逐核／方向／版本／精确组合契约。本轮只补这一缺口：不提交、不合并、不跑完整 Linux 门禁、不推送／打标签／发布、不碰生产与真实源；评审仍由主检阅负责。
- 基线：worktree `C:/Users/luanf/qui-wt/m1-contracts`，分支 `feat/v1.4-selfhost-m1`，HEAD `9045e1b`，保留原有未提交改动。
- proto 变化（只做加法，无迁移）：新增 `message SettingsCombination {protocol,direction,security,transport,flow}`；`CoreCapability.server_settings_contract_version = 10`、`server_settings_combinations = 11`（逐核）；`Capabilities` 同名字段 23／24 作为单核标量形式。先核对 schema 再取下一未用编号，未改任何既有字段编号；用 `.tools` 的 protoc 36.2、protoc-gen-go v1.36.11 与 protoc-gen-go-grpc v1.6.2 重新生成 `pkg/contracts/agentpb`（与仓库既有生成版本一致），未手写生成代码。
- 接线：`MultiRuntime.DetectCapabilities` 按各适配器上报复制到对应 `CoreCapability`；`control.reportCapabilities` 的单核装饰与面板 `node.EnabledCores` 的单核回退同样保留这两个字段；面板 `node.SettingsContractCapability` 只把上报转换为共享 `CoreSettingsCapability`（空安全／传输按 `ServerSettingsCombination` 相同口径归一），不推断来源；`validateMultiCapabilities` 用共享 `SettingsCombination.Valid()` 校验一次上报结构——组合缺版本、方向／传输非法、重复或超过 64 条都拒绝整条上报，未知版本仍接受（不断连）但不授权。
- 生产行为不变：Xray／sing-box 适配器尚无渲染器，`detect` 构造能力时不写这些字段，因此生产 Agent 上报版本 0 与空组合，保存／快照／发布三处门禁一致关闭；未启用或宣传任何 Reality／xhttp 组合，也未声称渲染或客户端映射完成。
- 测试：新增 `pkg/contracts/agentpb/settings_contract_wire_test.go`（二进制与 protojson 往返、旧 peer 以未知字段保留并在重新序列化后值不变）；`internal/panel/node/settings_contract_test.go` 覆盖零报告、精确正例、他核／未知版本／错方向／未列组合失败关闭与畸形上报被拒；`internal/agent/core/multi_test.go` 与 `internal/agent/control/settings_contract_test.go` 覆盖 Agent 侧复制与旧标量装饰；`internal/panel/proxy`、`internal/panel/deployment` 的 settings_contract 测试用合成上报走通保存／快照／发布放行路径（部署用单核标量回退）；`pkg/contracts/desiredstate/reality_test.go` 补“Reality 指针 + xhttp 传输但缺 xhttp 设置”的拒绝用例，保留 TLS+xhttp 真实证书的有效覆盖。
- 整理：`reality.go` 删除自写 `equalBytes` 改用 `bytes.Equal`，压缩与仓库风格不符的重复说明文字；保留此前对独立 Reality／XHTTP 半边与 TLS+xhttp 证书互斥的修正。
- 验证（Windows，Go 1.26.0，`.tools` 工具链，`-p 1`）：`go build ./...` exit 0；`go vet ./pkg/... ./internal/panel/... ./internal/agent/...` exit 0；`gofmt -l pkg internal cmd migrations` 无输出；`go test -count=1` 通过 agentpb、desiredstate、node、proxy（整包）、deployment（整包）、agent/core、agent/core/xray、agent/core/singbox、agent/control。
- 未完成与限制：没有 Linux `scripts/check.sh`、没有真实核连接与浏览器走查；契约真实启用仍待 M2（Xray）与 M4（sing-box）渲染器；公开客户端模型与编解码仍属 S3／S4；无迁移（0133 留给 S1）。
- 后续提交约定：提交信息结尾使用 `Co-Authored-By: Claude Code <noreply@anthropic.com>`；本轮未提交。

## 2026-11-05: M1 independent-review corrections (local commit; gate pending)

- Scope: isolated `.worktrees/selfhost-m1-review`, based on `3565153`; local correction commit authorized after root review. No production actions, real subscription fetch, push or Linux gate.
- Reality SNI validation now rejects wildcards. Evidence is the cached pinned `github.com/xtls/reality@v0.0.0-20260322125925-9234c772ba8f/tls.go` lines 211 and 466: the configured server-name map is indexed by the exact ClientHello SNI. No additional native/connection result is claimed.
- Unknown nonzero settings-contract versions keep generic count/object/field-length bounds without applying version-1 enum validation. Their persisted reports replace old grants but never authorize settings. Tests cover scalar and multi-core reports with future enum values and stale-grant replacement.
- Apply and rollback dispatch require a newly accepted capability report in this connection for new settings and recheck the current exact combination. Stored reports and identical accepted-envelope replays cannot authorize the new session. Capability withdrawal withholds queued commands while ACKs and unrelated control work continue; legacy settings require no new report. The Agent constructs fresh capability envelopes after Welcome; this uses sequence acceptance, not wall-clock ordering or version text.
- Backup inspection applies `ValidateSettings` with each row's protocol and direction only for the new fields/discriminators, preserving legacy draft semantics. Authenticated archive cases cover valid encrypted Reality/xhttp round-trip, mismatched keys, missing target, wildcard SNI, wrong protocol/role, missing typed blocks and invalid modes.
- Local targeted checks passed on Windows Go 1.27.1 with cached dependencies: desiredstate 0.590s, node 0.627s, backup archive matrix 14.220s; real mTLS control-stream apply/rollback reconnect, replay and withdrawal cases 2.778s. Additional regression passed: entire desiredstate 0.591s and node 13.431s packages; new stream cases plus existing enrollment/old-peer/device-gate cases 5.811s; Agent fresh-capability reconnect 1.504s. No Linux gate or real-core acceptance result is claimed; M2 has not started.
- Test rework: initial negative archive cases called `Source.Create`, which correctly self-verifies and refused them before the receive path. Negative fixtures now repack authenticated external archives; receiving inspection rejects each and valid encrypted/legacy cases still round-trip. Timestamp-based session gating was considered and discarded; actual Agent code constructs a fresh envelope after Welcome, and sequence acceptance avoids clock assumptions.
- Root review caught rollback revision zero: it clears runtime without carrying settings and must skip the new state lookup. Added the guard and a real mTLS old-peer case that receives the zero-revision command without a capability report, acknowledges success and verifies runtime removal. The full new control-stream matrix passed again (2.811s); no gate result is claimed.
