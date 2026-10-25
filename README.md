# Byte TV

桌面端短剧在线观看与下载客户端。Wails v3 外壳 + Go 后端 + React 前端运行在同一个进程内，只监听本机回环地址，无需单独启动服务。

只想看怎么用，直接跳到[安装与运行](#安装与运行)；想按界面找功能，看[界面导览](#界面导览)。

## 功能

| 能力 | 说明 |
| --- | --- |
| 在线播放 | 直接播放剧库里的剧集，无需先下载；自动续播上次位置 |
| 剧库检索 | 本地筛选 + 联网搜索，按类型、状态、题材、年份、排序多维度查看 |
| 榜单 | 总热播榜、真人剧榜、漫剧榜、AI 剧榜 |
| 追剧 | 想看 / 在看 / 已看三段式管理，新集提醒 |
| 观看历史 | 按进度续播，可搜索、单条删除、一键清空 |
| 下载与合并 | 批量下载、多档画质、断点信息保留、无损合并为整剧 |
| 首次引导 | 三步完成配置：欢迎 → 下载位置 → 网络检测 |

界面为**深浅双主题**，侧栏底部一键切换，设置里也可切换或跟随系统。

## 安装与运行

从 [Releases](https://github.com/Fim98/byte-tv/releases) 下载对应平台的产物：

| 平台 | 文件 | 说明 |
| --- | --- | --- |
| macOS（Intel + Apple Silicon） | `ByteTV-<版本>-universal.dmg` | 通用二进制，双击安装 |
| macOS 便携版 | `ByteTV-<版本>-universal-app.zip` | 解压即用 |
| Windows x64 / arm64 | `ByteTV-<版本>-<arch>-setup.exe` | 内置 WebView2 引导，缺失时自动安装 |
| Windows 免安装 | `ByteTV-<版本>-windows-portable.zip` | 解压后直接运行 |
| Linux x64 | `ByteTV-<版本>-linux-amd64.tar.gz` | 需系统具备 WebKitGTK 4.1 |

下载页同时提供 `SHA256SUMS.txt` 校验和。

**macOS 首次打开**：产物若未经 Apple 公证，双击会提示来源不明。到「系统设置 → 隐私与安全性」中允许一次即可，之后正常打开。

**程序行为**：单实例运行，重复启动会聚焦已有窗口；关闭窗口直接退出应用；若后台有未完成的下载或合并任务，退出时会先弹窗确认。

## 界面导览

界面基于 **Claude Design System（CDS）** 构建：暖黑表面阶梯、陶土色 `#d97757` 作为强调色、圆角与弹簧动效阶梯；中文字体 Noto Sans SC，等宽数字用 JetBrains Mono。样式全部用 Tailwind 工具类表达，令牌集中在 `frontend/tailwind.config.js` 与 `frontend/src/styles/tokens.css`，深浅两套由 `html[data-theme]` 驱动。

窗口由三部分组成：左侧通高侧栏、顶部工具栏、右侧内容区。

### 侧栏

主导航为六个功能页，底部是固定停靠的一排：

```
发现  剧库  榜单  追剧  历史  下载
              ⋮ 弹性留白
───────────────────────────────
  ⚙ 设置                    ☾ / ☀
```

底部的主题按钮显示**当前所处模式**的图标（浅色显太阳、深色显月亮），点击即时切换。

### 顶栏

后退、前进、搜索框；右侧依次是站源状态胶囊（连接正常时显示「红果源 · 正常」）、检查新内容、通知铃。

搜索框即时筛选本地剧库；输入时会显示站点联想，支持方向键选择与回车搜索。

## 页面

### 发现

落地页，从上到下：

- **主推位**：当前热播剧的大幅推荐，带榜单标签、类型、集数热度与「立即播放 / 追剧 / 下载」三个动作，多部剧之间自动轮播
- **继续观看**：横向走廊，按上次看到的位置继续
- **追剧有新集**：有更新的追剧单独一段
- **红果热播榜**：横向走廊 + 进入完整榜单
- **分类推荐**：来自站点分类推荐，一屏展示固定几张，更多进入对应题材的完整列表

### 剧库

检索主战场。顶部筛选条压成一行：

| 维度 | 选项 |
| --- | --- |
| 类型 | 全部 / 真人剧 / 漫剧 / AI 剧 |
| 状态 | 全部 / 已完结 / 连载中 |
| 排序 | 相关度 / 热度 / 最新 / 播放最多 / 名称 |
| 题材 | 胶囊式多选 |

下方为海报网格，悬停时浮现播放、收藏、下载三个快捷动作。

### 剧集详情

头图与资料区（剧名、别名、集数、状态、上线时间、标签、简介），下接**选集**：每页 30 集分页。选集按钮自带状态标记 —— 当前集高亮并显示进度条，已看集用字色区分，本地已下载标出圆点，付费集标 VIP。页面底部为同类推荐。

### 榜单

四个榜单切换查看，保留源站名次与排序。

### 追剧与历史

两个页签：

- **追剧**：全部 / 想看 / 在看 / 已看，进度条为视觉主轴，有新集时标出新增集数
- **观看历史**：可搜索、单条删除、清空全部

### 下载

顶部为状态计数与分段筛选（全部 / 进行中 / 已完成 / 暂停·失败），下接任务列表：

- 单任务与批量操作：暂停、继续、取消、重试
- 「更新本剧」只补新增或本地缺失的分集
- 「清理任务」删除记录但保留已下载文件
- 合并完成后出现「播放全集」，直接读取本地合并文件，支持拖动与续播

**下载目录**与**同时下载数**在设置里调整。

### 设置

左侧分节导航，右侧表单：

| 分节 | 项目 |
| --- | --- |
| 外观与窗口 | 主题（浅色 / 深色 / 跟随系统）、关闭窗口时保留在后台（默认开启） |
| 播放 | 自动播放下一集、默认开启弹幕、预缓存下一集 |
| 下载与存储 | 下载目录、同时下载数（1–32）、按站源分类保存 |
| 网络 | 当前网络、自定义代理、剧库请求并发与同站间隔、检测当前连接 |
| 关于 | 版本、检查更新、数据目录、诊断日志、重新运行首次引导 |

## 播放器

### 结构

播放页不是盖住整个窗口的浮层，而是内容区里的一个页面：左侧通高侧栏始终可见可点，播放页占据右侧内容区的剩余空间。层级只有三层，写死在这条约定里：

| 层 | 内容 | 形态 |
| --- | --- | --- |
| L0 外壳 | 侧栏、顶栏、页面内容 | 正常文档流 |
| L1 播放页 | 画面区 + 选集栏 | **同层兄弟**，开合互相挤压 |
| L2 真浮层 | 迷你窗、全屏、菜单、Toast | 才允许 absolute / fixed |

画面区是「**上下控件栏 + 居中画面**」：竖屏短剧铺不满窗口，空余处就是黑边，不做成带圆角阴影的卡片；**影像本体恒为暗调**，不跟主题变亮。

选集栏是画面区的兄弟列，不是浮在画面上的抽屉：展开时画面被压缩、控件栏整体跟随变窄，材质是「面板」而不是「弹窗」——只有一条发丝线分隔，不用毛玻璃、投影或圆角。栏宽可拖拽（260–460px），开合状态与宽度跨会话保留；挤压后画面列不足 420px 时自动降级为覆盖式，避免视频被压成一条竖缝。

播放中点侧栏或底部「设置」不会结束播放，而是把播放页收成右下角迷你窗继续播，页面同时露出可操作。

### Esc 是逐层返回，不是关闭

| 层级 | 当前状态 | 按 Esc 的结果 |
| --- | --- | --- |
| 1 | 原生全屏中 | 退出全屏（由系统接管，页面无法拦截） |
| 2 | 选集栏或「更多」菜单展开 | 收起最上面那一层 |
| 3 | 播放页 | 收成右下角**迷你窗**，画面继续播放 |
| 4 | 迷你窗 | 关闭并回到剧库（进度早已自动保存，可续播） |

核心原则是：误按 Esc 不该让你丢掉正在看的剧。前两层完全无损，第三层降级但不断播，只有到第四层才真的关闭。

**画中画 / 独立小窗是独立动作**，用 `I` 键或迷你窗上的按钮触发，不经由 Esc。

### 快捷键

| 按键 | 行为 |
| --- | --- |
| 空格 | 播放 / 暂停 |
| `←` / `→` | 后退 / 快进 5 秒 |
| 长按 `→` | 临时 3 倍速，松开恢复原速 |
| `↑` / `↓` | 调节音量 |
| `N` / `P` | 下一集 / 上一集 |
| `[` / `]` | 降低 / 提高倍速 |
| `M` | 静音 |
| `D` | 弹幕开关 |
| `I` | 迷你窗（画中画式收起） |
| `F` | 全屏 |
| `Esc` | 逐层返回 |

### 其它

- 播放中每 5 秒保存一次进度；暂停、切集、关闭时补记
- 弹幕按 60 秒窗口分段加载，暂停、倍速、跳转、切集保持同步
- 画质只有一档时隐藏选择器
- 控件栏 3 秒无操作自动隐藏，移动鼠标或点画面唤出
- 「预缓存下一集」在缓存余量足够时提前准备，切集无需等待

## 原生能力

以下能力通过 `window.juku` 暴露（前端 `src/lib/desktop.js` 装配），页面代码无需区分运行环境：

- **菜单栏与托盘**：托盘提示进行中的任务数，含「显示窗口 / 下载任务 / 退出」
- **快捷键**：`⌘/Ctrl+1–4` 前往首页 / 浏览 / 追剧 / 下载，`⌘/Ctrl+,` 打开设置，`⌘/Ctrl+R` 刷新，`Ctrl+⌘/Ctrl+F` 切换全屏
- **退出保护**：有下载或合并任务时先确认
- **阻止休眠**：任务进行中阻止系统空闲睡眠（macOS `IOPMAssertion` / Windows `SetThreadExecutionState`）
- **完成通知**：任务全部完成时发系统通知
- **目录与文件**：原生选择下载目录、在访达 / 资源管理器中显示
- **自动更新**：构建时注入更新源即可启用
- **稳定性**：后台 panic 恢复 + 30 秒健康看门狗

## 数据与日志

| 项目 | 位置 |
| --- | --- |
| 数据目录 | macOS `~/Library/Application Support/ByteTV`，Windows `%APPDATA%\ByteTV`（可用 `BYTETV_DATA` 覆盖） |
| 默认下载目录 | macOS `~/Movies/ByteTV`，Windows `~/Videos/ByteTV`；拿不到用户目录时退回 `ByteTV Downloads` |
| 桌面偏好 | 数据目录下 `desktop_prefs.json`（主题、倍速 / 音量 / 画质、引导状态、关闭行为），端口变化也不丢 |
| 诊断日志 | 数据目录 `logs/app.log`（自动轮转） |
| 崩溃日志 | 数据目录 `logs/crash-<时间戳>.log` |

首次启动会自动迁移旧的「果果剧库」数据目录（仅默认目录，显式指定 `-data-dir` 不迁移）。

## 开发

```bash
brew install go node            # Go ≥ 1.25（未装也可，GOTOOLCHAIN 会自动下载）
xcode-select --install          # cgo / WebKit 需要 Command Line Tools
brew install go-task            # 可选；Taskfile 只是 scripts/desktop/*.sh 的封装

cd frontend && npm ci && cd ..
task dev        # 开发运行：使用 frontend/dist，改完前端重新构建并刷新窗口即可
task build      # 构建 → bin/bytetv（前端嵌入二进制）
./bin/bytetv
```

不装 Task 也能直接用脚本：`bash scripts/desktop/dev.sh`、`bash scripts/desktop/build.sh`。

`task test` 跑 Go 静态检查、单元测试与前端构建。

**Linux 只用于开发和 CI**：需要 `libgtk-3-dev libwebkit2gtk-4.1-dev` 与 GStreamer（`gstreamer1.0-plugins-good` / `bad` / `libav`），脚本会自动加 `-tags gtk3`。

**目录约定**：前端在 `frontend/`，构建后拷贝进 `cmd/bytetv/ui/` 并嵌入二进制；桌面外壳与后端入口在 `cmd/bytetv/`；共享后端逻辑在 `internal/`。

## 打包

```bash
# macOS universal（arm64 + x86_64）→ bin/Byte TV.app 与 bin/ByteTV-<版本>-universal.dmg
VERSION=0.1.0 task package:mac

# Windows amd64 / arm64 → exe 与 NSIS 安装包（内置 WebView2 引导程序）
task package:windows
```

Windows 打包需要 `makensis`（`brew install makensis`），可在 macOS / Linux 交叉编译。

### 签名与公证（macOS）

不设置签名标识时执行 ad-hoc 签名，只适合本机使用。正式分发二选一：

```bash
MAC_SIGN_IDENTITY="Developer ID Application: 名称 (TEAMID)" NOTARY_PROFILE=bytetv task package:mac
MAC_SIGN_IDENTITY="Developer ID Application: 名称 (TEAMID)" \
  APPLE_ID=me@example.com APPLE_TEAM_ID=TEAMID APPLE_APP_PASSWORD=xxxx-xxxx task package:mac
```

### 自动更新

默认关闭。打包时注入两个变量即可启用：

- `BYTETV_UPDATE_URL` —— 更新清单地址，可用 `{{platform}}` / `{{arch}}` 占位
- `BYTETV_UPDATE_PUBKEY` —— ed25519 公钥（base64）

### 待补

当前打包流程**未携带 FFmpeg**，首次遇到需要转码或合并的片源时会按需自动准备（下载约 18–29 MiB）。若希望分发即完整，可在 `scripts/desktop/package-mac.sh` / `package-windows.sh` 里把 FFmpeg 一并放入程序目录。

## 自动发版（GitHub Actions）

`.github/workflows/release.yml` 负责三平台打包并发布到 Release，**复用 `scripts/desktop/` 下的同一套脚本**，不另起实现。

```bash
git tag v0.1.0 && git push origin v0.1.0     # 推 v* 标签即触发
```

也可在 Actions → Release → Run workflow 手动触发（只产出 artifact，不发 Release）。

| 平台 | Runner | 产物 |
| --- | --- | --- |
| macOS | `macos-14` | universal `.dmg` + `.app.zip`（用 `lipo` 校验同时含 arm64 与 x86_64） |
| Windows | `windows-latest` | amd64 / arm64 的 NSIS `setup.exe`，另附便携版 `zip` |
| Linux | `ubuntu-latest` | `tar.gz`（失败不阻断发布） |

Release 附带 `SHA256SUMS.txt`。签名与公证通过仓库 Secrets 配置，未配置时自动降级为 ad-hoc 签名：

| Secret | 用途 |
| --- | --- |
| `MAC_SIGN_IDENTITY` | Developer ID 证书名；留空则 ad-hoc |
| `APPLE_ID` / `APPLE_TEAM_ID` / `APPLE_APP_PASSWORD` | 公证凭据（也可用 `NOTARY_PROFILE`） |
| `BYTETV_UPDATE_URL` / `BYTETV_UPDATE_PUBKEY` | 启用自动更新 |

## 环境变量

| 变量 | 用途 |
| --- | --- |
| `BYTETV_DATA` | 覆盖数据目录 |
| `BYTETV_UI_DIR` | 开发时指向磁盘上的前端产物，替代嵌入资源 |
| `BYTETV_DEVTOOLS` | 设为 `1` 启用「开发者工具」菜单项 |
| `BYTETV_UPDATE_URL` / `BYTETV_UPDATE_PUBKEY` | 构建时注入的更新源与公钥 |
| `JUKU_OUTPUT_DIR` | 覆盖下载目录 |
| `JUKU_PROXY_URL` | 覆盖代理设置 |
| `JUKU_FFMPEG` | 指定 FFmpeg 路径 |
| `JUKU_HONGGUO_URL` | 覆盖站点入口地址 |

## 常见问题

| 问题 | 处理与排查 |
| --- | --- |
| macOS 提示「无法验证开发者」 | 产物未公证。到「系统设置 → 隐私与安全性」允许一次；或改用 zip 便携版 |
| Windows 启动后白屏 | 缺少 WebView2 运行时。用安装包重装（会静默补齐），或手动安装 Evergreen 运行时 |
| 下载目录不能写入 | 在设置里改用可写目录；修改后重启生效，已有任务仍使用原路径 |
| 播放卡顿、缓冲短 | 检查线路与代理；查看日志中的缓存与取流记录 |
| 日志里的 `playback.failed` / `media.failed` | 前者是播放过程失败，后者是视频连接或读取失败，结合剧名、集数、时间定位 |
| 页面能打开但取不到数据 | 检查设置 → 网络的代理配置，用「检测当前连接」确认可达性 |
| 想重置到初始状态 | 设置 → 关于 → 重新运行首次引导；或备份后清空数据目录 |

诊断日志在数据目录的 `logs/app.log`，单份 5 MiB、另保留两份备份。日志会隐藏凭据与 URL 签名。

## 延伸阅读

- [客户端模式接口评审](docs/client-api-review.md)
- [Wails 迁移评估与能力对照](docs/wails-migration-eval.md)