import { useCallback, useEffect, useRef, useState } from "react"
import Hls from "hls.js"
import {
  ChevronLeft, Expand, Maximize, Minimize, MoreHorizontal, Pause, PictureInPicture2,
  Play, SkipBack, SkipForward, Volume2, VolumeX, X,
} from "lucide-react"
import { apiGet, apiPost, native } from "../api"
import { isDesktopShell, withMediaToken } from "../lib/desktop"
import { formatTime } from "../lib/format"
import { PLAYER_RATES, applyPlayerPrefs, getPlayerPrefs, setPlayerPrefs } from "../lib/playerPrefs"
import { cn } from "../lib/utils"

// 播放器：完整复用 /api/ui/playback/* 协议（open → plan / hls/open → progress / control / danmaku / prefetch）。
// 结构：竖屏画面居中 + 上下主题化控件栏。影像本体恒为暗调，周边留白随主题走。
// Esc 逐层返回：全屏 → 收起抽屉 → 收成迷你窗（继续播）→ 关闭。
const RATES = PLAYER_RATES
const PAGE = 30

function capabilities(video) {
  const mse = typeof window.MediaSource !== "undefined"
  const can = (type) => (mse && window.MediaSource.isTypeSupported(type)) || video.canPlayType(type) !== ""
  const codecs = ["h264"]
  if (can('video/mp4; codecs="hvc1.1.6.L93.B0"') || can('video/mp4; codecs="hev1.1.6.L93.B0"')) codecs.push("hevc")
  if (can('video/mp4; codecs="av01.0.05M.08"')) codecs.push("av1")
  return {
    mp4: true,
    nativeHls: !(isDesktopShell() && Hls.isSupported()) && video.canPlayType("application/vnd.apple.mpegurl") !== "",
    hlsjs: Hls.isSupported(),
    video: codecs,
    audio: ["aac", "mp3", "opus"],
  }
}

/* 控件按钮：与主题同色系，压在影像上的那组除外 */
function Ctrl({ active, dim, className, children, ...props }) {
  return (
    <button
      type="button"
      className={cn(
        "flex h-[30px] items-center gap-1.5 rounded-sm px-2.5 text-xs text-ink-secondary",
        "transition-[background-color,color] duration-[160ms] ease-out",
        "hover:bg-[var(--cds-fill-ghost-hover)] hover:text-ink",
        "focus-visible:outline-none focus-visible:shadow-focus disabled:opacity-40",
        active && "bg-[var(--cds-fill-ghost-selected)] text-ink",
        dim && "text-ink-muted",
        className,
      )}
      {...props}
    >
      {children}
    </button>
  )
}

function SquareCtrl({ className, children, ...props }) {
  return <Ctrl className={cn("w-[30px] justify-center px-0", className)} {...props}>{children}</Ctrl>
}

export default function Player({ request, onClose }) {
  const videoRef = useRef(null)
  const rootRef = useRef(null)
  const hlsRef = useRef(null)
  const versionRef = useRef(0)
  const sequenceRef = useRef(0)
  const runRef = useRef(0)
  const indexRef = useRef(1)
  const sessionRef = useRef("")
  const retryRef = useRef(0)
  const requestSeqRef = useRef(0) // 服务端要求每次 plan / hls/open 的 version 单调递增
  const [opened, setOpened] = useState(null)
  const [index, setIndex] = useState(1)
  const [position, setPosition] = useState(0)
  const [duration, setDuration] = useState(0)
  const [buffered, setBuffered] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [status, setStatus] = useState("正在准备播放")
  const [error, setError] = useState("")
  const [source, setSource] = useState("")
  const [rate, setRate] = useState(() => getPlayerPrefs().rate)
  const [muted, setMuted] = useState(() => getPlayerPrefs().muted)
  const [volume, setVolume] = useState(() => getPlayerPrefs().volume)
  const boostRef = useRef(false)
  const holdTimerRef = useRef(null) // 长按 → 计时器放 ref，避免播放中频繁重渲染时被清掉
  const apply = () => applyPlayerPrefs(videoRef.current, { boost: boostRef.current })
  const [drawer, setDrawer] = useState(true)
  const [drawerPage, setDrawerPage] = useState(null) // null = 跟随当前集所在分页
  const [jumpText, setJumpText] = useState("")
  const [danmakuOn, setDanmakuOn] = useState(() => localStorage.getItem("juku.danmaku") !== "0")
  const [danmaku, setDanmaku] = useState([])
  const [autoNext, setAutoNext] = useState(() => localStorage.getItem("juku.autonext") !== "0")
  const [idle, setIdle] = useState(false)
  const [boost, setBoost] = useState(false)
  const [qualities, setQualities] = useState([])
  const [quality, setQuality] = useState(0) // 本集实际画质；用户偏好在 playerPrefs.quality
  const [mini, setMini] = useState(false)   // 收起为右下角迷你窗
  // 原生全屏状态：全屏时交通灯隐藏，浮层可贴顶；非全屏需让出 macOS 交通灯位
  const [fs, setFs] = useState(() => Boolean(document.fullscreenElement))
  const [menu, setMenu] = useState(false)
  const episodes = opened?.episodes || []

  const post = useCallback((path, body) => apiPost(path, { session: sessionRef.current, ...body }), [])

  const saveProgress = useCallback(async (completed) => {
    const video = videoRef.current
    if (!video || !runRef.current || !sessionRef.current) return
    const current = video.currentTime || 0
    const length = Number.isFinite(video.duration) && video.duration > 0 ? video.duration : 0
    sequenceRef.current += 1
    try {
      await post("/api/ui/playback/progress", {
        progress: {
          run: runRef.current, sequence: sequenceRef.current, episode: indexRef.current,
          position: current, duration: length,
          completed: Boolean(completed && length && current >= length - 2),
        },
      })
    } catch {
      /* 单次进度失败不打断播放 */
    }
  }, [post])

  const clearMedia = () => {
    hlsRef.current?.destroy()
    hlsRef.current = null
    const video = videoRef.current
    if (video) { video.removeAttribute("src"); video.load() }
  }

  const attach = (url, offset, shouldPlay, version) => {
    const video = videoRef.current
    const playlist = /\.m3u8(\?|$)/.test(url)
    const start = () => {
      if (version !== versionRef.current) return
      if (Number.isFinite(video.duration)) setDuration(video.duration)
      if (offset > 0) video.currentTime = offset
      applyPlayerPrefs(video, { boost: boostRef.current })
      setStatus(shouldPlay ? "正在播放" : "已暂停，点击继续")
      if (shouldPlay) video.play().catch(() => setStatus("已就绪，点击播放"))
    }
    clearMedia()
    // 桌面外壳里有 MSE 时一律用 hls.js：WKWebView 原生 HLS 走 AVFoundation，请求不带 Cookie 会 401。
    if (playlist && (isDesktopShell() || !video.canPlayType("application/vnd.apple.mpegurl")) && Hls.isSupported()) {
      const hls = new Hls({
        maxBufferLength: 30, backBufferLength: 60,
        fragLoadingMaxRetry: 6, manifestLoadingMaxRetry: 4,
        xhrSetup: (xhr) => { xhr.withCredentials = true },
      })
      hlsRef.current = hls
      hls.on(Hls.Events.MANIFEST_PARSED, start)
      hls.on(Hls.Events.ERROR, (_, data) => {
        if (!data?.fatal) return
        if (data.type === Hls.ErrorTypes.MEDIA_ERROR && retryRef.current < 2) {
          retryRef.current++
          hls.recoverMediaError()
          return
        }
        setError("播放列表加载失败，可重试")
      })
      hls.loadSource(url)
      hls.attachMedia(video)
      hls.on(Hls.Events.MEDIA_ATTACHED, () => applyPlayerPrefs(video, { boost: boostRef.current }))
      applyPlayerPrefs(video, { boost: boostRef.current })
      return
    }
    video.addEventListener("loadedmetadata", start, { once: true })
    video.src = withMediaToken(url)
    video.load()
    applyPlayerPrefs(video, { boost: boostRef.current })
  }

  const playEpisode = useCallback(async (nextIndex, offset = 0, shouldPlay = true, nextQuality = getPlayerPrefs().quality) => {
    const version = ++versionRef.current
    await saveProgress(false)
    indexRef.current = nextIndex
    retryRef.current = 0
    setIndex(nextIndex)
    setError("")
    setDanmaku([])
    setStatus(offset > 0 ? "正在跳转到上次位置" : "正在准备播放")
    const video = videoRef.current
    try {
      if (opened?.mode === "collection") await post("/api/ui/playback/prepare", { episode: nextIndex })
      if (version !== versionRef.current) return
      const plan = await post("/api/ui/playback/plan", {
        episode: nextIndex, start: offset, quality: nextQuality,
        version: ++requestSeqRef.current, mode: "auto", client: capabilities(video),
      })
      if (version !== versionRef.current) return
      let result = plan
      if (plan.player === "legacy" || !plan.url) {
        result = await post("/api/ui/playback/hls/open", {
          episode: nextIndex, start: offset, quality: nextQuality, version: ++requestSeqRef.current,
        })
        if (version !== versionRef.current) return
      }
      runRef.current = Number(result.run) || 0
      if (result.duration) setDuration(Number(result.duration))
      setQualities(result.qualities || [])
      if (result.quality) setQuality(result.quality)
      setSource(result.source === "local" || result.source === "collection" ? "本集播放本地已完成文件" : "在线播放")
      attach(result.url, offset, shouldPlay, version)
      // 预缓存下一集（后端按资源限制决定是否执行）
      if (nextIndex < episodes.length) {
        post("/api/ui/playback/prefetch", { episode: nextIndex + 1, run: runRef.current, version: requestSeqRef.current }).catch(() => {})
      }
    } catch (err) {
      if (version === versionRef.current) setError(err.message || "播放失败")
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [opened, saveProgress, post, episodes.length])

  // 打开会话
  useEffect(() => {
    let cancelled = false
    const body = request.taskId
      ? { taskId: request.taskId }
      : { dramaId: request.dramaId, resume: Boolean(request.resume), fromHistory: Boolean(request.fromHistory) }
    apiPost("/api/ui/playback/open", body)
      .then((result) => {
        if (cancelled) {
          apiPost("/api/ui/playback/control", { session: result.session, action: "close" }).catch(() => {})
          return
        }
        sessionRef.current = result.session
        setOpened(result)
      })
      .catch((err) => !cancelled && setError(err.message))
    return () => {
      cancelled = true
      versionRef.current++
      const session = sessionRef.current
      if (session) {
        saveProgress(false).finally(() => apiPost("/api/ui/playback/control", { session, action: "close" }).catch(() => {}))
      }
      hlsRef.current?.destroy()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [request])

  useEffect(() => {
    if (!opened) return
    const first = request.episode || opened.initialIndex || 1
    const offset = request.episode ? 0 : opened.initialPosition || 0
    playEpisode(first, offset, !(opened.resumePaused && !request.episode))
    if (opened.resumeMessage) setStatus(opened.resumeMessage)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [opened])

  // 心跳保活 + 进度
  useEffect(() => {
    if (!opened) return
    const beat = setInterval(
      () => post("/api/ui/playback/control", { action: "heartbeat" }).catch((e) => e.status === 410 && setError("播放会话已过期，请返回后重新打开")),
      20000,
    )
    const progress = setInterval(() => saveProgress(false), 5000)
    const onVisible = () => document.visibilityState === "visible" && post("/api/ui/playback/control", { action: "heartbeat" }).catch(() => {})
    document.addEventListener("visibilitychange", onVisible)
    return () => { clearInterval(beat); clearInterval(progress); document.removeEventListener("visibilitychange", onVisible) }
  }, [opened, post, saveProgress])

  // 弹幕：按 60 秒窗口加载
  useEffect(() => {
    const ep = episodes[index - 1]
    if (!danmakuOn || !ep?.danmaku || !opened) return
    const windowStart = Math.floor(position / 60) * 60000
    const controller = new AbortController()
    const q = new URLSearchParams({
      session: sessionRef.current, episode: String(index),
      start: String(windowStart), duration: "60000",
    })
    apiGet(`/api/ui/playback/danmaku?${q}`, { signal: controller.signal })
      .then((page) => setDanmaku((page.items || []).slice(0, 300))).catch(() => {})
    return () => controller.abort()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [danmakuOn, index, Math.floor(position / 60), opened])
  const visibleDanmaku = danmakuOn
    ? danmaku.filter((d) => d.timeMs / 1000 <= position && d.timeMs / 1000 > position - 6).slice(-12)
    : []

  // Media Session
  useEffect(() => {
    if (!("mediaSession" in navigator) || !opened) return
    navigator.mediaSession.metadata = new window.MediaMetadata({
      title: `${opened.title} 第${episodes[index - 1]?.episode || index}集`,
      artist: "Byte TV",
    })
    navigator.mediaSession.setActionHandler("nexttrack", index < episodes.length ? () => playEpisode(index + 1) : null)
    navigator.mediaSession.setActionHandler("previoustrack", index > 1 ? () => playEpisode(index - 1) : null)
  }, [opened, index, episodes, playEpisode])

  const toggle = () => { const v = videoRef.current; if (!v) return; v.paused ? v.play().catch(() => {}) : v.pause() }
  const seekBy = (s) => { const v = videoRef.current; if (v) v.currentTime = Math.max(0, Math.min((v.duration || 0) - 0.1, v.currentTime + s)) }
  const setPlaybackRate = (r) => { setPlayerPrefs({ rate: r }); setRate(r); apply() }
  const changeVolume = (v) => {
    v = Math.round(Math.min(1, Math.max(0, v)) * 100) / 100
    setPlayerPrefs({ volume: v, muted: v === 0 })
    setVolume(v); setMuted(v === 0); apply()
  }
  const toggleMuted = () => { const m = !getPlayerPrefs().muted; setPlayerPrefs({ muted: m }); setMuted(m); apply() }
  const fullscreen = () => {
    if (native?.toggleFullScreen) native.toggleFullScreen()
    else if (document.fullscreenElement) document.exitFullscreen()
    else rootRef.current?.requestFullscreen?.()
    // 原生全屏由外壳切换，状态稍后同步；这里先乐观翻转，随后由 fullscreenchange 校正
    setFs((v) => !v)
  }
  const seekTo = (ratio) => {
    const v = videoRef.current
    if (!v || !duration) return
    const t = Math.max(0, Math.min(duration, ratio * duration))
    v.currentTime = t
    setPosition(t)
  }

  /* ---------------------------------------------------------------------------
     全屏状态同步：DOM 全屏走 fullscreenchange；原生窗口全屏不派发该事件，
     用窗口尺寸变化兜底（全屏时视口会变成整屏尺寸）。
     --------------------------------------------------------------------------- */
  useEffect(() => {
    const sync = () => setFs(Boolean(document.fullscreenElement))
    document.addEventListener("fullscreenchange", sync)
    const syncNative = () => {
      const full = screen.width === window.innerWidth && screen.height === window.innerHeight
      setFs((prev) => (document.fullscreenElement ? true : full || prev))
    }
    window.addEventListener("resize", syncNative)
    syncNative()
    return () => {
      document.removeEventListener("fullscreenchange", sync)
      window.removeEventListener("resize", syncNative)
    }
  }, [])

  /* ---------------------------------------------------------------------------
     Esc 逐层返回：全屏 → 抽屉 → 迷你窗 → 关闭。
     最外层不销毁播放会话；进度早已自动保存，可续播。
     --------------------------------------------------------------------------- */
  const escapeLayer = useCallback(() => {
    if (document.fullscreenElement) { document.exitFullscreen(); setFs(false); return }
    if (fs) { native?.toggleFullScreen?.(); setFs(false); return }
    if (menu) { setMenu(false); return }
    if (drawer && episodes.length) { setDrawer(false); return }
    if (!mini) { setMini(true); return }
    onClose()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [menu, drawer, episodes.length, mini, onClose])

  // 键盘
  useEffect(() => {
    const down = (e) => {
      if (e.target.tagName === "INPUT" && e.target.type !== "range" && e.target.type !== "checkbox") return
      if (e.target.tagName === "SELECT" && e.key.startsWith("Arrow")) e.target.blur()
      const k = e.key
      if (k === " ") { e.preventDefault(); toggle() }
      else if (k === "ArrowRight") { e.preventDefault(); if (!e.repeat) { holdTimerRef.current = setTimeout(() => { boostRef.current = true; setBoost(true); apply() }, 400) } }
      else if (k === "ArrowLeft") seekBy(-5)
      else if (k === "ArrowUp") { e.preventDefault(); changeVolume(getPlayerPrefs().volume + 0.1) }
      else if (k === "ArrowDown") { e.preventDefault(); changeVolume(getPlayerPrefs().volume - 0.1) }
      else if (k === "f" || k === "F") fullscreen()
      else if (k === "i" || k === "I") setMini((v) => !v)   // 画中画式收起是独立动作，不由 Esc 触发
      else if (k === "n" || k === "N") index < episodes.length && playEpisode(index + 1)
      else if (k === "p" || k === "P") index > 1 && playEpisode(index - 1)
      else if (k === "d" || k === "D") setDanmakuOn((v) => !v)
      else if (k === "m" || k === "M") toggleMuted()
      else if (k === "]") setPlaybackRate(RATES[Math.min(RATES.length - 1, RATES.indexOf(rate) + 1)] || 1)
      else if (k === "[") setPlaybackRate(RATES[Math.max(0, RATES.indexOf(rate) - 1)] || 1)
      else if (k === "Escape") escapeLayer()
    }
    const up = (e) => {
      if (e.key !== "ArrowRight") return
      clearTimeout(holdTimerRef.current)
      if (boostRef.current) { boostRef.current = false; setBoost(false); apply() } else seekBy(5) // 松开恢复用户选择的倍速
    }
    window.addEventListener("keydown", down)
    window.addEventListener("keyup", up)
    return () => { window.removeEventListener("keydown", down); window.removeEventListener("keyup", up) }
  })

  // 3 秒无操作隐藏控制条
  useEffect(() => {
    if (mini) return
    let timer = setTimeout(() => setIdle(true), 3000)
    const move = () => { setIdle(false); clearTimeout(timer); timer = setTimeout(() => setIdle(true), 3000) }
    window.addEventListener("mousemove", move)
    return () => { window.removeEventListener("mousemove", move); clearTimeout(timer) }
  }, [mini])

  useEffect(() => localStorage.setItem("juku.danmaku", danmakuOn ? "1" : "0"), [danmakuOn])
  useEffect(() => localStorage.setItem("juku.autonext", autoNext ? "1" : "0"), [autoNext])

  const current = episodes[index - 1]
  const pct = duration ? Math.min(100, (position / duration) * 100) : 0
  const bufPct = duration ? Math.min(100, (buffered / duration) * 100) : 0
  const hideChrome = idle && playing && !mini

  /* ---------------------------------------------------------------------------
     迷你窗：播放会话不销毁，仅收敛呈现
     --------------------------------------------------------------------------- */
  if (mini) {
    return (
      <div
        ref={rootRef}
        className="absolute bottom-4 right-4 z-[60] w-[178px] animate-fade-up overflow-hidden rounded-lg bg-[#0b0b0c] shadow-[0_0_0_1px_var(--cds-border-strong),0_18px_40px_-12px_var(--film-shadow),0_4px_14px_rgba(0,0,0,.24)]"
      >
        <div className="group relative aspect-[9/16] overflow-hidden">
          <video
            ref={videoRef}
            playsInline
            className="size-full object-contain"
            onLoadStart={apply}
            onLoadedMetadata={apply}
            onDurationChange={apply}
            onRateChange={(e) => { const want = boostRef.current ? 3 : getPlayerPrefs().rate; if (e.currentTarget.playbackRate !== want) apply() }}
            onPlay={() => { setPlaying(true); setStatus("正在播放") }}
            onPause={() => { setPlaying(false); saveProgress(false) }}
            onWaiting={() => setStatus("缓冲中…")}
            onPlaying={() => { apply(); setStatus("正在播放"); setError("") }}
            onTimeUpdate={(e) => {
              const v = e.currentTarget
              setPosition(v.currentTime || 0)
              if (Number.isFinite(v.duration)) setDuration(v.duration)
              if (v.buffered.length) setBuffered(v.buffered.end(v.buffered.length - 1))
              if (!v.paused && v.readyState >= 3) setError((msg) => (msg ? "" : msg))
            }}
            onError={(e) => { const v = e.currentTarget; if (!v.currentSrc && !v.getAttribute("src")) return; setError("视频加载失败，可重试") }}
            onEnded={() => { saveProgress(true); if (autoNext && index < episodes.length) playEpisode(index + 1) }}
          />

          {/* 右上角：画中画（独立小窗）· 展开回播放器 · 关闭 */}
          <div className="absolute right-1.5 top-1.5 flex gap-1.5">
            {document.pictureInPictureEnabled ? (
              <button
                type="button" title="画中画 · 独立小窗"
                onClick={() => videoRef.current?.requestPictureInPicture().catch(() => {})}
                className="grid size-6 place-items-center rounded-xs bg-black/60 text-white backdrop-blur-md hover:bg-black/80"
              >
                <PictureInPicture2 className="size-3" strokeWidth={1.8} />
              </button>
            ) : null}
            <button
              type="button" title="展开回播放器"
              onClick={() => setMini(false)}
              className="grid size-6 place-items-center rounded-xs bg-black/60 text-white backdrop-blur-md hover:bg-black/80"
            >
              <Expand className="size-3" strokeWidth={1.8} />
            </button>
            <button
              type="button" title="关闭（Esc）"
              onClick={onClose}
              className="grid size-6 place-items-center rounded-xs bg-black/60 text-white backdrop-blur-md hover:bg-black/80"
            >
              <X className="size-3" strokeWidth={1.8} />
            </button>
          </div>

          {/* 中央播放/暂停 */}
          <button
            type="button" onClick={toggle} title={playing ? "暂停" : "播放"}
            className="absolute left-1/2 top-1/2 grid size-9 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-pill bg-black/45 text-white backdrop-blur-md shadow-[inset_0_0_0_1px_rgba(255,255,255,.2)] transition-opacity duration-200"
          >
            {playing ? <Pause className="size-3.5" fill="currentColor" strokeWidth={0} /> : <Play className="size-3.5" fill="currentColor" strokeWidth={0} />}
          </button>

          {/* 标题与进度 */}
          <div className="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/85 to-transparent px-2.5 pb-2.5 pt-7">
            <div className="truncate text-[11px] font-medium text-white [text-shadow:0_1px_6px_rgba(0,0,0,.7)]">
              {opened?.title || request.title || "正在播放"} · 第 {current?.episode || index} 集
            </div>
          </div>
          <div className="absolute inset-x-0 bottom-0 h-[3px] bg-white/25">
            <span className="block h-full bg-clay" style={{ width: `${pct}%` }} />
          </div>
        </div>
      </div>
    )
  }

  /* ---------------------------------------------------------------------------
     完整播放器
     --------------------------------------------------------------------------- */
  return (
    <div
      ref={rootRef}
      className="absolute inset-x-0 bottom-0 z-[60] flex flex-col overflow-hidden bg-surface-0 text-ink"
      // 非全屏时让出 macOS 交通灯安全区（--titlebar-inset，非 macOS 为 0）
      style={{ top: fs ? 0 : "var(--titlebar-inset)" }}
    >
      {/* 环境光：深浅两套（--amb-a / --amb-b） */}
      <div
        className="pointer-events-none absolute -inset-[12%] blur-3xl"
        style={{ background: "radial-gradient(58% 48% at 32% 26%, var(--amb-a), transparent 64%), radial-gradient(52% 46% at 74% 78%, var(--amb-b), transparent 66%)" }}
      />

      {/* ---------- 顶栏 ---------- */}
      <header className={cn("relative z-10 flex h-14 flex-none items-center gap-3 px-5 transition-opacity duration-300", hideChrome && "opacity-0")}>
        <button
          type="button" title="返回列表（Esc）" onClick={onClose}
          className="grid size-[30px] place-items-center rounded-pill bg-[var(--cds-fill-ghost-hover)] text-ink-secondary transition-colors duration-[180ms] hover:bg-[var(--cds-fill-ghost-selected)] hover:text-ink"
        >
          <ChevronLeft className="size-[18px]" strokeWidth={1.7} />
        </button>

        <div className="flex min-w-0 items-baseline gap-2.5">
          <b className="truncate text-sm font-medium tracking-[-.01em] text-ink">{opened?.title || request.title || "正在播放"}</b>
          <span className="flex-none text-xs text-ink-secondary">
            第 {current?.episode || index} 集 · 共 {episodes.length || "?"} 集
            {error ? "" : ` · ${source || status}`}
          </span>
        </div>

        <span className="grow" />

        {danmakuOn && visibleDanmaku.length ? (
          <span className="flex h-[22px] items-center gap-1.5 rounded-xs bg-clay px-2 text-[11px] text-white">
            <i className="size-1.5 rounded-full bg-white/90" />
            弹幕 · {danmaku.length} 条
          </span>
        ) : null}

        <div className="relative">
          <button
            type="button" title="更多" onClick={() => setMenu((v) => !v)}
            className="grid size-[30px] place-items-center rounded-pill bg-[var(--cds-fill-ghost-hover)] text-ink-secondary transition-colors duration-[180ms] hover:bg-[var(--cds-fill-ghost-selected)] hover:text-ink"
          >
            <MoreHorizontal className="size-[18px]" strokeWidth={1.7} />
          </button>
          {menu ? (
            <div className="absolute right-0 top-10 z-30 w-[220px] animate-fade-up rounded-lg bg-surface-popover p-1.5 shadow-popover">
              <button type="button" className="flex w-full items-center justify-between rounded-sm px-2.5 py-2 text-[12.5px] text-ink hover:bg-[var(--cds-fill-ghost-hover)]" onClick={() => setDanmakuOn((v) => !v)}>
                弹幕<span className="text-[11px] text-ink-muted">{danmakuOn ? "开" : "关"} (D)</span>
              </button>
              <button type="button" className="flex w-full items-center justify-between rounded-sm px-2.5 py-2 text-[12.5px] text-ink hover:bg-[var(--cds-fill-ghost-hover)]" onClick={() => setAutoNext((v) => !v)}>
                自动连播<span className="text-[11px] text-ink-muted">{autoNext ? "开" : "关"}</span>
              </button>
              <button type="button" className="flex w-full items-center justify-between rounded-sm px-2.5 py-2 text-[12.5px] text-ink hover:bg-[var(--cds-fill-ghost-hover)]" onClick={() => { setMini(true); setMenu(false) }}>
                收起为小窗<span className="text-[11px] text-ink-muted">I</span>
              </button>
              {document.pictureInPictureEnabled ? (
                <button
                  type="button"
                  className="flex w-full items-center justify-between rounded-sm px-2.5 py-2 text-[12.5px] text-ink hover:bg-[var(--cds-fill-ghost-hover)]"
                  onClick={() => { videoRef.current?.requestPictureInPicture().catch(() => {}); setMenu(false) }}
                >
                  画中画<span className="text-[11px] text-ink-muted">独立小窗</span>
                </button>
              ) : null}
            </div>
          ) : null}
        </div>
      </header>

      {/* ---------- 画面区：竖屏居中 ---------- */}
      <div
        className={cn("relative z-[5] grid min-h-0 flex-1 place-items-center overflow-hidden transition-[padding] duration-[380ms] ease-out", drawer && episodes.length && "pr-[320px]")}
        onClick={toggle}
        onDoubleClick={fullscreen}
      >
        <div
          className={cn(
            "relative aspect-[9/16] overflow-hidden rounded-lg bg-[#0b0b0c] shadow-film",
            "transition-[height] duration-[380ms] ease-out",
            drawer && episodes.length ? "h-[calc(100%-100px)]" : "h-[calc(100%-64px)]",
          )}
        >
          <video
            ref={videoRef}
            playsInline
            className="size-full object-contain"
            onLoadStart={apply}
            onLoadedMetadata={apply}
            onDurationChange={apply}
            onRateChange={(e) => { const want = boostRef.current ? 3 : getPlayerPrefs().rate; if (e.currentTarget.playbackRate !== want) apply() }}
            onPlay={() => { setPlaying(true); setStatus("正在播放") }}
            onPause={() => { setPlaying(false); saveProgress(false) }}
            onWaiting={() => setStatus("缓冲中…")}
            onPlaying={() => { apply(); setStatus("正在播放"); setError("") }}
            onTimeUpdate={(e) => {
              const v = e.currentTarget
              setPosition(v.currentTime || 0)
              if (Number.isFinite(v.duration)) setDuration(v.duration)
              if (v.buffered.length) setBuffered(v.buffered.end(v.buffered.length - 1))
              // 切集/换源时旧源拆除会触发一次 error 事件；新源已在正常推进就清掉误报。
              if (!v.paused && v.readyState >= 3) setError((msg) => (msg ? "" : msg))
            }}
            onError={(e) => { const v = e.currentTarget; if (!v.currentSrc && !v.getAttribute("src")) return; setError("视频加载失败，可重试") }}
            onEnded={() => { saveProgress(true); if (autoNext && index < episodes.length) playEpisode(index + 1) }}
          />

          {/* 弹幕层 */}
          {danmakuOn ? (
            <div className="pointer-events-none absolute inset-0 overflow-hidden">
              {visibleDanmaku.map((d, i) => (
                <span
                  key={d.id}
                  className="absolute whitespace-nowrap text-[13px] font-medium text-white [text-shadow:0_1px_4px_rgba(0,0,0,.9)] [animation:dm_9s_linear_forwards]"
                  style={{ top: `${(i % 8) * 9 + 4}%`, animationDelay: `-${Math.max(0, position - d.timeMs / 1000)}s` }}
                >
                  {d.text}
                </span>
              ))}
            </div>
          ) : null}

          {/* 中央悬浮键：压在影像上，两种主题共用白图标 + 深玻璃 */}
          {!playing && !error ? (
            <button
              type="button"
              onClick={(e) => { e.stopPropagation(); toggle() }}
              title="播放"
              className="absolute left-1/2 top-1/2 grid size-[54px] -translate-x-1/2 -translate-y-1/2 place-items-center rounded-pill text-white shadow-[inset_0_0_0_1px_rgba(255,255,255,.22),0_10px_28px_rgba(0,0,0,.45)] backdrop-blur-xl"
              style={{ background: "rgba(18,18,20,.46)" }}
            >
              <Play className="size-5" fill="currentColor" strokeWidth={0} />
            </button>
          ) : null}

          {boost ? (
            <div className="absolute left-1/2 top-4 -translate-x-1/2 rounded-sm bg-black/60 px-2.5 py-1 text-xs text-white backdrop-blur-md">
              3× 快进中
            </div>
          ) : null}

          {error ? (
            <div className="absolute inset-0 grid place-items-center bg-black/50 backdrop-blur-sm" onClick={(e) => e.stopPropagation()}>
              <div className="w-[300px] rounded-dialog bg-surface-popover p-5 text-center shadow-popover">
                <p className="text-[12.5px] text-ink">{error}</p>
                <div className="mt-4 flex items-center justify-center gap-2">
                  {opened ? (
                    <button
                      type="button"
                      className="h-[30px] rounded-sm bg-clay px-3 text-[12.5px] font-medium text-white hover:bg-clay-hover"
                      onClick={() => playEpisode(index, position, true)}
                    >
                      重试播放
                    </button>
                  ) : null}
                  <button type="button" className="h-[30px] rounded-sm bg-[var(--cds-fill-secondary)] px-3 text-[12.5px] text-ink hover:bg-[var(--cds-fill-secondary-hover)]" onClick={onClose}>
                    返回
                  </button>
                </div>
              </div>
            </div>
          ) : null}
        </div>
      </div>

      {/* ---------- 底栏：进度 + 控件 ---------- */}
      <footer className={cn("relative z-10 flex-none px-5 pb-4 pt-1 transition-opacity duration-300", hideChrome && "opacity-0")} onClick={(e) => e.stopPropagation()}>
        <div className="mb-2 flex items-center gap-3">
          <span className="tnum text-[11.5px] text-ink-secondary">{formatTime(position)}</span>
          {/* 进度条：点击跳转，同时表达已缓冲与已播 */}
          <div
            className="group relative flex h-4 flex-1 cursor-pointer items-center"
            onClick={(e) => {
              const rect = e.currentTarget.getBoundingClientRect()
              seekTo((e.clientX - rect.left) / rect.width)
            }}
          >
            <div className="relative h-1 w-full overflow-hidden rounded-pill bg-[var(--rail)]">
              <span className="absolute inset-y-0 left-0 rounded-pill bg-[var(--cds-alpha-3)]" style={{ width: `${bufPct}%` }} />
              <span className="absolute inset-y-0 left-0 rounded-pill bg-clay" style={{ width: `${pct}%` }} />
            </div>
            {/* 旋钮：陶土实心 + 挖空环，两种主题都成立 */}
            <span
              className="pointer-events-none absolute size-[13px] -translate-x-1/2 rounded-full bg-clay opacity-0 shadow-[0_0_0_3px_var(--cds-surface-0),0_2px_6px_rgba(0,0,0,.28)] transition-opacity duration-200 group-hover:opacity-100"
              style={{ left: `${pct}%` }}
            />
          </div>
          <span className="tnum text-[11.5px] text-ink-muted">{formatTime(duration)}</span>
        </div>

        <div className="flex items-center gap-1.5">
          <Ctrl className="w-[34px] justify-center px-0 text-ink" title="播放/暂停 (空格)" onClick={toggle}>
            {playing ? <Pause className="size-[18px]" fill="currentColor" strokeWidth={0} /> : <Play className="size-[18px]" fill="currentColor" strokeWidth={0} />}
          </Ctrl>
          <Ctrl title="下一集 (N)" disabled={index >= episodes.length} onClick={() => playEpisode(index + 1)}>
            <SkipForward className="size-[18px] fill-current" strokeWidth={0} />下一集
          </Ctrl>
          <SquareCtrl title="静音 (M)" onClick={toggleMuted}>
            {muted ? <VolumeX className="size-[18px]" strokeWidth={1.7} /> : <Volume2 className="size-[18px]" strokeWidth={1.7} />}
          </SquareCtrl>

          <span className="px-2 text-[11px] text-ink-muted">
            {duration ? `已缓存 ${Math.max(0, Math.round(buffered - position))} 秒` : ""}
          </span>

          <span className="grow" />

          {/* 倍速 */}
          <Ctrl title="播放速度" onClick={() => setPlaybackRate(RATES[(RATES.indexOf(rate) + 1) % RATES.length] || 1)}>
            <span className="tnum">{rate}×</span>
          </Ctrl>

          {/* 画质：只有一档时隐藏 */}
          {qualities.length > 1 ? (
            <Ctrl>
              画质
              <select
                title="画质"
                value={quality}
                onChange={(e) => {
                  e.target.blur()
                  const q = Number(e.target.value)
                  setPlayerPrefs({ quality: q })
                  setQuality(q)
                  playEpisode(index, position, playing, q)
                }}
                className="cursor-pointer appearance-none bg-transparent font-medium text-ink outline-none"
              >
                {qualities.map((q) => <option key={q.value} value={q.value}>{q.label}</option>)}
              </select>
            </Ctrl>
          ) : null}

          <Ctrl active={danmakuOn} dim={!danmakuOn} title="弹幕 (D)" onClick={() => setDanmakuOn((v) => !v)}>弹幕</Ctrl>

          {episodes.length ? (
            <Ctrl
              active={drawer}
              title="选集"
              onClick={() => setDrawer((v) => !v)}
            >
              选集 {index}/{episodes.length}
            </Ctrl>
          ) : null}

          {document.pictureInPictureEnabled ? (
            <SquareCtrl title="画中画 · 独立小窗" onClick={() => videoRef.current?.requestPictureInPicture().catch(() => {})}>
              <PictureInPicture2 className="size-[18px]" strokeWidth={1.7} />
            </SquareCtrl>
          ) : null}

          <SquareCtrl title="收起为小窗 (I)" onClick={() => setMini(true)}>
            <Minimize className="size-[18px]" strokeWidth={1.7} />
          </SquareCtrl>

          <SquareCtrl title="全屏 (F) · 退出全屏 (Esc)" onClick={fullscreen}>
            <Maximize className="size-[18px]" strokeWidth={1.7} />
          </SquareCtrl>
        </div>
      </footer>

      {/* ---------- 选集抽屉：只覆盖画面区，控件栏始终可见 ---------- */}
      <aside
        className="glass-heavy absolute bottom-0 right-0 top-14 z-20 flex w-[320px] flex-col p-4 shadow-[inset_1px_0_0_0_var(--hairline)] transition-transform duration-[380ms] ease-out"
        style={{ transform: drawer && episodes.length ? "none" : "translateX(100%)" }}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2">
          <b className="text-[13px] font-medium text-ink">选集</b>
          <span className="text-[11px] text-ink-muted">共 {episodes.length} 集 · 已看 {Math.max(0, index - 1)}</span>
          <button
            type="button" title="收起" onClick={() => setDrawer(false)}
            className="ml-auto grid size-[26px] place-items-center rounded-pill bg-[var(--cds-fill-ghost-hover)] text-ink-secondary hover:bg-[var(--cds-fill-ghost-selected)] hover:text-ink"
          >
            <X className="size-3.5" strokeWidth={1.8} />
          </button>
        </div>

        <input
          placeholder="输入集数跳转"
          value={jumpText}
          onChange={(e) => setJumpText(e.target.value.replace(/\D/g, ""))}
          onKeyDown={(e) => {
            e.stopPropagation()
            if (e.key === "Enter" && jumpText) {
              const n = Math.min(episodes.length, Math.max(1, Number(jumpText)))
              setJumpText("")
              setDrawerPage(null)
              e.currentTarget.blur()
              playEpisode(n)
            }
          }}
          className="mt-3 h-7 rounded-sm bg-surface-3 px-2.5 text-[12px] text-ink shadow-[inset_0_0_0_1px_var(--cds-border)] outline-none placeholder:text-ink-muted focus-visible:shadow-focus"
        />

        {(() => {
          const pages = Math.ceil(episodes.length / PAGE)
          const cur = drawerPage ?? Math.floor((index - 1) / PAGE)
          if (pages <= 1) return null
          return (
            <div className="mt-3 flex items-center gap-0.5 rounded-pill bg-[var(--cds-segmented-track)] p-0.5">
              {Array.from({ length: pages }, (_, i) => (
                <button
                  key={i}
                  type="button"
                  onClick={() => setDrawerPage(i)}
                  className={cn(
                    "h-6 flex-1 rounded-pill text-[11.5px] transition-all duration-[220ms] ease-snappy",
                    i === cur ? "bg-[var(--cds-segmented-thumb)] font-medium text-ink shadow-[0_1px_2px_rgba(0,0,0,.14),0_0_0_1px_var(--cds-border)]" : "text-ink-secondary hover:text-ink",
                  )}
                >
                  {i * PAGE + 1}-{Math.min(episodes.length, (i + 1) * PAGE)}
                </button>
              ))}
            </div>
          )
        })()}

        <div className="scroll-thin mt-3 grid flex-1 grid-cols-5 content-start gap-1.5 overflow-y-auto">
          {(() => {
            const cur = drawerPage ?? Math.floor((index - 1) / PAGE)
            return episodes.slice(cur * PAGE, cur * PAGE + PAGE).map((ep) => {
              const seen = ep.index < index
              const isCur = ep.index === index
              return (
                <button
                  key={ep.index}
                  type="button"
                  title={ep.title}
                  onClick={() => { setDrawerPage(null); playEpisode(ep.index) }}
                  className={cn(
                    "relative grid h-8 place-items-center rounded-sm text-xs transition-colors duration-[160ms] ease-out",
                    isCur
                      ? "bg-clay font-medium text-white shadow-[0_2px_10px_-2px_rgba(217,119,87,.7)]"
                      : seen
                        ? "bg-[var(--cds-fill-secondary)] text-ink-muted hover:bg-[var(--cds-fill-secondary-hover)] hover:text-ink"
                        : "bg-[var(--cds-fill-secondary)] text-ink-secondary hover:bg-[var(--cds-fill-secondary-hover)] hover:text-ink",
                  )}
                >
                  <span className="tnum">{ep.episode || ep.index}</span>
                  {seen && !isCur ? <i className="absolute bottom-[5px] h-0.5 w-3.5 rounded-pill bg-[var(--cds-alpha-3)]" /> : null}
                  {ep.taskId ? <i className="absolute right-1 top-1 size-1 rounded-full bg-clay" title="本地已下载" /> : null}
                </button>
              )
            })
          })()}
        </div>

        <div className="mt-auto flex flex-col gap-1 border-t border-hairline pt-3.5">
          <div className="flex items-center gap-2.5 text-xs text-ink-secondary">
            <span className={`relative h-[22px] w-[38px] flex-none rounded-pill transition-colors duration-200 ${autoNext ? "bg-clay" : "bg-[var(--cds-alpha-3)]"}`}>
              <i className={`absolute left-0.5 top-0.5 size-[18px] rounded-full bg-white shadow-[0_1px_3px_rgba(0,0,0,.35)] transition-transform duration-300 ease-snappy ${autoNext ? "translate-x-4" : ""}`} />
            </span>
            <button type="button" onClick={() => setAutoNext((v) => !v)} className="text-left">自动连播</button>
            <span className="ml-auto text-[11px] text-ink-muted">剩余 3 秒时提示</span>
          </div>
          <p className="text-[11px] leading-relaxed text-ink-muted">
            空格 播放/暂停 · ←/→ 5 秒 · 长按 → 3× · N/P 切集 · M 静音 · D 弹幕 · I 小窗 · F 全屏 · Esc 逐层返回
          </p>
        </div>
      </aside>
    </div>
  )
}