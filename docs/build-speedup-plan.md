# 构建与门禁提速规划（先于 v1.5.4）

> **状态：第一步与第二步已实施并合入 `main`（2026-11-12，`69f2fec`），记录见第 8 节。** Owner 决定单独成批、先于 [v1.5.4](v1.5.4-plan.md) 实施。本文取代 2026-11-09 的[提速方案 A](build-acceleration-plan.md)（其测试库模板一条保留并按实测更新，证书复用与前端拆分不再采用）。当前行为以[项目说明书](../AGENTS.md)与源码为准。

## 1. 实测：时间花在哪里

以 v1.5.3 的发布门禁（提交 `d1303f1`，测试机 10 核 7 GiB）为准：

| 环节 | 耗时 | 原因 |
| --- | --- | --- |
| 本地完整门禁 | 3073 秒（约 51 分钟） | `scripts/check.sh`（2403 秒）跑完后真实核套件（670 秒）才开始，两段串行（`~/.qui-local/gate.sh`） |
| check 中的 `go test -race ./...` | 约 34 分钟 | Go 并行跑各个包，整段由最慢的包决定：`internal/platform/server` 2026 秒 |
| 其余慢包 | `system/backup` 1336、`proxy` 1011、`system` 909、`deployment` 802、`node` 713、`recovery` 632 秒 | 同样的结构 |
| `server` 包本身 | 174 个测试、0 个 `t.Parallel()`，平均每个 11.6 秒 | 串行执行，10 个核基本只用 1 个；只有 `recovery` 包用了并行（40 个测试中 37 个） |
| 每个测试的初始化 | 从空库执行 142 个迁移：普通 0.15 秒，`-race` 下 **4.58 秒**；复制迁移好的模板库（1.6 MB）再打开：**0.063 秒**；`server.New` 其余初始化 0.2 秒 | SQLite 驱动 modernc 是 C 翻译成的纯 Go 代码，`-race` 给每次内存访问插桩，慢约 30 倍；测试里 91 个文件、125 处从空库打开，`server` 包的 91 处经由同一个 `testServer` |
| GitHub `check` | 50–73 分钟（v1.5.3 为 73 分钟） | 同样的测试，runner 核数更少；`cores` 任务 25 分钟，与 `check` 并行 |

不建议为提速换架构：换成 cgo 的 SQLite 驱动在 `-race` 下会快很多，但会失去纯 Go 静态单文件、交叉编译 arm64 与 Alpine 的简单性；拆服务或换语言违背第 3 节的轻量原则。

## 2. 第一步：低风险、收益最大

### 2.1 测试数据库模板

- 新增测试辅助包（例如 `internal/testutil/dbtest`）：每个测试进程用 `sync.Once` 调一次真实的 `db.Open` 生成迁移好的模板库，之后每次「打开测试库」都把模板文件复制到测试自己的临时目录，再用 `db.Open` 打开（已迁移的库只核对校验和，不重跑迁移）。
- 替换测试里从空库打开的 125 处（`testServer` 一处即覆盖 `server` 包 91 处），机械改动。
- **保留真实迁移路径的测试**：`migrations/` 包、`internal/platform/db`、备份恢复与升级相关的测试（`backup`、`recovery` 里回退夹具、旧归档、升级路径的用例）继续从空库执行，不用模板；判断标准是「测试的对象就是迁移或打开流程本身」。
- 生产代码（`db.Open`、`db.Migrate`、校验和检查）一行不改；模板只存在于测试进程的临时目录，不进仓库。
- 预期：`server` 包每个测试省约 4.5 秒，174 个约省 13 分钟（2026 → 约 1250 秒）；其他慢包按各自打开次数同比下降。

### 2.2 本地门禁两段并行

- `~/.qui-local/gate.sh`（Owner 本机脚本，不在仓库）把 `check` 与真实核套件改为同时启动，各写各的日志，最后汇总；真实核测试用自己的端口与目录，与 check 不冲突（实施时用一次完整门禁确认）。
- [发布流程手册](release-runbook.md)第 4 节与 [部署与升级](deployment.md)第 2 节同步说明。
- 预期：省掉真实核套件的约 11 分钟串行等待。

### 2.3 GitHub `check` 分片

- `.github/workflows/check.yml` 的 `check` 任务改为按包分组的矩阵（3–4 片，按上次耗时均衡：`server` 单独一片，`backup`＋`recovery`、`proxy`＋`system`、其余各一片），前端构建、自测、部署脚本测试放在其中一片。
- 发布任务要求「同一提交有名为 `check` 且成功的任务」（`check.yml` 第 286–301 行）：保留一个名为 `check` 的汇总任务，依赖全部分片、全部成功才成功，发布条件不变。
- `scripts/check.sh` 增加按分片运行 `go test -race` 的参数，本地不带参数时行为不变；纯前端补丁的短门禁判定（`scripts/gate-scope.sh`）不变。
- 预期：GitHub `check` 墙钟时间降到 20–30 分钟。

## 3. 第二步：给慢包加并行

- 按包逐个给 `server`、`backup`、`proxy`、`system`、`deployment`、`node` 的测试加 `t.Parallel()`。先排查共享状态，不安全的保留串行：
  - `t.Setenv`、修改包级变量或测试钩子（时间函数、全局注册表）、改工作目录的测试不能并行；
  - 固定端口、固定路径、共享的临时文件改为 `127.0.0.1:0` 与 `t.TempDir()`；
  - 依赖执行顺序或全局计数的断言改为各自独立。
- 每改完一个包：本地 `go test -race -count=5` 连跑确认没有偶发失败，再进入下一个包。
- 预期：`server` 包在第一步之后再缩短数倍；本地完整门禁有望压到 15 分钟以内。风险是排查不全引入偶发失败，所以逐包推进、逐包验证。

## 4. 不在本批

- 只对涉及并发的包跑 `-race`、让本地与 GitHub 两条门禁分工（一边 `-race`、一边构建加普通测试）：都牵涉检测范围或发布规则的取舍，待 Owner 另行决定。
- 测试证书与密钥复用（`server.New` 实测 0.2 秒，收益小）、前端 `build:fast` 与开发代理（原方案 A 阶段 1–2，不影响门禁时长）。

## 5. 版本与发布

本批只改测试辅助代码、测试文件、门禁脚本与 CI 配置，产品行为不变，**不发 GitHub Release**：改动合入 `main` 后用一次完整门禁与 GitHub `check` 确认通过即可，v1.5.4 的编号仍留给功能版。若 Owner 希望单独打版本号，可改为 v1.5.4 并把功能版顺延为 v1.5.5。

## 6. 工作量估计

| 部分 | 开发与评审 | 验证 |
| --- | --- | --- |
| 2.1 测试数据库模板 | 2–3 小时（辅助包＋125 处替换＋区分保留真实迁移的用例） | 一轮完整门禁 |
| 2.2 本地门禁并行 | 约 30 分钟 | 与上面同一轮门禁 |
| 2.3 GitHub 分片 | 1–2 小时 | 1–2 次 GitHub `check`（调分片均衡） |
| 第一步合计 | 约半天 | |
| 第三节 测试并行（6 个包、约 620 个测试） | 6–8 小时（排查共享状态、改端口与目录） | 每包 `-count=5` 连跑，加一轮完整门禁，共 3–4 小时 |
| 第二步合计 | 约 1–1.5 天 | |

总计约 2 个工作日。第一步完成后门禁就会明显变快，第二步的验证也因此更快。

## 7. 验收

- 本地完整门禁与 GitHub `check` 在同一提交上通过，耗时记录进本文：第一步后本地门禁目标 ≤ 25 分钟、GitHub `check` ≤ 30 分钟；第二步后本地门禁目标 ≤ 15 分钟。
- 迁移、备份恢复与升级类测试仍从空库执行（列出保留的用例清单）；`migrations/checksums_test.go` 不变。
- 第二步每个包 `-race -count=5` 连跑无失败；门禁中无新增偶发失败。

## 8. 实施记录（2026-11-12）

第一步与第二步已于 2026-11-12 经 Owner 同意合入 `main`（`69f2fec`，代码与门禁过的 `49946b2` 相同，只多了 v1.5.4 规划文档），未发版（第 5 节）。

### 8.1 第一步

- **2.1 测试数据库模板**：新增 `internal/testutil/dbtest`（`Open` 与 `db.Open` 同签名；目标文件不存在时先复制按迁移内容哈希命名、原子发布在系统临时目录的模板库，已存在的文件直接打开）及其测试 `TestOpenMatchesFreshMigration`。90 个测试文件改用 `dbtest.Open`；仍从空库执行迁移的是 `migrations` 包（自带迁移执行器，`certificates_test.go` 仍用 `db.Open`）与 `internal/platform/db`。备份恢复、升级与启动类测试里被测的生产代码（`recovery.InstallAndPrepare`、`runWithContext` 等）照旧自己调用 `db.Open` 与迁移，模板只替换测试准备与检查用的打开，所以这些路径仍被真实执行。`migrations/checksums_test.go` 未改。全量 `-race` 实测（单独运行，非门禁）：`server` 2026 → 888 秒、`backup` 1336 → 725、`proxy` 1011 → 71、`deployment` 802 → 29、`node` 713 → 53、`system` 909 → 608、`recovery` 632 → 435；首轮另有 3 个包失败，原因是临时脚本构建 Agent 夹具时漏了版本参数，按 `check.sh` 的方式重建后通过。
- **2.2 本地门禁两段并行**：`~/.qui-local/gate.sh` 同时启动 check 与真实核套件。真实核在副本 `/root/qui-gate/<短 SHA>.cores/` 里先自建前端再测（`internal/platform/recovery` 嵌入 `web/dist`，与 check 同目录会在 Vite 清空 `dist` 时编译失败；GitHub `cores` 任务同样先建前端），两段日志结束后按 check、cores 的顺序并入主日志，`gate-status.sh` 运行中显示两段各自的进度，`gate-rerun.sh` 不变。原版备份 `gate.sh.bak-2026-11-12`。
- **2.3 GitHub `check` 分片**：`scripts/check.sh` 新增 `QUI_CHECK_SHARD`（不设＝全部，本地门禁不变）：`server`、`backup`（`system/backup`＋`recovery`）、`system`（`panel/system`、`agent/control`、`agent/updater`、`panelhost`、`auth`）三片只跑 `go test -race`，`rest` 跑其余 77 个包、前端、`go vet`、构建与部署脚本测试；`rest` 的包列表由 `go list` 减去前三片得到，列表为空或数目不对时失败。`check.yml` 的 `check` 改为四片矩阵 `check-shard`，另设名为 `check` 的汇总任务，四片全部成功才成功，`release` 的判定不变。纯前端补丁时三个 Go 分片直接通过。
- **门禁**：提交 `80fb85b`（2.1–2.3）本地完整门禁 `GATE 80fb85b PASS total=1422s`（v1.5.3 为 3073 秒），check 1422 秒、真实核 686 秒同时进行，无 DATA RACE。

### 8.2 第二步：测试并行

- `server`、`system/backup`、`system`、`agent/control`、`auth`、`panelhost` 的顶层测试加 `t.Parallel()`（`recovery` 原本就并行）。13 个测试保留串行，并在函数开头写明原因（`// Not parallel: …`）：改包级 `Version` 的 4 个安装测试、替换 `control.NewRuntime` 的 2 个、占用 `tuningSlot` 的 1 个，以及在并行负载下截止时间失败的 6 个（读池延迟、配额失效草稿、重连能力撤回、TCP 探测在线判定、真实备份调度、备份子进程崩溃恢复）。排查过：无 `t.Setenv`／`Chdir`，包级变量只有上述两处，固定端口只出现在不监听的配置里，测试证书与模板只读共享。
- `scripts/check.sh` 的 `go test -race` 加 `-parallel 3`：默认并行度（GOMAXPROCS）下几个慢包同时运行约 60 个 race 测试（10 核），等待截止时间的测试成批失败；限为每包 3 个后这类失败消失。
- 修复测试辅助的竞态：配额夹具在自动修订刚出现在列表里时就上报应用结果，而配额发布器先建修订、后写 `deployment_targets`，负载高时 `Result` 返回 `sql: no rows`。新增 `quotaFixture.report`，等到目标行出现再上报（`Result` 在写入前就返回该错误，重复的成功结果幂等）。该竞态在原串行门禁中也存在，只是负载低时很少触发。
- 连跑验证（测试机，`-race -count=5`，多个慢包同时运行）：第 1、2 轮用默认并行度，失败的都是截止时间类测试；第 3 轮 `-parallel 3`（6 个包＋`recovery`）只剩配额夹具的 `no rows`；第 4 轮修好 `apply` 后又在直接上报回滚的测试里出现一次，改为统一走 `report`；第 5 轮又出现一次重连测试被拒「node already connected」（面板先写离线、再做数据库写入与通知、最后才从已连接集合移除旧会话，测试在这段间隙里重连），改为像真实 Agent 一样遇到 `AlreadyExists` 重试，产品代码不变；第 6 轮（`server`、`backup`、`system`、`recovery` 同时 `-count=5`）全部通过，`control`、`auth`、`panelhost` 在第 1–3 轮各 5 次均通过。第 6 轮每次运行的耗时（5 次合计 ÷ 5，含其他慢包的竞争）：`server` 约 460 秒、`backup` 约 330、`system` 约 395、`recovery` 约 607；`control` 约 150（第 3 轮）。
- **门禁**：提交 `49946b2`（第一步＋第二步）本地完整门禁 `GATE 49946b2 PASS total=977s`，无 DATA RACE；check 977 秒、真实核 342 秒（原 686 秒，真实核测试同样并行）。最慢的包：`recovery` 653 秒、`server` 591、`system` 420、`backup` 364。

### 8.3 与验收目标对照

| 目标（第 7 节） | 结果 |
| --- | --- |
| 第一步后本地完整门禁 ≤ 25 分钟 | 1422 秒（约 24 分钟），达到 |
| GitHub `check` ≤ 30 分钟 | `69f2fec` 的 run 43227991439（含 cores）全部通过：四片 `check (server)` 约 7 分钟、`check (rest)` 约 8、`check (system)` 约 9.5、`check (backup)` 约 12，汇总 `check` 13:44:02Z 成功，`cores` 约 13.7 分钟（原 25 分钟）；整次运行约 14 分钟（原 50–73 分钟），达到 |
| 第二步后本地完整门禁 ≤ 15 分钟 | 977 秒（约 16 分钟），**略超**（同一代码在 `69f2fec` 上为 `GATE 69f2fec PASS total=1258s`，各包耗时相近，多出的时间在 npm 安装等非测试步骤）：`recovery` 原本就并行，默认并行度下 435 秒，受 `-parallel 3` 限制变为 653 秒，成为最慢的包。可另行把 `recovery` 单独以较高并行度运行，需再做一轮连跑验证 |
| 迁移、备份恢复与升级类测试仍从空库执行 | 见 8.1 |
| 每个包 `-race -count=5` 无失败、门禁无新增偶发失败 | 第 6 轮与上表各包通过；修好 2 处测试竞态 |
