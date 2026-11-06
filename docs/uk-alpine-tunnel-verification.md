# UK → Alpine NAT 隧道配置与实流验证

2026-11-06 UTC。Owner 要求确认空闲映射端口并配置 UK 连接 Alpine；已完成，隧道保留在生产面板，临时验证资源清理。

## 实际映射与所选端口

在 Alpine 本机确认未占用后，临时分别监听 **21411–21419** 与截图候选 **34252–34270** 的 TCP/UDP，响应包含实例 hostname 和端口的固定标记；从 UK 对每个候选端口做一次定向 TCP／UDP 探测。标记服务不读取主机文件或用户数据，全部自动限时并在结束后显式清理。

- **21411–21419：TCP、UDP 全部收到当前 Alpine 实例的正确标记。** TCP 317–332ms，UDP 156–167ms。
- 34252–34270：TCP 全部被拒绝，UDP 全部 2.5 秒超时；不使用这段范围。
- 21410 由既有 xboard-node 占用，不接管、不改动。
- 选择 **21411** 为隧道接收端口，面板 Alpine 的 portRangeStart/End 登记为 **21411/21419**，公网地址保持 **113.29.231.218**。

这是同端口号 TCP/UDP 的主动实测；只证明本轮时刻的映射可达，不宣称公网地址或供应商规则永久不变。全部 56 个临时 TCP/UDP 监听已清理。

## 持久配置

通过标准核心任务给 uk-node、alpine-node 添加官方固定 **GOST 3.3.2**，归档 SHA-256 `162feb43b1c284dbea599cdfe898169931af875233060dedc19432c7a9d18274`；任务均 success，原有 Xray 保留为主核心。Alpine 为 linux/amd64 + OpenRC，实际安装和能力上报通过，不能再将它记为“不支持 GOST”。

| 字段 | 值 |
| --- | --- |
| 隧道 | UK → Alpine NAT |
| ID | ZlYYJLzcCbBCmsC0JcOOsoiAANZoMVegR0funyuMtks |
| 入口 | uk-node |
| 出口 | alpine-node |
| 连接地址 | 113.29.231.218:21411 |
| 传输 | GOST relay over TLS，逐隧道双向证书认证 |
| 状态 | published，生产浏览器显示“已发布” |
| 长期转发规则 | 0；尚未指定业务目标，临时验证规则已删除 |

初次发布顺序为 Alpine → UK，Alpine 配置 244 applied，UK 因无入口规则返回 unchanged。隧道按需由具体规则发起拨号，不将空闲隧道描述为持续存在的数据连接。

## 真实 TCP/UDP 经过加密隧道

临时目标仅在 Alpine `127.0.0.1:21419` 监听。临时规则在 UK 使用 39311，network=both，subscriptionVisible=false，关联上述隧道，目标为该 loopback 标记服务；未授予用户凭据、未导入订阅或改变现有分组。

路径：`UK 本机测试 → UK 临时 forward → TLS/mTLS → 113.29.231.218:21411 → Alpine loopback 目标`。

| 载荷 | 次数 | 正确响应 | 耗时 |
| --- | ---: | ---: | --- |
| TCP | 3 | 3 | 519 / 504 / 481ms |
| UDP | 3 | 3 | 500 / 493 / 493ms |

UDP 载荷由 relay 经 TLS/TCP 隧道传输，不是声明 TLS 监听本身提供原生 UDP。该结果证明两台生产节点之间的加密中继和目标到达，尚不等于终端用户 VLESS/Reality 客户端、订阅认证或三跳路径验收。

测试发布 UK 245 applied。随后删除临时规则并成功发布移除（UK 246 applied），确认 UK TCP/UDP 39311 和 Alpine loopback TCP/UDP 21419 均可重新绑定；临时目标、进程与文件清除。正式隧道、Alpine 21411 接收监听和两台 GOST 保留。

## 最终核对与恢复点

- 原 8 个入站逐项等于操作前 API 基线；只新增隧道管理的 Alpine 21411 forward 对象。
- 三台 Agent 在线：UK 成功修订 **246**，Alpine **244**，US 保持 **243**；UK、Alpine 启用 Xray + GOST，US 仍 Xray。
- 隧道 ruleCount=0；测试规则列表为空。再次无修改发布两端均 unchanged。
- 升级前加密备份 `dUvJgJEC9C2j7VXAYUaEkLRtpNjMxH5ftci-isiJP7Q` 已 verify，SHA-256 `4deacdd4e5deaf0df90cdcef27ff41973f338934dfe5c0039bf70afc944897f2`。没有执行恢复或修改其他节点的成功配置。
- 原始 API 基线、任务／计划／发布／移除结果保存在本机忽略目录 `.tools/uk-alpine/`；映射探测净化结果在 `.tools/rc2/alpine-mapping-probe.json`，不提交私有证书或配置正文。
