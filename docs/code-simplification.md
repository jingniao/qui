# 代码精简实施记录（未发布交接）

> 临时交接文档：来源为原会话 [772](codeg://session/772) 的四批代码精简规划及后续复审修复与文档清理。当前改动仅在工作区，未暂存、提交、推送、发布或部署，未操作生产。**下个正式版本发布后，将有效变更并入[变更记录](changelog.md)并删除本文件。**

## 1. 范围与状态

| 批次 | 内容 | 状态 |
| --- | --- | --- |
| 一 | 删除未引用前端页面/API/类型/helpers 与后端死函数、永假分支；缩减版本页分支 | 已收尾 |
| 二 | 共用字节换算（`web/src/app/bytes.ts`）、面板时区日期（`web/src/app/time.ts`）、规则集引用弹窗（`ServiceProviderDialog.vue`） | 已收尾（用户门户两处时间因无面板时区来源，保留浏览器时间） |
| 三 | 按 **v1.7.5** 最低兼容底线清理旧升级排队入口、旧浏览器草稿导入、旧 API/路由与更早版本兼容 | 已收尾（保留 v1.7.5 有效数据合同与既有 worker 任务恢复路径） |
| 四 | 拆分长说明、集中文档入口、删除冗余历史流水与合并重复文档 | 已收尾（本轮由 43 份进一步精简，现行索引见[文档导航](README.md)） |

## 2. 兼容底线与保留项

- **兼容底线**：后续仅要求兼容 **v1.7.5**（迁移头 `0158`）；Enroll 与 Hello 接入明确拒绝低于 `1.7.5` / dev / 缺版本 Agent 并提示终端升级（Enroll 在签发、CSR 与 token 消耗前检查）。`ProbeAgentImage` 要求被探测程序支持 v2 storage 与 control-only，不再降级兼容旧程序。
- **保留的 v1.7.5 有效数据合同**：安装记录 v1 读取、`user_source_grants` 表、`max_devices`/`max_connections` 列、历史 budget/weighted 修订、旧 checksum 链、`legacyRelayNodes`、只读 `port_forward`/GOST 画布、服务端 `importKey` 幂等去重、既有 worker 任务读取/重发/恢复路径与真实结果验真（仅停止为新任务排队旧 worker 升级）。
- **并发外部改动（保留原样，不计入精简范围）**：`internal/panel/access/traffic_delivery.go` 查询性能改动；`ServerValueCard.vue` 的 `nodeFinanceRevisions`/财务缓存版本、`nodeMeta.ts` 财务版本号及 `retained-refresh-selftest.mjs` 财务断言。
- **生产观察**：原会话只读记录生产面板为 1.7.5 / 迁移头 0158，本轮未连接生产，部署前须重新核实。

## 3. 复审修复与已跑检查

- **两个 P1 修复（2026-11-20）**：
  1. 撤回未发布的 `0159_retire_worker_agent_updates.sql`，迁移头保持 **0158**（0152–0158 字节与校验和不变），不再改写或拒绝归档中的 `queued`/`running`/`failed_closed` 历史任务。
  2. 恢复既有 worker 任务的原签名命令派发/重发、恢复 API 与恢复尝试；补回误删的 `PreparedRestore.Addresses` 与 `agentprocess.Start`。
- **已跑静态与编译检查**：
  - Linux 交叉编译 `GOOS=linux CGO_ENABLED=0 go build ./cmd/... ./internal/... ./pkg/...` 与终局补齐后 `go build ./internal/panel/node` 退出 0（不覆盖测试包）；`gofmt -l` 为空；`git diff --check` 退出 0。
  - 前端 `npm run typecheck --prefix web` 与 6 个自测脚本 `node --check` 退出 0（未运行自测场景）。
- **已跑定向测试（Windows/amd64、Go 1.27.1、CGO_ENABLED=0、`-count=1`，无新增测试/mock）**：
  - `internal/panel/deployment`：`TestAgentUpdateReservationReplayAndResults`、`TestUnconfirmedAgentRecoveryKeepsLateOriginalResultSeparate`、`TestAgentRecoveryAttemptsKeepOriginalFailureAndRequireFreshCapability`
  - `internal/panel/system/backup`：`TestAgentArchiveInventoryAndConflictingUpdates`
  - `internal/platform/recovery`：`TestOldPendingSnapshotUsesCompletedCurrentDeploymentWithoutReplayingCommand`
  - `internal/panel/node`：`TestAgentRecoveryCapabilityReconnectionClearsOldGrant`
  - `internal/platform/db`：`TestMigrateAcceptsCompleteLegacyChain`
  - `migrations`：`TestAppliedMigrationsAreImmutable`
  - `internal/platform/server`：`TestEveryAdminRouteRefusesAPortalSession`
- **未运行**：完整 Go/前端测试、发布门禁（`scripts/check.sh` + 真实核心套件）与 Linux 实机验收均**未运行**，不构成发布验收结论。
