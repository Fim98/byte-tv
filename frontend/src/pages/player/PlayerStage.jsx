import React, { memo } from "react"
import { AlertCircle, Loader2, Play, RefreshCw, FastForward, Volume2, VolumeX } from "lucide-react"
import { cn } from "../../lib/utils"
import { DanmakuCanvas } from "./DanmakuCanvas"

export const PlayerStage = memo(function PlayerStage({
  videoRef,
  position,
  playing,
  loading,
  error,
  boost,
  status,
  danmakuOn,
  danmaku,
  hud,
  nextCountdown,
  onPlayNextNow,
  onCancelAutoNext,
  onRetry,
  onTogglePlay,
  onApply,
  onApplyMeta,
  onPlay,
  onPause,
  onPlaying,
  onWaiting,
  onEnded,
  onTimeUpdate,
  autoNext,
  index,
  totalEpisodes,
}) {
  return (
    <div className="relative size-full overflow-hidden bg-black flex items-center justify-center select-none">
      {/* Ambient background glow for high-end cinema feel */}
      <div className="pointer-events-none absolute inset-0 opacity-20 blur-3xl scale-125 bg-gradient-to-tr from-clay/20 via-transparent to-blue-500/10" />

      {/* Main Video element */}
      <video
        ref={videoRef}
        playsInline
        className="relative z-[2] size-full object-contain"
        onLoadStart={onApply}
        onLoadedMetadata={onApplyMeta}
        onDurationChange={onApply}
        onPlay={onPlay}
        onPause={onPause}
        onPlaying={onPlaying}
        onWaiting={onWaiting}
        onEnded={onEnded}
        onTimeUpdate={onTimeUpdate}
      />

      {/* High-performance Canvas Danmaku layer (pauses when video is paused) */}
      <DanmakuCanvas
        danmaku={danmaku}
        videoRef={videoRef}
        playing={playing}
        danmakuOn={danmakuOn}
      />

      {/* Center Big Play Button when paused */}
      {!playing && !loading && !error && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            onTogglePlay()
          }}
          title="播放 (空格)"
          className="group/center-play absolute left-1/2 top-1/2 z-20 flex size-16 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full bg-black/60 text-white shadow-2xl backdrop-blur-md border border-white/20 transition-all duration-200 hover:scale-110 hover:bg-clay active:scale-95"
        >
          <Play className="size-7 fill-current ml-1 text-white transition-transform group-hover/center-play:scale-110" />
        </button>
      )}

      {/* Loading & Buffering indicator */}
      {loading && !error && (
        <div className="pointer-events-none absolute inset-0 z-10 flex flex-col items-center justify-center bg-black/40 backdrop-blur-xs transition-opacity duration-300">
          <div className="relative flex items-center justify-center">
            <Loader2 className="size-11 animate-spin text-clay" strokeWidth={2.2} />
            <div className="absolute size-5 rounded-full bg-clay/20 animate-ping" />
          </div>
          <span className="mt-3.5 text-xs font-medium tracking-wider text-white/80 [text-shadow:0_1px_3px_rgba(0,0,0,0.8)]">
            {status || "正在加载视频…"}
          </span>
        </div>
      )}

      {/* Boost 3x banner */}
      {boost && (
        <div className="pointer-events-none absolute top-6 z-20 flex items-center gap-2 rounded-full bg-black/75 px-4 py-1.5 text-xs font-medium text-white shadow-lg backdrop-blur-md border border-white/10 animate-pulse">
          <FastForward className="size-4 text-clay" />
          <span>长按 3× 超快进</span>
        </div>
      )}

      {/* HUD Notification Pill (Volume / Seek Feedback) */}
      {hud && (
        <div className="pointer-events-none absolute inset-0 z-20 flex items-center justify-center">
          <div className="flex items-center gap-2.5 rounded-full bg-black/80 px-5 py-2.5 text-sm font-semibold text-white shadow-2xl backdrop-blur-md border border-white/15 animate-fade-up">
            {hud.type === "volume" && (
              <>
                {hud.value === 0 ? <VolumeX className="size-5 text-white/70" /> : <Volume2 className="size-5 text-clay" />}
                <span>音量 {Math.round(hud.value * 100)}%</span>
              </>
            )}
            {hud.type === "seek" && (
              <>
                <FastForward className={cn("size-5 text-clay", hud.delta < 0 && "rotate-180")} />
                <span>{hud.delta > 0 ? `+${hud.delta}s` : `${hud.delta}s`}</span>
              </>
            )}
          </div>
        </div>
      )}

      {/* Next Episode Countdown Toast */}
      {nextCountdown > 0 && autoNext && index < totalEpisodes && (
        <div className="absolute bottom-24 right-8 z-30 flex items-center gap-3 rounded-card bg-surface-popover/90 p-3 shadow-2xl backdrop-blur-md border border-white/10 animate-fade-up">
          <div className="flex flex-col">
            <span className="text-xs font-semibold text-white">即将播放下一集</span>
            <span className="text-[11px] text-white/60">{nextCountdown} 秒后自动跳转</span>
          </div>
          <button
            type="button"
            onClick={onPlayNextNow}
            className="rounded-pill bg-clay px-3 py-1.5 text-xs font-medium text-white transition hover:bg-clay-hover"
          >
            立即播放
          </button>
          <button
            type="button"
            onClick={onCancelAutoNext}
            className="text-xs text-white/50 hover:text-white"
          >
            取消
          </button>
        </div>
      )}

      {/* Error Card Overlay */}
      {error && (
        <div className="absolute inset-0 z-20 flex items-center justify-center bg-black/75 backdrop-blur-sm p-4">
          <div className="flex w-full max-w-sm flex-col items-center rounded-2xl bg-surface-2 p-6 text-center shadow-popover border border-white/10">
            <div className="flex size-12 items-center justify-center rounded-full bg-danger/15 text-danger mb-3">
              <AlertCircle className="size-6" />
            </div>
            <h4 className="text-sm font-semibold text-ink">播放遇到问题</h4>
            <p className="mt-1 text-xs text-ink-secondary leading-relaxed">{error}</p>
            <div className="mt-5 flex gap-2.5">
              <button
                type="button"
                onClick={onRetry}
                className="flex items-center gap-1.5 rounded-pill bg-clay px-4 py-2 text-xs font-medium text-white shadow-sm hover:bg-clay-hover transition"
              >
                <RefreshCw className="size-3.5" />
                重试播放
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
})
