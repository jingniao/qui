# 提前准备展示数据：四批优化执行记录

状态：四批实现、相关回归与浏览器冒烟通过。Owner 后续授权发布 v1.7.2，发布提交 `aacf4db` 的完整 Linux 门禁和同 SHA GitHub 检查均通过；先前“不提交、不发布”的约束由本次发布授权替代，最初发布范围不含生产部署。Owner 后续另行授权部署，现已完成面板和三台指定 Agent 升级及页面复测；此前跳过的节点保持原版。v1.7.2 的附件与签名核验通过，结果见[发布记录](../../release-notes-1.7.2.md)和[生产验收](../../v1.7.2-production-acceptance.md)。

## 目标与契约

1. 运行状态：白名单 DTO、内存快照、SSE 局部更新、运行代次和节点版本，连接/恢复/每 30 秒轻量校准。保留完整卡片与旧值，真实离线仍显示；不添加恢复 banner 或 unknown 占位。
2. 流量：原始账本、去重、补传、额度不变。新增可重建小时读取索引；保留账户 epoch 来源选择、样本数、整数溢出和精度错误。完整小时用索引，边界小时读明细。同事务维护，压缩转移不重复累计。
3. 共享统计：同读事务、同时间范围、同配置准备日/月/趋势/整机统计，每 30 秒一轮，各页面复用。准备中、错误、旧结果的年龄明确；恢复后清空展示代次。
4. 分区加载：总览基础内容独立于统计；保留所有原卡片。服务器列表、详情只取所需节点结果。已发布 API 明确保留兼容行为。

## 分工与接口

- runtime implementer：node runtime / events / 前端节点状态；不得修改 machine_traffic.go 或共享统计。
- statistics implementer：共享统计、dashboard 后端、machine_traffic 只读重构；server.go 接线交 root。
- root：迁移/小时读取索引/恢复一致性、server.go 集成、前端统计集成及整体验证。
- access 提供 `ReadNodeTrafficSnapshot(ctx, *db.ReadSnapshot, from, to, nodeID, hourly)`，原 `ReadNodeTraffic` 保持可用。
- 不修改 Agent 协议，不引入 Redis、外置任务系统或新数据库。

## 执行与验证

- [x] 第 1 批：状态直推和卡片稳定，相关节点/Event/SSE/前端回归。
- [x] 第 2 批：小时索引/回填/边界查询，重复、来源切换、压缩、溢出、恢复对账。
- [x] 第 3 批：共享快照、错误可见、配置失效、生命周期与并发行为回归；发布阶段已补齐 Linux race。
- [x] 第 4 批：总览拆分/列表详情复用、局部失败和后台恢复回归。
- [x] 集成窄范围 Go 测试、前端自测和类型检查、独立审查。

生产目标：首屏 P95 ≤2 秒，热统计服务端 P95 ≤500ms，正常统计年龄 ≤35 秒；本次不部署，这些目标不能当作已验收结果。比较流量必须固定同一数据库快照和查询边界逐字节一致。测试失败不删断言或改期望绕过。

## 决策记录

- 继续在当前目录工作，遵守 Owner 先前选择；不创建工作树。实现阶段不提交，后续按 Owner 的 v1.7.2 发布授权提交并推送。
- 当前 SSE 队列可丢事件，版本号不能发现最后一条事件丢失，必须保留轻量定期校准。
- 心跳提交后的展示刷新只进入按节点合并的队列，生命周期受面板管理，不等待读取池，不拖住 Agent ACK。清单和元数据都成功后才消费失效标记，失败留待下一轮校准重试；清单另有 5 分钟核对，覆盖调优建议的时间窗口变化与丢失通知。
- 前端同一服务代次只接受已见最新统计版本，同版本的总览／详情可以复用；新版本立即淘汰其他范围的旧缓存。配置失效回执及服务代次变化也会清缓存，迟到的旧请求不得恢复旧结果。
- Owner 后续要求总览卡片固定高度：待处理问题、节点流量、用户用量与到期、近期阻断统一为 480px，加载状态保持相同高度；标题固定，超出的列表在卡片内滚动。保留原卡片内容、数据范围和统计口径。
  - 本次布局验证：前端类型检查与构建通过；本机 Chrome 在 1600px 和 390px 宽度下确认四张卡片均为 480px，扩充告警／节点排行内容后可内部滚动且标题位置不变；用户／阻断在该夹具中为空，验证了空态固定高度。未运行新增业务回归或生产检查，未部署。
- 核心 cursor 即使新增量为零，也会替换整个账户 epoch 的统计来源；读取索引必须保留 epoch，不仅跟踪样本增删。
- 已压缩的小时历史仍不能回答任意纳秒边界，保留明确精度错误。
- 展示索引溢出只标记本桶不可用；选中的桶会返回明确错误，被核心 cursor 排除的账户 epoch 不影响结果。禁止因派生总量溢出回滚本来有效的源样本或额度入账。
- 共享统计与用户额度账本分开；显示缓存不参与额度判断。
- 共享统计的今日总量、样本数和趋势包含已软删除节点的留存流量，节点排名和详情仅提供在册节点。节点详情的总量和趋势只计该节点。
- 新快照的代理流量截止到 `asOf`（不含），整机账期统计读取同一事务中截至当地当天已入库的日桶。代理查询失败，或已有同配置完整快照后整机查询失败，均保留上次结果、原截止时间及年龄，并显示本次错误。首次准备或配置改变后还没有可复用的完整快照时，整机查询单独失败返回 `data.machineError` 和空的 `machineTraffic`，本轮代理统计仍可读取。配置改变后立即停止提供旧结果。
- 兼容接口 `GET /dashboard` 按请求读取代理流量，沿用当前时刻加一秒的排他上界。旧 `/traffic/nodes/monthly` 保留当地下月初上界，旧整机接口保留完整账期。新页面使用共享快照。

## 本地性能测量（2026-11-19）

固定同一数据库和查询时间边界，先校验原查询与索引查询逐字节相同，再计时。数据为单节点 7 天、每 5 秒一条，共 120,960 条真实 SQLite 样本，包含小数秒边界。以下为 Windows 本机组件基准，不代表生产网页耗时或 P95。

| 测量项 | 结果 |
| --- | ---: |
| 原始查询 | 150.74 ms/op |
| 小时索引查询 | 2.71 ms/op |
| 存量 0154 库执行 0155 迁移 | 172.8 ms |
| 168 个小时键、每批 16 个的回填累计工作耗时 | 103.4 ms |
| 最大一批回填耗时（含事务，无并发写入竞争） | 10.81 ms |
| SQLite 逻辑页体积增量 | 3,829,760 字节 |

命令：`go test ./internal/panel/access -run '^$' -bench '^BenchmarkNodeTraffic(ExistingDatabasePreparation|HourIndex)$' -benchtime=1x -benchmem -count=1 -timeout 120s`。后台正常每批之间等待约 1 秒，因此这份数据回填至全就绪还需约 10 秒的批间等待；103.4 ms 不是生产就绪用时。未测 WAL／临时文件峰值、持续上报时的写锁竞争及真实生产页面耗时。

## 改动定位

| 文件 | 本次用途 |
| --- | --- |
| `migrations/0155_traffic_display_hours.sql` | 派生小时索引、待准备键、源表维护触发器；溢出明确标记，不阻断有效源数据入账 |
| `internal/panel/access/core_history.go` | 同一读事务的精确读取入口，完整小时与原始边界组合 |
| `internal/panel/access/traffic_hours.go` | 有写锁保护的分批准备、恢复后的索引重建 |
| `internal/panel/access/recovery.go` | 恢复权威数据后重建派生状态 |
| `internal/panel/access/traffic_hours_test.go`、`traffic_hours_bench_test.go` | 对账、来源切换、补传、溢出、恢复及性能测量 |
| `internal/platform/db/migration_state_test.go`、`read_snapshot.go` | 存量迁移验证；借用同一个只读事务供整机查询 |
| `internal/panel/node/runtime_snapshots.go`、`runtime_snapshots_test.go`、`runtime_snapshots_worker_test.go` | 安全运行状态快照、版本合并、异步队列不受读池阻塞及调优失效回归 |
| `internal/panel/node/control.go`、`handler_http.go`、`meta.go`、`tuning_recommendations.go` | 心跳／配置提交后排队刷新运行状态，轻量快照 API；忽略调优建议后通知刷新 |
| `internal/platform/events/events.go`、`roles_test.go` | Viewer 只接收白名单运行状态事件 |
| `internal/panel/statistics/service.go`、`snapshot.go`、`service_test.go` | 后台生产、配置失效、单一截止时间、错误和生命周期 |
| `internal/panel/node/machine_traffic.go`、`machine_period_test.go` | 整机统计在借用的同一读事务内执行，保留旧接口账期 |
| `internal/panel/dashboard/service.go`、`repository.go`、`handler_http.go`、`service_test.go` | 基础内容／共享统计分开，保留旧 API；夏令时边界回归 |
| `internal/platform/server/server.go`、`dashboard_test.go`、`display_reads_test.go` | 装配、工作线程生命周期和 HTTP／读池／恢复验证 |
| `internal/platform/server/testdata/portal_route_coverage.txt`、`migrations/checksums_test.go` | 发布检查补齐四个新接口的权限覆盖登记及 0155 校验和，保留原拒绝断言与历史校验值 |
| `web/src/app/statisticsCache.ts`、`useStatistics.ts` | 跨页面单次请求、统计年龄、配置失效、在途旧请求隔离 |
| `web/src/app/events.ts`、`nodeStatusEvents.ts` | 运行快照事件分发与连接状态 |
| `web/src/modules/dashboard/api.ts`、`pages/DashboardPage.vue` | 基础区先显示，统计单独更新，保留所有卡片 |
| `web/src/modules/nodes/runtimeSnapshots.ts`、`metrics.ts`、`components/serverListModel.ts` | 运行状态合并及旧值显示；确认离线后更新 |
| `web/src/modules/nodes/pages/NodeListPage.vue`、`NodeDetailPage.vue` | 清单共享统计、详情只读所需节点；快照校准与恢复 |
| `web/src/modules/nodes/components/detail/ServerOverview.vue`、`ServerSettings.vue`、`ServerSystemPanels.vue` | 消费节点统计及设置成功后失效通知 |
| `web/scripts/prepared-statistics-selftest.mjs`、`server-runtime-selftest.mjs` | 新行为回归，已由 `scripts/check.sh` 的通配循环自动收录 |
| `web/scripts/server-list-refresh-selftest.mjs`、`server-perf-selftest.mjs`、`server-order-selftest.mjs` | 调整已有刷新验证与校准依赖，保留失败恢复、卡片和排序断言 |
| `docs/architecture.md`、`docs/README.md`、本文件 | 工作区架构、导航与验证记录 |

`ServerCardGrid.vue` 与 `docs/v1.7.0-performance-acceptance.md` 含任务开始前已有的卡片显示改动，保留原改动，不据此声称本次已部署。

## 验证范围与结果

下表记录实现阶段的工作区验证，Go 1.26.11 / Windows amd64；当时未运行无关包测试或完整发布门禁。后续发布阶段在 `aacf4db` 上完成完整 Linux 门禁（876 秒，真实核心 306 秒）及 GitHub check 44525017037，均通过，详见[发布记录](../../release-notes-1.7.2.md)。

| 检查 | 结果与覆盖 |
| --- | --- |
| access / db 窄回归 | 通过。`TestTrafficHours*`、`TestReadNodeTraffic*`、`TestNodeTraffic*`、`TestMonthly*`、累计／重复上报、保留整理、旧迁移链与存量 0154 → 0155。原查询与索引在同一数据库快照、相同边界下逐字节一致 |
| statistics / dashboard / node 窄回归 | 通过。`TestShared*`、`TestDashboard*`、旧总览／整机接口边界、同配置错误保留完整旧结果、恢复、跨日／时区、Santiago 跳过午夜、配置变更和并发请求 |
| server / backup 窄回归 | 通过。读取不排队等待写连接、HTTP 统计与基础接口、恢复后只读池重新打开及关闭、匹配库／密钥／规则恢复、整机持久账本归档 |
| runtime / SSE 窄回归 | 通过。`TestRuntime*` 与 `TestSSERoleFilteringAndReauthentication`；占满实际读取连接时提交通知仍返回、工作线程结束、调优失效、校准及 Viewer 白名单 |
| 前端行为自测 | 通过。`prepared-statistics`、`server-runtime`、`server-list-refresh`、`server-perf`、`dashboard-live`、`server-detail`、`navigation-performance`、`monitoring-refresh` |
| 类型检查与前端构建 | `npm --prefix web run build` 通过，包含 `vue-tsc --noEmit` 和 Vite，380 个模块 |
| Go 静态检查及构建 | 相关七个包的 `go vet`、改动 Go 文件 `gofmt -l`、`go build ./cmd/panel` 通过 |
| 本机 Chrome 冒烟 | 独立临时数据库与开发面板，仅监听 127.0.0.1。主动延迟统计请求时，总览基础内容和待处理／节点流量／用户用量／近期阻断四块仍显示；服务器卡片、新建服务器弹窗、详情按节点请求统计均通过，0 浏览器 pageerror。本地离线节点是合成夹具，没有启动或连接 Agent |
| 独立审查 | 两名审查者的末轮问题已修复并只读复核；含派生溢出不得阻断源入账、已删除节点历史、旧 API、ACK 解耦、清单重试、调优及跨范围缓存失效 |
| 差异检查 | `git diff --check` 通过；实现阶段未提交、推送、发布、部署或访问生产，后续提交发布见文首状态 |

测试过程保留真实失败：新增索引入口、展示溢出错误归属、整机失败保旧、夏令时边界、清单重试、调优失效、跨范围 `pending` 和 `ready→ready` 缓存问题先被回归暴露后修复。集成类型检查暴露联合类型未正确收窄，改为显式判断后通过。本机浏览器最初的断言使用了组件没有的 `dialog` 角色，并误把待登记节点当作可查询流量的节点；根据组件行为改用弹窗标题和独立的已登记离线夹具后通过，没有更改产品行为迁就夹具。

**race 已在 Linux 发布门禁通过**：实现阶段曾尝试用现有 Zig 0.14.1 执行 statistics/node 的 `go test -race`，首次链接缺少 Windows 同步 API；显式链接现有同步库后进入执行阶段，但 ThreadSanitizer 在分配影子内存时返回 Windows 错误 87，测试主体没有运行。该 Windows 尝试不作为通过证据；后续 `aacf4db` 的完整 Linux 门禁已执行并通过全仓库 race。

**后续生产验证**：Owner 另行授权部署后，已建立并校验加密备份，在生产数据副本上执行迁移、分批回填和固定窗口对账，随后部署并完成逐页复测，见[生产验收](../../v1.7.2-production-acceptance.md)。副本没有并发 Agent 写入压力；真实上报压力下的迁移写锁等待和 WAL／临时空间峰值仍未验收。页面仅三次样本，不能声称所有 P95 目标通过；Owner 要求跳过手动后台恢复实测。

## 参考来源

- [CF Agent 差量累计](https://github.com/huilang-me/cfsm-agent/blob/b7a39a43749e4c961df8e6cae3b8d143cecfbf3a/internal/cfprobe/traffic.go#L65)
- [CF 最新指标缓存](https://github.com/huilang-me/CF-Server-Monitor/blob/1be24f0eabdcc2d06c215c45258382b02e438b49/src/utils/cache.js#L16)
- [哪吒实时快照推送](https://github.com/nezhahq/nezha/blob/8bcaf092baf8c78c9d8d65f7d2048da44513bf9e/cmd/dashboard/controller/ws.go#L149)
- [哪吒周期结果读取](https://github.com/nezhahq/nezha/blob/8bcaf092baf8c78c9d8d65f7d2048da44513bf9e/cmd/dashboard/controller/service.go#L26)
