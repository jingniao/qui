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
