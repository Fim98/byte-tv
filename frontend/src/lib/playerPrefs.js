// 播放器偏好：内存 + localStorage 持久化（倍速、音量、静音、画质偏好）。
const KEY = "juku.player.prefs"
const RATES = [0.75, 1, 1.25, 1.5, 2, 3]
const defaults = { rate: 1, volume: 1, muted: false, quality: 0 }

function load() {
  try {
    const saved = JSON.parse(localStorage.getItem(KEY) || "{}")
    return {
      rate: RATES.includes(Number(saved.rate)) ? Number(saved.rate) : defaults.rate,
      volume: Number.isFinite(Number(saved.volume)) ? Math.min(1, Math.max(0, Number(saved.volume))) : defaults.volume,
      muted: saved.muted === true,
      quality: Number.isInteger(Number(saved.quality)) && Number(saved.quality) >= 0 ? Number(saved.quality) : 0,
    }
  } catch {
    return { ...defaults }
  }
}

const prefs = load()

export const PLAYER_RATES = RATES
export function getPlayerPrefs() {
  return prefs
}
export function setPlayerPrefs(patch) {
  Object.assign(prefs, patch)
  try { localStorage.setItem(KEY, JSON.stringify(prefs)) } catch { /* 存储不可用时仅保留内存 */ }
  return prefs
}

// 把偏好应用到 video。设置 src / 重建 hls 会把 playbackRate 重置为 defaultPlaybackRate，所以两者都设。
export function applyPlayerPrefs(video, { boost = false } = {}) {
  if (!video) return
  const rate = boost ? 3 : prefs.rate
  if (video.defaultPlaybackRate !== prefs.rate) video.defaultPlaybackRate = prefs.rate
  if (video.playbackRate !== rate) video.playbackRate = rate
  if (video.volume !== prefs.volume) video.volume = prefs.volume
  if (video.muted !== prefs.muted) video.muted = prefs.muted
}
