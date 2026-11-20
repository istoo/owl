# Owl 对标同类工具：差距与改造方案（2026-11-20）

## 依据

- 竞品：cc-switch（15.6 万星）、Claude Code Router、CLIProxyAPI 及 EasyCLI、Quotio、claude-relay-service、new-api、LiteLLM、ccusage、zcf、AIClient2API。数据取自 2026-11-20 的 GitHub README、Release 和 issue。
- Owl 现状：`docs/user-guide.zh-CN.md`（v0.2.27），用空 HOME 跑 `owl web` 截的首屏，以及运行中的 v0.2.29 的 `owl heap`。

## Owl 已经领先、不用补的

| 方面 | Owl | 最强竞品 |
| --- | --- | --- |
| 支持的 Agent | 约 40 个 | cc-switch 12 个 |
| 路由 | 8 种策略、路由组可嵌套、意图路由、会话粘性 | CCR：条件路由加有序 fallback |
| 写入安全 | 保留格式只改目标键（`internal/edit`），原子写，接入和断开有预览（`agent/preview.go`），关掉接入时恢复原配置 | cc-switch / zcf 同级 |
| 深链导入 | `owl://import?…`，并能转换其他格式（`provider/importurl.go`） | `ccswitch://` |
| 隐私 | 本地脱敏：规则加本地模型，命中后可改走受信模型 | 无竞品有 |
| 资源库 | MCP、技能、Hooks、子 Agent、MCP 路由 | cc-switch 有 MCP、Prompt、Skills |
| 共享 | 网关密钥带限额，远程 Owl，WebDAV/S3/GitHub 同步 | new-api / LiteLLM 是服务端同类 |

结论：功能数量上 Owl 已经领先。它输在「轻、看得懂、快捷」，即 cc-switch 的护城河：前 5 分钟上手、托盘一键切换、一眼看出现在用的是谁。

## 差距（已核实），按用户感知排序

### P0-1 资源占用（架构）
- 实测：v0.2.29 窗口版启动 6 分钟，footprint 410 MB，Go 活跃堆 200 MB，安装包 82 MB。同类 Tauri 应用通常在几十 MB（未实测，本机没装竞品）。
- 堆的大头：用量账本 `usage.(*rowChunk).encode` 常驻 49 MB；脱敏模型 35 MB（本机开了模型脱敏，属合理占用）；`sessions.uuidSet` 13 MB；保留的请求体 14 MB。
- 目标：用户开启同样功能时，空闲 footprint 不超过 200 MB。冷的用量块落盘、按需读；会话索引改为紧凑结构。
- 验收：`owl heap` 前后对比，加同一套长时间压测（`internal/gateway` 现有的内存测试）。

### P0-2 托盘一键切换
- cc-switch、Quotio、EasyCLI 的共同做法：在托盘里每个 Agent 一行，显示当前供应商、模型和额度，在子菜单里直接切换。
- Owl 现状：托盘面板的 Agent 行只能点开窗口，托盘里只能切「方案」。
- 目标：托盘面板的 Agent 行可以直接换模型（下拉选模型或路由组），换完显示生效方式（新会话生效，或需要重启）。

### P0-3 前 5 分钟
- 首屏实测：没有 key 时，5 张 Agent 卡片都写着同一句「Add a provider」，加上引导里的一个，共 6 个相同按钮。
- 目标：
  1. 没有模型来源时，Agent 卡片只显示状态，主操作只在引导里出现一次。
  2. 发现本机 Agent 已在用的 key 或中转（如 Claude Code 的 `ANTHROPIC_BASE_URL`）时，首屏直接提示「导入为供应商」。cc-switch 会把它导入为 default。
  3. 首次启动的界面语言跟随系统。
- 验收：空 HOME 走一遍「下载 → 第一次请求成功」，记点击次数和截图。

### P1-1 一眼看出「现在用的是谁」
- 每个已接入的 Agent 显示：当前模型、实际命中的供应商和账号（取最近一次请求）、今日花费、额度余量。
- CCR 的做法是在日志里给出最终选中的供应商、凭据、延迟和费用；Owl 的路由页已有这些数据，缺的是在首屏露出。

### P1-2 供应商健康一键检查
- 一个按钮测全部启用供应商的连通性、首字延迟和鉴权，结果标在供应商列表上。熔断或冷却中的账号显示恢复时间。

### P1-3 额度按节奏着色
- 来自 Quotio #577：额度条按「消耗速度 / 剩余时间」着色，而不是只看剩余百分比。
- 只在现有颜色变量里取色，不新增配色。

### P2
- 用量账本支持更长周期，同时不涨内存（依赖 P0-1）。
- 信息架构：侧栏有 13 个入口。cc-switch 4.0 按模块改版招来反弹（#8038），所以只在用户同意后做，并先出真界面并排预览。

## 约束

- 配色、文字颜色和字体保持原版。纯审美改动先出预览让用户挑。
- 实现交给独立 worktree 里的子 agent；主会话负责拆分、验收和发版。
- 重型测试走 `scripts/gui-suite.sh`，一次只跑一套，全量交给 CI。
- 每个提交带 `Notes-zh:`。

## 进度（用户 2026-11-20 选定：版式不动，只加入口）

- [~] P0-1 资源占用：子 agent A（worktree）进行中
- [x] P0-2 托盘一键切换 + P0-3 前 5 分钟：B 分支 worktree-agent-a2c844c0eeb906eb7（678c34504），待合入
- [ ] P1-1 首屏露出「现在用的是谁」（在 B 合入后做，避免同改 app.js）
- [ ] P1-2 供应商健康一键检查
- [ ] P1-3 额度按节奏着色
- [ ] 验收、CI、发版
