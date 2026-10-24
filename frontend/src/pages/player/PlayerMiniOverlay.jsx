import React, { memo } from "react"
import { Expand, Pause, Play, X, PictureInPicture2 } from "lucide-react"

export const PlayerMiniOverlay = memo(function PlayerMiniOverlay({
  title,
  index,
  playing,
  progress,
  onTogglePlay,
  onExpand,
  onClose,
  videoRef,
}) {
  return (
    <div className="absolute inset-0 z-30 flex flex-col justify-between bg-gradient-to-t from-black/80 via-transparent to-black/60 p-2 select-none group">
      {/* Top action row */}
      <div className="flex items-center justify-between opacity-0 group-hover:opacity-100 transition-opacity duration-200">
        <span className="truncate text-[11px] font-medium text-white/90 drop-shadow">
          第 {index} 集
        </span>

        <div className="flex items-center gap-1">
          {typeof document !== "undefined" && document.pictureInPictureEnabled && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation()
                videoRef.current?.requestPictureInPicture?.().catch(() => {})
              }}
              title="独立画中画"
              className="flex size-6 items-center justify-center rounded-full bg-black/60 text-white/80 hover:bg-black/90 hover:text-white transition"
            >
              <PictureInPicture2 className="size-3" />
            </button>
          )}

          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation()
              onExpand()
            }}
            title="恢复大窗"
            className="flex size-6 items-center justify-center rounded-full bg-black/60 text-white/80 hover:bg-black/90 hover:text-white transition"
          >
            <Expand className="size-3" />
          </button>

          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation()
              onClose()
            }}
            title="关闭播放 (Esc)"
            className="flex size-6 items-center justify-center rounded-full bg-black/60 text-white/80 hover:bg-danger/80 hover:text-white transition"
          >
            <X className="size-3" />
          </button>
        </div>
      </div>

      {/* Center Play/Pause button */}
      <div className="flex items-center justify-center">
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            onTogglePlay()
          }}
          className="flex size-10 items-center justify-center rounded-full bg-black/60 text-white shadow-lg backdrop-blur-md opacity-0 group-hover:opacity-100 transition-all hover:scale-110 hover:bg-clay active:scale-95"
        >
          {playing ? <Pause className="size-4.5 fill-current" /> : <Play className="size-4.5 fill-current ml-0.5" />}
        </button>
      </div>

      {/* Bottom Title & Progress bar */}
      <div className="flex flex-col gap-1">
        <div className="truncate text-[11px] font-medium text-white drop-shadow">
          {title}
        </div>
        <div className="h-1 w-full overflow-hidden rounded-full bg-white/20">
          <div
            className="h-full bg-clay transition-all duration-150"
            style={{ width: `${Math.min(100, Math.max(0, progress * 100))}%` }}
          />
        </div>
      </div>
    </div>
  )
})
