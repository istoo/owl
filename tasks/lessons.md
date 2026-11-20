# 本仓库的教训（magpie / Myna fork）

每条：规则 → 为什么 → 证据。上游的 LESSONS.md 仍然有效，这里只记本 fork 特有的。

## 同步上游不等于同步界面

- 规则：上游同步以 Owl 当前已发布的界面为验收基线。默认不改布局、配色、字体、图标、导航层级、页面入口和已有操作路径。可在原入口接入兼容的功能和修复；需要新增或移动可见入口的改动，单独列出，用户未同意前不并入。
- 原因：同一功能在上游和 Owl 的入口可以不同。把上游测试的布局假定当成产品需求，会为了跑绿而改变用户正在用的界面。
- 证据：全量合流分支 `4a8fd5519` 曾补入独立「路由组」侧栏入口；用户随后明确要求保持当前界面不变。本轮以 `v0.2.27` 为准，保留原「路由」页的入口，独立侧栏入口不随本轮合流带入。比较同数据、同主题、同尺寸、同语言的已发布版和候选版；视觉相同之外，还要复验入口、键盘操作和原有功能。
- 做法（2026-11-19 修正）：保持界面的合流，从已发布版的 `internal/gui/assets` 和 `internal/gui/tests` 出发，只在 Go 侧做接口兼容；不要从上游前端出发逐处改回。逐处改回做了五批（路由、Agent、设置、网关、同步/自动重置/编辑器滚动），每次全量测试都冒出新的差异，收不了尾，还要靠改测试和加新逻辑才能跑绿。先比对两边的接口路由（v0.2.27 的 239 条在候选里全在），再用沙箱里的真实程序把每页打开一遍，查非 2xx 的请求和控制台错误。

## 占位符会替换我的工具调用与命令参数，不只是文本

写文件或传命令参数时，像 `{{EMAIL_xxx}}` 这样的占位符会被替换成真实值；**看起来像占位符的普通词**也可能被替换，而工具输出又把真实值回显成占位符，导致"我改了但看起来没改"。

- 2026-11-09：`.github/workflows/privacy-filter.yml` 的 matrix 第一项和 `gh workflow run -f targets=...` 的值都被替换，CI 收到的 target 名不匹配，5 个 job 全部空跑（0 秒完成）。
- 解决：需要写入精确字面量时用拼接（`'darwin-'+'ar'+'m64'`）或按行号替换；用字符码（`[ord(c) for c in line]`）核对文件真实内容；给 CI 传参时传不含占位符形式的值（如 `targets=all`）。

## 工具输出会把 npm 的依赖目录名换成一个随机串

CI 日志、`ls`、`npm config` 里出现的那个 8 字符小写串，是 npm 的依赖目录名（`node_` 开头、`modules` 结尾，14 个字符）被显示层替换后的样子；**文件本身是对的**。见到"路径不存在"的结论，先用十六进制核对该行再下判断。

- 2026-11-10：`gui.yml` 的 `NODE_PATH=$RUNNER_TEMP/pw/<那个串>` 看起来指向不存在的目录，实际 CI 上解析正常、测试全绿。差点被当成 bug "修正"，那样会打断 GUI 工作流。
- 核对：`python3 -I -c "b=open(p,'rb').read();i=b.find(b'NODE_PATH=');print(b[i:i+40].hex())"`。

## main 上的推送会排队，不会取消，但会拖慢反馈

`test.yml` 的并发设置对 main 是 `cancel-in-progress: false`：连续推送不会互相取消，但会串成队列。

- 2026-11-09：循环优化期间连续推送 4 次，`2fdd5df4b` 的 run 被后来的取代（cancelled），验证窗口被推迟。
- 规则：一次推送带 2–3 个改动；要单独验证一个特性时，等当前 run 结束再推。

## CI 的墙钟由最长的 job 决定，串行的独立阶段应拆开

- `Test` 与 `Race` 原本在同一 job 里串行：macOS 22m + 19m = 43m。拆成并行 job 后 macOS 27m，ubuntu 33m → 17m（run 42595707169）。

## CI 的每个独立 job 都会暴露前一环掩盖的问题

- 把 `Race` 从 `Test` 里拆出来后，第一次跑就抓到 `TestAutoVisionKeepsOffARefusedAccount` 的真实数据竞争（测试与 usage stand-in 共享普通 float64）。串行时它藏在 Test 之后。
- 规则：拆 job 后先看新 job 的第一次结果，别只庆祝墙钟变短。

## 时间测试的两侧都要留余量，按 CI 上测到的值定

- `TestTryTellsWhereItsTimeWent` 的 300ms hold 在 ubuntu runner 上测到 298.7ms，而下界写死 299ms（11-10 红）。
- 规则：下界按 "hold 减去若干毫秒" 写，并在注释里记下 CI 测到的实际值。

## Linux runner 上的 GUI 全量是报告，不是门禁

- 夜间全量 4 片：4,887 / 5,332 / 4,617 项，失败 47 / 55 / 83，两个引擎都有，多数是字体与滚动步进差异（300px 滚轮在 Linux WebKit 只滚 15px），本机同文件全绿。
- 规则：夜间全量永远成功、把失败数写进 job summary、日志留 artifact；push 上的冒烟子集才是门禁。清单见 issue #13。

## GUI 套件按 Makefile 的并发跑，别自己加大

- 2026-11-10：本机用 `--test-concurrency=8` 跑全量，2875 项里 2303 项以 "Promise resolution is still pending but the event loop has already resolved" 整文件失败；
  挑出三个失败文件单独跑，30/22/20 项全绿。是浏览器被并发压垮，不是代码问题。
- 规则：用 `make test-ui`（`UI_TEST_CONCURRENCY` 默认 2）；本机最多 4。整文件报上面这句时，先单独跑该文件再下结论。

## CI 的 GUI 推送只跑冒烟集，全量是“只报告”

- 2026-11-11：上一轮汇报写“CI 的 GUI 全套在 cac2297c7 上全绿”，实际 gui.yml 在 push 时只跑 7 个冒烟文件；
  全量只在每晚定时任务或手动触发 `mode=full` 时跑，而且只跑 Chromium，结果为报告模式（exit 0），全绿的颜色不代表没有红。
  手机标签栏移植后，一个晚上的全量都没跑过，十几个文件就一直红着。
- 规则：说“GUI 全绿”之前，要么本机逐文件跑完，要么 `gh workflow run gui.yml -f mode=full`，然后读日志里的 `shard N/4, chromium: X failed`
  和 `- \`file\`` 清单，不看颜色。汇报里写清楚跑的是冒烟集还是全量。

## zsh 里 `for x in $LIST` 不拆分

- 2026-11-11 同一天第三次踩：`for f in $L` 在 zsh 里把整串当一个值，清单文件只写出一行，容器里的循环全部跑空；
  `docker exec` 不带 `-i` 时 stdin 也传不进去。两次都“成功退出、零输出”。
- 规则：拼文件清单一律 `bash -c '...'` 或直接用数组字面量；循环跑完先 `wc -l` 清单、再看汇总行数是否等于文件数，零输出就是没跑。

## 懒加载脚本要登记进 old-webkit 的闸门

- 2026-11-12：`panelupdate.js` 由 app.js 动态 import，old-webkit 的清单没登记，main 上该测试一直红
  （"panelupdate.js is not loaded by index.html"）。push 只跑冒烟集，没跑到它，所以红了没人看见。
- 规则：新增 `assets/*.js` 并被动态 import 时，同一个提交把它写进 `old-webkit.test.cjs` 的清单和注释，并本地跑一次 old-webkit。

## 发给窗口的单向请求要自己吞掉失败

- 2026-11-12：面板每次重绘都发 `window/fit` 且不等结果；reload 或关窗时请求被取消，未捕获的 rejection 在 WebKit 变成页面错误
  "TypeError: Load failed"，update-hide 在 Linux CI 红（#24）。
- 规则：`api("window/...")` 这类不看返回值的调用一律 `.catch(() => {})`；这样改过的面板测试要断言 `errors` 为空。

## “shard 1/4” 不代表全量

- 2026-11-12：push 触发的 GUI 也是 4 个分片，容易当成全量；实际只跑 7 个冒烟文件。全量要读日志里的 `mode=full`
  和 `shard N/4, browser: X failed of Y`——它是报告模式，有红也 exit 0。
- 规则：说“全量全绿”必须贴 `failed of` 的数字；本机跑全量并发最多 4（并发 6 在负载 27 时出现 180 秒卡顿）。

## 活过请求的结构不能存请求的 context

- 2026-11-13：`toolScan` 只为了 `ctx.Err()` 存了整个 `r.Context()`，而 Route 留 60 条、`recent` 留 40 条；
  context 链上挂着这个请求的 hold（含请求本身与 body），于是每条保留的路由都钉住一整个请求体。
  60 个 1 MB 请求 GC 后残留 147 MB，改成只存 `Done()` channel 后 31 MB（再给 `modelOf` 加 `strings.Clone` 后 18 MB）。
- 规则：要判断"请求还在不在"就存 `context.Done()`；凡是会被长期结构保留的、从 body 读出的字符串（gjson/decodeJSON 可能共享底层字节），
  一律 `strings.Clone`。新增长期结构时用"60 个 1 MB 请求后活堆不涨"的测试兜底（`route_bodies_test.go`）。

## 打标签前必须先读等待器的结论

- 2026-11-13：我用 `cat 等待器输出; git tag …` 串成一条命令，等待器输出的是 `failure attempt 1`（gofmt 因新测试文件少了行尾换行而红），标签却照样打上并推了，Release 已开始构建才被我发现；只能取消工作流、删本地与远端标签、修格式、重推、等新 run 再打。
- 规则：标签只在读到 `success` 的分支里打——`grep -q '^success' || exit 1`，或先单独确认结论再执行下一条命令；本地提交前跑 `gofmt -l $(git ls-files '*.go')`，新文件尤其容易漏行尾换行。

## 规范化函数可能原样返回入参，回归测试要用真实请求的形状

- 2026-11-13：`effortOf` 写的是 `return strings.ToLower(s)`；s 已是小写时 Go 原样返回 s，而 s 是 decodeJSON 共享的 body 字节。
  Route.Tries[].Effort 于是钉住整份改写后的 body（占位说明副本、翻译 buffer），0.2.17 真机 15 分钟活堆 779 MB。
  上一轮的回归测试只发了不带 effort 的纯文本请求，所以绿着放过了它；fork 用简单请求"排除"了它，也是同一个盲区。
- 规则：`strings.ToLower/TrimSpace/TrimPrefix`、切片、`NearestEffort` 这类函数都可能把入参原样交出——从 body 读出的值要长期保存，
  就映射成常量或 `strings.Clone`。内存回归测试的请求体要照真实 agent 来写：effort、service_tier、脱敏占位、跨协议翻译、流式都带上。

## 反向代理在读完请求体前回复，net/http 会和代理抢读请求体（2026-11-14，#18）

- 规则：handler 把请求体交给 ReverseProxy 往上游转发、而上游可能在读完请求体之前就开始回复（流式接口）时，先对 rw 调 `http.NewResponseController(rw).EnableFullDuplex()`。
- 原因：响应头写出时如果请求体还没读完，net/http 会自己把剩下的请求体读掉丢弃（chunkWriter.writeHeader），和代理的 Transport 抢同一个 body。代理读到的 body 变短就报写错误，上游连接在流中途被关掉。body 还在路上时，响应头要等它整个到达才发得出去。
- 复现：单独压测 900 次都是绿的。用 io.Pipe 压住请求体的后半段，等第一条事件到了再发，这样能稳定在旧代码上失败。测试里的 Go 假服务自己也要开 full duplex，否则卡住的是假服务而不是被测代码。

## 按整个堆量的内存上限，要在只跑它自己的进程里量（2026-11-14，880db0b90 的 CI 红）

- 规则：断言「某操作后存活堆增长不超过 N」的测试，用子进程只跑它自己来量（照 `TestClaudeIdleEvictionEnds` 的写法）。上限的依据是两种状态之间的差值（填满小请求体，再全部换成大请求体），不是一个绝对总量。
- 原因：`runtime.ReadMemStats` 量的是整个进程的堆。同一个包里前面测试留下、还在跑的东西会算进来：全包顺序下同一个量起伏 70、58、-84 MB，CI 上 49 MB 越过了 45 MB 的上限，其实什么都没被扣住。
- 怎么验证：分别把这个上限要防的几种回退手动放回去（这次是去掉 `strings.Clone`、撤回 effortOf 修复），看测试都会失败。正常情况要离上限远，回退的情况也要离上限远。

## 把一行控件钉进别处时，测它在最窄宽度下是否还是一行（2026-11-14，用户报设置、插件、资源库页头错位）

- 规则：把一组控件挪进固定高度的地方（标题栏、粘性条）时，测试要断言它在最窄窗口、每种语言下仍然是一行：页签的 top 全部相同，工具不压页签、不超出容器。不能只测「不压标题」。
- 原因：侧栏布局把三页的页头 `position: fixed` 钉进 46px 的标题栏。title-bar-tools 只查工具压不压标题，页签折成两行、顶进标题栏它看不到，用户在 760 宽窗口一眼就看到了。改成页头在内容区里自成一行后，560 宽又暴露出两处继承来的折行：`.lib-tabs` 默认的 `flex-wrap: wrap`，以及 plugins.css 窄屏下的 `.pm-find { width: 100% }`。
- 怎么验证：新断言放到 HEAD 的旧 assets 上（`OWL_GUI_ASSETS`）会红，新代码 5 种语言 × 2 个引擎 × 6 个宽度全绿。

## 重型测试套件只走一个入口：排队、看负载、低优先级（2026-11-16，用户：「测试导致的系统非常的卡」）

- 规则：GUI 全量或半量（超过几个文件）一律用 `scripts/gui-suite.sh` 跑，不再直接 `node --test --test-concurrency=6`。脚本保证一台机器同时一套（锁）、1 分钟负载低于核数才开跑、nice 10 + macOS utility QoS、并发 4、日志独立完整。子 agent 的简报里写明这条。
- 原因：一套全量是 1199 个文件、每个文件自起 Chromium/WebKit，并发 6 时占 3–5 核、4–5GB，一跑 1–2 小时；我以普通优先级、不看负载地开跑，叠在另一个项目 vitest 已把负载顶到 40（18 核）之上，还出现过两个会话/agent 同时各跑一套。LESSONS「One full suite runs on a machine at a time」早说过「需要一个所有套件都经过的锁」，但仓库里一直没有。
- 怎么验证：负载门槛设 0.5 时脚本只等待不开跑；第一套运行时第二套 `GUI_SUITE_NOWAIT=1` 退出 75；运行中 node 与 playwright 浏览器进程 nice=15；结束（含被信号杀掉）锁释放。
- 补充（同日，同机 Neox 会话 ai-34 协调）：并发默认改为 min(2, 核数/4)，负载门槛 min(核数, 20) 且要连续 3 次（间隔 30 秒）低于门槛才开跑——只看一眼时，在负载瞬间降到 16.6 的时刻开跑，随后被叠到 72。`--heap-prof-interval` 是 Node 26 测试运行器转发的默认参数，没有开启堆分析、也不写分析文件，不是开销来源。结束后检查的是本次运行自己的进程树（脚本退出时按 PID 结束它起的整棵树），不是按名字数 `ms-playwright`。

## 清理浏览器只按自己的 PID，绝不按名字批量结束（2026-11-16，同机 Neox 会话 ai-34）

- 规则：清理测试留下的浏览器或测试进程，只结束自己起的进程树（记下的 PID，`pgrep -P` 递归）。禁止 `pkill -f ms-playwright`、`killall Chromium` 或按路径匹配批量结束；检查残留也按自己的进程树查，不按名字数。
- 原因：同一台机器上别的会话也在跑 Playwright。我 13:0x 两次 `pkill -f ms-playwright` 把对方正在用的测试浏览器杀掉，对方的测试连接被意外关闭。
- 怎么验证：`scripts/gui-suite.sh` 退出时（正常结束、Ctrl-C、被杀）先给 node 发 SIGINT 等它收尾，再结束自己进程树里剩下的；中途 SIGINT 后锁释放、自己的树为空。

## 脱敏召回率只认没调过规则的集合（2026-11-19，用户要求脱敏成功率 95%）

- 规则：报告脱敏召回率时，用规则写完后才第一次跑的合成集合，跑一次就报这个数；拿它调过规则，它就只是回归测试。还要在 v0.2.27（或上一个发布版）上跑同一份集合做对照，并用公开代码（Go 模块缓存）逐条比较新旧代码新增的命中，判断误遮。
- 原因：四份集合，调参后都是 97%–100%，可每份新集合第一次跑分别是 81.9%、64.8%、90.8%。规则只覆盖见过的上下文，按调参集报出的数会高出十几到三十多个点。加权总数还会被占比最大的类别（Token 73%）拉着走，所以必须分类报，口令最弱（新集合第一次跑 49%）。
- 另记：带 `fold` 的规则，匹配窗口从标记所在行的行首开始；规则的开头若在上一行（expect 脚本里 `send` 上一行的 `expect`），fold 要设在开头的那个词上。

## 每个提交带一行中文更新说明（2026-11-19，用户：「更新的内容，根据 owl 设置的语言环境来显示，不要默认就是英文」）

- 规则：会进 Release 的提交，在提交说明末尾加一行 `Notes-zh: <给用户看的中文说明>`。发版时 `scripts/release_notes.py` 用提交标题拼英文、用这一行拼 `<!-- lang:zh -->` 下面的中文，Owl 按界面语言显示。没写这一行的提交，在中文说明里只能显示英文标题。
- 原因：程序一直支持按语言显示说明（`internal/update/notes.go` 的 `InLang`），但中文要靠上游另一个仓库的翻译步骤补上，fork 时没带过来，所以 v0.2.27 及以前，中文界面看到的都是英文说明。
- 怎么验证：发版后用 `update.LatestIn(ctx, "zh")` 和 `"en"` 各对线上取一次；或者直接看 `releases/latest/download/latest.json` 的 notes 里有没有标记。日文和德文界面目前显示英文（脚本里记了 debt）。

## 验证先证明测的是哪份代码（2026-11-20，深度验证构建错了 worktree）

- 规则：交给验证 agent 的被测版本，给出确切目录，并要求构建前先过身份检查：分支名、未提交改动数、与基线的差异文件数，构建后再用只有新版才有的接口行为取一次指纹；报告首行写明。两边零差异时，先怀疑两边是不是同一份代码。
- 原因：深度验证按「分支名去找」时拿到了一个早已废弃的旧 worktree（HEAD 3cf21bcf9，代码等同 v0.2.27），跑了三个多小时，得出「零未知差异」。它其实是在拿 v0.2.27 和自己比。它报告里写了「资产与基线逐字节相同、后端源码与基线相同」，这本身就说明测错了对象，却被当成了结论。

## GUI 测试报「浏览器不存在」先是环境，不是代码红（2026-11-20，v0.2.30 验证）

- 规则：`browserType.launch: Executable doesn't exist at .../ms-playwright/chromium-NNNN` 是 NODE_PATH 里那份 Playwright 与 `~/Library/Caches/ms-playwright` 对不上。按 `internal/gui/tests/README.md` 跑 `playwright install chromium webkit`，再判断红绿；不要把这种失败记成「测试红」或「改坏了」。
- 原因：本机 Playwright 随 Neo-agent 升到 1.65.1（要 chromium-1255、webkit-2388），缓存里只有旧的 1234/2248。七个文件 128 项里 68 项因此变红，其中一批还按「环境问题」被误读成代码回归，白花了一轮排查。
- 怎么验证：装完再跑同一组文件，红项应从 68 降到只剩代码或既有红项。

## 断言弹窗内容要等它画完，不是等盒子出现（2026-11-20，首次启动导入测试中文偶发红）

- 规则：对话框先以 loading 态出现（这里是 `.editor.importapps` 里的「Reading other apps…」），`waitFor({state:"visible"})` 之后立刻读 `textContent()` 会读到 loading 文案。要么等真正内容出现（等待某个只可能来自加载完毕的字符串或行），要么等 loading 节点被替换。
- 原因：`first-run-guide` 在 en 下碰巧通过，zh/zh-TW/ja/de 全部读到 loading 文案而红；同一份测试在另一台机器连跑两次都绿，差点被当成偶发负载。
- 怎么验证：把等待条件退回「可见即读」，zh 下十次里十次红；换成等内容后两引擎五种语言全绿。

## 全局状态刷新不承担整份用量汇总，数据源先实测再选（2026-11-20，协调者退回 Agent 卡片 v1）

- 规则：给 `/api/state` 这类「几乎每次点击都读」的接口加字段时，字段的来源必须是内存里现成的（网关最近调用环、启动或其它页面已经算好并发布的小表），或由页面自己的既有读取提供；不允许在 state() 里调用 `usage.SummarizeTop` 一类整份汇总——即使命中缓存也不是轻量的（先读日志快照、按全部供应商目录算定价哈希、克隆所有列表）。若必须新增读取，先在真实规模的历史上量冷/热成本，把命令与数字写进提交说明；需要新路由或架构取舍时先交替代方案，不直接写。
- 原因：v1 在 state() 里每次读今日汇总。40 万行日志、今日 2.6 万次调用时，SummarizeTop 冷 750ms、热 1.2ms；而 state() 本身热态约 6ms。改为只读网关环 + `usage.LastCalls()`（ReadAhead 与 Usage 页已经算好的汇总顺手发布）后，agentLives 热态 0.13ms（3 个 agent）、无记录 0.4µs。
- 怎么验证：临时测量测试打印 state() 与 agentLives 的冷/热耗时（空日志、单条、40 万行/2.6 万今日）；`SummaryReads()` 计数在 state() 前后不变。红证：在 agentLives 里加回 SummarizeTop，`TestAgentLiveFromTheLastCall` 报 "state() read 1 summaries"。
- 同一教训的语义部分：「最近一次实际命中的供应商」只算 owl 真正送到供应商的请求——客户端自报的（reported）、owl 自己用 key 跑的搜索（SourceSearch）、标题生成（Provider "owl"）、以及在任何供应商被询问前就被拒的请求都不算；到达供应商后失败的请求算（它确实命中了）。直连/导入的会话用量不进 topSummary，也不会被当成经 owl 的请求。请求身份用 `Record.RouteID`/`Call.Route`，时间与用量记录同一瞬间；对应的测试是 `TestAgentLastCallSkipsWhatOwlDidNotCarry`、`TestAgentLiveSkipsWhatOwlDidNotCarry` 和 `TestAgentLiveFromTheLastCall`。
