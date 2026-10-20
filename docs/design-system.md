# qui 前端设计系统与视觉交互规范 (Design System Specification)

> **版本**：v1.2 (Apple iOS / HIG Modern Aesthetic)  
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
   - 杜绝任意随意出现的 `11px`、`13px`、`15px` 混杂，所有字阶与控件严格归一到规范比例尺中。
3. **触感反馈与平滑动效**：
   - 所有按钮必须具备按压缩放反馈 (`:active { transform: scale(0.98); }`)。
   - 弹窗与过渡必须遵循 Apple 弹簧曲线 `cubic-bezier(0.16, 1, 0.3, 1)`。
4. **数据稳定性与等宽保证**：
   - 时间戳、IP、端口、密钥/哈希、版本号、统计数值等动态刷新的数据，必须启用 `font-mono tabular-nums`，杜绝数字跳动与垂直错位。

---

## 二、色彩体系 (Color Tokens)

系统全面集成于 `web/tailwind.config.js` 的 `colors.ios` 配置中。**严禁在业务代码中硬编码 Hex 颜色或混用 Tailwind 默认的 slate/gray/blue/red 调色板。**

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
- **等宽字体 (数据/编码)**：`"SF Mono", SFMono-Regular, ui-monospace, Menlo, Monaco, Consolas, "Cascadia Mono", monospace`

### 2. 标准 7 级字阶表 (The 7-Tier Scale)

| 级别 | 字号 (px) | Tailwind 类名 | 默认字重 | 行高 | 典型应用组件/位置 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Page Title** | 24px (桌面 28px) | `text-2xl sm:text-[28px]` | `font-bold` (700) | `leading-tight` | `PageHeader.vue` 页面主标题 |
| **Card Title** | 17px (桌面 18px) | `text-[17px] sm:text-lg` | `font-semibold` (600) | `leading-snug` | 卡片区域大标题、`FormDrawer`/`ConfirmDialog` 模态标题 |
| **Section Title** | 15px | `text-[15px]` | `font-semibold` (600) | `leading-normal`| 数据表格首列重点对象名、分组小标题、侧栏活动导航 |
| **Body (正文)** | 14px | `text-sm` | `font-normal` (400) | `leading-normal`| 表格正文列、输入框文字、卡片说明段落、36px 按钮 |
| **Subhead** | 13px | `text-[13px]` | `font-normal` (400) | `leading-normal`| 表格对象首列下方的第二行说明（如别名、公网 IP、创建人） |
| **Caption / TH** | 12px | `text-xs` | 表头用 `font-medium`<br>时间用 `font-normal` | `leading-normal`| **`DataTable` 表头 (`th`)**、时间戳、表单底部说明、分页条 |
| **Micro / Badge** | 11px | `text-[11px]` | `font-semibold` (600) | `leading-none` | `StatusBadge.vue`、`ProtocolBadge.vue`、侧栏分类大写标 |

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
  font-size: 0.8125rem;  /* 13px */
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
- **侧边抽屉 (`FormDrawer`)**：右侧滑入，固定宽 `28rem` (448px) 或 `32rem` (512px)，平滑弹性进出。
- **背景蒙层**：统一采用 `bg-black/40 backdrop-blur-md` 毛玻璃模糊效果。

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
- **宽度**：`16rem` (256px)。
- **分组标题**：`text-[11px] font-semibold tracking-wider uppercase text-ios-tertiary px-3.5 pt-4 pb-1.5`。
- **一级折叠菜单项**：未激活为常规灰色文本；展开/激活时为 `text-ios-blue bg-ios-blue/10 font-semibold`，右侧 Chevron 箭头平滑旋转 90 度。
- **二级子路由项**：缩进 `pl-9`，字号收窄为 `text-[13px]`，形成明晰树形梯级。

### 2. 折叠态 (Collapsed State)
- **宽度**：`4.5rem` (72px)。
- **视觉**：仅展示居中矢量图标；悬浮时在右侧弹出具有毛玻璃与微投影的提示气泡（Tooltip），保持极简状态下的可操作性。
- **平滑过渡**：宽度与透明度均附加 `transition-all duration-300 cubic-bezier(0.16, 1, 0.3, 1)`。

---

## 八、表格与数据呈现 (DataTable & Data Display)

### 1. 表格骨架与表头 (`thead th`)
- **关键规则**：表头必须退居次席，避免粗黑抢镜。
- **样式规范**：`text-xs font-medium text-ios-tertiary tracking-wider uppercase px-4 py-3 bg-black/[0.015] border-b border-black/[0.06]`。

### 2. 数据单元格与首列重点
- **首列重点实体**：`text-[15px] font-semibold text-ios-label`（若可点击则为 `text-ios-blue hover:underline`），副说明文字为 `text-[13px] text-ios-secondary mt-0.5`。
- **常规数据列**：`text-sm text-ios-label`。
- **数值与指标列**：`text-xs font-mono tabular-nums text-ios-secondary`，保证上下垂直严格对齐。

### 3. 微型状态与度量组件
- **状态徽章 (`StatusBadge.vue`)**：
  - 规格：`text-[11px] font-semibold px-2.5 py-0.5 rounded-full inline-flex items-center gap-1.5`。
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
| **色彩与字阶** | 严格使用 `text-ios-*` 语义 Token 与 7 级标准字阶 | ❌ 随意写入 `text-slate-600`、`#007AFF`、`text-[15px]` 等非标样式 |
| **弹窗与交互动效**| 统一引入 Apple 弹簧曲线 `cubic-bezier(0.16, 1, 0.3, 1)` | ❌ 缺少按压反馈 (`active:scale(0.98)`) 或生硬无动画闪现 |
