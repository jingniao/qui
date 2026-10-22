# 文档导航

计划类文档的入口是[轻量重构计划](lightweight-refactor-plan.md)（15 个阶段已完成，第 9 节记录计划结束后的改动），它取代此前的现状、收尾计划和过程文档。接手项目时先读[接手说明](agent-handoff.md)。

## 日常入口

| 目的 | 文档 |
| --- | --- |
| 产品需求和架构原则 | [项目说明书](../AGENTS.md) |
| 项目首页 | [项目首页](../README.md) |
| 系统组成、模块边界和数据流 | [架构总览](architecture.md) |
| 前端设计系统、视觉交互与按钮规范 | [设计系统规范](design-system.md) |
| 节点删除功能规划（英文；**历史文档**，已按精简方案实现并被取代，现行行为见 [AGENTS.md 第 11.3 节](../AGENTS.md) 与 [运维手册 第 15 节](operations.md)） | [Node Deletion Implementation Plan](node-deletion-plan.en.md) |
| 安装面板、接入节点、升级 | [部署与升级](deployment.md) |
| 备份恢复、证书、额度、故障处理 | [运维手册](operations.md) |
| 协议与能力支持范围 | [协议支持矩阵](protocol-support-matrix.md) |
| 最终验收标准和证据要求 | [验收矩阵](v1-acceptance.md) |
| 重构方向、阶段、进度清单 | [轻量重构计划](lightweight-refactor-plan.md) |
| 交接给下一位实现者：现状、入口与规则 | [接手说明](agent-handoff.md) |

维护规则：现状写结论，计划写任务，验收写证据。日常只看上面“日常入口”中的几份文档。文档内容以当前源码为准；文档与源码冲突时以源码为准，并在同一次改动中修正文档。
