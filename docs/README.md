# 文档导航

| 目的 | 文档 |
| --- | --- |
| 产品需求、架构原则、字段口径与当前状态 | [项目说明书](../AGENTS.md) |
| 项目首页 | [项目首页](../README.md) |
| 系统组成、模块边界和数据流 | [架构总览](architecture.md) |
| 前端设计系统、视觉交互与按钮规范 | [设计系统规范](design-system.md) |
| 并发设备限制的判定方式与执行范围 | [设备限制](device-limit.md) |
| 协议与能力支持范围 | [协议支持矩阵](protocol-support-matrix.md) |
| 安装面板、接入节点、升级 | [部署与升级](deployment.md) |
| 从零开始：一键安装面板、加节点、日常操作与常见问题 | [项目安装与使用指南](install-guide.md) |
| 备份恢复、证书、额度、故障处理 | [运维手册](operations.md) |
| 提交、门禁、发布与升级生产的步骤（交接用） | [发布流程手册](release-runbook.md) |
| 完成标准、证据要求与实机验收结果 | [验收矩阵](v1-acceptance.md) |
| 1.2.0 发布说明 | [发布说明 1.2.0](release-notes-1.2.0.md) |
| 1.3.0 发布说明 | [发布说明 1.3.0](release-notes-1.3.0.md) |
| 1.3.1 发布说明（缺陷修复，2026-11-03） | [发布说明 1.3.1](release-notes-1.3.1.md) |
| 1.4.0 发布说明（2026-11-07） | [发布说明 1.4.0](release-notes-1.4.0.md) |
| 1.4.1 发布说明（补丁版，2026-11-08） | [发布说明 1.4.1](release-notes-1.4.1.md) |
| 1.4.2 发布说明（补丁版，2026-11-08） | [发布说明 1.4.2](release-notes-1.4.2.md) |
| 1.4.3 发布说明（补丁版，2026-11-08） | [发布说明 1.4.3](release-notes-1.4.3.md) |
| 1.4.4 发布说明（补丁版，2026-11-08） | [发布说明 1.4.4](release-notes-1.4.4.md) |
| 1.4.6 发布说明（补丁版，2026-11-09） | [发布说明 1.4.6](release-notes-1.4.6.md) |
| 1.4.7 发布说明（迁移 0135–0137，2026-11-09，生产未升级） | [发布说明 1.4.7](release-notes-1.4.7.md) |
| 1.5.0 发布说明（迁移 0138–0141，2026-11-11，生产未升级；Agent 从 ≤1.4.7 升级需运行两次 qui-update） | [发布说明 1.5.0](release-notes-1.5.0.md) |
| 1.5.1 发布说明（补丁版，无迁移，2026-11-11，生产未升级） | [发布说明 1.5.1](release-notes-1.5.1.md) |
| v1.5.1 受管 Agent 实机验收（`--web-upgrade` 安装、受管节点发布显式核心入站） | [受管 Agent 实机验收](v1.5.1-managed-agent-verification.md) |
| v1.5.0 验收：Alpine/OpenRC、systemd 调优、通知、Agent 网页升级、浏览器走查 | [Alpine/OpenRC](v1.5.0-alpine-openrc-verification.md)、[systemd 调优](v1.5.0-systemd-tuning-verification.md)、[通知](v1.5.0-notification-verification.md)、[Agent 网页升级](v1.5.0-agent-web-upgrade-verification.md)、[浏览器走查](v1.5.0-browser-walkthrough.md) |
| v1.5.0 审计与 Agent 网页升级启用修复方案 | [审计](v1.5.0-audit-2026-11-10.md)、[全面审计](v1.5.0-comprehensive-audit-2026-11-10.md)、[启用修复方案](v1.5.0-agent-supervision-plan.md) |
| v1.4 当前工作计划、决策与 API 合同 | [v1.4 计划](v1.4-plan.md) |
| v1.4 订阅导入与 Reality/xhttp 开发验证、未验收边界 | [协议兼容验证记录](v1.4-protocol-verification.md) |
| v1.4 离线 RC 生产部署、Agent 重连及回滚验收（部分完成） | [A1 主机记录](v1.4-a1-host-report.md) |
| v1.4 rc.2 面板收尾、恢复点与生产浏览器实际耗时 | [rc.2 收尾报告](v1.4-rc2-closeout.md)、[慢加载复测](page-loading-verification.md) |
| Alpine NAT 映射和 UK→Alpine 生产加密隧道实流 | [端口检查](alpine-network-verification.md)、[隧道验证](uk-alpine-tunnel-verification.md) |
| UK→Alpine→US 三节点 GOST 实流、失败回滚及中间 Agent 重启恢复 | [三节点主机验收](three-hop-host-verification.md) |
| 自建 VLESS / Reality 客户端认证、密钥轮换及历史回滚 | [客户端验收报告](vless-reality-client-verification.md) |
| 画布 P2/P3、关联规则 P4 的生产客户端实流与错误认证 | [画布及 P4 客户端报告](canvas-p2-p3-p4-client-verification.md) |
| v1.4 rc.3 面板收尾、导入消费、恢复／降级、浏览器、race 与长测结果 | [最终补充验收](final-v1.4-acceptance.md) |
| v1.4 GOST 中间跳契约与运行待测项 | [X0 记录](v1.4-x0-findings.md) |
| v1.4 隧道后端、发布、节点集成与订阅 | [X1a/X1b/N1 报告](v1.4-x1a-report.md) |
| v1.4 代理中转画布收敛 | [C1 报告](v1.4-c1-report.md) |
| v1.4 一次性迁移、备份与恢复 | [M1 报告](v1.4-m1-report.md) |
| 历次修订的完整条目 | [变更记录](changelog.md) |
| 构建与测试提速方案 A（**规划，尚未实施**） | [提速方案](build-acceleration-plan.md) |
| v1.4.7 规划（**已发布，待生产升级与实机验收**）：节点组自动下发凭据、节点连接地址、证书来源与自动申请 | [v1.4.7 规划](v1.4.7-plan.md) |
| v1.5.0 规划（**已发布（见发布说明），生产尚未升级**）：Agent 网页一键升级、多公网地址、通知系统、发布状态、去掉用户协议控制、按域名找证书、Hysteria2 端口跳跃、用途组 @all、规则集外部来源、服务器调优完善 | [v1.5.0 规划](v1.5.0-plan.md) |
| v1.5.1 规划（**已随 v1.5.1 发布，生产尚未升级**）：规则集页面补关键字与外部来源、修复在该页面保存会清掉关键字；修复受管 Agent 无法发布显式核心入站（`apply_failed`）；安装命令对话框去掉面板地址与端口输入、可勾选启用网页升级并修复新装未真正启用；Windows 上国旗显示；用户详情只列可用订阅节点；监控 CPU／网速大多为空的修复与图表改版；节点列表按入站显示是否生效 | [v1.5.1 规划](v1.5.1-plan.md) |
| v1.5.2 规划（**规划，尚未实施**）：监控页面反复读取历史指标并短暂清空图表、「版本与更新」页国旗；另列全协议实测发现的待确认候选 | [v1.5.2 规划](v1.5.2-plan.md) |

历史计划、阶段报告、走查记录与进度日志在 [docs/archive](archive/README.md)，只作追溯。上表 v1.4 计划/报告为当前工作文档，v1.4.0 于 2026-11-07 发布并升级到生产、v1.4.1 于 2026-11-08 发布并升级，v1.4.2 于 2026-11-08 发布并升级到生产，v1.4.3 于 2026-11-08 发布并升级到生产，v1.4.4 于 2026-11-08 发布并升级到生产，v1.4.5（只升面板）与 v1.4.6 于 2026-11-09 发布并升级到生产，v1.4.7 于 2026-11-09 发布、生产尚未升级，v1.5.0 与 v1.5.1 于 2026-11-11 发布、生产尚未升级，A1 验收待补；计划中的阶段状态不代替报告及源码。正式版为 v1.5.1。当前事实与已知限制见[项目说明书](../AGENTS.md) 的“当前状态”。

维护规则：现状写结论，计划写任务，验收写证据。日常只看上面表格中的几份文档。文档内容以当前源码为准；文档与源码冲突时以源码为准，并在同一次改动中修正文档。
