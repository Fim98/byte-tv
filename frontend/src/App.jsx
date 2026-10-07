import { useCallback, useEffect, useRef, useState } from "react"
import { BarChart3, Bell, ChevronLeft, ChevronRight, Clock, Compass, Download, Heart, LayoutGrid, Monitor, RefreshCw, Search, SlidersHorizontal } from "lucide-react"
import { apiGet, initViewer, native } from "./api"
import { navigate, useRoute } from "./router"
import { BrandMark, setToastHandler, toast } from "./components/common"
import { setPlayerOpener } from "./actions"
import Home from "./pages/Home"
import Browse from "./pages/Browse"
import Rankings from "./pages/Rankings"
import Detail from "./pages/Detail"
import Following from "./pages/Following"
import Downloads from "./pages/Downloads"
import Settings from "./pages/Settings"
import Onboarding from "./pages/Onboarding"
import Player from "./pages/Player"
import { applyTheme } from "./lib/theme"

applyTheme()

const NAV = [
  ["/home", "发现", Compass], ["/browse", "剧库", LayoutGrid], ["/rankings", "榜单", BarChart3],
  ["/following", "追剧", Heart], ["/history", "历史", Clock], ["/downloads", "下载", Download],
]

function Highlight({ text, q }) {
  const i = q ? text.indexOf(q) : -1
  if (i < 0) return text
  return <>{text.slice(0, i)}<span className="kw">{q}</span>{text.slice(i + q.length)}</>
}

function SearchBox() {
  const [text, setText] = useState("")
  const [items, setItems] = useState([])
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const composing = useRef(false)
  const input = useRef(null)
  useEffect(() => {
    const onKey = (e) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === "k" || e.key === "f")) {
        e.preventDefault()
        input.current?.focus()
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])
  useEffect(() => {
    const q = text.trim()
    if (!q || composing.current) return setItems([])
    const controller = new AbortController()
    const timer = setTimeout(() => {
      apiGet(`/api/ui/search/suggestions?q=${encodeURIComponent(q)}`, { signal: controller.signal })
        .then((r) => setItems((r.data || []).slice(0, 10)))
        .catch(() => {})
    }, 300)
    return () => { clearTimeout(timer); controller.abort() }
  }, [text])
  const submit = (q) => {
    const value = (q ?? text).trim()
    if (!value) return
    setOpen(false)
    navigate(`/browse?q=${encodeURIComponent(value)}`)
  }
  return (
    <div className="search">
      <Search size={15} />
      <input
        ref={input}
        value={text}
        placeholder="搜索剧名、标签或简介"
        onChange={(e) => { setText(e.target.value); setOpen(true); setActive(-1) }}
        onCompositionStart={() => (composing.current = true)}
        onCompositionEnd={(e) => { composing.current = false; setText(e.currentTarget.value) }}
        onFocus={() => setOpen(true)}
        onBlur={() => setTimeout(() => setOpen(false), 150)}
        onKeyDown={(e) => {
          if (composing.current) return
          if (e.key === "ArrowDown") { e.preventDefault(); setActive((a) => Math.min(items.length - 1, a + 1)) }
          else if (e.key === "ArrowUp") { e.preventDefault(); setActive((a) => Math.max(-1, a - 1)) }
          else if (e.key === "Enter") {
            const item = items[active]
            submit(item?.name)
          } else if (e.key === "Escape") { setOpen(false); e.currentTarget.blur() }
        }}
      />
      <kbd>⌘K</kbd>
      {open && items.length > 0 ? (
        <ul className="suggest">
          <li className="sugg-head" onMouseDown={(e) => e.preventDefault()}><span>联想结果</span><span>{items.length} 条 · 回车联网搜索红果</span></li>
          {items.map((item, i) => (
            <li key={(item.name || "") + i} className={i === active ? "on" : ""} onMouseDown={() => submit(item.name)}>
              <span><Highlight text={item.name || item.title || ""} q={text.trim()} /></span>
              <span className="tag">剧集</span>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}

export default function App() {
  const route = useRoute()
  const [ready, setReady] = useState(false)
  const [fatal, setFatal] = useState(null)
  const [status, setStatus] = useState(null)
  const [player, setPlayer] = useState(null)
  const [toasts, setToasts] = useState([])
  const [notices, setNotices] = useState([])
  const [onboarding, setOnboarding] = useState(() => localStorage.getItem("juku.onboarded") !== "1")

  useEffect(() => {
    setToastHandler((message, kind) => {
      const id = Math.random()
      setToasts((t) => [...t, { id, message, kind }])
      setNotices((n) => [{ id, message, kind, at: new Date() }, ...n].slice(0, 50))
      setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 3500)
    })
    setPlayerOpener((request) => setPlayer(request))
    initViewer().then(() => setReady(true)).catch(setFatal)
  }, [])

  const refreshStatus = useCallback(() => apiGet("/api/desktop/status").then(setStatus).catch(() => {}), [])
  useEffect(() => {
    if (!ready) return
    refreshStatus()
    const timer = setInterval(refreshStatus, 10000)
    return () => clearInterval(timer)
  }, [ready, refreshStatus])

  const checkNew = async () => {
    try {
      const r = await apiGet("/api/ui/dramas?update=1&source=hongguo")
      toast(r.updateAccepted === false ? "剧库正在更新中" : "已开始检查新内容", "ok")
    } catch (e) {
      toast(e.message, "error")
    }
  }

  if (fatal) return <div className="fatal"><h1>Byte TV 无法启动</h1><p>{fatal.message}</p><button className="btn" onClick={() => location.reload()}>重新加载</button></div>
  if (!ready) return <div className="fatal"><p>正在连接本机服务…</p></div>
  if (onboarding) return <Onboarding onDone={() => { localStorage.setItem("juku.onboarded", "1"); setOnboarding(false) }} />

  const [page] = route.parts
  let content
  switch (page) {
    case "browse": content = <Browse route={route} />; break
    case "rankings": content = <Rankings route={route} />; break
    case "drama": content = <Detail id={decodeURIComponent(route.parts[1] || "")} route={route} />; break
    case "following": content = <Following tab="following" />; break
    case "history": content = <Following tab="history" />; break
    case "downloads": content = <Downloads status={status} />; break
    case "settings": content = <Settings section={route.parts[1]} status={status} onChanged={refreshStatus} />; break
    default: content = <Home />
  }
  const ff = status?.ffmpeg
  return (
    <div className={`shell ${native?.platform === "win32" ? "win" : ""}`}>
      <aside className="side">
        <div className="drag traffic" />
        <div className="brand"><BrandMark /><span className="brand-name">Byte TV</span></div>
        <nav className="nav">
          {NAV.map(([path, label, Icon]) => (
            <a key={path} href={`#${path}`} className={`nav-item ${route.path.startsWith(path) || (path === "/home" && route.path === "/") ? "on" : ""}`}>
              <Icon size={18} />{label}
              {path === "/downloads" && status?.activeDownloads ? <span className="count red">{status.activeDownloads}</span> : null}
            </a>
          ))}
        </nav>
        <div className="side-bottom">
          <a href="#/settings" className={`nav-item ${route.path.startsWith("/settings") ? "on" : ""}`}><SlidersHorizontal size={18} />设置</a>
          <div className="account" title={`剧库 ${status?.library?.total ?? 0} 部`}>
            <span className="avatar"><Monitor size={15} /></span>
            <div className="acc-info">
              <span className="acc-name">本机</span>
              <span className="acc-role"><i className={`dot ${ff?.status === "ready" ? "green" : ff?.status === "failed" ? "red" : "amber"}`} />FFmpeg {ff?.status === "ready" ? "已就绪" : ff?.status === "failed" ? "准备失败" : "准备中"}</span>
            </div>
          </div>
        </div>
      </aside>
      <main className="main">
        <header className="top drag">
          <button className="round no-drag" title="后退" onClick={() => history.back()}><ChevronLeft size={16} /></button>
          <button className="round no-drag" title="前进" onClick={() => history.forward()}><ChevronRight size={16} /></button>
          <div className="no-drag"><SearchBox /></div>
          <span className="grow" />
          <span className={`status-chip no-drag ${status ? "" : "warn"}`}><i className={`dot ${status ? "green" : "amber"}`} />{status ? "红果源 · 正常" : "本机服务连接中"}</span>
          <button className="round no-drag" title="检查新内容" onClick={checkNew}><RefreshCw size={15} /></button>
          <NoticeBell notices={notices} />
        </header>
        <div className="page" key={route.path}>{content}</div>
      </main>
      {player ? <Player request={player} onClose={() => setPlayer(null)} /> : null}
      <div className="toasts">{toasts.map((t) => <div key={t.id} className={`toast ${t.kind}`}>{t.message}</div>)}</div>
    </div>
  )
}

function NoticeBell({ notices }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="no-drag bell">
      <button className="round" title="通知" onClick={() => setOpen(!open)}><Bell size={15} />{notices.length ? <i className="badge-dot" /> : null}</button>
      {open ? (
        <div className="notice-panel">
          <h4>通知</h4>
          {notices.length === 0 ? <p className="muted">暂无通知</p> : notices.map((n) => <p key={n.id} className={n.kind}>{n.message}<small>{n.at.toLocaleTimeString()}</small></p>)}
        </div>
      ) : null}
    </div>
  )
}
