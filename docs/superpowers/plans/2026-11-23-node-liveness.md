# B1 在线判定实施计划

依据：[存活判定设计](../specs/2026-11-23-node-liveness-design.md)。2026-11-23 Owner 要求 B1 与 B3 一起完成，并明确允许相关回归和最窄检查；不含提交发布或生产操作。

执行沿用当前工作分支，不开启子代理。B3 按[数据库工具计划](2026-11-23-update-database-go.md)实施；两项保持无迁移、最低兼容 v1.7.5 和提交后 Ack。

- [x] 新增唯一 `node.Liveness`，按身份和会话 token 记录连接、最近接收和在途消息；处理阶段收敛为 `waiting_db`、`sending_ack`。10 秒延迟、60 秒重复日志、1 秒观察。接收错误与 context 取消独立撤销保活，旧 token 不能清理新会话。
- [x] Control 在接收成功且纯内存边界校验通过后、进入 SQL 之前登记在途；握手、普通处理和 Ack 完成/失败均结束对应 token。失败保留原始错误；不提前 Ack、不刷新持久化心跳。
- [x] 将同一实例注入只读节点、运行快照、总览、门户、订阅和通知读取。新增展示事实字段；持久化状态与时间保留，控制准入和指标时效不改。维护、禁用、删除、drain、身份替换不能保活。
- [x] 内存变化通过既有快照队列触发 SSE，读取使用已有读池；有效期 45 秒，前端沿用 30 秒校准及断连冻结时钟。字段贯通列表、详情、画布，持续显示处理延迟；证据过期明确提示等待校准。
- [x] 先写失败回归：会话隔离、超时在途、独立断流、离线规则、禁用/维护、失败不 Ack、读写状态隔离；再实现。真实占用唯一写连接验证接收后等待、超过 45 秒不误报、断流独立失效；不以“没有通知”代替通知评估。
- [x] 前端回归覆盖字段投影、状态有效期、核心异常/指标过期保留。只运行涉及包及已有相关脚本，Linux 连接池/权限验证在隔离测试目录执行；通过后不重复。
- [x] 同步项目说明书、架构、运维与两份设计状态；记录实际覆盖和未运行项。最终审查包括 B3 工具快照与旧版降级，不运行全仓发布门禁。

明确不包含：独立 Telegram/Webhook 发送路径、写入队列重构、Agent 心跳协议变更、多副本面板、生产升级。页面和日志中的处理延迟必须保留，不能把传输存活说成数据库成功或核心健康。


## 当前交付与检查（2026-11-23，未发布）

| 文件 | 作用 |
| --- | --- |
| `internal/panel/node/liveness.go` | 单实例连接/会话/在途事实、45 秒证据、延迟观察与过期断流记录回收；不执行 SQL |
| `internal/panel/node/control.go` | 接收校验后登记、独立断流/context 失效、Welcome/Ack 完成、失败原因日志 |
| `internal/panel/node/repository.go` | 保留原始持久状态，装配只读事实，提供提交后的身份失效钩子 |
| `internal/panel/node/settings.go`、`identity_recovery.go`、`deletion.go` | 禁用、立即撤销恢复和删除提交后立即失效；删除预览使用统一解释 |
| `internal/panel/node/availability.go` | 连接存活与数据库心跳分开解释，保留降级/核心异常及优先级 |
| `internal/panel/node/runtime_snapshots.go` | 读池合并内存事实、revision/SSE、校准时间与轻量变更队列 |
| `internal/panel/dashboard/repository.go`、`internal/panel/subscription/availability/view.go` | 总览计数与完整订阅线路过滤合并同一存活事实 |
| `internal/panel/notification/evaluator.go`、`internal/platform/server/server.go` | 离线/恢复统一解释和延迟文案，单实例装配与读池分流（含门户） |
| `web/src/modules/nodes/api.ts`、`runtimeSnapshots.ts`、`useNodeRuntime.ts` | DTO、SSE 白名单、30 秒校准、服务器时钟校准、过期重校准 |
| `web/src/modules/nodes/components/serverListModel.ts`、`ServerInventory.vue`、`ServerCardGrid.vue`、`detail/ServerOverview.vue` | 统一状态解释，列表/卡片/详情持续显示延迟；真实指标年龄保留 |
| `web/src/modules/proxy/api.ts` | 画布引用贯通存活事实与原始业务状态 |
| `internal/panel/node/liveness_test.go`、`liveness_control_test.go`、`liveness_runtime_test.go` | 会话隔离、过期/断流、真实单写连接占用、失败无 Ack、读池 SSE 回归 |
| `internal/panel/notification/evaluator_test.go`、`internal/panel/subscription/availability/view_test.go` | 离线/恢复通知及禁用、维护、订阅过滤回归 |
| `web/scripts/node-liveness-selftest.mjs`、`node-runtime-sync-selftest.mjs` | 字段投影、过期提示、服务器时钟偏差、指标隔离与既有重连冻结回归 |
| `docs/project-spec.md`、`architecture.md`、`operations.md` 和两份设计/计划 | 现行规则、模块边界、排查步骤与实施证据；B3 文件职责另见对应计划 |

检查：Linux node/notification/availability 包 `go test -race -p 1` 通过；dashboard/portal/server 相关包普通回归与涉及包 `go vet` 通过。前端 node-liveness、server-runtime、node-runtime-sync、canvas-library、inbound-state 自检及 `npm --prefix web run typecheck` 通过。B3 的 51 个更新场景与真实旧版本材料检查见对应计划。

裁定：阶段收敛到 `waiting_db` / `sending_ack`。Recv EOF 立即撤销保活，但保留半关闭前收到的有效消息按原合同提交和确认；非 EOF 传输错误取消 SQL 等待。沿用当前 feature checkout，没有开启子代理或操作 Git 工作区历史。无新增数据库迁移，没有扩大到控制准入、写队列或 Agent 协议；兼容底线保持 v1.7.5。

未运行：真实浏览器/生产环境验证、完整发布归档、arm64 执行、全仓发布门禁与 GitHub check。未暂存、提交、推送、打 tag 或发布。
