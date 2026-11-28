# Owl 全功能深度 Bug 审计报告（2026-11-27，基于 `e29029645` / v0.2.40）

本次审计将 owl 全部功能划分为 9 大单元（10 个子任务），覆盖网关安全/路由/协议转换/桥接、供应商与目录、Agent 接线与配置编辑、资源库/MCP/浏览器扩展、会话/记忆/用量/预算、隐私脱敏与插件运行时、同步/备份/更新/OS 集成、GUI/CLI/TUI 全栈。

以下是基于 `e29029645` 的静态审查清单，不是全部已复现的缺陷。编号不等于独立缺陷数：High 实列 H1–H53，若干 Medium 和 Low 编号下合并了多个不同问题。修复、按设计不改及尚未验证的结论，以 `TASKS.md` 的逐批验收记录为准。

---

## 一、Critical（10 项）

| # | 所属单元 | 位置 | 缺陷概要 | 触发条件与后果 |
|---|---|---|---|---|
| C1 | Unit 1 网关安全 | `internal/gateway/gateway.go:958`, `peeruser.go:24-45`, `lan.go:964-971` | **本机多用户环回隔离完全失效**：生产 `http.Server` 未挂载 `ConnContext: peerUserContext`，而 `sameOSUser` 为 fail-open（`!known \|\| same`） | 共享主机上同机其他 OS 用户直连 `127.0.0.1:3425` 无需任何 key 即获得机主全权访问（消费额度、签发 key、读写记忆/会话、控浏览器、调 `/v1/owl/restart`） |
| C2 | Unit 8 自动更新 | `internal/update/fallback.go:71-77,128-130,239-245`, `signed.go:41` | **镜像可劫持未签名更新清单实现 RCE**：`signingOn = false` 下，GitHub 回退流程通过不受信任的用户镜像拉取无签名 `latest.json` 并信任其资产 URL 与 SHA-256 | 配置 `UpdateMirror` 且主 feed 失败或选 GitHub 源时，恶意镜像伪造 `latest.json` 与哈希，在 Linux/Windows 上无提示覆盖二进制执行任意代码 |
| C3 | Unit 8 自动更新 | `internal/update/update.go:1672-1687,426-435` | **`zipSafe` 漏检 `/..` 尾段、`\evil` 与 Zip Symlink**，可在 `codesign` 之前通过 `ditto` 实现 Zip Slip | 恶意/被劫持 `.zip` 含 symlink 条目（指向目录外）加后续文件条目，`zipSafe` 放行，`ditto -x -k` 在 `sameSigner` 前写任意文件 |
| C4 | Unit 8 自动更新 | `internal/update/update.go:672-681,597,1636` | **`openCommand` 用 Go `%q` 拼进 `/bin/sh -c`**（而非单引号 `shellQuote`） | macOS 上 `.app` 路径含 `$(...)`/`$VAR`/反引号时，更新后重启（`Relaunch`/`OpenBeside`）由 `/bin/sh` 展开执行任意命令 |
| C5 | Unit 9 GUI 前端 | `internal/gui/assets/app.js:138`, `plugins.js:1747-1758`, `library/client.js:6334,6446-6451` | **Web 端外链短路跳过协议校验导致 `javascript:` XSS**：`web && path === "open"` 直接 `window.open(body.url)`，且市场 `homepage`/`repository`/`websiteUrl` 未校验 `^https?://` | 恶意 npm 插件或 MCP Registry 在 homepage 填 `javascript:...`，`owl web` 点击（或中键点击 `a.href`）即在同源执行 JS，窃取所有 Provider Key 或经 MCP 接口 RCE |
| C6 | Unit 9 GUI 插件页 | `internal/gui/pageservice.go:101-110,120-168`, `pluginpage.go:364-372`, `web.go:245-248` | **插件页服务代理允许任意本机端口 SSRF**：`servicePort` 仅排除 `gateway.Port()`，且插件脚本可通过 `owl.options.set()` 动态改 `s.Option` 指向任意 `1..65535` 端口 | 社区插件在 `owl web` 沙箱内通过 `owl.options.set()` 切换端口号，请求无 cookie 校验的 `/_owl/plugin/<tok>/_owl/service/...`，将宿主机变成完整 `127.0.0.1` HTTP/WS 代理（绕过 `connect-src 'none'`） |
| C7 | Unit 3 供应商 | `internal/provider/plugins.go:774-781`, `provider.go:2613-2622` | **`pluginFetch` 同源校验不校协议（HTTPS→HTTP 降级）**且 `HostOf` 为粗糙字符串截取 | `base` 为 `https://api.vendor.com` 而 `rest` 为 `http://api.vendor.com/...` 时通过 `HostOf` 校验，插件宿主携带账号 OAuth Token/Cookie 明文 HTTP 发出 |
| C8 | Unit 4 Agent 接线 | `internal/agent/wsl.go:1168,1205-1231`, `applied.go:957-976` | **休眠 WSL Agent `Disconnect()` 清空刚恢复的用户原配置**：`asleep()` 的 `Get` 永远读休眠前缓存而非已唤醒的 `warm` | WSL 停止状态下断连，`Unwire()` 写回用户原配置后，`Values()` 读出旧 owl 缓存，`Disconnect` 误判仍为 owl 值调用 `Set("")` 把用户原配置彻底清空 |
| C9 | Unit 4 Agent 接线 | `internal/edit/file.go:104-122,146-148`, `agent/keptaway.go:30-32,129-217` | **全局写过滤器 `edit.Filtered` 无 goroutine 隔离**：后台 `SyncCatalogSoon()` 会静默拦截并回滚并发的用户显式修改 | Agent 原指向远端/代理时，用户在 `SyncCatalog` 期间并发执行 `Apply`/`Pick` 切回本机网关，写入被全局 `filter` 静默改回旧远端地址与 key 且返回成功 |
| C10 | Unit 4 WSL | `internal/wslrun/wslrun.go:72-120,261-271` | **`wslrun.Find` 漏 `plainName` 校验直接无引号拼入 `sh -lc` / `bash -ic`** | 传入含 `'`, `$(...)`, `;` 的 `name` 到 `wslrun.Find()` 时在运行中的 WSL 发行版内任意命令执行 |

---

## 二、High（53 个审计编号）

### 网关安全、协议与桥接（Unit 1 & Unit 2）
| # | 位置 | 缺陷概要 | 触发条件与后果 |
|---|---|---|---|
| H1 | `internal/gateway/image_fetch_guard.go:231-243,269-277` | **Fake-IP 启发式配合本地代理绕过 SSRF DNS Pinning** | 宿主机有本机 HTTP/SOCKS 代理时，攻击者 DNS 首次返回 `198.18.0.1` 触发 `byName=true` 放弃 IP pinning，交由本机代理重新解析至 `127.0.0.1`/`169.254.169.254` |
| H2 | `internal/gateway/responses_ws.go:309-319,459-497` | **Responses WS 无界历史累积（`generate:false`）与无界 `queued` 队列** | 任意低权限 key 单连接循环链式 `previous_response_id` + `generate:false` 大帧，或慢回合时猛灌帧，数秒打爆堆内存 OOM |
| H3 | `internal/gateway/draw.go:712-746`, `video.go:1085-1122` | **`/v1/images/*` 与 `/v1/videos` 省略 `model` 跳过 Key 白名单与按模型预算** | 受限 key 发 `{"prompt":"..."}`（不带 `model`），`named==false` 跳过 `groupGiven`/`modelAllowed` 与 scoped 预算检查，非授权消费默认图/视频模型 |
| H4 | `internal/gateway/responses.go:1919-1964` | **Responses 流式解码器在交错 `function_call` 时过早 `endCall` 串拼参数** | 上游在调用 1 未 `done` 前发调用 2 的 `output_item.added`，`endCall` 清空状态导致调用 1 后续 delta 拼接到调用 2，双工具参数全毁 |
| H5 | `internal/gateway/mcp_key.go:366-378` | **`mcpToolsFilter.finish` 在 SSE 尾事件缺空行时绕过工具白名单过滤** | 受限 MCP key 访问返回 SSE 且尾事件无空行的 `tools/list`，`finish()` 原样写回 `f.event`/`f.buf`，泄露禁用的工具名与 schema |
| H6 | `internal/gateway/gateway.go:7836-7850,7954-8122` | **透传流收尾 `flush` 跳过 `enumTidy` 且 `spaces`/`written`/`search` 逆序断裂** | 非 Codex Responses 客户端收尾收到未过 `enumTidy` 的非标准事件解析崩溃；非流式含命名空间工具跳过 `written` 文本调用提取 |
| H7 | `internal/gateway/responses.go:2311-2326,2445-2471` | **`responsesEncoder` 为 `tool_search_call` 发送孤立 `function_call_arguments.delta`** | `KToolArgs` 未像 `closeAs` 排除 `q.Search`，向 `tsc_` 条目发 `function_call_arguments.delta` 却不发 `.done`，触发严格客户端报错 |

### 供应商、账户、OAuth 与目录（Unit 3a & Unit 3b）
| # | 位置 | 缺陷概要 | 触发条件与后果 |
|---|---|---|---|
| H8 | `internal/provider/group.go:1955,2296`, `logins.go:321`, `copilot_accounts.go:113-153` | **`providersMu` ↔ `loginsMu` AB-BA 死锁与单 goroutine 重入永久死锁** | 存在待改名 Copilot GHE 账号（`l.was != ""`）时，`SaveGroup`/`Delete`/`Rename` 持 `providersMu` 调 `All()`→`loginsMu`→`writeLogins`→`moveAccountSettings`→再取 `providersMu`，直接永久死锁 |
| H9 | `internal/provider/signin.go:371`, `google.go:1785`, `copilot_signin.go:82-86`, `pluginsignin.go:57-68` | **`signInFlow.begin` 异步安装路径无锁写 `s.st`/`s.stop`（data race）且 `signIns.m` 泄漏** | 首次需安装 CLI 的登录同时轮询 `SignInStatus`/取消，触发并发读写竞态或漏调 `s.stop()` 泄漏子进程 |
| H10 | `internal/provider/chatgpt_api.go:404-456` | **`siwcFresh` 双重检查未命中时未更新 `c` 且未检删除，重复消费旧 `refresh_token`** | 并发刷新且新 token TTL 落入 `siwcMargin` 或期间重登，第二请求用锁前旧 `c.Refresh` 刷新触发 OpenAI `refresh_token_reused` 吊销整号 |
| H11 | `internal/provider/signin_revoked.go:143-162`, `logins_on.go:289-295` | **`NoteRevokedSignIn` 调 `renewSavedLogin(..., true)` 传空 `stale` 跳过并发去重** | 并发 401 触发 N 次串行无条件刷新轮换 refresh token；瞬时网络错误失败时直接永久 `markRevoked` |
| H12 | `internal/provider/migrate.go:413,487-544` | **`migrationsMu` 无跨进程锁且 `lockMoves` 存在 15 分钟强删锁 TOCTOU** | 桌面端后台迁移与 CLI `plugin remove`/`accounts forget` 并发覆盖 `migrations.json`；慢迁移超 15 分钟被误删锁导致双进程并发迁移 |
| H13 | `internal/provider/google.go:425-458` | **`googleAccount.token()` 缺刷新互斥/singleflight** | Gemini CLI/Antigravity 高并发临期刷新触发刷新风暴；瞬时 5xx/超时一律误报“signed out” |
| H14 | `internal/source/source.go:251-274` | **`source.Do` 在 `http.NoBody`（`GetBody==nil`）或切回官方源时跳过回退/发送已耗尽 Body** | `OWL_MIRRORS=on` 下带 `http.NoBody` 请求失败后因 `r.Body != nil && r.GetBody == nil` 直接 `break` 跳过所有镜像 |
| H15 | `internal/ghtoken/ghtoken.go:178-185` | **`ghtoken.Refused()` 在令牌刚被拉黑后返回空来源或错误指责其他环境变量** | 并发 GitHub 401 将 `GITHUB_TOKEN` 标记 refused 后，`Token()` 跳过它返回空或 `GH_TOKEN`，拼接出畸形/误导错误 |
| H16 | `internal/provider/plugin_accounts.go:474-504` | **`notePluginLapseOf` 对非 `toldNow` 不递增 `pluginSignInTold` 且用严格 `>` 比较** | 两路并发用量刷新同持 `began`，先返 401 标 `Lapsed` 后被同 `began` 的旧 200 覆盖清除，已失效账号反复被选中 |
| H17 | `internal/provider/planquota.go:869-876` | **`PlanQuotas` 查 Zhipu/Z.ai 续期与重置次数漏传 `j.p.Via(ctx)`** | Zhipu/Z.ai 配了专属代理时，主额度走代理但 `zhipuPersonalResets`/`zhipuTerm` 绕过代理直连 |
| H18 | `internal/provider/key_models_refresh.go:133-155`, `key_models_prefill.go:147-169`, `modelspec.go:242-303` | **模型列表后台刷新无锁 `Find -> Save` 覆盖用户并发编辑** | 后台刷新与用户编辑同一 Provider 并发时，`Save(*q)` 用旧快照覆写用户新改的代理/Key/模型配置 |
| H19 | `internal/provider/quota_history.go:314-365` | **`noteQuotaHistory` 指针字段值比较 `after[k-1] != last` 恒为 true 且 `lastNoted` 吞同账户多卡历史** | 带 `ResetsAt` 的窗口因每次新建指针导致每次轮询全量重写 `quota-history.json`；多卡同 `provider\|user` 会互相跳过漏记历史点 |

### Agent 接线与编辑（Unit 4）
| # | 位置 | 缺陷概要 | 触发条件与后果 |
|---|---|---|---|
| H20 | `internal/edit/file.go:153-155,396-401` | **`WriteAtomicMode` 与 `Atomically` 回滚在内容字节相同时早退，无法收紧 `0644` 权限** | 对已有 `0644` 密钥文件以 `0600` 写回相同内容（或事务回滚恢复权限）直接返回 `nil`，保持 `0644` 对他用户可读 |
| H21 | `internal/edit/env.go:67`, `agent/goose.go:190`, `piowl.go:60`, `qwenpaw.go:344-351`, `profile/profile.go:143` | **多处密钥文件与备份以 `0644` 落盘或存在 `0644→0600` TOCTOU 窗口** | 连接 Gemini/Goose/Reasonix/QwenPaw 或 `dropPiBlock` 备份用户 `models.json` 时，真实 API Key/网关密钥以 `0644` 暴露 |
| H22 | `internal/agent/gemini.go:53-83`, `claudedesktop.go:1037-1059`, `antigravity.go:208-225`, `applied.go:980` | **断连先 `unstash` 消费唯一备份再写盘且无原子事务回滚** | 断连写盘遇文件占用/磁盘满失败时，原配置与 `GEMINI_API_KEY` 备份已被销毁，重试无法恢复 |

### 资源库、MCP 与 Browser/Image MCP（Unit 5）
| # | 位置 | 缺陷概要 | 触发条件与后果 |
|---|---|---|---|
| H23 | `internal/library/skills.go:1085-1092,1448-1452,2084-2092` | **GitHub Skill URL 未拦截 `src.Path` 中的 `..` 路径穿越** | 输入 `https://github.com/o/r/tree/main/../../../../Users/...` 跳出 `tmp` 扫描并复制本机任意目录进 Skill 库 |
| H24 | `internal/library/plugin.go:239-247,274-286,201,296,326` | **`readPlugin` 枚举 `agents/*.md`、`skills/*` 与清单文件漏子条目 `inside()` 校验** | 恶意 git 插件放指向 `~/.ssh/config`/`~/.env` 的 symlink，读取本机任意文件进 `library.json` 并随同步外泄 |
| H25 | `internal/library/skill_edits.go:254-266` | **`mirrorDir` 反向同步未校验 symlink 目标范围** | Agent 副本目录中创建的逃逸 symlink 被原样同步回中央库并分发给全部 Agent |
| H26 | `internal/library/skill_beside.go:80-121` | **`freeSkillName` 确定性死循环，持全局 `library.mu` 锁死资源库** | 同名 Skill 的 `owner`/`repo` ≥ 61 字节（`withSuffix` 恒返 `""`）或 `Lstat` 返非 `ErrNotExist` 错误时 100% CPU 永久死锁 |
| H27 | `internal/library/mcp_check.go:103,931,1220`, `mcp_hub.go:979` | **MCP 健康检查与 Hub `checkClient` 默认跟随 3xx 重定向，绕过 `#37` 同源限制** | 远程 MCP 返回 307 指向 `127.0.0.1`/元数据服务，可 SSRF POST 并泄露 `X-API-Key` 等自定义认证头 |
| H28 | `internal/mcpauth/mcpauth.go:54-60,715-767` | **`mcpauth` SSRF 校验存在 DNS Rebinding 与代理侧解析绕过** | `targetAllowed` 校验后未在 `DialContext` 固定 IP 且走 `netproxy.Func`，恶意 MCP 元数据域名重绑定打内网/回环 |
| H29 | `internal/library/mcp_run.go:163-174,290-310,591-600` | **`mcp_run.ensure` 启动竞态造成孤儿进程树泄漏及已删服务继续运行** | `runStart` 耗时期间收到 `DELETE` 或 `runGiven` 删除服务，`s.stop()` 因 `child==nil` 空转，随后新启动子进程永久失管 |
| H30 | `internal/library/mcp_reap.go:41-60,88-106` | **`mcp_reap.launchedBy` 匹配无参命令末参数误杀无关用户进程，且忽略项目级 MCP** | 删除无参 MCP `my-mcp` 时匹配 `vim /path/my-mcp` 强杀用户进程；仅全局关闭但项目仍在用的 MCP 被误杀 |

### 会话、记忆、用量与预算（Unit 6）
| # | 位置 | 缺陷概要 | 触发条件与后果 |
|---|---|---|---|
| H31 | `internal/memembed/memembed.go:180,210-226` | **`memembed.Set(false)` 未等下载协程退出即 `RemoveAll`，重开还会永久卡死** | 下载中关闭语义搜索引发目录重建竞态；随即重新开启因旧 `dl.cancel != nil` 跳过启动，模型永不下载 |
| H32 | `internal/usage/request_page.go:904-948,1001-1023` | **`dayStart` 变更后 `queryPageOf`/`requestState` 缓存未失效** | 改 `DayStart`（如 0→4）后若逻辑日期字符串未变，持续返回旧日界线的统计页与热力图 |
| H33 | `internal/usage/usage.go:330-357` | **`usage.Append` 写盘前触发 `observeLedger` 且写盘失败仍经 `defer` 污染内存账本** | 磁盘满/权限错误写盘失败时，内存 `spendMemo` 仍累加虚高费用并向 OTLP 上报未落盘记录 |
| H34 | `internal/budget/budget.go:229-260,708-715,840` | **`budget.Append` 无视 `PriceVersion`；`Reserve` 对无 `/` 简称模型预留 0 费可超额穿透** | 价格变更间隙混算新旧单价；简称模型并发首笔请求预留 0 费用，多并发同时穿透限额 |
| H35 | `internal/sessions/manage.go:859,878-900` | **非 gateway `sessions.Restore` 漏校验 `m.From` 与 `m.Name == ".."`，且缺 `storeMu.Lock`** | 损坏/篡改的回收站 `session.json` 可移动目录至任意路径；恢复期间与并发扫描产生竞态 |

### 隐私脱敏与插件运行时（Unit 7）
| # | 位置 | 缺陷概要 | 触发条件与后果 |
|---|---|---|---|
| H36 | `internal/redact/json.go:1064-1093` | **`MaskJSON` `namedSecret` 短路绕过 `API_KEY` 策略、自定义词优先与占位符去重** | `Actions["SECRET"]=="warn"` 时 `{"api_key":"sk-proj-..."}` 明文放行；自定义 `Words` 被 `Allow` 压制；已有占位符被二次套娃脱敏 |
| H37 | `internal/redact/redact.go:1404-1429` | **`unescapedView` 将 `\uXXXX` 覆盖为换行，工具参数内 Unicode 转义密钥/中文敏感词漏脱敏** | Python `ensure_ascii=True` 或 Go 转义 `&` 发送的首发中文敏感词/密钥完全逃过所有规则与敏感词扫描 |
| H38 | `internal/redact/writer.go:234-290,441-461` | **`eventCtx` 仅取首个 `"index":`，并行多 `tool_calls` SSE 跨 chunk 串拼参数** | OpenAI 流式并行返回多工具调用且工具 0 尾部截留 `partialTail` 时，被错拼到工具 1 开头导致双工具 JSON 损坏 |
| H39 | `internal/plugin/store.go:429-431,453-459` | **`AddExact` 与 Git 规格绕过 `RefuseWithdrawn` 已下架高危插件拦截** | 通过 `github:...` 安装或在同步/备份恢复调 `AddExact` 可装入已下架的封号风险插件（如 Antigravity） |
| H40 | `internal/plugin/host.js:3600-3626` | **`host.js` `chat.headers` 向所有已安装插件明文广播目标 Provider `o.apiKey`/`o.headers` 可变引用** | 任意第三方插件均可读取其他所有插件 Provider 的明文 API Key/OAuth Token 并原地篡改缓存 `options` 劫持流量 |
| H41 | `internal/plugin/dist.go:170-214,231-257` | **`mergeDist` 允许镜像用更高版本号与自身 `sha512` 覆盖官方索引** | 开启国内镜像时，被污染的镜像伪造更高版本号及匹配 `sha512`，即便官方可达仍被优先采纳执行恶意插件 |
| H42 | `internal/gateway/redact.go`、`internal/masking/masking.go` | **按设计，不更改。** `RedactOn` 在用户没有开启任何类别时补上默认脱敏规则。 | 用户指定只处理某些词或个人信息时，继续按已选类别处理。回归：`TestForcedMasksOnlyWhereSettingsMaskNothing`。 |

### 同步、备份、更新、设置与 OS 集成（Unit 8）
| # | 位置 | 缺陷概要 | 触发条件与后果 |
|---|---|---|---|
| H43 | `internal/settings/settings.go:1953`, `settingsapi/settingsapi.go:137-156`, `settings/proxies.go:244` | **`settings` 缺少跨进程 `filelock`，且 `Apply`/`RestoreProxies`/`backup.Restore` 无锁 Read-Modify-Write** | GUI 与 CLI 跨进程并发或单进程 `Apply` 与其他 `settings.Update` 并发时静默丢失配置更新 |
| H44 | `internal/settings/settings.go:2419-2439` | **`SetRouterNote`/`SetRouterAllTools` 错传 `recording=true`** | 绕过 `GatewayConversations` 写保护，在 `lastgood` 回退或并发场景意外改写用户隐私录制同意开关 |
| H45 | `internal/backup/backup.go:266-280`, `davsync/davsync.go:1081-1144` | **无密钥备份与同步未剥离 `Settings` 中 6 个内联代理字段的账号密码** | `Proxy`/`PluginProxy`/`UpdateProxy`（含 `*Custom`）中的 `user:pass@host` 随 `Keys:false` 备份/同步泄露 |
| H46 | `internal/davsync/davsync.go:1532,1586-1590,1723-1746` | **首次加入同步 `keepUnseenGatewayKeys` 修改 `remote` 未同步更新 `merged`** | 远端 `AccountGroups==nil` 时本机 `AccountGroups` 不上传，下次远端设置变动同步时永久覆写丢失 |
| H47 | `internal/appdir/migrate.go:30-60`, `move.go:474` | **`appdir.migrate` 在外接盘未挂载时被 `Unreachable()` 触发，反删外接盘软链覆写旧 `magpie` 数据** | 曾由 `magpie` 迁移且 `owl data move` 到外接盘；拔盘启动时 `Unreachable()`→`Config()`→`migrate()` 见断链 `os.RemoveAll` 删软链并覆盖旧数据 |
| H48 | `internal/uninstall/uninstall.go:253-282` | **`uninstall.Data()` 跟随软链 `os.RemoveAll(at)` 前未校验 `removable(at, home)`** | `~/.config/owl` 为手动指向非 owl 目录（如 `$HOME`/`dotfiles`）的软链时，卸载直接递归删毁目标目录全部文件 |
| H49 | `internal/update/update.go:1146-1152` | **`InstallBinaryAsAdmin` 跨分区 `mv -f` 非原子替换且无回滚** | Linux `/home` 与 `/usr/local/bin` 分区不同时，提权更新中断或满盘导致原二进制被删/截断变砖 |

### GUI 后端、前端与管理 API（Unit 9）
| # | 位置 | 缺陷概要 | 触发条件与后果 |
|---|---|---|---|
| H50 | `internal/gui/admin.go:84-90,172-201` | **受限管理 Key 可通过管理 API 自我解除全部限额/模型/并发约束或签发无限制新 Key** | `adminMayChangeKey` 放行自身 Key 修改（`in.Key == Caller.KeyID`）与 `add-key`/`issuer-key`，且 JSON 解码失败时 fail-open 返回 `true` |
| H51 | `internal/gui/providers.go:2255-2298,2525-2565` | **`provider/save` Preset 分支遗漏拷贝字段；零值判断导致无法通过 API 关闭布尔开关或清零限额** | API 发送 `{"quotaRest":0}` 或 `{"codexAsSent":false}` 被后端静默覆写回旧值却返回 200 OK |
| H52 | `internal/gui/pluginpage.go:267-304`, `web.go:245-248`, `api.go:3451` | **`pluginpage` 直接信任 `?_o=` 注入 CSP 白名单；未认证 `POST /_owl/plugin/*` 绕过 `webGuard` 触发 `provider.Changed()`** | 诱导带 `?_o=https://evil.com` 打开插件页即向 CSP 注入外域破坏沙箱；未认证 POST 持续清空全站缓存 |
| H53 | `internal/gui/assets/pluginpages.js:90,121-150,338` | **`pluginpages.js` 缺失 `pagebridge.js` 依赖的 `asset`/`image.copy`/`image.save` 与 `_o` 参数** | 社区插件调用 `owl.image.copy/save` 必报 `No such call`；`owl web` 动态加载脚本与反代无 Host 场景全败 |

*(注：High 实际经核实共 53 项，含多单元独立缺陷，已全部保留以便逐项排查)*

---

## 三、Medium（41 项）

| # | 单元 | 位置 | 缺陷概要与触发后果 |
|---|---|---|---|
| M1 | U1 | `internal/gateway/search_fetch.go:83`, `chatpage_messages.go:648` | `web_fetch` 与 ChatPage `read_url` 直调 `budget.Reserve` 绕过 `gwbudget.Limiting`，限额插件关闭时未 fail-closed |
| M2 | U1 | `internal/gateway/codex_subagent.go:101-110` | 校验路由组仅对首成员调 `modelAllowed`，未用 `groupGiven`，受限 key 下错误改写致 403 或拒绝合法组 |
| M3 | U1 | `internal/gateway/gateway.go:2198,2230-2231` | `count_tokens` 命中 `maskRouteFor` 重定向后未按目标组重新施加 `heldByGroupAt` 账号限制 |
| M4 | U1 | `internal/gateway/handover.go:324-341` | 热重启 `forwardCallback` 每次请求新建未关闭的 `http.Transport`，高并发回调泄漏 fd 与 goroutine |
| M5 | U1 | `internal/gateway/lan.go:704-728` | 热重启窗口期 `ownCallback` 对所有 `POST /_owl/claude-mcp/*` 无条件放行，绕过 IP 白名单与限流（叠加 M4 放大耗尽 fd） |
| M6 | U2 | `internal/claudetui/tap.js:144-150` | 跨 chunk 的 `\r` + `\n` 被先后替换为 `\n\n` 劈碎 SSE 事件，静默丢失流式输出 |
| M7 | U2 | `internal/middleware/run.go:389-393` | JS 中间件 `writer.finish` 对无尾空行的末事件跳过 `onEvent` 钩子直接输出 |
| M8 | U2 | `internal/gateway/anthropic.go:1067-1079,1138-1149,1251-1253` | `anthropicBody` 丢失 PDF `document` 块的 `cache_control`；客户端仅标 `tools` 时所有缓存断点失效 |
| M9 | U2 | `internal/gateway/chat.go:1449-1453` | `chatDecoder` 遇 `[DONE]` 漏调 `d.end(emit)`，无 `finish_reason` 流的尾部缓冲文本丢失 |
| M10 | U2 | `internal/gateway/claude_subscription.go:2761-2766` | `shelve()` 构造 `savedSession` 漏设 `tui: run.tui`，TUI 会话入架/重启恢复后被错当 `-p` 会话 |
| M11 | U2 | `internal/gateway/mcp_ask.go:48-56` | `clientAsked` 全局表未绑定 `Mcp-Session-Id`/调用者身份，跨端点无校验应答 |
| M12 | U3 | `internal/provider/login_import.go:422-510,704-721`, `google_import.go:213-254` | `ImportLogins` 并发池共享静态 `kept` 快照，同批导入同账号新旧 token 会互相覆写绕过 `claudeNewerKept` |
| M13 | U3 | `internal/provider/claude_signin.go:391-424`, `signin.go:679-701` | `claudePaste` 未校 `state`/Path 且未禁 HTTP 代理（授权码可经代理外泄）；`signin.go` `/cancel` 无认证、`error` 校验早于 `state` 可被本机任意网页跨站中止登录 |
| M14 | U3 | `internal/provider/group.go:2361-2411` | `DeleteGroups` 校验与循环删之间无锁非原子，违背 all-or-none 语义可留部分删除 |
| M15 | U3 | `internal/provider/group_copy.go:17-62`, `group_share.go:159-300` | `CopyGroup`/`ImportGroup` 锁外分配新 ID 后 `SaveGroup` 覆盖，并发双击会互相覆盖且 `SetGroupOrder` 失败残留新组 |
| M16 | U3 | `internal/provider/provider.go:1823-1873`, `rename.go:30-130` | `Delete`/`Rename` 持 `providersMu` 期间写 `settings` 锁序不一致；`Rename` 先 `os.Rename` catalog 再 `store(f)` 失败不回滚 |
| M17 | U3 | `internal/provider/group.go:2448-2470` | `PickGroup` 未展开 `Match` 成员也未做 `cleanMember`，含模式匹配成员的 Manual 组永远报错拒绝切换 |
| M18 | U3 | `internal/provider/quota_alert.go:74-190,256` | `usage-alerts.json` 非原子写盘，崩溃丢状态重发全部告警；`WatchQuotas` 未过滤已隐藏卡片 |
| M19 | U3 | `internal/provider/alibaba_usage.go:459`, `volcengine_usage.go:327,375`, `planquota.go:131,195` | 额度百分比未夹紧 `[0,100]` 且未防 `NaN`/`Inf`（`ParseFloat("NaN")` 无错），异常上游致 `quotas.json` 落盘或 API 500 失败 |
| M20 | U3 | `internal/fx/fx.go:81-127,243` | `writeCache` 非原子写盘；`getAt` 无并发去重且单个 `ctx.Canceled` 会触发全局 10 分钟冷却 |
| M21 | U3 | `internal/probelog/probelog.go:46,110-147` | 全局函数变量 `probelog.Sink` 无同步保护，存在 data race 与 TOCTOU nil call 风险 |
| M22 | U3 | `internal/catalog/live.go:462` | `saveLive` 非原子 `os.WriteFile`，并发读大列表（如 OpenRouter）可读到截断 JSON |
| M23 | U3 | `internal/provider/pluginsignin.go:157,189,209-227` | `pluginCode` 输错一次即清空 `s.plugin` 终结流程；`pluginBegin`/`pluginCode` 调 `plugin.Finish` 均漏 `s.via(...)` 代理上下文 |
| M24 | U3 | `internal/provider/remote_quotas.go:412-416` | `refreshRemote` 丢弃 POST 返回的卡片再无条件 GET，冗余请求且 GET 成功会吞掉 POST 刷新报错 |
| M25 | U3 | `internal/catalog/catalog.go:1020-1049` | `TierPrice` 未 `bareID` 归一（带厂商前缀直接未命中少算 2x-6x 费用）、遍历 map 顺序随机、`CacheWrite1h` 缩放偏差 |
| M26 | U4 | `internal/desktopiso/desktopiso.go:96,206-211` | `pgrep -f` 未对 `$dir` 正则转义破坏单实例互斥；`Add` 先删旧 `.app` 再 `Rename` 失败不回滚 |
| M27 | U4 | `internal/edit/json.go:357-361` | `delJSONBytes` 回扫键名引号忽略 `\"` 转义，删除含转义引号键时损坏整个 JSON 文件 |
| M28 | U4 | `internal/agent/keptaway.go:53-72` | `owlFile` 在 Windows 上区分大小写匹配前缀，盘符/大小写差异时误篡改 owl 自身配置文件 |
| M29 | U4 | `internal/agent/claude_dirs.go:67-83` | 自定义 `CLAUDE_CONFIG_DIR` 且同时存在 `~/.claude-default` 与 `~/.claude` 时生成两个重复 `claude#default` |
| M30 | U4 | `internal/agent/preview.go:129,364-384` | `DisconnectPreview` 丢弃 `agentenv.NotPaths` 非路径环境变量；忽略 `EvalSymlinks` 错误可能污染当前目录 |
| M31 | U4 | `internal/profile/profile.go:191-244` | `profile.Save`/`Delete`/`Rename` 全无锁保护，并发修改丢失更新 |
| M32 | U4 | `internal/agentplug/agentplug.go:296-321` | `current()` 在锁外算 `stamp()` 再与 `Reload()` 并发，将陈旧插件快照写回全局缓存 |
| M33 | U4 | `internal/wslrun/wslrun.go:426-454,513-528` | `cancelable` 扫描业务参数误覆盖 `OWL_WSL_RUN=` 取消标记；`SetVia` 锁外写盘导致内存与磁盘失步 |
| M34 | U4 | `internal/agent/plugged_launch.go:104-113`, `agy.go:90-95`, `cursorlocal.go:215-221` | 启动命令生成对 `key`/`gw`/env 值未做 Shell/PowerShell 引号转义，特殊字符引发语法破坏或注入 |
| M35 | U5 | `internal/library/skills.go:1236-1255`, `skillgit.go:125-131` | `untar` 未检 `h.Size < 0` 或 `int64` 溢出绕过 `maxFetch`（Tar Bomb）；`gitSource` 在 Windows 上被反斜杠 `\` 绕过穿越检查 |
| M36 | U5 | `internal/mcpauth/mcpauth.go:487,1205-1215`, `browserext/ext/cdp.js:53-59,151-159` | `mcpauth.renew` 10s 锁超时 < 30s HTTP 超时且忽略 `held` 吊销轮换 token；固定 `CallbackPort` 重试被自身旧 flow 阻塞 10 分钟；`browserext` `detach()` 异步竞态误断活跃 tab |
| M37 | U5 | `internal/imagemcp/imagemcp.go:558,688-717,781-793`, `library/plugin.go:1022`, `carry.go:536` | `imagemcp` `reference()` 无文件类型/大小上限且扩展名优先绕嗅探、`target()` 悬空 symlink 任意文件写；`installPlugin` 升级先删旧文件再 `Rename` 无回滚；`carry.Put` 信任外部 `Source.Dir` 软链本机任意目录 |
| M38 | U6 | `internal/memembed/memembed.go:531-534`, `memory/search.go:200`, `memory/tidy.go:99`, `memdistill/dream.go:68-123` | `similar()` 未检向量长度与 `NaN`（`NaN` 污染 `fuse` 的 `max` 令整次语义搜索归零）；`Tidy` 按字节数限制 `MaxText` 误拒 >667 汉字合并；`Dream` 不可 `Stop()` 且 `Undo` 缺 `.run` 文件锁 |
| M39 | U6 | `internal/sessions/manage_quick.go:111-137`, `codex_catalog.go:334`, `codex_provider.go:1543-1554`, `said_index.go:197,552`, `otlp/otel.go:106`, `usage/feed.go:266,340` | `quickRead` 释放锁引发 `glances` 泄漏/占位缓存落盘；`restoreIn` 空列非法 SQL；`RetagLoadedCodexThreadsBut` 失败仍 `Commit()`；`saidKeys` 全报错静默返空；`otlp.Serve` 停机不等 `watchSessions`；`feedRefresh` 持锁全量扫描 |
| M40 | U7 | `internal/plugin/host.js:861-934,3685-3696`, `distcopy.go:93-138,243-283`, `store.go:154`, `redact/json.go:1120`, `writer.go:312`, `shape.go:352`, `pii/serve.go:680`, `host.go:1151` | `host.js` `proxy502` 绕流控可重复发 head、`tunnel` 忽略背压 OOM 且提前断连挂死；`distcopy` 无解压体积上限（gzip 炸弹）且 Windows 反斜杠穿越；`lockAuth` 无归属校验删锁；`signed` 回填无词边界毁签名；`sealedText` 误跳过 `"data:"` 文本；`pii/serve` 无界 `u32()` 分配 OOM；`startOn`/`startOne` FD 泄漏与 `sessions` Map 泄漏 |
| M41 | U8 & U9 | `internal/filelock/filelock.go:55,81`, `proc/tree_other.go:38-60`, `update/update.go:458-477`, `davsync/dav.go:165`, `appdir/move.go:263`, `filememo/filememo.go:298`, `handoff_unix.go:75`, `gui/assets/plugins/gateway/client.js:14-18`, `gui/lightweight.go:104`, `port.go:86`, `panelhome.js:85`, `cli/*`（6 个子命令）, `gui/providers.go:3676`, `tui/pages.go:1230` | `filelock` 10s 超时静默无锁进入；Unix `CommandContext` 漏杀孙进程；`sameSigner` 在 `TeamIdentifier==""` 时接受任意 ad-hoc 包；WebDAV HTTPS→HTTP 降级泄露凭证；`appdir.Move` 半迁移不回滚；`filememo.settle` 回滚旧缓存；`handoff` 固定 `.tmp` 竞态；`gateway/client.js` 顶层裸调 `localStorage` 在隐私模式白屏；`lightweight`/`port` 无锁覆写 settings；`panelhome` 乱序响应覆写；6 个 CLI 子命令把未知 `--flag` 直接写进配置/覆盖真实 Key；`POST /api/signin/export` 空 `res.Files` 越界 panic；TUI `View()` 主线程同步 4s 网络调 `fx.Get` 卡死终端 |

---

## 四、Low（15 项）

1. `internal/gateway/key_limit.go:71-84`：`keyLimited` 在小路由限额校验前即按 512 MiB 上限读取请求体且绕过 `bodyRoom` 内存预算。
2. `internal/codexcat/codexcat.go:120-127`：`DefaultEffort([]string{})` 直接索引 `e[0]` 越界 panic。
3. `internal/gateway/responses_ws.go:881-906`：非流式回退直接拼接未做 `json.Valid` 校验的原始响应体。
4. `internal/gateway/claude_subscription.go:750-756`：`randomToken()` 保留了 `crypto/rand.Read` 失败降级为纳秒时间戳的不安全分支。
5. `internal/provider/login_export.go:175-218`：`ExportLogins` 未跳过 `AccessOnly` Codex 账户，连带原 `refresh_token` 一同导出。
6. `internal/provider/codex_signin_gone.go:35,137-175`：`codexGoneMu` 仅进程内加锁，多进程并发读写 `codex-signin-gone.json` 会互相覆盖。
7. `internal/provider/plugin_icon.go:88-95,146-157`：`keepPlugIcon` 非原子写盘，后台下载完成后未复检路径切换。
8. `internal/provider/plugin_usage.go:86-92`：`pluginOfAgent` 失败前即调用 `pluginCard(pp)` 产生空 `Provider` 卡片。
9. `internal/provider/tencent_usage.go:346-352`、`catalog/public.go:39`、`catalog/fresh.go:101-118`：腾讯云周期推算负值冗余循环；`PublicDrawers` 主机名未转小写；`Asked(ctx)` 取消仍设 10 分钟冷却。
10. `internal/wslrun/wslrun.go:293`、`agent/claudedesktop_isolated.go:295-327`：WSL 盘符仅裁小写 `c`；`SetDesktopIsolated(true)` 中间失败无回滚。
11. `internal/library/project_scope.go:167-230`、`instructions.go:165-175`、`h264/encode.go:43-57,86-99`：`syncClaudeLocal` 改 `~/.claude.json` 前未备份；`instrOwner` 未 `EvalSymlinks`；`h264` 负尺寸/超大宽高越界溢出 panic。
12. `internal/dayzone/dayzone.go:117`、`memory/project.go:94`、`sessions/manage.go:531`、`usage/usage.go:1305`：`tzif()` `int32` 2038 溢出；`originURL` 64KB Scanner 截断；`Delete` 终写 manifest 失败未回滚；`LastSeen` 1MB Scanner 截断。
13. `internal/plugin/gitrev.go:216-258`、`readme_lang.go:164`、`host.go:2122`、`host.js:267`、`masking.go:164`、`pluginrt.go:122`、`redact` 三正则缓存：`github:` spec 路径/查询注入；混合大小写 `Https_Proxy` 失效、`Bun.spawn` 丢 `NO_PROXY`；`toGuard` 缺 `testing.Testing()` 保护；`pluginrt` 同名服务误删；三 `sync.Map` 正则缓存无上限。
14. `internal/backup/backup.go:368`、`backup/data.go:22-23`、`davsync/davsync.go:1614`、`daemon/launchd.go:110,141`：备份派生密钥缓存永不淘汰；冲突备份秒级命名同秒覆盖；`launchd.restart` 忽略 `bootout` 异步退出竞态。
15. `internal/cli/web_cli.go:60,71,135`、`daemon_cli.go:260`、`gui/api.go:3048-3064`、`library/client.js:3701`：`owl web` 命令行/进程参数明文暴露 `?k=<secret>`；`redact-actions` 缺少动作值与大小校验；`newSkillRow` 未校验链接协议。
