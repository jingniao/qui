# v1.10.2 发布说明

## 修复

- 面板监控整合由大事务改为每个五分钟桶独立提交，桶间释放数据库写连接。每 5 秒调度一轮，每轮最多每种 8 桶，连接排队与执行合计预算 2 秒。后续桶失败时保留已提交进度，重试不重复聚合；离线判定时限不变。
- 修复 Agent 升级、旧状态重置与网络 helper 的循环等待；安装器完成发布后异步排队重置，重置按 update→operation 顺序同时持锁，OpenRC 新服务不继承锁。
- 重置先停旧 Agent/helper 再封存、删除重建，恢复新目录/配置的服务账号权限和目录监听。未完成的升级回滚或核心交接记录会明确阻止清理；停服、封存或恢复失败不报告清理成功。
- 兼容 v1.10.1 留下的 pending/prepared/sealed/deleting/deleted/rebuilt 状态。尚未准备日志时以当前二进制摘要准备；已有 reset ID 和身份绑定保持原样，已重建的数据不再次删除，完成后的后续升级不重复清理。

## 兼容与范围

无新迁移，迁移头仍为 0163；最低兼容 v1.7.5。保留既有分层精度、已入账事实和在途任务；一次性旧 Agent 清理仍不补传待上报数据。

本轮未连接或升级生产。德国节点已有的故障 worker、未完成核心恢复可能在旧升级器预检时直接拒绝，实机恢复仍待单独授权执行。arm64 原生运行和 Alpine/OpenRC 真机升级未运行。

## 验证与发布

- 发布提交与 tag：`v1.10.2` → `7a6add0f2a9353d3b41f8e07c3b73b45440cecdf`；无新迁移，迁移头 0163。
- 同 SHA Linux 首轮完整门禁 856 秒，check 在 `TestRealDeviceGateSingboxUDP/hysteria2` 的客户端启动时失败（核心退出，具体启动原因未保留）；未改动的 sing-box 包单次复验 405 秒通过。首轮中止后未执行的构建与部署检查另行补齐并通过（46 秒）。独立真实核心套件 411 秒、213 项通过。
- [GitHub check run 45146487526](https://github.com/jingniao/qui/actions/runs/45146487526) 第 2 次执行通过。首轮 server 分片的 `TestK2bTuningFixedOperationOverAuthenticatedStream` 在等待心跳 Ack 时先收到调优命令；既有实现发送 Ack 后继续查询待发命令，测试并发发起调优请求，存在时序竞争。本次未修改这段逻辑或测试；仅复跑失败的 server 分片一次后通过，保留首轮失败记录。其余成功分片和真实核心未重复运行。
- 新增监控逐桶提交与写连接排队预算回归，观察旧实现失败后验证修复；node 包窄检查通过。隔离 shell 覆盖 systemd/OpenRC 的重置各阶段、失败屏障、锁持有、子进程描述符继承及安装器排队入口；发布快速检查通过。
- [Release workflow 45150499358](https://github.com/jingniao/qui/actions/runs/45150499358) 成功；[v1.10.2 Release](https://github.com/jingniao/qui/releases/tag/v1.10.2) 于 2026-11-23T09:55:44Z 发布。
- 已下载核验全部 13 个附件：GitHub SHA256、SHA256SUMS 的 10 项、双架构 ELF/源码 SHA、无源码修改标记、归档与独立 Agent 一致、官方 Ed25519 签名（序号 1010002）、安装脚本及 sing-box provenance。Linux 隔离目录运行 amd64 面板和 Agent 的 `--version` 均为 1.10.2。
- 未运行 arm64 原生验收、Alpine/OpenRC 真机升级及生产升级。真实核心套件中要求额外旧 Agent/固定兼容探针和父子进程 fixture 的非真实核心用例按既有条件跳过，213 项真实核心测试通过。

## 修改文件

| 文件 | 用途 |
| --- | --- |
| `internal/panel/node/telemetry_tiers.go` | 逐桶提交、索引选桶及单轮预算 |
| `internal/panel/node/latency.go` | 整合调度间隔改为 5 秒 |
| `internal/panel/node/telemetry_tiers_test.go` | 桶失败重试及写连接等待预算回归 |
| `deploy/linux/agent-state-reset.sh` | 修正锁、阶段顺序、重建权限与监听恢复 |
| `deploy/linux/install.sh` | 安装完成后异步排队待执行重置 |
| `deploy/systemd/proxy-agent-state-reset.service` | 解除重置与 Agent/helper 的循环排序依赖 |
| `deploy/openrc/proxy-agent-state-reset` | 后台运行并解除循环依赖 |
| `deploy/linux/tests/agent-state-reset-test.sh` | 两种 init 的阶段、失败和锁回归 |
| `scripts/check.sh` | 登记新增 shell 回归 |
| `docs/operations.md` | 更新离线误报与本版运维说明 |
| `docs/project-spec.md` | 同步调度、重置合同及当前版本 |
| `docs/superpowers/plans/2026-11-23-v1.10.2-runtime.md` | 记录实施与发布检查状态 |
| `README.md`、`AGENTS.md`、`docs/README.md`、`docs/architecture.md` | 同步正式版入口与迁移头 |
| `docs/changelog.md`、本文件 | 保存发布、失败复验和附件核验证据 |
