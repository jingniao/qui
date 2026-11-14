# v1.5.5 Release Closeout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans for the assigned scope.

**Goal:** 修复发布前验收阻塞，取得同一提交的 Linux 与 GitHub 门禁通过证据，发布 v1.5.5 并核对发布资产。

**Architecture:** 沿用 v1.5.5 已确定的模型和接口；导入格式保留真实参数，高级画布保留并编辑既有入口绑定，Agent 在应用边界验证本机源地址。发布失败要可见，不改为默认成功。修复独立并行，统一门禁。

**Tech Stack:** Go、Vue/TypeScript、SQLite、Linux 核心与部署脚本、GitHub Actions。

**Spec:** `docs/v1.5.5-plan.md`、`docs/v1.5.5-acceptance.md`、`docs/v1.5.5-item15-design.md`。

## Global Constraints

- Owner 2026-11-14 指示“一直完成到发布1.5.5”，覆盖必要的修复、验证、提交、推送、tag 和 Release；不包含生产升级。
- 保留工作区已有 v1.5.5 修改，只处理本版范围；不执行 reset/checkout/clean/stash。
- 不新增正式测试方法、测试实体或 mock；优先扩展既有用例、使用已有临时复现脚本，按失败→修复→最窄验证记录。
- 使用既有工作分支集成；Linux/客户端使用隔离副本和明确测试主机。禁止以生产主机作为验收对象。
- 同 SHA 本地门禁与 GitHub check 成功后才推 v1.5.5 tag；不隐藏失败或未覆盖环境。

## Review Focus

- TUIC 非默认 ALPN、拥塞控制与 UDP relay 在所有已支持格式中往返。
- 高级画布保存未修改的绑定、create/reuse 切换、服务器切换及撤除恢复。
- 无效本地源 IP 在应用前失败，保留最后成功配置；Xray/sing-box 共享边界与继承值一致。
- ACK 测试等待客户端持久确认，保留真正的重连与持久化断言。
- 节点安装参数测试在 root 下也不得越过边界执行真实安装；完整门禁包含全部现有前端自测。

## Tasks

- [ ] 修复 TUIC 解析和三个导出器；跑现有格式往返复现与直接相关包。
- [ ] 补齐高级画布入口绑定读写和编辑；跑既有画布自测、类型检查与页面验收。
- [ ] 修复 Agent 源地址应用校验；补现有用例断言并复验无效地址回滚。
- [ ] 修正 ACK 同步等待与节点安装测试边界；核对门禁前端自测清单。
- [ ] 独立审查变更，构建测试 RC，补可执行的验收并如实登记环境限制。
- [ ] 整理全部本版文件、检查秘密与脚本权限；提交推送工作分支并快进 main。
- [ ] 对发布提交并行运行 Linux 完整门禁与 GitHub check（含 cores），修复真失败直至同 SHA 通过。
- [ ] 推送 v1.5.5 tag，核对 Release 资产、哈希与 Agent 签名，更新发布文档并推送。
