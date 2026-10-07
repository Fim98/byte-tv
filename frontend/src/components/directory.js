import { apiPost, native } from "../api"

// 选择目录：Electron 下用原生对话框（dialog.showOpenDialog），浏览器/旧壳回退到后端 /api/ui/directory/pick。
export async function pickDirectory(initialPath) {
  if (native?.pickDirectory) return native.pickDirectory(initialPath || "")
  const r = await apiPost("/api/ui/directory/pick", { initialPath: initialPath || "" })
  return r.path || ""
}

export function revealPath(path) {
  if (native?.showItemInFolder) return native.showItemInFolder(path)
  return Promise.resolve(false)
}
