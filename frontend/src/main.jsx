import React from "react"
import { createRoot } from "react-dom/client"
import "./index.css"
import { bootstrapDesktop } from "./lib/desktop"

/* 禁用浏览器默认右键菜单。
   桌面外壳下 WKWebView / WebView2 会弹出自带的「重新加载 / 检查元素 / 朗读」菜单，
   这是浏览器功能而不是产品功能，且样式完全不受 CDS 控制。
   唯一保留的场景是文本输入（input / textarea / contenteditable）：
   那里的右键是「粘贴 / 全选」，属于真实可用性需求，不能一并砍掉。 */
window.addEventListener(
  "contextmenu",
  (e) => {
    const el = e.target instanceof Element ? e.target : null
    if (el?.closest('input, textarea, [contenteditable="true"], [contenteditable=""]')) return
    e.preventDefault()
  },
  { capture: true },
)

const root = document.getElementById("root")
// 先装配桌面兼容层并同步偏好，再加载应用（各模块在导入时读取 localStorage 偏好）。
bootstrapDesktop().finally(async () => {
  const { default: App } = await import("./App")
  createRoot(root).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
  )
})
