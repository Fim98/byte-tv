export function coverOf(d) {
  if (!d) return ""
  for (const key of ["cover", "coverUrl", "cover_url", "image", "imageUrl", "poster", "thumb"]) {
    if (typeof d[key] === "string" && d[key]) return d[key]
  }
  return ""
}
export function titleOf(d) {
  return (d && (d.title || d.name || d.dramaTitle)) || "短剧"
}
export function episodesOf(d) {
  if (!d) return 0
  for (const key of ["totalEpisode", "total_episode", "episodeCount", "chapterCount", "episodes", "total"]) {
    const n = Number(d[key])
    if (Number.isFinite(n) && n > 0) return n
  }
  return 0
}
export function categoryOf(d) {
  return (d && (d.category || d.categoryName || d.typeName || d.sortName || d.channelName)) || ""
}
export function releaseText(status) {
  return { finished: "已完结", ongoing: "连载中" }[status] || ""
}
export function formatTime(value) {
  if (!Number.isFinite(value) || value < 0) return "0:00"
  const t = Math.floor(value)
  const h = Math.floor(t / 3600)
  const m = Math.floor((t % 3600) / 60)
  const s = String(t % 60).padStart(2, "0")
  return h ? `${h}:${String(m).padStart(2, "0")}:${s}` : `${m}:${s}`
}
export function formatBytes(value) {
  let n = Number(value) || 0
  if (n <= 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  let i = 0
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${i ? n.toFixed(n >= 100 ? 0 : 1) : Math.round(n)} ${units[i]}`
}
export function dayLabel(date) {
  const d = new Date(date)
  if (Number.isNaN(d.getTime())) return "更早"
  const start = new Date()
  start.setHours(0, 0, 0, 0)
  const diff = Math.floor((start - new Date(d.getFullYear(), d.getMonth(), d.getDate())) / 86400000)
  if (diff <= 0) return "今天"
  if (diff === 1) return "昨天"
  return `${d.getMonth() + 1}月${d.getDate()}日`
}
