import { useCallback, useEffect, useRef, useState } from "react"
import Hls from "hls.js"
import { ChevronLeft, ListVideo, Maximize, MessageSquare, Pause, PictureInPicture2, Play, SkipForward, Volume2, VolumeX } from "lucide-react"
import { apiGet, apiPost, native } from "../api"
import { formatTime } from "../lib/format"

// 播放器：完整复用 /api/ui/playback/* 协议（open → plan / hls/open → progress / control / danmaku / prefetch）。
const RATES = [0.75, 1, 1.25, 1.5, 2, 3]

function capabilities(video) {
  const mse = typeof window.MediaSource !== "undefined"
  const can = (type) => (mse && window.MediaSource.isTypeSupported(type)) || video.canPlayType(type) !== ""
  const codecs = ["h264"]
  if (can('video/mp4; codecs="hvc1.1.6.L93.B0"') || can('video/mp4; codecs="hev1.1.6.L93.B0"')) codecs.push("hevc")
  if (can('video/mp4; codecs="av01.0.05M.08"')) codecs.push("av1")
  return { mp4: true, nativeHls: video.canPlayType("application/vnd.apple.mpegurl") !== "", hlsjs: Hls.isSupported(), video: codecs, audio: ["aac", "mp3", "opus"] }
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
  const [rate, setRate] = useState(1)
  const [muted, setMuted] = useState(false)
  const [volume, setVolume] = useState(1)
  const [drawer, setDrawer] = useState(true)
  const [danmakuOn, setDanmakuOn] = useState(() => localStorage.getItem("juku.danmaku") !== "0")
  const [danmaku, setDanmaku] = useState([])
  const [autoNext, setAutoNext] = useState(() => localStorage.getItem("juku.autonext") !== "0")
  const [idle, setIdle] = useState(false)
  const [boost, setBoost] = useState(false)
  const [qualities, setQualities] = useState([])
  const [quality, setQuality] = useState(0)
  const episodes = opened?.episodes || []

  const post = useCallback((path, body) => apiPost(path, { session: sessionRef.current, ...body }), [])

  const saveProgress = useCallback(async (completed) => {
    const video = videoRef.current
    if (!video || !runRef.current || !sessionRef.current) return
    const current = video.currentTime || 0
    const length = Number.isFinite(video.duration) && video.duration > 0 ? video.duration : 0
    sequenceRef.current += 1
    try {
      await post("/api/ui/playback/progress", { progress: { run: runRef.current, sequence: sequenceRef.current, episode: indexRef.current, position: current, duration: length, completed: Boolean(completed && length && current >= length - 2) } })
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
      video.playbackRate = rate
      setStatus(shouldPlay ? "正在播放" : "已暂停，点击继续")
      if (shouldPlay) video.play().catch(() => setStatus("已就绪，点击播放"))
    }
    clearMedia()
    if (playlist && !video.canPlayType("application/vnd.apple.mpegurl") && Hls.isSupported()) {
      const hls = new Hls({ maxBufferLength: 30, backBufferLength: 60, fragLoadingMaxRetry: 6, manifestLoadingMaxRetry: 4, xhrSetup: (xhr) => { xhr.withCredentials = true } })
      hlsRef.current = hls
      hls.on(Hls.Events.MANIFEST_PARSED, start)
      hls.on(Hls.Events.ERROR, (_, data) => {
        if (!data?.fatal) return
        if (data.type === Hls.ErrorTypes.MEDIA_ERROR && retryRef.current < 2) { retryRef.current++; hls.recoverMediaError(); return }
        setError("播放列表加载失败，可重试")
      })
      hls.loadSource(url)
      hls.attachMedia(video)
      return
    }
    video.addEventListener("loadedmetadata", start, { once: true })
    video.src = url
    video.load()
  }

  const playEpisode = useCallback(async (nextIndex, offset = 0, shouldPlay = true, nextQuality = quality) => {
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
      const plan = await post("/api/ui/playback/plan", { episode: nextIndex, start: offset, quality: nextQuality, version: ++requestSeqRef.current, mode: "auto", client: capabilities(video) })
      if (version !== versionRef.current) return
      let result = plan
      if (plan.player === "legacy" || !plan.url) {
        result = await post("/api/ui/playback/hls/open", { episode: nextIndex, start: offset, quality: nextQuality, version: ++requestSeqRef.current })
        if (version !== versionRef.current) return
      }
      runRef.current = Number(result.run) || 0
      if (result.duration) setDuration(Number(result.duration))
      setQualities(result.qualities || [])
      if (result.quality) setQuality(result.quality)
      setSource(result.source === "local" || result.source === "collection" ? "本集播放本地已完成文件" : "在线播放")
      attach(result.url, offset, shouldPlay, version)
      // 预缓存下一集（后端按资源限制决定是否执行）
      if (nextIndex < episodes.length) post("/api/ui/playback/prefetch", { episode: nextIndex + 1, run: runRef.current, version: requestSeqRef.current }).catch(() => {})
    } catch (err) {
      if (version === versionRef.current) setError(err.message || "播放失败")
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [opened, quality, saveProgress, post, episodes.length])

  // 打开会话
  useEffect(() => {
    let cancelled = false
    const body = request.taskId ? { taskId: request.taskId } : { dramaId: request.dramaId, resume: Boolean(request.resume), fromHistory: Boolean(request.fromHistory) }
    apiPost("/api/ui/playback/open", body)
      .then((result) => {
        if (cancelled) { apiPost("/api/ui/playback/control", { session: result.session, action: "close" }).catch(() => {}); return }
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
    const beat = setInterval(() => post("/api/ui/playback/control", { action: "heartbeat" }).catch((e) => e.status === 410 && setError("播放会话已过期，请返回后重新打开")), 20000)
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
    const q = new URLSearchParams({ session: sessionRef.current, episode: String(index), start: String(windowStart), duration: "60000" })
    apiGet(`/api/ui/playback/danmaku?${q}`, { signal: controller.signal }).then((page) => setDanmaku((page.items || []).slice(0, 300))).catch(() => {})
    return () => controller.abort()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [danmakuOn, index, Math.floor(position / 60), opened])
  const visibleDanmaku = danmakuOn ? danmaku.filter((d) => d.timeMs / 1000 <= position && d.timeMs / 1000 > position - 6).slice(-12) : []

  // Media Session
  useEffect(() => {
    if (!("mediaSession" in navigator) || !opened) return
    navigator.mediaSession.metadata = new window.MediaMetadata({ title: `${opened.title} 第${episodes[index - 1]?.episode || index}集`, artist: "果果剧库" })
    navigator.mediaSession.setActionHandler("nexttrack", index < episodes.length ? () => playEpisode(index + 1) : null)
    navigator.mediaSession.setActionHandler("previoustrack", index > 1 ? () => playEpisode(index - 1) : null)
  }, [opened, index, episodes, playEpisode])

  const toggle = () => { const v = videoRef.current; if (!v) return; v.paused ? v.play().catch(() => {}) : v.pause() }
  const seekBy = (s) => { const v = videoRef.current; if (v) v.currentTime = Math.max(0, Math.min((v.duration || 0) - 0.1, v.currentTime + s)) }
  const setPlaybackRate = (r) => { setRate(r); if (videoRef.current) videoRef.current.playbackRate = r }
  const fullscreen = () => { if (native?.toggleFullScreen) native.toggleFullScreen(); else if (document.fullscreenElement) document.exitFullscreen(); else rootRef.current?.requestFullscreen?.() }

  // 键盘
  useEffect(() => {
    let holdTimer = null
    const down = (e) => {
      if (e.target.tagName === "INPUT") return
      const k = e.key
      if (k === " ") { e.preventDefault(); toggle() }
      else if (k === "ArrowRight") { e.preventDefault(); if (!e.repeat) { holdTimer = setTimeout(() => { setBoost(true); if (videoRef.current) videoRef.current.playbackRate = 3 }, 400) } }
      else if (k === "ArrowLeft") seekBy(-5)
      else if (k === "ArrowUp") { e.preventDefault(); const v = Math.min(1, volume + 0.1); setVolume(v); if (videoRef.current) videoRef.current.volume = v }
      else if (k === "ArrowDown") { e.preventDefault(); const v = Math.max(0, volume - 0.1); setVolume(v); if (videoRef.current) videoRef.current.volume = v }
      else if (k === "f" || k === "F") fullscreen()
      else if (k === "n" || k === "N") index < episodes.length && playEpisode(index + 1)
      else if (k === "p" || k === "P") index > 1 && playEpisode(index - 1)
      else if (k === "d" || k === "D") setDanmakuOn((v) => !v)
      else if (k === "]") setPlaybackRate(RATES[Math.min(RATES.length - 1, RATES.indexOf(rate) + 1)] || 1)
      else if (k === "[") setPlaybackRate(RATES[Math.max(0, RATES.indexOf(rate) - 1)] || 1)
      else if (k === "Escape") { if (document.fullscreenElement) document.exitFullscreen(); else onClose() }
    }
    const up = (e) => {
      if (e.key !== "ArrowRight") return
      clearTimeout(holdTimer)
      if (boost) { setBoost(false); if (videoRef.current) videoRef.current.playbackRate = rate } else seekBy(5)
    }
    window.addEventListener("keydown", down)
    window.addEventListener("keyup", up)
    return () => { window.removeEventListener("keydown", down); window.removeEventListener("keyup", up); clearTimeout(holdTimer) }
  })

  // 3 秒无操作隐藏控制条
  useEffect(() => {
    let timer = setTimeout(() => setIdle(true), 3000)
    const move = () => { setIdle(false); clearTimeout(timer); timer = setTimeout(() => setIdle(true), 3000) }
    window.addEventListener("mousemove", move)
    return () => { window.removeEventListener("mousemove", move); clearTimeout(timer) }
  }, [])

  useEffect(() => localStorage.setItem("juku.danmaku", danmakuOn ? "1" : "0"), [danmakuOn])
  useEffect(() => localStorage.setItem("juku.autonext", autoNext ? "1" : "0"), [autoNext])
  const current = episodes[index - 1]
  return (
    <div ref={rootRef} className={`player ${idle && playing ? "idle" : ""}`}>
      <div className="player-bg" />
      <header className="player-top">
        <button className="icon-btn" onClick={onClose}><ChevronLeft size={18} /></button>
        <div>
          <h3>{opened?.title || request.title || "正在播放"}</h3>
          <span className="muted">第 {current?.episode || index} 集 / 共 {episodes.length || "?"} 集 · {error ? "" : source || status}</span>
        </div>
      </header>
      <div className="player-stage" onClick={toggle} onDoubleClick={fullscreen}>
        <video
          ref={videoRef}
          playsInline
          onPlay={() => { setPlaying(true); setStatus("正在播放") }}
          onPause={() => { setPlaying(false); saveProgress(false) }}
          onWaiting={() => setStatus("缓冲中…")}
          onPlaying={() => setStatus("正在播放")}
          onTimeUpdate={(e) => {
            const v = e.currentTarget
            setPosition(v.currentTime || 0)
            if (Number.isFinite(v.duration)) setDuration(v.duration)
            if (v.buffered.length) setBuffered(v.buffered.end(v.buffered.length - 1))
          }}
          onError={() => setError("视频加载失败，可重试")}
          onEnded={() => { saveProgress(true); if (autoNext && index < episodes.length) playEpisode(index + 1) }}
        />
        <div className="danmaku-layer">{visibleDanmaku.map((d, i) => <span key={d.id} style={{ top: `${(i % 8) * 9 + 4}%`, animationDelay: `-${Math.max(0, position - d.timeMs / 1000)}s` }}>{d.text}</span>)}</div>
        {boost ? <div className="boost">3× 快进中</div> : null}
        {error ? (
          <div className="player-error" onClick={(e) => e.stopPropagation()}>
            <p>{error}</p>
            {opened ? <button className="btn primary" onClick={() => playEpisode(index, position, true)}>重试播放</button> : null}
            <button className="btn ghost" onClick={onClose}>返回</button>
          </div>
        ) : !playing && opened ? <div className="center-hint">{status}</div> : null}
      </div>
      <footer className="player-controls" onClick={(e) => e.stopPropagation()}>
        <div className="timeline">
          <i className="buf" style={{ width: `${duration ? (buffered / duration) * 100 : 0}%` }} />
          <input type="range" min={0} max={duration || 1} step={0.1} value={Math.min(position, duration || 0)} onChange={(e) => { const v = Number(e.target.value); setPosition(v); if (videoRef.current) videoRef.current.currentTime = v }} />
        </div>
        <div className="ctrl-row">
          <button className="icon-btn" onClick={toggle}>{playing ? <Pause size={18} /> : <Play size={18} />}</button>
          <button className="icon-btn" disabled={index >= episodes.length} onClick={() => playEpisode(index + 1)}><SkipForward size={18} /></button>
          <span className="time">{formatTime(position)} / {formatTime(duration)}</span>
          <button className="icon-btn" onClick={() => { const m = !muted; setMuted(m); if (videoRef.current) videoRef.current.muted = m }}>{muted ? <VolumeX size={18} /> : <Volume2 size={18} />}</button>
          <input className="vol" type="range" min={0} max={1} step={0.05} value={volume} onChange={(e) => { const v = Number(e.target.value); setVolume(v); if (videoRef.current) videoRef.current.volume = v }} />
          <span className="grow" />
          <button className={`icon-btn ${danmakuOn ? "on" : ""}`} title="弹幕 (D)" onClick={() => setDanmakuOn(!danmakuOn)}><MessageSquare size={17} /></button>
          <select value={rate} onChange={(e) => setPlaybackRate(Number(e.target.value))}>{RATES.map((r) => <option key={r} value={r}>{r}×</option>)}</select>
          {qualities.length > 1 ? <select value={quality} onChange={(e) => { const q = Number(e.target.value); setQuality(q); playEpisode(index, position, playing, q) }}>{qualities.map((q) => <option key={q.value} value={q.value}>{q.label}</option>)}</select> : null}
          <label className="auto-next"><input type="checkbox" checked={autoNext} onChange={(e) => setAutoNext(e.target.checked)} />自动下一集</label>
          {document.pictureInPictureEnabled ? <button className="icon-btn" onClick={() => videoRef.current?.requestPictureInPicture().catch(() => {})}><PictureInPicture2 size={17} /></button> : null}
          <button className={`icon-btn ${drawer ? "on" : ""}`} onClick={() => setDrawer(!drawer)}><ListVideo size={17} /></button>
          <button className="icon-btn" onClick={fullscreen}><Maximize size={17} /></button>
        </div>
      </footer>
      {drawer && episodes.length ? (
        <aside className="ep-drawer" onClick={(e) => e.stopPropagation()}>
          <h4>选集 · 共 {episodes.length} 集</h4>
          <div className="ep-grid small">
            {episodes.map((ep) => (
              <button key={ep.index} className={`ep ${ep.index === index ? "current" : ""}`} onClick={() => playEpisode(ep.index)}>
                {ep.episode || ep.index}{ep.taskId ? <i className="ep-local" /> : null}{ep.vip ? <i className="ep-vip">VIP</i> : null}
              </button>
            ))}
          </div>
          <p className="muted small">空格 播放/暂停 · ←/→ 5 秒 · 长按 → 3× · N/P 切集 · D 弹幕 · F 全屏</p>
        </aside>
      ) : null}
    </div>
  )
}
