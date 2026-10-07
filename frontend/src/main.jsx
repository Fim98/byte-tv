import React from "react"
import { createRoot } from "react-dom/client"
import "./index.css"
import { bootstrapDesktop } from "./lib/desktop"

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
