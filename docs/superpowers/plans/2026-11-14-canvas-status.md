# 方案分项状态实施计划

**目标：** 实现第 15 项切片 10 的只读状态接口及画布六项状态展示。

**依据：** `docs/v1.5.5-plan.md` 第 15 节、`docs/v1.5.5-item15-design.md` 第 7 节及本轮任务。

**实现：** Go/SQLite 读取现有方案、部署批次、运行修订和入口；Vue/TypeScript 展示独立状态，由纯函数转换文案。不新增依赖、迁移或推断缓存，不主动探测。

## 任务

- 后端：先增加状态投影测试；新增 `canvas_status.go`，经部署模块适配读取批次；在 `canvas_handler.go` 登记 GET 路由，沿用现有画布读取权限。
- 前端：先增加 `canvas-status-selftest.mjs`；在 API、纯函数模块、独立卡片和 `RelayCanvasPage.vue` 接入六项状态；进行中批次每 3 秒刷新，终态停止，切换方案与卸载清理轮询。
- 集成：重读并更新门户路由清单；核对探测可关联性与只读保证；补充设计说明。
- 验证：gofmt -l、proxy 包 vet/test、server Portal 测试；vue-tsc 与 status/library/remove/ui-regression 四个前端自测。保留原始输出，审查修正后仅重跑受影响检查。

## 约束与审查重点

- 不暂存、提交、推送、切分支或覆盖现有工作；不改 backup/recovery、importer/node/rules 和 `canvas_import_usage.go`。
- 编辑采用 UTF-8 无 BOM、LF；不删除或整文件重写。
- 批次接收不等于 Agent 确认；配置条件不等于真实连通或所有用户均有权限。
- 未发布、修订缺失、失败及未探测均明确展示；读接口不得借用会写库的刷新函数。
- 单节点发布没有批次 ID 时保持批次缺失，不虚构成功批次。
- 服务端已有画布读取为 Owner-only，本接口保持相同权限。
