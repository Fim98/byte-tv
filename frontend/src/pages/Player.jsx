import React, { useCallback, useEffect, useRef, useState } from "react"
import { ChevronLeft, Info, MoreHorizontal, PictureInPicture2, Sparkles, X } from "lucide-react"
import { apiGet, native } from "../api"
import { cn } from "../lib/utils"
import { SidebarToggleIcon } from "../App"
import { usePlayback } from "./player/usePlayback"
import { PlayerStage } from "./player/PlayerStage"
import { PlayerControls } from "./player/PlayerControls"
import { PlayerDrawer } from "./player/PlayerDrawer"
import { PlayerMiniOverlay } from "./player/PlayerMiniOverlay"

export default function Player({ request, mini, onMiniChange, onClose, sidebarCollapsed, onToggleSidebar }) {
  const rootRef = useRef(null)
  const clickTimerRef = useRef(null)
  const holdTimerRef = useRef(null)
  const hudTimerRef = useRef(null)

  // Use core playback hook
  const playback = usePlayback(request)
  const {
    videoRef,
    opened,
    index,
    episodes,
    position,
    duration,
    buffered,
    playing,
    loading,
    status,
    error,
    source,
    rate,
    muted,
    volume,
    boost,
    qualities,
    quality,
    videoAspect,
    sessionRef,
    togglePlay,
    seekTo,
    seekBy,
    changeRate,
    changeVolume,
    toggleMute,
    changeQuality,
    playEpisode,
    setBoostMode,
    saveProgress,
    applyMeta,
    apply,
    setPlaying,
    setPosition,
    setDuration,
    setBuffered,
    setError,
    setStatus,
  } = playback

  // Local UI states
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [danmakuOn, setDanmakuOn] = useState(() => localStorage.getItem("juku.danmaku") !== "0")
  const [danmaku, setDanmaku] = useState([])
  const [autoNext, setAutoNext] = useState(() => localStorage.getItem("juku.autonext") !== "0")
  const [fs, setFs] = useState(() => Boolean(typeof document !== "undefined" && document.fullscreenElement))
  const [idle, setIdle] = useState(false)
  const [hud, setHud] = useState(null)
  const [cancelNextThisEp, setCancelNextThisEp] = useState(false)

  // Persist settings
  useEffect(() => {
    localStorage.setItem("juku.danmaku", danmakuOn ? "1" : "0")
  }, [danmakuOn])

  useEffect(() => {
    localStorage.setItem("juku.autonext", autoNext ? "1" : "0")
  }, [autoNext])

  // HUD notification helper
  const showHud = useCallback((info) => {
    setHud(info)
    clearTimeout(hudTimerRef.current)
    hudTimerRef.current = setTimeout(() => setHud(null), 1200)
  }, [])

  // Fullscreen toggle & synchronization
  const toggleFullscreen = useCallback(() => {
    if (native?.toggleFullScreen) {
      native.toggleFullScreen()
    } else if (document.fullscreenElement) {
      document.exitFullscreen()
    } else {
      rootRef.current?.requestFullscreen?.()
    }
    setFs((prev) => !prev)
  }, [])

  useEffect(() => {
    const syncFs = () => setFs(Boolean(document.fullscreenElement))
    document.addEventListener("fullscreenchange", syncFs)
    const syncNative = () => {
      const full = screen.width === window.innerWidth && screen.height === window.innerHeight
      setFs((prev) => (document.fullscreenElement ? true : full || prev))
    }
    window.addEventListener("resize", syncNative)
    return () => {
      document.removeEventListener("fullscreenchange", syncFs)
      window.removeEventListener("resize", syncNative)
    }
  }, [])

  // Esc hierarchical retreat
  const handleEscape = useCallback(() => {
    if (document.fullscreenElement) {
      document.exitFullscreen()
      setFs(false)
      return
    }
    if (fs) {
      native?.toggleFullScreen?.()
      setFs(false)
      return
    }
    if (drawerOpen) {
      setDrawerOpen(false)
      return
    }
    if (!mini) {
      onMiniChange(true)
      return
    }
    onClose()
  }, [fs, drawerOpen, mini, onMiniChange, onClose])

  // Danmaku fetching (60s rolling window)
  useEffect(() => {
    const ep = episodes[index - 1]
    if (!danmakuOn || !ep?.danmaku || !opened) return
    const windowStart = Math.floor(position / 60) * 60000
    const controller = new AbortController()
    const q = new URLSearchParams({
      session: sessionRef.current,
      episode: String(index),
      start: String(windowStart),
      duration: "60000",
    })
    apiGet(`/api/ui/playback/danmaku?${q}`, { signal: controller.signal })
      .then((page) => setDanmaku((page.items || []).slice(0, 300)))
      .catch(() => {})
    return () => controller.abort()
  }, [danmakuOn, index, Math.floor(position / 60), opened, episodes, sessionRef])

  // Auto-next countdown calculation
  const remainingTime = duration > 0 ? duration - position : 0
  const isNearEnd = duration > 0 && remainingTime <= 3.5 && remainingTime > 0.3
  const nextCountdown = isNearEnd && !cancelNextThisEp && autoNext && index < episodes.length
    ? Math.max(1, Math.ceil(remainingTime))
    : 0

  // Keyboard navigation
  useEffect(() => {
    const onKeyDown = (e) => {
      if (e.target.tagName === "INPUT" || e.target.tagName === "TEXTAREA") return
      const k = e.key
      if (k === " ") {
        e.preventDefault()
        togglePlay()
        showHud({ type: "playPause", playing: !playing })
      } else if (k === "ArrowRight") {
        e.preventDefault()
        if (!e.repeat) {
          holdTimerRef.current = setTimeout(() => {
            setBoostMode(true)
          }, 350)
        }
      } else if (k === "ArrowLeft") {
        e.preventDefault()
        seekBy(-5)
        showHud({ type: "seek", delta: -5 })
      } else if (k === "ArrowUp") {
        e.preventDefault()
        const newV = Math.min(1, volume + 0.05)
        changeVolume(newV)
        showHud({ type: "volume", value: newV })
      } else if (k === "ArrowDown") {
        e.preventDefault()
        const newV = Math.max(0, volume - 0.05)
        changeVolume(newV)
        showHud({ type: "volume", value: newV })
      } else if (k === "f" || k === "F") {
        toggleFullscreen()
      } else if (k === "i" || k === "I") {
        onMiniChange(!mini)
      } else if (k === "n" || k === "N") {
        if (index < episodes.length) playEpisode(index + 1)
      } else if (k === "p" || k === "P") {
        if (index > 1) playEpisode(index - 1)
      } else if (k === "d" || k === "D") {
        setDanmakuOn((v) => !v)
      } else if (k === "m" || k === "M") {
        toggleMute()
      } else if (k === "Escape") {
        handleEscape()
      }
    }

    const onKeyUp = (e) => {
      if (e.key === "ArrowRight") {
        clearTimeout(holdTimerRef.current)
        if (boost) {
          setBoostMode(false)
        } else {
          seekBy(5)
          showHud({ type: "seek", delta: 5 })
        }
      }
    }

    window.addEventListener("keydown", onKeyDown)
    window.addEventListener("keyup", onKeyUp)
    return () => {
      window.removeEventListener("keydown", onKeyDown)
      window.removeEventListener("keyup", onKeyUp)
    }
  }, [
    playing,
    volume,
    boost,
    index,
    episodes.length,
    mini,
    togglePlay,
    seekBy,
    changeVolume,
    setBoostMode,
    toggleFullscreen,
    onMiniChange,
    playEpisode,
    toggleMute,
    handleEscape,
    showHud,
  ])

  // Mouse idle detection (auto hide controls after 3.5s)
  useEffect(() => {
    if (mini) return
    let timer = setTimeout(() => setIdle(true), 3500)
    const onMove = () => {
      setIdle(false)
      clearTimeout(timer)
      timer = setTimeout(() => setIdle(true), 3500)
    }
    window.addEventListener("mousemove", onMove)
    return () => {
      window.removeEventListener("mousemove", onMove)
      clearTimeout(timer)
    }
  }, [mini])

  // Click on stage: debounce single click (play/pause) vs double click (fullscreen)
  const handleStageClick = () => {
    if (clickTimerRef.current) {
      clearTimeout(clickTimerRef.current)
      clickTimerRef.current = null
      toggleFullscreen()
    } else {
      clickTimerRef.current = setTimeout(() => {
        clickTimerRef.current = null
        togglePlay()
        showHud({ type: "playPause", playing: !playing })
      }, 250)
    }
  }

  // Video event handlers
  const handleTimeUpdate = (e) => {
    const v = e.currentTarget
    setPosition(v.currentTime || 0)
    if (Number.isFinite(v.duration)) setDuration(v.duration)
    if (v.buffered.length > 0) {
      setBuffered(v.buffered.end(v.buffered.length - 1))
    }
    if (v.paused && playing) {
      setPlaying(false)
    } else if (!v.paused && !playing) {
      setPlaying(true)
    }
  }

  const currentEp = episodes[index - 1]
  const dramaTitle = opened?.title || request.title || "正在播放"
  const episodeTitle = currentEp ? `第 ${currentEp.episode || index} 集` : `第 ${index} 集`
  const hideControls = idle && playing && !mini && !drawerOpen

  return (
    <div
      ref={rootRef}
      className={cn(
        mini
          ? "absolute bottom-5 right-5 z-[60] w-[260px] overflow-hidden rounded-xl bg-surface-1 shadow-2xl border border-subtle animate-fade-up"
          : "relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-surface-0 text-ink",
        !mini && fs && "fixed inset-0 z-50 bg-black text-white"
      )}
      style={mini ? { aspectRatio: videoAspect > 0 ? videoAspect : 16 / 9 } : undefined}
    >
      {/* Top Header Bar (Full mode only) */}
      {!mini && (
        <header
          className={cn(
            "drag relative z-30 flex h-14 flex-none items-center justify-between px-4 transition-all duration-300",
            fs
              ? "bg-gradient-to-b from-black/80 via-black/40 to-transparent text-white"
              : "bg-surface-1/90 border-b border-hairline text-ink backdrop-blur-md",
            sidebarCollapsed && !fs && "main-header-collapsed",
            hideControls && "pointer-events-none opacity-0"
          )}
        >
          {/* Left: Back & Title info */}
          <div className="no-drag flex items-center gap-2.5 min-w-0">
            {sidebarCollapsed && !fs && (
              <button
                type="button"
                onClick={onToggleSidebar}
                title="展开侧栏 (⌘B)"
                className="flex size-8 items-center justify-center rounded-full bg-surface-2 text-ink hover:bg-surface-3 border border-subtle transition -ml-1 mr-0.5"
              >
                <SidebarToggleIcon collapsed={true} className="size-4" />
              </button>
            )}

            <button
              type="button"
              onClick={onClose}
              title="返回 (Esc)"
              className={cn(
                "flex size-8 items-center justify-center rounded-full transition",
                fs
                  ? "bg-white/10 text-white/90 backdrop-blur-md hover:bg-white/25 hover:text-white"
                  : "bg-surface-2 text-ink hover:bg-surface-3 border border-subtle"
              )}
            >
              <ChevronLeft className="size-5" />
            </button>

            <div className="flex flex-col min-w-0">
              <div className="flex items-center gap-2">
                <span className={cn("truncate text-sm font-semibold", fs ? "text-white drop-shadow" : "text-ink")}>
                  {dramaTitle}
                </span>
                <span
                  className={cn(
                    "rounded-full px-2 py-0.5 text-[11px] font-medium",
                    fs ? "bg-white/15 text-white/90" : "bg-surface-2 text-ink-secondary border border-subtle"
                  )}
                >
                  {episodeTitle}
                </span>
              </div>
            </div>
          </div>

          {/* Right: Badges & Drawer button */}
          <div className="no-drag flex items-center gap-2">
            {danmakuOn && (
              <span
                className={cn(
                  "flex items-center gap-1 rounded-pill px-2.5 py-1 text-xs",
                  fs ? "bg-white/10 text-white/80" : "bg-surface-2 text-ink-secondary border border-subtle"
                )}
              >
                <Sparkles className="size-3.5 text-clay" />
                <span>{danmaku.length} 弹幕</span>
              </span>
            )}

            <button
              type="button"
              onClick={() => setDrawerOpen((v) => !v)}
              title="选集列表"
              className={cn(
                "flex h-8 items-center gap-1.5 rounded-pill px-3 text-xs font-medium transition",
                drawerOpen
                  ? "bg-clay text-white shadow-sm"
                  : fs
                  ? "bg-white/10 text-white/90 hover:bg-white/20"
                  : "bg-surface-2 text-ink hover:bg-surface-3 border border-subtle"
              )}
            >
              选集 ({index}/{episodes.length || 1})
            </button>
          </div>
        </header>
      )}

      {/* Main Video Stage Area */}
      <div
        className={cn("relative min-h-0 flex-1 overflow-hidden", mini && "size-full cursor-pointer")}
        onClick={mini ? () => onMiniChange(false) : handleStageClick}
      >
        <PlayerStage
          videoRef={videoRef}
          position={position}
          playing={playing}
          loading={loading}
          error={error}
          boost={boost}
          status={status}
          danmakuOn={!mini && danmakuOn}
          danmaku={danmaku}
          hud={hud}
          nextCountdown={nextCountdown}
          onPlayNextNow={() => playEpisode(index + 1)}
          onCancelAutoNext={() => setCancelNextThisEp(true)}
          onRetry={() => playEpisode(index, position, true)}
          onTogglePlay={togglePlay}
          onApply={apply}
          onApplyMeta={applyMeta}
          onPlay={() => {
            setPlaying(true)
            setStatus("正在播放")
          }}
          onPause={() => {
            setPlaying(false)
            setStatus("已暂停")
            saveProgress(false)
          }}
          onPlaying={() => {
            setPlaying(true)
            setLoading(false)
            setStatus("正在播放")
            setError("")
          }}
          onWaiting={() => setStatus("缓冲中…")}
          onEnded={() => {
            setPlaying(false)
            saveProgress(true)
            if (autoNext && index < episodes.length) {
              playEpisode(index + 1)
            }
          }}
          onSaveProgress={saveProgress}
          onTimeUpdate={handleTimeUpdate}
          autoNext={autoNext}
          index={index}
          totalEpisodes={episodes.length}
        />

        {/* Mini Player Overlay */}
        {mini && (
          <PlayerMiniOverlay
            title={dramaTitle}
            index={index}
            playing={playing}
            progress={duration > 0 ? position / duration : 0}
            onTogglePlay={togglePlay}
            onExpand={() => onMiniChange(false)}
            onClose={onClose}
            videoRef={videoRef}
          />
        )}
      </div>

      {/* Bottom Controls Bar (Full mode only) */}
      {!mini && (
        <div
          className={cn(
            "relative z-30 flex-none transition-opacity duration-300",
            hideControls && "pointer-events-none opacity-0"
          )}
        >
          <PlayerControls
            playing={playing}
            position={position}
            duration={duration}
            buffered={buffered}
            rate={rate}
            muted={muted}
            volume={volume}
            qualities={qualities}
            quality={quality}
            danmakuOn={danmakuOn}
            drawerOpen={drawerOpen}
            fs={fs}
            index={index}
            totalEpisodes={episodes.length}
            source={source}
            onTogglePlay={togglePlay}
            onSeekTo={seekTo}
            onPlayNext={() => index < episodes.length && playEpisode(index + 1)}
            onChangeRate={changeRate}
            onChangeVolume={changeVolume}
            onToggleMute={toggleMute}
            onChangeQuality={changeQuality}
            onToggleDanmaku={() => setDanmakuOn((v) => !v)}
            onToggleDrawer={() => setDrawerOpen((v) => !v)}
            onToggleMini={() => onMiniChange(true)}
            onToggleFullscreen={toggleFullscreen}
          />
        </div>
      )}

      {/* Episode Drawer Panel (Slide-over) */}
      {!mini && (
        <PlayerDrawer
          open={drawerOpen}
          episodes={episodes}
          currentIndex={index}
          autoNext={autoNext}
          onClose={() => setDrawerOpen(false)}
          onSelectEpisode={(epIndex) => {
            playEpisode(epIndex)
            setCancelNextThisEp(false)
          }}
          onToggleAutoNext={() => setAutoNext((v) => !v)}
        />
      )}
    </div>
  )
}
