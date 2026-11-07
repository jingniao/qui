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
| 备份恢复、证书、额度、故障处理 | [运维手册](operations.md) |
| 完成标准、证据要求与实机验收结果 | [验收矩阵](v1-acceptance.md) |
| 1.2.0 发布说明 | [发布说明 1.2.0](release-notes-1.2.0.md) |
| 1.3.0 发布说明 | [发布说明 1.3.0](release-notes-1.3.0.md) |
| 1.3.1 发布说明（缺陷修复，2026-11-03） | [发布说明 1.3.1](release-notes-1.3.1.md) |
| 1.4.0 发布说明（2026-11-07） | [发布说明 1.4.0](release-notes-1.4.0.md) |
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

历史计划、阶段报告、走查记录与进度日志在 [docs/archive](archive/README.md)，只作追溯。上表 v1.4 计划/报告为当前工作文档，v1.4.0 已发布而生产尚未升级，A1 验收待补；计划中的阶段状态不代替报告及源码。正式版为 v1.4.0。当前事实与已知限制见[项目说明书](../AGENTS.md) 的“当前状态”。

维护规则：现状写结论，计划写任务，验收写证据。日常只看上面表格中的几份文档。文档内容以当前源码为准；文档与源码冲突时以源码为准，并在同一次改动中修正文档。
