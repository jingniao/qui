# v1.12.2 发布说明

## 修复

- **旧状态重建封存超时**：面板 `ResetLegacyState` 原先在请求事务里把该节点全部历史 `access_batches`/`traffic_batches` 回执逐行写入 `reset_id`。生产面板（1 vCPU / 约 1 GB 内存）上德国 netcup、美国 CC、日本 zouter 分别有约 16.5 万、12.6 万、10.2 万行，事务超过 Agent 封存的 30 秒期限，同时独占唯一写连接，期间节点消息 `waiting_db` 超过 10 秒、遥测整理连续超时。超时后重建脚本退出，Agent 与核心保持停止。现只标记 `traffic_pending_batches` 与 `traffic_delivery_queues`（仍有读取方）；已完成回执继续按 `(node_id,batch_id)` 去重，其 `reset_id` 无读取方。
- **封存失败不再留下停机节点**：`agent-state-reset.sh` 在封存失败（尚未删除任何数据）时恢复启动旧 Agent 与网络/调优监听并以退出码 1 结束；开机、下次升级或 Agent 菜单「启动」按原 reset ID 重试，面板已提交而仅回复丢失时重试取回同一结果。
- **LXC 节点核心交接恒失败**：`proxy-agent.service` 去掉 `ProtectKernelTunables`。Proxmox LXC（lxcfs）中该选项重新挂载 `/proc/sys`，Agent 读到宿主 `boot_id`，root 更新器读到容器 `boot_id`，交接授权报 `core_handover_conflict: OS environment differs`，升级与安装时的网页升级初始化均回滚为“核心未确认 detached：armed”。服务账号无特权、无能力，本就不能写 sysctl。

## 改进

- `qui` 菜单（仅装 Agent 的主机）新增「3. 启动」：Agent 已运行时不操作；更新或重建持有 `update.lock` 时拒绝；存在未完成的旧状态重建时在后台继续重建（完成后由脚本启动新 Agent），否则启动已安装的网络/调优监听与 `proxy-agent`。
- 彻底卸载（`uninstall.sh --purge` 与菜单）只需在终端输入 `y` 确认，不再要求再输入 `purge`。

## 迁移与兼容

无新迁移，迁移头保持 **0165**，最低兼容 **v1.7.5**。面板修复对已在 `prepared` 阶段的 1.12.1 Agent 生效（面板侧改动，Agent 原 reset ID 与目标摘要不变）。已有 LXC 节点的运行中 Agent 仍使用旧单元，首次升级前须让它换用新单元（例如 drop-in `ProtectKernelTunables=false` 后重启一次 Agent），否则该次交接仍会失败并回滚。

## 文件职责

| 文件 | 本次改动 |
| --- | --- |
| `internal/panel/node/state_reset.go` | 封存事务只标记待处理批次与投递队列 |
| `deploy/linux/agent-state-reset.sh` | 封存失败恢复旧 Agent；服务启动合并为 `start_agent_services` |
| `deploy/systemd/proxy-agent.service` | 去掉 `ProtectKernelTunables` 并注明原因 |
| `deploy/linux/qui.sh` | Agent 菜单新增「3. 启动」 |
| `deploy/linux/uninstall.sh` | 删除 `purge` 输入确认 |
| `deploy/linux/tests/agent-state-reset-test.sh` | 封存失败场景检查旧 Agent 已恢复 |
| `deploy/linux/tests/purge-menu-test.sh` | 去掉 `purge` 输入；取消场景改为 `n` 与空回车；检查 Agent 菜单含「3. 启动」 |

## 验证

- 发布提交：`984f33236873d6aced66f6ab6eab24627e85b1c1`。
- `quick-check.sh --release --base v1.12.1` 通过：CR/BOM、改动脚本 `bash -n`、`gofmt` 与 34 个包 `go vet`。
- 提交前在 Linux 测试机对同树快照单独运行 `agent-state-reset-test.sh`、`purge-menu-test.sh` 及 `internal/panel/node` 重建用例，均通过。
- 同 SHA Linux 完整门禁首轮通过：check 1041 秒、真实核心 425 秒，总耗时 1041 秒；无重跑。
- [GitHub check 45541937684](https://github.com/jingniao/qui/actions/runs/45541937684) 同 SHA 首轮成功；本次未改核心适配与渲染，GitHub cores 未运行，真实核心证据来自 Linux 门禁。
- tag `v1.12.2` → `984f332`；tag 推送触发的 Check 运行 45543917596 中 `release` 任务成功，[v1.12.2 Release](https://github.com/jingniao/qui/releases/tag/v1.12.2) 于 2026-11-25T04:07:35Z 发布，13 个附件齐全。
- Linux 测试机下载核验：`SHA256SUMS` 共 10 项，amd64 程序包与独立 Agent 摘要匹配；包内 `VERSION`、`proxy-panel`、`proxy-agent`、`qui-update-db`、`qui-update-state` 均为 1.12.2，独立 Agent 与包内 Agent 字节一致；包内 `proxy-agent.service` 仅注释提及 `ProtectKernelTunables`，`uninstall.sh` 无 purge 输入，`qui.sh` 含 Agent「3. 启动」。arm64 附件与签名清单未逐项下载核验。
- 未运行真实浏览器交互、arm64 原生执行、Alpine/OpenRC 真机升级，也未在 LXC 真机验证新单元下的核心交接。

## 生产排查与处置（2026-11-25）

批量升级到 1.12.1 后，德国 netcup、美国 CC、日本 zouter 三台 Agent 停止：升级本身成功，旧状态重建封存超时后脚本退出。台湾 wap（Proxmox LXC）自 1.10 起每次升级/安装回滚为“核心未确认 detached：armed”，原因见上文修复项。

- 日本 zouter：03:25 UTC 重跑重建，面板在超时边缘提交但回复丢失；03:30 再跑按原 reset ID 取回结果，`sealed → rebuilt → complete`，Agent 与 xray 运行，面板显示在线。
- 美国 CC、德国 netcup：面板身份仍为原指纹（未提交），03:33–03:34 UTC 手工启动原 1.12.1 Agent 及网络/调优监听，核心运行并连上面板；重建停在 `prepared`，待面板升级到 1.12.2 后重跑。
- 台湾 wap：添加 `ProtectKernelTunables=false` drop-in 的操作被本会话权限策略拒绝，未执行；该节点仍以普通模式运行 1.12.0 并在线。
- 发布后按 Owner 授权升级生产面板，见下节。

## 生产面板升级与重建收尾

2026-11-25 04:38 UTC 记录基线（面板 1.12.1、迁移头 0165、13 节点状态/版本/修订、配置 SHA256）。Owner 会话不可用，未创建手工加密备份；`qui-update --component panel --version 1.12.2 --yes` 退出 0，更新器创建 `pre-update-1.12.1-20261125-053952.db`。升级后面板 1.12.2、`NRestarts=0`，配置未变，无回滚残留，`/healthz` 与门户 200，迁移头 0165，13 节点与基线一致，无新增 WARN/ERROR。

04:43 UTC 在美国 CC、德国 netcup 依次重跑旧状态重建，各约 10 秒完成封存、重建与重部署确认（此前每次 30 秒超时），核心运行、面板显示在线，重部署状态 `success`。Agent 未升级到 1.12.2；台湾 wap drop-in 仍未执行；真实浏览器交互与真实代理请求未验证。
