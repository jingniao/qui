# 流量存储淘汰实施计划

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task-by-task. Owner 已确认设计并要求实现；直接执行，不重复请求方案批准。

**Goal:** 安全合并核心历史明细，淘汰双方确认完成的新协议回执。

**Architecture:** access 模块拥有汇总、统一节点查询及回执检查点。Agent 在认证 spool 中持久编号与删除确认；控制协议传递身份和确认，备份恢复保留整组事实。

**Tech Stack:** Go、SQLite、protobuf/gRPC、现有 Vue 页面。

**Spec:** `docs/superpowers/specs/2026-11-18-traffic-storage-retirement-design.md`

## Global Constraints

- 核心明细保留 7 天；回执确认后至少等待 24 小时；旧无编号回执保留。
- 不新增或运行测试，不提交、不推送、不部署、不操作生产数据。
- 不改已应用迁移；不删累计游标；不把缺失证明当成功。
- 当前功能分支原地执行，开始时仅有本任务设计文档未跟踪，无他人代码改动。

## Review Focus

- 迟到样本和重复整理不能重复统计，整数溢出明确报错。
- 历史查询切入汇总小时必须明确报告精度不足。
- ACK 丢失、乱序隔离和 pending 续做不允许提前淘汰。
- spool 的编号和删除确认必须纳入认证状态，不能单独回退。
- 恢复同时保留统计和封存状态，旧协议不能绕过已切换节点。

## Tasks

- [x] 1. 新迁移：核心小时表、任务状态、队列检查点、回执及 pending 交付身份。
- [x] 2. access 核心整理事务与统一查询；更新 dashboard、月统计、审计查询和精度错误展示。
- [x] 3. protobuf 能力协商、固定交付身份与删除确认；生成 Go 协议代码。
- [x] 4. Agent spool 编号、认证升级、ACK 事务及控制发送确认。
- [x] 5. 面板交付绑定、完成前缀、二次确认、24 小时封存和回执分批淘汰；接后台生命周期。
- [x] 6. 备份材料、清单、恢复、节点删除和存储状态展示同步。
- [x] 7. 项目文档、最窄静态/编译检查与独立代码审查；测试记录为未运行。

## 执行记录

- 设计已获 Owner 批准。测试限制覆盖技能的 TDD 默认流程，不为此重复索取授权。

- 任务 1–6：代码已落地；protobuf 生成、相关 Go 包编译、Vue/TypeScript 类型检查退出 0。未新增或运行测试。
- Ruling: 保留 `retained_account_samples` 已发布 scope 值，文档更新实际合并口径，避免无关 API 变更；错误理解时会影响调用方对历史覆盖的判断。
- Ruling: 当前功能分支原地实施，不建 worktree、不提交；初始无他人代码改动。测试禁令优先于技能默认 TDD；运行时边界尚无测试证据，发布前仍须另行验证。
- 原始表已有 `core_traffic_time` 索引，未重复建索引。统一查询增加可走原始时间索引的粗筛，精确比较保持规范化纳秒。
- 无协议身份的旧回执保留、节点逻辑删除保留历史；未扩大为旧回执清理或生产操作。
- 最终独立只读审查发现 3 项，集中修复：确认 oneof 未纳入入口校验；旧冻结证据缺新表；显式身份恢复无法换队列。未新增测试，修复后以最窄编译、类型与格式检查交付，运行时场景仍未验证。
- 第一项：validateMessage 校验独立确认的队列身份和 int64 进度，控制层还要求批次／确认队列匹配本次握手。
- 第二项：冻结证据认证后按 Manifest 摘要复制 database/panel.db，迁移副本、checkpoint 关闭后附加，原始证据不变。
- 第三项：队列主键改为节点＋队列身份，唯一活动索引；迁移中的 access 所有权触发器仅在恢复记录由 issued 核销为 consumed 时封存旧队列，与换证同事务；不为 node 引入 access 父包依赖，避免既有同包检查导入环。新身份可登记新队列，或在原 spool 连续性校验后继续原队列；旧代检查点／回执／pending 保留，确认和淘汰均限定具体队列。协商事务核对当前证书指纹，避免恢复换证期间旧握手抢占新队列。
- Ruling: 对审查未评价的生产回收率及故障／性能结果，只记录未部署和未测试，不推断数据收益或运行通过；成本是这些结果仍需发布前独立验证。
- 无延期 Minor。未提交、未暂存、未推送，保留当前分支与全部改动。

## 交付检查

- 协议代码：protoc 生成成功。
- Go：相关 access、dashboard、node、backup、server、recovery、Agent spool/control 包编译退出 0（修复后复核）。
- 前端：vue-tsc --noEmit 类型检查退出 0。
- 格式：gofmt 已执行；git diff --check 无输出、退出 0。
- 测试：未新增、未运行；未执行测试任务、故障注入、实机验收或临时数据库演练。
- 部署／生产操作：未进行；未运行 VACUUM，未清理旧回执。
- 假设与范围：采用已批准的 7 天、24 小时及旧回执保留边界；沿用已有节点逻辑删除、额度计量与已发布 scope。无超出原定范围的修改，无等待 Owner 决定的实施项。发布与生产应用另行安排。
- 遗留验证：运行时一致性、崩溃／断流／恢复、性能和实际空间收益未验证；未把静态检查当作这些场景通过。

## 修改文件清单

| 文件 | 用途 |
| --- | --- |
| `AGENTS.md` | 更新产品行为、查询精度、回执淘汰及恢复合同。 |
| `docs/README.md` | 加入本功能设计与实施记录入口，标明未发布／未部署。 |
| `docs/architecture.md` | 补充汇总、检查点、队列代际与恢复边界。 |
| `docs/operations.md` | 说明整理观察、冲突处置、恢复与空间预期。 |
| `docs/superpowers/plans/2026-11-18-traffic-storage-retirement.md` | 记录实施步骤、裁决、审查修复、检查证据及文件清单。 |
| `docs/superpowers/specs/2026-11-18-traffic-storage-retirement-design.md` | 保存已批准设计和实现边界，更新交付状态。 |
| `internal/agent/control/control.go` | 能力协商、固定交付信封、ACK 身份校验和持久删除确认。 |
| `internal/agent/spool/bound_store.go` | 支持认证头 v2，按版本验证和提交摘要。 |
| `internal/agent/spool/traffic.go` | 入队时原子分配编号，ACK 删除更新认证状态。 |
| `internal/agent/spool/traffic_delivery.go` | 队列身份、已有队列编号、连续确认进度及 v1→v2 认证升级。 |
| `internal/panel/access/account_query.go` | 节点级汇总改用统一历史查询，用户／设备口径保留。 |
| `internal/panel/access/account_traffic.go` | 统计事务校验并持久保存交付身份。 |
| `internal/panel/access/core_history.go` | 旧核心明细分批原子汇总，统一读取原始／小时数据并校验精度和溢出。 |
| `internal/panel/access/handler_http.go` | 审计节点汇总透出历史精度错误。 |
| `internal/panel/access/monthly.go` | 月统计合并核心小时历史，精度不足返回 422。 |
| `internal/panel/access/recovery.go` | 整组保留当前冻结库的汇总、任务、队列和回执材料。 |
| `internal/panel/access/storage_observation.go` | 观察响应加入流量任务、队列和新表规模。 |
| `internal/panel/access/traffic_delivery.go` | 队列协商／代际恢复、完成和确认检查点、候选计时与安全淘汰。 |
| `internal/panel/access/traffic_pending.go` | 待续输入持久交付身份，后台续做加载身份，完成后推进连续前缀。 |
| `internal/panel/access/traffic_pending_recovery.go` | 恢复 pending 并集时校验／保留固定绑定。 |
| `internal/panel/access/traffic_service.go` | 将传输交付身份带入原计量链路。 |
| `internal/panel/access/traffic_storage_material.go` | 备份／恢复校验小时历史、队列代际、检查点与回执／pending 绑定。 |
| `internal/panel/access/traffic_storage_observation.go` | 读取整理进度和各节点确认／缺口／恢复状态。 |
| `internal/panel/dashboard/repository.go` | 总览与趋势读取小时历史，月精度错误单独返回。 |
| `internal/panel/node/control.go` | 接入新协议和确认消息，入口校验、固定原因码与 ACK 元数据。 |
| `internal/panel/system/backup/inspect.go` | 备份材料校验纳入新存储组。 |
| `internal/panel/system/backup/inventory.go` | 增加小时汇总与队列清单计数，旧归档可无新表。 |
| `internal/platform/recovery/database.go` | 认证冻结证据后迁移其摘要校验过的副本，再只读附加。 |
| `internal/platform/server/server.go` | 将整理接入已有后台生命周期并保留失败日志。 |
| `migrations/0154_traffic_storage_retirement.sql` | 新增小时历史、任务、各代队列与绑定索引；显式恢复核销触发原子封存。 |
| `pkg/contracts/agentpb/agent.pb.go` | 由 protoc 生成新消息和字段。 |
| `pkg/contracts/traffic/batch.go` | 定义独立交付元数据，不改变原 JSON 与摘要。 |
| `proto/agent.proto` | 增加能力位、固定交付字段、ACK 身份和独立确认消息。 |
| `web/src/modules/dashboard/api.ts` | 声明可选历史精度错误。 |
| `web/src/modules/dashboard/pages/DashboardPage.vue` | 月历史不可用时隐藏数值／比例／排行并显示原因。 |
| `web/src/modules/nodes/components/NodeRecoveryPanel.vue` | 恢复说明与确认内容交代流量旧队列封存和新队列登记。 |
| `web/src/modules/nodes/pages/NodeDetailPage.vue` | 月统计读取失败时清除旧值，显示已有错误提示。 |
| `web/src/modules/nodes/pages/NodeListPage.vue` | 月统计读取失败时清除旧值，保留精度错误原因。 |
| `web/src/modules/system/components/StorageObservationPanel.vue` | 展示整理进度、节点检查点及 7 天／24 小时边界。 |
| `web/src/modules/system/storageObservationApi.ts` | 增加整理观察响应类型与保留规则。 |


### v1.7.0 发布前兼容性修正

受管升级失败会用同一数据目录恢复旧 Agent。新编号队列使用 spool ABI 7（认证头 v2），不能在 OpenBound 时直接迁移：先由监督进程确认原始基线、当前和上一签名镜像都能读取该格式，且没有未完成升级任务，再持久提升存储 floor；worker 只在 floor 已提升后迁移。独立 Agent 同样校验原始认证契约。v1.6.1 回滚镜像仅支持旧格式，因此升级节点保留原流量上报并明确记录“回执淘汰未启用”，不删除旧回执；面板核心明细按小时整理独立生效。新装节点可在证明通过后启用新编号协议。此保护防止升级中断后旧镜像无法读取队列。

发布前扩充既有队列/格式回归断言，先复现无条件迁移再修复；真实镜像启动、恢复与完整套件纳入同 SHA Linux/GitHub 门禁。
