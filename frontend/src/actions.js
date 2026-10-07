import { apiPost } from "./api"
import { toast } from "./components/common"

let playerOpener = null
export function setPlayerOpener(fn) { playerOpener = fn }
// 打开播放器：{ dramaId, title, episode?, resume?, fromHistory?, taskId? }
export function play(request) { playerOpener?.(request) }

export async function follow(dramaId, saved = true) {
  try {
    await apiPost("/api/ui/following", { dramaId, saved })
    toast(saved ? "已加入追剧" : "已取消追剧", "ok")
    return true
  } catch (error) {
    toast(error.message, "error")
    return false
  }
}

export async function markCompleted(dramaId, completed = true) {
  try {
    await apiPost("/api/ui/following", { dramaId, completed })
    toast(completed ? "已标为已看" : "已取消已看", "ok")
  } catch (error) {
    toast(error.message, "error")
  }
}

export async function download(ids, quality) {
  try {
    const body = { ids: Array.isArray(ids) ? ids : [ids] }
    if (quality != null) body.quality = quality
    await apiPost("/api/ui/download", body)
    toast("已加入下载队列，可在“下载管理”查看", "ok")
    return true
  } catch (error) {
    toast(error.message, "error")
    return false
  }
}
