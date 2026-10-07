// Go 后端子进程管理：随机令牌、解析端口、健康检查、崩溃自动重启（退避）、随父进程退出。
const { spawn } = require("node:child_process")
const crypto = require("node:crypto")
const http = require("node:http")
const path = require("node:path")
const fs = require("node:fs")
const { EventEmitter } = require("node:events")

class Backend extends EventEmitter {
  constructor({ binary, uiDir, dataDir, logFile, preferredPort }) {
    super()
    this.preferredPort = preferredPort || 0
    this.binary = binary
    this.uiDir = uiDir
    this.dataDir = dataDir
    this.logFile = logFile
    this.token = crypto.randomBytes(32).toString("hex")
    this.port = 0
    this.child = null
    this.stopping = false
    this.restarts = []
  }

  get origin() {
    return `http://127.0.0.1:${this.port}`
  }

  start() {
    return new Promise((resolve, reject) => {
      const args = ["-client", "-client-ui", this.uiDir]
      if (this.dataDir) args.push("-data-dir", this.dataDir)
      if (this.port || this.preferredPort) args.push("-client-port", String(this.port || this.preferredPort))
      const log = this.logFile ? fs.createWriteStream(this.logFile, { flags: "a" }) : null
      const child = spawn(this.binary, args, {
        env: { ...process.env, JUKU_CLIENT_TOKEN: this.token },
        stdio: ["pipe", "pipe", "pipe"], // stdin 保持打开：Electron 退出/崩溃时管道关闭，后端自行退出
        windowsHide: true,
      })
      this.child = child
      let settled = false
      const timer = setTimeout(() => {
        if (!settled) { settled = true; reject(new Error("后端启动超时（30 秒）")); child.kill() }
      }, 30000)
      let buffer = ""
      child.stdout.on("data", (chunk) => {
        log?.write(chunk)
        if (settled) return
        buffer += chunk.toString("utf8")
        const match = buffer.match(/JUKU_CLIENT_READY port=(\d+)/)
        if (match) {
          settled = true
          clearTimeout(timer)
          this.port = Number(match[1])
          resolve(this.port)
        }
      })
      child.stderr.on("data", (chunk) => log?.write(chunk))
      child.on("error", (error) => {
        if (!settled) { settled = true; clearTimeout(timer); reject(error) }
      })
      child.on("exit", (code, signal) => {
        log?.end(`\n[backend exit] code=${code} signal=${signal}\n`)
        this.child = null
        if (!settled) { settled = true; clearTimeout(timer); reject(new Error(`后端启动失败（退出码 ${code}）`)); return }
        if (!this.stopping) this.emit("crash", { code, signal })
      })
    })
  }

  // 崩溃后自动重启：5 分钟内最多 3 次，超过则交给界面提示。
  async restart() {
    const now = Date.now()
    this.restarts = this.restarts.filter((t) => now - t < 5 * 60 * 1000)
    if (this.restarts.length >= 3) throw new Error("后端多次异常退出，请查看日志")
    this.restarts.push(now)
    await new Promise((r) => setTimeout(r, 500 * this.restarts.length))
    return this.start()
  }

  request(pathname, { method = "GET", timeout = 3000 } = {}) {
    return new Promise((resolve, reject) => {
      const req = http.request({ host: "127.0.0.1", port: this.port, path: pathname, method, headers: { "X-Juku-Token": this.token, Host: `127.0.0.1:${this.port}` }, timeout }, (res) => {
        let body = ""
        res.on("data", (c) => (body += c))
        res.on("end", () => { try { resolve(JSON.parse(body || "{}")) } catch (e) { reject(e) } })
      })
      req.on("timeout", () => req.destroy(new Error("timeout")))
      req.on("error", reject)
      req.end()
    })
  }

  async status() {
    try { return await this.request("/api/desktop/status") } catch { return null }
  }

  // 优雅停止：先关 stdin（后端收到 EOF 后保存状态并退出），超时再强杀。
  stop(timeoutMs = 6000) {
    this.stopping = true
    const child = this.child
    if (!child) return Promise.resolve()
    return new Promise((resolve) => {
      const timer = setTimeout(() => { try { child.kill("SIGKILL") } catch {} resolve() }, timeoutMs)
      child.once("exit", () => { clearTimeout(timer); resolve() })
      try { child.stdin.end("quit\n") } catch { child.kill() }
    })
  }
}

function resolveBackendBinary(app) {
  const name = process.platform === "win32" ? "juku-backend.exe" : "juku-backend"
  if (app.isPackaged) return path.join(process.resourcesPath, "bin", name)
  const dir = process.platform === "win32" ? "win" : process.platform === "darwin" ? "mac" : "linux"
  return process.env.JUKU_BACKEND_BIN || path.join(__dirname, "resources", "bin", dir, name)
}

function resolveUiDir(app) {
  return app.isPackaged ? path.join(process.resourcesPath, "ui") : path.join(__dirname, "..", "frontend", "dist")
}

module.exports = { Backend, resolveBackendBinary, resolveUiDir }
