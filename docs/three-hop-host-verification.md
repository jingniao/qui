# UK → Alpine → US 三节点真实流量验收

执行日期：2026-11-06 UTC；清理后核对时间 09:36:50Z。Owner 明确要求从三节点真实流量验收开始。本轮范围为生产三节点 GOST 加密中继、受控发布失败回滚、无操作重发和中间 Agent 重启恢复；全部通过。不是终端用户 VLESS/Reality 认证或代理画布 P3 的验收。

## 基线与恢复点

面板运行离线 1.4.0-rc.2（`bcdf16a`），三台 Agent 为 rc.1。跑前 UK/Alpine/US 成功修订为 **246/244/243**；原有“UK → Alpine NAT”隧道使用 21411。UK、Alpine 启用 Xray + GOST，US 仅 Xray。

保存节点、9 个入站（8 个既有业务入站和 1 个正式隧道管理对象）、出站、节点组、用户、隧道、规则及画布 API 基线。加密备份 `dGIBDd0_Vg4qHavviN4NaPBuT51TgH-n4RqTgMaRFgI` 已 verify，SHA-256 `dec98906fdafd1ac94226893df45c800218958acc1b412ca6fdfc936b8cccf40`。

通过标准面板核心任务临时为 US 添加官方固定 GOST 3.3.2；保留 Xray 主核心。UK、Alpine 使用原有 GOST，不重装。先检查每台系统监听，再使用空闲测试端口；没有触碰既有 xboard-node 服务。

## 独立测试路径

```text
UK 本机测试程序
  → UK forward :39411（TCP / UDP，订阅隐藏）
  → TLS/mTLS → 113.29.231.218:21412（Alpine NAT，中间跳）
  → TLS/mTLS → 23.254.158.3:39412（US，出口跳）
  → US 127.0.0.1:39419（只在回环监听的标记目标）
```

面板创建独立临时隧道 `iWCHMWxp0dVvIHC-afna7ez10B4LKKMVpGQrLYgBa6M` 和规则 `BgQPWr8IfdK41JKveMmVIiEJ1A2QsOWudDN7EKHdzxc`。三节点契约由实际能力允许，发布计划顺序为 **US → Alpine → UK**。原有 21411 两节点隧道未删除或改路由。使用固定测试字节，不创建用户凭据、不导出订阅、不向其他测试机传输凭据。

## 受控失败与自动回滚

在 UK 测试端口 39411 上临时绑定独立占用进程，保证冲突仅影响新规则。发布批次 `I2XrCAJhziD6gP7OZu5xJDU6c1FTy36Row7EtDVvrQQ`：

| 节点 | 候选修订 | 批次结果 | 恢复修订 |
| --- | ---: | --- | ---: |
| US | 247 | 后续失败，rolled_back | 243 |
| Alpine | 248 | 后续失败，rolled_back | 244 |
| UK | 249 | 预期 apply_failed，批次 failed／修订 rollback | 246 |

批次最终为 rolled_back，逐台心跳验证 current_revision 与发布前完全一致，三台在线。随后释放占用并验证 TCP/UDP 39411 可重新绑定。此为本轮有明确注入条件的回滚证据，不能据此推断早前 A1 首次隧道失败的未知首因。

## 正常发布与真实流量

移除占用后发布批次 `uZJd5UO_LN66zBYRhA0rmLw27YjPY6XtPtZPm1_Yqh4`：US **250**、Alpine **251**、UK **252** 均 applied。再次无修改发布三台均 unchanged。

目标回复固定 US 标记及完整输入字节；TCP 使用长度帧并校验完整响应，UDP 校验每个完整数据报。结果：

| 协议 | 载荷大小 | 首轮耗时 | Alpine 重启后耗时 | 响应一致性 |
| --- | ---: | ---: | ---: | --- |
| TCP | 64 B | 1030ms | 1047ms | 完整一致 |
| TCP | 1024 B | 1065ms | 1000ms | 完整一致 |
| TCP | 16384 B | 1345ms | 1363ms | 完整一致 |
| UDP | 32 B | 987ms | 1039ms | 完整一致 |
| UDP | 512 B | 992ms | 974ms | 完整一致 |
| UDP | 1200 B | 990ms | 999ms | 完整一致 |

两轮 **12/12** 请求全部通过，包含 US 标记和载荷 SHA-256 校验；目标只绑定 US 回环，测试从 UK 本机规则发起，经各跳公网地址和独立 mTLS relay 链到达。UDP 数据由 TLS/TCP relay 承载，不代表各跳为原生 UDP 传输或完成长流／性能验收。

## 中间节点恢复

09:34:31Z 使用 OpenRC 重启 Alpine 的 proxy-agent。未重启机器、未重签身份、未重新发布配置。Agent 恢复 started，三台在线并保持成功修订 250/251/252；随后第二轮 TCP/UDP 六项全部通过。该结果验证新连接／数据报恢复，不承诺既有长连接无中断。

## 清理与最终状态

1. 删除临时规则并发布：UK **253** applied；US/Alpine unchanged。
2. 删除临时三节点隧道并发布：US **254**、Alpine **255** applied；UK unchanged。
3. 停止并删除 US loopback 目标进程和临时文件。
4. 标准核心移除任务删除本轮临时添加的 US GOST；UK、Alpine 原有 GOST 保留。

最终 API 对比：9 个原有入站、全部出站、节点组、用户、画布与规则列表均等于跑前基线；原“UK → Alpine NAT”的 ID、路径、21411、材料公开元数据与 published 状态均保持，临时隧道／规则为 0。最终核心集合与基线一致，三台在线：

| 节点 | 最终成功修订 | 核心 |
| --- | ---: | --- |
| UK | 253 | Xray + GOST |
| Alpine | 255 | Xray + GOST |
| US | 254 | Xray |

主机定向绑定检查确认 UK 39411、Alpine 21412、US 39412/39419 等临时测试端口无残留监听。SQLite quick_check=ok、foreign_key_check 0 行，数据库仅保留 1 条正式两节点隧道、0 条规则。未恢复旧数据库、未更改 DNS、未创建正式 tag 或公开 Release。

原始基线、备份元数据、发布／回滚／重启／移除结果和净化流量记录位于本机忽略目录 `.tools/three-hop/`。报告不含私钥、证书材料或用户配置。没有为该运维验收修改业务源码或重跑全仓库 race。

仍需独立验收：终端用户 P1/P2/P4 的 VLESS/Reality/xhttp 认证与订阅、画布 P3 代理协议三跳、密钥新旧认证、长期混合流量及跨版本程序／数据库降级。不能将本次 GOST 三节点回显通过写成完整 A1 或 v1.4 正式发布通过。
