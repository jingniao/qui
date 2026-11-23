# v1.10.0 发布说明

## 功能与修复

- Linux Agent 更新经本机授权后保留 Xray、sing-box、GOST 核心，由新 Agent 验证并认领；覆盖引导程序与实际 worker 两次切换。
- 正常存活交接保留核心进程、epoch、修订及原有连接，沿用原 spool 上报原生累计流量。配置或依赖确实变化时仍按正常发布重启全部核心。
- 普通 stop/restart、卸载和非更新崩溃仍停核。systemd、OpenRC、回滚及中断恢复共用停核屏障；OpenRC 自动 respawn 在新 worker 的 lifecycle 锁内先清理旧核心。
- 修复 sing-box 将 WebSocket 路径误认作本地文件、启动中途崩溃的 prepared 材料匹配，以及先删 marker 导致孤立运行材料的问题。
- pidfd 不可用时明确提示并按已约定的停核更新处理，其余授权错误继续中止；不使用裸 PID 替代。
- 服务器续费与汇率支持的币种从 8 种扩展为 23 种，统一币种符号与美元类价格识别。

## 兼容与边界

无新迁移，迁移头仍为 **0160**，最低兼容版本仍为 **v1.7.5**，保留有效数据与在途任务。首次升级由旧 Agent 执行，仍会短暂断线；后续升级双方均支持交接且配置一致时才保持连接。

仅承诺核心正常存活交接的流量连续。空窗内核心死亡无法补取的尾部流量写入本机 `core_traffic_tail_unavailable` WARN，不估算、不新增面板告警。交接窗口为 300 秒 CLOCK_BOOTTIME。

Xray/sing-box 访问日志在更新窗口内允许积压，按完整行 checkpoint 恢复；访问记录允许少量重复或缺失。sing-box 不持久化既有连接的来源映射，恢复后来源可为 unavailable。长期故障遗留日志须按运维手册处理。

## 实机验收

Alpine/OpenRC 的 GOST、Xray + GOST 受管节点，及 Debian 12 systemd-nspawn 的 GOST 节点已验证原有 TCP 长连接在升级中持续收发，核心 PID 不变，150 秒零错误。GOST 单连接上下行各 1,532,928 字节与面板节点增量一致；Xray + GOST 两条连接的账户及节点流量增量也完全一致。两类服务的普通 stop/start/restart 均无存活孤儿。

追加 OpenRC 验证：候选 1.9.103 的引导程序与 worker 更新保持 GOST PID 16927、长连接零错误；随后同时 SIGKILL 引导程序与 worker，旧核心退出并恢复为单份新进程 19351，恢复后真实回显通过。候选版本号和测试签名仅用于隔离验收，不属于正式发布。

详细数据与修复记录见[交接实施记录](core-handover-plan.md#9-实施与验收结果2026-11-22)。systemd-nspawn 的伪造 boot_id 与服务命名空间不一致，需要在测试容器卸除伪造挂载后验证；LXC 未实测。sing-box 尚无真机交接节点，arm64 未做原生实机验收。

## 门禁与发布证据

发布提交为 `9e16206358dd31d4b04f2e03b36c4bd93d6bde5e`。Linux 首轮总计 840 秒：常规检查 839 秒后因 `internal/platform/server/TestSingboxOnlyTrafficCrossesQuotaAndRemovesCredential` 失败中断；真实核心 213 项通过，耗时 405 秒。该配额用例在 v1.9.0 也有首轮失败记录，本轮失败不经过修改的 Agent 交接路径，根因仍未证实。按发布手册仅对失败的 server 包做一次 `-race` 复验，185 秒通过；原检查尚未运行的面板/Agent 构建、Python 模块检查和部署脚本检查另行补齐，退出码为 0。保留首轮 FAIL、单次复验 PASS 和补齐日志，未改测试期望。

[GitHub 检查 45103187042](https://github.com/jingniao/qui/actions/runs/45103187042) 在同 SHA 的四个常规分片、汇总及真实核心任务均首轮通过。真实核心套件中三个旧版 Agent 探测因未提供对应旧 Agent 跳过，父子进程辅助用例仅在专用夹具运行，未更改断言或新增跳过。

[发布构建 45105096167](https://github.com/jingniao/qui/actions/runs/45105096167) 成功，正式 [v1.10.0 Release](https://github.com/jingniao/qui/releases/tag/v1.10.0) 于 2026-11-23T03:23:07Z 发布，tag 指向上述提交。已下载全部 13 个附件，GitHub SHA-256、SHA256SUMS 的 10 项摘要及双架构 Agent 官方 Ed25519 签名（序号 1010000）均通过核验；双架构 ELF、干净源码 SHA、包内安装脚本及 sing-box 来源摘要一致。amd64 面板和 Agent 在隔离 Linux 测试机仅执行 `--version`，均返回 1.10.0。

## 生产

本轮未连接或升级生产。
