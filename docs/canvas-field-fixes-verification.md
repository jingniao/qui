# 画布实机问题修复与验证

日期：2026-11-14。范围为 Owner 指定的 A、B、C 修复及 D 调查。未连接生产或测试服务器，未部署，未使用子代理，未暂存、提交、推送或回退原工作区改动。没有新增依赖或迁移。

## A：完整撤除遗留空通道行

根因：`finishCanvasRemoval` 的最终事务只删除入口、绑定、通道对象及方案，没有删除 `canvas_tunnels`。`published_version` 因此仍在，投影继续显示 published。

修复：规划沿用现有草稿引用与历史发布引用，记录不共享的通道 ID。两阶段发布确认成功后，最终事务删除方案及对象，再删除这些候选中已无对象的通道行。删除语句从 `DeleteCanvasTunnel` 提取为内部函数，两个入口复用；没有增加第二套资源删除执行器。

最终事务复查方案引用、发布关联、对象归属及通道行，防止最后删除前出现的新引用。共享资源、外部引用导致保留的对象及其通道行继续保留。失败、取消、最终通道行删除失败都不会留下半提交的删除事务。

## B：空通道污染遗留资源分组

根因：遗留分组把所有无草稿引用的通道都按持久端点连接。历史空行与新通道共享端点，被合并成重复边，布局校验随后拒绝整组；页面按整组挂接，无法排除空行。

修复：无对象通道不参与端点连通，每条独立列为 `orphanTunnels`，标记 `empty=true`。空通道跳过布局还原校验，继续检查历史发布引用；无引用时 `canRemove=true`、`canReattach=false`。界面显示「清除空通道」，直接使用既有 `canvasTunnelAPI.delete`／`DeleteCanvasTunnel`。不可用操作隐藏。

有资源的组保持原流程：重新挂接为草稿，或挂接后进入完整撤除预览。

## C：请求合同、节点名称、路由摘要

1. 原代码直接比较版本，空值被当成过期版本。现在先校验 `updatedAt` 必填且非空，两个接口缺失／空值返回 400 `invalid_request`；有值但不匹配仍为 409 `stale_scheme`。
2. 撤除发布结果直接透出部署器的节点 ID／修订／状态，未经过普通画布发布的名称补充。现在发布前读取参与节点名，发布返回后补入 `publications[].nodes[].name`，失败返回中已有的 publication 也保留名称。
3. 原摘要只读默认出站和画布绑定；GOST 管理的监听默认出站为 direct，但实际转发由隧道配置承担，因此被误标「直连」。现在识别 `managedBy.kind=tunnel|rule`，摘要类型为 `tunnel`，显示「隧道转发」及出口节点。仍保持两次批量查询；已有 direct／relay／custom 自测保留。没有改入站运行配置。

## D：快速创建通道的传输与加密（只调查）

依据当前源码的完整数据链路：

1. `internal/panel/proxy/canvas_quick.go` 的 `quickCanvasScheme` 建立 `proxy_chain`、`TunnelProtocol=tls`、TCP 开启、UDP 关闭的连接。
2. `internal/panel/proxy/canvas_publish.go` 的 `canvasTunnels` 将 tls 映射到 trojan；但当目标是角色为 exit 的具体入站时，后续分支把 `protocol` 和 `core` 覆盖为该出口入站的协议与核心。
3. `internal/panel/proxy/canvas_ensure.go` 用这个覆盖后的协议创建内部中转入站，再创建指向它的出站。新中转入站的协议设置为空，没有复制出口用户入站的证书或加密方法。
4. `internal/panel/proxy/service.go` 只对 Trojan／Hysteria2／TUIC 自动启用 TLS。Shadowsocks 的空设置保持不带 TLS；生成出站复制新中转入站的设置。快照将这些设置传给核心。
5. Xray `internal/agent/core/xray/protocols.go` 渲染为 shadowsocks，空 transport/security 不生成 `streamSettings`，加密方法默认 `chacha20-ietf-poly1305`。sing-box `internal/agent/core/singbox/render.go` 同样渲染 shadowsocks，`cipher` 默认相同算法，空 security 不生成 TLS。

因此，无中间服务器、出口 IB 为 Shadowsocks 的快速创建分支，入口到出口使用 **Shadowsocks/TCP，ChaCha20-Poly1305 AEAD 加密，无额外 TLS**。`shadowsocks:36002` 是实际中转监听协议，不仅是名称标签。当前材料是源码渲染链路证据，本次未读取 t1／t3 已运行的配置。

说明书第 14 节的「通道为 TLS/TCP」与该分支不符。本次保留这句话及协议实现，待 Owner 决定：

- 保持现行为：修改说明书第 14 节及 `docs/v1.5.5-item15-design.md` 第 13 节，说明连接声明 TLS、末跳到具体出口入站时采用该入站协议，不能笼统承诺全部 TLS。
- 保持 TLS/TCP 产品承诺：后续修改规划器，不让用户出口协议覆盖内部通道协议，内部通道固定 Trojan/TLS，并补核心能力及协议验证。此方案将改变通道协议和运行资源，需要单独实施。

## 修改文件清单

以下仅列本轮增量，不把这些文件原有的未提交改动计入本轮。

| 文件 | 本轮修改 |
| --- | --- |
| `internal/panel/proxy/canvas_remove_plan.go` | 校验必填版本；按既有共享判断收集通道清理候选 |
| `internal/panel/proxy/canvas_remove.go` | 复查清理候选，补发布节点名，最终事务删除专用空通道 |
| `internal/panel/proxy/canvas_remove_guard.go` | 最终事务复查通道及方案引用等记录 |
| `internal/panel/proxy/canvas_tunnels.go` | 提取现有方案引用查询，供最终事务守卫复用 |
| `internal/panel/proxy/canvas_tunnel_objects.go` | 提取通道行删除内部函数，供旧删除接口与撤除事务共用 |
| `internal/panel/proxy/canvas_legacy.go` | 空行隔离分组及可清除状态 |
| `internal/panel/proxy/canvas_entry_projection.go` | 隧道／规则的只读转发摘要及出口投影 |
| `internal/panel/proxy/canvas_remove_apply_test.go` | 成功清理、节点名、失败保留、两阶段取消、删除事务失败回滚 |
| `internal/panel/proxy/canvas_remove_execution_test.go` | 共享行保留、仅历史发布引用保留、最终事务新增方案引用拒绝 |
| `internal/panel/proxy/canvas_remove_handler_test.go` | 两个接口缺失／空版本的 400 合同，保留过期 409 检查 |
| `internal/panel/proxy/canvas_legacy_test.go` | 历史空行与 S2 同端点不污染分组，可清除及重新挂接撤除；分叉／环路夹具补真实归属对象，原断言保留 |
| `internal/panel/proxy/canvas_entry_projection_test.go` | tunnel／rule 两类摘要与出口名断言 |
| `internal/panel/deployment/canvas_remove_integration_test.go` | 真实部署批次成功／失败的通道行状态与节点名断言 |
| `web/src/modules/proxy/api.ts` | tunnel 摘要类型、遗留组 empty 字段 |
| `web/src/modules/proxy/routeSummary.ts` | 「隧道转发」标签与路径显示 |
| `web/src/modules/proxy/legacyResources.ts` | 可清除空通道的操作判定 |
| `web/src/modules/proxy/components/relay/LegacyResourcesPanel.vue` | 清除空通道按钮及既有 DELETE 接口调用；未增加嵌套 template |
| `web/scripts/route-summary-selftest.mjs` | 隧道标签、路径及无画布链接的自测 |
| `web/scripts/canvas-legacy-selftest.mjs` | 空通道清除操作及被引用时隐藏的自测 |
| `AGENTS.md` | 第 14 节同步撤除、空遗留行、版本错误码与摘要语义；D 待决定的 TLS 原句未改 |
| `docs/superpowers/plans/2026-11-14-canvas-field-fixes.md` | 本轮计划及执行记录 |
| `docs/canvas-field-fixes-verification.md` | 本报告 |

## 验证与结尾输出

使用指定 Go 工具链、GOCACHE、GOPATH。所有 Go vet／test 均带 `-p 1`。先运行新增针对性回归，确认 A、B、C 的原错误实际失败，再实现修复。最终事务新增共享引用回归也经历失败到通过。

命令：

```text
go vet -p 1 ./internal/panel/proxy ./internal/panel/deployment
go test -count=1 -p 1 -timeout 45m ./internal/panel/proxy ./internal/panel/deployment
cd web
npm run typecheck
npm run build
逐个执行 scripts/*-selftest.mjs
```

结尾输出：

```text
go vet exit=0
ok  proxycontrol/internal/panel/proxy       10.050s
ok  proxycontrol/internal/panel/deployment   8.791s
go test exit=0

> proxy-admin-ui@1.0.0 typecheck
> vue-tsc --noEmit
typecheck exit=0

✓ built in 5.70s
build exit=0

PASS USER PORTAL LOGIN: two-step save (success, second-step failure, first-step failure), portal address, admin entry validation, wiring
remaining selftests PASS 25/25; total PASS 40/40
```

首次 build 与 latency-chart 自测被沙箱拒绝读取工作区父目录，esbuild 报 `Access is denied`。获准使用沙箱外执行后构建成功；已通过的前 15 项不重复，重跑受阻项并继续剩余 25 项，全部通过。

原始日志在 `.tools/canvas-field-vet.log`、`.tools/canvas-field-go-test.log`、`.tools/canvas-field-typecheck.log`、`.tools/canvas-field-build.log`、`.tools/canvas-field-selftests.log`、`.tools/canvas-field-selftests-rest.log`。前一份 selftest 日志保留首次沙箱权限失败，后一份记录重试及剩余项目。

本轮修改在请求范围内；未改迁移或备份结构。按 Owner 要求由当前会话自行复核，未启动审查子代理。待决定事项仅为 D 的文档／协议行为取舍；实机重新部署验收未运行。
