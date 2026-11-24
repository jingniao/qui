# 文档导航

当前正式版为 **v1.12.0**（tag → `2c4f8a3`，迁移头 0165），兼容底线为 v1.7.5。本次只读核实生产，未部署升级；生产状态以[运维手册「当前生产环境」](operations.md#当前生产环境)为准，操作前须重新实机核实。

## 现行规则与操作

| 目的 | 文档 |
| --- | --- |
| 项目介绍／协作要求 | [项目首页](../README.md)、[AGENTS.md](../AGENTS.md) |
| 产品规则、字段、权限和数据恢复合同 | [项目说明书](project-spec.md) |
| 模块、数据流和生命周期 | [架构总览](architecture.md) |
| 从零安装、节点接入和日常使用 | [安装指南](install-guide.md) |
| 构建、安装、更新、目录与权限 | [部署与升级](deployment.md) |
| 日常运维、备份恢复、排错与生产未结记录 | [运维手册](operations.md)、[历史未结记录](operations.md#历史未结记录与恢复材料保护2026-11-20-文档核对) |
| 协议矩阵、设备执行范围、UI 设计规范 | [协议矩阵](protocol-support-matrix.md)、[设备限制](device-limit.md)、[设计系统](design-system.md) |
| 检查、门禁与发布流程 | [发布手册](release-runbook.md) |

## 发布、决策与归档

[docs/releases/](releases/) 存放各版本发布说明；[docs/superpowers/](superpowers/) 存放进行中的设计与计划，对应版本发布后移入 [docs/archive/](archive/)。

| 目的 | 文档 |
| --- | --- |
| 当前正式版 v1.12.0 | [发布说明](releases/release-notes-1.12.0.md)（重装接替旧更新、配置恢复及发布证据） |
| 上一正式版 v1.11.0 | [GitHub Release](https://github.com/jingniao/qui/releases/tag/v1.11.0)（更新状态 Go 下沉、存储整理及告警改进） |
| 历史正式版 v1.10.4 | [发布说明](releases/release-notes-1.10.4.md)（节点存活判定、数据库 Go 工具及发布证据） |
| 历史正式版 v1.10.2 | [发布说明](releases/release-notes-1.10.2.md)（离线误报、Agent 升级重置及发布门禁） |
| 历史正式版 v1.10.1 | [发布说明](releases/release-notes-1.10.1.md)（旧 Agent 状态换代、分层存储及发布门禁） |
| 历史正式版 v1.10.0 | [发布说明](releases/release-notes-1.10.0.md)（Agent 核心交接、崩溃恢复、币种扩展及发布证据） |
| 历史正式版 v1.9.0 | [发布说明](releases/release-notes-1.9.0.md)（可选内核节点、自动续费、实机验收与发布证据） |
| 历史正式版 v1.8.0 | [发布说明](releases/release-notes-1.8.0.md)（独立出口探测、迁移、门禁与附件核验） |
| 历史正式版 v1.7.6 | [发布说明](releases/release-notes-1.7.6.md)（含测试删除、门禁与附件核验） |
| 当前兼容基线 v1.7.5 | [发布说明](releases/release-notes-1.7.5.md)、[验证记录](v1.7.5-verification.md) |
| 历次版本变更摘要 | [变更记录](changelog.md)（完整旧发布说明与验收日志留存于 Git 历史及 GitHub Releases） |
| 核心设计不变式、防踩坑备忘与未验证边界 | [实施决定](implementation-decisions.md) |
| 审查后续设计 | [节点存活判定提案](superpowers/specs/2026-11-23-node-liveness-design.md)（待决定）、[更新器渐进下沉](superpowers/specs/2026-11-23-updater-go-design.md)（B3 已选方案 B，后续按需实施） |
| 原始底层探测与已发布设计、计划 | [归档索引](archive/README.md) |
| 测试夹具生成与维护说明 | [身份 Write=2](../internal/agent/identity/testdata/generate_store/README.md)、[旧备份负向夹具](../internal/platform/server/testdata/generate_prerefactor/README.md) |
| 第三方资源许可 | [DB-IP](../internal/panel/geoip/LICENSE-DB-IP.md)、[Twemoji 字体](../web/src/styles/fonts/LICENSE-TwemojiCountryFlags.md) |
