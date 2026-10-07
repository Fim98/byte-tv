// 只暴露最小的原生能力给页面（contextIsolation + sandbox）。
const { contextBridge, ipcRenderer } = require("electron")

contextBridge.exposeInMainWorld("juku", {
  platform: process.platform,
  pickDirectory: (initialPath) => ipcRenderer.invoke("juku:pick-directory", initialPath),
  showItemInFolder: (target) => ipcRenderer.invoke("juku:show-item", target),
  toggleFullScreen: () => ipcRenderer.invoke("juku:toggle-fullscreen"),
  getVersion: () => ipcRenderer.invoke("juku:version"),
  getSettings: () => ipcRenderer.invoke("juku:get-settings"),
  setSettings: (patch) => ipcRenderer.invoke("juku:set-settings", patch),
  checkForUpdates: () => ipcRenderer.invoke("juku:check-updates"),
  notify: (title, body) => ipcRenderer.invoke("juku:notify", title, body),
  onNavigate: (fn) => ipcRenderer.on("juku:navigate", (_e, hash) => fn(hash)),
})
