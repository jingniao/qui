# 历史文档归档

这里保留有长期工程价值的原始底层调查及已发布的设计与计划，不随代码更新。现行规则见[文档导航](../README.md)；版本、环境与未验证限制按报告原日期读取。

[docs/releases/](../releases/) 存放各版本发布说明；[docs/superpowers/](../superpowers/) 存放进行中的设计与计划，对应版本发布后移入本目录 `docs/archive/`。

| 原始报告 / 设计 | 保留原因 |
| --- | --- |
| [v1.1 Phase 0](v1.1-phase0-findings.md) | sing-box／Xray／mihomo／旧 Clash 上游行为、计数与来源审计（被 Go 代码注释直接引用 `item 1`、`item 2e`、`A.7`） |
| [GOST 3.3.2 探测](v1.3-g0-gost-findings.md) | 当前固定核心 GOST 3.3.2 的配置陷阱、认证拒绝、计量与生命周期原始测量，也是后续升级 GOST 的核对清单 |
| [核心交接设计](core-handover-plan.md) | v1.10.0 已发布的核心交接方案、实施与验收证据 |
| [可选内核节点设计](core-optional-nodes-plan.md) | v1.9.0 已发布的可选内核节点方案与验收记录 |

其余早期版本交接、历史验收报告与已落地设计均已清理（完整原文保留于 Git 历史），仍然生效的架构约束、防踩坑备忘与未验证边界已集中至[实施决定](../implementation-decisions.md)，生产未结事项见[运维手册](../operations.md#历史未结记录与恢复材料保护2026-11-20-文档核对)。
