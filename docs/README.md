# 文档导航

当前正式版 **[v1.12.1](releases/release-notes-1.12.1.md)**（tag `ccebc95`，迁移头 **0165**），最低兼容 **v1.7.5**。生产运行状态单独登记在[运维手册](operations.md#当前生产环境)，生产操作前须重新实机核实。

## 按任务阅读

| 任务 | 文档与职责 |
| --- | --- |
| 了解项目与协作要求 | [项目首页](../README.md)、[AGENTS.md](../AGENTS.md) |
| 从零安装并开始使用 | [安装指南](install-guide.md)：面板、节点、用户与订阅 |
| 开发、手工安装或升级 | [部署与升级](deployment.md)：构建、脚本参数、升级恢复、目录与权限 |
| 日常检查与排错 | [运维手册](operations.md)：生产登记、备份恢复、证书、日志与历史未结事项 |
| 理解代码组织与数据流 | [架构总览](architecture.md)：模块入口、关键链路和生命周期 |
| 确认产品行为与接口 | [项目说明书](project-spec.md)：字段、权限、API、安全及恢复规则 |
| 查询协议与执行边界 | [协议矩阵](protocol-support-matrix.md)、[设备限制](device-limit.md) |
| 修改界面 | [设计系统](design-system.md)：控件、排版、颜色与协议徽标 |
| 检查与发布版本 | [发布手册](release-runbook.md)：门禁、提交、标签、附件核验与生产授权 |
| 查约束与未验收事项 | [实施决定](implementation-decisions.md)：设计不变式、已接受限制与实机边界 |

## 发布与历史

| 范围 | 入口 |
| --- | --- |
| 当前正式版 v1.12.1 | [发布说明](releases/release-notes-1.12.1.md)：锁识别、UDP 提示与升级收尾时序 |
| v1.12.0 | [发布说明](releases/release-notes-1.12.0.md)：重装接替旧更新任务 |
| v1.11.0 | [GitHub Release](https://github.com/jingniao/qui/releases/tag/v1.11.0)：更新状态 Go 下沉、存储整理与告警 |
| v1.7.5 起的版本摘要 | [变更记录](changelog.md)，各版证据见 [releases/](releases/) |
| 兼容基线 v1.7.5 | [发布说明](releases/release-notes-1.7.5.md)、[验证记录](v1.7.5-verification.md) |
| v1.7.5 以前及底层调查 | [归档索引](archive/README.md) |
| 设计与实施过程 | [superpowers/](superpowers/) 保留近期记录，[archive/](archive/README.md) 保留已归档材料；按文首状态和对应发布说明判断进度 |

节点存活判定已随 v1.10.4 发布，更新器数据库工具随 v1.10.4 发布、状态工具随 v1.11.0 发布。设计背景见[存活判定](superpowers/specs/2026-11-23-node-liveness-design.md)与[更新器渐进下沉](superpowers/specs/2026-11-23-updater-go-design.md)；历史计划中的未勾选项不代表当前仍未实现。

测试夹具维护：[身份 Write=2](../internal/agent/identity/testdata/generate_store/README.md)、[旧备份负向夹具](../internal/platform/server/testdata/generate_prerefactor/README.md)。第三方资源许可：[DB-IP](../internal/panel/geoip/LICENSE-DB-IP.md)、[Twemoji 字体](../web/src/styles/fonts/LICENSE-TwemojiCountryFlags.md)。

## 文档维护

- 行为变更同步项目说明书；架构只概述实现与入口，操作命令分别放在安装、部署、运维和发布手册。
- 版本摘要写入变更记录，门禁、失败与附件核验保留在对应发布说明或原始实施记录中。
- 生产事实只在运维手册维护，并注明核实时间；发布版本与生产运行版本分别记录，管理员入口不入文档。
- 归档保留原日期与结论，通过链接补充后续结果；不把历史授权视为新任务的操作授权。
