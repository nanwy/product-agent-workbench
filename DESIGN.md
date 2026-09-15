---
name: Product Agent Workbench
description: 安静、可信、以真实预览为中心的规格驱动开发工作台
colors:
  canvas: "oklch(0.935 0.004 255)"
  rail: "oklch(0.965 0.003 255)"
  surface: "oklch(0.985 0.002 255)"
  surface-raised: "oklch(0.998 0.002 255)"
  surface-muted: "oklch(0.955 0.004 255)"
  ink: "oklch(0.205 0.012 255)"
  ink-soft: "oklch(0.39 0.012 255)"
  muted: "oklch(0.55 0.01 255)"
  line: "oklch(0.875 0.006 255)"
  accent: "oklch(0.57 0.205 258)"
  accent-hover: "oklch(0.51 0.205 258)"
  accent-soft: "oklch(0.94 0.035 258)"
  success: "oklch(0.56 0.135 150)"
  danger: "oklch(0.56 0.19 28)"
typography:
  title:
    fontFamily: "-apple-system, BlinkMacSystemFont, Segoe UI, PingFang SC, sans-serif"
    fontSize: "16px"
    fontWeight: 680
    lineHeight: 1.25
  body:
    fontFamily: "-apple-system, BlinkMacSystemFont, Segoe UI, PingFang SC, sans-serif"
    fontSize: "11px"
    fontWeight: 400
    lineHeight: 1.5
  label:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace"
    fontSize: "9px"
    fontWeight: 650
    lineHeight: 1.2
    letterSpacing: "0.08em"
rounded:
  sm: "6px"
  md: "9px"
  pill: "999px"
spacing:
  xs: "5px"
  sm: "8px"
  md: "10px"
  lg: "14px"
components:
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.surface-raised}"
    rounded: "{rounded.sm}"
    padding: "7px 11px"
    height: "32px"
  button-primary-hover:
    backgroundColor: "{colors.accent-hover}"
    textColor: "{colors.surface-raised}"
    rounded: "{rounded.sm}"
    padding: "7px 11px"
  input:
    backgroundColor: "{colors.surface-raised}"
    textColor: "{colors.ink}"
    rounded: "{rounded.sm}"
    padding: "9px 10px"
  panel-header:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    padding: "7px 12px"
    height: "46px"
---

# Design System: Product Agent Workbench

## 1. Overview

**Creative North Star: "The Quiet Workbench"**

界面借鉴 Praxis 的桌面工具空间组织：窄导航栏提供持续上下文，中间检查区承载一条连续 Agent 对话，真实预览占据剩余画布。视觉语言保持克制，让产品经理和前端工程师在高密度流程中仍能快速看到当前阶段、唯一主行动和可核查结果。

系统拒绝当前 MVP 式的三列等权卡片墙、过大的宣传标题、重复边框和相同视觉重量。结构优先于装饰，状态变化优先于品牌表演。

**Key Characteristics:**

- 52px 紧凑上下文栏，252px 工作流导航，392px 检查区，弹性预览画布。
- 冷灰中性色承载大部分面积，克制蓝色只用于当前状态与主行动。
- 内容区独立滚动，运行事实渐进披露，预览始终保有稳定尺寸。
- 平板重排为工作流加预览，移动端让预览先出现。

## 2. Colors

冷灰表面形成安静的工具背景，蓝色提供明确操作焦点，绿色、琥珀色和红色只表达语义状态。

### Primary

- **Workbench Blue**：只用于主按钮、当前阶段和键盘焦点，稀缺性使其保持明确。

### Secondary

- **Verified Green**：只表示健康、完成和验证通过。
- **Boundary Amber**：只表示需要确认的工程边界。
- **Blocking Red**：只表示失败、停止和阻塞。

### Neutral

- **Cool Canvas**：预览周围和窗体外层的背景。
- **Quiet Rail**：工作流导航的轻微分区色。
- **Raised Paper**：输入、按钮和预览容器的最高亮度表面。
- **Graphite Ink**：主要文字；柔和墨色与静音文字承载次要信息。

### Named Rules

**The One Active Signal Rule.** 蓝色只指向当前状态或当前主行动，同一区域禁止出现多个等权蓝色信号。

## 3. Typography

**Display Font:** 系统无衬线字体
**Body Font:** 系统无衬线字体
**Label/Mono Font:** SFMono Regular、Menlo 或系统等宽字体

**Character:** 使用操作系统原生字形获得成熟桌面工具的熟悉感。等宽字体只用于 ID、路径、状态提示和短标签，不用于长段正文。

### Hierarchy

- **Title**（680，16px，1.25）：需求名称等局部主标题。
- **Panel Heading**（680，13px）：固定面板标题。
- **Body**（400，11px，1.5）：说明、证据和表单辅助文字。
- **Label**（650，9px，0.08em）：英文分区标签、ID 和状态。

### Named Rules

**The Tool Scale Rule.** 页面不使用营销型大标题，层级通过面板位置、留白和字重建立。

## 4. Elevation

系统默认扁平，主要依靠相邻表面的轻微明度差和 1px 分隔线建立层级。阴影只用于悬浮错误提示和预览画布的轻微物理边界。

### Shadow Vocabulary

- **Floating Alert**（`0 12px 32px oklch(0.2 0.02 255 / 0.12)`）：只用于覆盖内容的错误提示。
- **Preview Lift**（`0 2px 8px oklch(0.2 0.02 255 / 0.06)`）：只用于真实页面预览容器。

### Named Rules

**The Flat by Default Rule.** 静止面板没有阴影；若每个区域都像浮层，层级已经失败。

## 5. Components

### Buttons

- **Shape:** 紧凑圆角矩形（6px），最小高度 32px。
- **Primary:** 工作台蓝底、最高亮度文字，内边距 7px 11px。
- **Hover / Focus:** hover 加深蓝色；focus 使用 2px 蓝色轮廓和 2px 间距。
- **Secondary / Risk:** 次级操作使用升高表面与灰色描边；风险操作使用浅红表面和语义文字。

### Chips

- **Style:** 胶囊形只用于短状态或项目身份，禁止承载主要操作。
- **State:** 当前阶段使用浅蓝背景和深蓝文字；运行健康状态同时使用圆点和文字。

### Cards / Containers

- **Corner Style:** 小容器 6px，预览和重要结果 9px。
- **Background:** 默认表面保持平坦，只有可交互或独立事实块使用升高表面。
- **Shadow Strategy:** 遵循扁平默认规则。
- **Border:** 1px 冷灰分隔线，不使用彩色侧边条。
- **Internal Padding:** 紧凑组件 8px，连续工作内容 14px。

### Inputs / Fields

- **Style:** 最高亮度表面、1px 强分隔线、6px 圆角。
- **Focus:** 蓝色边框配 3px 浅蓝焦点环。
- **Error / Disabled:** 错误使用完整浅红背景和描边；禁用通过透明度与不可用光标共同表达。

### Navigation

左侧 SDD 导航使用连续列表。当前阶段获得浅蓝整行背景与实心编号，已完成阶段使用绿色编号，未来阶段保持静音。

### Agent Conversation

- 对话顶部使用紧凑的 `Grilling → Spec → Tickets → Implement` 状态提示，阶段详情只进入消息时间线。
- Agent 与用户消息左右错位，系统门禁居中；工具和文件活动使用可折叠记录，不伪装成聊天气泡。
- composer 固定在对话底部，时间线独立滚动。用户离开底部后停止强制跟随，并提供“回到底部”。
- Markdown 只重绘当前流式消息；未闭合代码围栏暂按纯文本显示，完成后再进入完整解析。

### Preview Canvas

预览是主工作表面。工具栏固定在预览上方，iframe 占满剩余高度；运行事实放在底部 disclosure 中，默认折叠。

## 6. Do's and Don'ts

### Do:

- **Do** 保证当前阶段和唯一主行动在首屏可见。
- **Do** 将大部分可用宽度留给真实预览，并让导航与检查区独立滚动。
- **Do** 使用 150ms 到 200ms 的状态反馈，并尊重 reduced motion。
- **Do** 让文字、位置和形状共同表达状态，颜色只作为补充。

### Don't:

- **Don't** 回到“三列等权卡片墙、过大的宣传标题、重复边框和相同视觉重量”。
- **Don't** 使用通用 SaaS 仪表盘、装饰性渐变、玻璃拟态或无意义动画。
- **Don't** 使用大于 1px 的彩色单侧边框作为状态装饰。
- **Don't** 让产品经理必须理解内部技术术语才能找到下一步。
