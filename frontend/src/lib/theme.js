// 主题：dark / light / system，保存在 localStorage，作用于 <html data-theme>。播放器始终深色。
const KEY = "juku.theme"
const media = typeof window !== "undefined" && window.matchMedia ? window.matchMedia("(prefers-color-scheme: light)") : null

export function getTheme() {
  const v = localStorage.getItem(KEY)
  return v === "light" || v === "system" ? v : "dark"
}

export function applyTheme(theme = getTheme()) {
  const light = theme === "light" || (theme === "system" && media?.matches)
  document.documentElement.dataset.theme = light ? "light" : "dark"
}

export function setTheme(theme) {
  localStorage.setItem(KEY, theme)
  applyTheme(theme)
}

media?.addEventListener?.("change", () => getTheme() === "system" && applyTheme())
