# v1.12.5

## 修复

- 节点策略的「发布到节点」只列出已保存配置和当前生效配置有差异的入口节点。预览使用只读连接，提交时重新比对，跳过已经生效的配置。
- Linux 安装器和更新器解析 GitHub 最新发布时直接匹配版本字段，避免长响应触发管道提前关闭并误报缺少 `tag_name`。
- Agent 与调优助手的 systemd 单元使用同一个只读 `boot_id` 挂载。Proxmox LXC 的核心交接、重启后的权限探针和调优请求使用同一开机身份。

## 迁移与兼容

无新迁移，迁移头保持 **0166**，最低兼容 **v1.7.5**。旧更新器遇到版本查询误报时，首次升级可显式指定 `--version 1.12.5`。已经遇到 LXC 开机身份冲突的旧单元按部署手册处理。

## 验证

- 使用独立面板和临时 Agent 验证德国 netcup 与日本 zouter 的 Xray TCP 流量。直连回显德国 IP，指定日本出站和按名称匹配的日本出站组均回显日本 IP。测试监听已经关闭，生产分流配置和防火墙规则保持原状。
- 验证策略发布预览隐藏已生效节点，仅显示改动入口。重提同一配置返回 `unchanged`。
- 在台湾 wap 节点，将 GitHub 返回的实际响应带入旧、新解析式，分别复现错误并验证标签提取。台湾 LXC 和日本 KVM 只读探针验证 boot_id 挂载。台湾终端升级从 v1.12.3 到 v1.12.4 成功，核心交接完成，服务在线且未自动重启。旧网页升级任务的回滚未获面板确认，因此面板保留任务占用并拒绝新的升级。网页实际升级结果未验证。

- Linux 主检查通过，用时 1078 秒。真实核心套件首轮出现 `TestRealOutboundGroupsProbeSelectionAndConnectionWeights/latency` 的 Xray 启动失败。按发布门禁只重跑 `internal/agent/core/xray`，74 项通过，用时 270 秒。
- 调优单元的早期提交因探针和执行单元注释不同触发一致性检查。统一注释后该用例通过，最终提交的完整 [GitHub Check](https://github.com/jingniao/qui/actions/runs/46038738679) 通过。

## 发布记录

- 标签 `v1.12.5` 对应 `68f9d313dd736ad6af06c3723cddc345ac806ff7`。[GitHub 发布任务](https://github.com/jingniao/qui/actions/runs/46041570320)
- [GitHub Release](https://github.com/jingniao/qui/releases/tag/v1.12.5) 于 2026-11-27 17:31:27 UTC 发布，当前为最新正式版，13 个附件齐全。核验 amd64 归档、独立 Agent 和清单的 SHA256，以及归档版本和 Agent 只读挂载。清单版本为 1.12.5，序号为 1012005。arm64 附件未逐一下载验证。
- 生产面板仍为 v1.12.4。台湾 v1.12.4 的主 Agent 已对齐开机身份，生产调优助手补丁等待授权。
