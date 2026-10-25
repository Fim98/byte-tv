import React, { useRef, useState, memo } from "react"
import {
  Maximize,
  Minimize,
  Minimize2,
  Pause,
  PictureInPicture2,
  Play,
  SkipForward,
  Volume2,
  VolumeX,
  ListVideo,
  Sparkles,
} from "lucide-react"
import { formatTime } from "../../lib/format"
import { PLAYER_RATES } from "../../lib/playerPrefs"
import { cn } from "../../lib/utils"

/* Scrub bar component with hover preview and dragging support */
function ScrubBar({ value, buffered, duration, onChange }) {
  const barRef = useRef(null)
  const [hoverPct, setHoverPct] = useState(null)
  const [dragging, setDragging] = useState(false)

  const calcRatio = (e) => {
    const rect = barRef.current?.getBoundingClientRect()
    if (!rect || rect.width <= 0) return 0
    return Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width))
  }

  return (
    <div
      ref={barRef}
      className="group relative flex h-6 w-full cursor-pointer items-center select-none"
      onPointerDown={(e) => {
        e.preventDefault()
        e.currentTarget.setPointerCapture(e.pointerId)
        setDragging(true)
        const r = calcRatio(e)
        onChange(r)
      }}
      onPointerMove={(e) => {
        const r = calcRatio(e)
        setHoverPct(r)
        if (dragging) onChange(r)
      }}
      onPointerUp={(e) => {
        if (dragging) {
          onChange(calcRatio(e))
          setDragging(false)
        }
        e.currentTarget.releasePointerCapture?.(e.pointerId)
      }}
      onPointerCancel={() => setDragging(false)}
      onPointerLeave={() => {
        if (!dragging) setHoverPct(null)
      }}
    >
      {/* Track base */}
      <div className="relative h-1 w-full rounded-full bg-white/20 transition-all duration-200 group-hover:h-2">
        {/* Buffered progress */}
        {buffered > 0 && (
          <div
            className="absolute inset-y-0 left-0 rounded-full bg-white/30"
            style={{ width: `${Math.min(100, Math.max(0, buffered * 100))}%` }}
          />
        )}
        {/* Played progress */}
        <div
          className="absolute inset-y-0 left-0 rounded-full bg-clay shadow-[0_0_12px_rgba(217,119,87,0.7)]"
          style={{ width: `${Math.min(100, Math.max(0, value * 100))}%` }}
        />
      </div>

      {/* Thumb handle */}
      <div
        className={cn(
          "pointer-events-none absolute size-3.5 -translate-x-1/2 rounded-full bg-white shadow-md transition-all duration-150",
          "scale-0 group-hover:scale-100",
          dragging && "scale-125 ring-4 ring-clay/40"
        )}
        style={{ left: `${Math.min(100, Math.max(0, value * 100))}%` }}
      />

      {/* Hover time tooltip */}
      {hoverPct !== null && duration > 0 && (
        <div
          className="pointer-events-none absolute -top-8 -translate-x-1/2 rounded-md bg-black/90 px-2 py-0.5 text-[11px] font-mono text-white border border-white/10 shadow-xl backdrop-blur-md"
          style={{ left: `${hoverPct * 100}%` }}
        >
          {formatTime(hoverPct * duration)}
        </div>
      )}
    </div>
  )
}

/* Volume popover scrub slider */
function VolumeControl({ muted, volume, onToggleMute, onChangeVolume }) {
  const [hovered, setHovered] = useState(false)

  return (
    <div
      className="relative flex items-center"
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      <button
        type="button"
        onClick={onToggleMute}
        title={muted ? "取消静音 (M)" : "静音 (M)"}
        className="flex size-8 items-center justify-center rounded-full text-white/80 hover:bg-white/15 hover:text-white transition"
      >
        {muted || volume === 0 ? <VolumeX className="size-4" /> : <Volume2 className="size-4" />}
      </button>

      <div
        className={cn(
          "flex items-center overflow-hidden transition-all duration-200",
          hovered ? "w-20 pl-1 pr-2 opacity-100" : "w-0 opacity-0"
        )}
      >
        <input
          type="range"
          min="0"
          max="1"
          step="0.02"
          value={muted ? 0 : volume}
          onChange={(e) => onChangeVolume(Number(e.target.value))}
          className="h-1.5 w-full cursor-pointer appearance-none rounded-full bg-white/25 accent-clay outline-none"
        />
      </div>
    </div>
  )
}

export const PlayerControls = memo(function PlayerControls({
  playing,
  position,
  duration,
  buffered,
  rate,
  muted,
  volume,
  qualities,
  quality,
  danmakuOn,
  drawerOpen,
  fs,
  index,
  totalEpisodes,
  source,
  onTogglePlay,
  onSeekTo,
  onPlayNext,
  onChangeRate,
  onChangeVolume,
  onToggleMute,
  onChangeQuality,
  onToggleDanmaku,
  onToggleDrawer,
  onToggleMini,
  onToggleFullscreen,
}) {
  const [speedMenuOpen, setSpeedMenuOpen] = useState(false)
  const [qualityMenuOpen, setQualityMenuOpen] = useState(false)

  const progressRatio = duration > 0 ? position / duration : 0
  const bufferedRatio = duration > 0 ? buffered / duration : 0

  const btnBaseClass = "text-white/80 hover:bg-white/15 hover:text-white"

  return (
    <div className="relative z-30 flex flex-col bg-gradient-to-t from-black/90 via-black/50 to-transparent px-4 pb-3 pt-1 text-white select-none transition-colors">
      {/* Tier 1: Scrub bar */}
      <ScrubBar
        value={progressRatio}
        buffered={bufferedRatio}
        duration={duration}
        onChange={onSeekTo}
      />

      {/* Tier 2: Action Control Dock */}
      <div className="mt-1 flex items-center justify-between gap-2">
        {/* Left cluster */}
        <div className="flex items-center gap-1.5">
          {/* Big Play / Pause button */}
          <button
            type="button"
            onClick={onTogglePlay}
            title={playing ? "暂停 (空格)" : "播放 (空格)"}
            className="flex size-9 items-center justify-center rounded-full bg-clay text-white shadow-sm transition hover:bg-clay-hover hover:scale-105 active:scale-95"
          >
            {playing ? <Pause className="size-4.5 fill-current" /> : <Play className="size-4.5 fill-current ml-0.5" />}
          </button>

          {/* Next Episode */}
          <button
            type="button"
            disabled={index >= totalEpisodes}
            onClick={onPlayNext}
            title="下一集 (N)"
            className={cn(
              "flex size-8 items-center justify-center rounded-full transition disabled:opacity-30 disabled:hover:bg-transparent",
              btnBaseClass
            )}
          >
            <SkipForward className="size-4" />
          </button>

          {/* Volume */}
          <VolumeControl
            muted={muted}
            volume={volume}
            onToggleMute={onToggleMute}
            onChangeVolume={onChangeVolume}
          />

          {/* Time display */}
          <div className="ml-1 flex items-center text-xs font-mono tracking-tight">
            <span className="font-medium text-white">{formatTime(position)}</span>
            <span className="mx-1 text-white/40">/</span>
            <span className="text-white/60">{formatTime(duration)}</span>
          </div>

          {/* Source indicator */}
          {source && (
            <span className="ml-2 hidden rounded-pill bg-white/10 px-2 py-0.5 text-[10px] text-white/70 sm:inline-block">
              {source}
            </span>
          )}
        </div>

        {/* Right cluster */}
        <div className="flex items-center gap-1.5">
          {/* Danmaku toggle */}
          <button
            type="button"
            onClick={onToggleDanmaku}
            title={danmakuOn ? "关闭弹幕 (D)" : "开启弹幕 (D)"}
            className={cn(
              "flex h-7 items-center gap-1 rounded-pill px-2.5 text-xs font-medium transition",
              danmakuOn
                ? "bg-clay text-white shadow-sm"
                : cn("border border-white/15", btnBaseClass)
            )}
          >
            <Sparkles className="size-3.5" />
            <span>弹幕</span>
          </button>

          {/* Playback speed popover */}
          <div className="relative">
            <button
              type="button"
              onClick={() => setSpeedMenuOpen((v) => !v)}
              className={cn(
                "flex h-7 items-center justify-center rounded-pill px-2.5 text-xs font-mono font-medium transition",
                speedMenuOpen
                  ? "bg-clay text-white"
                  : btnBaseClass
              )}
            >
              {rate === 1 ? "倍速" : `${rate}×`}
            </button>

            {speedMenuOpen && (
              <div className="absolute bottom-9 right-0 z-50 flex flex-col gap-0.5 rounded-lg bg-black/90 p-1 shadow-2xl backdrop-blur-md border border-white/15 min-w-[70px]">
                {PLAYER_RATES.map((r) => (
                  <button
                    key={r}
                    type="button"
                    onClick={() => {
                      onChangeRate(r)
                      setSpeedMenuOpen(false)
                    }}
                    className={cn(
                      "flex items-center justify-center rounded px-3 py-1.5 text-xs font-mono transition",
                      rate === r ? "bg-clay font-bold text-white" : "text-white/80 hover:bg-white/15 hover:text-white"
                    )}
                  >
                    {r}×
                  </button>
                ))}
              </div>
            )}
          </div>

          {/* Quality selector if multiple qualities exist */}
          {qualities && qualities.length > 1 && (
            <div className="relative">
              <button
                type="button"
                onClick={() => setQualityMenuOpen((v) => !v)}
                className={cn(
                  "flex h-7 items-center rounded-pill px-2 text-xs font-medium transition",
                  btnBaseClass
                )}
              >
                {qualities.find((q) => q.value === quality)?.label || "画质"}
              </button>

              {qualityMenuOpen && (
                <div className="absolute bottom-9 right-0 z-50 flex flex-col gap-0.5 rounded-lg bg-black/90 p-1 shadow-2xl backdrop-blur-md border border-white/15 min-w-[90px]">
                  {qualities.map((q) => (
                    <button
                      key={q.value}
                      type="button"
                      onClick={() => {
                        onChangeQuality(q.value)
                        setQualityMenuOpen(false)
                      }}
                      className={cn(
                        "flex items-center justify-center rounded px-3 py-1.5 text-xs transition",
                        quality === q.value ? "bg-clay font-medium text-white" : "text-white/80 hover:bg-white/15 hover:text-white"
                      )}
                    >
                      {q.label}
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Episode list drawer toggle */}
          {totalEpisodes > 0 && (
            <button
              type="button"
              onClick={onToggleDrawer}
              title="选集"
              className={cn(
                "flex h-7 items-center gap-1.5 rounded-pill px-2.5 text-xs font-medium transition",
                drawerOpen
                  ? "bg-clay text-white shadow-sm"
                  : btnBaseClass
              )}
            >
              <ListVideo className="size-3.5" />
              <span>
                选集 {index}/{totalEpisodes}
              </span>
            </button>
          )}

          {/* PiP */}
          {typeof document !== "undefined" && document.pictureInPictureEnabled && (
            <button
              type="button"
              onClick={() => {
                const v = document.querySelector("video")
                v?.requestPictureInPicture?.().catch(() => {})
              }}
              title="画中画"
              className={cn("flex size-8 items-center justify-center rounded-full transition", btnBaseClass)}
            >
              <PictureInPicture2 className="size-4" />
            </button>
          )}

          {/* Mini Window Toggle */}
          <button
            type="button"
            onClick={onToggleMini}
            title="小窗播放 (I)"
            className={cn("flex size-8 items-center justify-center rounded-full transition", btnBaseClass)}
          >
            <Minimize2 className="size-4" />
          </button>

          {/* Fullscreen Toggle */}
          <button
            type="button"
            onClick={onToggleFullscreen}
            title={fs ? "退出全屏 (F / Esc)" : "全屏 (F)"}
            className={cn("flex size-8 items-center justify-center rounded-full transition", btnBaseClass)}
          >
            {fs ? <Minimize className="size-4" /> : <Maximize className="size-4" />}
          </button>
        </div>
      </div>
    </div>
  )
})
