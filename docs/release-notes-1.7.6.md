# v1.7.6 发布说明

已正式发布 v1.7.6，未升级生产。迁移头保持 **0158**，最低兼容版本为 **v1.7.5**。

## 变更

- 删除未引用的前端页面、API、类型、辅助函数，以及后端死代码和失效分支；共用字节换算、面板时区日期、节点运行状态和规则集引用弹窗。门户两处缺少面板时区来源的时间展示仍使用浏览器时间。
- 停止为新任务排队旧 worker 升级，保留 v1.7.5 既有签名任务的派发、重发、结果验真、恢复 API 与尝试记录。
- Enroll 与 Hello 明确拒绝低于 1.7.5、dev 或缺失版本的 Agent；Enroll 在证书签发及注册码消耗前检查。Agent 镜像探测要求 v2 storage 与 control-only。
- 集中产品说明、架构、运维与实施决定，精简重复历史文档；旧发布记录仍可从 Git 历史和 GitHub Releases 查阅。
- 一并发布工作区已有的流量交付查询优化及服务器财务卡片缓存版本调整。

## 兼容与数据

无新增迁移，0152–0158 迁移内容保持不变。只接受 v1.7.5 或更新版本的数据库与备份；保留 v1.7.5 接受的有效数据和在途任务，包括安装记录 v1、user_source_grants、max_devices/max_connections、历史 budget/weighted 修订、旧 checksum 链、legacyRelayNodes、只读 port_forward/GOST 画布和 importKey 幂等去重。

发布前复审撤回了未发布的 0159 清理迁移，并保留 PreparedRestore.Addresses、agentprocess.Start 和旧 worker 恢复路径。

## 检查与首轮失败

发布标签 v1.7.6 → `412028296bf8dcb653c5bee3ba6dde8e6dcd4eac`。同 SHA 的 Linux 完整门禁通过：常规检查 812 秒，真实核心 247 秒，总计 813 秒；[GitHub check 44873775631](https://github.com/jingniao/qui/actions/runs/44873775631) 的四个分片、汇总 check、官方及 qui 真实核心与发布试构建通过。

提交前前端类型检查、自测与构建通过；Go 测试编译发现重复变量声明及两个残留导入，修复后受影响包的 go vet 和用户身份/删除两项定向测试通过。旧删除接口测试经 Owner 允许，改为真实路由检查 404，数据和撤权断言保留。前端首次构建遇到本机沙箱目录访问限制，重跑构建通过。

首候选 `8e423c9` 的 [GitHub 44872897206](https://github.com/jingniao/qui/actions/runs/44872897206) server 分片及 Linux 真实核心失败：测试 Agent 使用 dev 或不满足最低版本的测试名称，旧接口/字段测试仍调用已移除合同。首轮失败日志保留，未打标签；取代该提交后终止剩余门禁。

Owner 明确要求删除这批测试。[提交 4120282](https://github.com/jingniao/qui/commit/412028296bf8dcb653c5bee3ba6dde8e6dcd4eac) 删除 18 个文件中的 23 个失败测试，涵盖旧设备路由、旧规则模板写入口、nodeIds 请求，以及使用旧版本夹具的注册/重连、真实多核心代理链、流量补传、自动证书续期、恢复后重连、备份和权限集成场景。其他测试和运行逻辑保留。**这些场景的测试覆盖已减少，本版通过的门禁不包含这 23 个测试。**

## 发布附件

[Release 工作流](https://github.com/jingniao/qui/actions/runs/44875282698) 成功，于 **2026-11-21T06:32:08Z** [正式发布 v1.7.6](https://github.com/jingniao/qui/releases/tag/v1.7.6)。13 个附件实际下载，文件大小、GitHub SHA-256 与 SHA256SUMS 的 10 项摘要通过；双架构 Agent 官方 Ed25519 签名有效，版本 1.7.6、序号 **1007006**，原始二进制与包内 Agent 相同。面板与 Agent 的 ELF 架构、干净源码提交、安装脚本及 sing-box 来源摘要通过核验。

隔离 Linux 测试机仅执行 amd64 面板/Agent 的 --version，均返回 1.7.6。arm64 未运行原生实机验收。新 root 桥接独立 systemd/OpenRC 生命周期、断电/重启等既有未验收边界见[实施决定](implementation-decisions.md)。

## 生产

本次仅推送与发布，未连接、升级或修改生产配置；未新建生产恢复点。生产实际版本与节点状态需在后续获准升级前重新核实。
