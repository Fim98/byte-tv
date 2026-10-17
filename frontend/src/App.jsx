import { useCallback, useEffect, useRef, useState } from "react"
import {
  BarChart3, Bell, ChevronLeft, ChevronRight, Clock, Compass, Download,
  Heart, LayoutGrid, Moon, RefreshCw, Search, SlidersHorizontal, Sun,
} from "lucide-react"
import { apiGet, initViewer } from "./api"
import { navigate, useRoute } from "./router"
import { Button, IconButton, StatusDot, setToastHandler, toast } from "./components/cds"
import { setPlayerOpener } from "./actions"
import { applyTheme, getTheme, setTheme } from "./lib/theme"
import Home from "./pages/Home"
import Browse from "./pages/Browse"
import Rankings from "./pages/Rankings"
import Detail from "./pages/Detail"
import Following from "./pages/Following"
import Downloads from "./pages/Downloads"
import Settings from "./pages/Settings"
import Onboarding from "./pages/Onboarding"
import Player from "./pages/Player"

applyTheme()

// 侧栏主导航 = 功能页；「设置」固定停靠在最底部（见 SideFoot）。
const NAV = [
  { path: "/home", label: "发现", icon: Compass },
  { path: "/browse", label: "剧库", icon: LayoutGrid },
  { path: "/rankings", label: "榜单", icon: BarChart3 },
  { path: "/following", label: "追剧", icon: Heart },
  { path: "/history", label: "历史", icon: Clock },
  { path: "/downloads", label: "下载", icon: Download, badge: "downloads" },
]

const navItemClass = (on) =>
  `flex h-8 items-center gap-2.5 rounded-sm px-2.5 text-[13px] transition-colors duration-[180ms] ease-out ${
    on
      ? "bg-[var(--cds-fill-ghost-selected)] font-medium text-ink"
      : "text-ink-secondary hover:bg-[var(--cds-fill-ghost-hover)] hover:text-ink"
  }`

/* ---------------------------------------------------------------------------
   搜索框：300ms 防抖联想 + CJK 输入法合成守卫
   --------------------------------------------------------------------------- */
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
    <div className="relative">
      <div className="flex h-[30px] w-[340px] items-center gap-2 rounded-pill bg-surface-2 pl-2.5 pr-2 shadow-[inset_0_0_0_1px_var(--cds-border)] transition-shadow duration-[220ms] ease-out focus-within:shadow-focus">
        <Search className="size-3.5 flex-none text-ink-muted" strokeWidth={1.7} />
        <input
          ref={input}
          value={text}
          placeholder="搜索剧名、标签或简介"
          className="min-w-0 flex-1 bg-transparent text-[12.5px] text-ink outline-none placeholder:text-ink-muted"
          onChange={(e) => { setText(e.target.value); setOpen(true); setActive(-1) }}
          // 中文输入法选字期间不请求、不触发搜索
          onCompositionStart={() => (composing.current = true)}
          onCompositionEnd={(e) => { composing.current = false; setText(e.currentTarget.value) }}
          onFocus={() => setOpen(true)}
          onBlur={() => setTimeout(() => setOpen(false), 150)}
          onKeyDown={(e) => {
            if (composing.current) return
            if (e.key === "ArrowDown") { e.preventDefault(); setActive((a) => Math.min(items.length - 1, a + 1)) }
            else if (e.key === "ArrowUp") { e.preventDefault(); setActive((a) => Math.max(-1, a - 1)) }
            else if (e.key === "Enter") submit(items[active]?.name)
            else if (e.key === "Escape") { setOpen(false); e.currentTarget.blur() }
          }}
        />
        <kbd className="flex-none rounded-xs bg-[var(--cds-fill-secondary)] px-1.5 py-0.5 text-[10px] text-ink-muted">⌘K</kbd>
      </div>

      {open && items.length > 0 ? (
        <ul className="absolute left-0 top-9 z-30 w-[420px] animate-fade-up rounded-lg bg-surface-popover p-1.5 shadow-popover">
          <li className="flex items-center justify-between px-2.5 pb-1.5 pt-2 text-[11px] text-ink-muted">
            <span>联想结果</span>
            <span>{items.length} 条 · 回车联网搜索</span>
          </li>
          {items.map((item, i) => (
            <li
              key={(item.name || "") + i}
              onMouseDown={() => submit(item.name)}
              className={`flex h-9 cursor-pointer items-center gap-2 rounded-sm px-2.5 text-[13px] ${
                i === active ? "bg-[var(--cds-fill-ghost-selected)]" : "hover:bg-[var(--cds-fill-ghost-hover)]"
              }`}
            >
              <Highlight text={item.name || item.title || ""} q={text.trim()} />
              <span className="ml-auto flex-none text-[11px] text-ink-muted">剧集</span>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}

function Highlight({ text, q }) {
  const i = q ? text.indexOf(q) : -1
  const base = "min-w-0 flex-1 truncate"
  if (i < 0) return <span className={base}>{text}</span>
  return (
    <span className={base}>
      {text.slice(0, i)}
      <span className="font-medium text-clay">{q}</span>
      {text.slice(i + q.length)}
    </span>
  )
}

/* ---------------------------------------------------------------------------
   通知铃铛
   --------------------------------------------------------------------------- */
function NoticeBell({ notices }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="relative">
      <IconButton onClick={() => setOpen((v) => !v)} title="通知">
        <Bell className="size-[18px]" strokeWidth={1.7} />
        {notices.length ? <i className="absolute right-[3px] top-[3px] size-1.5 rounded-full bg-clay" /> : null}
      </IconButton>
      {open ? (
        <div className="absolute right-0 top-9 z-30 w-[300px] animate-fade-up rounded-lg bg-surface-popover p-2 shadow-popover">
          <h4 className="px-2.5 py-1.5 text-xs font-medium text-ink-secondary">通知</h4>
          {notices.length === 0 ? (
            <p className="px-2.5 py-2 text-[12.5px] text-ink-muted">暂无通知</p>
          ) : notices.map((n) => (
            <p key={n.id} className={`rounded-sm px-2.5 py-2 text-[12.5px] ${n.kind === "error" ? "text-danger" : "text-ink"}`}>
              {n.message}
              <small className="ml-2 text-[10.5px] text-ink-muted">{n.at.toLocaleTimeString()}</small>
            </p>
          ))}
        </div>
      ) : null}
    </div>
  )
}

/* ---------------------------------------------------------------------------
   主题快捷切换：显示「当前所处模式」的图标（浅色显日 / 深色显月）
   --------------------------------------------------------------------------- */
function ThemeToggle() {
  const [theme, setLocal] = useState(getTheme)
  const light = theme === "light" || (theme === "system" && window.matchMedia?.("(prefers-color-scheme: light)").matches)
  return (
    <IconButton
      size={32}
      round={false}
      title={light ? "切换到深色" : "切换到浅色"}
      onClick={() => { const next = light ? "dark" : "light"; setTheme(next); setLocal(next) }}
    >
      {light ? <Sun className="size-[18px]" strokeWidth={1.7} /> : <Moon className="size-[18px]" strokeWidth={1.7} />}
    </IconButton>
  )
}

/* ---------------------------------------------------------------------------
   侧栏底部：左侧「设置」入口，右侧主题快捷切换（space-between 一排）
   --------------------------------------------------------------------------- */
function SideFoot({ route }) {
  const on = route.path.startsWith("/settings")
  return (
    <div className="mt-auto flex items-center justify-between gap-2 p-2.5 shadow-[inset_0_1px_0_0_var(--hairline)]">
      <a href="#/settings" className={`${navItemClass(on)} flex-1`}>
        <SlidersHorizontal className={`size-[18px] flex-none ${on ? "text-clay" : ""}`} strokeWidth={1.7} />
        设置
      </a>
      <ThemeToggle />
    </div>
  )
}

/* ---------------------------------------------------------------------------
   应用外壳
   --------------------------------------------------------------------------- */
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

  if (fatal) {
    return (
      <div className="grid h-full place-items-center bg-surface-0 text-center">
        <div>
          <h1 className="text-lg font-medium text-ink">客户端无法启动</h1>
          <p className="mt-2 text-[12.5px] text-ink-secondary">{fatal.message}</p>
          <Button variant="primary" className="mt-4" onClick={() => location.reload()}>重新加载</Button>
        </div>
      </div>
    )
  }
  if (!ready) {
    return <div className="grid h-full place-items-center bg-surface-0 text-[13px] text-ink-muted">正在连接本机服务…</div>
  }
  if (onboarding) {
    return <Onboarding onDone={() => { localStorage.setItem("juku.onboarded", "1"); setOnboarding(false) }} />
  }

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

  const downloadCount = status?.activeDownloads || 0

  return (
    <div className="relative flex h-full bg-surface-0">
      {/* ---------- 侧栏：通高，无独立拖拽条 ---------- */}
      <aside className="flex w-[var(--side-w)] flex-none flex-col bg-surface-1 shadow-[inset_-1px_0_0_0_var(--hairline)]">
        {/* macOS 下由 .titlebar-space 让出交通灯留白（见 index.css）；
            红绿灯是窗口原生绘制的，界面不得重绘。 */}
        <div className="titlebar-space flex-none" />

        <nav className="flex flex-col gap-px px-2.5">
          {NAV.map(({ path, label, icon: Icon, badge }) => {
            const on = route.path.startsWith(path) || (path === "/home" && route.path === "/")
            const count = badge === "downloads" ? downloadCount : 0
            return (
              <a key={path} href={`#${path}`} className={navItemClass(on)}>
                <Icon className={`size-[18px] flex-none ${on ? "text-clay" : ""}`} strokeWidth={1.7} />
                {label}
                {count > 0 ? (
                  <span className="ml-auto rounded-pill bg-clay px-1.5 text-[10.5px] font-medium leading-[18px] text-white">{count}</span>
                ) : null}
              </a>
            )
          })}
        </nav>

        <SideFoot route={route} />
      </aside>

      {/* ---------- 主区 ---------- */}
      <main className="relative flex min-w-0 flex-1 flex-col">
        <header className="glass drag relative z-20 flex h-[52px] flex-none items-center gap-2.5 px-[18px] pl-3.5">
          <IconButton className="no-drag" onClick={() => history.back()} title="后退">
            <ChevronLeft className="size-[18px]" strokeWidth={1.7} />
          </IconButton>
          <IconButton className="no-drag text-ink-muted opacity-55" onClick={() => history.forward()} title="前进">
            <ChevronRight className="size-[18px]" strokeWidth={1.7} />
          </IconButton>
          <div className="no-drag"><SearchBox /></div>

          <span className="grow" />

          <span className="no-drag flex h-[26px] items-center gap-1.5 rounded-pill bg-surface-2 px-2.5 text-[11.5px] text-ink-secondary shadow-[inset_0_0_0_1px_var(--cds-border)]">
            <StatusDot tone={status ? "ok" : "warn"} />
            {status ? "红果源 · 正常" : "本机服务连接中"}
          </span>
          <IconButton className="no-drag" onClick={checkNew} title="检查新内容">
            <RefreshCw className="size-[18px]" strokeWidth={1.7} />
          </IconButton>
          <div className="no-drag"><NoticeBell notices={notices} /></div>
        </header>

        <div key={route.path} className="mask-page scroll-thin min-h-0 flex-1 overflow-y-auto px-[var(--page-x)] pt-[22px]">
          {content}
          <div className="h-8" />
        </div>
      </main>

      {player ? <Player request={player} onClose={() => setPlayer(null)} /> : null}

      {/* ---------- Toast ---------- */}
      <div className="pointer-events-none absolute bottom-5 left-1/2 z-[70] flex -translate-x-1/2 flex-col items-center gap-2">
        {toasts.map((t) => (
          <div
            key={t.id}
            className={`animate-fade-up rounded-sm bg-surface-popover px-3.5 py-2 text-[12.5px] shadow-popover ${
              t.kind === "error" ? "text-danger" : "text-ink"
            }`}
          >
            {t.message}
          </div>
        ))}
      </div>
    </div>
  )
}