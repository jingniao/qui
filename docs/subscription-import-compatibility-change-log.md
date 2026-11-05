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
