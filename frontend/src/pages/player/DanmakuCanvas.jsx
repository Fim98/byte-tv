import React, { useEffect, useRef, memo } from "react"

const LANES_COUNT = 8
const LANE_HEIGHT = 30
const BASE_SPEED = 140 // pixels per second

export const DanmakuCanvas = memo(function DanmakuCanvas({
  danmaku = [],
  videoRef,
  playing,
  danmakuOn,
}) {
  const canvasRef = useRef(null)
  const activeItemsRef = useRef([])
  const processedIdsRef = useRef(new Set())
  const lastTimeRef = useRef(0)
  const lastVideoTimeRef = useRef(0)
  const rafIdRef = useRef(null)

  // Clear when danmaku items change (new episode) or disabled
  useEffect(() => {
    activeItemsRef.current = []
    processedIdsRef.current = new Set()
  }, [danmaku, danmakuOn])

  useEffect(() => {
    if (!danmakuOn) {
      const canvas = canvasRef.current
      if (canvas) {
        const ctx = canvas.getContext("2d")
        ctx?.clearRect(0, 0, canvas.width, canvas.height)
      }
      return
    }

    const canvas = canvasRef.current
    if (!canvas) return
    const ctx = canvas.getContext("2d")
    if (!ctx) return

    // DPR handling for retina crispness
    const updateSize = () => {
      const rect = canvas.getBoundingClientRect()
      const dpr = window.devicePixelRatio || 1
      canvas.width = rect.width * dpr
      canvas.height = rect.height * dpr
      ctx.scale(dpr, dpr)
    }
    updateSize()

    const resizeObserver = new ResizeObserver(updateSize)
    resizeObserver.observe(canvas)

    const findAvailableLane = (activeList) => {
      const laneUsage = new Array(LANES_COUNT).fill(0)
      for (const item of activeList) {
        if (item.lane >= 0 && item.lane < LANES_COUNT) {
          laneUsage[item.lane]++
        }
      }
      let minLane = 0
      let minCount = laneUsage[0]
      for (let i = 1; i < LANES_COUNT; i++) {
        if (laneUsage[i] < minCount) {
          minCount = laneUsage[i]
          minLane = i
        }
      }
      return minLane
    }

    const render = (now) => {
      const dt = lastTimeRef.current ? Math.min(0.1, (now - lastTimeRef.current) / 1000) : 0.016
      lastTimeRef.current = now

      const rect = canvas.getBoundingClientRect()
      const width = rect.width
      const height = rect.height
      const video = videoRef.current
      const isPaused = !video || video.paused
      const currentTimeMs = (video?.currentTime || 0) * 1000

      // Detect seeking (large time jumps)
      if (Math.abs(currentTimeMs - lastVideoTimeRef.current) > 1500) {
        activeItemsRef.current = []
        processedIdsRef.current.clear()
      }
      lastVideoTimeRef.current = currentTimeMs

      // Enqueue new danmaku items whose time has arrived (within a 500ms window)
      if (!isPaused && danmaku.length > 0) {
        for (const item of danmaku) {
          const diff = currentTimeMs - item.timeMs
          if (diff >= 0 && diff < 600 && !processedIdsRef.current.has(item.id)) {
            processedIdsRef.current.add(item.id)

            // Measure text width
            ctx.font = "bold 15px -apple-system, BlinkMacSystemFont, 'Noto Sans SC', sans-serif"
            const textMetrics = ctx.measureText(item.text)
            const textWidth = textMetrics.width

            const lane = findAvailableLane(activeItemsRef.current)
            const y = 24 + lane * LANE_HEIGHT

            // Assign subtle variations to speed based on length so they don't overlap as easily
            const speed = BASE_SPEED + (textWidth > 150 ? 25 : 0)

            activeItemsRef.current.push({
              id: item.id,
              text: item.text,
              x: width + 10,
              y,
              speed,
              lane,
              width: textWidth,
            })
          }
        }
      }

      // Draw all active items
      ctx.clearRect(0, 0, width, height)
      ctx.font = "bold 15px -apple-system, BlinkMacSystemFont, 'Noto Sans SC', sans-serif"
      ctx.textBaseline = "middle"

      const nextActive = []
      for (const item of activeItemsRef.current) {
        // Move item only when video is playing!
        if (!isPaused) {
          item.x -= item.speed * dt
        }

        // Draw shadow/outline for readability against any background
        ctx.lineJoin = "round"
        ctx.lineWidth = 3.5
        ctx.strokeStyle = "rgba(0, 0, 0, 0.85)"
        ctx.strokeText(item.text, item.x, item.y)

        ctx.fillStyle = "#ffffff"
        ctx.fillText(item.text, item.x, item.y)

        // Keep item if it has not exited the left edge
        if (item.x + item.width > -20) {
          nextActive.push(item)
        }
      }
      activeItemsRef.current = nextActive

      rafIdRef.current = requestAnimationFrame(render)
    }

    rafIdRef.current = requestAnimationFrame(render)

    return () => {
      if (rafIdRef.current) {
        cancelAnimationFrame(rafIdRef.current)
      }
      resizeObserver.disconnect()
    }
  }, [danmaku, danmakuOn, videoRef])

  if (!danmakuOn) return null

  return (
    <canvas
      ref={canvasRef}
      className="pointer-events-none absolute inset-0 z-[5] size-full"
    />
  )
})
