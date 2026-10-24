import { apiGet, apiPost } from "./api"
import { toast } from "./components/cds"

let playerOpener = null
export function setPlayerOpener(fn) { playerOpener = fn }
// 打开播放器：{ dramaId, title, episode?, resume?, fromHistory?, taskId? }
export function play(request) { playerOpener?.(request) }

// 全局追剧状态管理（Set 与订阅分发，确保所有页面、所有卡片实时响应高亮）
const followingIds = new Set()
const followingListeners = new Set()

export function isFollowing(dramaId) {
  if (!dramaId) return false
  return followingIds.has(String(dramaId))
}

export function subscribeFollowing(listener) {
  followingListeners.add(listener)
  return () => followingListeners.delete(listener)
}

function notifyFollowing() {
  followingListeners.forEach((fn) => fn())
}

export async function loadFollowing() {
  try {
    const res = await apiGet("/api/ui/following")
    const list = res.data || []
    followingIds.clear()
    for (const item of list) {
      const id = String(item.dramaId || item.id || "")
      if (id && (item.saved !== false)) {
        followingIds.add(id)
      }
    }
    notifyFollowing()
  } catch {
    // ignore
  }
}

export async function follow(dramaId, saved = true) {
  try {
    const id = String(dramaId)
    await apiPost("/api/ui/following", { dramaId, saved })
    if (saved) {
      followingIds.add(id)
    } else {
      followingIds.delete(id)
    }
    notifyFollowing()
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
