# v1.5.0 全面审计证据

对应 [全面审计报告](../../v1.5.0-comprehensive-audit-2026-11-10.md)，源码快照 `0b60eca7b3c57f72a8ddd7c1b6a86eafb95940eb`，测试机 `192.168.5.36`。证据中的账号、节点、凭据均为隔离测试材料，不是生产资源。

| 文件 | 用途 |
| --- | --- |
| [runner.log](runner.log)、[verification-summary.json](verification-summary.json) | 标准门禁、真实核心、签名阶段的时间与结论；补充测试总览 |
| [api-audit.json](api-audit.json) | 24 组权限/CSRF/遮蔽/协议补齐/地址/抓取/路径检查 |
| [browser-audit.json](browser-audit.json) | 真实浏览器的 9 项结果；其中 5 项复现产品缺陷 |
| [ui-tuning-status.png](ui-tuning-status.png)、[ui-tuning-adopt-error.png](ui-tuning-adopt-error.png)、[ui-viewer-mobile.png](ui-viewer-mobile.png) | 调优误判、字段缺失、无错误反馈以及 Viewer 移动视图 |
| [helper-summary.log](helper-summary.log) | 直接 helper、打包 systemd 沙箱、仅扩充测试单元写路径的真实内核对照 |
| [hy2-requests.csv](hy2-requests.csv) | 双客户端逐轮 HTTP 结果，各 24/24；端口集合见 verification-summary.json |
| [audit-overlays.log](audit-overlays.log) | 两个归档拒绝测试、一个导入版本绑定测试；3 项失败表示复现缺陷 |
| [old-agent-compat.log](old-agent-compat.log) | 当前门禁放行，但 v1.4.7 严格解码拒绝禁用实体新字段 |
| [prior-probes-linux.log](prior-probes-linux.log)、[real-cache-inspect.log](real-cache-inspect.log) | Vault、缓存身份、通知状态复现；实际 34 条来源缓存含目标域名的确认 |
| [certificate-async.json](certificate-async.json) | 提取实际 SFC 异步函数、注入网络/定时器后的域名串用复现 |
| [actual-binary-probes.log](actual-binary-probes.log) | 补齐实际 Agent/Panel 二进制后通过的预检和启动恢复场景 |
| [fuzz-rules.log](fuzz-rules.log)、[cross-arm64.log](cross-arm64.log) | 有限 fuzz 与交叉编译，不代表原生 arm64 或全部输入已验证 |
| [go-vuln-summary.json](go-vuln-summary.json) | Go 1.26.0 两个实际入口的符号级扫描命中摘要，不是可利用性证明 |
| [npm-audit.json](npm-audit.json)、[npm-audit-production.json](npm-audit-production.json) | 全依赖与 omit-dev 的独立扫描结果 |
| [baseline.txt](baseline.txt)、[cleanup.log](cleanup.log) | 主机前后状态、审计资源清理确认 |
| [reproduction-sources.zip](reproduction-sources.zip) | 本轮新增的回归、探针和实验脚本；不含运行时密码/Cookie/私钥 |
| [SHA256SUMS.txt](SHA256SUMS.txt) | 本目录公开文件 SHA-256 |
| [raw-log-hashes.json](raw-log-hashes.json) | 较大原始日志的位置、字节数与摘要 |

## 复核说明

原始不变源码、补充测试源码分开放置，防止新增失败回归污染“原仓库门禁”的结论。ZIP 内路径和端口绑定本次实验环境，复用前应阅读脚本，在全新隔离副本上调整；不要直接对生产执行。审计没有修改服务实现。

完整原始日志保留在本地 `.tools/v150-audit/evidence/linux/` 和测试机 `/root/qui-audit/20261110-0b60eca/evidence/`。Cookie/临时密码已删除，已关闭的测试业务库也已删除；公开包不包含这些材料。
