# Byte TV：Electron → Go + Wails 迁移评估

> 2026-10-07 · 只做评估，**没有改动主线代码**。PoC 放在 `/workspace/wails3_poc`（独立的 Go module，通过 `replace juku => ../hongguo_client` 直接引用主线后端）。

## 0. 结论速览

| 项 | 结论 |
|---|---|
| 推荐 | **Wails v3（beta），并采用「页面与视频都走本机 127.0.0.1 端口」架构**。同时把 Electron 版保留一个版本周期作为回退 |
| 体积 | PoC 实测：Linux 单个二进制 **16.3 MB**（已包含后端、前端和 Wails）。Electron 现状：仅 Electron 运行时解压后就有 263 MB，再加 10.5 MB 后端；mac 安装包约 100 MB 量级 |
| 内存 | PoC 实测（Linux/WebKitGTK，首屏）：主进程 210 MB（含整个后端）+ WebKit 网络进程 72 MB + Web 进程 324 MB。和 Electron 同一量级，**主要收益在体积，不在内存** |
| 最大风险 | ① macOS 上 WKWebView 原生 HLS（AVFoundation）发起的请求可能不带 HttpOnly Cookie，导致分片 401，需要改成「播放会话 URL 自带凭据」；② 同进程运行后没有崩溃自动重启；③ v3 仍是 beta 且每晚自动发版 |
| 工时 | 约 **9–13 人日**（不含 Apple / Windows 证书申请的等待时间） |

## 1. 版本与稳定度

- **Wails v3**：最新为 **v3.0.0-beta.28（2026-10-05）**。
  - 2026-08-02 从 alpha2.122 升到 beta（beta.2）。
  - 官方表述是「API 已稳定，但正式 3.0 前仍可能遇到问题」。
  - 版本是 master 分支的**自动每晚发版**，两个月里出了 27 个 beta。
  - 近期修复集中在三块：
    - WebView2 恢复/崩溃（beta.17、26、28）；
    - macOS 主线程与菜单崩溃（beta.6、13）；
    - Linux 托盘和 GTK4。
  - 说明桌面端核心路径还在收敛，但修复速度很快。
  - 需要 **Go 1.25**。主线 go.mod 是 1.24.1，迁移时需要升级工具链；PoC 用 `GOTOOLCHAIN=go1.25.0+auto` 自动下载。
  - Linux 默认是 GTK4 + WebKitGTK 6.0，用 `-tags gtk3` 可退回 WebKit2GTK 4.1（PoC 用的就是后者）。
- **Wails v2**：你提到的 v2.11 并不是最新版。git tag 显示**最新为 v2.16.0**（go.mod 同样要求 Go 1.25）。v2 已进入维护期，结构是单窗口 + 绑定；托盘、通知、单实例等能力不如 v3 完整。
- **已知问题**中和我们相关的（来自 changelog）：
  - Windows：WebView2 在最小化或跨 DPI 显示器拖动后进程被回收 → 白屏或崩溃，beta.26 / 28 已加自动恢复；
  - macOS：替换应用菜单时崩溃（beta.6 已修）；
  - Linux：WebKit 资源请求在非主线程完成导致 SIGSEGV（alpha2.105 已修）。
  - 这三类问题**都和自定义协议 / AssetServer 有关**，也印证了视频不走自定义协议的思路。

## 2. 视频播放方案（最关键）

### 2.1 架构

- 主线后端已有 `-client` 模式：只监听 127.0.0.1，端口优先用上次记住的、否则随机；启动令牌通过 `/?token=` 换成 HttpOnly Cookie；做 Host 校验防 DNS 重绑定。
- 迁移后不再起子进程，改为**在 Wails 进程内启动同一个 HTTP 服务**。WebView 直接 `URL: http://127.0.0.1:<port>/?token=...`（PoC 就是这样做的，已跑通，见 `poc-screenshot.png`）。
- **页面、API、HLS / MP4 全部走本机端口**，Wails 自定义协议（`wails://` / `http://wails.localhost`）完全不参与。这样：
  - Range 请求、长连接、流式响应都由 Go 的 net/http 原生处理，不受 AssetServer 的 64 MiB 限制和线程问题影响；
  - 后端代码和现在的 Electron 版、网页版完全一致。
- **代价**：页面不在 Wails 的源下，Wails 的 JS 运行时和绑定（`@wailsio/runtime`）在该页面里**不可用**。原生能力改为由后端新增 `/api/desktop/native/*` 接口提供：因为同进程，Go 可以直接调用 `app.Dialog`、`app.OpenFileManager`、窗口 API。前端 `window.juku` 的形态保持不变，只是把 ipcRenderer 换成 fetch（见第 4 节）。
- **不推荐的备选**：页面走 Wails 源，只有视频/API 走 127.0.0.1 端口。这样会变成跨源，需要 CORS + 不带 Cookie 的鉴权；而且 WKWebView 中 `wails://` 源访问 http 本机地址有混合内容和 Cookie 隔离问题，复杂度更高。

### 2.2 WebView 能力对比

| 能力 | macOS WKWebView | Windows WebView2 (Chromium) | 对我们的影响 |
|---|---|---|---|
| hls.js（MSE） | macOS 上 **MSE 可用**（caniwebview：macOS 全版本支持 MediaSource，iOS 13+ 才有）；ManagedMediaSource 在 macOS WKWebView **标为不支持** | 支持 | 现有播放器**优先用原生 HLS**（`canPlayType("application/vnd.apple.mpegurl")` 为真时直接 `video.src = m3u8`），在 mac 上实际会走 AVFoundation，hls.js 只作为兜底 |
| 原生 HLS 回退 | 支持（AVFoundation） | 不支持（只能走 hls.js） | **风险点**：AVFoundation 发起的 m3u8 / 分片请求不经过 WebKit 的网络栈，**可能不带 HttpOnly Cookie** → 401。对策二选一：① 播放计划里的 URL 带上会话内一次性 token（推荐，`session=` 参数已经是不可猜测的值，鉴权中间件对 `/api/ui/playback/*` 校验 session 即可）；② mac 上强制走 hls.js（`Hls.isSupported()` 为真即可），代价是更费电 |
| MP4 Range | 支持（AVFoundation 依赖 206 / Range；Go `http.ServeContent` 已满足） | 支持 | 无需改动；但同样有原生媒体请求不带 Cookie 的问题，对策同上 |
| HEVC | 硬解支持（hvc1；hev1 标签可能失败） | 依赖系统「HEVC 视频扩展」；WebView2 新版对硬件 HEVC 支持有限，**不可依赖** | 现有 `capabilities()` 已经按 `MediaSource.isTypeSupported` / `canPlayType` 探测并上报，后端按能力选源，保持即可 |
| 画中画 | 支持 `requestPictureInPicture`（WKWebView 需 `allowsPictureInPictureMediaPlayback`，mac 默认开） | 支持 | 代码已用 `document.pictureInPictureEnabled` 做门控，无需改 |
| 全屏 | Element Fullscreen API：较新的 macOS 版本才在 WKWebView 中可用，需要开启 `elementFullscreenEnabled`；Wails v3 有 `Window.Fullscreen()` | 支持 | 保留 `window.juku.toggleFullScreen` → Go 调 `win.ToggleFullscreen()` 作为兜底（与 Electron 版一致） |
| Media Session | WKWebView 支持度弱，系统「正在播放」/ 媒体键不一定生效 | 支持（媒体键、SMTC） | 降级可接受，必要时用 Go 端监听媒体键 |
| 自动播放 | v3 有 `MacWebviewPreferences.EnableAutoplayWithoutUserAction`（beta.7） | 可用启动参数开启 | 用户点击触发播放，影响很小 |
| localStorage 持久化 | origin = `http://127.0.0.1:<port>`，**端口变化即换 origin，数据「丢失」** | 同左 | 后端已记住上次端口（`-client-port`），仅在端口被占用时才变。还应把播放偏好、主题、引导标记同步到后端 `juku.json`（`/api/desktop/prefs`），localStorage 只做缓存。Electron 版有同样的问题，不是新增风险 |
| Cookie（SameSite=Strict, HttpOnly） | WebKit 网络栈正常 | 正常 | 只有原生媒体请求例外，见上 |

### 2.3 必做的验证

PoC 在 Linux 上只验证了页面能加载、后端在同进程正常运行。**mac 与 Windows 的播放必须上真机验证**：
- 竖屏/横屏 HLS；
- 拖动进度；
- 切集；
- 3× 倍速；
- 画中画；
- 全屏；
- 本地 MP4 Range。

## 3. Electron 现有能力逐项对应

| Electron 能力（electron/） | Wails v3 | Wails v2 | 缺口与对策 |
|---|---|---|---|
| 托盘 + 菜单 + 任务数提示 | `app.SystemTray.New()`，支持 SetMenu / SetLabel / SetTooltip / Show / Hide | 无官方托盘（只能用第三方 systray 库） | v3 无缺口；mac 菜单栏图标需模板图 |
| 系统通知（任务完成） | 内置 notifications 服务（mac 需授权，Windows Toast） | 无 | v3 无缺口；mac 需签名后才能稳定弹出 |
| 单实例 | `SingleInstance` 选项（第二实例唤起已有窗口，可传参） | `SingleInstanceLock` | 无缺口 |
| 关窗留后台 | 窗口 `WindowClosing` hook → `Cancel()` + `Hide()`；mac 设 `ApplicationShouldTerminateAfterLastWindowClosed:false` | `HideWindowOnClose` | 无缺口 |
| 退出确认（有下载时） | `ShouldQuit` 回调里弹 `app.Dialog.Question()` | `OnBeforeClose` | 无缺口 |
| 菜单与快捷键 | 应用菜单 + 加速键；v3 还有 `KeyBinding` 和全局快捷键 | 应用菜单 + 加速键 | 「前往」菜单改为 Go 侧调用前端导航（通过 `/api/desktop/native/events` SSE 或 `ExecJS` 修改 `location.hash`） |
| 防休眠（下载中） | **无内置** | 无 | 自己实现：mac 用 cgo `IOPMAssertionCreateWithName` 或 `caffeinate -i -w <pid>`；Windows 用 `SetThreadExecutionState`；约 0.5 天 |
| 目录选择 | `app.Dialog.OpenFile().CanChooseDirectories(true)` | `runtime.OpenDirectoryDialog` | 无缺口 |
| 在访达 / 资源管理器中显示 | `app.OpenFileManager(path, true)` | 无（需 `open -R` / `explorer /select,`） | v3 无缺口 |
| 自动更新（electron-updater） | v3 自带 updater（Update Manifest 协议、GitHub 提供方，带失败回滚） | 无 | 需要自己定义发布清单和签名校验；mac 必须签名后替换 .app；约 1–1.5 天 |
| 后端崩溃自动重启（3 次 / 5 分钟） | **同进程后不再存在** | 同左 | 见下文 3.1 |
| 签名 / 公证 / 打包 | `wails3 package`：mac .app / DMG（含 notarize 任务）、Windows NSIS / MSIX、Linux deb / rpm / AppImage | `wails build` + NSIS；mac 公证需手工 | v3 更完整；**Windows 需注意 WebView2 Runtime**（Win11 自带，Win10 可内置 bootstrapper） |
| `platform`、版本号、设置读写（closeToTray） | Go 侧直接提供 | 同左 | 设置改存后端 `juku.json` 的 `desktop` 段 |
| mac 原生标题栏 `hiddenInset` / Windows 标题栏颜色 | `MacTitleBarHiddenInset`、Windows `Theme` / 自定义标题栏 | 类似 | 前端已有 `.drag` / `.win` 适配；**注意**：页面不在 Wails 源时，`--wails-draggable` 拖拽属性依赖 Wails 运行时，可能失效，需验证；不行就用原生标题栏 |

### 3.1 同进程后的稳定性（替代崩溃重启）

1. `net/http` 已经对每个请求的 panic 做了 recover，handler 里的 panic 不会让进程崩溃。
2. **自己起的 goroutine 必须包一层 recover**，例如下载 worker、合并、FFmpeg 准备、预取、站源刷新。panic 时记录诊断日志、把任务标为失败，并发通知。统一提供 `safeGo(name, fn)`，迁移时用 grep 逐个替换 `go func`。
3. 加看门狗：后端提供 `/api/desktop/health`（已有），Wails 侧每 10 秒自检一次；连续失败时用 `ln.Close()` + 重建 `clientServer` 做**软重启**，HTTP 层可以重来，状态在内存里的部分会丢失。
4. 致命错误（`fatal error: concurrent map writes`、cgo 段错误）无法 recover → 进程退出。启动时检查上次是否异常退出的标记，弹窗提供「查看日志 / 重新打开」，并上报诊断信息。
5. **可选的混合模式**：仍是同一个二进制，Wails 用 `os.Executable() -client ...` 把自己作为子进程拉起来跑后端，复用现有 `backend.js` 的重启逻辑，改成用 Go 实现。体积不变（仍是单个文件），保留崩溃隔离；代价是多一个进程，内存多约 30–50 MB。**建议先做同进程，把混合模式作为开关保留。**

## 4. 前端改动

- 现在的桥接是 `window.juku`（preload 暴露），方法有：`platform`、`pickDirectory`、`showItemInFolder`、`toggleFullScreen`、`getVersion`、`getSettings`、`setSettings`、`checkForUpdates`、`notify`、`onNavigate`。实际只有 `api.js`、`directory.js`、`Settings.jsx`、`Player.jsx` 四处引用（约 12 个调用点）。
- **按 2.1 的架构**（页面在 127.0.0.1 上）：
  - 新增一个约 60 行的 `src/native.js`：`window.juku` 不存在时，用 fetch 调 `/api/desktop/native/{pick-directory,show-item,fullscreen,version,settings,check-updates,notify}`，`onNavigate` 用 `EventSource('/api/desktop/native/events')` 实现。Electron 版继续用 preload，两种壳共存。
  - 平台通过 `/api/desktop/status` 返回的 `platform` 获取，Windows 标题栏适配逻辑不变。
  - 播放器：如果采用「mac 强制 hls.js」方案，只需改 1 行判断；如果采用「播放 URL 自带 token」方案，前端无需改动，改动在后端。
  - 偏好持久化（可选但建议做）：`playerPrefs` / theme / onboarded 增加后端同步，约 0.5 天。
- **如果改用 Wails 绑定**（页面放在 Wails 源）：需要引入 `@wailsio/runtime`、生成绑定、改写全部 API 的 base URL 并处理 CORS 鉴权，约 2–3 天，**不推荐**。
- 工作量：前端约 **1–1.5 人日**；后端 native 接口和 Wails 外壳约 3–4 人日（见第 6 节）。

## 5. 体积与内存（实测 + 估算）

| | Electron（现状） | Wails v3 PoC（实测） |
|---|---|---|
| 后端 | `juku-backend` 10.5 MB（strip 后 10.6 MB） | 编进同一个二进制 |
| 壳 | Electron 运行时：Linux 解压后 **263 MB**（node_modules/electron/dist）；mac .app 一般 230–260 MB（单架构），DMG / zip 压缩后约 90–110 MB | Wails + 前端 + 后端合计：**16.3 MB**（Linux amd64，`-tags gtk3 -trimpath -s -w`，embed 前端 0.86 MB） |
| 预计 mac 安装包 | ≈ 100 MB（单架构 DMG），universal ≈ 190 MB | .app ≈ 18–22 MB（单架构，含图标资源），universal ≈ 40 MB；DMG 压缩后约 **8–15 MB** |
| 预计 Windows | NSIS ≈ 85–100 MB | exe ≈ 17–20 MB，NSIS ≈ 7–10 MB（依赖系统 WebView2 Runtime；需要离线安装时 +170 MB） |
| 运行内存 | 未在本轮实测；Electron 一般是主进程 + GPU + Renderer + 后端子进程，空载约 300–450 MB | Linux 首屏实测：主进程 **210 MB**（含后端剧库缓存）+ WebKitNetworkProcess 72 MB + WebKitWebProcess 324 MB ≈ 606 MB（WebKitGTK 在 xvfb 下用软件渲染，偏高；mac WKWebView 一般更低） |

**解读**：体积约缩小到 **1/6 以内**，是这次迁移的主要收益。内存取决于各平台的 WebView，不会明显下降，播放时视频解码占大头，两者差不多。

PoC 复现方式见 `/workspace/wails3_poc/README.md`：
- 依赖：`libgtk-3-dev`、`libwebkit2gtk-4.1-dev`、Go 1.25；
- 构建：`go build -tags gtk3`；
- 测量：`xvfb-run ./run.sh`。

PoC 已实现：
- 同进程启动后端，并解析 READY 端口；
- 嵌入前端 dist；
- 托盘菜单（Linux 无 StatusNotifier 时会报错但不影响运行）；
- 单实例选项；
- 后端 goroutine recover 示例。

## 6. 推荐、风险与计划

### 推荐：Wails v3（beta.28 起，锁定具体版本，不跟每晚版本）

- **相比 v2**：托盘、通知、单实例、更新器、DMG / 公证、OpenFileManager 都是现成的，v2 需要自己补 4–5 项。v2 唯一的优势是「稳定版」，但它的 WebView 层与 v3 是同一套系统组件，播放风险两者相同。
- **相比继续 Electron**：Electron 的优势是 Chromium 一致性（MSE、HEVC、Media Session 在三端行为一致）和零迁移成本。Byte TV 的播放已经做了能力探测和原生 HLS 回退，WebView 差异可控，而包体收益显著。

### 主要风险

1. mac 原生 HLS / MP4 请求的 Cookie 问题 → 播放 URL 改用会话凭据（必须做）。
2. WKWebView 的元素全屏、Media Session 弱于 Chromium → 原生全屏兜底，媒体键可接受降级。
3. Windows 用户机器上 WebView2 Runtime 缺失或版本过旧（Win10 早期版本） → 安装器内置 bootstrapper，启动时检测。
4. Windows HEVC 不可依赖 → 后端按能力优先选 H.264（现有逻辑已支持）。
5. v3 仍是 beta、每晚发版 → 锁版本、关注 WebView2 恢复相关修复；升级前跑播放回归脚本。
6. 同进程后崩溃隔离消失 → `safeGo` + 看门狗 + 可选混合模式。
7. 端口变化导致 localStorage 丢失 → 偏好存到后端。
8. 页面不在 Wails 源时，拖拽区域与 Wails 运行时功能不可用 → 用原生标题栏或自绘拖拽区，需要真机验证。

### 分步计划（人日）

| 步 | 内容 | 工时 |
|---|---|---|
| 1 | 主线小改：后端导出 `StartClient(ctx, opts) (port, token, stop)`，不走 `os.Args` / stdout；新增 `/api/desktop/native/*` 接口的空实现；播放 URL 会话鉴权；`safeGo` 替换 | 2–3 |
| 2 | `desktop-wails/` 外壳：窗口、托盘、菜单与快捷键、单实例、关窗留后台、退出确认、通知、防休眠、目录选择、访达显示、看门狗 | 3–4 |
| 3 | 前端 `native.js` 兼容层 + 偏好后端同步 + 平台判断 | 1–1.5 |
| 4 | mac / Windows 真机播放回归（HLS、MP4、HEVC、PiP、全屏、倍速、切集、长时间播放内存） | 1.5–2 |
| 5 | 打包：mac universal DMG 签名公证、Windows NSIS + WebView2 bootstrapper、自动更新清单 | 1.5–2 |
| 6 | 并行发布一个版本（Electron 版保留），收集崩溃 / 播放反馈后下线 Electron | — |
| **合计** | | **约 9–13 人日** |

建议的第一个里程碑：先完成第 1、2 步的最小版本，在 mac 真机上跑通**播放回归**（即第 4 步的播放部分）。这一步大约 4 天，用来确认第 2 节的 Cookie 和全屏风险，再决定是否继续投入。

## 迁移状态（2026-10-07，分支 `wails3`）

已按本文方案完成迁移，Electron 目录保留作回退：

- 外壳：`cmd/bytetv`（Wails v3 `v3.0.0-beta.28`，Go 1.25），同进程运行后端；`internal/app/client_embed.go` 提供外壳钩子（`SetClientHost`、`ClientUIFS` 嵌入前端、`StopClient`、`ClientActivity`、`ClientEmit` SSE 事件、`desktop_prefs.json`、端口记忆 `client_port`、媒体令牌 `?bt=`、panic 恢复）。
- 页面无法使用 Wails 绑定（页面来自 `http://127.0.0.1:端口`，`WebviewWindow.ExecJS` 也会因运行时未加载而一直排队），原生能力统一走 `/api/desktop/native/*`，导航事件走 SSE `/api/desktop/native/events`。
- 播放：桌面外壳内有 MSE 时 HLS 强制 hls.js；原生 `<video>` 直连地址附带只读媒体令牌（仅 GET/HEAD 播放、媒体路径有效）。
- 打包：`Taskfile.yml`、`scripts/desktop/*.sh`、`packaging/`。
- Linux/xvfb 实测：hls.js（`blob:` 源）播放竖屏剧 1080×1920，6 秒内 currentTime 前进 6 秒；菜单“前往 → 下载”经 SSE 导航成功；端口复用与后端偏好持久化生效。
- 待 macOS / Windows 真机确认：见 README“Byte TV 桌面客户端”与提交说明。
