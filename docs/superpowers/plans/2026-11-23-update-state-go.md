# B3 方案 B：journal、快照丢弃与 JSON 事件下沉实施计划

> **执行状态（2026-11-23）：** 已按 `executing-plans` 在现有分支落实代码与隔离验证，未开启子代理、未暂存/提交/推送。Owner 已明确授权本计划的测试和夹具，并明确取消测试面板触发告警与渠道收件验收。生产升级不属于本计划。

**Goal：** 将 `update.sh` 的 journal v1 读写、快照丢弃和 JSON 事件编码下沉到独立 Go 工具，保留 Bash 编排及已发布恢复、网页更新合同。另附一项独立任务：续费提醒、证书续期失败等系统告警通知补全正文（第 7 节）。

**Architecture：** 新增 `qui-update-state`，提供 `journal-read`、`journal-write`、`discard`、`event`。Bash 只选择阶段、传递参数和处理结果；每轮固定同包工具，面板与 Agent 快照都保存工具及摘要。新恢复只用自身快照工具，旧快照先转交自身旧 updater。保留 `qui-update-db` 及其 `qui-update-database-v1` 协议。

**Tech Stack：** 沿用 `go.mod`、标准库和现有 `golang.org/x/sys/unix`；Linux amd64/arm64 静态工具，无新增依赖或数据库迁移。

**依据：** [已选方案 B](../specs/2026-11-23-updater-go-design.md)、[数据库首项实施记录](2026-11-23-update-database-go.md)、[v1.10.4 发布说明](../../releases/release-notes-1.10.4.md)。代码基线 `1fa769b`，发布代码 `ac48994`。

**全局约束：** 最低兼容 v1.7.5，保留其有效数据与在途任务；迁移头保持 0163。四阶段为 `prepared|started|committed|restored`，Agent 确认仍须在提交后发送。先完成 journal/丢弃，再接事件，每个任务单独验证；不扩展为完整更新器重写。

## 1. 当前行为与边界

- `journal_write` 写固定顺序的 v1 文本，文件 0600，先同步临时文件、rename，再同步快照目录。组件为 agent 时总是追加 `core_request=`、`core_helper=` 两行（值可为空）；v1.7.5 的 Agent journal 没有这两行。
- `journal_read` 和 `journal_value` 按行读取、从不执行文本；同名键取首次出现值，未知键忽略，末行无换行也读取，值原样保留（不去除 `\r`）。必需项为 magic、component、was_active、phase；已发布读取器允许 previous/target 缺失显示“未知”，backup/database_source/core_request/core_helper 缺失为空。这些兼容真实旧快照的规则须显式保留，不能改成新格式必填。
- 备份目前按 `$backups/` 前缀及最终普通非符号链接文件检查；代码实际接受嵌套路径，不能照注释误改成“必须直接子文件”。恢复来源与目标路径的现有边界不扩大。
- `dispose_snapshot` 先将 `rollback.SUFFIX` 改名为 `discard.SUFFIX` 再删除；当前 registry 同步错误被忽略，删除返回值也可能被最终 `return 0` 遮蔽。7 处调用（`update.sh` 1484、1514、1534、1539、1548、1618、1660 行）以 `|| true` 忽略结果；`finish_recovered_snapshot` 直接返回其结果。
- `report_update` 的固定输出为 `{"stage":"...","state":"...","exitCode":null,"error":"..."}\n`。另有四处直接 `printf` 的事件：`managed_request`（448 行，error 为 `request:digest:manifest` 纯文本）、`agent_install` 失败（461 行）与 `managed_recovery`（470 行）的 error 是受管安装器已编码的 JSON 值、`snapshot_prepared`（1553 行）。`agent-update-request.sh` 按字段顺序、无空格前缀和 error 位于末尾提取事件，`managed_request` 与 `snapshot_prepared` 还绑定原始执行/恢复材料。不能只核对 JSON 解析后的对象相同。`panel-update-request.sh` 不传报告 fd，不消费这些事件。
- 事件可能在工具固定前发出：72 行 EXIT trap 的终态事件，以及 `recover_interrupted_updates`（1961 行）、`agent_read_status`、`ensure_database_record` 等处 `fail` 发出的失败事件；数据库工具当前在 2073 行才固定。现有 `cleanup`（2056 行）先删除临时工具与下载目录，再调用 `report_terminal`。
- `recover_interrupted_updates` 先 `rm -rf` 清理 `discard.*`/`check.*`，再对每个 `rollback.*` 用当前 `journal_read` 校验并打印「发现被中断的更新：路径（组件 旧→新，阶段）」，然后执行快照自身 updater。`update-test.sh:1228` 断言该提示前缀。
- `--system-update-contract` 仍单独返回 `proxy-agent-system-update-v1`，不要求 Go 工具存在。report fd 仍只允许 3～8，更新锁仍为 fd 9。
- 范围仅 `update.sh` 的 journal/丢弃/事件；不迁移 `agent-update-request.sh` 自身的 request/result/progress JSON，不修改 web-handover worker 的另一种 journal、开机恢复收据、数据库工具、核心交接、init 服务实现或 Agent worker updater。

## 2. 实施选择与合同

### 2.1 工具边界

选择独立 `cmd/qui-update-state` 与 `internal/platform/hostupdatestate`。扩展 `qui-update-db` 会让数据库专用协议承担 Agent 状态事务；完整 `qui-updater` 则超出已选方案 B。`--protocol` 输出 `qui-update-state-v1` 加换行，`--version` 为 `qui-update-state X.Y.Z`；三个文件命令仅 Linux 支持，`event` 可跨平台编码。

库接口：

```go
type Journal struct {
    Component, Previous, Target string
    WasActive bool
    Backup, DatabaseSource, Phase, CoreRequest, CoreHelper string
}
type Event struct {
    Stage string `json:"stage"`
    State string `json:"state"`
    ExitCode *int `json:"exitCode"`
    Error string `json:"error"`
}
func ReadJournal(file, backups string) (Journal, error)
func WriteJournal(snapshot string, journal Journal) error
func DiscardSnapshot(registry, snapshot string) error
func RemoveDiscarded(registry, discarded string) error
func EncodeEvent(w io.Writer, event Event) error
func EncodeEventRawError(w io.Writer, stage, state string, exitCode *int, errorJSON []byte) error
```

CLI 文件操作失败返回 1，参数/工具自身错误返回 70，成功 0；stdout 仅承载约定输出，诊断到 stderr。

- `journal-write --snapshot DIR --component panel|agent --previous ... --target ... --was-active true|false --backup ... --database-source ... --phase ... [--core-request ... --core-helper v1|'']`：core 两行是否写出只由 `--component agent` 决定，agent 必写（值可为空），panel 传入 core 参数时拒绝。
- `journal-read --file FILE --backups DIR`。
- `discard --registry DIR --snapshot DIR` 或 `discard --registry DIR --discarded DIR`（互斥）。
- `event --stage STAGE --state STATE --exit-code null|0..255 [--error-json]`：从 stdin 读取 error；默认作为字符串编码，`--error-json` 时 stdin 必须是一个完整 JSON 字符串字面量（见 2.3）。

### 2.2 journal 与丢弃

- writer 保持 magic `proxycontrol-update-journal-v1`、字段名、顺序、Agent 附加字段和末尾换行。拒绝不能由单行格式表达的 LF/CR/NUL，不限制合法空格、中文或 `=`。保持 `.journal.tmp` 暂存名；不新增磁盘阶段或 journal 版本。Go 写出的 journal 必须能被 v1.10.3、v1.10.4 的 Bash `journal_read` 原样接受：降级被 kill -9 后，已换回的旧 `qui-update` 会作为父进程读取新 updater 写的 journal。
- reader 保持既有“首次键生效/未知键忽略/历史可缺字段/末行无换行/不去 `\r`”语义。禁止 `source`、`eval`、命令替换解释字段。CLI 输出九个值，顺序为 `component,previous,target,was_active,backup,database_source,phase,core_request,core_helper`，每值之后都有 NUL，bool 用 `true|false`。Bash 写入 root 私有临时文件后先核对命令成功，再用 `mapfile -d ''` 装配并确认恰好九项；不用会隐藏退出状态的进程替换。
- 文件操作在 root 0700 登记目录及快照内执行，journal 必须 root 0600 普通非符号链接文件。writer 固定目录句柄，文件 fsync、renameat、目录 fsync；失败保留原始原因。只加包内最窄文件同步/精确竞争接缝，无生产故障开关。
- discard 只处理已核实 registry 的直接 `rollback.*` 子目录，保留历史非空 suffix，不强加八位限制；目的 `discard.*` 已存在时明确拒绝，不能覆盖另一份恢复材料。改名前失败保留 rollback；改名后同步失败保留 discard 并报错；删除失败明确报错。删除与最后目录同步成功才返回 0。
- `RemoveDiscarded` 清理合法 `discard.*`：不跟随顶层符号链接，不删除未确认路径；非目录残留沿用现有“跳过并提示”。snapshot 全量建立/恢复仍在 Bash。

**Bash 调用方处理丢弃失败（逐点规定，不再 `|| true` 静默）：**

| 调用点 | 处境 | 丢弃失败时 |
| --- | --- | --- |
| 1484、1514、1534、1539、1548 | 安装前失败（停服失败、备份/预检失败、journal 写入失败），本来就以 `fail`/`exit 1` 结束 | 把丢弃错误和保留路径追加到 stderr 与任务日志，原失败原因与非零退出不变 |
| 1618、1660 | 更新已提交、版本与服务已核实，且 `journal_write committed` 成功 | stderr 与任务日志明确警告“更新成功但快照清理失败：路径”，退出码保持 0 |
| 1618、1660 | 面板 `journal_write committed` 失败（现有代码只打印提示仍继续丢弃）且丢弃也失败 | 残留的 `rollback.*` 仍是 prepared/started，下一轮会回滚这次已成功的更新。必须非零退出，stderr 写明“快照停在未完成阶段，下次运行会回滚本次更新，请先人工处理：路径” |
| `finish_recovered_snapshot` | 恢复已完成，且 journal 已确认写为 restored（或本来就是 committed/restored） | 明确警告并保留路径，返回 0；不得让 `rollback_component` 误报恢复失败、触发 `report_failure` 降级指引 |
| `finish_recovered_snapshot` | `journal_write restored` 失败（现有代码提示“下次重新恢复”后继续）且丢弃也失败 | 返回非零并写明下一轮会重做恢复（面板 started 阶段会再次用备份覆盖数据库，先保留当前库为 `failed-update-*`） |

保持 0 的依据：此时残留只可能是 journal 为 committed/restored 的 `rollback.*`（rename 未持久化）或 `discard.*`，下一轮恢复对这两者只做收尾，不会回滚已成功的状态。Bash 须记录本次 journal 最终写入是否成功，作为上表的判断条件。警告必须可见，不得只写日志文件。

### 2.3 JSON 事件和失败

- 由 Go 一次生成完整、紧凑的单行事件；结构体声明顺序固定 stage/state/exitCode/error，`encoding/json` 关闭 HTML escaping。真实阶段集合以当前全部 `report_stage`/`report_update` 调用和桥接消费者为准，不随意收紧到少数已知阶段。
- **非法 UTF-8（显式兜底，数据保护理由）：** error 正文来自 install.sh、curl、Agent 输出，可能含非 UTF-8 字节；硬失败会让真实失败报告丢失。规则：每个非法字节转写为可见文本 `\xNN`（两位小写十六进制，JSON 中即 `\\xNN`），合法部分原样保留，原始字节仍在任务日志。该转写集中在 `event.go` 一个函数中，测试固定其行为；不使用标准库的 U+FFFD 静默替换。stage/state 仍要求合法 ASCII，非法即 70。
- **已编码 JSON 透传：** 461、470 行改为 `event --error-json`。工具要求 stdin 恰好是一个 JSON 字符串字面量（`json.Valid` 且解码为 string，前后无多余内容），然后用同一编码器重新输出。含孤立代理项转义（如 `\ud800`）的输入视为无效：标准库解码时会把它静默替换为 U+FFFD。校验失败时工具返回 1，Bash 走现有 else 分支：把完整原始输出作为普通字符串事件上报，并在 stderr 写明“结构化错误无效，已按原文上报”（显式兜底，理由同上：保证失败可见）。448 行 `managed_request`（state `bound`）与 1553 行 `snapshot_prepared` 改为普通 `event`，输出字节须与现状一致（error 为 hex/base64/路径，无需转义）。
- 有效 JSON 转义可采用标准库表示；必须证明引号、反斜杠、控制字符、中文、`<>&`、U+2028/U+2029 能往返，且网页桥接仍按原布局提取原始 error JSON。保留 null 与整数区别、空 error 字符串和末尾唯一换行；不截断或默认清空错误。
- Bash `report_update` 先运行工具生成到本次 root 私有事件暂存文件，检查成功后转发到继承 fd 并检查转发状态。转发遇到断管可能留下不完整行，必须报错，消费者沿用既有无完整收据即不成功的规则；不承诺任意长度写入的原子性。工具不会把错误正文放进进程参数，也不持有 fd 9。
- 错误事件本身失败时，记录原始操作错误和上报错误，最终非零；禁止递归调用 `fail -> report_update -> fail`。数据保护要求：上报失败不能阻止必要的停核、数据库恢复或回滚，清理继续执行并汇总错误；保留现场和日志，绝不以版本号或耗时补造成功事件。实施时不加入 Bash JSON 备用实现。
- **工具固定前的事件（定死规则）：** 从脚本开始到 `prepare_state_helper` 完成之间（参数解析、root 校验、`recover_interrupted_updates`、`agent_read_status`、`ensure_database_record` 等处的 `fail`，以及 72 行 EXIT trap），`report_update` 按 2.4 的来源规则（同包优先，否则已安装）就地调用工具，先做一次 `--protocol` 校验，不复制。无报告 fd 时不调用工具，保留原 stderr/退出。工具不可用或协议不符时，stderr 明确写“更新事件未写出：原因”并非零退出；不改用 Bash 编码。桥接保留既有“无终态收据不能假成功”的解释。`--system-update-contract`、`--help` 与恢复转交不得因此产生副作用。
- **EXIT 顺序：** 改写 `cleanup`（`update.sh:2056`）为：必要恢复（bootstrap 保留释放、worker 停核屏障）→ 删除下载目录与 agent 输入 → `report_terminal` 及 fd 同步 → 删除事件暂存、数据库工具与状态工具活动副本。`on_signal` 退出后同样经过 `cleanup`。上报错误不抹掉原始退出原因，最终仍失败。

### 2.4 工具发布和跨版本恢复

- 每个 Linux 架构的程序归档内增加 `bin/qui-update-state`，安装到 `/usr/local/lib/proxycontrol/qui-update-state`（root:root 0755）。面板与 Agent 共享，最后一个组件卸载时清除；不新增独立附件、签名协议或下载回退。
- `component_files` 增加 `$root/usr/local/lib/proxycontrol/qui-update-state`（与 `qui-update-db` 并列）。这样回滚会恢复旧工具；回滚或降级到不含该工具的版本时，快照中没有它，恢复会删除它。例外：从 v1.10.4 及更早版本首次升级时由旧 updater 执行，旧 `component_files` 不含该路径，若这次安装失败回滚，新装的工具会留下，旧版卸载也不会删。成功降级到 v1.10.4 及更早版本时同理：旧安装脚本不认识也不删除该文件，工具会留下。工具无副作用、下次成功升级时被覆盖，两种残留都接受并写入运维手册故障排查，不为旧 updater 补兼容。`update-test.sh:1534` 的共享文件排除名单同步加入。
- 正常更新固定 root 0500 活动副本（`prepare_state_helper`），来源沿用数据库工具“发布目录优先随包，否则已安装”的规则，缺失或协议错误拒绝，不尝试其他版本。固定时机：`recover_interrupted_updates` 完成旧快照转交之后、`discard.*` 清理与下载、停服之前。
- 面板和 Agent 的新快照都保存 `state-helper`、`state-helper.sha256`，同步后才允许写 prepared journal。`--recover` 在读取新 journal、修改文件或丢弃快照前验证自身工具，并复制为可持续到 EXIT 的私有活动副本，避免 discard 删除工具后无法发送终态。`--recover` 分支在第 2071 行 `trap cleanup EXIT` 之前就 `exit`，现有 EXIT trap 只发终态事件；须为该分支单独注册删除活动副本的 EXIT 处理。
- 活动副本与事件暂存使用固定前缀（`.state-helper.*`、`.update-event.*`），与现有 `.database-helper.*` 一样放在 registry。kill -9 会留下这些文件，现有代码从不清理 `.database-helper.*`。`recover_interrupted_updates` 在持锁后、固定本轮工具前，删除三类前缀的普通非符号链接文件。
- `recover_interrupted_updates` 调整为：① 现有 `check.*` 清理保持 Bash，同时删除上条所列三类残留临时文件；② 对每个 `rollback.*` 完成根目录/权限/updater 核验后，不再用当前 `journal_read` 解读，只打印「发现被中断的更新：路径，交给其快照 updater 恢复」（保留 `update-test.sh:1228` 断言的前缀），组件、版本与阶段由快照 updater 自己打印；③ 返回后由 `prepare_state_helper` 固定工具；④ 再用 `discard --discarded` 清理 `discard.*`。固定发生在 1961 行，早于 2071 行 `trap cleanup EXIT`；之间的 `fail`、“已是最新版本”正常退出、用户取消都会走 72 行的旧 trap，活动副本会泄漏。因此 `cleanup` 的注册须提前到 `prepare_state_helper` 之前（`directory` 等变量为空时跳过对应步骤），保证固定之后的每条退出路径都删除活动副本。不能用“缺 state-helper 就猜是旧版”的分支。新快照自己的 updater 缺工具时明确拒绝；旧 updater 自行恢复，不依赖已安装新工具。
- 真实 v1.7.5、v1.10.3、v1.10.4 快照在已安装工具缺失/替换时仍能转交恢复。开机单元 `update-recovery.sh` 仍调用 `/bin/bash DIR/updater --recover DIR --boot`，不修改既有锁、journal 四阶段、数据库恢复条件和服务启动顺序。

## 3. 文件职责

| 文件 | 改动 |
| --- | --- |
| 新增 `internal/platform/hostupdatestate/journal.go`、`journal_linux.go`、`journal_other.go` | v1 解析、序列化、Linux 持久化边界与其他平台拒绝文件入口 |
| 新增同包 `discard_linux.go`、`event.go` | 受限快照改名/清理、单行事件编码与原始 JSON 透传；无 init/网络操作 |
| 新增同包 `journal_test.go`、`journal_linux_test.go`、`discard_linux_test.go`、`event_test.go` | 真实旧格式、权限/同步/竞争与 JSON 回归 |
| 新增 `cmd/qui-update-state/main.go`、`main_test.go`、`main_linux_test.go` | 子命令、stdin/退出码、CLI 合同与错误传播 |
| `deploy/linux/update.sh` | journal/丢弃/报告薄包装、`component_files`、工具固定、快照副本、先转交旧恢复、丢弃失败调用点、`cleanup` 顺序 |
| `deploy/linux/install.sh`、`uninstall.sh` | 同包暂存校验/发布、共享生命周期 |
| `scripts/build-release.sh`、`scripts/check.sh` | 双架构构建、`go build -o bin/qui-update-state`，新 root 文件测试包加入 v1.10.4 已修复的“普通用户编译、sudo 执行”名单 |
| `deploy/linux/tests/update-test.sh`、`purge-menu-test.sh` | 真实新工具 fixture、原 51 项（45 场景＋6 组）和新增恢复/共享检查，不替换状态断言 |
| 新增 `deploy/linux/tests/update-state-event-test.sh`，登记到 `scripts/check.sh`，100755 | 调用真实 Go 编码与实际 Agent 网页桥接解析逻辑，验证事件布局、阶段和错误/终态 |
| `docs/project-spec.md`、`architecture.md`、`deployment.md`、`operations.md` 和 B3 设计 | 实施后同步工具边界、恢复、故障排查；更正设计文档第 18 行“`panel-update-request.sh` 依赖事件”的错误描述；计划阶段不宣称行为已生效 |

## 4. 实施任务与最窄检查

### 任务 1：journal v1 与 CLI

**接口：** 产出第 2 节 `Journal`、`ReadJournal`、`WriteJournal`，以及 CLI 的 journal 子命令、`--protocol`、`--version`。非 Linux 文件入口在 `journal_other.go` 明确返回不支持错误。

- [x] 从 v1.7.5、v1.10.3、v1.10.4 的真实 updater 生成 panel/agent 的四阶段 journal，作为测试输入，禁止用新 writer 冒充旧材料。
- [x] 先增加 `TestReadReleasedJournalV1`：核对缺省历史字段、首次重复键、未知键、空值/中文/`=`、未以换行结束的末行、带 `\r` 的值按旧实现拒绝；必需字段错误、非法 phase/component/bool、符号链接及不安全备份返回明确错误。先观察新接口缺失或错误行为的失败。
- [x] 实现第 2 节接口；`TestJournalWriteDurability` 覆盖字节格式（含 agent 空 core 两行、panel 无 core 行）、root/0600、覆盖旧 phase、文件 sync/rename/目录 sync 错误、临时文件清理和固定目录竞争。区分 rename 前后失败的磁盘状态。
- [x] 反向兼容：Go writer 为 panel/agent 各阶段写出的 journal，交给 `git show v1.10.3:` 与 `v1.10.4:` 的 `update.sh` 中 `journal_read` 读取，九个值逐一相同（放在 `update-test.sh` 或独立 shell 夹具）。
- [x] CLI 回归 `TestJournalCLI` 验证 NUL 输出字段数量/顺序、正文不被执行、参数边界（含 panel 传 core 参数被拒）与错误退出码。Linux root 测试机执行 `go test -p 1 ./internal/platform/hostupdatestate ./cmd/qui-update-state -run '^(TestReadReleasedJournalV1|TestJournalWriteDurability|TestJournalCLI)$'`，预期全部 PASS；再执行 `go vet ./internal/platform/hostupdatestate ./cmd/qui-update-state`，预期退出 0。CI 普通用户编译后经 `-exec 'sudo -n --'` 执行两个包，成功后不重复未改范围。

### 任务 2：快照丢弃

**接口：** 产出 `DiscardSnapshot`、`RemoveDiscarded` 与 `discard` 子命令；非 Linux 拒绝入口放在任务 1 的 `journal_other.go`。

- [x] 增加 `TestDiscardSnapshotPhases` 与 `TestRemoveDiscarded`：正常清理、源已不存在的已完成清理、目的冲突、非登记直接子目录、顶层符号链接、改名/同步/删除错误及重启再次清理，失败不能删除另一份快照。
- [x] 实现受限路径操作和目录持久化；同步失败不得继续删除唯一恢复材料。内部错误保留原始原因和真实保留路径。
- [x] Linux root 测试机执行 `go test -p 1 ./internal/platform/hostupdatestate ./cmd/qui-update-state -run '^(TestDiscardSnapshotPhases|TestRemoveDiscarded)$'`，预期全部 PASS；执行同包 `go vet`，预期退出 0。在 shell 中保留原先“未写 journal 不会恢复程序、restored 只收尾”的断言。

### 任务 3：事件编码与桥接兼容

**接口：** 产出第 2 节 `Event`、`EncodeEvent`、`EncodeEventRawError` 与 `event` 子命令；`report_update` 消费其 stdout/退出码，最终活动工具路径由任务 4 固定。

- [x] 增加 `TestEncodeEventBridgeLayout`、`TestEncodeEventCharacters`、`TestEncodeEventInvalidUTF8`、`TestEncodeEventRawError`、`TestEventOutputFailure`，先失败后实现。固定四字段顺序、无空格前缀、末尾换行、null/0/143/255、错误正文往返、非法字节转写为 `\xNN`、`--error-json` 接受字符串字面量并拒绝对象/多值/尾随内容、writer 失败；取消或短写不能报成功。
- [x] 增加 `update-state-event-test.sh`：真实工具输出交给当前桥接的事件解析分支，核对 managed_request 绑定（字节与现状一致）、snapshot_prepared、agent_install 原始 error JSON、managed_recovery、bootstrap/agent/recovery、INT/TERM/HUP 和 terminal。fd 关闭、工具消失、输出失败均非零且不能补造终态。
- [x] 接通 report 薄包装、四处直接 `printf` 的替换、`--error-json` 校验失败的显式按原文上报，以及第 2.3 节的错误汇总/工具固定前规则/EXIT 顺序；测试“上报失败仍执行原有回滚、日志含两类原因”，不新增独立通知/事件消费者重写。
- [x] 执行 `go test -p 1 ./internal/platform/hostupdatestate ./cmd/qui-update-state -run '^(TestEncodeEventBridgeLayout|TestEncodeEventCharacters|TestEncodeEventInvalidUTF8|TestEncodeEventRawError|TestEventOutputFailure)$'`，预期全部 PASS；任务 4 完成固定副本接线后运行 `bash deploy/linux/tests/update-state-event-test.sh`，预期退出 0。脚本新增后登记执行位与门禁。

### 任务 4：共享安装、固定副本与恢复接线

**接口：** 消费任务 1～3 的全部 CLI；Bash 的 journal 薄包装保持现有调用方 globals，核心交接字段取任务 1 九项输出的最后两项，禁止另留文本解析器。

- [x] 构建和安装工具；先注册 staging 清理 trap，再校验协议，最后才 publish。扩展共享卸载与缺工具/协议错拒绝安装检查。`component_files` 加入工具路径。
- [x] 接入两种组件快照、工具摘要、活动私有副本，以及第 2.4 节 `recover_interrupted_updates` 的四步顺序。替换 journal_value 的核心交接字段读取；按第 2.2 节表格改写 7 处丢弃调用和 `finish_recovered_snapshot`；删除 `update.sh` 中已无调用的 `report_json`、journal 文本实现。
- [x] 原 51 项更新检查全部保留；新增新 journal 的 prepared/started/committed/restored、工具缺失/摘要错/被替换、提交后与恢复后丢弃失败（退出 0 且有警告、下一轮收尾）、committed/restored 写入失败又丢弃失败（非零且提示下一轮会回滚/重做）、安装前丢弃失败（原错误保留）、`--recover` 退出后以及固定工具后“已是最新”、用户取消、早期 `fail` 退出时活动副本均被删除、kill -9 残留临时文件下一轮被清理、回滚后工具恢复/降级后工具被删除、继承 fd 事件回归。真实旧 updater 产生的快照分别经 next-run 与 boot 转交，旧版本恢复不依赖新工具。
- [x] 运行 `bash deploy/linux/tests/update-test.sh`、`purge-menu-test.sh`、`update-state-event-test.sh`；直接改动 shell 逐文件 `bash -n`、`git diff --check`；验证双架构静态构建及 Windows 编译。Linux 文件测试与 CI sudo 执行覆盖都必须有证据。
- [x] 同步第 3 节文档，记录每项实际通过与未运行范围。发布与生产操作另按当次授权和发布手册执行，不在任务中自动提交、推送或打 tag。

## 5. 重点审查与验收

1. 损坏/缺省/重复键旧 journal：必需字段拒绝，真实旧有效字段仍可恢复；Go 写出的 journal 被 v1.10.3/v1.10.4 Bash 读取器接受。由任务 1 验证。
2. rename 后目录同步或删除失败：返回失败并保留 discard，不再误识别成待恢复 rollback；Bash 调用方按第 2.2 节表格处理，已成功的更新/恢复不被误报失败。由任务 2、4 验证。
3. 新快照工具丢失、当前安装被回退：新快照明确拒绝且不动服务，旧快照独立恢复；回滚后工具文件与快照前一致（快照中没有则删除）。成功降级到不含该工具的版本会留下工具，按第 2.4 节例外处理。由任务 4 验证，不能靠缺文件猜旧版本。
4. 报告 fd/工具失败恰逢 SQL 或程序回滚：原始错误、上报错误与恢复结果都可见，数据保护动作继续，绝不报告成功；工具固定前的失败按 2.3 规则处理。由任务 3 与 4 验证。
5. JSON 合法但布局改变：实际 shell 桥接仍取得相同阶段、原始错误 JSON、收据绑定和终态；非法 UTF-8 与无效结构化错误仍能上报。由任务 3 验证，不能只测试 json.Unmarshal。

验收时 `update.sh` 不再自行解析/写 journal、递归丢弃快照或转义 JSON；服务编排、锁、四阶段语义、数据备份/恢复、核心交接和收据判断保持原合同。只承诺上述热点下沉，不称作整个更新器 Go 重写。

本计划包含两处显式兜底（非法 UTF-8 转写、无效结构化错误按原文上报），理由均为保证失败报告可见，集中实现、测试固定、可单独删除。

## 6. 生产升级

不在本计划内。生产升级按[发布手册](../../release-runbook.md)与当次 Owner 授权执行，结果记入[变更记录](../../changelog.md)与[运维手册](../../operations.md#当前生产环境)。

## 7. 独立任务：系统告警通知补全正文

与 B3 无依赖，可先于任务 1 单独实施和发布。不新增迁移、设置项或接口字段。

### 7.1 共同根因

告警由 `system.alert` 事件进入 `internal/panel/notification/worker.go` 的 `Alert`。它只把 `message`（没有时取 `reason`）当正文，不填 `Notification.NodeID`；渠道文本为 `[SEVERITY] 标题\n正文`（`sender.go` 的 `notificationText`）。发布方没有给 `message` 时，通知就只剩标题。

`Alert` 改动：读取 `nodeId` 写入 `Notification.NodeID`（两种 map 类型都处理），其余行为不变。`notifications.node_id` 无外键，节点已删除也能写入。界面影响：通知中心 `NotificationBell.vue` 对带 `nodeId` 的条目显示“前往节点”按钮，续费提醒、自动额度失败、证书自动发布失败将出现该按钮（预期行为）；Viewer 仍按 kind 过滤，看不到 `system_alert`。

### 7.2 续费提醒显示服务器名称

**现状：** 渠道只收到 `[WARNING] 服务器续费提醒`。`internal/platform/server/maintenance.go:47` 只发布 `{"alert":"node_renewal","nodeId":id}`；`node.Repository.RenewalAlerts`（`internal/panel/node/meta.go:241`，唯一调用方即 maintenance）只返回节点 ID。

- `RenewalAlerts` 改为返回 `[]RenewalAlert{NodeID, Name, RenewDate string; DaysLeft int; AutoRenew bool}`，同一查询 JOIN 取 `nodes.name`、`m.auto_renew`；`DaysLeft` 按面板时区计算（与现有判断同一 `day`/`renew`），事务与 `last_alert_day` 更新不变。
- `maintenance.go` 发布 `{"alert":"node_renewal","nodeId":id,"message":正文}`：
  - 未到期：`服务器「名称」将于 YYYY-MM-DD 到期，还剩 N 天。`
  - 当天：`服务器「名称」今天（YYYY-MM-DD）到期。`
  - 已过期：`服务器「名称」已于 YYYY-MM-DD 到期，已过 N 天。`
  - 开启自动续费时追加 `已开启自动续费，日期将自动顺延。`（提醒在顺延前发送，日期为原日期，与现有规则一致）。
  - 名称为空时显示 `未命名服务器（节点 ID）`。

### 7.3 证书续期失败告警显示证书与到期信息

**现状：** 渠道只收到 `[WARNING] 证书续期失败`。`internal/panel/certificate/worker.go:325` 的 `alertIfExpiring` 只发布 `{"alert":"certificate_renewal","certificateId":id}`。证书没有名称字段，只有 `Domains`、`NotAfter`；失败码在调用方 `failed` 里已算出（`failureCode(err)`），没有传进 `alertIfExpiring`。

- `alertIfExpiring` 增加 `code string` 参数，由 `failed` 传入；触发条件、每日一次的审计去重（`RecordDaily`）不变。
- 发布 `{"alert":"certificate_renewal","certificateId":id,"message":正文}`，正文：
  `证书 首个域名（另 N 个域名）续期失败，将于 YYYY-MM-DD HH:MM UTC 过期，还剩 N 天 H 小时。失败代码：code，下次重试：YYYY-MM-DD HH:MM UTC。`
  - 只有一个域名时省略括号；已过期写 `已于 … 过期`。
  - 证书服务没有面板时区，时间统一用 UTC 并标注，不另行读取设置；下次重试取 `failed` 中 `NextAttempt(now, attempts)` 已写入数据库的同一时间戳（返回字符串），解析后按 UTC 格式化。
  - 失败代码沿用 `internal/panel/certificate/failure.go` 的固定错误码原文，不翻译。

### 7.4 自动额度发布失败：修正标题映射并显示节点名称

**现状：** `internal/platform/server/quota.go:803` 发布键为 `user_access_failed`，`worker.go` 映射的是从未发布的 `auto_quota_failed`，标题落到默认的“系统告警”；`worker_test.go:76` 测的就是这个死键，所以一直没被发现。正文只有 `reason`，没有节点名称。

- `worker.go` 映射改为 `user_access_failed` → “自动额度发布失败”，删除 `auto_quota_failed` 分支；`worker_test.go` 对应用例改为真实发布键（这是测试本身写错，改的是用例输入，不是放宽期望）。
- `quota.go` 发布前用已有的 `d.nodes.Get(ctx, nodeID)` 取名称，加入 `message`：`节点「名称」自动额度发布连续 N 次失败，已停止重试：reason`（N 取 `quotaAttemptLimit`）。

### 7.5 证书自动发布失败：显示节点名称

**现状：** `internal/platform/server/certificate_deploy.go:350` 发布 `{"alert":"certificate_deploy_failed","nodeId":…,"reason":…}`，正文只有原因。`certificateDeployer` 没有节点仓库。

- `certificateDeployer` 增加 `nodes *node.Repository` 字段，`newCertificateDeployer` 增加参数，`server.go:405` 传入已有的 `s.nodeRepository`。
- 发布 `message`：`节点「名称」证书自动发布连续 N 次失败，已停止重试：reason`（N 取 `certificateAttemptLimit`）。

### 7.6 用户访问状态变化与设备上限：标题与中文正文

**现状：** `quota.go:143`（额度状态变化）与 `:188`（删除用户）发布 `user_access`，没有标题映射，显示“系统告警”；正文只有原因码，事件里已有的 `username` 没用上。原因码只有 `disabled`、`quota_exceeded`、`expired`（`internal/panel/user/quota.go` 的 `InactivityReason`）、`reactivated`（同文件）和 `deleted`（`user/handler_http.go:228`）。

- `worker.go` 增加 `user_access` → “用户访问状态变化”。
- 两处发布加 `message`：`用户「用户名」原因说明。`。告警发出时凭据变更只是排入自动额度版本（`d.schedule`），还没有到达节点，失败时另有 7.4 告警，所以措辞不能写“已移除”。固定映射：`disabled` 已被禁用；`quota_exceeded` 流量额度已用尽；`expired` 已到期；`deleted` 已被删除——以上四种后接“，节点凭据将在下一次自动额度发布中移除”；`reactivated` 已恢复访问，节点凭据将在下一次自动额度发布中重新下发。映射放在 `quota.go` 一个函数里；出现映射外的新原因码时正文写原码，不报错（显示用，不影响额度执行）。
- severity 保持 `warning` 不变，不调整通知规则或开关。
- **设备上限告警（同类）：** `internal/panel/user/device_observer.go:285` 发布 `{"category":"user_access","userId":…,"message":"同时在线设备上限已触发"}`，没有 `alert` 键，标题为“系统告警”，正文没有用户名。调用方 `Command`（237 行）已持有 `u`（含 `Username`、`DeviceLimit`）。改为 `alert(uid, u.Username, u.DeviceLimit)`，发布 `{"alert":"device_limit","userId":…,"message":"用户「用户名」已触发同时在线设备上限（N 台），超出的设备被拦截。"}`，删除无消费方的 `category` 键；`worker.go` 增加 `device_limit` → “设备上限触发”。每用户每小时一次的节流不变。

### 7.7 节点名称读取失败（7.4、7.5 共用，显式兜底）

告警是 Owner 获知“已停止重试”的唯一渠道，不能因为读名称失败而丢掉。读取失败时：正文写 `节点 <节点 ID>（名称读取失败）`，照常发布告警，同时 `logger.Error` 记录原始错误。名称为空时写 `未命名节点（<节点 ID>）`。该处理集中在 `internal/platform/server` 一个辅助函数里，测试固定行为，可单独删除。

### 7.8 文件、文档与检查

- 代码：`internal/panel/node/meta.go`、`internal/platform/server/maintenance.go`、`quota.go`、`certificate_deploy.go`、`server.go`、`internal/panel/certificate/worker.go`、`internal/panel/notification/worker.go`、`internal/panel/user/device_observer.go`。
- 测试（新增或修改断言须 Owner 授权）：`internal/platform/server/maintenance_test.go`、`quota_test.go`（告警 payload 含 `message`）、`certificate_deploy_test.go`（构造参数与告警正文、名称读取失败）、`internal/panel/certificate/` 告警相关测试、`internal/panel/notification/worker_test.go`（标题映射、正文、`NodeID`）、`internal/panel/user/` 设备观测告警相关测试（payload 键与正文）。
- 文档：`docs/project-spec.md` 第 235 行（续费提醒正文）、第 428 行（自动额度失败告警正文）、第 801 行（证书续期失败告警正文）、第 803 行（证书自动发布失败告警正文），第 717 行附近补一句系统告警标题与正文内容（含用户访问状态变化、设备上限触发）；设备限制章节（第 9.2 节）补告警正文。
- 检查：`go test ./internal/panel/node ./internal/platform/server ./internal/panel/certificate ./internal/panel/notification -run '<相关用例>'`、同四包 `go vet`。在测试面板配一个渠道，分别触发：节点到期日设为 3 天后、余期不足 7 天的证书续期失败、禁用再启用一个用户、让一个用户超出设备上限；确认通知含服务器名称、证书域名与到期时间、用户名与中文原因。自动额度与证书自动发布连续 3 次失败难以在测试面板复现，以单元测试为准，实机未覆盖项在汇报中注明。

## 8. 实施与验证记录（2026-11-23）

代码与本次授权范围内的验证已落实；未发布、未升级生产。原工作区已有的 `docs/operations.md`、`docs/releases/release-notes-1.10.4.md` 改动保留，发布说明未被本次修改。

### 8.1 文件交付

| 文件 | 实际改动 |
| --- | --- |
| `cmd/qui-update-state/main.go` | 四个子命令、协议/版本、stdin 错误正文、九项 NUL 输出及 0/1/70 退出码 |
| `cmd/qui-update-state/main_test.go`、`main_linux_test.go` | 事件/参数、Linux journal CLI 回归；Windows 参数错误码也验证 |
| `internal/platform/hostupdatestate/journal.go` | v1 解析、单行字段校验与原顺序序列化，保留历史缺省/首次键语义 |
| `journal_linux.go`、`journal_other.go` | root 私有固定目录、临时文件/rename/fsync、Linux 文件入口与其他平台拒绝 |
| `discard_linux.go` | registry 直接子目录、不覆盖改名、目录同步后受限删除及错误路径 |
| `event.go` | 紧凑固定布局、关闭 HTML escaping、非法字节可见转写与孤立代理项拒绝 |
| `journal_linux_test.go`、`discard_linux_test.go`、`event_test.go`、`testdata/*` | 真实三版材料、旧字节布局、同步/竞争/权限/路径与 JSON 回归；旧 updater 完整 gzip 夹具及来源说明 |
| `deploy/linux/update.sh` | Go 薄包装、工具活动/快照副本、旧恢复转交、丢弃失败分级、上报失败汇总、EXIT 顺序 |
| `deploy/linux/install.sh`、`uninstall.sh` | 同包校验发布与共享工具生命周期 |
| `scripts/build-release.sh`、`scripts/check.sh` | 双架构打包、新工具构建、root 测试包名单、事件脚本登记 |
| `deploy/linux/tests/update-test.sh`、`purge-menu-test.sh` | 真实工具 fixture、保留原 51 组，新增 48～53 六组；安装拒绝、共享清理与恢复检查 |
| `deploy/linux/tests/update-state-event-test.sh` | 实际 Agent 桥接解析代码、旧 reader 反向读取、fd/早期失败/结构化错误及无副作用入口检查 |
| `internal/panel/node/meta.go`、`internal/platform/server/maintenance.go` | 续费提醒携带名称、原日期、日历天数及自动续费说明；UTC 日期序号计算避免 DST 与 duration 截断 |
| `internal/panel/certificate/worker.go` | 续期失败携带域名、UTC 到期/重试时间及固定错误码，复用同一已保存重试时间 |
| `internal/panel/notification/worker.go` | NodeID 落库、修正真实额度失败键、用户访问/设备上限标题 |
| `internal/platform/server/quota.go`、`certificate_deploy.go`、`server.go`、`alert_message.go` | 自动发布失败正文、节点仓储注入、集中名称读取失败处理及用户中文状态说明 |
| `internal/panel/user/device_observer.go` | 设备告警真实 alert 键、用户名/上限正文，保留小时节流 |
| 对应 `maintenance_test.go`、`quota_test.go`、`certificate_deploy_test.go`、`alert_message_test.go`、`certificate/service_test.go`、`notification/worker_test.go`、`user/device_observer_test.go` | 真实 payload、名称读取失败、通知 NodeID、日期/DST、证书多域名/过期、用户状态与节流回归 |
| `docs/project-spec.md`、`architecture.md`、`deployment.md`、`operations.md`、B3 设计与本计划 | 同步实现边界、首次升级残留、恢复/故障说明与实际验证记录；不改变正式版状态 |

### 8.2 实际检查

- Linux root：两个新包全量 `go test -p 1 ./internal/platform/hostupdatestate ./cmd/qui-update-state` 通过。补充的旧 writer 字节一致、最终目录同步失败和嵌套符号链接用例随后定向通过。
- 原 `update-test.sh` 51 组全部保留，增加六组后 **57/57 通过**。其中包含新快照工具损坏/缺失、清理警告和未完成阶段双重失败、活动副本生命周期、真实三版旧 updater 的 next-run/boot 转交、报告失败仍回滚、回滚删除快照中原本没有的工具。
- `purge-menu-test.sh` 随上述更新测试执行通过（含状态工具缺失/错协议拒绝安装、暂存清理与共享卸载）。
- `update-state-event-test.sh` 通过：真实 Go 工具输出交给当前桥接解析代码、managed_request 绑定、全部阶段/信号、结构化错误按原文上报、关闭 fd/工具缺失、早期终态、help/contract 和旧 Bash reader。
- 后续 fd 同步汇总改动只复验原更新测试的第 52 组（保留原断言），**1/1 通过**；未重复已通过的其余 56 组。
- 告警五个相关包的 11 个命名窄用例通过，覆盖自动额度/证书发布放弃、续费日期/DST/历史日期、证书每日去重及正文、NodeID、用户中文状态与设备节流；`node` 包在该运行中无独立匹配用例，由 server 用例实际调用其仓储。
- `go vet`：两个新包与 node/server/certificate/notification/user 五个改动包通过；日期与文件测试补充后，node/hostupdatestate 定向 vet 再通过。
- Linux amd64/arm64 `CGO_ENABLED=0` 构建通过，`file` 确认为静态 ELF，`readelf -l` 无 INTERP。Windows 构建通过，跨平台事件及 CLI 参数窄测试通过。
- 两个新包在隔离目录由 uid 65534 编译测试程序，再由 root 执行，均 PASS。测试机系统 Go 1.26.0 不满足模块要求，改用隔离复制的项目固定 Go 1.26.11 后通过；未修改系统工具链。
- 直接改动的八个 shell 文件逐个 `bash -n` 通过；`git diff --check`、改动 Go 文件 `gofmt -l` 通过。

### 8.3 决定与未运行边界

- 沿用当前工作分支，不提交、不推送、不开子代理；最终按五项重点审查做作者自审，未发现待修复的重要问题。无延期的小问题。
- 原“restored”手工夹具补齐新快照要求的工具/摘要；kill -9 残留与本次恢复活动副本分别核对，保留“下一轮持锁清理”合同。
- 真实旧 journal 由旧 Bash writer 提取字节；Windows 提取时只将 `sync` 换为空操作，真实文件持久化另在 Linux 验证，不用 Go writer 冒充旧材料。
- Linux 测试机没有 sudo，故普通用户编译/root 执行的权限拆分已实测，CI 中字面 `-exec 'sudo -n --'` 路径仅完成接线，**CI 完整任务未运行**。
- 新事件脚本在 Linux 执行副本为 0755；当前未改 Git 索引，Owner 暂存时须保留 100755 执行位。
- 完整发布构建、完整 `scripts/check.sh`、真实核心/真实 systemd 或 OpenRC 部署、生产升级均**未运行**；本次只执行规划涉及的窄检查。
- 测试面板触发及渠道收件核对按 Owner 明确指示取消；没有向真实渠道发送通知。
- 无新增 Go 依赖、数据库迁移或范围外行为修改；正式版仍为 v1.10.4，迁移头仍为 0163。

### 8.4 完成状态审计后的修正（2026-11-23）

- 降级残留：成功降级到 v1.10.4 及更早版本也会留下 `qui-update-state`，已写入第 2.4 节例外、第 5 节验收第 3 条和运维手册故障排查。
- 证书续期失败告警：`failed` 直接把重试时间值传给 `alertIfExpiring`（新增包内 `nextAttemptAt`，`NextAttempt` 由它格式化，存库字节不变），删除“解析已保存时间戳失败就不发告警”的不可能分支；`service_test.go` 只同步调用参数，断言不变。
- `take_snapshot`：状态工具、数据库工具与停核 helper 的复制/摘要/同步统一移入 `snapshot_helpers`，任一步失败时删除半成品快照并清空 `snapshot`，避免下一轮把无 journal 的残留误报为旧版快照（Owner 确认一并修正原有两处同类写法）。
- 补充测试（Owner 授权）：`update-state-event-test.sh` 让 v1.10.3/v1.10.4 读取器读取 Go 写出的非空嵌套备份路径与含中文/`=`/空格的 `database_source`，并验证三种工具缺失时 `take_snapshot` 不留下 `rollback.*`；`update-test.sh` 第 49 组增加 Agent 的 committed/restored/restored-unwritten 丢弃失败，第 51 组增加 Agent 的真实三版旧 updater next-run/boot 转交。原有断言未改。
- 新事件脚本执行位：本机 `core.filemode=false`，暂存时须执行 `git update-index --chmod=+x deploy/linux/tests/update-state-event-test.sh`；未暂存。
- 修正后检查：见本次汇报；测试未运行。