# B3 方案 B：数据库备份、比较与预检下沉到 Go 实施计划

> **执行方式：** 使用 `executing-plans` 在当前工作区实施，不开启子代理。Owner 后续明确要求 B1 与本项一并实现，并允许相关回归和最窄检查；未提交、推送或发布。实施前候选清单保留，当前实际交付与验证见第 7 节。

**Goal：** 将 `update.sh` 的数据库复制与逐字节比较、备份预检文件操作下沉到 Go，保持面板更新、降级和中断恢复的既有结果与安全边界。

**Architecture：** 保留 Bash 更新入口和恢复状态机，新增独立小工具 `qui-update-db`，由 Bash 调用 `copy`、`compare`、`preflight` 子命令。每次涉及面板数据库的更新固定工具副本，并随面板快照持久化，恢复时只使用快照里的同一副本。目标数据库能否打开仍由目标版本的 `proxy-panel --check-database` 判定。

**Tech Stack：** Go（版本沿用 `go.mod`）、现有 `golang.org/x/sys/unix`、Bash、Linux；不引入新依赖或数据库迁移。

**Spec：** [B3 已选方案 B](../specs/2026-11-23-updater-go-design.md)、[面板更新磁盘开销设计](../specs/2026-11-23-panel-update-io-design.md)、[部署与升级 §5](../../deployment.md#5-升级)。依据提交 `d3ca9fd`；下述具体工具与接口是本计划提出的实施选择。

## 全局约束与授权

- 最低兼容 **v1.7.5**，保留有效数据、备份、旧快照及在途任务；迁移头保持 **0163**。
- 本阶段只处理数据库文件链路；不改服务启停、Agent 核心交接、锁顺序、journal v1、恢复阶段、健康检查或网页 JSON 事件合同，不迁移 journal 与事件上报。
- 最终备份仍在停服后取得；主库与 WAL 均独立复制、逐字节比较、设置属主和 0600、同步文件后发布，最后同步目标目录。不能用摘要、文件大小或修改时间代替字节比较。
- 配置派生源文件继续以面板账号权限读取；只有 Owner 的 `--database` 已冻结到 root 私有暂存区的输入允许 root 读取。不以 root 直接读取配置控制的主库或 WAL。
- root 的临时文件、检查硬链接和递归清理仍限于登记目录；向面板账号目录发布时必须固定目录句柄，保留同文件系统要求。
- 无新增静默兜底：新工具缺失、协议不匹配、无法执行、复制、比较、同步或清理失败，均明确失败；不回退到 Bash 复制，不跳过检查，不返回占位成功。
- `AGENTS.md` 第 4 节的测试授权已由 Owner 明确给出：“允许相关回归和最窄检查”。仅执行本项与 B1 直接涉及的检查；未执行完整发布门禁。
- 后续只运行改动直接涉及的最窄检查，通过后不重复；未获新的提交、推送、发布或生产授权时不得执行这些操作。

## 1. 当前调用链与方案选择

当前 `deploy/linux/update.sh` 的相关入口：

| 函数 | 当前职责与调用方 | 本阶段处理 |
| --- | --- | --- |
| `copy_database_atomic`（约 593 行） | 主库/WAL 复制、再次读源比较、属主/权限、同步、固定目录内发布；用于 `backup_database`、`preserve_database`、`restore_database` | Go 接管文件操作，Bash 保留原函数签名作薄包装 |
| `compare_database_source`（约 698 行） | 面板账号读源并与备份比较，处理 `cat`/`cmp` 的管道退出码 | Go 接管；保留 0 相同、1 不同、2 错误 |
| `database_differs` | 综合主库/WAL 差异，决定 prepared 阶段是否需要恢复数据库 | 保留在 Bash，继续消费上述比较结果 |
| `preflight_backup`（约 1403 行） | root `check.*` 中链接备份、调用预检、清理 | 文件操作、检查进程等待与结果收集移入 Go；Bash 保留当前目录登记，供信号清理使用 |
| `preflight_database` | 准备 root 可执行检查程序并解释目标版本的检查退出码；也用于停服前的 `--database` 检查 | 保留唯一一份目标结果映射；增加调用 Go 备份预检的分支，直接检查暂存库的路径保持原样 |

本计划选择**独立数据库工具**，不完整重写更新器。比较过的其他落点：

- 放进 `proxy-panel` 新命令：发布包简单，但同机只升级 Agent 会先更新共享脚本，面板可能仍旧；恢复又先恢复旧程序再操作数据库，会引入对面板版本的依赖。
- 直接扩展为完整 `qui-updater`：同时涉及服务、journal 和更新流程，超出本阶段。

独立工具仅服务这三类数据库操作，不加载面板配置、不启动服务、不内置迁移集合、不提供任意命令执行接口。增加构建、安装和快照文件是保证旧版降级与恢复可用的必要工作，不把工具演变为第二套更新状态机。

## 2. 工具与函数合同

### 2.1 CLI

归档内路径：`bin/qui-update-db`。安装路径：`/usr/local/lib/proxycontrol/qui-update-db`，root:root 0755；运行和恢复副本为 root:root 0500。

```text
qui-update-db --protocol
  stdout: qui-update-database-v1\n；退出 0
qui-update-db --version
  stdout: qui-update-db <构建版本>\n；退出 0

qui-update-db copy --registry DIR --account USER --source FILE --destination FILE
                   [--restore] [--owner-staged]
qui-update-db compare --account USER --source FILE --backup FILE
qui-update-db preflight --check-directory DIR --backup FILE --checker FILE
```

- `copy`：成功 0、失败 1；stdout 为空，错误及底层原因写 stderr。`--restore` 清理目标旧 WAL/SHM 后按既有顺序发布；`--owner-staged` 只由 Bash 在源等于已冻结的 `$staged_database` 时传入，不用于普通备份或恢复备份。
- `compare`：相同 0、不同 1、打开/读取/权限/子进程错误 2；stdout 为空，错误写 stderr。参数错误也是 2，并有明确诊断。
- `preflight`：完整完成检查及清理时退出 **0**，stdout 首行固定为 `checker-exit: <十进制状态码>\n`，随后逐字节附上目标检查程序合并的 stdout/stderr。目标非零结果不等于工具执行失败；Bash 去掉首行后沿用唯一一份目标结果映射。工具自身的参数、链接、清理或进程管理错误退出 **70**；错误和已收集的目标报告写 stderr，不能因清理失败被覆盖丢失。
- Bash 先检查 `preflight` 的工具退出码，任何非零值（含工具根本没启动时 shell 的 126/127）均拒绝更新；仅工具退出 0 才解析固定首行，要求状态为合法的 0～255 进程状态，格式不符也拒绝。这样不会把工具缺失当成旧目标不支持检查。
- `preflight_database` 对外仍返回 0 可用、2 既有“不支持检查”分支、3 目标不认识迁移、4 拒绝。原有检查程序准备及旧目标结果映射集中保留，不新增跳过条件；本轮不顺手清理旧兼容分支。被检查目标的 ENOENT 映射为 127，EACCES/ENOEXEC 映射为 126，报告保留原始错误，再由既有目标结果映射处理；其他启动错误拒绝，不按未知旧版猜测。
- 普通 `--check-database FILE` 的参数、报告和退出码 **0/3/4/5** 不变；不改 `cmd/panel` 和 `db.Inspect` 的兼容判定。
- 不增加文件大小限制、超时上限、重试或默认成功。读写和进程操作支持 context 取消；INT/TERM/HUP 必须停止子进程并等待退出，再清理私有文件，取消不得被解释为兼容成功。

### 2.2 Go 边界

新增 `internal/platform/hostupdatedb`，只导出以下必要接口；具体小型内部函数由实现者组织，不提供通用插件、命令执行器或恢复框架。

```go
type CopyOptions struct {
    Registry, Account, Source, Destination string
    Restore, OwnerStaged bool
}
type PreflightOptions struct {
    CheckDirectory, Backup, Checker string
}
type CheckResult struct {
    ExitCode int
    Output []byte
}
func Copy(ctx context.Context, options CopyOptions) error
func Compare(ctx context.Context, account, source, backup string) (equal bool, err error)
func Preflight(ctx context.Context, options PreflightOptions) (CheckResult, error)
```

CLI 负责参数边界与退出码转换；库负责文件和进程操作。`Preflight` 的 `CheckResult` 保留目标报告与状态，清理失败时同时返回结果和错误，CLI 不能丢弃已有报告。生产代码不接受测试根目录环境变量，路径由现有 Bash 的 `QUI_UPDATE_TEST_ROOT` 机制展开后显式传入。Linux 文件操作放在平台文件；为现有 Windows 开发编译保留一个明确返回“仅支持 Linux”的入口，不实现 Windows 更新逻辑或模拟成功。

### 2.3 文件与读取规则

- 读取普通源继续调用 `runuser -u USER -- cat -- FILE`，由 Go 管理流、stderr 与 `Wait`；不在多线程 Go 进程里临时切换 UID，不经 `sh -c` 拼接命令。只对明确授权的 root 暂存输入使用 Go 直接打开。
- 比较采用固定大小缓冲区，内存占用不随数据库大小增长。比较发现早期差异时可停止读源，只有这次主动结束产生的 SIGPIPE 可忽略；源真实读取错误、非预期退出、取消和启动失败都要返回错误。相同结果必须确认源进程正常结束。
- 复制先写 root 登记目录的 `database.*.partial`，再次读取源进行字节比较，成功后设置属主/0600 并 `File.Sync`。主库与存在的 WAL 都准备完成后才进入发布；使用固定目标目录 fd 与 `renameat`/`unlinkat`，发布后同步该目录。沿用 WAL 先、主库后的顺序，不宣称两个文件合为一次原子事务。
- 目标目录 fd 的 uid 必须为面板账号，设备号必须与登记目录一致；目录被改名或路径被替换时，继续操作原已固定目录，不跟随替换后的路径。
- 普通返回及可捕获信号清理本次 partial；SIGKILL/断电留下的同名材料仍交给现有 `remove_database_partials`。不为新工具再建一套磁盘状态协议。
- Bash 在调用备份预检前保留 `check_directory=$(mktemp -d "$registry/check.XXXXXXXX")`；Go 在这个 root 0700 空目录里链接主库/存在的 WAL，以原有 `--check-database` 调用已准备好的检查程序，等待退出并清理。拒绝非普通链接结果；不复制整库，不在备份目录创建 SHM 或临时文件。
- 保留 `.check-panel.<更新器 PID>` 的检查程序命名；它由 Bash 准备，预检成功、失败、清理和信号路径都沿用同一所有者。Bash 的 `on_signal` 仍能找到当前 `check_directory`；Go 被强杀时，Bash 清理和下次运行的 `check.*` 扫描仍有效。清理失败必须拒绝更新并保留原始检查错误。

## 3. 文件职责与改动范围

| 文件 | 职责 |
| --- | --- |
| `cmd/qui-update-db/main.go`、`main_test.go`、`main_linux_test.go` | CLI、协议/版本、信号、退出码与授权回归 |
| `internal/platform/hostupdatedb/database.go`、`database_linux.go`、`database_other.go` | 上述类型/接口，Linux 实现，非 Linux 明确拒绝入口 |
| `internal/platform/hostupdatedb/database_linux_test.go` | 真实临时文件、权限、比较、持久化错误及预检回归 |
| `deploy/linux/update.sh` | 三个薄包装、工具固定与快照副本、恢复选择及清理；其余状态机不变 |
| `deploy/linux/install.sh` | 从同一发布目录暂存/原子发布共享工具；缺失在替换文件前明确拒绝 |
| `deploy/linux/uninstall.sh` | 沿用共享工具生命周期，最后一个组件卸载时移除工具；purge 既有共享目录清理覆盖它 |
| `scripts/build-release.sh` | 静态构建 amd64/arm64 工具并放入既有两份归档，不增加独立附件或签名体系 |
| `scripts/check.sh` | 构建工具，沿用新包的 Go 检查与现有 shell 门禁 |
| `deploy/linux/tests/update-test.sh` | 调用真实 Go 工具，维护现有状态断言与新增跨版本恢复场景 |
| `deploy/linux/tests/purge-menu-test.sh` | 共享工具保留/最后卸载清理、缺失及错误协议拒绝安装 |
| `docs/deployment.md`、`docs/operations.md`、`docs/project-spec.md` | 工具位置、源码安装要求、固定副本/恢复边界；规则只写在对应入口 |

不修改前端、Agent 业务代码、迁移、`internal/agent/updater` 或面板数据库检查实现。安装入口继续消费完整可信归档；源码手工安装需要同时构建 `bin/qui-update-db`，在部署文档写明，不偷偷下载工具或使用其他版本代替。

## 4. 重点审查条件

| 条件 | 必须观察到的结果 | 验证归属 |
| --- | --- | --- |
| 面板账号读不到 root 0600 WAL | 底层权限错误完整保留，数据库及 WAL 均不发布 | 任务 1；shell 原场景 27 |
| 固定目录后被面板账号替换成符号链接 | 私有目标未改，发布只发生在原目录句柄 | 任务 1；原场景 28 适配 |
| 大文件首段不同/读取失败 | 不同与错误分开；恢复不会把读取失败当成无需恢复 | 任务 1；原场景 31、32 |
| 预检期间 TERM/HUP、helper 被杀或清理失败 | 不安装，旧服务按现有路径恢复；私有文件可清理或明确保留等待恢复 | 任务 1、3；原场景 39～41 |
| 回退到旧面板或丢失已安装工具 | 新快照使用自身固定工具恢复，旧快照继续用自身旧 Bash；工具丢失时不得启动不一致服务 | 任务 2、3 |

## 5. 实施任务

### 任务 1：Go 文件操作与 CLI（先不接入安装更新）

**文件：** 新建表中 `cmd/qui-update-db` 和 `internal/platform/hostupdatedb` 文件。**输入：** 第 2 节合同。**产物：** 可独立调用的三个子命令。

- [ ] 获得本阶段新增/调整测试与运行的明确授权后，先加入下列有实际断言的回归；先观察缺少工具/函数或原行为失败，再实现。
  - `TestCopyOfflineDatabase`：有/无 WAL，内容逐字节相同、独立 inode、面板 uid/gid 与 0600，成功无 partial。
  - `TestCopySourcePermissions`：真实非 root 账号无法读 root WAL，返回包含原始权限错误，目标主库/WAL 均不存在；Owner 暂存库路径另测可用。
  - `TestCopyPinnedDestination`：目录固定后重命名并换符号链接；私有目标内容/属主不变，备份在原目录。
  - `TestCopyFailureBeforePublish`：复制、比较、文件同步错误均不得发布；目录同步错误必须返回失败，不能承诺已回滚已发生的 rename。只为同步错误和精确竞争点加入包内最窄测试接缝，无生产环境开关；需包含在测试授权中。
  - `TestCopyRestoreSidecars`：恢复清理旧 WAL/SHM，无 WAL 备份不能带上新版本 WAL；主库/WAL 发布顺序保持。
  - `TestCompareSource`：相同、长度不同、首块不同、晚期读取错误、取消；区分不同与错误，早期差异不被 SIGPIPE 误判。
  - `TestPreflightBackup`：复用现有检查程序 fixture 风格，核对 checker 收到 `check.*/panel.db`、硬链接和私有目录集合；备份原文件集合/内容不变，报告与 0/3/4/5 保留。
  - `TestPreflightFailureAndCancel`：链接/目标启动/删除错误返回内部失败，保留检查报告和清理错误；取消等待子进程退出，不能返回成功。
  - `TestRunDatabaseTool`：非法参数不启动服务；compare 的 0/1/2、preflight 的工具成功/目标状态首行/内部 70、协议与版本输出准确。目标正常退出、126/127 启动错误和工具自身失败必须可区分。
- [ ] 按第 2 节接口实现，错误用 `%w`/`errors.Join` 保留原因；不得用 `recover`、空 catch 类路径或无条件忽略 `Close`/子进程退出错误。
- [ ] 授权后在 Linux 运行一次：`go test -p 1 ./internal/platform/hostupdatedb ./cmd/qui-update-db`，应全部通过；运行同包 `go vet`。Windows 只核实新包能随现有构建编译，不运行 Linux 文件操作测试替代品。

### 任务 2：归档、共享安装与恢复副本

**文件：** `scripts/build-release.sh`、`deploy/linux/install.sh`、`deploy/linux/uninstall.sh`、`deploy/linux/update.sh`、`scripts/check.sh` 及相关既有 shell 测试。**输入：** 任务 1 的工具与协议。**产物：** 安装、降级和快照都能获得稳定工具，暂不替换数据库操作。

- [ ] 构建静态 Linux amd64/arm64 工具，与两个主程序一样注入 `main.version`；放进既有控制归档，归档摘要覆盖新增文件。Agent 独立签名清单及现有附件集合不变。
- [ ] 安装器从同一包 `bin/qui-update-db` 暂存、校验协议并原子发布。面板和 Agent 都安装共享工具，以覆盖“只升级 Agent，之后再运行共享更新器”的组合。新安装器缺少工具时在开始替换文件前拒绝；不从网络补齐。
- [ ] 将共享工具加入 `component_files`，对应 shell 镜像清单同步。工具和共享脚本一样，在仍有其他组件时保留，最后卸载才移除；保留现有忙锁及恢复材料保护。
- [ ] 只有本次包含面板数据库操作时才准备活动工具；纯 Agent 更新不调用数据库工具。已安装脚本使用已安装工具；从同一完整新归档运行脚本时使用该归档随包工具，不按执行失败尝试其他版本。开始任何本轮组件停服前，将选定工具复制到 root 登记目录的独占临时文件，检查属主、普通文件、不可由组/其他用户写及协议；以全局 `database_helper` 保存固定副本路径，本轮不在安装后重新选择。
- [ ] 面板 `take_snapshot` 将固定副本保存为 `database-helper`，保存 `database-helper.sha256` 并随快照同步，完成后才允许写 journal。它独立于 `files/...` 中将要恢复的共享工具副本。
- [ ] 新快照恢复的 prepared/started 阶段，在修改程序和数据库前核对快照工具类型、属主、权限、摘要和协议；后续数据库操作只使用该工具。committed/restored 仅收尾路径不因不使用的工具阻塞清理。旧快照继续派发其自带的旧 updater，不要求补文件，不迁移旧 journal，不用缺文件猜版本。
- [ ] 更新运行中临时副本的 EXIT 清理；不能在恢复结束前删除快照工具。构建失败、工具准备失败时不停止服务。
- [ ] 授权后加入并运行归档/安装/卸载的窄验证：双架构 ELF 与源码 SHA、缺工具拒绝、协议错拒绝、共享保留与最后清理、新面板快照含固定副本且旧快照无新依赖。优先扩展现有脚本；如必须新建测试脚本，明确列入授权并登记 `scripts/check.sh`，保留 100755。

### 任务 3：接通薄包装与跨版本回归

**文件：** `deploy/linux/update.sh`、`deploy/linux/tests/update-test.sh` 及三份产品/运维文档。**输入：** 任务 2 固定工具路径。**产物：** 实际数据库操作由 Go 执行，Bash 状态机结果保持。

- [ ] `copy_database_atomic SOURCE DEST [restore]` 改成组装 `copy` 参数的薄包装；仅源等于已冻结 `$staged_database` 时设置 `--owner-staged`。`compare_database_source` 调用 `compare`，保留 0/1/2。删除已无调用的 Bash 复制/管道实现，不保留“双实现自动切换”。
- [ ] `preflight_backup` 仅登记当前 `check_directory`，复用 `preflight_database` 的 checker 准备和结果映射，令备份分支调用 `preflight`；Go 负责链接、执行检查及清理。先判断工具是否成功，再解析固定目标状态首行，工具失败及格式错误统一拒绝；`--database` 停服前直接检查和已有目标结果分类不变。
- [ ] 保持 `database_differs`、备份名称、恢复新库留存、partial 扫描、信号退出码及 `check.*` 扫描规则；检查 Go 子进程被杀时 Bash 仍会清理当前登记目录并拒绝安装。
- [ ] 授权后让现有 `update-test.sh` 构建一次真实工具并放入临时安装/归档；不使用总是成功的假数据库工具。原有假面板和假 init 仍只验证编排，真实 SQLite 判定继续由已有 `db.Inspect` 回归负责。
- [ ] 适配原场景 28 的竞争触发位置，继续验证真实 Go 目录 fd；同步错误迁到任务 1 的 Go 接缝。原场景 36 保留“同步失败中止、恢复旧服务、版本不变、无不完整备份/快照”断言，用仅在测试目录生成的失败工具包装器注入退出，不给生产工具加故障开关；真实文件同步失败传播由 Go 测试证明。
- [ ] 原场景 37 中安装器 `publish` 的同步断言原样保留；数据库文件/目录同步的断言移到 Go 层并说明旧 shell `sync` 日志已观察不到它。原场景 41 保留 `.check-panel.<更新器 PID>` 信号目标及 143/恢复/清理断言，不能改成对子进程发信号后自报通过。
- [ ] 在现有 45 个场景基础上增加：①共享脚本/工具更新而面板仍旧；②新更新器携带匹配备份回退到 v1.7.5；③恢复前已安装工具被删除/替换，快照副本仍恢复成功；④快照工具缺失/摘要错时 prepared/started 保留现场并拒绝恢复和启动；⑤旧版真实快照仍由旧 updater 恢复；⑥工具在预检期间被杀，不继续安装且检查目录可清理；⑦工具非零退出/状态首行格式错一律拒绝，只有有效目标结果进入旧映射。真实旧二进制/快照用已发布 v1.7.5、v1.10.3 材料和隔离路径构造，不能用新 updater 冒充旧快照。
- [ ] 同步部署流程、运维排查与项目规则；写明首次升级仍由旧更新器执行，Go 工具从新更新器后续运行起接管数据库操作。未实测时不写性能提升数字。
- [ ] 授权后在 Linux 运行一次 `bash deploy/linux/tests/update-test.sh`（当前脚本无单场景筛选，覆盖全部受影响入口），应原 45 场景及新增场景全部通过；测试失败先判断实现还是测试观测点失效，不删除或放松状态断言。新包接通变化涉及的 Go 窄检查仅在有新改动时补跑。

## 6. 验收与交付

- [ ] 三个 Bash 函数只剩参数、当前检查目录登记及既有结果映射；不再承载复制、逐字节管道比较、硬链接与子进程预检文件流程。
- [ ] 旧面板降级、新旧快照恢复、同机组件版本错位均不依赖当前面板支持新命令；新快照恢复不依赖当前安装的工具。
- [ ] 所有已获授权的窄检查通过并保存原始结果；Windows 未跑的 Linux 检查明确记“未运行”。Shell 语法用 `bash -n`，新增文本 LF/无 BOM、新增 shell 保留执行位。
- [ ] 只有后续明确授权发布时，才按发布手册在最终提交上运行一次 Linux 完整门禁和 GitHub check，然后打 tag；本计划不预定版本号、不预先执行门禁。
- [ ] 交付改动文件、检查证据、未验证范围及任何接口差异；未经新授权不提交推送、不操作生产。

## 7. 实施记录（2026-11-23，发布前）

任务 1～3 的数据库 Go 下沉、CLI、构建接入、共享安装/卸载、工具固定、快照摘要与 Bash 薄包装已落地。第 5 节为实施前的详细验证候选清单，未逐条执行的候选场景不记作已通过；当前证据如下。

- Linux：`go test -p 1 ./internal/platform/hostupdatedb ./cmd/qui-update-db`、同包 `go vet` 通过。覆盖独立复制/字节比较、私有 WAL 读取失败与 Owner 暂存成功、uid/gid/0600、固定目录竞争、文件/目录 fsync 失败、恢复侧文件、早期差异、晚期读取错误、取消、目标报告、检查目录清理/清理失败、CLI 参数及工具错误/目标结果区分。CLI 关闭输出回归先观察 0 错误成功，再修复为 70。
- `bash deploy/linux/tests/update-test.sh`：**51 passed, 0 failed**（原 45 场景＋6 组）。覆盖工具持久副本、缺失/摘要损坏拒绝恢复、工具 127/错误状态首行/预检 SIGKILL 拒绝安装、共享工具与旧面板版本错位。原场景 28 的精确目录 fd 竞争移到 Go 测试；原场景 36 使用测试目录失败包装器验证编排，真实 fsync 错误传播在 Go 测试；原场景 37 的数据库同步观察移到 Go，安装器 publish 同步断言保留。
- `bash deploy/linux/tests/purge-menu-test.sh`：通过。实际安装/清除、两端共享保留、最后卸载移除、缺工具/错误协议在发布前拒绝且清理 staging。
- 隔离兼容检查：从 **v1.7.5、v1.10.3 标签**提取真实旧 updater，在 prepared/started 四个组合生成真实旧快照；删除已安装工具后，由新 updater 转交旧快照恢复成功。另从 v1.7.5 源码构建真实面板与旧库，Go 工具复制并调用旧检查程序返回 `checker-exit: 0 / database: compatible`，备份 SHA 保持不变、检查目录移除。服务管理仍为隔离 fixture，没有启动真实代理服务。
- Linux amd64/arm64 工具均构建成功，ELF 架构正确且静态链接；Windows 工具编译通过。改动 shell 的 `bash -n` 和 `git diff --check` 通过。未构建完整发布归档、未执行 arm64 工具、未运行完整 Linux/GitHub 发布门禁、未操作生产。

本项只包含数据库热点。journal/快照丢弃与 JSON 事件继续由 Bash 负责，没有迁移、Agent 协议变更或版本号变更。旧安装首次更新仍执行旧 updater；工具在新脚本后续运行接管数据库操作。B1 同批实施见[在线判定计划](2026-11-23-node-liveness.md)。

## 发布收尾（2026-11-24）

Owner 后续明确授权完整门禁、提交推送及通过 GitHub check 后打 tag 发布。已将功能与门禁权限修正提交为 `ac48994`，推送工作分支与 main，GitHub check 45303141640 成功后发布 v1.10.4；完整证据与首轮真实核心失败的单次复验见[发布说明](../../releases/release-notes-1.10.4.md)。没有连接或升级生产。以上“未运行/未提交”记录为实施阶段当时状态；浏览器交互与 arm64 原生执行仍未运行。
