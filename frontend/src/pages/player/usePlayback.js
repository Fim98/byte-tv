import { useCallback, useEffect, useRef, useState } from "react"
import Hls from "hls.js"
import { apiGet, apiPost } from "../../api"
import { isDesktopShell, withMediaToken } from "../../lib/desktop"
import { applyPlayerPrefs, getPlayerPrefs, setPlayerPrefs } from "../../lib/playerPrefs"

function detectCapabilities(video) {
  const mse = typeof window !== "undefined" && typeof window.MediaSource !== "undefined"
  const can = (type) => (mse && window.MediaSource.isTypeSupported(type)) || (video && video.canPlayType(type) !== "")
  const codecs = ["h264"]
  if (can('video/mp4; codecs="hvc1.1.6.L93.B0"') || can('video/mp4; codecs="hev1.1.6.L93.B0"')) codecs.push("hevc")
  if (can('video/mp4; codecs="av01.0.05M.08"')) codecs.push("av1")
  return {
    mp4: true,
    nativeHls: !(isDesktopShell() && Hls.isSupported()) && video?.canPlayType("application/vnd.apple.mpegurl") !== "",
    hlsjs: Hls.isSupported(),
    video: codecs,
    audio: ["aac", "mp3", "opus"],
  }
}

export function usePlayback(request) {
  const videoRef = useRef(null)
  const hlsRef = useRef(null)
  const versionRef = useRef(0)
  const sequenceRef = useRef(0)
  const runRef = useRef(0)
  const indexRef = useRef(1)
  const sessionRef = useRef("")
  const retryRef = useRef(0)
  const requestSeqRef = useRef(0)
  const boostRef = useRef(false)

  const [opened, setOpened] = useState(null)
  const [index, setIndex] = useState(1)
  const [position, setPosition] = useState(0)
  const [duration, setDuration] = useState(0)
  const [buffered, setBuffered] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [loading, setLoading] = useState(true)
  const [status, setStatus] = useState("正在准备播放")
  const [error, setError] = useState("")
  const [source, setSource] = useState("")
  const [rate, setRate] = useState(() => getPlayerPrefs().rate)
  const [muted, setMuted] = useState(() => getPlayerPrefs().muted)
  const [volume, setVolume] = useState(() => getPlayerPrefs().volume)
  const [boost, setBoost] = useState(false)
  const [qualities, setQualities] = useState([])
  const [quality, setQuality] = useState(0)
  const [videoAspect, setVideoAspect] = useState(16 / 9)

  const episodes = opened?.episodes || []

  const post = useCallback((path, body) => {
    return apiPost(path, { session: sessionRef.current, ...body })
  }, [])

  const apply = useCallback(() => {
    applyPlayerPrefs(videoRef.current, { boost: boostRef.current })
  }, [])

  const applyMeta = useCallback(() => {
    apply()
    const v = videoRef.current
    if (v && v.videoWidth > 0 && v.videoHeight > 0) {
      setVideoAspect(v.videoWidth / v.videoHeight)
    }
  }, [apply])

  const saveProgress = useCallback(async (completed) => {
    const video = videoRef.current
    if (!video || !runRef.current || !sessionRef.current) return
    const current = video.currentTime || 0
    const length = Number.isFinite(video.duration) && video.duration > 0 ? video.duration : 0
    sequenceRef.current += 1
    try {
      await post("/api/ui/playback/progress", {
        progress: {
          run: runRef.current,
          sequence: sequenceRef.current,
          episode: indexRef.current,
          position: current,
          duration: length,
          completed: Boolean(completed && length && current >= length - 2),
        },
      })
    } catch {
      // ignore
    }
  }, [post])

  const clearMedia = useCallback(() => {
    if (hlsRef.current) {
      hlsRef.current.destroy()
      hlsRef.current = null
    }
    const video = videoRef.current
    if (video) {
      video.removeAttribute("src")
      video.load()
    }
  }, [])

  const attachMedia = useCallback((url, offset, shouldPlay, version) => {
    const video = videoRef.current
    if (!video) return
    const playlist = /\.m3u8(\?|$)/.test(url)

    const onStart = () => {
      if (version !== versionRef.current) return
      setLoading(false)
      if (Number.isFinite(video.duration)) setDuration(video.duration)
      if (offset > 0) video.currentTime = offset
      apply()
      setStatus(shouldPlay ? "正在播放" : "已暂停")
      if (shouldPlay) {
        video.play().catch(() => setStatus("已就绪，点击播放"))
      }
    }

    clearMedia()

    if (playlist && (isDesktopShell() || !video.canPlayType("application/vnd.apple.mpegurl")) && Hls.isSupported()) {
      const hls = new Hls({
        maxBufferLength: 30,
        backBufferLength: 60,
        fragLoadingMaxRetry: 6,
        manifestLoadingMaxRetry: 4,
        xhrSetup: (xhr) => {
          xhr.withCredentials = true
        },
      })
      hlsRef.current = hls
      hls.on(Hls.Events.MANIFEST_PARSED, onStart)
      hls.on(Hls.Events.ERROR, (_, data) => {
        if (!data?.fatal) return
        if (data.type === Hls.ErrorTypes.MEDIA_ERROR && retryRef.current < 2) {
          retryRef.current++
          hls.recoverMediaError()
          return
        }
        setLoading(false)
        setError("播放列表加载失败，请重试")
      })
      hls.loadSource(url)
      hls.attachMedia(video)
      hls.on(Hls.Events.MEDIA_ATTACHED, apply)
      apply()
      return
    }

    video.addEventListener("loadedmetadata", onStart, { once: true })
    video.src = withMediaToken(url)
    video.load()
    apply()
  }, [apply, clearMedia])

  const playEpisode = useCallback(async (nextIndex, offset = 0, shouldPlay = true, nextQuality = getPlayerPrefs().quality) => {
    const version = ++versionRef.current
    await saveProgress(false)
    indexRef.current = nextIndex
    retryRef.current = 0
    setIndex(nextIndex)
    setError("")
    setLoading(true)
    setStatus(offset > 0 ? "正在跳转到上次位置" : "正在加载视频…")
    const video = videoRef.current

    try {
      if (opened?.mode === "collection") {
        await post("/api/ui/playback/prepare", { episode: nextIndex })
      }
      if (version !== versionRef.current) return

      const plan = await post("/api/ui/playback/plan", {
        episode: nextIndex,
        start: offset,
        quality: nextQuality,
        version: ++requestSeqRef.current,
        mode: "auto",
        client: detectCapabilities(video),
      })
      if (version !== versionRef.current) return

      let result = plan
      if (plan.player === "legacy" || !plan.url) {
        result = await post("/api/ui/playback/hls/open", {
          episode: nextIndex,
          start: offset,
          quality: nextQuality,
          version: ++requestSeqRef.current,
        })
        if (version !== versionRef.current) return
      }

      runRef.current = Number(result.run) || 0
      if (result.duration) setDuration(Number(result.duration))
      setQualities(result.qualities || [])
      if (result.quality) setQuality(result.quality)
      setSource(result.source === "local" || result.source === "collection" ? "本地已缓存" : "在线流畅")
      attachMedia(result.url, offset, shouldPlay, version)

      if (nextIndex < episodes.length) {
        post("/api/ui/playback/prefetch", {
          episode: nextIndex + 1,
          run: runRef.current,
          version: requestSeqRef.current,
        }).catch(() => {})
      }
    } catch (err) {
      if (version === versionRef.current) {
        setLoading(false)
        setError(err.message || "播放失败，请稍后重试")
      }
    }
  }, [opened, saveProgress, post, attachMedia, episodes.length])

  // Open playback session
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
      .catch((err) => {
        if (!cancelled) {
          setLoading(false)
          setError(err.message || "初始化播放会话失败")
        }
      })

    return () => {
      cancelled = true
      versionRef.current++
      const session = sessionRef.current
      if (session) {
        saveProgress(false).finally(() => {
          apiPost("/api/ui/playback/control", { session, action: "close" }).catch(() => {})
        })
      }
      if (hlsRef.current) {
        hlsRef.current.destroy()
        hlsRef.current = null
      }
    }
  }, [request, saveProgress])

  // Start first episode once opened
  useEffect(() => {
    if (!opened) return
    const first = request.episode || opened.initialIndex || 1
    const offset = request.episode ? 0 : opened.initialPosition || 0
    playEpisode(first, offset, !(opened.resumePaused && !request.episode))
    if (opened.resumeMessage) setStatus(opened.resumeMessage)
  }, [opened, request.episode, playEpisode])

  // Heartbeat & periodic progress save
  useEffect(() => {
    if (!opened) return
    const beat = setInterval(() => {
      post("/api/ui/playback/control", { action: "heartbeat" }).catch((e) => {
        if (e.status === 410) setError("播放会话已过期，请重新打开")
      })
    }, 20000)

    const prog = setInterval(() => saveProgress(false), 5000)
    const onVisible = () => {
      if (document.visibilityState === "visible") {
        post("/api/ui/playback/control", { action: "heartbeat" }).catch(() => {})
      }
    }
    document.addEventListener("visibilitychange", onVisible)
    return () => {
      clearInterval(beat)
      clearInterval(prog)
      document.removeEventListener("visibilitychange", onVisible)
    }
  }, [opened, post, saveProgress])

  // Actions
  const togglePlay = useCallback(() => {
    const v = videoRef.current
    if (!v) return
    if (v.paused) {
      v.play().catch(() => {})
    } else {
      v.pause()
    }
  }, [])

  const seekTo = useCallback((ratio) => {
    const v = videoRef.current
    if (!v || !duration) return
    const t = Math.max(0, Math.min(duration, ratio * duration))
    v.currentTime = t
    setPosition(t)
  }, [duration])

  const seekBy = useCallback((seconds) => {
    const v = videoRef.current
    if (!v) return
    const maxDur = v.duration || duration || 0
    const target = Math.max(0, Math.min(maxDur > 0 ? maxDur - 0.2 : 0, (v.currentTime || 0) + seconds))
    v.currentTime = target
    setPosition(target)
  }, [duration])

  const changeRate = useCallback((newRate) => {
    setPlayerPrefs({ rate: newRate })
    setRate(newRate)
    apply()
  }, [apply])

  const changeVolume = useCallback((newVol) => {
    const val = Math.round(Math.min(1, Math.max(0, newVol)) * 100) / 100
    setPlayerPrefs({ volume: val, muted: val === 0 })
    setVolume(val)
    setMuted(val === 0)
    apply()
  }, [apply])

  const toggleMute = useCallback(() => {
    const nextMuted = !muted
    setPlayerPrefs({ muted: nextMuted })
    setMuted(nextMuted)
    apply()
  }, [muted, apply])

  const changeQuality = useCallback((q) => {
    setPlayerPrefs({ quality: q })
    setQuality(q)
    playEpisode(index, position, playing, q)
  }, [index, position, playing, playEpisode])

  const setBoostMode = useCallback((active) => {
    boostRef.current = active
    setBoost(active)
    apply()
  }, [apply])

  return {
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
    // Actions
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
    setLoading,
  }
}
