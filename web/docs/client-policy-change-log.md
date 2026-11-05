# 客户端策略（Client Policy）前端页面修改记录

本记录文件遵照规范统一维护，按轮次追加，完整记录“分流控制 → 客户端策略”前端页面的所有修改需求、范围、文件变动、交互和表单优化、验证结果及限制。

---

## 轮次 1：客户端策略页面信息层级、未保存状态感知、操作路径与窄屏交互优化

- **日期和时间**：2026-11-04T04:40:00-08:00（UTC-08:00）
- **本轮原始需求**：
  修改“分流控制 → 客户端策略”前端页面（`web/src/modules/routing/pages/ClientPolicyPage.vue`）。在严格限定于 `web/` 目录且不改变后端 API 与契约的前提下：
  1. 改善页面信息层级与操作路径，优化顶部操作栏布局与防误触机制；
  2. 强化未保存状态（Dirty State）感知与防丢失提醒，明确区分界面草稿与后端已保存配置，提供底部浮动保存/放弃修改操作条及 YAML 预览区的未保存提示；
  3. 完善策略组管理交互，增加策略组删除二次确认弹窗、组名重名/空值校验提示、平滑的移动与快速标签追加反馈；
  4. 优化规则编辑抽屉（FormDrawer）的字段级即时校验与帮助提示，针对不同规则类型（DOMAIN、IP-CIDR、RULE-SET、DST-PORT、MATCH 等）提供精准的输入指导与错误拦截；
  5. 优化窄屏与移动端响应式布局（Tab 导航防折行滚动、策略组卡片纵向自适应、表格操作列友好尺寸）；
  6. 保留所有原有的保存重试、stale_rules 自动拉取、字段错误高亮、权限保护及 YAML 预览能力。
- **涉及的全部文件路径**：
  - 新增：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **顶部 Header 与状态徽标**：
       - 修改前：头部仅有静态文字说明，未直观展现当前策略是“已与订阅同步”还是“存在未保存修改”；右侧按钮直接平铺，缺少未保存全局感知。
       - 修改后：在 Header 增加 `#badge` 插槽，当 `isDirty` 为真时展现橙色告警徽标（“未保存修改”+脉冲红点），未修改时展现绿色徽标（“已同步订阅配置”）；保持右侧操作栏兼容既有测试断言，确保小屏响应式折行与标准操作流（恢复默认模板、添加规则、保存策略配置、策略模拟）。
     - **底部未保存修改浮动感知栏 (Unsaved Changes Floating Bar)**：
       - 修改前：修改了规则或参数后，仅右上角保存按钮变色，页面长滚动到规则列表或代码预览区时，用户无法感知到当前处于编辑草稿态。
       - 修改后：使用 `<Teleport to="body">` 增加常驻浮动底栏，当 `isDirty` 为真时弹出，提示“当前存在未保存的策略修改（页面暂存的内容需点击保存后才在订阅中生效）”，并提供“放弃修改”（还原为 `originalSnapshot`）和“立即保存”按钮，彻底解决草稿丢失或误以为编辑即生效的问题。
     - **KPI 指标条与工作台 Tab 导航**：
       - 修改前：导航标签在移动端容易换行折成三四行，影响纵向空间；规则统计未展示启用状态比例。
       - 修改后：指标条展示“X / Y 条规则启用”，工作台 Tab 导航栏增加 `overflow-x-auto no-scrollbar scroll-smooth flex-nowrap`，移动端可左右平滑滑动不换行。
     - **策略组定义 (Proxy Groups)**：
       - 修改前：策略组名称无空值或重名校验提示；点击垃圾桶图标会直接执行删除，易发生误删；输入框在窄屏下容易挤压换行。
       - 修改后：
         - 增加重名实时检测函数 `isGroupNameDuplicate` 与空名称提示；
         - 增加 `ConfirmDialog` 二次确认弹窗（“删除策略组”，告知关联规则影响），杜绝误点删除；
         - 优化卡片响应式排版（`flex-col lg:flex-row`），包含节点输入框增加全宽自适应与标签去重提示；
         - 新增策略组名称自增防冲突生成算法（`GROUP_N` 自动跳过已有名称）。
     - **分流规则列表 (Rules List)**：
       - 修改前：表格宽度在窄屏下挤压操作按钮，搜索与筛选工具栏在窄屏下容易溢出。
       - 修改后：搜索工具栏采用 `flex-col sm:flex-row` 响应式流式排列，DataTable 指定 `min-w-[880px]` 保持右侧操作列吸附不遮挡；规整编号功能确保 `MATCH` 规则永远保留在末尾兜底。
     - **规则新建/编辑抽屉 (FormDrawer)**：
       - 修改前：当类型为 `RULE-SET` 且未选择规则集时直接保存可能导致空 payload；MATCH 类型缺乏直观兜底说明；各类规则占位符说明不够详细。
       - 修改后：
         - `RULE-SET` 增加非空校验提示，并明确告知将以 rule-providers 形式同步；
         - `MATCH` 类型展示专属兜底规则说明框；
         - `DOMAIN`、`DOMAIN-SUFFIX`、`IP-CIDR`、`DST-PORT`、`GEOIP` 占位符详细说明各匹配机制；
         - 优先级输入框提示步长推荐与匹配规则（越小越优先）。
     - **YAML 实时预览 (YAML Preview)**：
       - 修改前：在有未保存修改的情况下查看 YAML 预览，用户可能误以为预览展现的是未保存的草稿。
       - 修改后：当 `isDirty` 为真时，在代码块上方显示明确黄色提示条，指出“此预览展示的是服务器当前已保存的生效版本；保存配置后将自动同步”，消除认知混淆。
     - **微调与撤销记录**：
       - 在初次调整 Header 操作区顺序后，运行 `ui-regression-selftest.mjs` 发现测试对 `<template #actions>` 内元素排列及 `gap-2.5` 有严格正则表达式比对，随即撤销了 actions 内的顺序与类名改动，完全保持与既有测试兼容，同时将未保存状态强化迁移至 Header badge 与底部浮动条中，达成体验提升与测试兼容的最佳平衡。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  1. **表单**：
     - 新增混合端口范围校验（1024 ~ 65535）与即时错误文案呈现；
     - 新增策略组名称非空与唯一性校验，重名显示红字提示；
     - 新增规则抽屉内 `RULE-SET` 未选择时拦截与提示，防止提交无效规则；
     - 新增策略组成员快捷标签加入时去重判断，已存在时 toast 提示。
  2. **文案**：
     - 执行动作标签文案优化，统一为产品语义：“用户直连 (DIRECT)”、“客户端阻断 (REJECT)”、“策略组 · XXX”；
     - 说明性文字强化“客户端本地直连/阻断”与“服务器端直连/阻断”的区别；
     - 增加机制说明折叠/展开开关（默认折叠节省屏幕空间，按需展开）。
  3. **样式与布局**：
     - Tab 导航栏支持窄屏横向平滑滚动；
     - 策略组卡片小屏采用纵向流式排版，避免输入框文字被截断；
     - 增加底部未保存浮动条动画与脉冲点样式。
  4. **交互与前端数据处理**：
     - 新增“放弃修改”操作及其对应 `ConfirmDialog`，一键回滚未保存编辑；
     - 新增策略组删除 `ConfirmDialog`；
     - 新增 `persist` 保存前前端数据合规性校验（端口范围、重名策略组、空组名），提前拦截无效提交；
     - 套用预设确认弹窗增加覆盖已保存草稿的明确提示。
- **实际执行的验证及结果**：
  1. `npm.cmd run typecheck`（Vue 3 + TypeScript 静态类型检查）：
     - 执行命令：`npm.cmd run typecheck`
     - 验证结果：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/client-policy-selftest.mjs`（客户端策略运算与模拟自测）：
     - 执行命令：`node web/scripts/client-policy-selftest.mjs`
     - 验证结果：通过（exit code 0，20 项测试全数 PASS）。
  3. `node web/scripts/ui-regression-selftest.mjs`（前端界面与交互回归测试）：
     - 执行命令：`node web/scripts/ui-regression-selftest.mjs`
     - 验证结果：通过（exit code 0，涵盖 review 4 clientActionsReview、F2、U1b、F3 等全部用例 PASS）。
  4. `npm.cmd run build`（Vite 生产构建编译打包）：
     - 执行命令：`npm.cmd run build`
     - 验证结果：通过（exit code 0，279 modules transformed，dist 完整生成，耗时 4.66s）。
  5. 检查工作区 Git 状态：
     - 执行命令：`git status --porcelain`
     - 验证结果：确认本次修改仅限于 `web/src/modules/routing/pages/ClientPolicyPage.vue` 与新增记录文件 `web/docs/client-policy-change-log.md`，未修改任何 `web/` 外的文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：工作区中保留了其他任务正在进行的未提交修改（如 Agent gost 模块、tunnels 模块及 contracts 变更），本次未触碰并完整保留。
  - 限制：客户端策略配置生效依然严格依赖客户端执行“刷新订阅”操作，且仅 Owner 角色具备保存和查看用户订阅 YAML 预览的权限（受后端权限与 Token 保护约束）。
- **是否涉及接口契约变化**：
  - 无（接口 URL、HTTP 请求方法、Payload 结构、字段名及类型完全保持一致）。

---

## 轮次 2：精简页面顶部操作栏（移除顶部“恢复默认模板”、“添加规则”、“策略模拟”按钮）

- **日期和时间**：2026-11-04T04:56:14-08:00（UTC-08:00）
- **本轮原始需求**：
  用户提供页面截图并使用红框圈出顶部操作栏中的 3 个按钮：“恢复默认模板”、“+ 添加规则”、“策略模拟”，明确要求“这些去掉”。
  从页面顶部操作栏中移除这 3 个冗余/危险按钮，仅保留核心全局操作“保存策略配置”；同时将“添加规则”能力合理下沉至“分流规则列表”专属工作台板块头部，保持规则增添能力不丢失，信息层级更专注清晰。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/scripts/ui-regression-selftest.mjs`](file:///C:/Users/luanf/qui/web/scripts/ui-regression-selftest.mjs)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **页面顶部操作栏精简化**：
       - 修改前：`<template #actions>` 中排列了“恢复默认模板”、“+ 添加规则”、“保存策略配置”、“策略模拟” 4 个按钮，视觉拥挤且破坏性重置按钮与保存按钮混杂。
       - 修改后：从 `<template #actions>` 彻底移除“恢复默认模板”、“+ 添加规则”、“策略模拟” 3 个按钮，仅保留唯一的全局核心操作“保存策略配置”（以及未保存修改时的强化状态与加载指示）。
     - **分流规则列表操作入口下沉**：
       - 修改前：分流规则板块工具栏仅有“规整编号”按钮，缺少就地添加规则的入口。
       - 修改后：在“分流规则列表 (Rules)”标题栏右侧工具区就地增加“+ 添加规则”按钮（调用 `openCreateRuleDrawer`），遵循“在哪里管理、在哪里创建”的局部聚合原则，保持分流规则的新增能力完整可用。
  2. [`web/scripts/ui-regression-selftest.mjs`](file:///C:/Users/luanf/qui/web/scripts/ui-regression-selftest.mjs)：
     - 修改前：review 4 测试用例强制断言 `<template #actions>` 必须包含 `confirmResetToDefault`、`openCreateRuleDrawer` 与 `router-link`。
     - 修改后：同步更新测试断言，验证 Header `#actions` 仅保留 `saveAll` 保存操作，同时验证页面完整源码中依然保留规则创建入口 `openCreateRuleDrawer`，测试语义升级为“头部操作精简，保存留在头部，规则创建下沉至规则列表”。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **样式与布局**：页面顶部 Header 彻底清爽，只展示标题、已同步/未保存状态徽标以及一个高辨识度的“保存策略配置”按钮；操作区在窄屏/移动端不再因按钮过多而产生多行折行。
  - **交互体验**：消除了顶部重置按钮带来的误触风险；“添加规则”直接位于规则列表表格上方，操作距离更短，与用户浏览规则后的下一步操作更加贴合。
  - **数据处理**：无变化，依然严格维护 `clientConfig` 响应式模型与 `isDirty` 比对。
- **实际执行的验证及结果**：
  1. `npm.cmd run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，所有 regression 测试通过，包含更新后的 review 4）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20 项客户端策略核心测试全数通过）。
  4. `npm.cmd run build`：通过（exit code 0，`vite build` 顺利编译 279 个模块生成 dist，耗时 4.53s）。
  5. `git status --porcelain`：确认未修改 `web/` 以外的任何文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：工作区中保留了其他模块已有的未提交修改，本次未受影响。
  - 限制：无新增限制。
- **是否涉及接口契约变化**：
  - 无（接口保持“无”变化）。

---

## 轮次 3：卡片化布局重构与弹窗化配置工作台（“新建配置”与弹窗承载详细配置）

- **日期和时间**：2026-11-04T05:07:33-08:00（UTC-08:00）
- **本轮原始需求**：
  根据用户反馈截图与指令：“右上角的保存策略配置改为新建配置，图中的这些内容全部换成在这个页面里去掉换成一个个配置的卡片，点击右上角的新建配置就出来一个弹窗页面，显示这些内容”。
  重构客户端策略页面结构：
  1. 右上角按钮由“保存策略配置”改为“新建配置”；
  2. 主页面内容区域移除原本直接平铺展示的庞大编辑区域（指标条、Tab 导航栏、参数表单、策略组列表、分流规则表格和 YAML 预览），改为以整洁的“配置卡片（当前生效策略卡片 + 预设策略模板卡片库）”形式呈现；
  3. 点击右上角“新建配置”或点击任一卡片上的“编辑/查看”时，弹出大屏模态弹窗页面（[`AppDialog`](file:///C:/Users/luanf/qui/web/src/components/AppDialog.vue)），在弹窗内完整展示原有的配置工作台内容（包含预设、运行基础参数、策略组、分流规则和实时 YAML 预览），并在弹窗底部提供保存与关闭操作。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/scripts/ui-regression-selftest.mjs`](file:///C:/Users/luanf/qui/web/scripts/ui-regression-selftest.mjs)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **主页面头部操作栏**：
       - 修改前：头部操作栏为单项“保存策略配置”按钮。
       - 修改后：将按钮改为“新建配置”（附带 `+` 图标），点击调用 `openNewConfigModal()`，唤出配置编辑弹窗。
     - **主页面内容区卡片化改造**：
       - 修改前：主页面直接渲染长篇幅的指标条、Tab 栏、预设卡片、参数表单、策略组输入框、规则 DataTable 及 YAML 代码块。
       - 修改后：将原有长页面整体收拢进大屏模态弹窗 [`AppDialog`](file:///C:/Users/luanf/qui/web/src/components/AppDialog.vue) 中；主页面改为呈现现代化的配置卡片体系：
         - **「当前生效客户端策略」主卡片**：突出呈现生效中状态徽标、当前预设、运行模式、混合端口、代理组数量、启用规则比例、已包含策略组标签集、版本号及最后更新时间；提供【编辑配置】与【YAML 预览】两大核心快捷入口。
         - **「分流策略预设模板库」卡片网格**：将 4 大经典预设（经典分流、基础代理、分流+去广告、白名单模式）独立为卡片网格，展示预设定位、描述、推荐标记；提供【基于此新建】（载入预设草稿并打开弹窗）与【直接套用】（二次确认后下发生效）操作。
     - **配置弹窗页面（AppDialog）**：
       - 采用 `maxWidthClass="max-w-6xl w-full"` 的大屏 iOS 风格模态弹窗；
       - 弹窗内完整承载原工作台的全部能力：运行参数配置、策略组增删改移与测速设置、分流规则搜索筛选与抽屉编辑、YAML 实时渲染预览与复制；
       - 弹窗底部提供保存状态提示（“存在未保存的修改” / “已与服务器配置同步”）以及【关闭】与【保存策略配置】操作按钮，保存成功后关闭弹窗并刷新主页面卡片数据。
  2. [`web/scripts/ui-regression-selftest.mjs`](file:///C:/Users/luanf/qui/web/scripts/ui-regression-selftest.mjs)：
     - 修改前：review 4 断言头部操作必须是保存按钮。
     - 修改后：更新 review 4 测试，断言头部操作包含“新建配置”入口，验证页面源码完整包含弹窗保存操作 `saveAll` 以及规则创建抽屉 `openCreateRuleDrawer`。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **文案与交互**：
    - 主页面更加清晰、聚焦，用户一目了然看清当前生效的配置指标与预设库；
    - 点击“新建配置”直观呼出弹窗工作台；点击卡片“编辑配置”直接针对当前生效策略进行微调；点击“基于此新建”一键载入对应模板草稿并弹出弹窗。
  - **视觉体验**：
    - 当前生效策略卡片采用高亮边框与渐变背景底色（`border-ios-blue/20 bg-gradient-to-br from-blue-50/20 via-white to-white`），在视觉层级上具有最高权重；
    - 弹窗采用居中大屏视口，支持弹窗内平滑滚动与分页。
- **实际执行的验证及结果**：
  1. `npm.cmd run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全部 PASS，包含更新后的 review 4）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，全部 20 项 PASS）。
  4. `npm.cmd run build`：通过（exit code 0，Vite 4.33s 编译通过，生产 dist 资源正常生成）。
  5. `git status --porcelain`：确认未触碰任何 `web/` 以外的文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：工作区中保留了其他模块正在进行的未提交修改，本次未受影响。
  - 限制：目前后端 API 仅持久化存储一份全局生效的订阅规则模板（`/api/v1/subscription-rules`），因此弹窗内保存时均作为当前全局生效策略下发；多份独立命名策略共存需依赖未来后端配置库多租户模型扩展。
- **是否涉及接口契约变化**：
  - 无（接口保持“无”变化）。


---

## 轮次 4：移除页面顶部“当前生效客户端策略”卡片与预设库标题栏，精简卡片流
- **日期和时间**：2026-11-04T05:28:30-08:00（UTC-08:00）
- **本轮原始需求**：
  根据用户反馈截图（红框框选区域：顶部“当前生效客户端策略”大卡片与下方“分流策略预设模板库 常用分流拓扑模板，支持一键直接套用或基于此模板新建自定义配置 共 4 个官方预设”标题栏）及指令：“去掉”。
  在保留头部导航栏“+ 新建配置”按钮和下方分流策略预设配置卡片网格（经典分流、基础代理、分流+去广告、白名单模式）的前提下，完全移除红框内的两部分内容，使主页面直接平铺展示各套配置卡片，视觉更加简洁紧凑。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **主页面内容区**：
       - 修改前：主页面卡片区域包含顶部高亮的“当前生效客户端策略”卡片（含模式、端口、策略组数、生效规则数等指标概览以及“YAML 预览”、“编辑配置”按钮），紧接着是“分流策略预设模板库”的标题栏（含标题、说明文案及“共 4 个官方预设”数量标签），下方才是 4 个配置预设卡片。
       - 修改后：彻底移除“当前生效客户端策略”大卡片以及“分流策略预设模板库”标题栏，主页面直接呈现 4 个分流策略配置卡片组成的响应式网格（`grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4`）。
       - 卡片细节增强：在各配置卡片标题旁，如果为当前生效预设，清晰展示“当前在用”蓝色状态标签（附微脉冲圆点），若为推荐预设则展示“推荐”绿色标签，使得主页面在移除顶部大卡片后依然直观呈现当前生效的策略归属。
       - 配置详细工作台保持在右上角“新建配置”与各卡片“基于此新建”弹出的模态弹窗中，包含完整参数、策略组编排、规则列表与实时 YAML 预览。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **文案与交互**：
    - 移除了冗余的顶部配置摘要卡片与分组标题，用户进入页面即可直接面对清晰的配置卡片库；
    - 点击右上角“+ 新建配置”即可随时呼出全局配置工作台弹窗；
    - 点击任意卡片上的“基于此新建”将该模板参数快速载入弹窗工作台；点击“直接套用”保留二次确认后直接持久化。
  - **样式与布局**：
    - 主页面移除了外层 `space-y-6` 嵌套，直接以 `grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4` 排布配置卡片；
    - 消除因顶部大卡片带来的屏幕纵向占用，让整个卡片布局在首屏更为紧凑聚焦。
- **实际执行的验证及结果**：
  1. `npm.cmd run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全部 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，全部 20 项 PASS）。
  4. `npm.cmd run build`：通过（exit code 0，Vite 4.47s 编译通过，生产 dist 构建正常）。
  5. `git status --porcelain`：确认仅修改了 `web/` 目录中与本任务直接相关的文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：工作区中其他目录的历史未提交改动继续保留，不受影响。
  - 限制：目前后端 API 仅持久化存储一份全局生效的订阅规则模板（`/api/v1/subscription-rules`），因此弹窗内保存时均作为当前全局生效策略下发；多份独立命名策略共存需依赖未来后端配置库多租户模型扩展。
- **是否涉及接口契约变化**：
  - 无（接口保持“无”变化）。


---

## 轮次 5：主页面改造为展示已建好的策略配置，每张卡片提供【编辑】【启用】【删除】三操作
- **日期和时间**：2026-11-04T05:35:00-08:00（UTC-08:00）
- **本轮原始需求**：
  根据用户反馈截图（红框框住 4 个预设模板卡片）及明确指令：“这里不是展示这中内容，而是展示我已经新建设置好的内容，下面有3个按钮编辑，启用，删除”。
  1. 主页面不再直接平铺展示官方预设模板库；
  2. 主页面转为展示用户已经新建和保存好的客户端策略配置卡片集合；
  3. 每个策略卡片下方明确提供 3 个操作按钮：【编辑】、【启用】、【删除】；
  4. 支持点击右上角“+ 新建配置”创建新策略，在弹窗中配置名称、说明、运行参数、策略组与分流规则，保存后加入卡片列表；
  5. 初始状态自动加载服务端当前已生效的真实客户端策略作为默认配置卡片，确保用户立即看到真实设置。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **主页面卡片展示体系重构**：
       - 移除：静态预设模板库（`clientPresets`）卡片直出。
       - 新增：已建好的客户端策略配置数据模型 `SavedClientPolicy` 及本地持久化管理（`savedPolicies`）。
       - 页面卡片直观呈现：
         - 策略名称与适用说明；
         - 启用状态徽标（当前在服务端生效的配置高亮展示【已启用】+ 呼吸微绿标，其余显示【未启用】）；
         - 核心指标网格：运行模式（RULE/GLOBAL/DIRECT）、策略组数量、生效规则条数；
         - 底部辅助信息：混合端口与最后更新时间戳。
       - 卡片操作栏：标准配置 3 个按钮：
         - **【编辑】**：加载对应策略数据，打开全屏配置工作台弹窗进行详细修改；
         - **【启用】**：调用后端 `PUT /api/v1/subscription-rules` 真实持久化下发此策略，将当前策略设为服务端生效版本，卡片状态同步切换为【已启用】；
         - **【删除】**：弹出带二次安全确认的 `ConfirmDialog` 弹窗；提供安全防护（禁止删除唯一配置、提示正在生效配置先切换再删除）。
     - **配置弹窗支持策略元数据配置**：
       - 弹窗顶部增加“策略配置名称”（必填）与“策略配置说明”（选填）输入框；
       - 新建模式下提供“保存后立即设为当前生效策略并下发生效”快捷勾选；
       - 弹窗标题动态响应新建或当前正在编辑的策略名称。
     - **初始化与数据一致性处理**：
       - `load()` 请求真实接口 `/api/v1/subscription-rules`，若本地暂无策略集合，自动以服务端当前正在运行的完整配置（端口、策略组、规则集）生成首个“标准分流策略”配置卡片，状态直接标记为【已启用】；
       - 若本地已有保存列表，将当前服务端的真实版本与状态与列表中的生效项实时同步，保持多配置切换与单版本后端的契约完全一致。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **文案与交互**：
    - 主页面中心位置彻底从“预设模板展示”转向“我的自定义策略库”；
    - 卡片底部按钮严格按照用户指定包含【编辑】、【启用】、【删除】；
    - 删除保护机制友好提示“至少需要保留一个策略配置，无法删除全部配置”及“该配置当前正在生效中，请先启用其他配置后再删除”。
  - **样式与布局**：
    - 卡片采用 `grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4` 响应式网格布局；
    - 增加策略为空时的引导空状态（EmptyState）；
    - 3 个按钮排版紧凑：编辑与启用均分宽度，删除按钮使用危险色强调 hover。
- **实际执行的验证及结果**：
  1. `npm.cmd run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全部 PASS，包含 review 4 校验）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，全部 20 项 PASS）。
  4. `npm.cmd run build`：通过（exit code 0，Vite 4.34s 生产构建成功）。
  5. `git status --porcelain`：确认仅修改了 `web/` 目录下与本任务直接相关的前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：工作区中保留其他模块历史未提交文件，本次未改动。
  - 限制：后端 API 仅支持保存当前一份全局生效的订阅规则（`/api/v1/subscription-rules`）；多策略集合采用前端安全持久化存储，点击【启用】即刻调用后端接口实现当前策略的热切换与下发。
- **是否涉及接口契约变化**：
  - 无（接口保持“无”变化，完全兼容现有 GET/PUT `/subscription-rules`）。


---

## 轮次 6：策略编辑弹窗按手绘交互重构为左右分栏布局与右上角三操作（保存、关闭、版本）

- **日期和时间**：2026-11-04T05:58:00-08:00（UTC-08:00）
- **本轮原始需求**：
  根据用户手绘线框图与详细文字指令：
  “点击编辑之后的弹窗内容展示如图，左边是选择模块，右边是设置对应模块的内容，右上角3个按钮保存，关闭，版本
  左边模块按钮按照顺序如下：
  1. 基础设置：Clash 如何运行 比如监听端口、运行模式、是否允许局域网设备连接等。
  2. 代理组：proxy-groups 把节点组织起来，方便手动选择、自动测速或故障切换。组里也可以包含其他代理组。
  3. 分流规则：rules 决定一个请求应该交给哪个节点或代理组，也可以指定直连 DIRECT 或拦截 REJECT。
  4. DNS 设置：dns 控制域名如何解析，例如使用哪些 DNS 服务器、是否启用 Fake-IP，以及是否为不同域名指定不同解析方式。
  5. 规则集：引用规则集里面的内容决定那些域名ip直连，那些走代理等等。
  （手绘图中第 6 个模块为 YAML 预览：用于最终订阅 YAML 文本预览与一键复制）”
- **涉及的全部文件路径**：
  - 修改：[`web/src/components/AppDialog.vue`](file:///C:/Users/luanf/qui/web/src/components/AppDialog.vue)
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/components/AppDialog.vue`](file:///C:/Users/luanf/qui/web/src/components/AppDialog.vue)：
     - **头部操作栏插槽支持（`#header-actions`）**：
       - 修改前：弹窗头部仅支持默认标题、子标题以及右侧固定的单个关闭“X”图标按钮，无法在头部定制多操作按钮。
       - 修改后：增加 `<slot name="header-actions"></slot>` 插槽；当父组件提供自定义头部操作时展示插槽内容，并隐式隐藏默认的单个关闭图标，使得弹窗右上角能够按需展示组合按钮组（如本轮要求的【保存】、【关闭】、【版本】）。
  2. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **弹窗右上角三操作按钮（保存、关闭、版本）**：
       - 修改前：弹窗为常规顶部标题 + 底部操作栏（取消与保存），右侧仅有关闭“X”图标。
       - 修改后：利用 `AppDialog` 的 `#header-actions` 插槽，将顶部右侧严格定制为 3 个功能按钮：
         - **【版本】**：点击弹出版本详情对话框，展示当前策略配置修订版本号（Revision #）、最后更新时间、操作者及配置同步状态说明；
         - **【关闭】**：关闭弹窗。当用户修改过配置尚未保存时（`isDirty` 为 true），自动弹出防误触二次确认对话框询问是否放弃修改；未修改时直接平滑关闭；
         - **【保存】**：执行数据校验与持久化。在当前生效策略模式下，实时下发调用后端 `PUT /api/v1/subscription-rules` 并更新策略列表；在草稿模式下保存至本地策略卡片列表，附带保存中 loading 状态与未保存脏状态红点提示。
     - **弹窗主体重构为左右 Master-Detail 分栏结构**：
       - 修改前：弹窗内采用横向顶部 Tab 导航，内容上下堆叠滚动。
       - 修改后：遵循手绘图交互，左侧设计为垂直模块导航选择栏，右侧为当前选中模块的独立设置工作区：
         - **左侧导航栏（严格按照用户指定顺序）**：
           1. **基础设置**：Clash 如何运行 比如监听端口、运行模式、是否允许局域网设备连接等。
           2. **代理组**：proxy-groups 把节点组织起来，方便手动选择、自动测速或故障切换。组里也可以包含其他代理组。
           3. **分流规则**：rules 决定一个请求应该交给哪个节点或代理组，也可以指定直连 DIRECT 或拦截 REJECT。
           4. **DNS 设置**：dns 控制域名如何解析，例如使用哪些 DNS 服务器、是否启用 Fake-IP，以及是否为不同域名指定不同解析方式。
           5. **规则集**：引用规则集里面的内容决定那些域名ip直连，那些走代理等等。
           6. **YAML 预览**：匹配手绘图第 6 个模块，提供完整订阅配置的实时 YAML 语法高亮预览与一键复制功能。
           - 每个导航按钮不仅展示模块主标题，还附带用户指定的使用说明辅文，并展示各模块的实时状态徽标（如规则条数、代理组数、DNS 解析模式等）。
         - **右侧各模块独立配置区域**：
           1. **基础设置**：配置策略名称与描述、Clash 运行模式（RULE/GLOBAL/DIRECT）、混合代理端口（mixed-port，带 1024~65535 校验）、局域网连接开关（allow-lan）、IPv6 选项、日志等级（info/warning/error/debug/silent）及外部控制参数。
           2. **代理组 (proxy-groups)**：包含策略组添加、重命名、类型切换（select / url-test / fallback / load-balance）、测速 URL 与间隔设置、代理节点标签输入与快捷添加、拖拽排序与重名校验。
           3. **分流规则 (rules)**：包含规则搜索筛选、规整编号、添加规则抽屉、规则类型精准匹配（DOMAIN、DOMAIN-SUFFIX、IP-CIDR、DST-PORT、RULE-SET、MATCH）、指定目标节点/策略组及行内删除。
           4. **DNS 设置 (dns)**：配置是否启用内置 DNS（enable）、域名解析模式（fake-ip / redir-host）、Fake-IP 过滤名单、上游 DNS 服务器列表（nameserver / fallback / default-nameserver），以及针对特定域名的解析指定；完整映射至 `extra.dns` 结构。
           5. **规则集 (rule-providers)**：配置外部与服务规则集，设置规则集类型（http / file）、格式（yaml / text）、下载行为（behavior: domain / ipcidr / classical）、更新周期（interval: 秒）及远端 URL 路径；完整映射至 `extra.providers` 结构。
           6. **YAML 预览**：实时生成并高亮展示最终生成的 Clash / Mihomo 完整订阅 YAML 文本，支持一键复制到剪贴板。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **文案**：弹窗左侧导航严格使用用户指定的名称与说明文案；
  - **样式与布局**：弹窗内部使用全宽左右分栏（`flex-col md:flex-row min-h-[580px]`），左侧宽度固定（`w-full md:w-80`），右侧自适应撑满（`flex-1`），右侧内容区带独立滚动条；
  - **交互体验**：右上角三大操作按钮使得操作路径极其自然聚焦；切换左侧模块时右侧即时响应，各模块数据在前端保持单一数据源双向同步。
- **实际执行的验证及结果**：
  1. `npm.cmd run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全部回归测试用例 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，全部 20 项 PASS）。
  4. `npm.cmd run build`：通过（exit code 0，Vite 4.45s 生产编译通过，生成完整生产静态资源）。
  5. `git status --porcelain`：确认仅修改 `web/` 内文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：工作区中保留其他模块历史未提交文件，本次未受影响。
  - 限制：`extra.dns` 与 `extra.providers` 采用现有的订阅规则模型持久化，在客户端通过订阅拉取时自动渲染到最终 YAML 配置中。
- **是否涉及接口契约变化**：
  - 无（严格保持现有接口契约不变）。


---

## 轮次 7：弹窗宽度水平扩增与左右分栏响应式伸展优化

- **日期和时间**：2026-11-04T06:17:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户根据弹窗实际呈现截图反馈页面过窄、左右两栏挤压拥挤，明确提出指令：“左右扩大一点”。
- **涉及的全部文件路径**：
  - 修改：[`web/src/components/AppDialog.vue`](file:///C:/Users/luanf/qui/web/src/components/AppDialog.vue)
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/components/AppDialog.vue`](file:///C:/Users/luanf/qui/web/src/components/AppDialog.vue)：
     - **支持更宽的 `maxWidth` 与自定义 `maxWidthClass` Prop**：
       - 修改前：`AppDialog` 仅声明了 `maxWidth?: 'sm' | 'md' | 'lg' | 'xl' | '2xl' | '3xl'`，未声明 `maxWidthClass` prop，导致父组件传入自定义宽度样式类时被作为未捕获属性丢弃，内部 computed 逻辑始终回退默认值 `max-w-md`（448px），致使弹窗在宽屏下呈现为窄条。
       - 修改后：将 `maxWidth` 类型扩展支持至 `'4xl' | '5xl' | '6xl' | '7xl' | 'full'`，并在 `defineProps` 中正式支持 `maxWidthClass?: string`。计算属性优先使用传入的 `maxWidthClass`，确保任意宽度的弹窗均能正确生效。外层遮罩内边距优化为 `p-3 sm:p-5 lg:p-6`，最大高度微调为 `max-h-[92vh]`。
  2. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **策略编辑弹窗水平宽度扩充**：
       - 修改前：弹窗受限默认 448px 宽度，左右两栏内容被极度压缩（左侧按钮文字折行，右侧预设卡片挤成极窄条）。
       - 修改后：配置弹窗指定 `maxWidth="full"` 与 `maxWidthClass="max-w-[1400px] xl:max-w-[1520px] 2xl:max-w-[1640px] w-full"`，桌面端宽度扩充至 1400px~1640px，占据大屏宽度的 75%~85%，视觉极度舒展。
     - **弹窗内部左右分栏比例优化**：
       - 修改前：采用 `grid-cols-1 lg:grid-cols-12`（左 3 列右 9 列），但在窄宽基准下左侧仅有 100 余像素。
       - 修改后：采用 Flex 左右弹性分栏（`flex flex-col md:flex-row gap-6 min-h-[620px]`），左侧模块选择列固定为 `w-full md:w-80 lg:w-[350px] shrink-0`，5 大模块的标题、英文代号、状态计数徽标与完整说明文案不再截断；右侧设置区域使用 `flex-1 min-w-0 overflow-y-auto px-1 space-y-6` 占满全部剩余宽度，表单和 4 个预设卡片平铺舒展。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **样式与布局**：弹窗水平宽度由原先固定小屏宽度扩增至自适应宽屏（1400px~1640px）；左右两栏拥有充足视口空间，彻底消除了文字折行和卡片纵向挤压。
  - **文案与交互**：保持既有逻辑完全一致，未改动任何业务逻辑或表单字段。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全部回归用例 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，全部 20 项 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.35s 编译完成，生成完整静态资源）。
  5. `git status --porcelain`：确认仅修改了 `web/` 目录下文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：工作区中保留其他目录的历史未提交改动，本次未受影响。
  - 限制：保持既有限制，无新增限制。
- **是否涉及接口契约变化**：
  - 无（严格保持接口契约不变）。


---

## 轮次 8：移除基础设置模块底部的预设模板拓扑卡片区

- **日期和时间**：2026-11-04T06:38:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户根据策略编辑弹窗的实际呈现截图，用红框框选基础设置底部的“快速填入预设模板拓扑”区域（包含“经典分流”、“基础代理”、“分流+去广告”、“白名单模式” 4 个预设模板卡片），提出明确指令：“去掉”。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **基础设置（Settings）模块去除非必要预设卡片**：
       - 修改前：在“基础设置”模块底部展示了“快速填入预设模板拓扑”标题与 4 个预设卡片网格，占用了右侧区域的纵向可视空间。
       - 修改后：彻底移除该预设卡片网格与标题说明，基础设置模块仅保留策略配置名称与说明、Clash 运行模式、混合端口、允许局域网连接、日志级别以及 IPv6 开关等关键参数表单，界面更加聚焦干净。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **文案与样式**：去除了预设模板标题、说明文字与 4 张模板卡片；基础设置右侧工作区在首屏即可完整呈现全部运行参数，无需不必要的纵向滚动。
  - **交互与数据**：原有基础运行参数的双向绑定、未保存脏状态（dirty）检测及保存逻辑完全保持不变。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全量测试 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，全部 20 项 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.34s 顺利完成生产构建打包）。
  5. `git status --porcelain`：确认仅修改了 `web/` 内的前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：工作区中保留其他目录的历史未提交改动，本次未受影响。
  - 限制：保持既有限制，无新增限制。
- **是否涉及接口契约变化**：
  - 无（严格保持接口契约不变）。


---

## 轮次 9：策略编辑弹窗固定视口高度锁定，消除切换模块时的上下高度跳动

- **日期和时间**：2026-11-04T06:42:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户在点击左侧不同配置模块（如基础设置、代理组、分流规则等）时，发现弹窗高度随右侧模块内容的多寡忽高忽低、上下变动，明确提出指令：“弹窗固定高度，不要点击到不同页面的时候高度变来变去”。
- **涉及的全部文件路径**：
  - 修改：[`web/src/components/AppDialog.vue`](file:///C:/Users/luanf/qui/web/src/components/AppDialog.vue)
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/components/AppDialog.vue`](file:///C:/Users/luanf/qui/web/src/components/AppDialog.vue)：
     - **支持自定义高度类（`heightClass`）与 Body 弹性布局控制（`bodyClass`）**：
       - 修改前：弹窗外框面板仅有自适应流式高度类 `max-h-[92vh]`，当内部内容较少时高度收缩、内容较多时长高；且 Body 区域未设置 `flex-1 min-h-0`。
       - 修改后：在 `defineProps` 中增加 `heightClass?: string`、`bodyClass?: string`、`panelClass?: string`；外框高度使用 `heightClass ? heightClass : 'max-h-[92vh]'`；Header 与 Footer 增加 `shrink-0` 保持尺寸固定，Body 容器增加 `flex-1 min-h-0` 并应用 `bodyClass`，具备完全撑满固定外框的弹性支持。
  2. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **配置编辑弹窗锁定固定高度与内外滚动解耦**：
       - 修改前：弹窗高度由各个模块内容撑起，基础设置无复杂列表时弹窗高度较矮，切换到包含多个代理组的“代理组”或表格的“分流规则”时弹窗突然向下大幅伸长，产生明显的晃动跳跃感。
       - 修改后：
         - 弹窗通过 `heightClass="h-[88vh] max-h-[900px] min-h-[640px]"` 显式锁定高度视口，并通过 `bodyClass="flex flex-col flex-1 min-h-0 p-6 overflow-hidden"` 将滚动条限制在内部工作区；
         - 弹窗内部布局采用 `flex flex-col md:flex-row gap-6 flex-1 min-h-0 h-full`；
         - 左侧模块选择栏设置为 `overflow-y-auto h-full pr-2`，独立自洽；
         - 右侧各模块工作区统一设置为 `flex-1 min-w-0 overflow-y-auto px-1 space-y-6 h-full pr-2`，长内容模块在右侧区域独立平滑滚动。
       - 切换“基础设置”、“代理组”、“分流规则”、“DNS 设置”、“规则集”、“YAML 预览”任意模块时，弹窗外部轮廓与顶部/底部位置完全静止纹丝不动。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **样式与布局**：弹窗高度锁定为 `h-[88vh] max-h-[900px] min-h-[640px]`；左右两侧各自拥有平滑的独立内滚动（`h-full overflow-y-auto`），消除弹窗外框跳跃；
  - **交互体验**：模块切换过程视觉稳定，鼠标操作聚焦不移位。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全量测试 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，全部 20 项 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.18s 顺利完成生产构建打包）。
  5. `git status --porcelain`：确认仅修改了 `web/` 内的前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：工作区中保留其他目录的历史未提交改动，本次未受影响。
  - 限制：保持既有限制，无新增限制。
- **是否涉及接口契约变化**：
  - 无（严格保持接口契约不变）。


---

## 轮次 10：代理组编排界面全面重构与地区组一键生成（自选、自动测速、地区组、直连/拦截可视化）

- **日期和时间**：2026-11-04T06:52:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户针对代理组编排界面提出优化反馈：“这里展示设置代理组的内容，比如自动测速，直连，REJECT，自选节点，我想可以设置一些地区组的代理组的内容这种，你帮我优化一下，我感觉现在的有点看不懂”。
  优化代理组管理界面，消除晦涩的逗号纯文本语法门槛，使“自选节点”、“自动测速”、“故障转移”、“地区分组（港/日/美/新/台/韩）”、“DIRECT 直连”、“REJECT 拦截”等核心概念变得通俗易懂、一键配置，并提供完整的可视化成员胶囊标签与智能插入器。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **新增代理组概念设计指引（指南卡片）**：
       - 在代理组编排顶部增加图文清晰的设计指引卡片，用普通用户易懂的语言解释 🎯 自选节点 (select)、⚡ 自动测速 (url-test)、🌏 地区分组 (Region) 与 🚀 直连 / 🛑 拦截的核心作用与典型用法，消除用户对底层逻辑的理解障碍。
     - **一键快捷添加常用代理组与地区组工具栏**：
       - 顶部工具栏提供快捷模版按钮：
         - 常用核心组：🎯 自选总入口 (PROXY)、⚡ 自动优选 (AUTO)、🛡️ 故障切换 (FALLBACK)；
         - 常见地区组：🇭🇰 香港节点、🇯🇵 日本节点、🇺🇸 美国节点、🇸🇬 新加坡节点、🇹🇼 台湾节点、🇰🇷 韩国节点；
       - 智能匹配机制：创建地区组时，自动检索系统中已创建的真实节点分组（`systemNodeGroups`，通过 `groupAPI.list` 读取），若存在对应地区的节点分组自动绑定为 `@nodeGroup:<id>`，并自动将新建的地区组挂载至 `PROXY` 自选总入口组内供客户端直接点选。
     - **代理组内成员可视化标签与选择器重构（告别纯文本输入）**：
       - 修改前：每个策略组仅有一个简单的单行文本框 `group.proxies`，用户需自行手打晦涩的逗号分隔字符串（如 `AUTO, FALLBACK, @all, DIRECT`）。
       - 修改后：
         - 每个组内成员以彩色微型胶囊徽标展示（🌐 全部节点、🚀 DIRECT 直连、🛑 REJECT 拦截、📁 节点分组、🔀 其他代理组、🏷️ 自定义标签），点击即可单项快速移除；
         - 提供分类添加快捷工具栏：
           - 快捷动作：`+ 🌐 全部可用节点 (@all)`、`+ 🚀 DIRECT (直连)`、`+ 🛑 REJECT (拦截)`；
           - 动态下拉：`+ 📁 插入节点分组...`（展示真实名称如“香港专线”而非无意义的 UUID）；
           - 动态下拉：`+ 🔀 引用其他代理组...`（展示其他已定义组，避免循环引用）；
           - 手动输入：支持输入任意自定义节点名/标签按回车直接插入；
         - 保留“切换原始逗号文本编辑”高级开关，满足资深管理员复制粘贴批量配置的需求。
     - **组类型选择与健康检查直观化**：
       - 组类型下拉框添加 Emoji 标记与作用说明（select / url-test / fallback）；
       - 自动测速与故障转移扩展面板提供“Google 204”、“Cloudflare 204”一键填入，并明确提示建议测速间隔为 300 秒。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **文案与交互**：
    - 代理组类型文案清晰化，每张卡片顶部实时展示当前类型的直观解释（如“延迟最低节点自动优选”）；
    - 成员清单从抽象代码文本升级为具象可视化标签，支持即点即删、即选即加；
    - 增加地区组一键配置，让用户无缝建立按国家/区域归集的分流拓扑。
  - **样式与布局**：
    - 引入卡片化指引区与快速模板栏，布局层次分明；
    - 胶囊标签采用不同主题色区分成员类别（蓝色：全部节点，绿色：直连，红色：拦截，紫色：节点分组，琥珀色：代理组）。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全量测试 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，全部 20 项 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.22s 顺利完成生产构建打包）。
  5. `git status --porcelain`：确认仅修改了 `web/` 内的前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：工作区中保留其他目录的历史未提交改动，本次未受影响。
  - 限制：保持既有限制，无新增限制。
- **是否涉及接口契约变化**：
  - 无（严格保持接口契约不变）。


---

## 轮次 11：策略编辑弹窗左侧导航固定锁定，解除右侧滚动时的左侧联动位移

- **日期和时间**：2026-11-04T06:57:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户在策略编辑弹窗中向下滚动查看右侧多组长列表内容时，发现左侧模块选择清单（包含“基础设置”、“代理组”、“分流规则”等按钮）也被整体向上卷走滚出了屏幕视口，红框圈选左侧导航栏明确提出指令：“这里内容固定一下”。
- **涉及的全部文件路径**：
  - 修改：[`web/src/components/AppDialog.vue`](file:///C:/Users/luanf/qui/web/src/components/AppDialog.vue)
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/components/AppDialog.vue`](file:///C:/Users/luanf/qui/web/src/components/AppDialog.vue)：
     - **修复 Body 外层默认 `overflow-y-auto` 覆盖局部滚动的冲突**：
       - 修改前：`AppDialog` 的 Body 节点始终携带硬编码的 `p-6 overflow-y-auto space-y-4`，即使父组件传入 `bodyClass="overflow-hidden..."`，浏览器仍然启用了弹窗整体的纵向滚动条，致使左栏跟随右栏一起被滚出视口。
       - 修改后：将 Body 容器修改为 `:class="['flex-1 min-h-0', bodyClass ? bodyClass : 'p-6 overflow-y-auto space-y-4']"`。当自定义 `bodyClass` 声明了 `overflow-hidden` 时，弹窗外层 Body 彻底禁止滚动。
  2. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **左侧模块选择列完全固定常驻**：
       - 弹窗内部分栏根容器声明 `overflow-hidden`（`<div class="flex flex-col md:flex-row gap-6 flex-1 min-h-0 h-full overflow-hidden">`）；
       - 左侧模块导航栏（`w-full md:w-80 lg:w-[350px] shrink-0 h-full`）完全吸附并锁定在弹窗左侧，纹丝不动；
       - 右侧各个模块的工作区（`flex-1 min-w-0 h-full overflow-y-auto px-1 space-y-6 pr-2`）独立接管滚动条。
       - 无论右侧代理组列表或分流规则表格滚动到多长，左侧 6 大模块按钮始终固定可见，支持随时直接点击切换。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **样式与布局**：弹窗外层滚动被彻底禁用，左右两栏滚动完全隔离解耦，左侧导航锁定常驻。
  - **交互体验**：消除了右侧长内容把左侧导航栏顶出视口的困扰，模块切换与浏览体验极大改善。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全量测试 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，全部 20 项 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.13s 顺利完成生产构建打包）。
  5. `git status --porcelain`：确认仅修改了 `web/` 内的前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：工作区中保留其他目录的历史未提交改动，本次未受影响。
  - 限制：保持既有限制，无新增限制。
- **是否涉及接口契约变化**：
  - 无（严格保持接口契约不变）。

---

## 轮次 12：移除基础设置中的“运行模式 (Mode)”表单项并平滑自适应布局

- **日期和时间**：2026-11-04T07:25:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户在策略配置编辑弹窗的“基础设置 (Settings)”面板中，截图使用红框框选了“运行模式 (Mode)”下拉选择项（包含 Rule / Global / Direct 选项），明确提出修改指令：“去掉这个”。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **表单视图层移除运行模式选择控件**：从基础设置的表单卡片中彻底移除“运行模式 (Mode)”的 `<label>`、`<select>` 以及 `按分流规则决定走向` 说明文案。
     - **网格布局平滑自适应**：将表单区域的响应式容器从 `sm:grid-cols-2` 优化为 `grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-5`，使保留的三个核心参数“混合端口 (Mixed Port)”、“允许局域网连接 (Allow LAN)”、“日志级别 (Log Level)”在大屏与编辑弹窗中整齐平铺为一行，消除原先因缺少一项而产生的右下角空白。
     - **完全保留数据模型与接口协议**：前端状态数据模型中的 `clientConfig.settings.mode` 保持默认值为 `'rule'`，向后端提交 PUT 保存及渲染订阅配置时，契约字段完整传递，保证客户端分流策略逻辑与订阅生成 100% 兼容。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **表单精简**：去除了不必要的模式切换下拉框，避免用户产生“是否需要切换 Global/Direct”的混淆。
  - **样式与排版**：三项基础参数在弹窗右侧面板横向三列整齐对齐，下方的 IPv6 复选框整齐分隔。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全量测试 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，全部 20 项 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.36s 顺利完成生产构建打包）。
  5. `git status --porcelain`：确认仅修改了 `web/` 内的前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：工作区中保留其他目录的历史未提交改动，本次未受影响。
  - 限制：保持既有限制，无新增限制。
- **是否涉及接口契约变化**：
  - 无（严格保持接口契约不变）。

---

## 轮次 13：代理组重构为“用途分组 + 地区分组”双层拓扑架构与链路可视化设计

- **日期和时间**：2026-11-04T07:53:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户根据完整的设计规划说明，要求对策略配置中的“代理组 (Proxy Groups)”模块进行全面系统性重构与重新设计：
  1. 结构分类：代理组页面划分为两个清晰标签页「用途分组」和「地区分组」，支持全部分组查看；顶部提供搜索、新建分组和刷新延迟。
  2. 用途组设计：明确回答“这类流量使用哪个出口”，卡片分行清晰展示“当前选择出口”与“最终出口节点”；可视化展示完整链路（例如 `AI 服务 → 美国节点 → 美国02`）；提供可选出口单选与即时点击切换；展示关联分流规则与快速配置入口。
  3. 地区组设计：明确回答“这个地区有哪些节点，当前用哪一个”，展示选择方式、当前使用节点、节点健康度；支持关键词动态筛选（包含/排除关键词）及实时匹配节点预览；展示被哪些用途组引用的反查信息。
  4. 联动与异常：支持闭环循环引用（A→B→A）实时检测与告警阻止；删除被引用组时列出引用位置告警拦截；提供一键应用标准分流拓扑。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **页面架构与分类 Tab**：顶部增加「🎯 用途分组 (N)」、「🌏 地区分组 (N)」与「🔀 全部 / 其他 (N)」Tab 切换与实时数量角标，引入搜索过滤框与延迟测速刷新。
     - **用途组卡片 (Service/App Groups)**：
       - 顶部提供渐变调用链路条：`当前出口路径: 【AI 服务】 ──→ 【美国节点】 ──→ 【美国02 (150ms · 可用)】`；
       - 分行独立展示“当前首选出口”与“最终出口真实节点”；
       - 可选出口候选药丸支持一键直接点击切换当前出口（自动调整顺序并标记 dirty）；
       - 规则联动：反查显示命中当前用途组的分流规则列表，无规则时提示并提供一键跳转创建分流规则。
     - **地区组卡片 (Region Groups)**：
       - 展示自动测速 (url-test) / 故障转移 (fallback) / 手动选择 (select)；
       - 展示当前胜出节点与节点健康度规模（总数、可用、超时）；
       - 增加“节点来源与关键词筛选”面板：支持包含关键词（如 `美国, US, 🇺🇸`）和排除关键词（如 `过期, 维护`），实时计算并预览匹配到的系统节点，支持一键同步；
       - 被引用情况反查：自动列出引用该地区组的所有用途组。
     - **异常与防错**：
       - 引入深度优先搜索（DFS）检测代理组循环引用，出现环路时顶部醒目告警并阻止错误保存；
       - 删除代理组时，若被用途组或分流规则引用，弹窗详细列出引用来源提示风险；
       - 增加「⚡ 推荐标准拓扑」一键配置（AI服务->美国、YouTube->日本、Telegram->新加坡、默认代理->香港）。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **交互体验**：用户在用途组卡片上单选任一候选出口（如美国节点、日本节点），整条链路路径和最终节点实时动态切换更新，彻底告别抽象文本输入；
  - **样式规范**：严格遵循 iOS / Tailwind 柔和圆角与色彩标识体系，用途组采用深蓝色主题，地区组采用琥珀金色主题；
  - **底层完全兼容**：前端完全基于 Clash / mihomo 底层标准协议，数据通过 `fromConfig` 序列化为标准的 `groups: [{ name, type, members, testUrl, intervalSeconds }]`，后端契约 0 改动。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全量测试 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，全部 20 项 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.38s 顺利完成生产构建打包）。
  5. `git status --porcelain`：确认仅修改了 `web/` 内的前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：工作区中保留其他目录的历史未提交改动，本次未受影响。
  - 限制：保持既有限制，无新增限制。
- **是否涉及接口契约变化**：
  - 无（严格保持接口契约不变）。

---

## 轮次 14：代理组布局精简与三层视觉层级（Tier）体系重构

- **日期和时间**：2026-11-04T08:00:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户在查看代理组页面后反馈并提出指令：“优化一下这里的布局内容，注意层级”。
  原界面存在信息多重嵌套与视觉冗余：同一张卡片内重复展示了“当前出口路径栏”与两个并列的“首选出口框 + 最终落地框”，外加候选出口药丸与单独一整行的添加按钮、关联规则栏，纵向堆叠了 7 个层级的灰底矩形与边框，缺少清晰的视觉主次焦点；同时存在美国组误匹配到系统第一个非美区域节点（东京入口）的逻辑问题。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **视觉层级架构重构（严格遵循清晰三层结构 Tier 1 -> Tier 2 -> Tier 3）**：
       - **层级 1（身份与控制层 Header）**：组名改为轻量内联标题输入风格，去除粗重外框；紧凑保留分类小胶囊（🎯 用途分组 / 🌏 地区分组）、选择模式下拉菜单及右侧排序/删除工具，去除冗余描述文字，保持顶层透气清爽。
       - **层级 2（主控决策看板与候选切换器 Core Decision Board）**：
         - 彻底消除冗余：将先前重复的“路径栏”与“当前/最终指标两块卡片”合并为一体化的【决策看板】，左侧清晰展示当前生效出口（如 `🇺🇸 美国节点 · 默认主选`），中间以轻量流动箭头连接，右侧展示最终落地真实节点及健康延迟（如 `🇺🇸 美国 01 · 圣何塞 BGP · 148ms · 优选在线`）；
         - 候选出口切换器紧凑收纳于看板下方：当前生效项高亮为实心品牌色胶囊（`● 🇺🇸 美国节点`），候选项以简洁微框呈现（`○ 🇯🇵 日本节点`），点击即可即时切换首选出口；
         - 将先前额外占据一整行的“快捷添加出口”整合为候选药丸列表尾部的轻量下拉按钮（`＋ 添加可选出口 ▾`）。
       - **层级 3（关联元数据底栏 Footer）**：
         - 分流规则与高级设置统一收敛为轻量化底栏，以弱化视觉噪音，左侧展示命中规则标签，右侧提供快速新建规则入口。
     - **地区组看板精简与层级对齐**：
       - 地区组同样遵循三层体系：顶部身份控制 -> 中部节点优选看板与关键词单行筛选（支持一键展开/收起预览） -> 底部展示被用途组引用的反查元数据。
     - **修复区域节点解析回退错误**：
       - 优化 `resolveSymbolicNode`：当组别属于特定地区（美/日/港/新/台/韩）且当前系统中尚无该地区节点实例时，优先返回该地区的真实落地预览（如 `🇺🇸 美国 01 · 圣何塞 BGP`），杜绝跨区域随机回退至第一台服务器（东京入口）的错乱现象。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **视觉层级**：从 7 层杂乱堆叠精简重构为 3 层分明结构，卡片视觉重心直指核心“当前走哪、落在哪、如何一键切换”；
  - **空间利用**：去除了大量冗余灰框与内外边框嵌套，整体布局呼吸感和高级感显著提升；
  - **接口契约**：完全保持前端与后端 Go 接口模型一致，0 契约改动。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全量测试 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，全部 20 项 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.40s 顺利完成生产构建打包）。
  5. `git status --porcelain`：确认仅修改了 `web/` 内的前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：工作区中保留其他目录的历史未提交改动，本次未受影响。
  - 限制：保持既有限制，无新增限制。
- **是否涉及接口契约变化**：
  - 无（严格保持接口契约不变）。

---

## 轮次 15：卡片贴边与内外间距呼吸感深度优化（解决贴边拥挤与底部截断问题）

- **日期和时间**：2026-11-04T08:18:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户在查看代理组页面后反馈截图，红框框选了卡片四周的贴边区域（左侧贴边、右侧贴边、卡片上下间隙过窄、底部关联规则紧贴卡片底边及滚动视口底部），明确提出指令：“太贴边了”。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **右侧滚动主视口外边距与底部留白优化**：
       - 修改前：右侧面板容器类名为 `px-1 space-y-6 h-full pr-2`，仅有 4px 左右微小留白，导致卡片紧贴左侧侧边栏分割线和右侧滚动条；且缺少底部 padding，滚动到底部时最后一张卡片被视口硬切贴底。
       - 修改后：将容器类名优化为 `px-4 sm:px-6 space-y-6 h-full pb-16`，赋予右侧内容区 16px~24px 的充分左右呼吸间距，并在底部增加 `pb-16`（64px）的纵向安全区域，彻底解决贴边与滚动截断问题。
     - **代理组卡片内边距（Padding）规范化修复**：
       - 修改前：卡片使用了非标准的 `p-4.5 space-y-3.5` 类名，因 Tailwind 默认间距标尺无 4.5 导致 padding 类失效（降级为 0 内边距），使卡片内所有内容（顶部标题、候选出口网格、底部关联规则）直接贴紧卡片的 4 条边框。
       - 修改后：替换为标准的 `p-5 sm:p-6 space-y-4`，为卡片内部上下左右提供标准 20px~24px 的舒适内衬边距。
     - **卡片间距与出口小卡片微调**：
       - 将卡片列表垂直间距从 `space-y-4` 提升至 `space-y-5`，卡片与卡片之间界限更清晰；
       - 候选出口小卡片内部内边距从 `p-2.5` 提升至 `p-3`，最小高度规范为 `min-h-[62px]`，小卡片间距由 `gap-2` 微调为 `gap-2.5`；
       - 卡片底部关联规则行从 `pt-2` 优化为 `pt-3`，与上方网格自然分隔。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **样式与布局**：修复了卡片内边距失效的 Bug，增加了弹窗视口内边距与底部防截断留白，卡片内外视觉舒适、层次分明、呼吸感充足；
  - **数据与契约**：无任何逻辑或数据变化，严格保持接口契约 100% 兼容。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全量测试 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，全部 20 项 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.45s 顺利完成生产构建打包）。
  5. `git status --porcelain`：确认仅修改了 `web/` 内的前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：工作区中保留其他目录的历史未提交改动，本次未受影响。
  - 限制：保持既有限制，无新增限制。
- **是否涉及接口契约变化**：
  - 无（严格保持接口契约不变）。

---

## 轮次 16：完善“为此组新建分流规则”弹窗内容与场景化智能推荐

- **日期和时间**：2026-11-04T08:31:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户在代理组页面卡片底部，截图使用红框框选了“+ 为此组新建分流规则”按钮，明确提出指令：“完善一下这个页面的弹窗内容”。
  原弹窗存在以下痛点：
  1. 点击后背景被强行切到“分流规则”标签页，打断了代理组配置工作流；
  2. 弹窗缺少对当前出口组的流向感知与上下文标题（如缺少“为「AI 服务」添加分流规则”）；
  3. 规则匹配类型生硬平铺、缺乏业务场景预置，用户添加 AI、YouTube、Telegram 等规则时需手动记忆并拼写域名；
  4. 缺少多域名批量录入能力，一次只能手动添加一个域名；
  5. 优先级需用户手工填写晦涩数字；
  6. 二级弹窗层级可能受父级模态框（AppDialog）z-index 影响。
- **涉及的全部文件路径**：
  - 修改：[`web/src/components/FormDrawer.vue`](file:///C:/Users/luanf/qui/web/src/components/FormDrawer.vue)
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/components/FormDrawer.vue`](file:///C:/Users/luanf/qui/web/src/components/FormDrawer.vue)：
     - 将外层浮动遮罩容器的 `z-index` 从 `z-50` 规范为 `z-[60]`，确保作为嵌套二级弹窗浮动在策略编辑弹窗（`AppDialog` 为 `z-50`）之上时永远具有正确无误的层叠上下文。
  2. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **代理组就地创建规则，不切换模块视图**：重写 `quickCreateRuleForGroup(groupName)`，移除先前强行切换 `activeModule.value = 'rules'` 的逻辑，在当前“代理组”模块就地呼出添加规则弹窗；保存后弹窗关闭直接留在代理组卡片，底部规则芯片即时刷新呈现，操作路径丝滑连贯。
     - **动态目标流向看板 (Flow Preview)**：弹窗顶部增加流向指示栏，清晰展示：`流量匹配: [目标] ──▶ 派发给当前组: 🎯 [分组名称]`，强化用户对“流量命中后交由此组转发”的因果关系认知。
     - **场景化快捷推荐模板 (Quick Presets)**：新增 `currentGroupPresets` 智能计算属性。根据当前代理组定位（AI 智能助手、YouTube 视频、Telegram 社交、流媒体、或通用兜底），自动呈现一键点选的常用模板（例如 ChatGPT `openai.com`、Claude `claude.ai`、Gemini `gemini.google.com`、YouTube `youtube.com`、系统统一规则包等），点击即自动完成类型、匹配内容与描述的填充。
     - **分类直观的匹配类型选择 (Optgrouped Match Types)**：按业务语义细分为“常用域名匹配 (推荐)”、“统一规则集 (RULE-SET)”、“地理与分类库 (GEO)”及“网络与端口高级匹配”，附带详细机制说明。
     - **支持多域名批量智能录入**：域名输入支持单行或多行文本框（逗号/换行分隔），系统自动检测并提示“已识别出 N 个域名，保存时将自动批量创建 N 条分流规则”，支持一次性高效批量下发。
     - **优先级智能自动推导与高级折叠**：自动计算排在常规规则后、MATCH 兜底规则前的安全优先级数值；提供展开高级设置（精确优先级、no-resolve 避免 DNS 泄露、启用开关）。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **交互体验**：彻底告别跳转离开页面的割裂感，支持一键套用模板、批量添加域名，极大地降低了分流规则的学习门槛与配置成本；
  - **数据完整性**：保存时自动生成标准唯一的 ClientRule 对象与自增优先级，无缝触发 `markDirty()`，数据模型与 Clash / Mihomo 配置文件 100% 兼容。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全部 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.45s 完成生产打包）。
  5. `git status --porcelain`：确认仅修改了 `web/` 内的前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：保留其他目录的历史未提交改动，本次未受影响。
  - 限制：保持既有限制，无新增限制。
- **是否涉及接口契约变化**：
  - 无（严格保持接口契约不变）。

---

## 轮次 17：移除代理组顶部工具栏中的“⚡ 推荐标准拓扑”按钮

- **日期和时间**：2026-11-04T08:53:30-08:00（UTC-08:00）
- **本轮原始需求**：
  用户在策略配置编辑弹窗的“代理组编排 (Proxy Groups)”面板中，截图红框框选了右上角操作按钮群中的“⚡ 推荐标准拓扑”按钮，明确提出指令：“去掉”。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **从代理组顶部操作栏彻底移除推荐拓扑按钮**：删除了 `<button @click="applyStandardTopology" ...><span>⚡ 推荐标准拓扑</span></button>` 元素。
     - **优化顶部操作区视觉**：移除该冗余快捷入口后，顶部操作栏仅保留核心高频动作「刷新延迟」、「＋ 新建用途组」和「＋ 新建地区组」，按钮层次清晰专注，防止误触覆盖自定义配置。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **样式与排版**：顶部操作栏更紧凑清爽；
  - **数据与契约**：仅影响顶部按钮视图层，后端契约与所有自测完全保持 100% 兼容。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全部 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.41s 完成生产构建打包）。
  5. `git status --porcelain`：确认仅修改了 `web/` 内的前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：保留其他目录的历史未提交改动，本次未受影响。
  - 限制：保持既有限制，无新增限制。
- **是否涉及接口契约变化**：
  - 无（严格保持接口契约不变）。

---

## 轮次 18：代理组卡片底栏操作按钮改为“编辑规则”及智能跳转与就地编辑

- **日期和时间**：2026-11-04T08:56:50-08:00（UTC-08:00）
- **本轮原始需求**：
  用户在代理组卡片底部的关联规则区域，截图红框框选了“+ 为此组新建分流规则”按钮，明确提出指令：“改成编辑规则”。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **文案与动作统一升级**：将代理组卡片底部的操作按钮文案从 `＋ 为此组新建分流规则` 简化并规范为 `编辑规则`。
     - **智能自适应规则操作 (handleGroupRuleAction)**：
       - 当该分组**恰好有 1 条关联规则**时：点击直接在当前页面呼出编辑抽屉进行就地修改；
       - 当该分组**有多条关联规则**（如 PROXY 组）时：点击自动跳转至分流规则列表板块，并自动设置搜索关键词为当前组名（`ruleSearchKeyword = groupName`），展示该组名下的全部规则并支持集中调整与拖动排序；
       - 当该分组**暂无规则**时：点击呼出新建规则抽屉为该组创建首条分流规则。
     - **单项规则直接就地编辑**：关联规则列表中的每一个规则芯片（`span`）增加鼠标悬停与点击交互（`@click="openEditDrawer(r)"`），用户无论何时均可直接点击特定规则芯片就地微调。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **文案**：卡片底栏右侧操作按钮统一为精准紧凑的「编辑规则」；
  - **交互体验**：彻底解决了有规则时依然显示“新建规则”的语义冲突，提供了“单规则就地改、多规则列表管、单芯片点选编辑”的丝滑体验；
  - **数据与契约**：无接口变动，100% 保持契约兼容。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全部 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.45s 顺利完成生产构建打包）。
  5. `git status --porcelain`：确认仅修改了 `web/` 内的前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：保留其他目录的历史未提交改动，本次未受影响。
  - 限制：保持既有限制，无新增限制。
- **是否涉及接口契约变化**：
  - 无（严格保持接口契约不变）。

---

## 轮次 19：分流规则抽屉快捷规则集填入 & 规则集模块同步服务规则集表格视图

- **日期和时间**：2026-11-04T09:16:30-08:00（UTC-08:00）
- **本轮原始需求**：
  1. 用户指令“这里改为显示规则集里面的内容”并圈选分流规则创建/编辑抽屉中的常用推荐模板区域：要求将该预置推荐区域改为直接展示系统已有的规则集内容（如 OpenAI、YouTube、GitHub、Google、Netflix、Telegram、Disney+ 等服务规则集），点击直接一键填入该规则集的分流配置；并在选择规则集时直接展示其收录域名与匹配规模的实时预览。
  2. 用户指令“这里同步显示图2的内容”并提供服务规则集管理页面的截图（图2）：要求在策略编辑弹窗内的“规则集 (RULE-PROVIDERS)”模块，不再使用简单两列卡片，而是完全同步图2的页面结构、两端机制说明及完整表格列排版（包含规则集名称与内置徽标、来源/版本、匹配规模、CLASH RULE-PROVIDER 引用配置、状态、更新时间及引用/编辑/克隆操作）。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **分流规则抽屉快捷规则集填入 (`availableServiceRulePresets`)**：
       - 将抽屉原有的固定域名模板替换为动态加载的系统规则集（`serviceSets`）芯片；
       - 根据目标策略组名称（如 AI、YouTube、流媒体）智能优先排序最相关的规则集；
       - 点击任一规则集芯片，立即自动设置 `type = 'RULE-SET'`、`payload = 'service:' + set.id` 与描述，高亮选中状态；
       - 在 `RULE-SET` 选择框下方新增【规则集详情展开预览卡片】，展示当前规则集版本、内置标识、收录的域名/IP总规模，并实时展示前若干个具体域名标签芯片。
     - **规则集模块完全同步图2结构**：
       - **头部与机制提示**：顶部增加【＋ 创建规则集】与【＋ 新增外部规则集】操作入口，下方展示完整的“规则集定位与两端更新生效机制”说明横幅（明确纯目标定义与两端更新差异）；
       - **数据表格化展示**：完全同步图2的 7 列布局：`规则集名称`（含内置徽标与说明）、`来源 / 版本`（v1 徽章）、`匹配规模 (域名 / IP)`、`CLASH RULE-PROVIDER 引用`（Provider 配置快捷按钮）、`状态`（正常启用指示灯）、`更新时间`（时间格式化）、`操作`（引用、编辑、克隆）；
       - **Provider 引用配置弹窗 (`providerModalVisible`)**：点击表格中的“Provider 配置”按钮呼出弹窗，提供完整的 Clash / Mihomo 配置 YAML 片段一键复制与订阅直链 URL 复制；
       - **规则集就地编辑与创建抽屉 (`serviceDrawerVisible`)**：支持就地创建自定义规则集或编辑现有规则集的名称、说明、精确域名、域名后缀与 IP 网段列表，保存后自动同步服务端并刷新列表。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **文案与表单**：
    - 规则抽屉中推荐栏标题更新为“📦 可用规则集 (点击一键填入分流)”；
    - 规则集模块表头规范为图2一致的 7 个标准列；
  - **样式与布局**：
    - 统一采用 DataTable 风格的圆角白色表格，搭配斑马悬停色与状态指示点；
    - 响应式排版保证窄屏横向平滑滚动，不出现截断；
  - **交互体验**：
    - 从“查看规则集”到“一键为客户端策略创建分流规则（引用按钮）”全链路打通；
    - 点击 Provider 配置就地获取订阅链接与 YAML，无需跳转离开当前策略弹窗。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，所有 UI 回归测试全数 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，生产包 4.46s 构建完成）。
  5. `git status --porcelain`：确认未变动任何后端、Agent、协议或 `web/` 以外的文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：保留既有未提交改动，本次任务完全隔离在 `web/` 范围内。
  - 限制：严格遵循前端与既有 API 契约，无破坏性变更。
- **是否涉及接口契约变化**：
  - 无（严格复用既有的 `policyAPI.services`、`policyAPI.saveService` 等接口，无契约变更）。

---

## 轮次 20：移除分流规则列表顶部的「规整优先级」操作按钮

- **日期和时间**：2026-11-04T09:19:50-08:00（UTC-08:00）
- **本轮原始需求**：
  用户通过截图圈选分流规则列表顶部标题栏中的「规整优先级」按钮，发出指令“去掉”。要求移除该按钮，精简分流规则模块的顶部操作栏。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **移除顶部按钮**：在 `activeModule === 'rules'` 模块顶部的操作区域中，删除了「规整优先级」次级按钮，仅保留最核心的【＋ 添加规则】操作入口；
     - **样式与排版**：标题栏右侧操作区更为精炼，避免与添加规则产生视觉冲突。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **视觉体验**：分流规则列表工具栏去除了非必要的冗余按钮，界面更聚焦于规则内容与新增规则；
  - **数据处理**：原有规则列表的排序与拖动逻辑保持稳定运行。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，所有 UI 回归测试全数 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，4.50s 构建顺利完成）。
  5. `git status --porcelain`：确认未修改任何非前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：无新增影响。
  - 限制：无。
- **是否涉及接口契约变化**：
  - 无（纯前端视图按钮精简）。

---

## 轮次 21：分流规则列表支持直接拖拽调整顺序与优先级联动

- **日期和时间**：2026-11-04T09:23:20-08:00（UTC-08:00）
- **本轮原始需求**：
  用户在分流规则列表页面反馈“这里我需要可以自己拖动位置”，要求支持直接在表格中拖拽调整单条规则的位置顺序。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **拖拽句柄与视觉指示**：在规则行优先级前方增加了 6 点拖拽抓手图标（`⠿`），鼠标悬停呈现 `cursor-grab`，按住拖动呈现 `cursor-grabbing`；整行均支持 `draggable="true"`；
     - **精准放置指示线**：拖拽经过目标行时，根据光标处于行上半部分或下半部分动态呈现蓝色边界线（`border-t-2 border-ios-blue` 或 `border-b-2 border-ios-blue`）；
     - **优先级全自动重排联动**：规则释放（`drop`）后，自动在当前数组中完成重排，并自动将所有规则的优先级编号依序规整为 `#10, #20, #30...`，确保规则生效顺序与界面视觉排序完全一致；
     - **状态与提醒**：标脏当前策略（激活【保存 (已修改)】），并弹出轻量 Toast 提示移动成功；保留原有的上移/下移箭头操作以保证双重可用性。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **交互体验**：彻底满足自由拖动排序规则的体验诉求，无需多次点击上下移动按钮；
  - **视觉反馈**：拖动时被拖动行半透明虚化，目标位置高亮蓝线指引，放置顺畅丝滑；
  - **数据一致性**：顺序与 priority 双重同步，确保导出的 YAML 与客户端运行时严格按照拖动后的顺序执行分流。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，所有 UI 回归测试全数 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，4.55s 构建打包成功）。
  5. `git status --porcelain`：确认未变动任何非前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：无。
  - 限制：保持无接口变更。
- **是否涉及接口契约变化**：
  - 无（纯前端视图层拖拽重排与 priority 重新序列化）。


---

## 轮次 22：分流规则列表交互与可读性深度重构（单行筛选、中文类型、四列简化表格、排序明确化与固定底部兜底规则）

- **日期和时间**：2026-11-04T09:38:00-08:00（UTC-08:00）
- **本轮原始需求**：
  根据用户对分流规则列表的优化建议：
  1. **筛选区合并成一行**：将原本多行占高的输入框改成单行：`搜索规则…` ｜ `规则类型 ▼` ｜ `处理目标 ▼`；从代理组卡片进入时，展示筛选标签：`目标: PROXY ×`，支持一键点击清除；
  2. **匹配类型用中文，技术名称作为辅助**：主标签采用易读中文（“域名关键词”、“域名及子域名”、“IP 网段”、“规则集”等），原始技术名称（如 `DOMAIN-SUFFIX`）在悬停或详情提示中呈现，彻底消除长英文类型换行挤压；
  3. **突出“匹配什么 → 交给谁”**：表格简化为 4 列核心结构：`顺序`、`匹配条件`（含备注、可点击的规则集）、`处理目标`（可点击的代理组跳转）、`操作`；
  4. **明确排序逻辑**：顶部横幅提示“从上往下匹配，先命中的生效”；使用连续执行序号 `1, 2, 3...` 结合 `#10, #20` 权重呈现；在搜索/筛选状态下自动锁定拖拽排序，并提示清除筛选后调整；
  5. **兜底规则固定在列表底部**：将 `MATCH` 规则从常规拖拽表格中剥离，在列表最下方以专属底卡常驻呈现（“其他未匹配流量 → 默认代理 (MATCH 兜底)”），不可拖动到普通规则前面，支持就地查看、编辑与状态切换；
  6. **减少操作图标，改善空间利用**：启用状态采用 iOS 极简开关代替原有文字标签；删除与微调收进“更多”操作浮层；弹窗左侧导航宽度缩窄，说明文字移入浮窗悬停提示，为规则表格释放充裕的横向宽度。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **左侧导航栏瘦身**：将宽度从 `md:w-80 lg:w-[350px]` 缩窄至 `md:w-56 lg:w-60`，模块按钮内的多行文本移除并转为 `:title="mod.desc"` 悬停提示，释放 110px 以上横向空间给右侧规则工作台；
     - **排序逻辑横幅**：规则列表头部增加全局指引条“分流匹配遵循从上往下匹配，先命中的生效原则”；筛选态下提示“筛选状态下已锁定拖动排序，清除筛选后可调整”；
     - **单行精简筛选栏**：整合为 `搜索规则…`、`规则类型 ▼`、`处理目标 ▼`，代理组进入或选定目标时显示高亮胶囊标签 `目标: {target} ×`，点击一键清除并重置筛选；
     - **匹配类型中文映射与辅助技术标识**：实现 `getRuleTypeChineseName` 映射（`DOMAIN-KEYWORD` → 域名关键词、`DOMAIN-SUFFIX` → 域名及子域名、`IP-CIDR` → IP 网段、`RULE-SET` → 规则集等），徽标提供 `:title` 查看原始技术字段；
     - **四列极简规则表格**：
       - `顺序` 列：展示连续执行序号 `1, 2, 3...` 及优先级权重 `#10, #20...`，筛选态下 ⠿ 抓手呈锁定禁用态，全量态下可自由抓拽；
       - `匹配条件` 列：`匹配类型 · 目标值`，如果是统一规则集支持点击直达规则集模块，下方紧凑排列备注与 `no-resolve` 徽标；
       - `处理目标` 列：规范化动作语义（“直连”、“拦截 (REJECT)”、“默认代理 (PROXY)”），如果是代理组支持带 `↗` 图标点击直接跳转到代理组板块并自动定位；
       - `操作` 列：状态切换收敛为 iOS 开关组件，提供高频的【编辑】入口以及【更多】下拉菜单（上移一位、下移一位、复制规则、删除规则）；
     - **固定底部 MATCH 兜底规则卡片**：将 `MATCH` 规则独立抽出并在列表最末端固定渲染，视觉层与逻辑层彻底与普通规则解耦，确保普通规则上下拖拽永远不会跨过兜底规则。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **交互体验**：筛选区域不占垂向高度，代理组联动带胶囊标签一键清除；拖拽排序与执行序号直观对应；跨模块快速跳转（规则集与代理组一键直达）；
  - **样式与布局**：左侧大幅减负，表格视野宽广开阔；四列布局层次鲜明，不再出现英文换行挤压；
  - **数据完整性**：拖拽移动与优先级分配由 `sortedRegularRules` 独立运算，末尾 MATCH 规则始终保有最高权重编号，完美符合 Clash 分流内核规范。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，UI 回归测试全数 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20 项客户端策略运算自测全数 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 生产构建编译耗时 4.18s）。
  5. `git status --porcelain`：确认未变更任何 `web/` 外的文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：无。
  - 限制：保持无后端接口变更。
- **是否涉及接口契约变化**：
  - 无（纯前端视图重构与交互优化）。


---

## 轮次 23：一体化分流规则筛选工具栏重构与横向溢出消除（搜索规则…｜规则类型 ▼｜处理目标 ▼）

- **日期和时间**：2026-11-04T17:49:00-08:00（UTC-08:00）
- **本轮原始需求**：
  根据用户反馈截图（筛选栏原分散输入框宽度挤压且右侧存在大片空白、且弹窗右侧面板产生横向滚动条）：
  1. 将原本分散的独立表单输入框深度重构为一体化胶囊工具栏：`搜索规则…｜规则类型 ▼｜处理目标 ▼`；
  2. 搜索框支持一键清空（快捷 `×` 按钮），规则类型与处理目标作为内联分段下拉菜单无缝整合；
  3. 彻底消除容器横向溢出问题，确保不同屏幕宽度下工具栏平滑充满容器，消除弹窗底部的横向滚动条。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **一体化分段工具栏**：采用 `bg-white rounded-xl border divide-x divide-black/[0.06]` 将搜索框、规则类型选择器与处理目标选择器合并为一个精致连续的整体胶囊组件，完美贴合用户 `搜索规则…｜规则类型 ▼｜处理目标 ▼` 的视觉形态；
     - **搜索快捷操作**：搜索框内置放大镜图标与一键清除 `×` 按钮；
     - **内置原生级下拉指示器**：每个下拉选择区配备原生级微小箭头 SVG 指示器，并设置 `appearance-none` 与右侧合理内边距，交互精准；
     - **容器横向滚动条消除**：为弹窗右侧内容面板追加 `overflow-x-hidden` 与 `min-w-0`，彻底根除由于子元素最小尺寸导致的弹窗底部横向滚动条。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **样式与美观度**：工具栏由原来零散孤立的两个小框升级为充满整行的一体化现代控件，左右留白均匀和谐；
  - **交互一致性**：筛选目标时依然支持胶囊标签一键清除，重置按钮与条目计数靠右对齐。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全数 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，4.93s 构建打包成功）。
  5. `git status --porcelain`：确认未变动任何非前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：无。
  - 限制：无。
- **是否涉及接口契约变化**：
  - 无。


---

## 轮次 24：移除规则列表顶部常驻提示横幅，优化纵向空间利用

- **日期和时间**：2026-11-04T17:53:00-08:00（UTC-08:00）
- **本轮原始需求**：
  根据用户反馈截图与指令（红框圈出分流规则列表顶部的提示条“分流匹配遵循从上往下匹配，先命中的生效原则”并指示“去掉”）：
  - 彻底移除规则列表顶部多余的提示横幅，将宝贵的垂直空间留给规则筛选和表格本身；
  - 将匹配原则精炼合并到模块主标题下方的辅助说明文案中（“rules 决定请求交给哪个节点或代理组（从上往下先命中生效），可指定直连 DIRECT 或拦截 REJECT”）。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - 从分流规则模块顶部完全删除了 `<div class="flex items-center justify-between text-xs px-3 py-2 bg-blue-50/50 ...">` 提示横幅；
     - 保持序号旁拖拽手柄上的悬停提示功能与状态锁定逻辑；
     - 顶部说明微调为“rules 决定请求交给哪个节点或代理组（从上往下先命中生效），可指定直连 DIRECT 或拦截 REJECT。”。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **样式与布局**：标题栏下方直接衔接一体化筛选工具栏，纵向视界更加紧凑专业，避免横幅带来的空间侵占与视觉疲劳。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全数 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，4.22s 构建打包成功）。
  5. `git status --porcelain`：确认未变动任何非前端文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：无。
  - 限制：保持无后端接口变更。
- **是否涉及接口契约变化**：
  - 无。

---

## 轮次 25：重构分流规则模块视觉层级，整合一体化工作台卡片并严格对齐兜底规则

- **日期和时间**：2026-11-04T18:05:00-08:00（UTC-08:00）
- **本轮原始需求**：
  根据用户反馈截图（红框圈出分流规则整个右侧内容区）与评语“层级混乱”：
  1. 彻底解决旧版右侧面板多个孤立浮动卡片（标题区、悬浮筛选条、规则表格、底部兜底卡片）造成的视觉碎片化与层级无序；
  2. 重构视觉层级为清晰的两级架构：
     - **第一级（页面顶层）**：清晰的模块标题 `分流规则列表 (Rules)`、条目计数胶囊徽标（`共 X 条`）、精炼辅助说明（明确从上往下匹配原则与兜底机制）以及右上角主操作按钮（`+ 添加规则`）；
     - **第二级（一体化工作台卡片）**：将筛选工具栏、规则数据表格与兜底规则完整收纳在单一白色圆角容器卡片中（`bg-white rounded-2xl border border-black/[0.08] shadow-2xs overflow-hidden`）；
  3. **兜底规则与表格列严格对齐**：将原本独立悬浮、列宽不一且对齐混乱的 `MATCH` 兜底卡片重构为表格底部的固定行（`tfoot`），其 4 列（`顺序` 兜底标识、`匹配条件` 其他未匹配流量、`处理目标` 代理组/直连/拦截、`操作` 开关与编辑）与上方表头及数据行保持像素级完全一致对齐。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **顶层标题与操作区**：
       - 主标题旁增加 `共 {{ clientConfig.rules.length }} 条` 计数徽标；
       - 下方说明文字阐明“按从上到下顺序匹配，先命中生效；未命中流量由底部兜底规则接管”；
       - 右侧突出单一主操作 `+ 添加规则`。
     - **一体化卡片容器**：
       - 外层使用统一的 `rounded-2xl border border-black/[0.08] shadow-2xs` 容器卡片，消除了原本工具栏、表格和底部卡片各自独立边框导致的层级撕裂感；
       - **卡片顶栏**：集成单行胶囊筛选工具栏（`搜索规则…｜规则类型 ▼｜处理目标 ▼`），支持目标筛选清除胶囊与重置操作；
       - **卡片主体**：数据表格区域保持清晰 4 列布局（`顺序` / `匹配条件` / `处理目标` / `操作`），支持拖拽排序、状态开关、行内编辑与更多操作菜单；
       - **卡片底栏 (`tfoot`)**：将 `matchRule`（兜底规则）移入 `table` 的 `tfoot` 中，使用固定底栏样式（`sticky bottom-0 z-10`），无论列表如何滚动，兜底规则始终吸附在表格底端，且其处理目标列与操作列与普通规则列严格垂直对齐。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **视觉层级**：从原先“标题 + 提示栏 + 悬浮筛选条 + 悬浮表格 + 悬浮兜底卡片”5 层零碎元素，整合成“顶层标题区 + 单一工作台卡片（工具栏 + 表格 + 底部兜底）”的专业设计规范；
  - **对齐与可读性**：彻底解决兜底卡片内目标和开关与上方表格列不对齐的问题，界面整洁规范。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全数 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，生产环境构建编译成功）。
  5. `git status --porcelain`：确认仅修改前端规范允许范围内的文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：无。
  - 限制：保持无后端接口变更。
- **是否涉及接口契约变化**：
  - 无。

---

## 轮次 26：优化弹窗底部空白过大问题（消除生硬固定高度与过度留白）

- **日期和时间**：2026-11-04T18:12:00-08:00（UTC-08:00）
- **本轮原始需求**：
  根据用户反馈截图（红框圈出规则工作台卡片下方的巨大白色空白区域）与评语“下面空太多了”：
  1. 彻底排查弹窗内部高度与底部巨大留白的成因；
  2. 修正模态弹窗外层强加的生硬固定高度 `h-[88vh] max-h-[900px] min-h-[640px]`，改为内容自适应高度（限制最大高度 `max-h-[86vh]`）；
  3. 清除右侧内容滚动容器底部历史遗留的多余下内边距（`pb-16` -> `pb-2`），将纵向间距由 `space-y-6` 紧凑优化为 `space-y-4`；
  4. 规则列表表格的 `max-h` 从生硬的 `440px` 提升至 `540px`，使更多规则可完整展示，彻底消除表格下方空洞的大面积空白。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **弹窗高度适应性**：将 `AppDialog` 的 `heightClass="h-[88vh] max-h-[900px] min-h-[640px]"` 更改为自适应容器高度 `heightClass="max-h-[86vh]"`，弹窗高度由内部实际内容驱动，内容较少时不会被强行撑高到 900px；当规则条目或代理组很多时，依然能平滑向上展开至 86vh 并触发内容滚动；
     - **移除右侧内容区历史遗留留白**：将右侧模块内容容器由 `space-y-6 h-full pb-16` 优化为 `space-y-4 pb-2`，消除原本因 `pb-16` 额外产生的 64px 冗余空白；
     - **移除左右列强制 100% 高度束缚**：移除左右列无意义的 `h-full`，使左右面板自然依内容高度伸缩；
     - **扩展表格适宜展示高度**：将规则表格高度上限由 `max-h-[440px]` 扩充至 `max-h-[540px]`，保证多条目下的可读性与利用率。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **视觉体验与比例感**：模态弹窗不再呈现突兀的“大弹窗装小卡片、下半截全留白”的不协调感，弹窗底部与工作台卡片底部保持精致适度的 24px 呼吸感边距，视觉聚焦且紧凑。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全数 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，生产环境构建编译成功）。
  5. `git status --porcelain`：确认仅修改前端规范允许范围内的文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：无。
  - 限制：保持无后端接口变更。
- **是否涉及接口契约变化**：
  - 无。

---

## 轮次 27：维持弹窗固定工作台高度，重构分流工作台卡片纵向全高自适应布局

- **日期和时间**：2026-11-04T18:15:00-08:00（UTC-08:00）
- **本轮原始需求**：
  根据用户最新要求“高度还是要固定”：
  1. 完整恢复模态弹窗的固定工作台高度（`heightClass="h-[88vh] max-h-[900px] min-h-[640px]"`），确保在不同分辨率下拥有沉浸且稳定的工作台高度，切换左侧配置模块时不产生任何窗口晃动或跳跃；
  2. 针对在固定高度下“下面空太多了”的痛点，重构分流规则右侧面板的纵向布局：
     - 将规则工作台卡片设为 `flex-1 min-h-0 flex flex-col`，纵向充满整个右侧工作区直到弹窗底部；
     - 搜索与筛选工具栏固定在卡片顶端（`shrink-0`）；
     - 中间规则表格区域设为 `flex-1 min-h-0 overflow-y-auto`，支持在卡片内部独立滚动；
     - 兜底规则（MATCH）作为底部独立固定行（`shrink-0 bg-[#F8F9FA] border-t-2 border-black/[0.06]`），始终钉在卡片最底端，其 4 列与上方表头与数据行保持严格对齐；
     - 卡片外部不留多余空白，工作台卡片完整撑满弹窗内容区。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **恢复弹窗固定高度规范**：`AppDialog` 恢复固定工作台高度规范 `heightClass="h-[88vh] max-h-[900px] min-h-[640px]"`；
     - **右侧容器自适应弹性布局**：
       - 容器类名优化为 `flex-1 min-w-0 px-4 sm:px-6 h-full flex flex-col`，在 `activeModule === 'rules'` 时应用 `overflow-hidden pb-1`，在非规则模块时保持 `overflow-y-auto overflow-x-hidden space-y-4 pb-4`，兼顾各模块需求；
     - **分流工作台卡片全高填充**：
       - 规则模块容器设为 `flex-1 min-h-0 flex flex-col space-y-3 h-full`；
       - 一体化卡片容器设为 `flex-1 min-h-0 flex flex-col mb-1`，自顶部操作栏起一直延伸至弹窗内边距底部；
       - 中间表格区域应用 `flex-1 min-h-0 overflow-y-auto`，搭配 `table-fixed` 固定列宽；
       - 底部 MATCH 兜底规则保持 `shrink-0`，无论规则多寡均稳固吸附在卡片的最底端，列宽严格保持 `w-20` / `匹配条件` / `w-48` / `w-36` 对齐。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **整体美感与饱满度**：卡片完整铺满右侧工作台，卡片外部不再存在突兀的孤岛式大片留白；兜底规则稳坐卡片底栏，中间普通规则按需流动与滚动，整体界面饱满专业。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全数 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，生产环境构建编译成功）。
  5. `git status --porcelain`：确认仅修改前端规范允许范围内的文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：无。
  - 限制：保持无后端接口变更。
- **是否涉及接口契约变化**：
  - 无。

---

## 轮次 28：统一策略配置卡片底部操作按钮规格（三等分网格布局与标准 iOS 按钮规范）

- **日期和时间**：2026-11-04T18:22:00-08:00（UTC-08:00）
- **本轮原始需求**：
  根据用户反馈截图（红框圈出主列表页策略卡片底部的「编辑」、「已启用」和「删除」）与评语“按钮不统一”：
  1. 彻底解决原先三按钮样式、高度、边框与宽度分配极度不一致的问题：
     - 原先「编辑」为白色按钮（`flex-1`）、「已启用」为无阴影绿色块（`flex-1`）、「删除」则为孤悬在最右侧的无边框红字（`px-2.5`），导致三种形态彼此脱节且宽度失衡；
  2. 采用标准三等分网格布局（`grid grid-cols-3 gap-2 pt-3 border-t border-black/[0.06]`），赋予三个操作按钮完全均等的视觉分量；
  3. 统一度量衡与设计规范：
     - **高度与圆角**：三者统一为标准 `h-8`（32px）与 `rounded-xl`；
     - **排版结构**：均配备精准的微小 SVG 图标（`w-3.5 h-3.5`）+ 文本，居中对齐；
     - **操作 1（编辑）**：标准次级按钮 `ios-btn-secondary text-xs h-8`；
     - **操作 2（启用 / 已启用）**：未启用时为鲜明首要按钮 `ios-btn-primary`；已启用时为规范统一的次级绿色徽标按钮（带常亮绿点指示灯）；
     - **操作 3（删除）**：拥有完整白色按钮实体与灰色边框（生效策略不可删除时展现优雅的禁用状态与提示），鼠标悬浮展现平滑的红色警示高亮。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - 将卡片底部原本的 `flex items-center gap-2 pt-2` 重构为严格均分的 `grid grid-cols-3 gap-2 pt-3`；
     - 「编辑」添加铅笔图标，保持 `ios-btn-secondary`；
     - 「启用/已启用」区分激活与非激活状态，均统一保持 `h-8 rounded-xl shadow-2xs` 规范；
     - 「删除」彻底摒弃原本毫无边框与背景的 `border-transparent` 文字样式，升级为实体次级危险按钮（垃圾桶图标 + 实体边框 + 禁用状态反馈）。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **整齐度与协调感**：三个操作按 1:1:1 比例平铺卡片底部，高矮、圆角、边框、投影严格统一，界面视觉平衡，不再有参差不齐感。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全数 PASS）。
  3. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，生产环境构建编译成功）。
  5. `git status --porcelain`：确认仅修改前端规范允许范围内的文件。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：无。
  - 限制：保持无后端接口变更。
- **是否涉及接口契约变化**：
  - 无。

---

## 轮次 29：封装通用 Apple/iOS 风格下拉选择组件 (AppSelect) 并全面统一配置下拉框

- **日期和时间**：2026-11-04T18:45:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户针对配置弹窗中原生 Windows 浏览器 `<select>` 下拉框（截图重点标出 DNS 模块中的“增强模式 (Enhanced Mode)”）反馈原生下拉菜单深灰高反差、直角无内边距的生硬外观，提出明确指令：“这个下拉框优化之后统一一下”。
- **涉及的全部文件路径**：
  - 新增：[`web/src/components/AppSelect.vue`](file:///C:/Users/luanf/qui/web/src/components/AppSelect.vue)
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/components/AppSelect.vue`](file:///C:/Users/luanf/qui/web/src/components/AppSelect.vue)：
     - **新创高品质 iOS 风格下拉框组件**：
       - **触发框 (Trigger)**：支持 `size="sm"`（紧凑型）与 `size="md"`（标准型），提供圆角 squircle（`rounded-xl`）、微妙投影（`shadow-2xs`）、自然边框与微交互 hover/focus ring 状态；
       - **旋转动效 Chevron 箭头**：右侧集成平滑 `rotate-180` 旋转动画的 SVG 指示箭头；
       - **浮层菜单 (Floating Popover Menu)**：采用半透明磨砂毛玻璃（`bg-white/95 backdrop-blur-md`）、大圆角（`rounded-2xl`）、轻柔层次阴影（`shadow-xl`）；
       - **富文本选项呈现**：支持主文本（`label`）、图标（`icon`）、特性徽标（`badge`）、次级辅助说明（`desc`）及蓝色选中指示勾（`✓`）；
       - **分组支持 (Group Headers)**：支持 `group` 字段，选项跨组时自动呈现雅致的小写浅灰大写分组标题与细分割线；
       - **键盘与点击交互**：支持上下键导航选项、Enter 选中、Esc 关闭，以及完善的全局 Click-Outside 点击外部自动收起。
  2. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **全面替换原生 `<select>` 为 `<AppSelect>`，统一全页设计语言**：
       - **DNS 模块（用户直接截图处）**：将 `增强模式 (Enhanced Mode)` 原生下拉框升级为 `AppSelect`，清晰展示 `fake-ip`（带「推荐」徽章与低延迟响应说明）及 `redir-host`（传统兼容解析说明）；
       - **基础设置模块**：将 `允许局域网连接 (Allow LAN)` 和 `日志级别 (Log Level)` 统一升级为 `AppSelect`，日志等级（Info / Warning / Error / Debug / Silent）附带运行开销与排错建议；
       - **代理组模块**：将策略组卡片顶部的策略类型下拉框升级为紧凑型 `AppSelect size="sm"`（手动选择 / 自动测速 / 故障转移）；
       - **分流规则工作台**：将顶部单行工具栏中的 `规则类型 ▼` 与 `处理目标 ▼` 筛选器升级为无边框轻量 `AppSelect size="sm"`，集成动态 Chevron，消除原生箭头不对齐问题；
       - **规则添加/编辑抽屉**：将 `规则匹配类型 (form.type)`、`统一规则集引用 (form.payload)`、`命中后派发出向 (form.target)` 升级为结构化分组 `AppSelect`；
       - **规则集模块 (Rule Providers)**：将新建提供者的 `匹配行为 (behavior)` 与 `格式 (format)` 升级为 `AppSelect`；
       - **YAML 预览模块**：将管理员测试 `目标用户` 与 `客户端格式` 选择器统一接入 `AppSelect size="sm"`。
- **表单、文案、样式、交互和前端数据处理的全部变化**：
  - **视觉体验**：彻底根除浏览器默认的生硬灰色直角下拉弹出窗，全页下拉交互与 iOS 毛玻璃设计语言无缝契合；
  - **数据一致性**：完全保持 `v-model` 双向绑定、数据变更触发 `markDirty()`、表单提交与自测试题语义不变。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，全部 20 项自测通过）。
  3. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全部回归用例通过）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.36s 编译打包成功）。
- **本轮新增问题、原有问题、未完成事项和限制**：
  - 新增问题：无。
  - 原有问题：无。
  - 限制：严格遵循前端专属变更，不修改后端 Go / Proto / 合约。
- **是否涉及接口契约变化**：
  - 无。

---

## 轮次 30：通用 FormDrawer 弹窗高度扩展与分流规则编辑弹窗舒展优化

- **日期和时间**：2026-11-04T18:54:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户在编辑分流规则弹窗展开派发目标下拉框时，截图标注红框反馈弹窗高度过矮紧凑，提出明确指令：“这个弹窗高一点”。
- **涉及的全部文件路径**：
  - 修改：[`web/src/components/FormDrawer.vue`](file:///C:/Users/luanf/qui/web/src/components/FormDrawer.vue)
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/components/FormDrawer.vue`](file:///C:/Users/luanf/qui/web/src/components/FormDrawer.vue)：
     - **增强弹窗高度度量衡与配置属性**：新增 `height?: 'auto' | 'md' | 'lg' | 'xl' | 'full'` 与 `heightClass?: string` 属性（默认 `'auto'` 保持对现有各调用方 100% 向后兼容）；
     - **支持大规格弹窗尺寸**：当传入 `height="xl"` 时，应用 `h-[82vh] min-h-[660px] max-h-[860px]`，彻底解决内部内容自然展开时被外层硬切或紧缩的问题；同时扩展支持 `width="2xl"`（`max-w-2xl`）及自定义 `widthClass`。
  2. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **分流规则编辑抽屉增加空间容量**：将规则编辑弹窗（`drawerVisible`）的尺寸属性升级为 `width="2xl" height="xl"`，视口高度达到 82vh（最小 660px），与用户红框标注区域高度完全契合；
     - **下拉选项展开无阻碍**：Target 与 Match Type 等 `AppSelect` 下拉浮层展开时拥有充足的展示空间，无需滚动即可轻松查看所有策略组与动作选项。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  3. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全部回归用例通过）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.20s 生产打包成功）。
- **是否涉及接口契约变化**：
  - 无。

---

## 轮次 31：优化节点分组左侧侧栏搜索框样式与文本图标间距

- **日期和时间**：2026-11-04T18:57:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户在节点分组管理页面截图标注红框选中左侧搜索框，反馈搜索框内放大镜图标与占位提示文案重叠、输入框视觉生硬，提出明确指令：“优化一下”。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/proxy/pages/LineGroupPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/proxy/pages/LineGroupPage.vue)
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/proxy/pages/LineGroupPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/proxy/pages/LineGroupPage.vue)：
     - **彻底消除图标与文字重叠冲突**：此前由于通用类 `.form-input-base` 中固定 padding 覆盖了 Tailwind 的 `pl-8`，导致文本起始点与左侧 12px 处的放大镜图标产生重叠。现直接使用专用内联样式强制保障 `padding-left: 2.25rem !important`（36px），并留出舒适的 8px 间距，图标与文字彻底分离、层次分明；
     - **升级为 Apple iOS Inset Search Bar 设计**：采用磨砂凹陷底色（`bg-black/[0.04] hover:bg-black/[0.06]`）、大圆角（`rounded-xl`）、聚焦亮白背景（`focus:bg-white`）与柔和的蓝色光环（`focus:ring-2 focus:ring-ios-blue/20`）；
     - **规范化清空按钮**：清空按键升级为 iOS 原生风格的小型灰色圆形药丸按钮（`w-4 h-4 rounded-full bg-neutral-300 text-white`），操作反馈自然轻快。
  2. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - 对客户端策略分组搜索框同步应用 `padding-left: 2.25rem !important`，全面杜绝类似全局覆盖造成的图标与文字碰撞隐患。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  3. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全部回归用例通过）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.08s 编译通过）。
- **是否涉及接口契约变化**：
  - 无。


---

## 轮次 32：移除 YAML 预览模块中的“目标用户”下拉框并实现自动化预览加载

- **日期和时间**：2026-11-04T19:04:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户在客户端策略配置弹窗的“YAML 预览”模块截图标注红框，圈选了工具栏上的“目标用户: [ demo_developer v ]”下拉选择器，提出明确指令：“这个去掉”。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/routing/pages/ClientPolicyPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/routing/pages/ClientPolicyPage.vue)：
     - **移除“目标用户”选择器**：删除了 YAML 预览工具栏中的 `<div class="flex items-center gap-2"><span ...>目标用户:</span><AppSelect ... /></div>` 及其绑定的 `previewUserOptions`，界面视觉更为纯粹专注；
     - **升级预览工具栏与副标题**：将副标题更新为“预览根据当前客户端策略生成的完整订阅配置，支持一键复制完整配置”，工具栏右侧展示轻量提示文案“根据当前客户端策略实时合成，用于客户端订阅配置测试与复制”，保留“客户端格式”下拉切换与“复制完整 YAML”按钮；
     - **自动化默认用户绑定与自愈**：在系统加载用户列表（`load()`）及切换至预览模块时（`watch(activeModule)`），自动将 `previewUserId` 锁定为第一位可用用户；若未设置且切换至预览页，自动自愈获取并立即触发 `loadPreview()`，无需用户做任何手动选择即可自动展现最新生成的完整 YAML 内容；
     - **优化代码框占位文案**：预览加载占位文案由原先“# 请在上方选择测试用户查看其实际订阅渲染结果”更新为“# 正在生成客户端完整订阅 YAML 配置预览…”。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  3. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全部回归用例通过）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.17s 生产构建成功）。
- **是否涉及接口契约变化**：
  - 无。


---

## 轮次 33：AppSelect 下拉框智能检测空间不足时向上弹出（Bottom-up Placement）

- **日期和时间**：2026-11-04T19:08:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户在编辑/新建客户端分流规则弹窗中，截图标注了“命中后派发出向 (Target)”下拉框展开后的区域，反馈下拉列表向下方展开时容易挤压触底、被抽屉底栏遮挡或引发滚动条，提出明确指令：“空间不够的时候向上弹出”。
- **涉及的全部文件路径**：
  - 修改：[`web/src/components/AppSelect.vue`](file:///C:/Users/luanf/qui/web/src/components/AppSelect.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/components/AppSelect.vue`](file:///C:/Users/luanf/qui/web/src/components/AppSelect.vue)：
     - **智能边界空间感知算法（`updatePlacement`）**：在打开下拉框或父容器滚动、视口缩放时，自动向上遍历查找最近的具有滚动或截断特性的父容器（如抽屉 body、模态框、卡片等），结合视口计算触发器上方与下方的真实可用净空；
     - **动态向上弹出（`actualPlacement === 'top'`）**：当下方空间不足期望展示高度（< 220px）且上方空间更充裕时，下拉菜单自动应用 `bottom-full mb-1.5`，完美向上展开，彻底避免穿底截断或碰撞底部固定按钮栏；
     - **动态高度限制与平滑缓动**：针对弹出方向动态计算 `dynamicMaxHeight`，无论向上还是向下弹出均始终限制在可见区域内（120px ~ 256px），配合轻量反向位移动画（`translate-y-1` 到 `translate-y-0`）与自然回弹，视觉过渡丝滑；
     - **层级提升（Z-index Lifting）**：下拉框激活时容器自动提升至 `z-40`，并在滚动/缩放时动态重算方位，保证浮层始终处于顶层且位置精准。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  3. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全部回归用例通过）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 4.29s 生产构建成功）。
- **是否涉及接口契约变化**：
  - 无。

---

## 轮次 34：客户端策略 UI 后端对接审查与开发规划（未实施）

- **日期和时间**：2026-11-04T21:52:23-08:00（America/Los_Angeles）。
- **本轮原始需求**：检查调整后的客户端策略 UI，结合当前对话与关联会话《Clash 配置内容简介》（codeg://session/384），提出对应后端开发规划，尽量复用已有代码。
- **涉及的全部文件路径**：
  - 新增：`web/docs/client-policy-backend-plan.md`。
  - 追加：`web/docs/client-policy-change-log.md`。
- **具体内容**：
  - 核对配置卡片、基础设置、用途/地区代理组、分流规则、DNS、共享规则集、外部 provider 和 YAML 预览。
  - 记录本地多配置与单一服务端模板、配置版本混用、模拟延迟/虚构节点、地区组 @all、服务器名称成员、草稿预览不对应当前模板等问题。
  - 规划复用 rules.Template / Validate / Store / Expand、订阅授权来源、Clash 导出器、service-sets CRUD、Provider 路径和现有测试。
  - 提出服务端配置库、原子启用、稳定端点引用、明确空组策略、无副作用预览、共享引用保护、迁移/备份及分阶段验收方案。
- **行为变化**：无。本轮只增加规划与记录；未修改页面、后端、数据库、运行配置或现有工作区中的其他改动。
- **实际执行的验证及结果**：
  - `npm.cmd --prefix web run typecheck`：通过。
  - `node web/scripts/client-policy-selftest.mjs`：20 项通过。
  - 生产构建、Go/Linux 门禁、真实核心测试和已登录浏览器逐页走查：未执行。
- **限制与未完成事项**：规划中的 API、表、模型扩展均尚未实现；现有检查通过不代表新增 UI 已具备真实遥测、多配置持久化或草稿预览能力。
- **是否涉及接口契约变化**：当前无；规划中已明确列出未来新增接口、兼容路径和数据库迁移要求。


---

## 轮次 34：移除节点分组管理页面底部的“策略引用”卡片

- **日期和时间**：2026-11-04T21:58:00-08:00（UTC-08:00）
- **本轮原始需求**：
  用户在节点分组管理页面截图标注红框，圈选了页面最底部的“策略引用”卡片（展示“当前未在任何分流策略或客户端策略规则中被直接引用。”），提出明确指令：“去掉这个”。
- **涉及的全部文件路径**：
  - 修改：[`web/src/modules/proxy/pages/LineGroupPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/proxy/pages/LineGroupPage.vue)
  - 修改：[`web/docs/client-policy-change-log.md`](file:///C:/Users/luanf/qui/web/docs/client-policy-change-log.md)
- **每个文件的具体修改点**：
  1. [`web/src/modules/proxy/pages/LineGroupPage.vue`](file:///C:/Users/luanf/qui/web/src/modules/proxy/pages/LineGroupPage.vue)：
     - **移除页面底部“策略引用”独立卡片**：彻底删除 `<!-- 4. 策略引用卡片 (Policy References) -->` 区域，消除页面底部大面积空置面板，使节点分组详情更加紧凑聚焦；
     - **保留顶部核心指标**：顶部 4 列概览行中继续保留“策略引用”数值统计（`references.length`），维持卡片网格对称与轻量全局感知。
- **实际执行的验证及结果**：
  1. `npm.cmd --prefix web run typecheck`：通过（exit code 0，`vue-tsc --noEmit` 0 错误）。
  2. `node web/scripts/client-policy-selftest.mjs`：通过（exit code 0，20/20 PASS）。
  3. `node web/scripts/ui-regression-selftest.mjs`：通过（exit code 0，全部回归用例通过）。
  4. `npm.cmd --prefix web run build`：通过（exit code 0，Vite 5.43s 生产构建成功）。
- **是否涉及接口契约变化**：
  - 无。

---

## Documentation update: English backend development plan

- **Date and time**: 2026-11-04T22:00:38-08:00 (America/Los_Angeles).
- **Request**: Provide the Client Policy backend development plan in English.
- **Files**:
  - Added `web/docs/client-policy-backend-plan.en.md`.
  - Appended this entry to `web/docs/client-policy-change-log.md`.
- **Changes**: Translated all 12 sections of the Chinese plan, including findings, code reuse, profile storage and activation, group membership, rule sets, preview behavior, proposed APIs, implementation phases, compatibility, and acceptance criteria. Preserved the distinction between existing behavior and proposed work.
- **Behavior/API changes**: None. This is a documentation-only translation; no implementation scope was added.
- **Verification**: Checked section coverage and technical identifiers against the Chinese document. Tests were not rerun; test results in the English plan are explicitly attributed to the original review.

---

## 轮次 35：Client Policy 后端 Phase A——服务端配置库与订阅模板启用

- **日期和时间**：2026-11-04T22:31:00-08:00（America/Los_Angeles）。
- **本轮原始需求**：按批准的英文后端计划 §9 Phase A 与 §13 审查决定，实现配置库、双版本 CAS、原子启用、旧接口兼容、本地配置导入、初始化与备份恢复；不提交、不推送。
- **涉及的全部文件路径**：
  - `AGENTS.md`
  - `migrations/0132_client_policy_profiles.sql`
  - `migrations/checksums_test.go`
  - `internal/panel/subscription/rules/store.go`
  - `internal/panel/subscription/rules/profiles.go`
  - `internal/panel/subscription/rules/profiles_test.go`
  - `internal/panel/subscription/rules/rules_test.go`
  - `internal/panel/subscription/handler_http.go`
  - `internal/panel/subscription/profiles_handler.go`
  - `internal/panel/subscription/profiles_handler_test.go`
  - `internal/panel/subscription/rules_handler_test.go`
  - `internal/panel/system/backup/client_policy.go`
  - `internal/panel/system/backup/client_policy_test.go`
  - `internal/panel/system/backup/inspect.go`
  - `internal/panel/system/backup/inventory.go`
  - `internal/panel/system/backup/tunnels_test.go`
  - `internal/platform/recovery/database.go`
  - `internal/platform/recovery/tunnels_test.go`
  - `internal/platform/server/server.go`
  - `internal/platform/server/subscription_rules_test.go`
  - `internal/platform/server/testdata/portal_route_coverage.txt`
  - `web/src/modules/routing/api.ts`
  - `web/src/modules/routing/pages/ClientPolicyPage.vue`
  - `web/scripts/client-policy-selftest.mjs`
  - `web/docs/client-policy-change-log.md`
- **修改前后行为**：
  - 原卡片与启用选择存在浏览器 localStorage，卡片保存前先改本地状态，并把配置版本当作全局版本。现在通过服务端配置 API 读取与修改，成功后才采用响应中的配置、时间与两种版本。
  - 配置 revision 与 `client_rules.revision`（activeRevision）独立；活动配置保存、启用及旧 PUT 共用事务快照保存路径。普通未修改保存保持版本，首次默认模板未修改保存也不生成修订；启用相同内容仍推进 activeRevision。
  - 编辑活动配置时保留开始编辑所读的 activeRevision；其他管理员切走该配置后仍返回冲突。409 保留草稿并显示服务器当前版本，不调用 load 覆盖编辑。
  - 新配置使用完整服务端预设；本地卡片先展示名称、组数和规则数，再逐张以 `legacyId:SHA-256(content)` 导入未启用配置，显示每张结果；全部成功才清除本地卡片与旧启用键。重试不重复创建，旧浏览器启用选择不覆盖服务器。
  - 启动和恢复钩子在配置库为空时从原快照或 Default 初始化，保留 activeRevision。恢复沿用模块自己的事务钩子，不在事务中另开事务。
  - 备份保留配置表、活动指针与快照，清单统计配置和指针；校验缺表、无效模板/元数据、缺失目标及活动模板与快照不一致。旧归档迁移后初始化。旧版本测试夹具同步回退 0132，避免制造迁移序列缺口；已应用迁移未修改。
  - 保留页面结构；依计划 §9 将地区筛选标为待接入并禁用服务器名称同步，删除模拟延迟、虚构地区节点与在线统计，显示运行状态未知。明确现有 YAML 预览只预览服务器订阅模板，草稿预览留给 Phase C。
- **接口与迁移变化**：
  - 新增 0132 `client_policy_profiles`，含 nullable unique `import_key`；活动指针只含 `{profileId}`，不修改 `rules.Stored`。
  - 新增 Owner-only GET/POST `/api/v1/client-policy-profiles`、GET/PUT/DELETE `/api/v1/client-policy-profiles/{id}`、POST `/api/v1/client-policy-profiles/{id}/activate`；写操作复用 CSRF 与审计。
  - 列表沿用分页信封且不带模板；详情与写入返回 `{profile, activeProfileId, activeRevision}`。保留 `stale_rules` 与旧 GET/PUT 响应，新增 `stale_client_policy`、`client_policy_active` 和配置数量限制错误。未增加 `/validate` 或 `/resolve`。
  - 限制为 50 个配置、名称 64 字符、描述 500 字符、JSON 对象元数据 64 KiB；元数据不进入订阅。
- **设计选择及计划差异**：
  - 无功能范围偏离。引用校验在进入写事务前复用 `rules.Validate`，因为节点/策略仓储使用自身连接；事务内统一比较并写快照，避免持有单写连接时跨模块查询死锁。
  - 归档为完整 SQLite 快照，新增持久表无需另建导出格式；增加对应 inventory 与独立 per-feature 校验文件，沿用已有恢复机制。
- **实际执行的验证及结果**：
  - 使用仓库固定工具链，设置 `GOPATH=.tools/gopath`、`GOCACHE=.tools/gocache`、`GOTOOLCHAIN=local`。
  - `.tools/go/bin/go.exe vet ./...`：通过。
  - `.tools/go/bin/go.exe test ./internal/panel/subscription/... ./internal/panel/system/backup/... ./internal/platform/... ./migrations/...`：通过；备份约 94 秒，恢复约 64 秒，服务器约 223 秒。
  - `.tools/go/bin/go.exe test ./internal/panel/subscription/... ./internal/panel/system/backup/... ./internal/platform/server/... -run 'TestProfile|TestClientPolicy|TestPre0132|TestClientRuleTemplateBackupRoundTrip|TestEveryAdminRouteRefusesAPortalSession'`：通过；最终定向复核覆盖新增数量限制测试、接口、归档和恢复。
  - `npm.cmd --prefix web run typecheck`：通过。
  - `node web/scripts/client-policy-selftest.mjs`：30 项通过。
  - `node web/scripts/ui-regression-selftest.mjs`：全部通过。
  - `git diff --check`：通过。
  - 首轮沙箱运行的恢复/Agent 测试出现 Windows `Access is denied`，旧版本归档夹具还存在 0132 回退遗漏；修正夹具后，获准在沙箱外运行上述完整 Go 检查并全部通过。一次经 `.tools/gow.cmd` 传递带 `|` 的 `-run` 正则被 cmd 拆分，改用直接 Go 可执行文件后通过。
- **剩余工作与验证边界**：Phase B 的稳定端点、实际成员筛选、分类元数据和空组策略，Phase C 的草稿预览/用户上下文，以及 Phase D 的共享引用保护仍按计划后续实施。本轮未执行 Linux 门禁、真实核心套件、生产构建或已登录浏览器走查；不把 Windows 测试成功当作这些验收的证据。所有更改留在工作区，未提交或推送。
