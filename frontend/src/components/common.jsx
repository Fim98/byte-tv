import { useEffect, useRef, useState } from "react"
import { Download, Play, Plus } from "lucide-react"
import { coverOf, episodesOf, titleOf } from "../lib/format"
import { navigate } from "../router"

export function Poster({ drama, badge, progress, subtitle, onPlay, onFollow, onDownload, rank, size = "md" }) {
  const [failed, setFailed] = useState(false)
  const cover = coverOf(drama)
  const total = episodesOf(drama)
  const title = titleOf(drama)
  return (
    <div className={`poster poster-${size}`}>
      {rank ? <span className="rank-num">{rank}</span> : null}
      <button className="poster-art" onClick={() => (onPlay ? onPlay() : navigate(`/drama/${encodeURIComponent(drama.id)}`))} title={title}>
        {cover && !failed ? (
          <img src={cover} alt="" loading="lazy" onError={() => setFailed(true)} />
        ) : (
          <span className="poster-fallback">{title.slice(0, 1)}</span>
        )}
        {total ? <span className="tag-episodes">{total}集</span> : null}
        {badge ? <span className="tag-state">{badge}</span> : null}
        {drama.local ? <span className="tag-local">本地</span> : null}
        <span className="poster-title">{title}</span>
        <span className="poster-hover">
          <span onClick={(e) => { e.stopPropagation(); onPlay ? onPlay() : navigate(`/drama/${encodeURIComponent(drama.id)}?play=1`) }}><Play size={14} /> 播放</span>
          {onFollow ? <span onClick={(e) => { e.stopPropagation(); onFollow() }}><Plus size={14} /> 追剧</span> : null}
          {onDownload ? <span onClick={(e) => { e.stopPropagation(); onDownload() }}><Download size={14} /> 下载</span> : null}
        </span>
      </button>
      {progress != null ? (
        <div className="bar"><i style={{ width: `${Math.max(0, Math.min(100, progress))}%` }} /></div>
      ) : null}
      <button className="poster-name" onClick={() => navigate(`/drama/${encodeURIComponent(drama.id)}`)}>{title}</button>
      {subtitle ? <div className="poster-sub">{subtitle}</div> : null}
    </div>
  )
}

export function Row({ title, hint, action, children }) {
  return (
    <section className="row">
      <header>
        <h2>{title}</h2>
        {hint ? <span className="hint">{hint}</span> : null}
        <span className="grow" />
        {action}
      </header>
      <div className="row-scroll">{children}</div>
    </section>
  )
}

export function Segmented({ value, options, onChange }) {
  return (
    <div className="seg">
      {options.map(([key, label]) => (
        <button key={key} className={key === value ? "on" : ""} onClick={() => onChange(key)}>{label}</button>
      ))}
    </div>
  )
}

export function Empty({ children }) {
  return <div className="empty">{children}</div>
}

export function ErrorBox({ error, onRetry }) {
  if (!error) return null
  return (
    <div className="error-box">
      <span>{String(error.message || error)}</span>
      {onRetry ? <button className="btn ghost sm" onClick={onRetry}>重试</button> : null}
    </div>
  )
}

export function Skeleton({ count = 8 }) {
  return Array.from({ length: count }, (_, i) => <div key={i} className="poster poster-md skeleton"><div className="poster-art" /></div>)
}

// useAsync：带取消与重试的数据加载。
export function useAsync(fn, deps) {
  const [state, setState] = useState({ loading: true, data: null, error: null })
  const [tick, setTick] = useState(0)
  const alive = useRef(0)
  useEffect(() => {
    const id = ++alive.current
    const controller = new AbortController()
    setState((s) => ({ ...s, loading: true, error: null }))
    Promise.resolve(fn(controller.signal))
      .then((data) => id === alive.current && setState({ loading: false, data, error: null }))
      .catch((error) => id === alive.current && error.name !== "AbortError" && setState({ loading: false, data: null, error }))
    return () => controller.abort()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick])
  return { ...state, reload: () => setTick((t) => t + 1), setData: (data) => setState((s) => ({ ...s, data })) }
}

let toastHandler = null
export function setToastHandler(fn) { toastHandler = fn }
export function toast(message, kind = "info") { toastHandler?.(message, kind) }
