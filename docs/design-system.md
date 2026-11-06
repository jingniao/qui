# qui 前端设计系统与视觉交互规范 (Design System Specification)

> **版本**：v1.3（2026-11-05，统一字号与可读性）
> **适用范围**：qui 管理平台所有前端页面 (`web/src`)、基础组件、表单与对话框  
> **设计基调**：轻量、克制、层级分明、触控与桌面融合的现代化 Apple iOS 视觉风格

---

## 目录
1. [核心设计原则](#一核心设计原则)
2. [色彩体系 (Color Tokens)](#二色彩体系-color-tokens)
3. [文字排版与层级 (Typography & Hierarchy)](#三文字排版与层级-typography--hierarchy)
4. [按钮系统规范 (Button System)](#四按钮系统规范-button-system)
5. [表单与控件规范 (Form Controls)](#五表单与控件规范-form-controls)
6. [模态框、抽屉与动效 (Modals, Drawers & Transitions)](#六模态框抽屉与动效-modals-drawers--transitions)
7. [侧边栏与导航系统 (Navigation & Sidebar)](#七侧边栏与导航系统-navigation--sidebar)
8. [表格与数据呈现 (DataTable & Data Display)](#八表格与数据呈现-datatable--data-display)
9. [开发自查规范与反模式 (Checklist & Anti-Patterns)](#九开发自查规范与反模式-checklist--anti-patterns)

---

## 一、核心设计原则

1. **内容沉浸，框架后退**：
   - 固定框架（侧边栏、表头、字段标签、边框）必须保持适度灰度，严禁比业务真实数据（名称、IP、流量、状态）更深更粗。
   - 页面背景采用浅冷灰 `#F5F5F7`，内容容器采用纯白 `#FFFFFF`，通过 20px 圆角（iOS Inset Grouped Squircle）和极微弱柔和阴影构建层次感。
2. **严苛的尺寸收敛**：
   - 常规界面只使用 12、14、16、18、20px；统计数值另用 24、30px，错误页编号可用 36px。任何可见文字不得小于 12px，空间不足时换行、滚动或调整布局，不缩小字体。
3. **触感反馈与平滑动效**：
   - 所有按钮必须具备按压缩放反馈 (`:active { transform: scale(0.98); }`)。
   - 弹窗与过渡必须遵循 Apple 弹簧曲线 `cubic-bezier(0.16, 1, 0.3, 1)`。
4. **数据稳定性与等宽保证**：
   - 时间戳、IP、端口、密钥/哈希、版本号、统计数值等动态刷新的数据，必须启用 `font-mono tabular-nums`，杜绝数字跳动与垂直错位。

---

## 二、色彩体系 (Color Tokens)

语义色定义于 `web/tailwind.config.js` 的 `colors.ios`，共享控件样式在 `web/src/styles/main.css`。**严禁在业务代码中硬编码 Hex 颜色或混用 Tailwind 默认的 slate/gray/blue/red 调色板。**

### 1. 中性文字阶梯 (Neutral Typography Colors)

| 语义 Token | Tailwind 类名 | Hex 色值 | 适用场景与语义要求 |
| :--- | :--- | :--- | :--- |
| **Primary Label** | `text-ios-label` | `#1D1D1F` | 最高对比度 (100%)。页面大标题、卡片主标题、表格主要对象名称、表单输入框文本、核心指标主数值。 |
| **Secondary** | `text-ios-secondary` | `#48484A` | 次级对比度 (~70%)。表格子行补充说明、表单字段 Label、卡片描述文本、次级导航文字。 |
| **Tertiary** | `text-ios-tertiary` | `#6E6E73` | 中等对比度 (~45%)。表格表头 (`th`)、面包屑父级、时间戳、搜索框 Placeholder、未选中的分类辅助标签。 |
| **Quaternary** | `text-ios-quaternary` | `#86868B` | 弱对比度 (~30%)。极次要元数据说明、禁用态文字、流量辅助单位 (`/ GiB`)、边框与分割线。 |

### 2. 功能语义与品牌色 (Functional Semantic Colors)

| 语义名称 | 基础色值 | Hover 交互色 | 对应 Token | 适用场景 |
| :--- | :--- | :--- | :--- | :--- |
| **System Blue** | `#0071E3` | `#0077ED` | `text-ios-blue` / `bg-ios-blue` | 主操作按钮、导航选中态、可点击跳转链接、活动选项卡 |
| **System Red** | `#FF3B30` | `#D70015` | `text-ios-red` / `bg-ios-red` | 危险操作（删除、吊销、重启）、故障拦截状态、错误提示 |
| **System Green**| `#34C759` | `#28B048` | `text-ios-green` / `bg-ios-green` | 在线状态、正常启用、健康心跳、成功发布 |
| **System Orange**| `#FF9500` | `#D97D00` | `text-ios-orange` / `bg-ios-orange`| 降级运行、维护中、草稿待发布、警告对话框 |
| **System Yellow**| `#FFCC00` | `#E0B400` | `text-ios-yellow` | 弱警告、暂缓提醒 |

### 3. 背景与结构色 (Backgrounds & Borders)

- **Canvas 整体底板**：`#F5F5F7` (`bg-ios-bg`)
- **Card 卡片容器**：`#FFFFFF` (`bg-ios-card`)
- **Border 标准边框**：`#D1D1D6` (`border-ios-border`)
- **Separator 极细分割线**：`rgba(0, 0, 0, 0.08)` (`divide-ios-separator`)
- **Backdrop 毛玻璃遮罩**：`bg-black/40 backdrop-blur-md`

---

## 三、文字排版与层级 (Typography & Hierarchy)

### 1. 字体家族 (Font Stack)
- **无衬线字体 (通用界面)**：`-apple-system, BlinkMacSystemFont, "SF Pro Text", "SF Pro Display", "Segoe UI", Roboto, "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", "微软雅黑", sans-serif`
- **等宽字体 (数据/编码)**：`"SF Mono", SFMono-Regular, ui-monospace, Menlo, Consolas, "Cascadia Mono", "Segoe UI Mono", monospace`
- 字体栈统一维护于 `web/tailwind.config.js`；全局正文通过 `font-sans` 引用，不在业务页面重复声明。使用本机系统字体，无需下载字体文件。

### 2. 标准字阶表

| 级别 | 字号 (px) | Tailwind 类名 | 默认字重 | 行高 | 典型应用组件/位置 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Page Title** | 20px | `text-xl` | `font-bold` (700) | `leading-snug` | `PageHeader.vue`、节点详情页主标题，移动端同字号 |
| **Card / Dialog Title** | 18px | `text-lg` | `font-semibold` (600) | `leading-snug` | 主要卡片标题、`FormDrawer` / `AppDialog` / `ConfirmDialog` 标题 |
| **Entity Card Name** | 18px | `text-lg` | `font-bold` (700) | `leading-snug` 或默认行高 | 服务器、节点、节点组、客户端配置卡片的主名称；各页面同类卡片保持一致 |
| **Section Title** | 16px | `text-base` | `font-semibold` (600) | `leading-normal` | 分区标题、表格首列重点对象名 |
| **Body / Control** | 14px | `text-sm` | `font-normal` (400)，按钮 500–600 | `leading-normal` | 正文、输入框、标准按钮、筛选、导航、重要说明 |
| **Caption / Badge / TH** | 12px | `text-xs` | 400–500，协议标签可用 600 | `leading-normal` | 辅助信息、表头、徽标、图表刻度、分页、行内操作 |
| **Secondary Metric** | 24px | `text-2xl` | 600–700 | 默认行高 | 次级统计数值 |
| **Primary Metric** | 30px | `text-3xl` | `font-bold` (700) | 默认行高 | 总览主要统计数值 |
| **Error Code** | 36px | `text-4xl` | `font-extrabold` (800) | 默认行高 | 仅 403 / 404 等错误页编号 |

### 3. 渲染与可读性约束

- 根字号 16px，正文默认 14px，正文行高 1.5；字号使用上表的 Tailwind 类，禁止任意字号和按 `em` 缩小辅助文字。流量单位固定为 12px，避免嵌入小号文本后继续缩小。
- 默认字距为 `normal`，不对中文正文、标签和数据施加负字距。短英文分类可保留正字距。
- 保留浏览器和操作系统的原生字体平滑，不全局强制 `antialiased`、`-webkit-font-smoothing` 或 `text-rendering: optimizeLegibility`。字体清晰度仍受系统缩放、浏览器缩放及屏幕像素密度影响，CSS 不保证消除所有锯齿。
- 侧栏恢复原有半透明毛玻璃背景；导航 14px，普通项字重 400、选中项 600，一级分类保留 `tracking-wide` 字距。
- 标题使用 1.375 行高，正文和说明采用 1.5；带文字徽标不使用 `leading-none` 压缩行高，状态徽标最小高度 24px。固定大小图标可以使用独立行高。
- 常用字重为 400 / 500 / 600 / 700；不使用细字重或文字描边来补偿小字号。不要通过长期 `transform: scale(...)` 缩小普通界面文字；代理画布的用户缩放属于画布交互。
- 新增页面优先复用公共标题、表格、表单和徽标组件；浏览器检查至少覆盖中文长名称、弹窗、紧凑标签和窄屏，确认文字不裁切。
- 分区标题显式使用 `text-base font-semibold leading-normal`，不可仅写 `font-semibold` 后从父容器继承为 12/14px。弹窗与主要区域标题使用 18px / 600；空状态及错误提示的小标题可用 14px / 600。
- 标准按钮、搜索、筛选、分段控制器及普通表单输入遵循公共样式 14px，页面不得用 `text-xs`、`!text-xs` 或响应式字号覆盖。`AppSelect` 默认触发器及选项为 14px，显式 `size="sm"` 紧凑触发器可为 12px；说明及徽标仍为 12px。配置代码、证书 PEM 等等宽多行编辑区可用 12px，普通输入保持 14px。
- 带文字刻度的延迟 SVG 图表保留至少 800px 绘图区，窄屏在图表容器内横向滚动，避免 `viewBox` 整体缩放将 12px 刻度再次缩小。

---

## 四、按钮系统规范 (Button System)

系统严格划分为**三类操作按钮**，各类高度、圆角与使用场景绝对禁止混用：

```
+--------------------------------------------------------------------------+
| 场景划分           | 核心类名                               | 高度  | 圆角 | 适用环境      |
+--------------------+---------------------------------------+-------+------+---------------+
| 1. 标准操作按钮    | .ios-btn-primary / secondary / danger  | 36px  | 12px | 页头、卡片标题 |
| 2. 模态弹窗按钮    | .ios-dialog-btn-primary / sec / danger| 36px  | 12px | 弹窗底部操作条 |
| 3. 表格行内按钮    | .ios-action-btn-primary / sec / danger| 28px  | 8px  | 仅限于表格 td  |
+--------------------------------------------------------------------------+
```

### 1. 标准操作按钮 (Standard Action Buttons)
- **定义**：全站通用 36px 高度、12px 圆角、弹性伸缩按钮。
- **主按钮 (`.ios-btn-primary`)**：高对比蓝色实心底 `#0071E3`，白色文本，用于“新建”、“保存”、“执行检查”等核心主操作。
- **次级按钮 (`.ios-btn-secondary`)**：纯白底灰边 `#D1D1D6`，深灰文本 `#1D1D1F`，用于“刷新”、“返回列表”、“节点设置”等次级操作。
- **危险按钮 (`.ios-btn-danger`)**：轻量浅红半透明背景 `rgba(255, 59, 48, 0.08)`，红色边框与文本，用于“吊销证书”、“重启当前核心”等敏感卡片操作。
- **尺寸与属性**：
  ```css
  height: 2.25rem;       /* 36px */
  min-width: 4.5rem;     /* 72px 基础防坍缩，防止两个汉字缩为圆形 */
  padding: 0 1.25rem;    /* 文本自适应缩放 */
  border-radius: 0.75rem;/* 12px rounded-xl */
  font-size: 0.875rem;   /* 14px */
  ```
- **状态行为**：
  - Hover: 轻微提亮与增加投影。
  - Active: 按压缩小 `transform: scale(0.98)`。
  - Disabled: `opacity: 0.4; cursor: not-allowed; pointer-events: none;`。

### 2. 模态弹窗按钮 (Dialog Action Buttons)
- **定义**：专用于 `ConfirmDialog.vue`、`FormDrawer.vue`、`AppDialog.vue` 弹窗底部操作区。
- **类名**：`ios-dialog-btn-primary`, `ios-dialog-btn-secondary`, `ios-dialog-btn-danger`, `ios-dialog-btn-warning`。
- **尺寸**：高度 36px，圆角 12px，最小宽度 4.5~5rem，完全对称平衡。

### 3. 表格行内按钮 (Table Row Action Buttons)
- **定义**：**严格仅用于 `DataTable` 的 `<td>` 单元格中**，例如“编辑”、“复制”、“删除”。
- **类名**：`ios-action-btn-primary`, `ios-action-btn-secondary`, `ios-action-btn-danger`。
- **尺寸**：高度 **28px** (`1.75rem`)，圆角 **8px** (`0.5rem`)，字号 12px (`0.75rem`)，左右内边距 `0 0.65rem`。
- **禁忌**：**绝对禁止在页面头部、卡片面板右上角使用 `ios-action-btn-*`**（否则会产生高低错落、参差不齐的严重视觉缺陷）。

### 4. 辅助控件
- **搜索/过滤触发按钮 (`.ios-search-btn`)**：高度 36px，圆角 12px，与搜索输入框等高严丝合缝。
- **分段控制器 (`.ios-segmented-container` + `.ios-segmented-item`)**：胶囊底座背景 `#E5E5EA`，圆角 9999px，选中项纯白浮起并投射软阴影。

---

## 五、表单与控件规范 (Form Controls)

### 1. 文本输入框 (`.form-input-base`)
- **外观尺寸**：字号 14px (`0.875rem`)，圆角 12px (`0.75rem`)，内边距 `0.55rem 0.85rem`。
- **聚焦态**：高亮苹果蓝边框，外加光晕外发光：
  ```css
  border-color: #0071E3;
  box-shadow: 0 0 0 3.5px rgba(0, 113, 227, 0.2);
  ```
- **占位符**：`placeholder: text-ios-quaternary` (`#86868B`)。

### 2. 下拉选择器 (`select.form-input-base` / `.ios-filter-select`)
- **外观**：隐藏浏览器原生简陋箭头 (`appearance: none`)。
- **内置图标**：通过背景 SVG 嵌入 Apple 风格精致灰色 Chevron 箭头：
  ```css
  background-image: url("data:image/svg+xml,...");
  background-position: right 0.85rem center;
  background-size: 1.15rem 1.15rem;
  padding-right: 2.25rem; /* 防止选项文字与右侧箭头重叠 */
  ```

### 3. 复选框与单选框 (`input[type="checkbox"]`, `input[type="radio"]`)
- **主题色**：`accent-color: #0071E3`。
- **触控体验**：外层 Label 强制具有手型光标 `label { cursor: pointer; }`。

### 4. 字段标签 (Field Labels)
- 统一样式：`text-xs font-medium text-ios-secondary mb-1.5`。
- 严禁对表单 Label 使用比输入框更粗更黑的样式，防止干扰输入视线。

---

## 六、模态框、抽屉与动效 (Modals, Drawers & Transitions)

### 1. 弹出面板容器与圆角
- **确认弹窗 (`ConfirmDialog`)**：居中，最大宽度 448px (`max-w-md`)，圆角 **24px** (`rounded-3xl`)，柔和边框与深层投影。
- **表单弹窗 (`FormDrawer`)**：**居中模态**（`fixed inset-0` + `flex items-center justify-center`），圆角 **24px** (`rounded-3xl`)，宽度按 `width` 属性取 `max-w-md`（默认，448px）/ `max-w-lg` / `max-w-xl` / `max-w-2xl`，最高 `85vh`，内容区独立滚动；头部与底部操作条都是 `#F5F5F7` 背景。组件名沿用旧称，**它不再是右侧抽屉**。
- **背景蒙层**：统一采用 `bg-black/40 backdrop-blur-md` 毛玻璃模糊效果（`FormDrawer` 为 `bg-black/45`）。

### 2. 动效曲线规范
- 统一使用 Apple 标志性的弹簧减速曲线：
  ```css
  transition: all 0.24s cubic-bezier(0.16, 1, 0.3, 1);
  ```
- 弹窗面板进出场动效 (`.ios-modal-panel`)：
  - 进场：从 `scale(0.95) translateY(8px)` 平滑过渡到 `scale(1) translateY(0)`，伴随透明度由 0 至 1。
  - 退场：平滑渐隐并微量下移。

---

## 七、侧边栏与导航系统 (Navigation & Sidebar)

### 1. 展开态 (Expanded State)
- **宽度**：`17rem` (272px)，`web/src/layouts/MainLayout.vue` 使用 `w-[17rem]` 固定展开宽度。Tailwind 3.4 默认间距表没有 `68`，因此不能用 `w-68`。
- **分组标题**：`text-xs font-semibold tracking-wider uppercase text-ios-tertiary px-3.5 pt-4 pb-1.5`。
- **一级折叠菜单项**：14px，普通字重 400、选中项 600，分类标题保留 `tracking-wide`；展开/激活通过 `text-ios-blue bg-ios-blue/10` 等颜色与背景区分，右侧 Chevron 箭头平滑旋转 90 度。
- **二级子路由项**：缩进 `pl-9`，字号使用 `text-sm`（14px），通过缩进表达树形层级。

### 2. 折叠态 (Collapsed State)
- **宽度**：`w-20` (80px)。
- **视觉**：仅展示居中矢量图标；悬浮时在右侧弹出具有毛玻璃与微投影的提示气泡（Tooltip），保持极简状态下的可操作性。
- **平滑过渡**：宽度与透明度均附加 `transition-all duration-300 cubic-bezier(0.16, 1, 0.3, 1)`。

---

## 八、表格与数据呈现 (DataTable & Data Display)

### 1. 表格骨架与表头 (`thead th`)
- **关键规则**：表头必须退居次席，避免粗黑抢镜。
- **样式规范**：共享 `DataTable.vue` 的 `thead` 为 `text-xs font-medium text-ios-tertiary tracking-wider uppercase bg-[#F5F5F7] border-b border-black/[0.08]`。

### 2. 数据单元格与首列重点
- **首列重点实体**：`text-base font-semibold text-ios-label`（若可点击则为 `text-ios-blue hover:underline`），副说明文字为 `text-sm text-ios-secondary mt-0.5`。
- **常规数据列**：`text-sm text-ios-label`。
- **数值与指标列**：`text-xs font-mono tabular-nums text-ios-secondary`，保证上下垂直严格对齐。

### 3. 微型状态与度量组件
- **状态徽章 (`StatusBadge.vue`)**：
  - 规格：`text-xs font-medium leading-normal px-2.5 min-h-[24px] rounded-full inline-flex items-center gap-1.5`。
  - 内置圆点：`w-1.5 h-1.5 rounded-full shrink-0`。
  - 配色方案：浅色半透明背景 + 对应语义色文字与边框（如绿色 `bg-ios-green/10 text-ios-green border border-ios-green/20`）。
- **流量数值展示 (`TrafficValue.vue`)**：
  - 数字部分：`font-mono tabular-nums font-semibold text-ios-label`。
  - 单位部分：`text-xs font-normal text-ios-tertiary ml-0.5`（如 `MiB`、`GiB` 弱化后退）。

---

## 九、开发自查规范与反模式 (Checklist & Anti-Patterns)

为保障设计系统长期一致性，日常组件开发与代码审查（Code Review）必须对照以下检查项：

| 检查项 | 正确做法 (Do) | 严重反模式 (Don't) |
| :--- | :--- | :--- |
| **页面/卡片按钮** | 使用 `ios-btn-primary / secondary / danger` (36px/12px) | ❌ 在卡片顶部混用 `ios-action-btn-*` (28px) 导致高低不齐 |
| **表格行内操作** | 使用 `ios-action-btn-*` (28px/8px) | ❌ 在表格 `<td>` 中放入 36px 大按钮破坏行高 |
| **危险按钮禁用态** | 保证 `:disabled` 时具有 `opacity: 0.4` 与 `pointer-events: none` | ❌ 禁用时依然保持鲜红且可被 hover 触发变色 |
| **下拉选择器** | 统一采用 `select.form-input-base`，保留 `padding-right: 2.25rem` | ❌ 使用浏览器原生未美化 select，导致箭头紧贴右侧或遮挡文字 |
| **动态数值展示** | 所有数字、IP、时间戳、UUID 添加 `tabular-nums` | ❌ 使用非等宽普通字体导致数值跳动与列表对齐错乱 |
| **色彩与字阶** | 使用语义颜色与标准字阶，最小 12px，正文 14px | ❌ 9～11px 小字、任意字号、负字距、按比例缩小单位文字 |
| **弹窗与交互动效**| 统一引入 Apple 弹簧曲线 `cubic-bezier(0.16, 1, 0.3, 1)` | ❌ 缺少按压反馈 (`active:scale(0.98)`) 或生硬无动画闪现 |
