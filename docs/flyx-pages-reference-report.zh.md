# flyx 页面内容与 qui 借鉴建议

**日期：** 2026-10-26  
**参考版本：** [flyx `79af497`](https://github.com/kure29/flyx/tree/79af4975727cf58c24ea0db3715048ee2f502436)  
**阅读范围：** 实际路由、导航、主要页面及相关页签组件。本报告基于源码阅读，未运行 flyx，也未进行浏览器实测。

## 核心结论

flyx 最值得借鉴的是页面分工：**服务器负责运维，节点负责接入，中转负责线路，订阅负责分发，用户前台负责个人使用。** 次要功能放进页签或弹窗，减少侧边栏堆叠。

qui 可以借鉴这种组织方式，继续复用现有的配置发布、回滚、权限和统计能力。页面内容应围绕真实数据与常用操作补充，避免为填满页面引入新的复杂系统。

## 各页面展示什么，可以借鉴什么

以下按页面及主要功能区归纳，页签和弹窗归入所属页面，不逐项罗列表单字段。

| 页面 / 功能区 | flyx 展示内容 | qui 可以借鉴 |
| --- | --- | --- |
| 登录 | 账号密码登录，以及可选的其他登录方式 | 页面简洁，登录动作明确；其他方式按需增加 |
| 管理员总览 | 在线服务器、运行节点、中转线路、有效订阅、待处理问题、流量与到期提醒 | 少量核心卡片，加可直接进入处理页面的异常列表 |
| 用户总览 | 个人转发额度、已用流量、规则数量、授权隧道、公告等 | 根据 qui 的代理接入场景，显示自己的额度、到期和使用状态 |
| 服务器 | Agent 状态、地址、版本、运行时间、CPU/内存/磁盘、网速、月流量、续费及操作 | 展示机器真实运行情况，并说明配置是否同步 |
| 内核（服务器页签） | 安装、选择或更换版本、启停、重启、日志、卸载 | 清楚区分 Agent 升级、代理核心更新与配置发布 |
| 节点 | 自建入站与导入节点，协议、地址、状态、分享和配置操作 | 用接入点列表呈现实际可用状态和连接信息 |
| 订阅源（节点页签） | 外部订阅地址或上传配置，刷新设置、更新时间及同步结果 | 将来实现导入时，清楚显示来源、刷新状态和失败原因 |
| 节点配置弹窗 | 协议预设、传输与安全选项、中转和服务端分流配置 | 常用项直接显示，高级项按需展开 |
| 中转：线路 | 按落地节点组织中转线路，显示路径、状态和探测信息 | 把已有的中转与代理链关系直观展示出来 |
| 中转：隧道 | 入口到出口的转发线路，以及下发、诊断等操作 | 让基础线路与用户接入点分工明确 |
| 中转：规则 / 转发规则页 | 将入口端口流量经隧道转发到目标，显示状态、用量及批量操作 | 借鉴“入口 → 路径 → 目标”的清晰表达 |
| 中转：质量 | 隧道延迟、吞吐和错误历史 | 有采集与历史数据后再增加 |
| 订阅管理 | 独立凭据、到期、额度、重置、节点组、预览、客户端链接和模板 | 简化创建、授权、复制和导入流程，明确额度与到期状态 |
| 订阅节点组 / 模板 | 节点选择、筛选、改名、客户端模板及分流选项 | 复用节点组；初期保持规则模板简单 |
| 我的订阅 | 分配给自己的订阅、节点数、流量、额度、到期、重置时间、导入、二维码和链接重置 | 最适合借鉴到 qui 的个人使用页面 |
| 用户 | 登录账号、状态、转发额度、到期、隧道权限和订阅分配 | 明确账号归属，以及该账号拥有的接入权限 |
| 用户分组 / 隧道分组 / 权限分配 | 将用户和隧道编组，再按组授权 | 借鉴批量授权体验，但不要与现有订阅节点组混淆 |
| 限速 | 授予隧道权限时使用的带宽限制规则 | 有明确带宽限制需求时再考虑 |
| 监控 | 当前资源、历史趋势、服务检查、结果记录和监控权限 | 从服务器摘要进入详情，优先用已有采集数据 |
| 面板共享 | 向其他面板分享服务器、导入远程资源、查看限制和绑定 | 当前内部使用规模下可以暂缓 |
| 设置 | 基本、外观、安全登录、服务器监控、测试点、通知、公告、备份和升级 | 按用途分组，折叠区先显示简短摘要 |
| 账户与登录 | 账号信息、改密、GitHub 绑定和通行密钥 | 一个统一的账户安全入口，仅展示已经支持的功能 |
| 改密 / 登录回调 | 独立的密码修改流程和认证结果处理 | 保持流程清楚，回调页无需进入导航 |
| 移动布局 / 更多 | 窄屏导航及次要功能、账户操作 | 保证手机上常用操作易找，复用同一套业务逻辑 |

## 借鉴时要分清的概念

- **服务器在线不等于代理可用。** Agent 在线、核心健康和客户端能否连通，应分别表达。
- **转发规则不等于网站分流。** flyx 的转发页主要配置端口和隧道；网站分流、客户端规则属于其他配置区域。
- **主机流量不等于个人用量。** 页面应写明统计范围和周期。
- **共享订阅不能准确识别物理设备。** 没有独立身份来源，就不能承诺准确的设备名称、数量和各设备流量。
- **同名分组不代表同一权限模型。** flyx 的隧道分组授权与 qui 的代理节点组授权用途不同。

## qui 建议优先做什么

### 第一优先级：完善现有页面

1. **后台总览：** 关键数据、服务器状态、最近发布结果、待处理问题。
2. **后台服务器 / 节点：** 系统资源、Agent 与核心版本、最后心跳、配置同步及现有管理操作。
3. **个人总览：** 当前登录用户的已用流量、总额度、到期时间、接入状态和订阅入口。
4. **可用节点：** 已授权接入点、支持协议、真实状态及连接方式。
5. **订阅接入：** 复制链接、二维码、支持的客户端导入，以及清楚的重置说明。
6. **设置：** 使用现有组件与接口，按功能分区。

以上是页面职责，可以合并呈现，不必增加六个独立菜单。例如，个人总览与订阅接入可以共用一个简洁页面。

### 第二优先级：已有数据支持再补充

- 个人流量历史与最近活动，注明统计口径。
- 清楚的中转、代理链路径展示。
- 发布错误详情、更新任务进度。
- 确实有运维价值的备注、元数据和监控信息。

### 暂缓

面板联邦共享、完整外部订阅处理流水线、大量独立客户端模板、系统调优、带宽整形和全面线路诊断，都需要更多后端或 Agent 能力，不应夹带进普通 UI 改造。

## 与 qui 当前 v1.1 计划的关系

工作区已有 [v1.1 实施计划](v1.1-plan.md)，记录了用户独立账号、每用户一条共享订阅、用户级权限/额度/到期，以及启用 `with_v2ray_api` 的未修改上游 sing-box 源码构建。

因此，现阶段重点借鉴 flyx 的**个人订阅流程和页面组织**。无需为了模仿 flyx 再引入 nftables 计量；当前计划已比较并选择了统计构建方案。此前讨论的设备管理页面，也应按新的用户级接入决策调整。

实现时继续复用 Vue 组件和 API 封装，局部改动局部完成，验证本次真正修改的页面和交互，控制抽象、依赖及测试规模。

## 源码依据

以下链接均固定到本次阅读版本：

| 范围 | 源码入口 |
| --- | --- |
| 路由与导航 | [App.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/App.tsx)、[admin.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/layouts/admin.tsx) |
| 总览 | [dashboard.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/dashboard.tsx)、[admin-overview.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/dashboard/admin-overview.tsx) |
| 服务器与内核 | [node-hub.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node-hub.tsx)、[node.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node.tsx)、[kernel-panel.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/inbound/kernel-panel.tsx) |
| 节点与来源 | [inbound.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/inbound.tsx)、[sources.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/inbound/sources.tsx) |
| 中转、隧道、规则 | [relay.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/relay.tsx)、[tunnel.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/tunnel.tsx)、[forward.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/forward.tsx) |
| 订阅 | [subscription.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/subscription.tsx)、[my-subscriptions.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/my-subscriptions.tsx) |
| 用户与授权 | [user-hub.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/user-hub.tsx)、[user.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/user.tsx)、[group.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/group.tsx)、[limit.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/limit.tsx) |
| 监控 | [monitor.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/monitor.tsx)、[monitor-view.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/node/monitor-view.tsx) |
| 共享、设置、账户 | [panel-sharing.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/panel-sharing.tsx)、[config.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/config.tsx)、[profile.tsx](https://github.com/kure29/flyx/blob/79af4975727cf58c24ea0db3715048ee2f502436/vite-frontend/src/pages/profile.tsx) |

其他参考：[英文同版报告](flyx-pages-reference-report.en.md)、[架构对比报告（英文）](flyx-comparison-report.en.md)、[服务器模块报告（英文）](flyx-server-module-report.en.md)。本次仅新增中文文档。
