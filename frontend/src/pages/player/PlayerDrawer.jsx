import React, { useState, memo } from "react"
import { Check, Download, Search, X } from "lucide-react"
import { cn } from "../../lib/utils"

const EPISODES_PER_PAGE = 30

export const PlayerDrawer = memo(function PlayerDrawer({
  open,
  episodes = [],
  currentIndex,
  autoNext,
  onClose,
  onSelectEpisode,
  onToggleAutoNext,
}) {
  const [jumpInput, setJumpInput] = useState("")
  const [page, setPage] = useState(() => Math.floor((currentIndex - 1) / EPISODES_PER_PAGE))

  const totalPages = Math.ceil(episodes.length / EPISODES_PER_PAGE)
  const activePage = Math.min(totalPages - 1, Math.max(0, page))
  const pageEpisodes = episodes.slice(
    activePage * EPISODES_PER_PAGE,
    (activePage + 1) * EPISODES_PER_PAGE
  )

  const handleJump = (e) => {
    if (e.key === "Enter" && jumpInput) {
      const num = parseInt(jumpInput, 10)
      if (!Number.isNaN(num) && num >= 1 && num <= episodes.length) {
        setPage(Math.floor((num - 1) / EPISODES_PER_PAGE))
        onSelectEpisode(num)
        setJumpInput("")
      }
    }
  }

  return (
    <>
      {/* Backdrop overlay for easy close when clicking outside */}
      {open && (
        <div
          className="absolute inset-0 z-40 bg-black/25 backdrop-blur-[2px] transition-opacity duration-300"
          onClick={onClose}
        />
      )}

      {/* Drawer slide-over container */}
      <aside
        className={cn(
          "absolute inset-y-0 right-0 z-50 flex w-80 max-w-[85vw] flex-col bg-surface-1 p-4 shadow-2xl border-l border-subtle transition-transform duration-300 ease-out select-none",
          open ? "translate-x-0" : "translate-x-full pointer-events-none"
        )}
      >
        {/* Header */}
        <div className="flex items-center justify-between pb-3 border-b border-hairline">
          <div className="flex items-baseline gap-2">
            <h3 className="text-sm font-semibold text-ink">剧集列表</h3>
            <span className="text-xs text-ink-muted">共 {episodes.length} 集</span>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="flex size-7 items-center justify-center rounded-full text-ink-muted hover:bg-surface-3 hover:text-ink transition"
          >
            <X className="size-4" />
          </button>
        </div>

        {/* Quick jump input */}
        <div className="mt-3 relative flex items-center">
          <input
            type="text"
            placeholder="输入集数按回车跳转"
            value={jumpInput}
            onChange={(e) => setJumpInput(e.target.value.replace(/\D/g, ""))}
            onKeyDown={handleJump}
            className="h-8 w-full rounded-pill bg-surface-2 pl-3 pr-8 text-xs text-ink placeholder:text-ink-muted border border-subtle focus:border-clay outline-none"
          />
          <Search className="pointer-events-none absolute right-3 size-3.5 text-ink-muted" />
        </div>

        {/* Episode pagination tabs */}
        {totalPages > 1 && (
          <div className="mt-3 flex flex-wrap gap-1">
            {Array.from({ length: totalPages }, (_, i) => {
              const start = i * EPISODES_PER_PAGE + 1
              const end = Math.min(episodes.length, (i + 1) * EPISODES_PER_PAGE)
              const isActive = i === activePage
              return (
                <button
                  key={i}
                  type="button"
                  onClick={() => setPage(i)}
                  className={cn(
                    "h-6 rounded-pill px-2.5 text-[11px] font-medium transition",
                    isActive
                      ? "bg-clay text-white shadow-sm"
                      : "bg-surface-2 text-ink-secondary hover:bg-surface-3 hover:text-ink border border-subtle"
                  )}
                >
                  {start}-{end}
                </button>
              )
            })}
          </div>
        )}

        {/* Episode grid list */}
        <div className="mt-3 flex-1 overflow-y-auto scroll-thin pr-1">
          <div className="grid grid-cols-5 gap-2">
            {pageEpisodes.map((ep) => {
              const isCurrent = ep.index === currentIndex
              const isWatched = ep.index < currentIndex
              const isDownloaded = Boolean(ep.taskId)

              return (
                <button
                  key={ep.index}
                  type="button"
                  onClick={() => onSelectEpisode(ep.index)}
                  title={ep.title || `第 ${ep.episode || ep.index} 集`}
                  className={cn(
                    "group relative flex h-10 flex-col items-center justify-center rounded-lg text-xs font-medium transition-all duration-150 active:scale-95 border border-transparent",
                    isCurrent
                      ? "bg-clay text-white shadow-md shadow-clay/30 font-bold scale-[1.03]"
                      : isWatched
                      ? "bg-surface-2/60 text-ink-muted hover:bg-surface-3 hover:text-ink border-subtle"
                      : "bg-surface-2 text-ink hover:bg-surface-3 border-subtle"
                  )}
                >
                  <span className="font-mono text-xs">{ep.episode || ep.index}</span>

                  {/* Badges */}
                  <div className="absolute right-1 top-1 flex items-center gap-0.5">
                    {isDownloaded && (
                      <span className="size-1.5 rounded-full bg-emerald-500" title="已下载" />
                    )}
                  </div>
                </button>
              )
            })}
          </div>
        </div>

        {/* Footer options */}
        <div className="mt-auto border-t border-hairline pt-3 flex flex-col gap-2">
          {/* Auto next toggle */}
          <div className="flex items-center justify-between">
            <span className="text-xs text-ink-secondary">自动连播下一集</span>
            <button
              type="button"
              role="switch"
              aria-checked={autoNext}
              onClick={onToggleAutoNext}
              className={cn(
                "relative h-5 w-9 rounded-full transition-colors",
                autoNext ? "bg-clay" : "bg-surface-3 border border-subtle"
              )}
            >
              <span
                className={cn(
                  "absolute top-0.5 left-0.5 size-4 rounded-full bg-white shadow-sm transition-transform duration-200",
                  autoNext && "translate-x-4"
                )}
              />
            </button>
          </div>

          {/* Keyboard shortcut tips */}
          <div className="rounded-lg bg-surface-2 p-2.5 text-[11px] leading-relaxed text-ink-muted border border-hairline">
            <span className="font-medium text-ink-secondary">快捷键提示：</span>
            <div className="mt-1 grid grid-cols-2 gap-1 text-[10.5px]">
              <span>空格：播放 / 暂停</span>
              <span>← / →：快退 / 快进 5s</span>
              <span>↑ / ↓：调节音量</span>
              <span>长按 →：3× 超快进</span>
              <span>F：全屏切换</span>
              <span>D：开启/关闭弹幕</span>
              <span>I：收起为小窗</span>
              <span>N：下一集</span>
            </div>
          </div>
        </div>
      </aside>
    </>
  )
})
