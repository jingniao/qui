# v1.12.3 发布说明

## 修复

- 修复发布时漏读入站草稿的出口 IP。面板发布配置时会使用草稿中保存的出口 IP。
- Agent 随心跳检查运行修订用到的出口地址和网卡。已配置的地址或网卡从主机消失时，面板显示「出口 IP 缺失」。恢复地址或发布可用的新配置后，受影响的连接可恢复。[运维手册](../operations.md#9-节点出站网络)
- host-network helper 声明 `panel_exempt` 时，发往面板控制地址的连接自动豁免整机强制出站源地址。强制出站地址失效时，Agent 仍可通过控制连接接收修正配置。

## 迁移与兼容

标签 `v1.12.3` 指向 `9a6d731`，无新迁移，迁移头为 **0165**，最低兼容 **v1.7.5**。

## 发布证据

- GitHub Release 发布于 2026-11-25 10:08:50 UTC，共 13 个附件。[GitHub Release](https://github.com/jingniao/qui/releases/tag/v1.12.3)
- 同提交的 GitHub Check 有两次成功记录。[推送触发](https://github.com/jingniao/qui/actions/runs/45590622768) · [手动触发](https://github.com/jingniao/qui/actions/runs/45587361236)
- Linux 完整门禁未找到记录。
