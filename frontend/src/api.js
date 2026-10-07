// 前端 API 层：所有请求走同源 HTTP（客户端模式由 Go 后端在 127.0.0.1 随机端口提供，令牌在 HttpOnly Cookie 中）。
let viewer = { id: "", sources: "", onlineOnly: "false" }

async function readJSON(response) {
  const result = await response.json().catch(() => ({}))
  if (!response.ok) {
    const error = new Error(result.error || result.message || `请求失败（${response.status}）`)
    error.status = response.status
    error.code = result.code
    throw error
  }
  return result
}

export async function initViewer() {
  const read = async (path) =>
    readJSON(await fetch(path, { credentials: "same-origin", cache: "no-store", headers: { Accept: "application/json" } }))
  let result = await read("/api/ui/viewer")
  if (!result.ready) result = await read("/api/ui/viewer?confirm=1")
  if (!result.ready || !result.id) throw new Error("无法确认观看身份")
  viewer = { id: result.id, sources: (result.sources || []).join(","), onlineOnly: String(result.onlineOnly === true) }
  return result
}

function headers(extra) {
  return {
    Accept: "application/json",
    "X-Juku-Viewer": viewer.id,
    "X-Juku-Sources": viewer.sources,
    "X-Juku-Online-Only": viewer.onlineOnly,
    ...extra,
  }
}

export async function apiGet(path, { signal } = {}) {
  return readJSON(await fetch(path, { credentials: "same-origin", cache: "no-store", headers: headers(), signal }))
}

export async function apiPost(path, body, { signal } = {}) {
  return readJSON(
    await fetch(path, {
      method: "POST",
      credentials: "same-origin",
      cache: "no-store",
      headers: headers({ "Content-Type": "application/json" }),
      body: JSON.stringify(body ?? {}),
      signal,
    }),
  )
}

// Electron preload 暴露的原生能力；在普通浏览器中为 undefined，调用方需降级。
export const native = typeof window !== "undefined" ? window.juku : undefined
