# 文档导航

当前正式版为 **v1.7.5／迁移头 0158**，工作区精简尚未发布，后续兼容底线为 v1.7.5。生产状态以[项目说明书的最近核实记录](project-spec.md#当前状态)为准，操作前须重新实机核实。

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
| 当前未发布代码精简交接（发版后移除） | [精简实施记录](code-simplification.md) |

## 发布、决策与归档

| 目的 | 文档 |
| --- | --- |
| 当前兼容基线 v1.7.5 | [发布说明](release-notes-1.7.5.md)、[验证记录](v1.7.5-verification.md) |
| 历次版本变更摘要 | [变更记录](changelog.md)（完整旧发布说明与验收日志留存于 Git 历史及 GitHub Releases） |
| 核心设计不变式、防踩坑备忘与未验证边界 | [实施决定](implementation-decisions.md) |
| 保留的两份原始底层探测（v1.1 Phase 0／GOST 3.3.2） | [归档索引](archive/README.md) |
| 测试夹具生成与维护说明 | [身份 Write=2](../internal/agent/identity/testdata/generate_store/README.md)、[旧备份负向夹具](../internal/platform/server/testdata/generate_prerefactor/README.md) |
| 第三方资源许可 | [DB-IP](../internal/panel/geoip/LICENSE-DB-IP.md)、[Twemoji 字体](../web/src/styles/fonts/LICENSE-TwemojiCountryFlags.md) |
