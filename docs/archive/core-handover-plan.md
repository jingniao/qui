# Agent 更新时核心不断线：Linux 脱离与认领实施设计

状态：已实施（目标版本 1.10.0），验收结果见第 9 节

第 1～8 节保留实施前的设计与审阅记录，源码行号基于当时工作区 HEAD `fbce03a`；其中“当前”“建议”“待决”和未勾选清单均属设计时状态。最终取舍以第 8.1 节 Owner 决定为准，实施与验收范围以第 9 节为准，现行运行规则见项目说明书、部署与运维文档。

## 1. 结论、范围与需要先解决的边界

采用已确定的方案 B：Linux 核心独立于 Agent 存活，新 Agent 验证原进程身份、运行材料与新渲染配置后接管。systemd 与 OpenRC 同时支持；Windows 启停、退出观测、输出及恢复行为保持现状。

成功交接的验收条件是：核心 PID、内核启动 token、epoch、Revision 均不变，既有 TCP/UDP 会话持续，账户与节点累计计数接着上报。仅 Agent 控制连接短暂重连。配置确实变化时，明确进入正常发布，允许这一次连接中断。首次升级由旧 Agent 执行，仍会断线，已接受。

设计须覆盖两种更新：直接服务进程更新，以及“引导程序 → 受管 Agent worker”的更新。当前 `update.sh` 会先重启引导服务，再独立提交实际 worker 镜像；只改 SIGTERM 路径会漏掉 EOF 停止的 worker，且一次完整更新可能断两次。

有三项约束不能用当前代码直接保证：

1. **核心在交接空窗内死亡后的完整流量**：累计值只在核心内存中；最后一次成功采样之后、核心死亡之前的字节无法补取。正常存活交接可以做到不漏计、不重复计；对死亡场景承诺绝对零丢失，需要额外的独立持久计量机制，超出本方案。必须暴露“不完整”，不得按最后样本冒充完整结算。
2. **无人读取时的原始日志留存**：当前 Xray 的约 100 ms 清理依赖 Agent。Agent 停止后文件继续增长，既有 20 MiB 截断也无人执行。单纯改为文件无法同时保证断更期间持续审计、严格留存时间与无限期 Agent 故障下的空间限制。
3. **存活连接的观测与动态门控**：sing-box 的来源关联在 Agent 内存；Xray 动态路由门控不在静态配置摘要中。不能把“进程活着、JSON 相同”直接报告为全部功能恢复。

第 8 节列出这些事项的 Owner 决策。未决项须在对应阶段启用前确认；本文不把它们藏进默认值或自动降级。

## 2. 现状调用链与证据

### 2.1 核心启动、发布与恢复

| 环节 | 当前调用链与代码位置 | 对交接的影响 |
| --- | --- | --- |
| 入口与信号 | `cmd/agent/main.go:378` 用 `signal.NotifyContext` 合并 SIGINT/SIGTERM；`:380` 进入 managed worker，`:396` 进入 agenthost，`:398` 直接进入 control | context 取消目前没有携带退出原因 |
| worker 生命周期 | `agentprocess/process.go:86` 创建 worker，`:103` 用 Wait 等退出，`:146` 将父进程 lifetime 结束转为 Stop；`:177` 的 Stop 关闭 stdin，30 秒后杀 worker，再等 5 秒；`cmd/agent/managed.go:20` 任何额外输入、EOF 都 cancel | 更新用 EOF，不能只判断是否收到 SIGTERM；异常 EOF 也不能授权保留核心 |
| 运行时构造 | `internal/agent/control/control.go:246` 合并 inventory，`:267` 打开核心安装记录，`:276` 恢复 inventory，`:305` 构造各核，`:312` 包为 MultiRuntime | 必须检查每个运行目录，包括已移除/禁用但仍有标记的核心 |
| 启动存储门禁 | `control/control.go:335` 起检查 CoreAccounting 格式与所有保留回退镜像；`:380` 构造 Reconciler，`:417` 打开 bound spool，`:446` 注入 hooks | 认领须在身份、存储、spool 门禁通过之后，supervisor 启动之前 |
| 普通发布 | `reconcile/reconcile.go:254` 激活规则集，`:259` Render，`:270` Validate，`:311` Apply；`core/multi.go:272` 全部校验后按配置顺序 Apply，无活跃入站者 Close，失败逆序补偿 | 普通发布必须保留全核重启语义，不能顺手变成按摘要跳过发布 |
| 启动恢复 | `control/control.go:451` 修复中断的核心安装，`:463` 调 `manager.Restore`；`reconcile/reconcile.go:533` 校验保留快照摘要，`:501` 验证身份、成功回执后 Render → Apply → host-network | 当前恢复最终仍走强制重启的 Apply；需要专门的启动恢复/认领入口 |
| Xray 启动 | `core/xray/adapter.go:178` Apply → `applySwitch`（`:210`）stop → start（`:236`）；`:287` Claim，`:311` Start，`:318` Wait，`:322` Bind，`:384` 设置 current，`:388` 设置新 statsEpoch | 每次 start 都生成新 epoch；认领不得调用 start |
| sing-box 启动 | `core/singbox/adapter.go:280` Apply → `applySwitch`（`:310`）→ `startLocked`（`:336`）；`:363` Claim，`:379` Start，`:385` Wait，`:405` Bind，`:446` current，`:447` statsEpoch | 同上；`:128`、`:138` 分配私有端口，`:145` 生成 Clash secret，均为内存状态 |
| GOST 启动 | `core/gost/adapter.go:153` Apply → stopLocked → startLocked（`:212`）；`:239` 写 YAML，`:244` 删除旧 metrics.sock，`:249` Claim，`:262` Start，`:267` Wait，`:283` Bind，`:286` settle | 认领时不得写配置、删 socket、重跑启动 settle |
| 核心与 Agent 绑定 | `core/procattr_linux.go:22` 返回 `Pdeathsig: SIGKILL`，三核启动均使用它 | Setsid 只脱离会话，不脱离 cgroup；必须同时改服务停止策略 |

本节表中 `agentprocess/`、`control/`、`reconcile/`、`core/` 的省略路径前缀均为 `internal/agent/`。

### 2.2 退出观测、停止与服务停止

| 环节 | 当前实现与位置 |
| --- | --- |
| Xray 退出/健康 | `internal/agent/core/xray/adapter.go:513` Health 读一次性 done，清 process 与 marker；`:543` 检查 TCP。`:422` stop 在杀核心前最终采样；`:449` Close 即使最终采样失败仍 forceStop；`:463` Kill 后 ReapProcess，确认退出才 ConfirmExit |
| sing-box 退出/健康 | `internal/agent/core/singbox/adapter.go:547` Health 读 done，`:575` 检查监听；`:462` stopLocked 最终采样，`:488` forceStop，`:524` Close；UDP 检查经 `configurationHealth`（`:589`） |
| GOST 退出/健康 | `internal/agent/core/gost/health.go:110` Health 读 done、检查监听；`gost/adapter.go:305` stopLocked 当前忽略最终采样返回值（统计源会标失败），`:324` forceStop，`:344` Close |
| 通用退出确认 | `internal/agent/core/runtime.go:33` ReapProcess 等 done 最多 5 秒，超时返回 ErrCoreStopUnconfirmed。done 的值只能消费一次，不能给认领伪造一个 `exec.Cmd` 或已成功 Wait 的 channel |
| 启动遗留标记 | `internal/agent/core/run_marker.go:36` Observe 读 marker；仅本 Agent 持有的 OwnedChild（`:73`）或已证明退出者可继续；外部存活者一律 blocked。`:86` Ready 在已退出时 ConfirmExit；`:112` Claim 在启动前落 PID=0，`:130` Bind 再写真实 PID/token |
| 身份解析 | `internal/agent/core/lifetime.go:116` ResolveCoreRun：同 host 不同 boot 证明退出；跨 host/同 boot 跨 namespace/缺事实均 unknown。`lifetime_process_linux.go:16` 读 `/proc/<pid>/stat` 字段 22，token 为 `l<ticks>`；`lifetime_process.go:26` PID 不存在或 token 不同说明原进程退出 |
| 格式 | `internal/agent/core/lifetime.go:40` 当前 marker version=2，读取兼容 version=0/1；`:172` 严格拒绝未知字段，旧环境缺失不补造；`:194` 加密、临时文件 fsync、rename、目录 fsync；`:244` 移除也同步目录 |
| 恢复错误传播 | `internal/agent/control/control.go:188` 只把 ReleaseRecovery、CoreStopUnconfirmed、RuntimeRestore 视为可保留控制连接的恢复失败；`:467` 明确记录失败；`:1162` 心跳处理未确认停止。ErrCoreLifecycleUnconfirmed 在 `core/lifetime.go:17` 声明，当前实际共享 RunMarker 路径主要返回 ErrCoreStopUnconfirmed，不能按背景假定两者均有独立分支 |
| supervisor | `internal/agent/supervisor/supervisor.go:97` 只有监听失败等连续 3 次；`:219` 每 3 秒检查，`:274` 分核 RestoreCore，`:333` 健康后 Refresh；`reconcile/multi.go:11` 与发布串行化；`core/multi.go:413` 仅恢复目标核心 |
| Agent 收尾 | `internal/agent/control/control.go:318` closeCore 最终调 adapter.Close；`:474` 后启动的 supervisor、采样协程先退出，再关闭核心、collector、spool。`core/multi.go:337` Close 所有核心 |
| init 停止 | `deploy/systemd/proxy-agent.service:17` TimeoutStopSec=90，`:18` KillMode=mixed；`deploy/openrc/proxy-agent:54` TERM/90/KILL/5，`:55` rc_cgroup_cleanup=yes |
| 更新停止/启动 | `deploy/linux/update.sh:1205` update_component，`:1219` 受管 bootstrap reserve，`:1236` service_stop，`:1277` 停服后快照，`:1301` install，`:1364` 写 started，`:1377` 附近 service_start + 10 秒稳定后提交 |
| 受管第二次切换 | `deploy/linux/update.sh:275` install_managed_release 调本地安装 CLI；`internal/agent/agenthost/jobs.go:408` 持久记录 started，`:414` Stop worker，`:449` NewDriver，`:466` Installer.Apply；`agentprocess/driver.go:91` Activate 内仍会 Stop/Start |
| 回滚与重启恢复 | `deploy/linux/update.sh:957` recover_snapshot，`:1014` 当前 stop 失败仍继续恢复文件，`:1016` restore_snapshot，`:1060` 启旧服务；`:1080` rollback_component。受管恢复见 `agenthost/host.go:183` Installer.Recover、`agenthost/recovery.go:62` recoverJob |
| 卸载 | `deploy/linux/uninstall.sh:27` uninstall_component，现行合同 `docs/deployment.md:312` 只要求服务完全停止才删除；改 KillMode 后“主服务 inactive”不足以证明核心全部退出 |

Linux 当前 UDP 健康通过端口占用推断（`internal/agent/core/udp_endpoint_other.go:15`），未逐 socket 验证 PID 所属；`docs/project-spec.md:443` 的“socket 归属”描述强于此实现。认领身份必须单独证明，不能拿监听拨通或 EADDRINUSE 代替；本任务不扩大为全仓 UDP 健康重构。

### 2.3 记账开始、持久基线与停止采样

**当前实现没有 AccountingRun 的可执行类型或独立结算日志。** `internal/agent/core/lifetime.go:58` 与适配器注释仍提到 accounting run，但 `accounting_material.go` 是保护材料的安全读取，`accounting_sync_unix.go` 是目录同步；它们不保存流量。下文“继续原 run”精确定义为继续原核心身份、epoch、Revision 和统计映射，沿用 spool 与面板游标。

| 层 | 当前行为与证据 |
| --- | --- |
| 开始运行 | 三核 start 为真实新进程生成 epoch（Xray `adapter.go:256`、sing-box `adapter.go:351`、GOST `adapter.go:235`），Claim/Bind 绑定身份。Xray/sing-box statsEpoch 与 marker.Epoch 相同 |
| 核心累计源 | Xray `internal/agent/core/xray/stats.go:148` readSample，`:189` QueryStats Reset_=false；sing-box `user_stats.go:104` readCounters、`:124` 同样不 reset；官方无用户 API 时 `singbox/stats.go:95` 取 Clash 累计总量；GOST `metrics.go:35` 读累计服务指标，`adapter.go:400` 用 marker.Epoch |
| 归属 | Xray `stats.go:211` 从 StatsAccounts 映射账户，并单独累计 forward 白名单；sing-box 仅有用户 API 才计用户；GOST 只计 forward 节点，不计 relay、不造用户数据 |
| 持久化 | `internal/agent/core/node_traffic.go:44` SaveAccountAndNode 先账户后节点，节点 epoch=`原epoch.node`（`:15`）；`control/control.go:431` TrafficSink=spool.SaveTraffic；`:482` 每 5 秒采样，MultiRuntime `multi.go:492` 采所有活跃核 |
| Agent 基线 | `internal/agent/spool/traffic.go:17` SaveTraffic，`:42` 按 epoch+credential 读取加密 high-water；`:55` 倒退报错，`:58` 相同值不重复入队；`:70` 更新游标与 `:114` 入队在 `:121` 同一提交边界，磁盘失败不推进游标 |
| 面板增量 | `internal/panel/access/account_traffic.go:85` 读 node+epoch+credential 游标，`:89` 不允许同 epoch 换归属/Revision，`:101` 累计减旧游标，`:123` 批次去重同事务；节点见 `node_traffic.go:20`、`:34` |
| 停止的真实含义 | 目前最终采样后 Kill/确认退出，不写独立“已结算”记录；意外退出无法再读统计。RunMarker.ConfirmExit 只删生命周期 marker，不能被描述成流量结算成功 |
| 非记账内存状态 | `core/counter_sources.go:11` 明说来源观测不等于持久游标；`singbox/node_traffic.go:33` 的 delta 和 `:58` NodeTrafficTotals 是旧心跳口径，不是 spool 的记账基线，不能拿它给认领累计流量做减法 |

### 2.4 输出与会话关联

- Xray：`internal/agent/core/xray/adapter.go:290` stdout discard，`:303` 建 tail；`xray/access_log.go:71` 新 tail **先 truncate**，`:147` 读后清理/20 MiB 隐私保护截断，`:173` close 假定核心已退出。认领不能复用“新进程先清空”入口。
- sing-box：`internal/agent/core/singbox/render.go:374` log 输出 stdout；`singbox/adapter.go:375` NewSingboxWriter，stdout 接 Agent 内 writer，stderr discard。`singbox/device_gate.go:76` 从 Clash 活跃连接与 writer.SourceUsers 关联；只重建 writer 会失去升级前已建立连接的来源归属。
- GOST：`internal/agent/core/gost/adapter.go:118` childLog 为 4 KiB 内存诊断尾部；`:261` stderr 接该 writer；`gost/health.go:26` settle 从它取失败原因。它是诊断日志，不能被当作访问审计或流量源。

### 2.5 现行产品合同

`docs/project-spec.md:402`、`docs/architecture.md:110` 规定普通发布重启全部核心，异常恢复只影响异常核；`../project-spec.md:443` 规定巡检；`:387` 规定访问行清理。`docs/deployment.md:236` 规定停服后的快照/回滚，`:285`、`:293` 区分受管与完整网页更新。实施时需同步这些位置，本文阶段不改它们。

## 3. 生命周期、认领与各文件的修改设计

### 3.1 进程句柄与身份不变量

新增 `internal/agent/core/process_handle.go`、`process_handle_linux.go`：提供同一 `ProcessHandle`，包含 Identity、可重复查询的 ExitObservation、Done（关闭广播）、Stop(ctx)、Detach。退出结果明确区分“原父进程 Wait 得到状态”与“外部进程退出已证明，退出码未知”。实际子进程仍只调用一次 cmd.Wait 回收；认领进程禁止 Wait、禁止伪造退出码为 0。

Linux 设计优先选 **pidfd**：先验证 marker 的环境与 PID token，再 pidfd_open，再次验证 token、进程状态与环境；用 pidfd 观测及发信号，消除验证后 PID 被复用的误杀窗口。`/proc/<pid>/stat` 的 zombie 状态视为原进程已退出；现有只读启动 ticks 的函数须扩展状态读取。pidfd 不可用返回明确 `core_handover_unsupported`，不在运行时悄悄换成裸 PID kill。旧内核支持策略为第 8 节 D1；在确认前不提高全体节点安装底线。

环境/身份规则沿用 `ResolveCoreRun`：跨 host、namespace、缺 machine-id/boot/token、PID=0 未绑定均阻止认领和覆盖；同 host 不同 boot证明退出；同环境 PID token 不同只证明原进程已退出，**绝不向复用 PID 发信号**。pidfd 打开前后身份不一致，放弃该句柄并重新分类为退出或 unknown，禁止把新 PID 接进原 epoch。

修改 `core/run_marker.go` 的 Observe/OwnedChild/Ready：将“本 Agent 持有句柄且身份完整匹配”纳入 owned，新增明确的 `Adopt(ProcessHandle, CoreRun)`；Claim 继续仅用于真实新进程；ConfirmExit 继续只在确证退出后调用。ErrCoreStopUnconfirmed 仍表示退出无法确认，认领材料损坏另报 `core_handover_invalid` 并保留原错误链，禁止把所有错误改为可认领。

修改三核 adapter 的 start、stop/stopLocked、forceStopLocked、Close、Health、UnconfirmedStop、CoreStartedAt，以及 stats、executable、gate、observer 中所有 `a.process != nil` / `.Process.Pid` / done 使用：统一查询句柄，真实启动持有 child handle，认领持有 external handle。Xray `device_gate.go:186` 的缓存不能再以 `*exec.Cmd` 指针识别核心，改用完整 run identity。Windows 仍走现有 child/Wait 语义，新增认领实现只编译于 Linux。

### 3.2 运行材料与摘要：先恢复渲染输入，再比较

**建议保持 CoreRun version=2、Kind、字段与现有 CoreAccountingFormat=4 不变。** 另建加密伴随文件 `runtime/<core>/core-runtime.json.enc`，格式 `core-runtime-v1`，使用该目录原 accounting.key，严格解码与安全读写沿用 accounting_material/lifetime 的边界。避免为了加摘要让所有保留 v1.7.5 回退镜像无法读取现有 marker。

新增 `core/handover_material.go`，包含以下持久信息：

- 完整 CoreRun 身份副本与其规范编码 SHA-256；每次读取与原 marker 精确比对。
- 运行时实际原生配置字节的 SHA-256、原生配置字节（加密）、渲染元数据版本；保留 StatsAccounts、forward 白名单、健康端点、AccessContext、日志路径、私有端口及端口用途、stats/userStats 端口、Clash secret、probePorts、ProbeFingerprints。
- 运行的核心可执行文件摘要/版本、固定参数和影响行为的环境（含 GODEBUG/规则集路径），所引用证书/规则集内容摘要。只比较路径会漏掉同路径内容变化；不输出配置原文、secret 或私钥到错误日志。
- 运行开始时间事实、用于当前渲染的出站健康事实及原观测时间；恢复时按原时间判断新鲜度，不能把旧观测时间改为现在来强行取得相同摘要。
- 已见计数源集合、最后成功采样时间；sing-box 活跃来源关联仅保存已有 collector 需要的最少字段，并绑定原 run，禁止额外收集 URL 或正文。持久更新与恢复规则见 4.3。

新 start 的顺序：保存 prepared 运行材料 → Claim(PID=0) → Start → Bind → 原生健康通过 → 保存绑定完整身份的 active 运行材料 → 才报告应用成功。active 材料未同步成功，停止确切核心并返回错误；退出未证实则保留标记，不报告成功。prepared/marker 之间崩溃不得被当作可认领。普通 stop 先采样、确证退出，再清 marker 和对应材料；任一步失败保留明确的清理状态，重入按完整身份处理。

新 Agent 的认领顺序：

1. 获取现有 lifecycle worker 独占锁，通过 identity/storage/inventory/成功修订回执校验；没有成功修订却有活核是错误。先处理已有核心安装 hold，禁止越过中断安装修复认领不确定镜像。
2. 枚举三个固定核心目录中所有 marker，逐一分类 alive/exited/unknown；不只查看启用 inventory。加载 active 材料并比对 identity；新旧 Agent 不得同时持有管理权。
3. 在所有核心 Render 之前恢复全节点私有端口保留集，给 `core/ports.go` 新增“认领已占用私有端口”的注册方法；它不尝试 bind 核心正在监听的端口。校验跨核用途、公开监听冲突；恢复各 adapter stats/probe 端口与原 secret。两遍渲染及全核 private-port guard 沿用 `multi.go:227`。
4. `reconcile.Reconciler.Restore` 增加仅启动使用的恢复模式（建议独立 `RestoreStartup`，不把普通 Restore/Rollback 改成隐式跳过发布）。读取成功 DesiredState，在当前时间用新版本渲染；先做不改变运行文件的原生校验，再比较配置字节和外部依赖、环境、核心镜像摘要，以及账户归属元数据。元数据变化即使 JSON 一样也不能继续原 epoch。
5. 所有活核均完全一致时，持有 pidfd、安装 current/history、恢复原 epoch/Revision 和采集上下文；读取统计并成功写回原 spool，恢复日志及门控，重新检查核心仍是原身份，才向上层提交认领成功。设置 MultiRuntime.current/expected/history 与 Reconciler.renderedDigest，避免下一次 Refresh 再重启。
6. 任一核配置确实不一致：记录 `core_handover_config_changed` 与差异类别，先建立旧配置的管理句柄/统计上下文，最终采样后按**现有全核发布**重启。保留“只认领一致配置”的规则，不能用新配置给老进程归属流量；新核心换 epoch。不一致本身是明确的重启分支，不是尝试认领失败后的通用兜底。
7. 材料损坏、unknown、身份/账户冲突、计数倒退均是阻塞错误，不能借“配置变化”杀进程重开。认领部分失败时只释放已安装的 observer/tail/句柄，不杀已经核对无误的健康同伴；不走 MultiRuntime.Apply 的逆序重启补偿。控制面保留在线，报告具体未确认核心；更新事务不能提交为成功。

缺伴随材料的旧 marker：仍按现行规则读取、证明退出；若旧核心仍活着则明确拒绝不断线认领。首次升级旧进程本来会被停止；若发现存活旧核，须走显式“确认身份后停止”的恢复步骤，不能伪造运行材料。保留旧 DesiredState、旧 spool 和在途更新任务，禁止清队列或改成功回执格式绕过恢复。

若实施改为扩展 CoreRun，必须提高 marker 内容版本和对应 CoreAccounting ABI，并改 `validateCoreRun`/`ResolveCoreRun` 按明确版本分别解释，继续接受合法 v0/1/2 原文。遵守 `control.go:335`、`agenthost/identity_preflight.go:22`、`storage/contract.go:233`：所有保留 baseline/current/previous 镜像证明可读才激活，禁止自动把旧标记补齐为新事实。这是替代设计，需要 Owner 确认后才能采用。

### 3.3 收尾入口与崩溃规则

新增 `core/handover.go`：定义显式 `PrepareHandover(ctx, request)`、`DetachForHandover(ctx, request)`，MultiRuntime 聚合；Close 始终保留“停止核心”的语义。Detach 不 Kill、不 ConfirmExit、不改 epoch，不作为普通 Close 的默认模式。

修改 `cmd/agent/main.go` 的信号入口、`cmd/agent/managed.go` 的 EOF 处理与 `control/control.go` 的 closeCore：取消原因按“更新授权、普通停止、异常取消”分开记录，使用独立有时限的收尾 context，不能拿已 cancel 的 ctx 做最终采样。只有有效交接请求且 Prepare 成功才允许 Detach；普通 stop/SIGINT、卸载、未授权 EOF 必须 Close。

受管路径不改变 `agent-start-v1` 协议，不向旧 worker 塞新字段/命令。外部交接文件是唯一保留授权，worker 在 EOF 时核验；`agentprocess.Process.Stop` 仍负责确认 worker 确实退出，退出超时或被迫 Kill 不能被父层当作交接成功。`agenthost/jobs.go` 必须在第一次 Stop 前准备请求；`Driver.Activate`、失败候选收尾与 Recover 在切回旧镜像前调用停核屏障，见第 5 节。

修改 `procattr_linux.go:22` 为 Setsid，不设置 Pdeathsig；修改 systemd KillMode=process，OpenRC rc_cgroup_cleanup=no。**这三项最后一阶段才一起启用**，此前进程绑定保持现状。

为兑现普通 stop/卸载，新增固定本机命令 `--finish-core-stop --config ...`（Linux，只接受本地配置路径，执行固定三核目录操作，无任意 PID/命令参数），服务 `ExecStopPost` / OpenRC `stop_post` 调用：

- 有本次已确认 detached 回执：验证授权、原进程集合及更新事务仍匹配，允许保留；只有请求而无 detached 回执不够。
- 普通停止或主进程超时/崩溃无成功交接：确认 worker 已退出、取得 lifecycle 锁，按原身份停掉仍存活核心；最终样本可读则持久化，不可读记录明确缺口，失败使服务停止失败。不能因主 PID 已退出就返回成功。
- worker 尚活/生命周期锁仍占用：返回 worker-stop-unconfirmed，不与旧 worker 并发采样、清材料或操作核心。需跟踪 worker PID/token，不能用 pkill、进程组编号猜测或 kill 整个 cgroup。
- `deploy/linux/uninstall.sh` 在删核心、配置、账号、状态之前显式执行同一屏障，并撤销交接授权；任何未确认退出均阻止删除。快照恢复及安装也不得覆盖活核仍引用的 TLS/规则集文件。

**非更新崩溃的建议规则**：不授予保留权；服务 post-stop 屏障停止遗留核心，init 重启 Agent 后按正常恢复启动新 epoch。若 Agent 在无 init 钩子的环境被 SIGKILL，核心可能暂存，新 Agent 先验证并停止遗留核心再恢复；不把残留自动认作计划交接。这样保持“只有更新保留”的范围，异常恢复仍可断线。该建议及是否改为崩溃也认领，列为 D4；不能在发布说明里暗示本阶段已经实现崩溃不断线。

## 4. 记账接续的精确规则

### 4.1 正常交接：原 epoch 下上报原累计值

设最后持久化的本地游标为 L，面板已入库游标为 P，认领后核心当前累计值为 C（上传、下载分别比较）。

1. 保持 node/core/process/epoch/Revision、credential→user/device/protocol 映射及统计源口径。Xray forward 白名单、GOST fwd 服务白名单和 `.node` 后缀也保持；账户与节点不相互替代。
2. 新 Agent 直接调用原 `SaveTraffic(C)`，不上传 `C-L`，不 reset 核心，不创建新 epoch，不将 C 当作“第一条只建基线”的样本。L 从原 spool 游标读取，P 由面板原事务读取；不把基线复制进交接标记当作第二个权威。
3. C=L 时本地不重复入队；C>L 时本地游标和不可变样本同事务提交。面板收到原队列的 L 及后续 C，最终增量为 C-P。例：P=80、L=100、C=135，面板原待发 20 加新 35，总增量 55；不能再加完整 135。
4. C<L、已见统计源消失、方向不完整、API 归属冲突：明确记账失败，保留原 epoch、游标及 marker，禁止改 epoch/补零恢复成功。认领失败，更新不提交；不能靠重启让错误消失。
5. spool 打开/认证/写入失败：Prepare 不产生成功 detached 回执。已经停止旧 Agent后才发现错误时，核心身份材料保持，控制面报告失败，不产生假基线。修复磁盘后可重试读取原累计值。
6. 旧 Agent 先停发布、核心更新/移除与采样/门控协程，串行取得 runtime 锁；独立 ctx 采每一活核并持久化成功，再同步运行材料、出具 detached 回执。新 Agent 先拿生命周期锁再采样，禁止双写。无需等待远端 Ack 才交接，原 spool 正常补传。
7. 账户批与 node 批当前分别提交；交接不清除其中任何一批。若账户已提交而 node 失败，重试账户不重复，node 用其原游标补入；不得标交接成功直到两者均持久化。旧 v1.7.5 有效批次、编号队列、Ack/隔离状态原样保留。

### 4.2 核心在交接期间退出

| 退出时机 | 记账与恢复动作 |
| --- | --- |
| Prepare 前/最终采样前已退出 | 核验原身份退出，保留已写 spool；记录末次采样到退出间“尾部不可取”，交接失败。按异常核心恢复规则另起新 epoch，不能声称完整结算 |
| detached 后、认领前退出 | 证明原进程退出，按原 run 记录不完整结束；保存失败交接回执后才清生命周期 marker；新核心独立 epoch。旧批仍补传，不把旧累计移到新 epoch |
| 认领验证/第一次 C 写入之间退出 | 重新核验 pidfd；已成功持久化的 C 保留，未提交则不推进游标；返回 core_exited_during_handover。不得返回 adopted/ready |
| 认领成功后退出 | 走现有分核 supervisor 恢复；先记录原 run 退出事实与末次采样位置，再清 marker，重启只生成一次新 epoch |
| 退出状态 unknown | 保留原材料，不清 marker、不出具退出或结算成功、不启替代核心；报告 ErrCoreStopUnconfirmed 及原始原因 |

新增 run 结束事实可放在加密运行材料的 `exit_observed` 状态中，含 identity、last_persisted_at、exit_observed_at、`tail_status=unavailable`；它只记录已知事实，不估算缺失字节，不充当金额/额度结算。记录写失败同样阻止材料清理。长期告警与展示方式见 D2。

即使普通 stop 在 Kill 前完成最终采样，采样到 Kill 之间也可能继续传输；现有核心 API 没有“冻结数据面并原子读最终值”的合同。因此这里的无漏计承诺限于**同一仍活核心累计值覆盖的更新空窗**。若要求所有死亡/停止场景逐字节完整，方案 B 单独不满足，需 Owner 扩大范围。

### 4.3 观测状态、日志与门控

- **计数源集合**：现有 CounterSources.seen 只有内存，重启会忘记“以前见过但现在消失”的源。新增可恢复的源集合快照，绑定原 epoch；成功采样提交后同步元数据，下一次交接前强制同步。崩溃窗口内至少依据 spool 已持久化账户的非零游标检查来源不能缺失，不能仅依赖 possibly-stale seen。零值懒创建源保持现有语义，不为从未出现的源造零。
- **sing-box 旧心跳总量**：认领时根据同一核心原生总量恢复 NodeTrafficTotals 的内存累积视图；持久流量仍走 SampleTraffic 的原累计。禁止把旧 nodeUpload/nodeDownload 加到 C 再发送。新进程才允许按新 epoch 从零计数；同身份计数倒退是错误。
- **Linux 文件输出**：sing-box stdout 改为运行目录内 0600 的 `output.log`（原日志级别与 parser 保持），GOST stderr 改为 `error.log`；传给 exec 的必须是 `*os.File`，不能外包一层 Writer 重新引入 Go pipe。均 O_APPEND，父 Agent 退出只关自己的 FD；GOST 诊断从文件读取原有尾部长度，读取失败保留底层错误。
- **tail 模式分离**：新核心启动允许清理已结束 run 的旧日志；认领模式禁止 truncate 未读取数据。Xray `access_log.go` 拆出 attach/detach，sing-box 使用相同文件 tail 基础设施，parser 仍用 SingboxWriter；关闭本 Agent tail 不意味着核心退出。inode 被替换、文件不安全或不可读明确报错，不能重建空文件冒充连续。
- **读位置与重复访问记录**：在旧 Agent detach 前把当前读取边界和文件 identity 同步到运行材料；新 Agent 从该边界读取空窗内容。访问 collector 分桶/落 spool 与游标提交目前不是一个事务；不得据此承诺访问记录恰好一次，需选定独立交付规则后实现（D3）。严格流量记账不依赖访问日志。
- **解析器交接边界**：共享 tail 的交接 checkpoint 必须停在完整行边界，未完成行保留在原文件，不沿用当前“已读入 parser 就可清空”的规则。sing-box 还需保存同连接多条日志之间的最小 pending 解析状态；仅保存 byte offset 和 sources 会丢掉已读到前半段的连接归属。为 `collector/singbox.go`、`singbox_sources.go` 新增显式 Snapshot/Restore，原时间戳、poisoned/歧义状态一并恢复，不能通过重建 parser洗成完整观测。若涉及原始日志字段持久化，须先按 D3 限定字段与留存。
- **collector 收尾确认**：`internal/agent/collector/runtime.go:66` 的 Wait 目前只等结束，`:88` 的 Run 在 shutdown flush 失败时仅记录日志。新增能返回真实 drain/flush 错误的 `Drain(ctx) error`，旧 tail 停止供给后调用；成功后才提交日志 checkpoint 和 detached 回执。它不自动解决进程在 flush 与 checkpoint 之间崩溃的访问重放问题，该交付语义仍由 D3 决定。
- **sing-box 来源关联**：在日志关联变化时持久化最小来源映射/完整性状态，绑定同一核心 run；恢复后用 Clash 当前活跃连接裁剪。先读取空窗日志，再形成快照；缺失原关联时明确 source unavailable，禁止给所有旧连接算零、把来源猜给用户、或上报 device gate supported。持久化频率与隐私留存属于 D3；若不允许持久化，必须承认活连接归属无法跨更新完整恢复。
- **Xray 动态门控**：Prepare 前显式恢复静态基线路由并验证 gate-free（沿用 `xray/device_gate.go:247`、`:264`）；成功后记录 gate-free 事实。认领先验证路由基线，不能重置 gateDigest 就假定核心 gate-free；无法证明则拒绝认领并报告，不伪装静态摘要一致。新 Agent 不复用旧 TTL 的新起点，等待当前控制会话的新授权。sing-box 清内存 lease 不调用关闭全部连接，恢复观测后等待新授权。
- **输出写失败**：文件无法创建/同步、磁盘满、tail/parser/collector 错误必须经结构化日志和现有 collector/source 状态暴露，Prepare/认领不能报告完整就绪。隐私保护截断若继续采用，必须记录丢弃事实与原因，不能无声补零；具体空窗清理方案待 D3。

## 5. 交接标记与更新事务

### 5.1 格式、位置与权限

区分三种材料，禁止把同名 marker 同时当作“保留授权”和“核心身份”：

| 材料 | 写入方 → 消费方 | 内容与生命周期 |
| --- | --- | --- |
| 原 `core-run.json.enc` | 核心 adapter → 新 Agent/停核屏障 | 身份事实，格式保持 v2；仅确证原进程退出后清理，无 TTL |
| `core-runtime.json.enc` | 核心 adapter → 新 Agent | 加密 active 配置/运行元数据，绑定原 CoreRun，核心存活期间持续有效；与对应 run 一起清理 |
| `handover/request.json`、加密 `receipt.json.enc` | update.sh 创建 request；Agent 写 receipt → 新 Agent、更新器、服务 post-stop | 一次更新的保留授权与状态机，不等于成功应用或流量结算 |

建议请求目录：`/run/proxy-agent-handover/` root 所有 0750、组 proxy-agent；`request.json` root:proxy-agent 0640、单硬链接普通文件，Agent 只读；Agent 回执放 `$DataDirectory/handover/receipt.json.enc`，目录 0700、文件 0600，使用既有节点身份密钥加密认证。自定义 DataDirectory 从本地 config 解析，不在 shell 猜 runtime 路径。新 helper 通过固定 CLI 创建/验证 JSON，shell 不手拼身份与进程事实。`/run` 防止跨 boot 残留授权；持久回执支持断电后的诊断与回滚。

请求 v1 逻辑结构如下（示意字段值，实施不得将示意值当默认）：

```json
{
  "version": 1,
  "kind": "core-handover-request",
  "id": "<随机256位十六进制>",
  "node_id": "<已登记节点>",
  "boot_id": "<当前内核boot UUID>",
  "updater": {"pid": 123, "token": "l12345"},
  "source_agent": {"pid": 456, "token": "l23456"},
  "target_agent_sha256": "<实际将启动镜像的摘要>",
  "transaction_id": "<现有shell快照或受管request id>",
  "scope": "bootstrap",
  "phase": "armed",
  "created_boottime_ns": 1000000000,
  "deadline_boottime_ns": 301000000000
}
```

`scope` 仅允许 bootstrap、worker；bootstrap 的 target 是重启后仍选用的实际 worker（普通模式为直接 Agent），后续换 worker 要新 id/target，不能将引导二进制摘要误当 worker 摘要。具体核心集合由旧 Agent 在 Prepare 时写入认证 receipt，不相信 shell 提供 PID 列表。

receipt v1 含 request 摘要/id、node、完整 OS 环境、源/目标 Agent 身份、事务身份、`state=prepared|detached|adopting|adopted|failed|stopped`、每核 run 身份+runtime 材料摘要+最后成功采样时间+日志边界、失败码与安全的原因。只描述事实，不含用户明文统计或私钥。状态更新原子临时文件、fsync、rename、目录 fsync；请求同样由 root 原子写并同步。

### 5.2 消费、过期与残留

1. 旧 Agent 只对本次 source_agent PID/token、boot、node、活跃 updater、transaction 完全匹配且未过期的 armed 请求执行 Prepare；运行材料与 spool 全部同步才写 detached。请求一旦被 used 的 receipt 绑定，不能再次授权其他进程。
2. 新 Agent 在 lifecycle 锁内将 detached → adopting；验证自身镜像摘要、原活核与材料，并完成第 3、4 节后写 adopted。崩溃停在 adopting 时，后继只允许同一目标镜像恢复同一事务；不得按文件存在直接跳到 adopted。
3. 新 Agent 启动成功不立刻删除历史证据；更新器核实 adopted 与实际版本、核心身份、统计采样、服务稳定，提交现有更新 journal 后撤销 armed 请求并标记事务消费完成。下一次 stop 没有有效新请求，必然停核。
4. 建议授权窗口以 CLOCK_BOOTTIME 计算 300 秒，包含系统挂起时间；只用于本次更新保留/认领授权，不是核心运行 TTL。窗口数值为 D5，不以墙钟时间回拨延长权限。
5. 过期、updater 已退出、target/transaction 不匹配：报告 `core_handover_expired/conflict`，不允许 Detach，不允许当作不断线更新提交。旧 Agent 尚运行则继续服务并拒绝本次更新；已 detached 的核心由当前新 Agent或显式停核屏障核验后停止，再按普通恢复启动。不得删 request 后把活核当不存在。
6. updater SIGKILL 留下的有效/过期请求，在下一次更新、开机恢复和 stop 屏障处显式识别处理；原 run 无法观察时保留全部证据并失败。boot 改变使授权失效，但原 core-run 必须按其自身环境规则解析。
7. `/run` 请求已丢失、持久 detached 回执仍在：不据回执恢复保留授权；同机新 boot 可证明原核心退出，保存诊断后清理。不同 host/namespace 不作退出推断。

### 5.3 update.sh 正常路径：顺序不得颠倒

1. 下载校验、签名/版本/存储检查仍按现有事务；新增固定 CLI 能力探测 `--core-handover-protocol`，只有源实际 Agent、目标实际 Agent、执行更新 helper 都返回明确 v1 才走交接。旧版本无法探测明确记录“首次升级按停核更新”，不伪称支持。新版本探测运行失败与“旧版本不支持”分开报错。
2. 取得现有更新锁；受管模式取得 bootstrap reserve，阻止并发任务。将 Agent 的回滚快照与 prepared journal 提前到停止之前，确保停服前已保存恢复材料和将使用的 helper 镜像。面板事务保持现状。
3. 请求只在所有预检通过、即将 stop 时 armed；必须包含 source 实际进程身份与事务 id。停止前保留新 helper 的不可变校验副本，供后续回滚使用，不能覆盖后才找停核工具。
4. service_stop：主进程 SIGTERM 或父进程关闭 worker stdin；worker Prepare → detached → 退出，父进程确认退出，post-stop 验证本次 detached 才保留核心。停止超时、无 detached、采样失败均中止交接，不继续覆盖文件。
5. 验证每核仍是原身份，安装新引导/服务定义/工具；不得修改活核仍引用的 TLS、规则集、socket 与日志 inode。写 started 后启动新服务，新 Agent认领，写 adopted。仅主 PID 稳定 10 秒不足，须读取认领结果和真实工作版本。
6. 提交 bootstrap journal、撤销本次请求。受管完整升级接着以**新 id**、scope=worker、目标镜像摘要执行 `install_managed_release`；agenthost 在 jobs.MarkStarted 后、第一次 Process.Stop 前 armed 下一次请求。第二次仍保持同一核心 run。
7. `agenthost/jobs.go`、`agentprocess/driver.go` 将 Stop → 安装/Activate → 认领 Ready 纳入原安装事务；任一环节失败必须运行下一节屏障。纯本地受管安装没有 shell 时，由 agenthost 通过受保护本机 helper 准备同格式请求；普通用户控制消息不得直接写 request。
8. `service.sh:252` 首次启用网页升级还有额外 stop/start。对支持交接的新版本，用同一事务协议包住这个初始化；不能让已完成认领的更新在初始化时再断一次。旧版本首次初始化仍明确允许停核，成功与否原样报告。
9. 服务原本未运行：不 armed、不启动，先证明没有遗留活核才更新；有遗留核而无有效管理权，返回明确错误，不能按“inactive”放行覆盖。

### 5.4 回滚、信号恢复与开机恢复

1. 先将交接授权撤销为 rollback，阻止候选退出时再保留。停止候选 Agent/worker并确认其退出，冻结其采样与发布；失败则不启动旧 Agent。
2. 在**覆盖新二进制/服务/helper 之前**，调用步骤 2 保存的新 helper执行 `--finish-core-stop`：验证每个原身份，能读则用原 epoch 最终采样，pidfd Kill，证明原进程退出；将退出/尾部不完整事实同步持久化，再移除相应 core-run/runtime 材料。禁止 `kill -0` 或端口空闲充当退出证明。
3. 停核身份 unknown、权限错误、写盘失败、worker-stop-unconfirmed、退出超时：回滚明确失败，保留新 helper、快照与原材料；不能继续 restore_snapshot 或启动不支持认领的旧 Agent。将 `recover_snapshot:1014` 的“stop 失败仍恢复”在 Agent 分支改为严格屏障，面板分支不扩大改动。
4. 全部核心已证明退出后，才恢复旧引导、服务定义、核心安装器文件与记录，reload 单元，按原 was_active 启旧服务。旧 Agent按旧格式正常恢复，新核心是新 epoch；spool 与旧在途任务保留，不把本次回滚目录覆盖到数据目录。
5. 受管 Installer.Apply 失败的 Driver.Activate(old)、Recover、restartProtected、baseline recovery 同样先过屏障；不能只改 shell 回滚。即使回退镜像也支持认领，建议统一先停核，避免让错误候选的运行材料被另一版本接着解释；回滚断线明确可见。
6. 更新器被信号打断、机器重启：现有 journal 决定恢复方向，交接材料只决定是否还需停核。开机不同 boot 不向旧 PID 发信号；若旧记录来自不同 host/namespace 则阻塞恢复，不因 boot recovery 模式省略身份门禁。

## 6. 失败场景表

“保留核心”只表示不擅自操作身份不明进程，不能宣称数据面已受管或健康。所有错误保留底层原因，日志仅输出白名单元数据，不回显秘密。

| 异常 | 明确行为 | 更新/记账结果 |
| --- | --- | --- |
| core-run/key 认证失败、符号链接、多硬链接、未知版本 | 保留原文，拒绝覆盖/认领/启动替代核 | core_handover_invalid，更新失败 |
| v0/v1 无环境、PID=0 启动窗口、host/ns 不可观察 | 沿用 unknown，拒绝认领和自动清理 | stop/lifecycle unconfirmed；需恢复原事实 |
| 同 host 不同 boot | 原 run 退出已证实；不碰旧 PID，保存结束事实后清理 | 新启动新 epoch；尾部不完整显式记录 |
| PID 已复用/token 不同 | 原核心已退出，绝不向现 PID发信号 | 交接失败，按退出恢复 |
| zombie、pidfd 报退出 | 认定该核心结束；退出码若无法取得明确 unknown | 不把外部退出伪造为 Wait 成功 |
| pidfd 不支持或权限不足 | 不断线能力预检失败，不在中途转裸 PID kill | 返回 unsupported/permission 原因，按 D1 决策处理 |
| runtime 材料缺失/不匹配、Clash secret 无法恢复 | 不生成新 secret强接旧 API，不把错误归类配置变化 | 拒绝认领；只有明确受控停核后可恢复 |
| 新渲染配置/归属/外部依赖摘要不同 | 先校验候选，建立旧统计上下文，按全核正常发布重启 | 记录 config_changed；新进程新 epoch |
| 原生校验或新版本渲染失败 | 不杀原活核，不报告新版本 Ready | 更新失败；进入明确回滚路径 |
| 端口恢复冲突/跨核 guard 集不完整 | Render 前拒绝，不重分配随机端口强行相等 | 更新失败，原流量不重置 |
| 最后采样或 spool 提交失败 | 无 detached 成功回执；游标保持真实提交边界 | 更新失败，保留累计源待重试 |
| 认领首采累计倒退/来源消失/归属变化 | 记账失败，禁止补零、换 epoch掩盖 | 更新不提交，暴露统计源异常 |
| 核心在空窗/认领中退出 | 按 4.2 证明退出并记录尾部不可取 | 交接失败；新 run 不继承旧累计 |
| 活核监听失败一次 | 不算已认领成功；受管 Ready 失败，已运行监督仍保留原三次阈值 | 返回 listener 原因，禁止新增立即全核重启规则 |
| 多核只有部分认领成功 | 不用 Apply 补偿重启健康核；控制面列出失败核 | 整体更新未确认，后续回滚按屏障停核 |
| 请求过期/残留/重复消费/目标镜像不同 | 拒绝保留授权，不按文件存在认领 | 记录 expired/conflict，不假成功 |
| updater 在 detached 后死亡 | 后续恢复先识别事务，证明身份后停核再恢复；unknown 则保留现场 | 不自动删除原 marker |
| worker Stop 超时或仍占锁 | 不覆写镜像，不并发操作核心，不启动旧 worker | worker-stop-unconfirmed |
| SIGTERM 无有效请求 / 普通 stop / 卸载 | Close + 服务 post-stop 停核屏障；全部退出才可删文件 | 失败必须影响 stop/卸载退出状态 |
| Agent 非更新崩溃 | 按 3.3 建议停止遗留核心再恢复 | 允许断线，新 epoch，尾部不完整 |
| file tail 开错 inode/解析或提交失败 | 报 collector/source 错误，不空建日志、不宣称完整观测 | 认领未就绪；隐私清理按已确认政策 |
| Xray gate-free 未证实 | 不把静态配置相同视为可认领 | gate_recovery_required，更新失败 |
| sing-box 活连接无来源映射 | 不猜用户，不补零，不宣称设备门控可用 | 明确观测缺失，按 D3 决定是否阻塞交接 |
| 回滚停核未确认/退出事实写盘失败 | 停在恢复旧镜像之前，保留快照、新 helper、run | rollback_failed；不得启动旧 Agent |
| Windows | 不进入 Linux 交接/新日志路径 | 保持现有行为 |

## 7. 分阶段实施清单与真实核心验证项

按四个审查单元推进，先有可审查的底层能力，最后一起启用脱离。阶段 1～3 不修改系统服务停止策略或 Pdeathsig，避免半成品留下无主活核；各阶段可独立审查，完整不断线验收依赖阶段 4。仅列验证，不在本阶段执行；新增测试及执行验证均须另获 Owner 授权。

### 阶段 1：进程身份、退出广播与运行材料协议

主要文件：新增 `core/process_handle*.go`、`core/handover_material.go`、`core/handover.go`；修改 `core/run_marker.go`、`core/lifetime_process_linux.go`、`core/ports.go`、必要的 `core/runtime.go`。`lifetime.go` 仅在有必要复用安全写入时调整内部实现，保持 v2 内容 ABI；accounting_material/sync 继续负责安全持久化。不改 adapter 启动属性。

- [ ] 固定句柄与 ExitObservation 接口、运行材料字段/状态、安全读写和已占用端口注册；明确 Linux 与 Windows 编译边界。
- [ ] 新增 Linux 原进程身份检查与 pidfd 观测/信号，退出可重复读取，保留 Wait 回收真实子进程的能力。
- [ ] 定义 request/receipt 状态机的 Go 数据合同及失败码，保持旧 core-run、spool、握手格式可读。

真实核心验证项：独立启动三核并通过句柄观测退出；父进程存活与消失、zombie、PID 重用、namespace/boot 不同、pidfd 权限/内核不支持；重复 Stop/Health 不耗尽退出通知；原 v1.7.5 合法保护材料/缺环境标记按旧规则接受与拒绝；磁盘同步失败不产生 active 成功记录。

### 阶段 2：三核运行时接管、记账与输出

主要文件：三核 `adapter.go`、Xray/sing-box `lifetime.go`、stats/uptime/executable/observer/device_gate 等直接调用点；`xray/access_log.go`、`singbox/render.go`、`singbox/node_traffic.go`、`gost/health.go`；新增共享文件 tail；`core/counter_sources.go`、`collector/singbox.go`、`collector/singbox_sources.go`、`collector/runtime.go`。使用阶段 1 接口，原则上不回改阶段 1 文件；接口缺陷单独标注审查。

- [ ] 三核替换 process/done 依赖，补齐 Adopt、Prepare/Detach，与真实新 start/stop 分开。
- [ ] 持久配置输入、secret/端口、统计映射、源集合与最小来源关联；接回原 epoch/Revision，首采使用原累计值。
- [ ] Linux sing-box/GOST 输出接 `*os.File`；Xray attach 不 truncate；Windows 保持管道/Wait 原行为。
- [ ] 明确 gate-free 与来源完整性恢复，不将来源缺失报告为零；处理失败的 active 材料与退出事实。

真实核心验证项：三核长 TCP、UDP 会话及 Xray forward/GOST relay；已建立 sing-box 会话跨接管来源归属；Xray 动态门控跨交接；用户 API 与官方无用户 API 的 sing-box 两种版本；账户/节点 C=P、C>L、重复补传、方向倒退、源消失、账户成功但 node 提交失败；输出文件 FD 在 Agent 退出后仍可写、附加不清空、inode 变更/磁盘满明确失败；Windows 原生命周期及输出行为回归（单列，不作为 Linux 真实核心证据）。

### 阶段 3：启动恢复与多核事务接线

主要文件：`internal/agent/reconcile/reconcile.go`、必要的新增 `reconcile/handover.go`，`core/multi.go`、`core/multi_telemetry.go`，`control/control.go`、`supervisor/supervisor.go`。普通 Apply/发布与手工 Rollback 原语义保持。

- [ ] RestoreStartup 在 Render 前恢复运行输入/端口；全一致时认领，真实差异走现有全核发布；未知/损坏明确阻塞。
- [ ] 恢复 MultiRuntime current/expected/history 与 renderedDigest；认领失败不误触 Apply 的重启补偿。
- [ ] 按真实状态上报控制面、保留 startup 错误链；新目标版本只有采样/日志/门控就绪才发 Ready。
- [ ] 枚举禁用核遗留 marker；协调 inventory/核心安装 hold、host-network、时间窗及 supervisor，阻止并发认领/发布/更新。

真实核心验证项：单核/混合三核/纯 GOST/无活核节点；一致配置接管后首次 Refresh 不重启；跨时间窗、过期健康事实、证书内容同路径变化、规则集变化、新版本渲染变化触发明确全核发布；一核 unknown 不杀健康同伴；核心安装 switching/rolling_back/failed_closed、成功回执异常、禁用核残留；认领后单核崩溃仅恢复异常核；原发布全部重启语义仍成立。

### 阶段 4：更新授权、普通停止屏障与 Linux 脱离启用

主要文件：`cmd/agent/main.go`、`cmd/agent/managed.go`，新增固定本机 handover/stop CLI 实现；`agentprocess/process.go`、`driver.go`，`agenthost/jobs.go`、`host.go`、`recovery.go`、`local_bootstrap.go` 相关本地操作；`deploy/linux/update.sh`、`service.sh`、`uninstall.sh`、必要的 `install.sh` 发布/快照条目；systemd/OpenRC Agent 定义与恢复钩子；`core/procattr_linux.go`。控制层只做阶段 3 预留的退出模式接线，尽量不重复改恢复主体。

- [ ] updater armed→detached→adopted→提交，分别覆盖引导、worker、首次受管初始化；提供独立能力探测，旧协议不加字段。
- [ ] stop/卸载、shell 与受管回滚、信号中断与开机恢复接入同一停核屏障；新 helper 校验副本在覆写前保存。
- [ ] 同时启用 Setsid、KillMode=process、OpenRC 关 cgroup 清理；post-stop 先证实 worker 退出再判授权/停核。
- [ ] 按最终决定同步 `docs/project-spec.md:387`、`:402`、`:443`、`docs/architecture.md:110`、部署/运维/发布手册对应规则，记录首次升级断线与异常退出计量边界。

真实核心验证项：Debian/Ubuntu systemd、Alpine/OpenRC 各跑完整网页/终端更新；引导与 worker 两次切换均保留三核 PID/token/epoch/既有 TCP/UDP 会话；首次从 v1.7.5 升级允许断线且有效数据/在途任务完整；直接 Agent与已受管节点；普通 stop、restart、SIGINT、EOF、超时 Kill、卸载/清除数据；候选启动失败、认领失败、回滚到不支持认领的旧 worker/引导；每个事务节点 updater SIGTERM/SIGKILL 与主机断电；过期/残留/重放请求；stop 屏障失败禁止文件覆盖和账号删除；systemd 残留 cgroup、PrivateTmp、服务重启前后 FD/文件可见性与 OpenRC 无 cgroup 环境；低位端口 capability、GODEBUG 与 sandbox 权限保持。

所有真实记账验证应同时保存：核心原生 C、Agent 原 spool 游标/待发批、面板 P 与最终增量，不凭 UI 月流量曲线判断完整性。连接持续验证使用升级前建立、升级中持续收发的同一连接，不能用升级后新拨通代替。

## 8. Owner 待决问题

| 编号 | 需要决定的事项 | 建议与影响 |
| --- | --- | --- |
| D1 | pidfd 与现有 Linux 主机兼容底线 | 建议交接要求内核支持 pidfd_open/pidfd_send_signal 及所需权限，预检明确报告；旧内核需决定保留“明确选择停核更新”的入口，还是另做等价安全进程控制。仅 `/proc` 轮询能看退出，验证后再裸 PID kill 仍有复用竞态，不能声称同等安全。不默认把所有 v1.7.5 有效安装拒之门外 |
| D2 | “不漏计”的承诺是否限定为正常存活交接 | 建议限定，并将原核心死亡后的尾部标为不完整，允许现有异常核心自动恢复；若要求所有死亡场景绝对无损，需另立独立持久计量项目。还需决定不完整事实只进本机运维记录，还是增加面板持久告警；后者扩大接口/展示范围 |
| D3 | 空窗日志隐私/空间与来源映射的持久化政策 | 文件在 Agent不在时无人清理，现有约 100 ms 留存合同必须有明确替代。可选接受明示的更新窗口留存并规定故障人工处理，或授权独立日志清理/采集进程；后者扩大方案。还需决定访问记录跨崩溃的交付语义、sing-box 最小来源关联的加密持久化/清理周期及缺失是否阻塞交接。未决定前不写死“访问连续且恰好一次” |
| D4 | 非更新崩溃是否也要保留/认领核心 | 本文建议遵守“只有更新保留”：无 detached 回执走 stop 屏障再恢复，允许断线。若要崩溃也不断线，需改变 post-stop 授权规则，并补全崩溃窗口内日志/门控/来源与计量恢复；这是行为范围变化 |
| D5 | 授权窗口与无主交接的处置时限 | 本文提出 300 秒 CLOCK_BOOTTIME 授权；是否满足当前安装/启用受管模式耗时需真实验证。过期不再认领，按显式停核恢复；若要求 updater 消失后在固定时限内自动停核/清原始日志，必须有 Agent之外的执行者，不能只写过期时间假定有人执行 |

### 8.1 Owner 决定（2026-11-22）

- D1：交接要求 pidfd_open/pidfd_send_signal；不支持时明确记录并按停核更新，不提供裸 PID 替代。
- D2：“不漏计”只承诺正常存活交接；核心在空窗内死亡的尾部只写本机日志（结构化 WARN），不新增面板告警/接口。
- D3：接受更新窗口内访问日志在文件中积压，新 Agent 从交接 checkpoint 继续读；**不持久化 sing-box 来源映射**，交接前已建立连接的来源标记为 unavailable，不阻塞交接；访问记录（非流量）在空窗内允许少量重复或缺失，不做恰好一次。因此 4.3 中 sing-box 来源快照/恢复、parser pending 状态持久化不实施；Xray/sing-box tail checkpoint 只需完整行边界。
- D4：只有更新保留核心；非更新崩溃走停核屏障后正常恢复。
- D5：授权窗口 300 秒 CLOCK_BOOTTIME；过期按显式停核恢复。
- 验证：每阶段只做编译/静态检查；阶段 4 完成后在测试机（t1/t3）做真实核心验证，然后按发布手册门禁发布。

实施前的其余设计选择：CoreRun v2 保持、配置元数据用独立加密伴随文件；配置差异沿用全核发布；回滚统一停核后再切旧镜像；新增固定本机停核命令及服务 post-stop 钩子。现行合同已同步至项目说明书、架构、部署与运维文档。

设计阶段检查：已读取并交叉核对相关生命周期、适配器、记账上下游、受管进程和部署恢复路径；该阶段未运行测试、构建或真实核心验证。

## 9. 实施与验收结果（2026-11-22）

已实施 Linux 更新授权、核心脱离与认领，以及 systemd/OpenRC 停核屏障，目标版本为 1.10.0。以下为测试环境的既有验收记录；本节文档同步未重新执行测试，也不代表生产已升级。

### 9.1 环境与连接验证方法

面板使用本地测试机测试面板；受管节点包括 Alpine/OpenRC 的 `v190-legacy`（GOST）、`v190-zero`（Xray + GOST，带用户 SOCKS 入站），以及 Debian 12 systemd 的 `hodeb`（systemd-nspawn 容器，GOST）。每次升级期间保持升级前建立的同一条 TCP 长连接，每 100 ms 发送 1 KiB 并核对回显，持续 150 秒。

| 节点与升级 | 连接与进程结果 | 认领与流量结果 |
| --- | --- | --- |
| legacy：1.9.95→1.9.97、1.9.97→1.9.98 | GOST PID 不变；每轮每方向 1497 行 / 1,532,928 字节，零错误，最大间隔 0.10 s | 引导程序与 worker 两次切换都认领；1.9.98 轮节点流量增量上下行各 1,532,928 字节，与连接收发完全一致 |
| zero：1.9.98→1.9.99 | Xray 与 GOST PID 均不变；两条长连接各每方向 1,532,928 字节，零错误 | 用户 `v190-proof` 流量增量 3,065,856（上下行之和）；节点流量上下行增量各 3,065,856（GOST + Xray 两条连接之和） |
| hodeb（systemd）：1.9.101→1.9.102 | GOST PID 不变；150 秒零错误 | `KillMode=process` 与 `ExecStopPost` 屏障生效 |

追加验证（2026-11-23）：legacy 更新至 1.9.103 时 GOST PID 保持 16927，150 秒连接每方向 1,532,928 字节、零错误；随后同时 SIGKILL 引导程序与 worker，旧核心退出，自动恢复为单份新核心 19351，恢复后 3 秒回显每方向 30,720 字节、零错误。该轮验证针对 OpenRC 自动恢复修复；后续清理与 pidfd 分支修复另由发布提交门禁验证。

未授权路径也已验收：systemd 执行 `systemctl stop` 后无核心残留，`Result=success`，运行标记清除；start/restart 后核心为新进程且只有一份。OpenRC 的 `rc-service stop/start/restart` 结果相同。

### 9.2 验收中发现并已修复的问题

| 问题 | 修复 |
| --- | --- |
| 引导程序以 `proxy-agent` 用户核对授权时，读取 root 更新进程的 `/proc/<pid>/ns/pid` 被拒，授权总失败 | 此处只核对 PID + 启动时间 |
| 授权核对失败时静默改为无授权停止 | 记录 WARN，并给出字段级原因 |
| `update.sh` 在认领后立即查询实际 worker 摘要，早于引导程序本地套接字就绪 | 先等服务稳定，再重试查询，等待不超过 30 秒 |
| GOST 计数源要求上下行成对，但 GOST 按方向懒创建 | 改为按出现记录 |
| 每 5 秒采样都写盘运行材料 | 只有计数源集合变化时写盘，交接准备时写最终材料 |
| root 停核屏障写出的回执归 root，Agent 读不了 | root 写入时沿用目录属主 |
| 受管 worker 只接受 worker 作用域授权，导致引导程序更新仍停核 | 受管 worker 接受 bootstrap 与 worker 两种作用域 |
| sing-box 将 WebSocket 传输的 `path` 当成本地依赖文件 | 依照已有配置类型只核对 TLS 证书、私钥与本地规则集 |
| OpenRC 自动 respawn 不执行 `stop_post`，遗留核心阻塞恢复 | 新 worker 在 lifecycle 锁内先执行同一已认证停核屏障，再恢复配置；有效更新交接仍走认领 |
| Bind 后、运行材料激活前崩溃，prepared 材料与已绑定 marker 不同 | 停核路径单独匹配此已认证启动状态；认领仍须完整一致 |
| 停核清理先删 marker，崩溃后残留孤立运行材料 | 确认退出后先删伴随材料，再删 marker |
| pidfd 不可用时 arm 直接中止更新 | 明确记录原因并按 D1 进入无授权停核更新，其余授权失败继续中止 |

### 9.3 测试环境限制与未实测项

- systemd-nspawn 给容器伪造 `boot_id`，服务的挂载命名空间却看到宿主真实 `boot_id`，导致授权环境不一致；验收中卸下 nspawn 的伪造挂载后通过。普通 VM 只有一个 `boot_id`，不受影响；Debian LXC 下未实测。
- sing-box 无测试节点，未做真机交接，仅有单元与真实核心测试。
- 第一次升级到本版本由旧 Agent 执行，实测会断线，符合预期。
