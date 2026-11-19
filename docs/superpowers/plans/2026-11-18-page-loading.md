# 页面加载修复与数据库诊断计划

范围：前三批与第四批代码已在当前目录实现并通过相关本地检查；第四批生产部署与上线验收待单独授权。生产基线 v1.6.1、迁移 0153。保留工作区现有流量存储与 Agent 升级改动，不部署、不提交、不改生产配置。

## 执行记录

- 第一批：用户选择接口、请求内共享在线状态计算、节点编辑器并行加载及服务器范围查询。
- 第二批：服务器选择接口、元数据与今日流量批量接口、可见服务器入站状态批量读取、详情按需加载和刷新合并。
- 第三批：总览、分组、策略编辑器、中转画布、门户预览分区加载；核心版本按展开加载；任务记录后端分页；客户端策略卡片消除逐条详情请求。
- 用户允许直接在当前目录修改，允许相关测试及类型检查。前三批使用已有测试覆盖；第四批用户另行明确允许新增连接等待、只读隔离与恢复重连的回归测试。所有任务不自动提交。
- 接口依赖：第一批选择接口供第三批使用；第二批元数据供列表、总览、中转画布使用。保留能力位、核心选择限制和权限检查，轻量接口不能放宽保存与发布校验。
- 验证口径：类型检查与相关 Go 包现有测试；生产性能改善须部署后用同样数据复测，不把本地检查通过记为生产提速。

## 前三批实现清单

| 批次 | 文件与行为 |
| --- | --- |
| 1 | `internal/panel/user/choices.go`、`handler_http.go`：新增 Owner 用户选择与额度摘要接口；实际用户列表继续计算在线状态。`device_observer.go` 在一次请求内共享每台服务器的配置解码与凭据映射，`observations.go` 共享分组读取；部署仓库的 `Published` 不再读取用不到的入站意图。 |
| 1 | `web/src/modules/users/api.ts` 及访问记录、分组、入站、策略编辑器、门户预览调用方使用对应的轻量 DTO。`NodeEditorDrawer.vue` 并行加载协议、证书、用户和服务器选择；按选定服务器读取入站、出站、出口组和链，保存前仍重新核对端口。必要数据未齐时禁止保存，初始表单可见。 |
| 2 | `internal/panel/node/choices.go`、`repository.go`：服务器选择保留能力与核心选择限制，跳过调优建议计算。`meta.go` 不再为校验存在性读取完整服务器；`meta_batch.go` 批量读取元数据，区分在用/已删除服务器。`access/node_summary.go` 复用当前流量读取与历史精度约束，批量返回各服务器区间汇总。 |
| 2 | `deployment/inbound_state.go`、`handler_http.go` 与前端 `deploymentApi.ts`：按可见服务器批量读取入站状态，每个服务器独立报告错误。`NodeListPage.vue`、`nodeMeta.ts` 用批量元数据与流量；`NodeDetailPage.vue` 删除未被展示组件使用的全量分组查询，按总览/设置加载补充数据，切换标签时补发必要请求。 |
| 2 | `ProxyNodeListPage.vue` 合并在途状态请求；在途期间收到变化事件只记录一次补读，保留节流、过期结果与卸载检查。选择不同服务器集合时丢弃旧集合结果。 |
| 3 | `DashboardPage.vue` 使用用户额度摘要和服务器选择数据，按区域就绪显示；`ServerValueCard.vue` 在已有每日财务缓存基础上批量读取元数据。`LineGroupPage.vue` 先显示卡片，辅助数据未齐时禁用操作；`RoutingRuleEditorPage.vue` 先显示策略内容、后填选择项，加载中不能保存。 |
| 3 | `ServerSettings.vue` 只在展开核心更新时挂载版本查询组件。`RelayCanvasPage.vue` 元数据独立加载，入口分组由选中入口时的既有加载流程读取。`PortalPreviewPage.vue` 在 URL 已指定用户时并行读取选择项与快照。 |
| 3 | `deployment/task_history.go`、`TaskRecordPage.vue`：Owner 任务记录在同一只读事务中合并 Agent/核心任务、筛选、计数、分页，保留恢复任务所需的当前代次和命令 ID；不再拉全量服务器、全部任务和发布目录。筛选变化重置页码，清除后页数缩减会回到最后有效页。 |
| 3 | `subscription/rules/profiles.go`、路由模块 `api.ts`、`ClientPolicyPage.vue`：列表一次返回模式、端口、组数、启用规则数；不再逐条读取完整配置。初始只读当前生效配置，编辑时读取选中详情，并通过请求代次防止晚到结果覆盖另一个草稿。 |

### 新增读取接口

| 接口 | 权限 | 内容 |
| --- | --- | --- |
| `GET /api/v1/users/choices` | Owner | 分页 ID、用户名、显示名、状态，支持 keyword。 |
| `GET /api/v1/users/summaries` | Owner | 分页基础身份、到期时间、额度、用量及授权分组；不计算在线设备/连接。 |
| `GET /api/v1/nodes/choices` | Owner、Viewer | 分页服务器信息与能力，保留核心选择门禁，不计算调优建议。 |
| `GET /api/v1/nodes/meta?deleted=1` | Owner、Viewer | 元数据批量读取；不传 deleted 时只读未删除服务器。 |
| `GET /api/v1/traffic/nodes/summary?from=…&to=…` | Owner、Viewer | 各节点上传、下载、样本数，可选 nodeId；不接受用户或域名维度。 |
| `GET /api/v1/nodes/inbound-states?nodeIds=…` | Owner、Viewer | 最多 200 个唯一服务器 ID；逐项返回入站状态或错误。 |
| `GET /api/v1/task-records` | Owner | page/pageSize/type/status/keyword；跨两类任务的全局分页与总数。 |

### 验证记录与限制

- Go 现有测试：`internal/panel/user`、`node`、`deployment`、`subscription/rules`、`access`。
- 服务端集成：用户/分组/订阅权限、节点 HTTP 权限、代理端口与权限、总览 Viewer 隔离、访问记录分页与 Viewer 汇总，共 5 个既有 HTTP 用例。
- 前端：类型检查与 Vite 生产构建通过；节点编辑器、客户端策略、中转状态的既有自测通过。前两项首次失败来自旧夹具：保存流程未设置加载完成，新摘要接口尚未接入测试依赖。仅同步 `web/scripts/node-editor-selftest.mjs`、`client-policy-selftest.mjs` 的既有夹具和对应 API 名，保留保存/发布顺序、草稿保留、分页完整性、错误与权限断言；复验通过。未新增测试方法、实体或 mock。
- 独立审查发现并修正：策略详情晚返回覆盖、详情切标签漏加载、分组操作过早启用、在途刷新丢最后一次事件。
- 未运行：生产部署与页面耗时复测、完整发布门禁、真实代理核心端到端。未新增数据库迁移，未更改连接池、索引、生产配置；现有 0154 流量存储工作保持独立。
- 不能把 HTTP 请求减少等同于相同倍数的服务器耗时下降。批量入站状态仍需逐服务器构造快照；剩余 SQL、连接等待和配置体积开销归第四批测量。

## 第四批：数据库等待与查询开销的详细计划（代码已实现，生产验收待做）

### 1. 建立可比较的基线

核实实际面板构建版本、迁移头、数据库体积、节点及用户数量。记录浏览器位置、网络、缓存状态和 SSE 是否连接。逐项采集总览、用户列表、服务器列表及三种服务器状态的详情标签、节点新建与编辑、分组、策略编辑、任务记录、门户预览。

每项预热一次，顺序采样至少 20 次；另采冷导航 5 次。记录点击到首个可用内容、全部必要内容完成、请求数、失败数、p50/p95。外部版本查询分别测缓存命中与过期。禁止以压测方式冲击生产。

### 2. 分解服务端耗时

在测试环境增加可关闭的只读诊断：请求总耗时、获取数据库连接的等待、SQL 执行、已发布配置读取/解密/解析、外部 HTTP、响应编码。通过请求 ID 串联，不记录 SQL 参数、凭据、订阅 Token、完整配置或地址密钥。

连接等待应直接计量显式连接获取；DB.Stats 的 WaitCount/WaitDuration 仅作进程时间窗趋势，不能归因到单个请求。复用连接时把开始事务、执行和提交分别计量，避免重复累计。生产采样的开关、保留时间和输出位置另行确认，不擅自启用。

### 3. 查询审查与实验

在获准的脱敏副本上，使用同一版本迁移和接近生产的数据规模。对前两步确认的慢 SQL 执行 EXPLAIN QUERY PLAN，核对扫描行数、排序临时表、过滤及关联列、现有索引。重点看用户授权/凭据、节点状态、流量区间汇总、任务 UNION 分页。

优先消除重复读取和不必要的完整配置解码；只有扫描证据支持时设计组合索引。记录索引前后耗时、空间、写入成本和迁移回退办法。当前工作区的 0154 属于流量存储工作，本计划不得占用或修改它；新增迁移号待实施时重新确认。

### 4. 只读连接池的候选迁移

沿用 internal/platform/db/readpool.go 的已有池，不直接增加主库连接数。按查询列出：是否写入、是否要求读后写一致、是否在事务中、是否依赖临时表或连接级设置、是否可能触发惰性修复。

候选：纯显示的节点选择、元数据、用户选择、历史任务和流量统计。排除：认证与授权变更、额度扣减、凭据生成、发布快照、配置修订、备份恢复、在线数据库迁移、任何隐含写入。需要一致快照的多查询使用同一个只读事务，不在多个连接上拼接不同时间的数据。

先迁一个纯读取模块做对照，再决定后续迁移。数据库连接参数与只读池生命周期必须兼容恢复期间的关闭/重开；检查 WAL 增长、长读事务、busy、连接数与内存。观察到写延迟或 WAL 恶化则回退该模块的接线。

### 5. 验收与上线顺序

现有相关测试与窄范围新增回归测试需按用户授权执行，覆盖取消请求、读失败显式报错、数据一致性、恢复后连接有效、Viewer 权限及并发写入。先测试环境复测基线，再按发布流程生成可审查版本；生产部署单独授权。

普通页面主要内容目标 2 秒内、复杂页面 3 秒内；这是优化目标，须同时报告 p50/p95、异常及网络影响。验收还要求没有新增请求风暴、没有因缓存显示旧权限、没有把失败显示成空数据、写入与发布没有回退。上线保留旧二进制和匹配备份，按实际数据库迁移决定回退是否需要恢复数据库。

### 6. 最终交付证据

逐页前后对照表、瀑布图、服务端阶段耗时、慢 SQL 的执行计划、连接池接线清单、测试输出与生产复测限制。若连接等待占比很低，停止扩大读池改造，按测量转向 SQL、配置解码或外部请求。


## 第四批执行结果（2026-11-18，本地；生产采样为 2026-11-19 UTC）

### 结果与边界

已实现显示读取与写连接隔离、短只读快照、可关闭的分阶段计时，并修正任务记录 SQL 同名字段以及恢复准备/恢复启动取消时的读池释放。未提交、发布或部署，未修改生产配置，未新增索引或迁移。生产读取慢的各部分占比仍待部署诊断版本后采样。

执行裁定：用户要求保留当前目录改动，因此不新建 worktree、不暂存或提交。沿用现有四连接只读池、单写连接和零空闲只读连接；不增加主库连接数。当前工作区 0154 属于既有流量存储改动，本批只在 ReadNodeTraffic 中替换读取事务入口与释放方式，保留其 SQL 和写路径。原计划中的生产数据副本与全面上线前后页面对照，留到另行授权部署与采样时执行；本次没有生产数据库副本，不能用小型测试库代表生产 679 MiB 数据。

### 修改文件

以下路径均相对于仓库根目录。同一行列出的测试文件只有注册函数参数随接线同步，未改变原断言。

| 文件 | 本批修改 |
| --- | --- |
| internal/platform/server/server.go | 为纯显示路由提供只读仓库；总览及用户在线观察依赖也接只读池；读池打开失败直接报错；接入默认关闭的计时开关。 |
| internal/panel/node/handler_http.go | 列表、选择、详情、元数据、整机流量和指标使用读 Handler，修改操作仍用原 Handler。 |
| internal/panel/user/handler_http.go | 用户列表、详情、选择和额度摘要使用读 Handler。 |
| internal/panel/access/handler_http.go | 访问记录、域名汇总及节点/账户流量显示使用读 Handler。 |
| internal/panel/deployment/handler_http.go | 任务历史及配置版本列表使用读 Handler；发布、快照、差异及入站状态构建沿用原路径。 |
| internal/platform/db/read_snapshot.go | 显式获取连接、开启短读事务，记录获取/开始/SQL/结束耗时，取消或失败后释放连接。 |
| internal/panel/node/repository.go | 列表计数、当前页与核心选择限制在同一个读快照中完成；调优建议在释放该快照后读取。 |
| internal/panel/node/core_selection.go | 复用同一个读取结果处理函数，让列表可在当前事务内读取核心选择限制。 |
| internal/panel/node/meta_batch.go | 元数据批量读取接入短读事务及 SQL 计时。 |
| internal/panel/user/choices.go | 选择、额度摘要的计数/当前页/授权分组共用同一连接与读快照。 |
| internal/panel/deployment/task_history.go | 历史任务接入显式取连接计时；修正 nodes 与 tasks 都有 status 导致的 SQL 字段歧义。 |
| internal/panel/deployment/repository.go | 配置内容读取使用短快照，连接释放后解密和解析，并分别计时。 |
| internal/panel/access/core_history.go | ReadNodeTraffic 沿用原一致性快照和查询，只替换为可计时的事务封装；压缩/写入不变。 |
| internal/platform/readtiming/timing.go | 请求上下文聚合耗时，输出请求序号、路由模板、状态码、总时长及阶段时长；排除 SSE。 |
| internal/platform/httpapi/response.go | 记录响应编码与写出合计耗时。 |
| internal/panel/deployment/core_releases.go | 记录核心版本外部 HTTP 请求至响应体读取完成的耗时。 |
| internal/platform/server/restore.go | 关闭未消费的恢复准备读池；恢复启动取消或早退也关闭已移交的读池。 |
| internal/platform/server/display_reads_test.go | 13 台节点/4 个用户下的 10 个接口隔离；恢复重开/关闭/取消；诊断阶段、敏感参数与真实 SQL 失败检查。 |
| internal/platform/db/read_snapshot_test.go | 并发写入时读快照稳定、等待可取消、只读拒绝写入、释放后读到新数据与零空闲连接。 |
| internal/platform/readtiming/timing_test.go | 路由模板/状态/阶段输出，不记录路径实值、查询参数或认证头。 |
| internal/panel/deployment/task_history_test.go | 真实 Agent 与核心任务合并分页、中文筛选、恢复标识、取消请求、选择项核心限制及查询计划。 |
| internal/panel/node/certificate_matches_test.go、machine_http_test.go、reissue_enrollment_test.go | 注册调用显式传入测试读 Handler。 |
| internal/panel/deployment/baseline_test.go、publish_node_test.go | 注册调用显式传入测试读 Handler。 |
| docs/superpowers/plans/2026-11-18-page-loading.md、docs/frontend-performance-audit.md | 更新实施状态、证据、计时使用说明及未完成的生产验收范围。 |

读池候选审查：上述显示查询没有隐式写入、临时表或连接级设置要求，不参与读后写决策。权限仍由原认证中间件执行。用户完整列表/详情与总览保持原先的分段读取语义，在线观察还结合内存状态，不能视为一次全局数据库快照。凭据变更、额度扣除、发布快照、数据库恢复写入、任务恢复操作保持原写仓库。

### 生产修改前基线

在现有已登录浏览器会话内用同源 GET 请求顺序采样，三个接口先各访问一次，再轮流读取各 20 次，共 60 次，全部 200；不并发压测、不修改任何对象。包含网络与完整响应体读取，不包含页面渲染，也不是冷导航数据。浏览器所在机器的实际出口位置和线路未独立核实；原页面仍开着，页面自身刷新/SSE带来的负载未隔离。接口返回 no-store；本次没有重新控制浏览器缓存或 SSE。

实际版本 v1.6.1，迁移头 0153_drop_one_shot_markers.sql，WAL，数据库 711,491,584 字节，13 台服务器、4 个用户，Go 1.26.11、SQLite 3.53.4。采样开始于 2026-11-19 06:54 UTC。分位数按排序后的最近秩计算。

| 接口 | 样本 | p50 | p95 | 最小—最大 |
| --- | ---: | ---: | ---: | ---: |
| /api/v1/dashboard | 20 | 6.870 s | 11.834 s | 6.403—13.154 s |
| /api/v1/users?page=1&pageSize=20 | 20 | 6.818 s | 12.646 s | 3.833—14.043 s |
| /api/v1/nodes?page=1&pageSize=20 | 20 | 2.084 s | 7.419 s | 1.278—7.622 s |

这些数字不证明每一秒都在数据库排队；生产尚未运行新增计时。原始数值保存在本地 .git/performance-phase4/production-api-baseline.json，不含响应正文或登录材料。

### 本地因果验证与 SQL 审查

改前占住唯一主库连接，10 个显示接口全部等待到 1 秒请求超时。改接只读池后，使用实际迁移、13 个节点与 4 个用户的测试库，同样占住主库连接，10 个接口全部返回 200，单次约 3.8–42.9 ms，返回条目数正确。该测试库只有少量业务数据，不是生产数据库副本。

另外用同一选择查询做控制实验：主库连接被占用 100 ms，只读池请求 1.61 ms，其中取连接 0.009 ms、SQL 1.60 ms；随后主库请求等待剩余时间，总计 98.07 ms，主要落在 db_acquire。该实验验证计时能区分连接获取与查询工作，不能换算为生产提升比例。

本地 EXPLAIN QUERY PLAN 结果：

- 用户选择当前页为扫描加临时排序；4 个用户下没有新增索引的证据。
- 用户授权分组使用 (user_id, group_id) 主键覆盖索引；元数据连接使用节点及元数据主键；运行修订及配置内容使用主键查找。
- 节点流量视图使用 core_traffic_time / account_traffic_time 的时间范围索引，游标存在性检查使用复合主键；小时数据使用已有 core_traffic_hourly_time，分组使用临时 B-tree。
- 任务 UNION 两个任务表仍需扫描与临时排序，发布目录关联使用主键。没有生产任务数量、该 SQL 阶段耗时及真实执行规模，暂不增加索引；后续若 SQL 占比高，再对生产授权副本测量。

本地库含工作区已有的 0154，生产是 0153；小时流量表的计划只代表本地工作区。没有执行生产 EXPLAIN、WAL 长期增长/写延迟/内存对照，也没有据此调整数据库参数。

### 诊断开关与读数

启动进程环境变量 QUI_READ_TIMING=true（或 1）才启用；未设置或 false 为关闭，无效值直接启动报错。仅本地代码提供能力，本轮生产未启用。日志写到现有应用 logger 的输出，沿用部署环境日志保留规则；如需生产采样，需另行确定开启窗口、日志位置与保留时间，结束后关闭并重启进程。

每个有限时长的 GET /api/v1 请求结束输出一条 read timing：request_id 为本进程请求序号，route 是模板（如 GET /api/v1/nodes/{id}），status、total_ms 与 stages。没有记录 SQL 文本/参数、URL 查询、Cookie、认证头、配置内容或密钥。

| 阶段 | 读数含义 |
| --- | --- |
| db_acquire | 显式 DB.Conn 的全过程，包含排队及必要的新连接打开；不能把全部时间叫作纯排队。 |
| db_begin / db_end | 读事务开始、回滚结束及归还连接。 |
| sql | 已接入 helper 的 Query、Next、Scan 和 Close 时间之和，不包括循环内业务处理；calls 是这些操作次数，非 SQL 语句数。 |
| config_decrypt / config_decode | 配置解密、解码；已先释放读取连接。 |
| external_http | 核心版本查询的外部 HTTP 与响应体读取；缓存命中时不会出现此项。 |
| response_encode_write | JSON 编码与写入 ResponseWriter 的合计，包含写阻塞；不是浏览器完整下载/渲染时间。 |

SQL/连接计时覆盖：节点列表/选择及核心限制、批量元数据、用户选择/摘要、任务历史、配置内容读取和节点流量 ReadNodeTraffic。完整用户列表的基础查询、版本列表、总览其他 SQL 等尚未逐条接入 helper，缺少阶段表示未覆盖或未执行，不能解释为耗时为零。各阶段不是完整请求总量，不能直接相减后把余数都归为 CPU。DB.Stats 不用于把全进程等待归因到某个请求。诊断关闭时不创建聚合器、不写计时日志。

### 检查、审查与后续验收

已通过 7 个包的窄范围 Go 检查：db、readtiming、node、user、deployment、access、server。覆盖新回归、既有读池关闭、节点整机流量、核心选择/版本读取、已发布配置读取、在线观察、月流量、用户/节点/代理/总览/访问记录 HTTP 权限。测试选择正则与输出保存在 .git/performance-phase4/final-check.log，执行命令见下方。git diff --check 通过。随后针对补充的核心选择限制断言，以及节点调优建议/中转清理字段/标签读取，运行了 deployment 与 node 的 4 个窄范围用例并通过（list-regressions.log）。

新增连接隔离用例先失败后通过；恢复准备关闭与恢复启动取消的泄漏分别先失败后通过。独立审查确认显示路由权限和依赖，并发现恢复启动早退清理遗漏，修复后已复核。其余新回归验证分页筛选、恢复标识、真实 SQL 错误不变为空列表、日志不泄漏请求数据。

未运行：本批前端类型检查/构建（未改前端，前三批已运行）、完整发布门禁、真实代理核心端到端、生产优化后复测。未作生产提速达标声明。

窄范围检查命令（go 1.26.11）：

~~~powershell
go test ./internal/platform/db ./internal/platform/readtiming ./internal/panel/node ./internal/panel/user ./internal/panel/deployment ./internal/panel/access ./internal/platform/server -run '^(TestReadPool|TestReadSnapshot|TestMiddlewareUsesPattern|TestDisplayReads|TestPreparedRestoreReopens|TestDisplayReadTiming|TestUsersGroupsSubscriptionHTTPPermissions|TestNodeHTTPAuthorizationAndExpiredEnrollment|TestDashboardHTTPAuthenticationAndViewerDomainIsolation|TestAccessHTTPPaginationPrivacyAndViewerSummary|TestProxyHTTPPermissionPortAndRelayMasking|TestEncryptedSnapshot|TestRedeployPublished|TestCoreRelease|TestTaskHistory|TestDeviceObserver|TestHandlerObserve|TestMonthly|TestNodeTraffic|TestMachine|TestCoreSelection|TestMeta)' -count=1 -timeout 180s
go test ./internal/panel/deployment ./internal/panel/node -run '^(TestTaskHistoryPaginationFiltersAndRecovery|TestAdviceCountInNodeList|TestNodeRelayCleanupFieldsInListAndDetail|TestNodeTagsNormalizeSaveAndList)

剩余上线验收：用户决定发布/部署后，按正式发布流程验证同一版本；先复测上述三个接口，再补原计划中各页面/详情标签/节点弹窗的冷、热导航和 p50/p95，短期开启诊断确认慢在连接、SQL、配置还是外部请求，同时观察 WAL、内存和写入/发布延迟。普通页面 2 秒、复杂页面 3 秒仍为目标，当前没有上线后的达标数据。若取连接耗时很低，停止扩大读池接线，根据具体阶段继续定位。
 -count=1 -timeout 60s
~~~

剩余上线验收：用户决定发布/部署后，按正式发布流程验证同一版本；先复测上述三个接口，再补原计划中各页面/详情标签/节点弹窗的冷、热导航和 p50/p95，短期开启诊断确认慢在连接、SQL、配置还是外部请求，同时观察 WAL、内存和写入/发布延迟。普通页面 2 秒、复杂页面 3 秒仍为目标，当前没有上线后的达标数据。若取连接耗时很低，停止扩大读池接线，根据具体阶段继续定位。
