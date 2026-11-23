# v1.10.3 发布说明

## 修复与整理

- 减少面板更新停服窗口内的磁盘工作：程序回滚快照移到停服前准备；数据库预检通过私有硬链接读取离线备份及 WAL，取消第二份整库复制。最终一致备份仍在停服后取得，保留逐字节校验、完整性检查、迁移校验、健康检查和回滚。
- 明确原子复制、journal 与安装文件发布的文件和目录同步，安装器逐级同步父目录；递归快照保留整体同步。
- 面板停服失败时丢弃提前准备的快照。备份预检在 root 登记目录的 `check.*` 内进行，正常结束和信号中断时清理，下次运行清理被 `kill -9` 或断电打断的真实检查目录；检查失败明确拒绝更新。`--check-database` 额外响应 SIGHUP，取消检查并清理私有索引。
- 统一节点心跳新鲜度的 45 秒常量，保持各调用点比较规则；只读连接池拒绝非正连接数，复用 SQLite URI 构造，删除订阅读取处恒真的分支。
- 生产状态集中到运维手册；发布说明统一放在 `docs/releases/`，已发布的核心交接与可选内核设计移入 `docs/archive/`，面板更新说明按规则、流程和运维入口整理。

## 兼容与范围

无新迁移，迁移头仍为 0163；最低兼容 v1.7.5，保留有效数据和在途任务。Agent 协议、数据库检查参数与更新恢复合同不变。

首次升级仍由主机已安装的旧更新器执行，提前快照及 root 私有检查目录等脚本优化从新更新器安装后生效。首次由旧更新器调用新检查程序时，若检查进程被 `kill -9`，备份目录仍可能残留 `.proxy-panel-check-*`；确认无更新运行后按运维说明手工清理。

本轮不实施节点存活判定改造或更新器 Go 下沉。节点存活判定交付[设计提案](../superpowers/specs/2026-11-23-node-liveness-design.md)，等待 Owner 决定；B3 已选定[方案 B](../superpowers/specs/2026-11-23-updater-go-design.md)，保留 Bash 入口，后续逐项评审复杂逻辑下沉。

## 验证与发布

- 发布提交与 tag：`v1.10.3` → `c910e890dda0cb47462bfd8b8128b39bf9db5e46`。
- Linux 完整门禁快照 `5b7c5ca498ddc8ae4454529743747741674fdc5c` 与发布提交树完全一致（`git diff --exit-code 5b7c5ca c910e89` 通过）；常规 check 966 秒、真实核心套件 408 秒均通过，真实核心 213 项通过。
- 真实核心套件中依赖额外旧 Agent、固定兼容探针及父子进程 fixture 的四项非真实核心用例按原条件跳过；未修改断言或跳过规则。
- [GitHub check 45183473746](https://github.com/jingniao/qui/actions/runs/45183473746) 同 SHA 首轮通过，四个检查分片、汇总 check 和 cores 均成功；本次接手后只查询既有检查证据，未重复运行测试。
- [Release workflow 45190340314](https://github.com/jingniao/qui/actions/runs/45190340314) 成功；[v1.10.3 Release](https://github.com/jingniao/qui/releases/tag/v1.10.3) 于 2026-11-23T14:11:24Z 发布。
- 已下载核验全部 13 个附件：GitHub SHA256 与大小、`SHA256SUMS` 的 10 项、双架构 ELF 及发布源码 SHA、无源码修改标记、归档与独立 Agent 一致、官方 Ed25519 签名（序号 1010003）、安装脚本及 sing-box provenance 均通过。Linux 测试机隔离目录运行 amd64 面板和 Agent 的 `--version`，均为 1.10.3。
- 本轮未连接或升级生产，未运行生产更新耗时对比、arm64 原生验收或 Alpine/OpenRC 真机升级。

实施批次与授权见[审查后续计划](../superpowers/plans/2026-11-23-review-followups.md)。
