# 节点离线持续时间 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 管理后台共用节点离线通知持续时间，断线等待期保留卡片。

**Architecture:** 原始控制状态不变。节点及运行快照提供 `offlineAfterSeconds`，前端共享状态函数计算等待与离线；保存规则刷新快照。

**Tech Stack:** Go、SQLite、Vue 3、TypeScript。

**Spec:** `docs/superpowers/specs/2026-11-20-node-offline-duration-design.md`

后续批次：Owner 已要求合并到 v1.7.5 发布，并明确允许现有测试和发布门禁，不新增测试方法、实体或 mock。下文“不运行测试、不提交”的记录描述首次独立实施阶段；本次统一验证与发布状态见 [v1.7.5 发布说明](../../release-notes-1.7.5.md)，生产不部署。

## Global Constraints

- 用户已授权按方案实施；当前工作区内修改，不提交、不部署，不新增或运行测试。
- 无默认值兜底，缺少规则时暴露读取错误；通知开关不改变持续时间。
- 控制心跳、指标新鲜度及写操作准入不延长。

## Review Focus

- 0 分钟、没有历史心跳、断线后未超时和恢复各分支。
- 保存规则缩短或延长持续时间后，已有 SSE 快照不能覆盖新设置。
- 禁用通知时，恢复事件不再发送。
- 节点等待状态优先于发布徽标，且不被离线筛选或隐藏误伤。
- 前端等待状态不参与写操作可用性判断。

---

### Task 1: 后端展示字段与规则刷新

**Files:** `internal/panel/node/repository.go`、`runtime_snapshots.go`，`internal/panel/notification/handler.go`、`evaluator.go`，`internal/platform/server/server.go`。

- [x] 节点读取和运行快照在原读取中取得规则持续时间，附带 `offlineAfterSeconds`，保留原状态。
- [x] 保存规则后刷新快照，广播已有节点及总览事件；错误显式返回。
- [x] 禁用通知时不发送离线恢复通知，恢复仍清理状态。

### Task 2: 前端展示统一

**Files:** 节点 API/运行快照类型、`serverListModel.ts`、节点配置页、服务器详情、总览、版本状态及必要的状态类型。

- [x] 增加规则时间字段，快照覆盖时同步；共享状态函数输出 reconnecting 展示状态。
- [x] 节点列表保留等待卡片并显示等待恢复，超时沿用隐藏规则。
- [x] 服务器详情、总览、版本展示共用判定，操作仍使用原始连接状态。

### Task 3: 文档与静态检查

**Files:** `AGENTS.md`，相关运维说明，本文。

- [x] 更新状态语义与通知规则说明。
- [x] 执行本次前端类型检查、Go 定向编译和 diff 格式检查；不运行测试。
- [x] 只读审查最终 diff，记录结果和未覆盖的浏览器场景。

## 检查结果

- `npm.cmd --prefix web run typecheck`：最终退出 0。首轮发现 reconnecting 未同步到关联画布展示类型，补齐后通过；随后新增审查修正后再次通过。未放宽 TypeScript 检查。
- 使用本机 `.tools/go/bin/go.exe` 执行 `go build ./internal/panel/node ./internal/panel/notification ./internal/platform/server`：退出 0。只编译，不运行测试、不启动服务。
- `git diff --check`：通过；仅对本轮修改的 Go 文件执行 gofmt。
- 测试、浏览器断线／恢复及跨标签页保存实机验证：未运行。生产环境未操作，没有新增数据库迁移或依赖。
- 没有需要用户额外决定的行为；新增展示覆盖与刷新校准均在已确认方案范围内。

## 实施记录

- 在用户指定的当前工作区实施；没有创建工作树、提交、发布或部署。
- 独立只读审查发现并修正：总览状态时钟与规则变更刷新、卡片资源指标的 45 秒有效期、详情和版本页在途读取的变更补读。删除不必要的总览流量事件全量读取，避免频繁查询。
- 节点配置页新增可见时每 60 秒服务器状态校准，防止 SSE 丢失导致离线时限长期不更新。复用原读取及其明确错误提示，不使用旧时限默认值。
- 同步既有自测的生产函数依赖、已有节点字段与源码提取范围；没有新增用例、测试方法、实体或 mock，没有改变断言与期望值，没有运行测试。
- 假设：展示等待期从面板最后有效心跳开始；0 分钟仍需确认断线或原心跳过期，未改变控制可用性。保持已有维护、禁用与未登记优先级及中转归属规则。

## 逐文件说明

| 文件 | 修改 |
| --- | --- |
| `internal/panel/node/repository.go` | 节点读取附带离线持续秒数，保留原始状态与心跳判断 |
| `internal/panel/node/runtime_snapshots.go` | 实时快照附带并跟踪持续秒数变化 |
| `internal/panel/node/identity_recovery.go` | 同步共享查询新增字段的手写 Scan 顺序 |
| `internal/panel/notification/handler.go` | 规则保存后调用展示刷新，刷新失败明确告知规则已保存 |
| `internal/panel/notification/evaluator.go` | 规则禁用时不发恢复通知，恢复仍清理状态 |
| `internal/platform/server/server.go` | 装配规则变更快照刷新与既有节点事件广播 |
| `web/src/modules/nodes/api.ts` | 节点类型增加持续秒数字段 |
| `web/src/modules/nodes/runtimeSnapshots.ts` | 快照应用同步持续秒数字段 |
| `web/src/modules/nodes/components/serverListModel.ts` | 共享等待恢复与离线展示判定 |
| `web/src/modules/nodes/components/ServerCardGrid.vue` | 延迟区域显示等待恢复；旧资源指标不延长有效期 |
| `web/src/modules/nodes/pages/NodeDetailPage.vue` | 详情提示共享展示状态；事件遇到在途读取时补读 |
| `web/src/modules/proxy/pages/ProxyNodeListPage.vue` | 等待期保留节点卡片、增加等待筛选与可见状态校准 |
| `web/src/modules/proxy/proxyNodes.ts` | 节点及关联展示类型支持 reconnecting |
| `web/src/modules/dashboard/pages/DashboardPage.vue` | 等待计数、提示、共享时钟与规则变更读取 |
| `web/src/modules/system/components/NodeVersionPanel.vue` | 版本状态共享展示判定与时钟；事件在途补读 |
| `web/src/modules/system/components/NotificationSettings.vue` | 说明持续时间共用、取消勾选只关闭通知 |
| `web/scripts/inbound-state-selftest.mjs` | 原用例源码提取范围同步新增状态表达式 |
| `web/scripts/ui-regression-selftest.mjs` | 原用例接入真实共享状态函数和 Vue ref，补齐已有节点字段 |
| `AGENTS.md` | 登记展示字段、规则时限与控制边界 |
| `docs/operations.md` | 更新离线规则和等待状态运维说明 |
| `docs/superpowers/specs/2026-11-20-node-offline-duration-design.md` | 记录用户已确认设计 |
| 本文件 | 记录实施清单、逐文件说明与验证边界 |
