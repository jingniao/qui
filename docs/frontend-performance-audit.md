# 前端导航与页面性能审计（v1.5.5，2026-11-14）

本文合并两个并行工作流：A 负责「服务器」相关页面（nodes 模块、版本与更新），见文末「工作流 A：服务器页面」；B 负责全局导航与其余模块（下文默认指 B）。日期：2026-11-14。基于当前未提交的 v1.5.5 工作区。只修改 B 范围前端、两个自测和本记录；未修改 Go、nodes 模块、NodeVersionPanel、CoreUpdatesPanel，未安装依赖、提交、部署或访问生产。

本文的请求数来自源码调用链，chunk 大小来自本地生产构建；没有本轮生产浏览器点击计时。A 同时修改工作区，因此整包大小包含 A 的同期变动，不能将所有差值归因于 B。

## 全局导航

| 检查项 | 结论与改动 |
| --- | --- |
| 管理员守卫 | `ensureSession()` 已缓存 ready 并合并首次 session，不会每次导航读取；保持原权限判断。 |
| 门户守卫 | 原来每次调用都实际读取 session；现在 portal API 复用尚未到 expiresAt 的已认证会话。没有有效到期时间或已过期时仍读服务端，门户数据接口 401 继续清会话并回登录页。 |
| 布局 | MainLayout 不读取业务数据，路由按 path 而非 fullPath 挂载，query 切栏保留实例；未增加 KeepAlive 或全局数据缓存。现有 out-in 渐隐退出约 150ms 保留。 |
| SSE | main.ts 在认证/路由变化时调用 connectEvents，但 events.ts 已有连接存在检查；后台导航不会重连。进入门户/退出认证关闭，回后台创建一条。未重做连接。 |
| GET | 新增仅进行中的同路径、同超时 GET 合并；完成/失败即移除。不同超时分开。返回 structuredClone，避免两个编辑器共用对象。写入、上传开始和结束以及 CSRF 身份变化清除合并表，保存后的读不复用旧请求。 |
| choices/allPages | 保留原分页完整性检查与顺序分页，不引入跨页强缓存。并发的相同页请求由 API 去重；隧道节点选择、总览/用户详情/任务节点分页明确使用 100 条。客户端策略节点选择仍使用原分页调用，完整拉取但每页 20。 |
| 公共大依赖 | Chart.js 原本已有独立 chunk，后台主入口未直接静态载入它。门户原来静态导入五栏目，导致概览也载入图表/二维码；改为五栏目分别异步加载。其余页面继续沿用现有懒路由。 |
| 全局通知 | 事件合并至最多每 5 秒一次，隐藏页不读，回前台补读；序号避免旧筛选覆盖新结果。全部已读使旧请求失效，结束后重新读真实列表，失败不遗留 loading。 |
| 页面首显 | 访问记录不再等筛选项；用户详情先显示用户信息；客户端策略配置、辅助选项、候选并行；策略编辑详情与七组选择项并行。 |
| 刷新稳定性 | 任务静默合并、总览隐藏暂停、代理节点状态序号与节流、画布同方案保留状态、用户告警不重读订阅或重置门户编辑。没有新增自动整页 reload。 |

### 构建 chunk 对比

数值单位 kB，Vite 未压缩大小。动态加载改变依赖时机，并不缩小 Chart.js 库本身。

| chunk | 修改前 | 修改后 | 说明 |
| --- | ---: | ---: | --- |
| index | 178.39 | 179.25 | 增加请求与刷新保护；包含同期工作区变化 |
| chart | 172.36 | 172.36 | 门户概览不再经静态栏目依赖加载 |
| qrcode | 6.82 | 6.82 | 门户订阅栏目按需加载 |
| PortalLayout 共享实现 | 21.69 | 5.22 | 拆出五栏目；概览另加载 5.42 |
| ClientPolicyPage | 138.75 | 138.78 | 加载批次并行，未改配置编辑器布局 |
| RelayCanvasPage | 99.69 | 99.94 | 状态卡可见性及保留显示 |
| SettingsPage | 88.87 | 89.37 | 标签加载和更新轮询生命周期 |
| DashboardPage | 29.34 | 29.51 | 并行、隐藏暂停、错误保留旧数据 |
| UserDetailPage | 34.12 | 34.85 | 首显、状态刷新与序号 |
| AccessRecordPage | 13.80 | 13.99 | 并行及标签竞态修复 |
| 主 CSS | 95.89 | 95.89 | 布局/字体未调整 |

PortalLayout 共享实现 gzip 从 8.10 降为 2.28 kB；概览独立 chunk gzip 2.39 kB。主入口 gzip 64.65 → 64.91 kB。Vite 输出仍为 `web/dist`，未改 vite.config 或嵌入方式。

## 逐页审计

接口省略 `/api/v1`，均指 GET，另有说明除外。`P` 为资源页数，`N` 为服务器/配置数量；数量以各资源只有一页的下限计算，不含全局 session/SSE/通知。验证列：**T**＝typecheck/build；**N**＝navigation-performance-selftest；**L**＝page-refresh-lifecycle-selftest；**E**＝既有相关自测；**S**＝源码核对。自动刷新列描述修改后的行为，问题列保留发现的原问题。

| 页面／路由 | 打开时请求 | 自动刷新来源与频率 | 发现的问题 | 本次修改 | 验证 |
| --- | --- | --- | --- | --- | --- |
| 总览 `/dashboard` | `/dashboard`、全量 `/nodes`、Owner `/users`，至少 3 次，现并行 | 30 秒；隐藏跳过、前台补一次 | nodes→users 串行；后台继续读；失败可能将用户清空 | 三类并行，100 条节点分页，单飞/静默，失败保留旧数据并报错 | T/N/E |
| 用户 `/users` | `/users`、`/node-groups`、`/admins`，3 路并行 | system.alert 合并 5 秒；前台恢复 | 事件每次三请求，无序号；辅助失败静默置空 | 告警只刷新 users；隐藏暂停，序号；辅助错误显式暴露 | T/N/E |
| 管理员（合并 `/users`） | 同上 `/admins?pageSize=50` | 只随初载/显式用户页面操作读取 | 用户告警反复读取管理员 | 告警保留 admins/groups；编辑操作仍完整刷新 | T/N/S |
| 用户详情 `/users/:id` | 首批 user、groups、nodes、inbounds 并行；再 subscription-link、runtime-policy、install-endpoint、subscription-preview、accounts summary、access-records、两次 operations，至少 12+分页 | system.alert 5 秒，仅用户状态及四类活动请求；隐藏暂停/前台恢复 | 用户首显等所有选项；每事件清链接/二维码且覆盖门户开关 | user 返回即显示；节点100/页；次要请求并行补齐；告警保留门户草稿和订阅；活动/预览序号 | T/N/L/E |
| 节点列表 `/proxy/nodes` | nodes、inbounds、canvas-schemes、subscription-sources、imported-proxies、node-groups 并行；协议表一次；再 N 个 relays、可见服务器 inbound-states | watch 可见节点；node/deployment SSE 共用5秒节流；隐藏跳过/恢复 | 状态旧响应覆盖新视图；匿名 click 永不移除 | 状态 generation；发布事件合并；卸载 click/visibility 监听 | T/N/E |
| 入站 `/proxy/inbounds` | 重定向 `/proxy/nodes` | 同节点列表 | 旧 InboundListPage 不挂载 | 未修改旧页；实际入站详情另属 A | S |
| 出站 `/proxy/outbounds` | 重定向 `/proxy/nodes` | 同节点列表 | 旧 OutboundListPage 不挂载 | 未修改旧页；实际出站详情另属 A | S |
| 出口组 `/proxy/outbound-groups` | 重定向 `/proxy/nodes` | 同节点列表 | 旧组件5秒全量刷新不在当前路由执行 | 未修改不可达组件 | S |
| 代理链 `/proxy/chains`、new、id/edit | 重定向 `/proxy/relay` | 同画布 | 旧列表/编辑页不挂载 | 未修改不可达组件 | S |
| 节点分组 `/proxy/groups` | groups、nodes、inbounds、policies、subscription-rules、sources、imports、users，8路+分页 | 无自动刷新，保存/操作后 load | 主列表仍等待全量辅助信息 | 未改本页；共享GET去重生效；未加会覆盖选择的后台刷新 | T/S/E |
| 隧道 `/proxy/tunnels` | tunnels、tunnel-rules、nodes、Owner inbounds，4路+分页 | 无定时/SSE，保存/发布后刷新 | choices 的 size 被丢弃，节点实际20/页 | 正确传100条页大小；列表/编辑语义不变 | T/E |
| 隧道规则 `/proxy/tunnel-rules` | 同隧道，共用组件 | 标签 watch 关闭编辑；操作后刷新 | 同上 | 同上 | T/E |
| 中转方案 `/proxy/relay` | nodes、inbounds、outbounds、schemes、sources、imports 并行；legacy-resources 后台；N份节点元数据；当前方案 status、remove-plan（POST） | 批次进行中3秒；方案版本/刷新变化重启订阅 | 后台轮询；同方案版本变化清空状态卡 | 状态卡隐藏停止、恢复重读；同方案保留状态，仅方案ID变化清空 | T/N/L/E |
| 客户端策略 `/routing/client` | profiles list→N详情；presets、service-sets、nodes、groups；candidates | 无SSE/轮询，编辑watch更新dirty | 配置→辅助→候选三批串行 | 配置链、辅助四路、候选三批同时启动，配置就绪即显示 | T/E/S |
| 节点策略 `/routing/rules` | policies 当前页、service-sets、outbounds、全部outbound-groups/chains，5路+分页 | 搜索/分页/操作；无自动 | 快速搜索旧结果覆盖 | generation 防旧结果；辅助每次重读暂保留 | T/S |
| 策略新建 `/routing/rules/new` | users、nodes、groups、sets、outbounds、outbound-groups、chains，7路 | 无自动 | 选择项全就绪才显示 | 保留新建逻辑 | T/L |
| 策略编辑 `/routing/rules/:id/edit` | 上述7组加 `/policies/:id`，8路同时 | 无自动；path key切ID重挂载 | 详情在选择项后串行 | 详情加入同一 Promise.all，只读一次 | T/L |
| 规则集 `/routing/services` | `/service-sets` 当前页，1次 | 分页/保存后；无自动 | 旧页响应可能覆盖新页 | generation，旧响应不得清 loading/报旧错误 | T/S/E |
| 策略模拟 `/routing/simulator` | nodes、users、inbounds、service-sets，4路；有地址查询参数后模拟POST | 无自动 | 首100条选择限制；无刷新风暴 | 未改选择范围；共享GET去重 | T/S/E |
| 全部访问 `/access` | 列表、traffic summary、accounts summary 三路；另 users/nodes/policies/chains/outbounds 五组独立，至少8+分页 | 无自动；筛选/页码/query tab变化读取 | 五组辅助阻塞；三类主数据串行；loading丢操作并将旧数据写到新标签 | 三类主数据并行，辅助后台；捕获tab/page/filter+序号；watch query | T/N/L |
| 已阻断 `/access?tab=blocked` | 同上，主接口 `/access-records/blocked` | 同上 | 同上 | 同上 | T/N/L |
| 域名统计 `/access?tab=stats` | 同上，主接口 `/access-records/domains` | 同上 | 同上 | 同上 | T/N/L |
| 备份恢复 `/system/backups` | backups、backup-runs、子组件restore-records，3次并行 | backup_status 合并5秒；隐藏跳过/恢复 | 突发事件重复请求 | 合并事件，保留已有列表及恢复密码/预览 | T/S/E |
| 操作日志 `/system/operations` | `/operations` 当前页，1次 | 手动搜索/分页 | 整表反复骨架、旧结果覆盖 | 有数据时保留列表，generation | T/S |
| 任务记录 `/system/tasks` | 全量nodes、agent-updates、core-updates、releases，4组并行、各组串行分页 | node_status 5秒；在途只排一次后续；隐藏暂停/恢复 | 每心跳拉全历史、重叠请求和刷新状态 | 单飞+尾随、静默；节点100/页；全量任务合并仍保留 | T/N/E |
| 配置版本 `/system/revisions` | 重定向 `/nodes` | 旧 RevisionPage 不挂载 | 真正配置版本在服务器详情 | 未改，属 A | S |
| Agent/Core旧路由 | `/system/agents`、cores、updates、task-records 重定向 tasks | 同任务页 | 旧wrapper不执行 | 不碰 A 的组件 | S |

### 系统设置各标签

所有路径为 `/system/settings?tab=...`。之前 Owner 无论标签都额外读 retention/base/install-endpoint/tuning 四项；现在仅当前标签首次按需读取，返回已加载标签保留未保存表单。基础设置更新事件在其他标签期间会记为待处理，返回时按 dirty 规则刷新或提示。

| 标签 | 打开时请求（本次后） | 自动刷新来源与频率 | 发现的问题 | 修改／保留 | 验证 |
| --- | --- | --- | --- | --- | --- |
| base | system/base、install-endpoint、tuning-auto-apply、certificate-settings、certificates，5次并行 | system_settings重读base/tuning，dirty提示；证书处理中5秒且隐藏不读 | 额外读retention；证书回前台要等下个tick | 去除retention；基础事件不丢；证书现有轮询保留 | T/N/S/E |
| accessLog 默认 | access-retention、access-retention-job、storage-thresholds，3次 | 手动，无轮询 | 额外读取隐藏base/endpoint/tuning三项 | 去除三项；storage observation仍点击才发 | T/S |
| network | runtime、runtime-policy、subscription-formats，3路 | 无自动 | 仍有两个无关子组件请求 | 去除父组件四项；子组件读取保留 | T/S |
| security | 上述3项+admin-entry，4次 | 无自动 | 格式注册表无关 | 去除父组件四项；原表单保留 | T/S/E |
| subscription | runtime、runtime-policy、subscription-formats，3路 | 无自动 | runtime仅为共同加载条件 | 去除父组件四项；子组件耦合待拆 | T/S |
| info | 上述3项+panel-update，4次 | 更新中3秒、15分钟原限时；隐藏暂停/恢复 | 卸载时在途tick可能重新安排timer；后台持续轮询 | disposed+polling+inflight；晚返回不复活；结果核实保持 | T/L |
| latency | probe-targets，1次 | 操作后刷新 | 原额外四项公共读取 | 取消公共请求 | T/S/E |
| backup | backup-schedule，1次 | backup_status/system_settings事件；dirty不覆盖 | 子组件事件未节流 | 取消公共请求；保留既有dirty保护 | T/S/E |
| notifications | notification-rules、notification-channels，2路 | 保存后局部刷新 | 未发现心跳覆盖编辑 | 取消公共请求 | T/S/E |

### 门户

门户路径统一省略 `/portal` 接口前缀。以下数量不含无有效缓存时的一次 `/portal/auth/session`。

| 页面／路由 | 打开时请求（本次后） | 自动刷新来源与频率 | 发现的问题 | 修改 | 验证 |
| --- | --- | --- | --- | --- | --- |
| 登录 `/` | 门户auth/session；管理员首次守卫另有auth/session | mounted一次 | 无轮询；已有管理员首次探测 | 保留独立身份边界 | T/E |
| 概览 `/portal/overview` | me、subscription、devices-online三路并行，原4次两阶段 | section/snapshot watch，无定时 | 额外nodes，devices串行，提前载入Chart/QR | 只读所需三项并行，栏目懒加载 | T/L/E |
| 订阅 `/portal/subscription` | subscription，1次，原3次 | 切栏；重置POST后局部更新 | 多读me/nodes | 只读订阅，QR按需 | T/L/E |
| 节点 `/portal/nodes` | nodes，1次，原3次 | 切栏，无后台 | 多读me/subscription | 只读节点 | T/E |
| 流量 `/portal/traffic` | traffic?range，1次，原4次两阶段 | range变化只读traffic | 切范围重拉三公共接口、整图卸载、旧范围覆盖 | 独立range watch+序号，保留图表实例；Chart按需 | T/N/L/E |
| 账号 `/portal/account` | me，1次，原3次 | 切栏，无后台 | 多读nodes/subscription | 只读profile；密码操作保持 | T/E |
| Owner预览 `/portal-preview` | 上游管理员预览快照；PortalPage无门户请求 | section/snapshot watch | 原静态加载所有栏目 | 五栏目懒加载；只读快照保持 | T/E |

## 文件清单

仅列本工作流实际写入的增量文件；工作区其他有效改动未回退。

| 文件 | 本次改动 |
| --- | --- |
| web/src/app/apiClient.ts | GET在途合并、独立返回值、写入/上传/身份失效 |
| web/src/components/NotificationBell.vue | 通知节流、可见性、序号、已读竞态与失败loading |
| web/src/portal/api.ts | 有明确有效期的门户session复用 |
| web/src/portal/PortalPage.vue | 按栏目读接口、异步子组件、range独立更新/序号 |
| web/src/modules/access/pages/AccessRecordPage.vue | 主数据并行、辅助后台、query同步、序号 |
| web/src/modules/dashboard/pages/DashboardPage.vue | 用户/节点并行、100分页、可见性、静默单飞、错误保留数据 |
| web/src/modules/users/pages/UserListPage.vue | 状态事件仅读users、节流/可见性/序号、辅助错误暴露 |
| web/src/modules/users/pages/UserDetailPage.vue | 用户先显示、100分页、告警不覆盖草稿、活动序号与错误展示 |
| web/src/modules/proxy/pages/ProxyNodeListPage.vue | 发布状态序号/事件节流/可见性、click监听清理 |
| web/src/modules/proxy/components/relay/CanvasStatusCard.vue | 隐藏取消轮询、恢复读取、同方案保留状态 |
| web/src/modules/tunnels/api.ts | choices正确传节点pageSize |
| web/src/modules/routing/pages/ClientPolicyPage.vue | 配置、选择项、候选并行 |
| web/src/modules/routing/pages/RoutingRuleEditorPage.vue | 策略详情与选择项并行 |
| web/src/modules/routing/pages/RoutingRulePage.vue | 防过期列表响应 |
| web/src/modules/routing/pages/ServiceRulePage.vue | 防过期规则集响应 |
| web/src/modules/system/pages/SettingsPage.vue | 按标签首次读取，后台标签更新待处理 |
| web/src/modules/system/pages/TaskRecordPage.vue | 静默单飞尾随刷新、后台暂停、节点100分页 |
| web/src/modules/system/pages/BackupPage.vue | 备份事件节流和可见性 |
| web/src/modules/system/pages/OperationLogPage.vue | 旧列表保留、防倒序 |
| web/src/modules/system/components/PanelUpdatePanel.vue | 更新轮询卸载/在途/可见性生命周期 |
| web/scripts/navigation-performance-selftest.mjs | GET行为及页面接线回归 |
| web/scripts/page-refresh-lifecycle-selftest.mjs | 实际加载函数的并行/乱序/草稿/轮询生命周期测试 |
| docs/superpowers/plans/2026-11-14-navigation-performance-b.md | 实施计划与基线 |
| docs/frontend-performance-audit.md | 本审计表及验证证据 |

## 验证与输出

PowerShell 使用 npm.cmd，避免 npm.ps1 执行策略限制。esbuild 在沙箱内读取父目录被拒，构建及包含 esbuild 的全量自测在授权本地执行；未下载依赖。本轮未调用 Go，vet/test 未运行。

```powershell
cd web
npm.cmd run typecheck
npm.cmd run build
```

结尾（最终构建记录见本地 `.tools/performance-b/build-after.log`）：

```text
> proxy-admin-ui@1.0.0 typecheck
> vue-tsc --noEmit
✓ 360 modules transformed.
dist/assets/chart-D2kUuchd.js  172.36 kB │ gzip: 60.41 kB
dist/assets/index-TOYf3awf.js  179.25 kB │ gzip: 64.91 kB
✓ built in 4.93s
```

全量测试按文件逐个 Node 执行并收集失败，没有遇到首个失败就中止：

```powershell
$failed = @(); $count = 0
foreach ($file in Get-ChildItem web/scripts/*-selftest.mjs) {
  $count++; node $file.FullName
  if ($LASTEXITCODE -ne 0) { $failed += $file.Name }
}
"SELFTEST total=$count failed=$($failed.Count)"
$failed
```

```text
PASS page-refresh-lifecycle-selftest.mjs
PASS portal-selftest.mjs
PASS server-perf-selftest.mjs
FAIL ui-regression-selftest.mjs
ReferenceError: error is not defined
  at Object.refreshNode (...ui-regression-selftest.mjs:441:24)
  at ...ui-regression-selftest.mjs:457:1
PASS user-entries-selftest.mjs
PASS user-portal-login-selftest.mjs
SELFTEST total=38 passed=37 failed=1
ui-regression-selftest.mjs
```

此失败来自该测试抽取的 `modules/nodes/pages/NodeDetailPage.vue` refreshNode 与测试注入变量不一致，属于工作流 A 正在修改范围；B 没改该实现或测试。其余37个通过。首轮额外的 client-policy 精确源码断言、portal guard stub 与沙箱 esbuild 问题已消除：客户端节点分页保留原调用；session复用移到原API方法，不改变guard依赖接口；全量自测改为授权本地执行。没有删除或降低既有断言。

新增行为自测结尾：

```text
PASS navigation GET: concurrency, isolated values, no cache, mutation/session boundaries, timeout and failure cleanup
PASS navigation pages: stale response guards, parallel access reads, isolated traffic refresh, hidden refresh and listener cleanup
PASS background refresh: user drafts preserved, list ordering, update disposal, canvas visibility and notification ordering
PASS access loader: three parallel reads, latest tab/page wins, stale totals ignored
PASS panel poller: hidden pause, foreground single read, pending unmount, terminal stop
PASS page dependencies: lazy traffic/QR and one parallel policy detail request
PASS user status: new usage and activity, unsaved portal switch preserved
```

对17个本次修改的Vue文件使用 compiler-sfc + compiler-dom解析模板AST，递归拒绝无任何指令/属性的嵌套template；结果：

```text
PASS Vue AST: 17 changed components, no bare nested template
PASS new files: LF, no BOM
```

## 未修改的问题与建议

- **A 范围**：服务器详情的上述全量自测失败需要 A 同步测试上下文；配置版本、Agent/Core面板性能也由 A 验收。
- **任务历史**：仍将全部 Agent/Core任务及版本拉到前端合并分页。当前消除并发积压和不可见刷新；长期应提供后端统一分页接口，避免历史增长成本。本轮不改Go接口或数据口径。
- **节点分组/代理列表**：首屏仍等多组辅助资源；导入共享store仍有旧页面迟到写入风险。后续可拆主列表与选择项状态，需专门覆盖编辑、导入及跨页状态，避免打断现有v1.5.5功能。
- **客户端策略**：profiles list→N详情仍存在；卡片当前依赖完整config，不能直接删除详情请求。节点选择仍20/页，但完整分页。
- **选择项上限**：访问记录用户/策略/出站、策略模拟等仍读取前100项；本轮未扩大数据选择语义。
- **设置子组件**：RuntimeSettingsPanel仍固定读取runtime/policy/formats，network/info有冗余；BackupSchedulePanel事件未节流但已有dirty保护；证书隐藏不发请求，回前台仍可能等5秒。可继续按实际kind拆读取并补专项测试。
- **代理发布状态错误**：原逐服务器请求失败时返回空状态数组的行为仍在，建议单独改为保留旧值并展示错误，覆盖“未知/未发布”口径。
- **导航动画/移动侧栏**：现有150ms退出过渡保留；移动抽屉可能额外挂载一份通知组件，共享GET可合并同期读取。若要改动画或侧栏实例布局，单独确认交互后处理。
- **真实耗时**：没有本轮生产浏览器基线和优化后计时，不能给出点击耗时下降比例。下一步可在授权测试面板同一数据集测冷/热导航、快速切tab、隐藏恢复、慢接口与SSE突发。

假设：沿用现有接口分页、会话expiresAt和浏览器structuredClone支持；权限、不可用选项隐藏规则、字体和布局不改。没有超出B约定范围的修改。

## 工作流 A：服务器页面

范围：`web/src/modules/nodes/**`、`system/components/NodeVersionPanel.vue`、`CoreUpdatesPanel.vue`。请求数按源码调用链统计（接口省略 `/api/v1`），未做生产浏览器计时。

| 页面／路由 | 打开时请求 | 自动刷新来源与频率 | 问题与修改 |
| --- | --- | --- | --- |
| 服务器列表 `/nodes` | nodes、server-order、node-groups、monthly、machine-traffic；每台另有 meta 与今日流量；可见卡片读 latency | SSE 节流 5 秒；可见时 30 秒；回前台补读 | 筛选、翻页改为本地处理不再完整重读；首屏只等节点列表，顺序与补充数据后台填入；静默刷新去重，失败保留数据 |
| 详情公共部分 | node、groups、revisions、今日流量、monthly、machine-traffic、meta 共 7 个 | **原来每次心跳 SSE（约 15 秒）都完整重读 7 个请求**；现在心跳只读 node，附属数据可见时 60 秒 | 节点返回即展示；读取错误不再替换已显示的页面；旧响应按序号丢弃 |
| 概览 | 公共＋inbounds、outbounds | 随详情 | 心跳不再重读附属数据，布局不变 |
| 节点 | 公共＋inbounds、outbounds、outbound-groups | 打开、保存后 | 心跳不重新挂载 |
| 监控 | 公共＋metrics、latency | 各 60 秒、切换范围、回前台 | 页面隐藏暂停；指标请求防重叠；静默刷新保留图表 |
| 配置版本 | 公共＋分页 revisions | 标签打开、操作后 | 未打开该标签不读取；静默刷新不显示骨架 |
| 调优 | 公共＋tuning、recommendations | **原来每次心跳重读**；现在可见时 60 秒，执行中 3 秒 | 删除心跳 watcher；保留草稿；保存后旧读取失效 |
| 设置 | 公共＋整机流量设置、restart-core、core-updates、core-releases | 重启执行中 2.5 秒；核心任务 60 秒 | 元数据按字段合并，编辑中的字段不被刷新覆盖；整机流量读写竞态隔离；隐藏暂停 |
| 版本与更新 | 节点列表、发布目录；核心页另读任务 | SSE 只轻刷节点；任务可见时 60 秒 | 任务后台读取；静默刷新不切加载状态 |
| 新增／安装弹窗 | 提交后登记、保存 meta；安装阶段读 node、安装端点 | 匹配节点 SSE＋15 秒、回前台 | 在途去重；隐藏暂停；关闭或切换目标后旧响应失效 |

遗留（未改）：服务器列表首次打开每台 2 个请求（meta、今日流量）仍为 N+1，需新增批量接口；配置版本父子组件各读一次 revisions；设置页折叠的核心更新区域仍会初始读取；Viewer 打开概览会请求出站配置。

新增自测：`server-perf-selftest.mjs`。审查后补修：`ui-regression-selftest.mjs` 接线更新；后台刷新失败不再弹 toast（`background-refresh-selftest.mjs`）。
