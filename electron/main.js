// Byte TV Electron 主进程：启动 Go 后端子进程（127.0.0.1 随机端口 + 令牌），窗口通过 HTTP 加载前端。
const { app, BrowserWindow, Menu, Notification, Tray, dialog, ipcMain, nativeImage, powerSaveBlocker, session, shell } = require("electron")
const fs = require("node:fs")
const path = require("node:path")
const { Backend, resolveBackendBinary, resolveUiDir } = require("./backend")

const APP_NAME = "Byte TV"
// 数据目录名不含空格和中文，与 Go 后端 clientAppName 保持一致。
const DATA_DIR_NAME = "ByteTV"
const LEGACY_DATA_DIR_NAME = "果果剧库"

// 改名迁移：必须在 setPath/任何写入之前完成，否则新目录会先被 Electron 创建，导致后端跳过迁移。
function migrateLegacyDataDir() {
  const appData = app.getPath("appData")
  const target = path.join(appData, DATA_DIR_NAME)
  const legacy = path.join(appData, LEGACY_DATA_DIR_NAME)
  try {
    if (!fs.existsSync(target) && fs.statSync(legacy, { throwIfNoEntry: false })?.isDirectory()) {
      fs.renameSync(legacy, target)
      console.log(`[migrate] ${legacy} -> ${target}`)
    }
  } catch (error) {
    console.error("[migrate] 迁移旧数据目录失败：", error)
  }
}
migrateLegacyDataDir()

// Electron 自身数据放子目录，避免与 Go 后端数据（同一 ByteTV 目录根部）混在一起。
app.setName(APP_NAME)
app.setPath("userData", path.join(app.getPath("appData"), DATA_DIR_NAME, "Shell"))

const settingsFile = () => path.join(app.getPath("userData"), "shell-settings.json")
function readSettings() {
  try { return JSON.parse(fs.readFileSync(settingsFile(), "utf8")) } catch { return {} }
}
function writeSettings(patch) {
  const next = { ...readSettings(), ...patch }
  fs.mkdirSync(path.dirname(settingsFile()), { recursive: true })
  fs.writeFileSync(settingsFile(), JSON.stringify(next, null, 2))
  return next
}

let mainWindow = null
let tray = null
let backend = null
let quitting = false

if (!app.requestSingleInstanceLock()) {
  app.quit()
} else {
  app.on("second-instance", () => showWindow())
}

function showWindow() {
  if (!mainWindow) return
  if (mainWindow.isMinimized()) mainWindow.restore()
  mainWindow.show()
  mainWindow.focus()
}

function createWindow() {
  const saved = readSettings().window || {}
  mainWindow = new BrowserWindow({
    title: APP_NAME,
    width: saved.width || 1280,
    height: saved.height || 840,
    x: saved.x,
    y: saved.y,
    minWidth: 960,
    minHeight: 640,
    backgroundColor: "#07070a",
    show: false,
    titleBarStyle: process.platform === "darwin" ? "hiddenInset" : "hidden",
    titleBarOverlay: process.platform === "win32" ? { color: "#07070a", symbolColor: "#ffffff", height: 40 } : undefined,
    trafficLightPosition: { x: 16, y: 18 },
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      contextIsolation: true,
      sandbox: true,
      nodeIntegration: false,
      spellcheck: false,
      backgroundThrottling: false, // 后台播放 / 心跳不被节流
    },
  })
  mainWindow.once("ready-to-show", () => mainWindow.show())
  // 仅允许加载本机后端页面；外部链接交给系统浏览器。
  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    if (/^https?:\/\//.test(url) && !url.startsWith(backend.origin)) shell.openExternal(url)
    return { action: "deny" }
  })
  mainWindow.webContents.on("will-navigate", (event, url) => {
    if (!url.startsWith(backend.origin)) event.preventDefault()
  })
  mainWindow.webContents.on("render-process-gone", (_e, details) => {
    if (details.reason !== "clean-exit") setTimeout(() => mainWindow?.reload(), 500)
  })
  const persistBounds = () => {
    if (!mainWindow || mainWindow.isFullScreen() || mainWindow.isMinimized()) return
    writeSettings({ window: mainWindow.getBounds() })
  }
  mainWindow.on("resized", persistBounds)
  mainWindow.on("moved", persistBounds)
  mainWindow.on("close", async (event) => {
    if (quitting) return
    if (readSettings().closeToTray !== false) {
      event.preventDefault()
      mainWindow.hide()
      return
    }
    event.preventDefault()
    if (await confirmQuit()) { quitting = true; app.quit() }
  })
  mainWindow.loadURL(`${backend.origin}/?token=${backend.token}`)
}

// 有下载 / 合并进行中时二次确认（现有 CloseDesktop 会中断合并）。
async function confirmQuit() {
  const status = await backend?.status()
  const downloads = status?.activeDownloads || 0
  const merges = status?.activeMerges || 0
  if (!downloads && !merges) return true
  const { response } = await dialog.showMessageBox(mainWindow, {
    type: "warning",
    buttons: ["退出", "取消"],
    defaultId: 1,
    cancelId: 1,
    message: `${downloads} 个下载、${merges} 个合并进行中`,
    detail: "退出会中断这些任务（下次启动可继续下载，合并需重新开始）。",
  })
  return response === 0
}

function navigate(hash) {
  showWindow()
  mainWindow?.webContents.executeJavaScript(`location.hash = ${JSON.stringify(hash)}`).catch(() => {})
}

function buildMenu() {
  const isMac = process.platform === "darwin"
  const template = [
    ...(isMac ? [{ label: APP_NAME, submenu: [{ role: "about", label: `关于 ${APP_NAME}` }, { type: "separator" }, { label: "设置…", accelerator: "Cmd+,", click: () => navigate("/settings") }, { type: "separator" }, { role: "hide", label: "隐藏" }, { role: "hideOthers" }, { type: "separator" }, { label: "退出", accelerator: "Cmd+Q", click: () => requestQuit() }] }] : []),
    { label: "文件", submenu: [...(!isMac ? [{ label: "设置", accelerator: "Ctrl+,", click: () => navigate("/settings") }, { label: "退出", click: () => requestQuit() }] : [{ role: "close", label: "关闭窗口" }])] },
    { label: "编辑", submenu: [{ role: "undo" }, { role: "redo" }, { type: "separator" }, { role: "cut" }, { role: "copy" }, { role: "paste" }, { role: "selectAll" }] },
    { label: "前往", submenu: [
      { label: "首页", accelerator: "CmdOrCtrl+1", click: () => navigate("/home") },
      { label: "分类", accelerator: "CmdOrCtrl+2", click: () => navigate("/browse") },
      { label: "追剧", accelerator: "CmdOrCtrl+3", click: () => navigate("/following") },
      { label: "下载管理", accelerator: "CmdOrCtrl+4", click: () => navigate("/downloads") },
      { type: "separator" },
      { label: "后退", accelerator: "CmdOrCtrl+[", click: () => mainWindow?.webContents.navigationHistory?.goBack?.() },
      { label: "前进", accelerator: "CmdOrCtrl+]", click: () => mainWindow?.webContents.navigationHistory?.goForward?.() },
    ] },
    { label: "窗口", submenu: [{ role: "minimize", label: "最小化" }, { role: "togglefullscreen", label: "全屏" }, ...(app.isPackaged ? [] : [{ role: "toggleDevTools" }, { role: "reload" }])] },
  ]
  Menu.setApplicationMenu(Menu.buildFromTemplate(template))
}

function buildTray() {
  const icon = nativeImage.createFromPath(path.join(__dirname, "build", process.platform === "darwin" ? "trayTemplate.png" : "tray.png"))
  tray = new Tray(icon.isEmpty() ? nativeImage.createEmpty() : icon)
  tray.setToolTip(APP_NAME)
  if (icon.isEmpty() && process.platform === "darwin") tray.setTitle("果")
  const menu = Menu.buildFromTemplate([
    { label: "显示窗口", click: () => showWindow() },
    { label: "下载管理", click: () => navigate("/downloads") },
    { type: "separator" },
    { label: "退出", click: () => requestQuit() },
  ])
  tray.setContextMenu(menu)
  tray.on("click", () => showWindow())
}

async function requestQuit() {
  if (await confirmQuit()) { quitting = true; app.quit() }
}

function registerIpc() {
  ipcMain.handle("juku:pick-directory", async (_e, initialPath) => {
    const result = await dialog.showOpenDialog(mainWindow, { properties: ["openDirectory", "createDirectory"], defaultPath: typeof initialPath === "string" && initialPath ? initialPath : undefined })
    return result.canceled ? "" : result.filePaths[0] || ""
  })
  ipcMain.handle("juku:show-item", (_e, target) => {
    if (typeof target !== "string" || !target) return false
    if (fs.existsSync(target) && fs.statSync(target).isDirectory()) shell.openPath(target)
    else shell.showItemInFolder(target)
    return true
  })
  ipcMain.handle("juku:toggle-fullscreen", () => mainWindow?.setFullScreen(!mainWindow.isFullScreen()))
  ipcMain.handle("juku:version", () => app.getVersion())
  ipcMain.handle("juku:get-settings", () => readSettings())
  ipcMain.handle("juku:set-settings", (_e, patch) => {
    const allowed = {}
    if (patch && typeof patch.closeToTray === "boolean") allowed.closeToTray = patch.closeToTray
    return writeSettings(allowed)
  })
  ipcMain.handle("juku:notify", (_e, title, body) => {
    if (Notification.isSupported()) new Notification({ title: String(title || APP_NAME), body: String(body || "") }).show()
  })
  ipcMain.handle("juku:check-updates", async () => {
    if (!app.isPackaged) return { message: "开发模式不检查更新" }
    try {
      const { autoUpdater } = require("electron-updater")
      const result = await autoUpdater.checkForUpdates()
      return { message: result?.updateInfo ? `最新版本 ${result.updateInfo.version}` : "已是最新版本" }
    } catch (error) {
      return { message: `检查更新失败：${error.message}` }
    }
  })
}

// 后台下载进行中阻止系统休眠；下载完成时发送系统通知。
function watchTasks() {
  let blocker = null
  let lastActive = 0
  setInterval(async () => {
    const status = await backend.status()
    if (!status) return
    const active = (status.activeDownloads || 0) + (status.activeMerges || 0)
    if (active && blocker === null) blocker = powerSaveBlocker.start("prevent-app-suspension")
    if (!active && blocker !== null) { powerSaveBlocker.stop(blocker); blocker = null }
    if (lastActive > 0 && active === 0 && Notification.isSupported()) new Notification({ title: APP_NAME, body: "下载与合并任务已全部完成" }).show()
    lastActive = active
    tray?.setToolTip(active ? `${APP_NAME} · ${active} 个任务进行中` : APP_NAME)
  }, 5000)
}

async function startBackend() {
  const settings = readSettings()
  backend = new Backend({
    binary: resolveBackendBinary(app),
    uiDir: resolveUiDir(app),
    logFile: path.join(app.getPath("userData"), "backend.log"),
    preferredPort: settings.port,
  })
  const port = await backend.start()
  writeSettings({ port })
  backend.on("crash", async () => {
    try {
      await backend.restart()
      mainWindow?.loadURL(`${backend.origin}/?token=${backend.token}`)
    } catch (error) {
      dialog.showErrorBox(APP_NAME, `本机服务异常退出：${error.message}\n日志：${path.join(app.getPath("userData"), "backend.log")}`)
    }
  })
}

app.whenReady().then(async () => {
  // 页面不需要任何敏感权限（摄像头、定位等），统一拒绝；全屏允许。
  session.defaultSession.setPermissionRequestHandler((_wc, permission, callback) => callback(permission === "fullscreen" || permission === "notifications"))
  try {
    await startBackend()
  } catch (error) {
    dialog.showErrorBox(APP_NAME, `无法启动本机服务：${error.message}`)
    app.exit(1)
    return
  }
  registerIpc()
  buildMenu()
  createWindow()
  buildTray()
  watchTasks()
  if (app.isPackaged) {
    try { require("electron-updater").autoUpdater.checkForUpdatesAndNotify() } catch {}
  }
})

app.on("activate", () => showWindow())
app.on("before-quit", () => { quitting = true })
app.on("window-all-closed", () => { if (process.platform !== "darwin") requestQuit() })
app.on("will-quit", (event) => {
  if (!backend || backend.stopped) return
  event.preventDefault()
  backend.stop().finally(() => { backend.stopped = true; app.quit() })
})
