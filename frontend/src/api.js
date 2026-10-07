let viewer = { id: "", sources: "", onlineOnly: "false" }

async function readJSON(response) {
  const result = await response.json().catch(() => ({}))
  if (!response.ok) throw new Error(result.error || "请求失败")
  return result
}

export async function initViewer() {
  async function read(path) {
    const response = await fetch(path, {
      credentials: "same-origin",
      cache: "no-store",
      headers: { Accept: "application/json" },
    })
    return readJSON(response)
  }
  let result = await read("/api/ui/viewer")
  if (!result.ready) result = await read("/api/ui/viewer?confirm=1")
  if (!result.ready || !result.id) throw new Error("无法确认观看身份")
  viewer = {
    id: result.id,
    sources: (result.sources || []).join(","),
    onlineOnly: String(result.onlineOnly === true),
  }
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

export async function apiGet(path) {
  const response = await fetch(path, { credentials: "same-origin", cache: "no-store", headers: headers() })
  return readJSON(response)
}

export async function apiPost(path, body) {
  const response = await fetch(path, {
    method: "POST",
    credentials: "same-origin",
    cache: "no-store",
    headers: headers({ "Content-Type": "application/json" }),
    body: JSON.stringify(body),
  })
  return readJSON(response)
}
