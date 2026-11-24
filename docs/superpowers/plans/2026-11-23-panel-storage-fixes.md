# 面板数据库存储修复计划

以 Owner 本次需求为规格，直接在指定工作区实施；不启用子代理，不暂存或提交，不接触 state/。已有改动保持原样。

1. 分钟边界：先补混合精度、跨分钟/小时/月、24 小时 cutoff、500 键追平回归，再修正分钟选取、汇总、删除与关联查询/保留边界。整秒边界使用无后缀秒前缀，精确小数边界使用规范化比较，保持索引粗筛。
2. 旧回执：核对完成标记、去重、恢复与 reset 索引；补 7 天窗口、pending、未完成、编号隔离、500 行批次测试；增加独立短事务与累计计数。编号规则不变，不增迁移。
3. 空闲页：补新库模式、连续有界回收、整理失败仍回收测试；新建空库在建表前启用 incremental，已有库不转换；独立调度每轮最多 2048 页、每段 256 页、2 秒预算，剩余超过 32 页时 1 秒续跑，整理失败仍按 1 分钟重试。
4. 同步产品、架构、运维和存储观察文案；只跑相关窄 Go 测试、改动包 go vet、gofmt 与差异检查。自行审查，不运行全量测试或 Linux 门禁。

Owner 追加授权：修复 `PrepareNodeTrafficHours` 把分钟行当单样本的问题。新增回填守恒和 samples 溢出回归，已先复现 10 个样本被记为 4 个、溢出未被展示；改为累加原始行的 1 和分钟行的 samples，沿用显式溢出标记。

Owner 再次明确授权：新增 0164，一次性失效含分钟汇总的已就绪小时索引，纠正已有错误样本数；不改原始数据或账本，重算仍由每轮 16 桶的后台流程执行。升级回归先复现旧索引未失效，再验证仅分钟相关索引失效、原始小时保持就绪、分批重算和总量守恒。

预期：回归先因原始 bug 失败，修复后通过；总量守恒、边界桶正确；回执删除只影响授权条件；回收不受整理失败阻塞，保留原始错误。

## 执行与验证结果

以上任务均已实施。按 Owner 要求自行审查，未启用子代理。未暂存、提交、推送；未运行全量测试、scripts/check.sh、Linux 门禁或前端构建；未接触 state/、生产环境和既有升级记录。

使用仓库 `.tools/go/bin`、`GOTOOLCHAIN=local`，专用 Go 缓存位于被忽略的 `.tools/panel-storage-go-cache`。系统默认缓存不可写，切换专用缓存后检查正常。首轮两个测试夹具问题（core 截到整秒后的重复键、digest 长度）已修正，未放宽断言。之后先复现分钟 samples CHECK 失败、cutoff 提前删除、旧回执未淘汰、小时样本 10→4、溢出被隐藏和旧索引未失效，再验证修复通过。

直接相关检查（均退出 0；已经通过的检查仅在关联代码/迁移改变后或补充未覆盖行为时再运行）：

```text
go test -p 1 ./internal/panel/access -run 'Test(ReadNodeTrafficMixedPrecisionBoundaries|ReadNodeTrafficHourlyPrecisionAndUnknownNode|TrafficHoursBackfillAndCompaction|TrafficMinuteMixedPrecisionBoundaries|TrafficMinuteBacklogBounded|TrafficMinuteHourCutoffAndResolution|LegacyReceiptRetirementWindowAndBatch|AccountMinuteRetentionExactCutoff|TrafficMinutesMergeLateSamplesAndPreserveFallback|ReadNodeTrafficRejectsPartialMinuteBoundary|RetentionPreservesDailyAndReceiptsAndOverflowRollsBack|ConfiguredRetentionControlsLateAccessAndNativeCounters|HourlyRetentionBoundaryCannotRecreateExpiredBucket|InvalidRetentionPreventsCleanupAndPartialIngestion)$' -count=1
go test -p 1 ./internal/panel/access -run '^TestTrafficHours(PreserveMinuteSampleCounts|MinuteSampleCountOverflowIsVisible|ExactChangesAndSourceSwitch|BackfillAndCompaction|OverflowPreservesLedgerAndReportsReadError|ExcludedAccountOverflowDoesNotBlockCore|MatchOriginalQueryAndRestoreReset)$' -count=1
go test -p 1 ./internal/panel/access -run '^TestLegacyReceiptRetirementWindowAndBatch$' -count=1
go test -p 1 ./internal/panel/access -run '^TestTrafficHoursMigrationRebuildsPreparedMinuteCounts$' -count=1
go test -p 1 ./internal/platform/db -run 'TestOpenNewDatabaseIncrementalAndPreservesExistingModes|TestReclaimFreePagesBoundedContinuation|TestOpenMigratesAndPersists' -count=1
go test -p 1 ./internal/platform/db -run '^TestReclaimFreePagesIncludesConnectionWaitInBudget$' -count=1
go test -p 1 ./internal/platform/db -run '^TestOpenNewDatabaseIncrementalAndPreservesExistingModes$' -count=1
go test -p 1 ./internal/platform/server -run '^TestSpaceMaintenanceContinuesAfterCleanupFailure$' -count=1
go test -p 1 ./internal/platform/server -run '^TestHTTPAndGRPCStartAndStopTogether$' -count=1
go test -p 1 ./migrations -run '^TestAppliedMigrationsAreImmutable$' -count=1
go vet ./internal/panel/access ./internal/platform/db ./internal/platform/server
go vet ./internal/panel/access ./migrations
```

最后补充存储观察保留层级后，`go vet ./internal/panel/access ./internal/platform/server` 亦通过。全部改动 Go 文件的 `gofmt -l` 与 `git diff --check` 均无输出。旧迁移校验值全部保留，只为新增的 0164 增加校验条目，没有修改已有断言的期望值。

边界与保留结论：

- 分钟选桶、前后计数、求和与删除统一无后缀整秒前缀；该前缀严格早于同秒任意小数/Z 后缀，半开区间兼容所有模块写入的 UTC 秒/纳秒格式。24 小时 cutoff 分钟不会被选入。
- 同类修复覆盖核心分钟转小时 cutoff、核心/账户分钟精度拒绝查询、账户分钟两条保留删除路径。traffic_hours.go 无同类时间比较，但按 Owner 追加授权修复样本累加并新增 0164。
- CompletePendingTraffic 在两个仓库正常完成后才写 accounting_completed_at 并移除 pending；prepareTrafficDelivery 可绑定既有旧回执，绑定为正序号后不再进入旧格式删除。恢复校验只要求正序号 R…C 完整覆盖；reset_id 是代次隔离字段，其部分索引无额外保留合同。删除不动累计游标、队列检查点或 pending。
- 新空库原本没有建表前的 auto_vacuum 设置；现在默认 incremental。已有 NONE/FULL 库保持原模式，转换和全库 VACUUM 留给 Owner 安排。

未发现仍阻挡本次交付的问题。生产大库上的实际追平耗时、WAL/文件体积变化和 Linux 门禁未验证，交由 Owner 后续测试机/发布流程核实。
