// 桌面兼容层：Wails v3 外壳下页面运行在 http://127.0.0.1:端口，拿不到 Wails 运行时绑定，
// 原生能力改由后端 /api/desktop/native/* 转发给同进程外壳。这里把它们装配成与 Electron
// preload 相同形状的 window.juku，现有页面代码无需区分两种外壳。
import { navigate } from "../router"

const PREF_KEYS = /^juku\./
let mediaToken = ""
let desktopShell = false

export function isDesktopShell() {
  return desktopShell
}

// 给原生 <video> 直连的媒体地址加只读凭据（macOS AVFoundation 原生 HLS/MP4 请求不带 Cookie）。
export function withMediaToken(url) {
  if (!mediaToken || !url || !url.startsWith("/api/")) return url
  return url + (url.includes("?") ? "&" : "?") + "bt=" + encodeURIComponent(mediaToken)
}

async function call(action, body) {
  const response = await fetch(`/api/desktop/native/${action}`, {
    method: "POST",
    credentials: "same-origin",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body ?? {}),
  })
  const data = await response.json().catch(() => ({}))
  if (!response.ok) throw new Error(data?.error || `HTTP ${response.status}`)
  return data.result
}

function platformName(goos) {
  return goos === "windows" ? "win32" : goos
}

function createBridge(info) {
  return {
    shell: "wails",
    platform: platformName(info.platform),
    pickDirectory: (initial) => call("pickDirectory", { initial }),
    showItemInFolder: (path) => call("showItemInFolder", { path }),
    openExternal: (url) => call("openExternal", { url }),
    toggleFullScreen: () => call("toggleFullScreen"),
    getVersion: () => call("getVersion"),
    getSettings: () => call("getSettings"),
    setSettings: (patch) => call("setSettings", patch),
    checkForUpdates: () => call("checkForUpdates"),
    notify: (title, body) => call("notify", { title, body }),
    onNavigate: (handler) => {
      navHandlers.add(handler)
      return () => navHandlers.delete(handler)
    },
  }
}

const navHandlers = new Set()

function listenEvents() {
  let source
  const connect = () => {
    source = new EventSource("/api/desktop/native/events", { withCredentials: true })
    source.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data)
        if (message.event === "navigate" && typeof message.data === "string") {
          if (navHandlers.size) navHandlers.forEach((handler) => handler(message.data))
          else navigate(message.data)
        }
      } catch { /* 忽略格式错误的事件 */ }
    }
    source.onerror = () => {
      source.close()
      setTimeout(connect, 3000)
    }
  }
  connect()
}

// 偏好（主题、播放偏好、引导标记等 juku.* 键）以后端 desktop_prefs.json 为准：
// 端口变化导致 origin 改变时 localStorage 会丢，后端存储不会。
async function syncPrefs() {
  let remote = {}
  try {
    const response = await fetch("/api/desktop/prefs", { credentials: "same-origin", cache: "no-store" })
    if (!response.ok) return
    remote = await response.json()
  } catch {
    return
  }
  const missing = {}
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i)
    if (PREF_KEYS.test(key) && !(key in remote)) missing[key] = localStorage.getItem(key)
  }
  for (const [key, value] of Object.entries(remote)) {
    if (PREF_KEYS.test(key) && typeof value === "string") localStorage.setItem(key, value)
  }
  if (Object.keys(missing).length) pushPrefs(missing)

  const original = Storage.prototype.setItem
  const originalRemove = Storage.prototype.removeItem
  Storage.prototype.setItem = function (key, value) {
    original.call(this, key, value)
    if (this === localStorage && PREF_KEYS.test(key)) queuePref(key, String(value))
  }
  Storage.prototype.removeItem = function (key) {
    originalRemove.call(this, key)
    if (this === localStorage && PREF_KEYS.test(key)) queuePref(key, null)
  }
}

let pending = {}
let timer = 0
function queuePref(key, value) {
  pending[key] = value
  clearTimeout(timer)
  timer = setTimeout(() => {
    const patch = pending
    pending = {}
    pushPrefs(patch)
  }, 300)
}

function pushPrefs(patch) {
  fetch("/api/desktop/prefs", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
    keepalive: true,
  }).catch(() => {})
}

// 在渲染前调用：普通浏览器/服务器版下 /api/desktop/native/info 不存在，静默降级。
export async function bootstrapDesktop() {
  if (typeof window === "undefined") return
  if (window.juku) {
    desktopShell = true
    return // Electron preload 已注入
  }
  let info
  try {
    const response = await fetch("/api/desktop/native/info", { credentials: "same-origin", cache: "no-store" })
    if (!response.ok) return
    info = await response.json()
  } catch {
    return
  }
  mediaToken = info.mediaToken || ""
  await syncPrefs()
  if (!info.shell) return
  desktopShell = true
  window.juku = createBridge(info)
  document.documentElement.dataset.shell = "wails"
  listenEvents()
}
