import { useEffect, useRef, useState } from "react"
import Hls from "hls.js"
import { Pause, Play } from "lucide-react"
import { apiPost } from "./api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Slider } from "@/components/ui/slider"

function formatTime(value) {
  if (!Number.isFinite(value) || value < 0) return "0:00"
  const total = Math.floor(value)
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`
}

function capabilities(video) {
  return {
    mp4: true,
    nativeHls: video.canPlayType("application/vnd.apple.mpegurl") !== "",
    hlsjs: true,
    video: ["h264", "hevc", "av1"],
    audio: ["aac", "mp3", "ac3", "opus"],
  }
}

export default function Player({ opened, onClose }) {
  const videoRef = useRef(null)
  const hlsRef = useRef(null)
  const versionRef = useRef(0)
  const sequenceRef = useRef(0)
  const runRef = useRef(0)
  const indexRef = useRef(opened.initialIndex || 1)
  const seekingRef = useRef(false)
  const [index, setIndex] = useState(opened.initialIndex || 1)
  const [position, setPosition] = useState(opened.initialPosition || 0)
  const [duration, setDuration] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [status, setStatus] = useState("正在准备播放")
  const [error, setError] = useState("")
  const episodes = opened.episodes || []

  useEffect(() => {
    let stopped = false
    playEpisode(opened.initialIndex || 1, opened.initialPosition || 0, !opened.resumePaused)
    return () => {
      stopped = true
      versionRef.current += 1
      saveProgress(false)
      apiPost("/api/ui/playback/control", {
        session: opened.session,
        action: "close",
      }).catch(() => {})
      hlsRef.current?.destroy()
      if (stopped) videoRef.current?.pause()
    }
    // The session is fixed for this mounted player.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [opened.session])

  useEffect(() => {
    const timer = setInterval(() => saveProgress(false), 2000)
    return () => clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [opened.session, index])

  async function saveProgress(completed) {
    const video = videoRef.current
    const run = runRef.current
    if (!video || !run) return
    const current = Number.isFinite(video.currentTime) ? video.currentTime : position
    const length = Number.isFinite(video.duration) && video.duration > 0 ? video.duration : duration
    if (!Number.isFinite(current) || current < 0) return
    sequenceRef.current += 1
    try {
      await apiPost("/api/ui/playback/progress", {
        session: opened.session,
        progress: {
          run,
          sequence: sequenceRef.current,
          episode: indexRef.current,
          position: current,
          duration: length > 0 ? length : 0,
          completed: completed && length > 0 && current >= Math.max(0, length - 2),
        },
      })
    } catch {
      // A missed progress tick should not interrupt playback.
    }
  }

  function clearMedia() {
    hlsRef.current?.destroy()
    hlsRef.current = null
    const video = videoRef.current
    if (video) {
      video.removeAttribute("src")
      video.load()
    }
  }

  function attachURL(url, offset, shouldPlay, version) {
    const video = videoRef.current
    const playlist = /\.m3u8(\?|$)/.test(url) || url.includes("index.m3u8")
    const start = () => {
      if (version !== versionRef.current || !video) return
      const length = Number.isFinite(video.duration) ? video.duration : 0
      if (length > 0) setDuration(length)
      if (offset > 0) video.currentTime = length > 0 ? Math.min(offset, Math.max(0, length - 0.05)) : offset
      setStatus(shouldPlay ? "正在播放" : "已暂停")
      if (shouldPlay) video.play().catch(() => setStatus("已就绪，点击播放"))
    }
    if (playlist && video && !video.canPlayType("application/vnd.apple.mpegurl") && Hls.isSupported()) {
      clearMedia()
      const hls = new Hls()
      hlsRef.current = hls
      hls.loadSource(url)
      hls.attachMedia(video)
      hls.on(Hls.Events.MANIFEST_PARSED, start)
      hls.on(Hls.Events.ERROR, (_, data) => {
        if (data?.fatal) setError("播放列表加载失败")
      })
      return
    }
    clearMedia()
    video.addEventListener("loadedmetadata", start, { once: true })
    video.src = url
    video.load()
  }

  async function playEpisode(nextIndex, offset, shouldPlay) {
    const version = ++versionRef.current
    indexRef.current = nextIndex
    setIndex(nextIndex)
    setError("")
    setStatus(offset > 0 ? "正在跳转到上次位置" : "正在准备播放")
    const video = videoRef.current
    try {
      if (opened.mode === "collection") {
        await apiPost("/api/ui/playback/prepare", { session: opened.session, episode: nextIndex })
        if (version !== versionRef.current) return
      }
      const plan = await apiPost("/api/ui/playback/plan", {
        session: opened.session,
        episode: nextIndex,
        start: offset,
        quality: 0,
        version,
        mode: "auto",
        client: capabilities(video),
      })
      if (version !== versionRef.current) return
      if (plan.player === "legacy" || !plan.url) {
        const native = await apiPost("/api/ui/playback/hls/open", {
          session: opened.session,
          episode: nextIndex,
          start: offset,
          quality: 0,
          version,
        })
        if (version !== versionRef.current) return
        runRef.current = Number(native.run) || 0
        if (native.duration) setDuration(Number(native.duration))
        attachURL(native.url, offset, shouldPlay, version)
        return
      }
      runRef.current = Number(plan.run) || 0
      if (plan.duration) setDuration(Number(plan.duration))
      attachURL(plan.url, offset, shouldPlay, version)
    } catch (err) {
      if (version === versionRef.current) setError(err.message || "播放失败")
    }
  }

  function togglePlay() {
    const video = videoRef.current
    if (!video) return
    if (video.paused) video.play().catch(() => setStatus("已就绪，点击播放"))
    else video.pause()
  }

  return (
    <section className="grid h-full min-h-0 gap-4 lg:grid-cols-[minmax(0,1fr)_280px]">
      <div className="flex min-h-0 flex-col gap-3">
        <video
          ref={videoRef}
          className="aspect-video w-full rounded-lg bg-black"
          playsInline
          onPlay={() => setPlaying(true)}
          onPause={() => {
            setPlaying(false)
            saveProgress(false)
          }}
          onTimeUpdate={(event) => {
            if (!seekingRef.current) setPosition(event.currentTarget.currentTime || 0)
            if (Number.isFinite(event.currentTarget.duration)) setDuration(event.currentTarget.duration)
          }}
          onEnded={() => saveProgress(true)}
        />
        <div className="flex items-center gap-3">
          <Button size="icon" onClick={togglePlay} aria-label={playing ? "暂停" : "播放"}>
            {playing ? <Pause /> : <Play />}
          </Button>
          <span className="w-24 shrink-0 text-xs text-muted-foreground">
            {formatTime(position)} / {formatTime(duration)}
          </span>
          <Slider
            value={[Math.min(position, duration || position)]}
            max={duration > 0 ? duration : 1}
            step={0.1}
            disabled={duration <= 0}
            onValueChange={([value]) => {
              seekingRef.current = true
              setPosition(value)
            }}
            onValueCommit={([value]) => {
              seekingRef.current = false
              if (videoRef.current) videoRef.current.currentTime = value
              setPosition(value)
            }}
            aria-label="播放进度"
          />
        </div>
        <div className="flex items-center justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold">{opened.title || "正在播放"}</h2>
            <p className="text-sm text-muted-foreground">{error || status}</p>
          </div>
          <Button variant="outline" onClick={onClose}>返回</Button>
        </div>
      </div>
      <ScrollArea className="h-full rounded-lg border">
        <div className="flex flex-col gap-2 p-3">
          {episodes.map((episode) => (
            <Button
              key={episode.index}
              variant={episode.index === index ? "default" : "secondary"}
              className="justify-start"
              onClick={() => playEpisode(episode.index, 0, true)}
            >
              <Badge variant="outline">{episode.episode || episode.index}</Badge>
              <span className="truncate">{episode.title || `第${episode.episode || episode.index}集`}</span>
            </Button>
          ))}
        </div>
      </ScrollArea>
    </section>
  )
}
