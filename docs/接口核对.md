# 果果剧库 · 客户端接口核对与技术选型

> 依据：`/workspace/hongguo` 源码（`internal/app/*.go` 路由注册见 `ui_server.go:routes()`、`ui_playback.go:registerPlaybackRoutes`），以及《客户端重设计方案.md》与设计图。
> 标记：✅ 已满足　🟡 需小改　❌ 缺失。“改造后”一栏是 `/workspace/hongguo_client` 中已经完成的处理。

---

## 一、逐页接口核对

### 0. 全局前提（所有页面）

| 需求 | 现有接口 / 代码 | 结论 | 改造后 |
| --- | --- | --- | --- |
| 本机使用者无需登录即可改设置、选目录、FFmpeg、管理员接口 | `account_admin.go:accountAdminPath` 要求 `account.Admin`；桌面匿名身份 `attachDesktopViewer` 不是管理员；管理员随机密码只打印在控制台 | ❌ | 已补：客户端模式注入 **本机所有者** 上下文（`client_mode.go:withClientOwner`），`withAccountAccess` 对其视为管理员，关闭“必须登录 / 仅在线 / 首次改密”；写操作仍要求 `X-Juku-Viewer`（CSRF） |
| 首次不 bootstrap 管理员、不打印密码 | `ui_viewer.go:prepareBrowserViewers` | 🟡 | 已改：`clientMode` 时跳过 |
| 观看身份（历史、追剧按身份存） | `/api/ui/viewer` + `desktop-viewer` 固定身份 | ✅ | 复用 `attachDesktopViewer` |
| 数据目录在用户目录 | `paths.go:preparePortableWorkingDirectory` 切到程序目录 | ❌ | 已改：`-client` 时数据目录 = `os.UserConfigDir()/果果剧库`，并 chdir 到该目录（`bin/` FFmpeg 随之落入数据目录）；首次运行默认下载目录 `~/Movies/果果剧库`（macOS）/ `%USERPROFILE%\Videos\果果剧库`（Windows）；已有配置不覆盖 |
| 侧栏状态（站源 / FFmpeg / 剧库数 / 进行中任务） | 分散在 `/api/ui/ffmpeg`、`/api/ui/dramas`、`/api/ui/tasks` | 🟡 | 已补：`GET /api/desktop/status`（数据目录、下载目录、FFmpeg、剧库数量、进行中下载 / 合并数，也供 Electron 退出确认、防休眠使用） |
| 本地鉴权（防其他本机程序 / 网页调用） | 无（HTTP 版靠账号） | ❌ | 已补：仅监听 `127.0.0.1`，启动令牌（环境变量传入、读后清除）→ HttpOnly SameSite=Strict Cookie / `X-Juku-Token`；校验 Host 防 DNS 重绑定 |
| 应用内通知事件源 | 无 | ❌ | 未做（阶段 2）：建议 `/api/desktop/events`（SSE）。当前前端用 toast + 通知面板，Electron 主进程轮询 status 发“任务全部完成”系统通知 |

### 1. 首页（home.png）

| 区块 | 所需数据 / 操作 | 现有接口 | 结论 | 改造后 |
| --- | --- | --- | --- | --- |
| 轮播 | 总热播榜前 5：剧名、封面、简介、类型、集数、完结状态 | `GET /api/ui/rankings?board=hongguo-hot&page=1` | ✅ | 直接使用 |
| 继续观看 | 历史 + 封面 + 第 k/总集 + 进度；点海报续播 | `/api/ui/playback/history`（**无封面**）+ `/api/ui/playback/open {resume,fromHistory}` | 🟡 | 已补聚合接口 `GET /api/desktop/home`：历史合并剧库封面、分类、本地标记 |
| 追剧有新集 | 追剧条目 + 新增集数 + 封面 | `/api/ui/following`（有 `newEpisodes`，**无封面**） | 🟡 | `/api/desktop/home` 的 `following[]`/`followingNew` |
| 榜单行（四榜） | 名次、热度指标 | `/api/ui/rankings?board=hongguo-hot/real/comic/ai` | ✅ | |
| 分类推荐行（真人 / 漫剧 / AI，翻页） | | `POST /api/ui/recommendations {genre,offset,sessionId,seen}` | ✅ | |
| 本地已下载 | 有完成文件的剧 | `/api/desktop/catalog` 的 `local` | ✅ | 也并入 `/api/desktop/home.local` |
| 卡片“＋追剧 / ↓下载” | | `POST /api/ui/following {dramaId,saved}`、`POST /api/ui/download {ids,quality}` | ✅ | |
| 首页聚合（减少首屏请求） | | — | ❌ | 已补 `/api/desktop/home`（只读本地数据，不访问网络；榜单与推荐仍并行请求原接口） |

### 2. 分类与搜索（browse.png）

| 需求 | 现有接口 | 结论 | 说明 / 改造后 |
| --- | --- | --- | --- |
| 类型（全部 / 真人 / 漫剧 / AI） | `/api/ui/categories?source=hongguo`（仅这 4 项） | ✅ | |
| 题材筛选 | 无专门接口；条目自带 `tags` | 🟡 | 前端从剧库 `tags` 统计前 18 个题材；后端无“题材字典” |
| 状态（完结 / 连载） | 条目 `releaseStatus` | ✅ | 前端筛选 |
| 排序（热度 / 最新 / 播放量 / 名称） | 条目 `heat/onlineDate/views` | ✅ | 前端排序 |
| 全量剧库 | `GET /api/ui/dramas`（一次返回全部 + `revision` 增量） | ✅ | 前端无限滚动渲染（每次 +60） |
| 服务端分页 / 筛选 | `/api/desktop/catalog` 只有 `q` | 🟡 | 未改。剧库数千条时一次性返回可接受；上万条时建议给 catalog 加 `type/tag/status/sort/offset/limit` |
| 搜索联想 | `GET /api/ui/search/suggestions?q=`（返回 `name`） | ✅ | 300ms 防抖、输入法合成期间不请求 |
| 联网搜索 | `GET /api/ui/search?q=`（另有 `stream=1` 流式） | ✅ | 现用非流式；流式结果可在后续接入 |
| 分类筛选隐藏联网结果提示 | 前端逻辑 | ✅ | 已实现 |

### 3. 详情 / 选集（detail.png）

| 需求 | 现有接口 | 结论 | 改造后 |
| --- | --- | --- | --- |
| 资料（简介、热度、集数、上线、标签、完结） | 只能从 `/api/ui/dramas` 全量里找；`/api/desktop/drama` 仅返回卡片字段 | 🟡 | `/api/desktop/episodes` 返回 `drama`（含 intro/heat/views/onlineDate/tags/releaseStatus/vip） |
| 打开即补资料 | `POST /api/ui/dramas/refresh {dramaId}`（5 分钟冷却） | ✅ | 前端打开详情即调用，4 秒后静默刷新 |
| **在线选集列表** | 只在 `playback/open` 建会话时返回；`/api/desktop/drama` 只有本地分集 | ❌ | 已补 **`GET /api/desktop/episodes?id=[&refresh=1]`**：封装 `GetDramaChapters`（5 分钟内存缓存），合并本地完成任务（`desktopLocalEpisodesLocked`）、VIP、观看进度；在线失败时回退本地分集并给出 `onlineError` |
| 已看 / 当前 / 本地 / VIP 四态 | 历史只记“最后一集+位置” | 🟡 | `current` 精确；`watched` 按“当前集之前视为已看”推算（界面有说明）。要逐集精确需扩展历史存储（未做） |
| 主按钮：开始 / 继续第 N 集 · 时间 / 重新观看 | 历史 | ✅ | `episodes.progress` |
| 追剧 / 已追剧 | `/api/ui/following` | ✅ | `episodes.following` |
| 下载全剧 | `POST /api/ui/download {ids:[dramaId]}` | ✅ | |
| **多选分集批量下载** | `/api/ui/download` 只接受剧 ID | ❌ | 未做：需新增按 chapterId 入队的接口（`enqueueDramasAsync` 需支持分集过滤） |
| 标为已看 | `POST /api/ui/following {completed:true}` | ✅ | |
| 同类推荐 | `/api/ui/recommendations` | ✅ | 按 `short_play`，不是个性化 |
| 导出 Emby | `/api/emby/export` | ✅ | 客户端隐藏（高级） |

### 4. 播放器（player.png）

| 需求 | 现有接口 | 结论 | 改造后 |
| --- | --- | --- | --- |
| 开会话 / 续播 | `POST /api/ui/playback/open {dramaId|taskId,resume,fromHistory}` | ✅ | |
| 播放计划 / 能力协商 | `POST /api/ui/playback/plan {episode,start,quality,version,mode,client}` | ✅ | 前端按 MSE 实测上报 h264/hevc/av1 |
| HLS 回退 | `POST /api/ui/playback/hls/open` + `hls/index.m3u8` + `hls/segment.ts` | ✅ | 注意：服务端要求每次请求 `version` **单调递增**，否则 409（已按此实现） |
| 原始 MP4 / Range | `/api/ui/playback/media/...` | ✅ | 实测 `Range: bytes=0-1023` → 206 |
| 合集（本地）播放 | `prepare` + `taskId` | ✅ | 下载页“播放” |
| 会话保活 | `POST /api/ui/playback/control {action:"heartbeat"}` | ✅ | 20 秒心跳 + 回到前台立即心跳 |
| 进度记录 | `POST /api/ui/playback/progress` | ✅ | 5 秒 + 暂停 / 切集 / 关闭时补记 |
| 关闭释放 | `control {action:"close"}` | ✅ | |
| 画质 | plan 返回 `qualities` | ✅ | 只有一档时隐藏 |
| 弹幕 | `GET /api/ui/playback/danmaku?session&episode&start&duration` | ✅ | 60 秒窗口加载 |
| 预缓存下一集 | `POST /api/ui/playback/prefetch` | 🟡 | 已调用，但目前返回 409（参数语义与 run/version 绑定，需对照 `player-*.js` 再校准；失败不影响播放） |
| 缓存秒数 / 状态 | `/api/ui/playback/status` | ✅ | 前端暂用 `video.buffered` 显示 |
| 倍速、长按 3×、N/P、D、F、Esc、PiP、Media Session | 前端 | ✅ | 已实现 |

### 5. 追剧 / 观看历史（following.png）

| 需求 | 现有接口 | 结论 | 改造后 |
| --- | --- | --- | --- |
| 追剧列表（想看 / 在看 / 已看 / 新集） | `/api/ui/following`（无封面） | 🟡 | 用 `/api/desktop/home?full=1` 的 `following[]`（带封面）+ 历史判断在看 |
| 全部标为已读 | `POST /api/ui/following {acknowledge:true}`（逐部） | 🟡 | 前端逐部调用；可加批量接口（未做） |
| 取消追剧 / 标为已看 | `/api/ui/following {saved:false}/{completed:true}` | ✅ | |
| 历史按日期分组、封面 | `/api/ui/playback/history`（无封面） | 🟡 | `/api/desktop/home?full=1` 的 `continue[]`（含已看完、带封面） |
| 删除 / 清空历史 | `POST /api/ui/playback/history/remove {dramaId}|{all:true}` | ✅ | 清空二次确认 |

### 6. 下载管理（downloads.png）

| 需求 | 现有接口 | 结论 | 说明 |
| --- | --- | --- | --- |
| 任务列表（分集级）+ 合并状态 + FFmpeg | `GET /api/ui/tasks` → `data[]`、`merges{}`、`ffmpeg` | ✅ | 前端按剧分组，1.5 秒轮询 |
| 暂停 / 继续 / 取消 | `POST /api/ui/tasks/{pause,resume,cancel} {ids|dramaIds}` | ✅ | |
| 重试失败分集 | `POST /api/ui/tasks/retry` | ✅ | |
| 合并 / 取消合并 | `POST /api/ui/merge {dramaIds,deleteEpisodes,async}`、`/merge/cancel` | ✅ | |
| 更新本剧（补集） | `POST /api/ui/update {ids}` | ✅ | |
| 清理记录（保留视频） | `POST /api/ui/tasks/clear {ids}` | ✅ | |
| 播放全集 | `playback/open {taskId}` | ✅ | |
| 打开文件夹 | 无（服务端不应替客户端开窗） | 🟡 | Electron `shell.showItemInFolder`（preload `juku.showItemInFolder`） |
| 下载目录 / 并发 | `GET/POST /api/ui/config` | 🟡 | 现在 owner 可改；**改目录仍需重启生效**（`restartRequired`），即时生效需改 `download_directories.go`（未做） |
| 磁盘可用空间条 | 无 | ❌ | 未做：需新增读取卷可用空间的接口 |
| 新任务画质 | `/api/ui/download {quality}` | ✅ | 页面工具条未放画质选择（后续） |

### 7. 设置（settings.png）

| 分组 | 现有接口 | 结论 | 说明 |
| --- | --- | --- | --- |
| 下载与存储（目录、并发、按站源分类） | `/api/ui/config` | ✅（owner 后） | 目录用 Electron 原生对话框，回退 `/api/ui/directory/pick` |
| 网络与代理（代理、并发、间隔、检测） | `/api/ui/config {proxyURL,requestConcurrency,requestIntervalMs}`、`POST /api/ui/network/check` | ✅ | |
| FFmpeg（状态、路径、进度、重新检查） | `GET/POST /api/ui/ffmpeg` | ✅ | |
| 播放（自动下一集、弹幕默认） | 前端偏好 | ✅ | `/api/ui/preferences` 可做跨设备同步（未接） |
| 播放高级（会话 / 转码并发） | `/api/ui/admin/playback` | ✅ | 未放进界面 |
| 通用（关闭窗口行为） | — | 🟡 | Electron 设置（`shell-settings.json`） |
| 数据与备份（打开数据目录、迁移包） | `/api/ui/sync/package` | 🟡 | 打开目录已做；迁移包导入导出界面未做 |
| 关于与更新 | — | ❌→🟡 | Electron `electron-updater`（需配置发布地址与签名） |
| Emby（高级） | `/api/ui/admin/emby*` | ✅ | 客户端模式**不自动启动 Emby 同步**，界面未接（阶段 4） |

### 8. 首次启动引导

| 步骤 | 现有接口 | 结论 |
| --- | --- | --- |
| 欢迎 / 版权提示 | — | ✅ 前端 |
| 选择下载位置 | `/api/ui/config` + 原生目录选择 | ✅（owner 后） |
| 准备 FFmpeg（带进度） | `GET /api/ui/ffmpeg`（`downloadedBytes/totalBytes`）、`POST` 重试 | ✅ |
| 检测网络 / 代理 | `POST /api/ui/network/check` | ✅ |
| 导入旧便携数据 | `config_store.go:importLegacyConfig`（仅首次、读 cwd 的 config.json） | 🟡 未做界面：需新增“从指定目录导入旧 data/”接口 |

### 结论汇总

- **已满足（直接复用）**：榜单、分类推荐、联想与联网搜索、剧库全量、追剧增删改、历史删除、全套播放协议（open/plan/hls/media Range/heartbeat/progress/danmaku/close）、下载任务全套操作、合并、补集、FFmpeg、网络检测、配置读写。
- **需小改（已在客户端完成）**：管理员接口对本机放行（本机所有者）、跳过管理员 bootstrap、数据目录迁移、首页/历史/追剧缺封面（聚合接口）、Emby 不自动启动、目录选择换原生。
- **缺失 → 已新增**：`/api/desktop/episodes`、`/api/desktop/home`、`/api/desktop/status`、客户端令牌鉴权与 Host 校验。
- **仍缺失（未做）**：分集级批量下载、事件流 SSE、磁盘空间、改目录即时生效、逐集“已看”记录、旧数据导入界面、catalog 服务端分页筛选、批量“标为已读”、prefetch 参数校准。

---

## 二、技术选型：Wails v2 / Wails v3 / Electron

前提：用户唯一硬要求是**稳定性**。

| 维度 | Wails v2.11（现状） | Wails v3（alpha） | Electron + Go 子进程（127.0.0.1 随机端口 + 令牌） |
| --- | --- | --- | --- |
| 视频流稳定性 | 媒体走 AssetServer `Handler`：WKWebView 上通过 `WKURLSchemeHandler` 转发，**Range / 长连接 / MSE 分片并发**依赖 Wails 自己的桥接实现；方案文档已列为阶段 0 头号风险；WebView2 需另验。HEVC：WKWebView 原生支持，WebView2 依赖系统扩展 | 同样基于系统 WebView + 自定义 scheme，资产服务器重写过但仍在 alpha，行为可能随版本变化 | 页面与媒体都走**真实 HTTP**（Go `net/http` 原生 Range、206、流式、取消），Chromium 媒体栈 + hls.js/MSE 在 mac/Win **同一内核、同一行为**；本次已实测 HLS 分片 206、MP4 Range 206、hls.js 播放推进。HEVC：Chromium 在 macOS 走 VideoToolbox、Windows 走 D3D11 硬解（Electron ≥ 22 支持，需硬件），不支持时后端已有 remux/转码回退 |
| 进程隔离 / 崩溃恢复 | Go 与 WebView 同进程，任一崩溃全挂 | 同进程 | 主进程 / 渲染进程 / Go 后端**三者隔离**：后端崩溃自动重启（已实现，5 分钟 3 次退避）；渲染崩溃自动 reload；Electron 退出或崩溃时后端读到 stdin EOF 自行退出（已实测，无孤儿进程） |
| 托盘 / 通知 / 菜单 | 无托盘、无通知；需接第三方 systray（macOS 主线程冲突） | 内置 tray、通知、多窗口 | 内置 `Tray`、`Notification`、`Menu`、`powerSaveBlocker`、`globalShortcut`，成熟多年 |
| 自动更新 | 无，需自建 | 无内置（规划中） | `electron-updater`：差分更新、签名校验、mac zip / Win NSIS，生态成熟 |
| 打包签名公证 | `wails build` + 手工 codesign/notarytool；NSIS 模板 | 新打包系统，仍在演进 | `electron-builder` 一条命令完成 mac 签名 + 公证 + dmg/zip，Win NSIS + 代码签名；需额外对 Go 后端二进制签名（已在配置 `mac.binaries` 中列出） |
| 体积 | 最小（约 15–25 MB） | 同左 | 大：Electron 运行时约 90–110 MB（安装包 dmg 压缩后约 90 MB，universal 约 180 MB；Win 约 80 MB）+ Go 后端约 10 MB |
| 内存 | 低（系统 WebView） | 低 | 较高（多一个 Chromium，空闲约 150–250 MB） |
| 开发体验 | Go 绑定、单二进制 | 同左，API 不稳定 | 前端就是普通 Web（Vite 开发服务器 + 代理即可），后端可独立 `-client` 运行调试；与“服务器版”共用同一套 HTTP 接口 |
| 成熟度 | 稳定但功能少 | **alpha**，不建议用于“稳定性优先”的产品 | 最成熟（VS Code、Slack、Discord 等） |

### 推荐：Electron + Go 后端子进程

理由：
1. **稳定性最高的一点在视频链路**：媒体请求走真实 loopback HTTP，Go 端 Range/流式是标准库行为，不经过 WebView 的自定义协议桥；而且 mac 与 Windows 使用同一 Chromium 内核，解码与 MSE 行为一致，测试一次覆盖两端。Wails 方案最大的风险恰好在这里（方案文档原本的备选就是“给媒体单开 127.0.0.1 端口”，Electron 方案直接把它变成主路径）。
2. **故障隔离**：后端、渲染、主进程互相隔离，可以单独重启；Wails 同进程，任一崩溃都会关掉整个应用。
3. 托盘、系统通知、自动更新、签名公证都是现成能力，阶段 3 的工作量和不确定性大幅降低；Wails v3 仍是 alpha，不符合“稳定优先”。
4. 后端改动最小：客户端模式就是 HTTP 服务器版加上 loopback、令牌和本机所有者，**服务器版功能完全保留**。

代价与对策：体积和内存更大（可接受，属于视频类客户端的常见量级）；需要处理本地端口安全（已做：只绑 127.0.0.1、启动令牌、HttpOnly SameSite=Strict Cookie、Host 校验防 DNS 重绑定、Electron 只允许导航到本机 origin、`contextIsolation + sandbox`）；页面 origin 随端口变化会丢 localStorage（已做：Electron 记住上次端口，后端优先复用，`-client-port`）。

Wails 壳（`desktop_wails.go`）未删除，可继续作为轻量版本或回退方案。

---

## 三、改造实施记录（`/workspace/hongguo_client`）

提交（`git log --oneline`）见汇报。主要文件：

- 后端：`internal/app/client_mode.go`（客户端模式：目录、令牌、Host 校验、本机所有者、静态前端、优雅退出）、`internal/app/desktop_client_api.go`（`/api/desktop/episodes`、`/home`、`/status`）、`cli.go`（`-client`、`-client-ui`、`-client-port`、`-client-watch-stdin`）、`account_admin.go`（owner 放行）、`ui_viewer.go`（跳过管理员 bootstrap；新接口挂观看身份）。
- 前端：`frontend/src/`（`App.jsx` 外壳、`pages/` 下 Home / Browse / Rankings / Detail / Player / Following / Downloads / Settings / Onboarding）。
- Electron：`electron/main.js`、`backend.js`、`preload.js`、`package.json`（electron-builder 配置）、`scripts/build-backend.js`、`build/entitlements.mac.plist`。

### 运行方式

```bash
# 后端单独运行（开发）
cd frontend && npm ci && npm run build && cd ..
JUKU_CLIENT_TOKEN=$(openssl rand -hex 32) go run . -client -client-ui frontend/dist -client-watch-stdin=false
# 浏览器打开 http://127.0.0.1:<端口>/?token=<令牌>

# Electron（Mac）
cd electron && npm install && node scripts/build-backend.js darwin && npm start
# 打包：设置 CSC_LINK/CSC_KEY_PASSWORD、APPLE_ID/APPLE_APP_SPECIFIC_PASSWORD/APPLE_TEAM_ID 后 npm run dist:mac
```
