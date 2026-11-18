# v1.6.1 发布说明

**发布准备中。** 迁移基线 **0152_baseline** ＋ **0153**，迁移头 **0153**。范围见[规划](v1.6.1-plan.md)，界面改动逐项记录见[前端修改记录](v1.6.1-ui-changes.md)。

## 变更内容

- **以 v1.6.0 为最低版本精简代码**：0001–0152 合并为 `migrations/0152_baseline.sql`。已在 v1.6.0 的数据库按旧的 152 条迁移记录识别（校验和保存在 `internal/platform/db/legacy.go`），只追加 0153；只记录了部分旧迁移的数据库启动报 `ErrPredatesBaseline`，`--check-database` 报 `outdated`，须先升到 v1.6.0 并启动一次。备份只接受 v1.6.0 及以后的归档。
- **删除一次性转换**：0124 画布隧道认领、0131 画布转发转换、0147 导入身份转换与 0150 残留清理的代码删除；0153 删除它们的标记与报告表及 `app_metadata`，任一标记未完成时 0153 失败、数据库不变。备份与恢复去掉约 40 处旧归档分支，清单计数去掉 `relayCleanupReconcile`、`importIdentityMap`、`canvasForwardReconcile`、`tunnelMigrationReport`。
- **删除的接口**：`GET /api/v1/canvas-tunnels/legacy-objects`、`POST /api/v1/canvas-tunnels/{id}/adopt`、`POST /api/v1/canvas-tunnels/{id}/rotate-credentials`、`GET /api/v1/tunnels/migration-report`、`POST /api/v1/policies/simulate`（含「策略模拟」页面）与 `POST /api/v1/releases`（手工登记签名 Agent 版本）。
- **Agent 版本只保留最新**：升级对话框与 `GET /api/v1/agent-releases` 只给 GitHub 最新正式版，导入只接受它；导入成功后与面板启动时删除旧版本的清单、归档以及没有任务引用的目录行。未完成任务需要的文件保留。
- **`qui-update` 不再降级 Agent**：目标版本低于已安装 Agent 时，在下载与停止服务前拒绝；组合主机只回退面板须加 `--component panel`。升级失败的自动恢复不变。
- **总览服务器价值**：「核心运行」卡片换成服务器总价值、月均支出与剩余价值，可在八种币种间换算；新增 `GET /api/v1/dashboard/exchange-rates`（ExchangeRate-API 日汇率，按供应商更新时间缓存，失败返回 502 `exchange_rates_unavailable`，不使用过期汇率）。
- **界面调整**（Owner 2026-11-18）：服务器卡片布局（系统图标、离线去色、四列指标、标题行网速）、新增／编辑服务器共用表单（连接地址、默认端口范围、整机流量设置）、「版本与更新」合并 Agent 与核心、全站协议徽标统一配色、节点分组用复选框授权、节点列表隐藏维护与离线服务器的自建节点、总览卡片排成一行。

## 迁移与升级注意

1. 面板须已在 v1.6.0 且启动过一次。0153 只删除标记表，不改业务数据。
2. 升级前保留更新器备份。回到 1.6.0 时数据库已有 0153，需用更新前备份。
3. v1.6.0 之前的加密备份在 1.6.1 上不能恢复；升级后先做一份新的加密备份并校验。
4. Agent 协议与能力位门禁不变，1.5.8 的 Agent 照常连接与网页升级。

## 已知限制

- 界面改动未做浏览器实机验收。
- 汇率来自第三方免费接口，只是日汇率，不是实时报价。

## 发布验证

提交前：基线与旧迁移链在空库上逐个对象比较，175 个表／索引／视图／触发器的 SQL 完全相同，内置数据只有生成时间与四个一次性标记不同，外键检查为空；Windows 上受影响的 Go 包测试通过；`quick-check.sh --release` 通过（49 项前端自测全部通过）。

（门禁与发布记录发布后填写）
