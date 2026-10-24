import { useCallback, useEffect, useRef, useState } from "react"
import {
  BarChart3, Bell, ChevronLeft, ChevronRight, Clock, Compass, Download,
  Heart, LayoutGrid, Moon, RefreshCw, Search, SlidersHorizontal, Sun,
} from "lucide-react"

/* 原生 macOS 规范的侧栏切换图标（极简窗格 + 侧边栏激活态指示） */
export function SidebarToggleIcon({ collapsed, className }) {
  return (
    <svg
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className || "size-4"}
    >
      <rect x="2.5" y="3.5" width="15" height="13" rx="2.5" />
      <path d="M7.5 3.5v13" />
      {!collapsed && (
        <rect x="3.5" y="4.5" width="3" height="11" rx="1" fill="currentColor" opacity="0.28" stroke="none" />
      )}
    </svg>
  )
}
import { apiGet, initViewer } from "./api"
import { navigate, useNavigation, useRoute } from "./router"
import { Button, IconButton, StatusDot, setToastHandler, toast } from "./components/cds"
import { loadFollowing, setPlayerOpener } from "./actions"
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

/* ---------------------------------------------------------------------------
   窗口拖拽：页面走本机 HTTP，拿不到 Wails 运行时，所以 --wails-draggable 与
   -webkit-app-region 都不生效，改由原生桥接触发 Wails 的平台实现。

   时机必须跟 Wails 运行时一致：在「按下后第一次移动」时启动，而不是一按下就启动。
   macOS 的 performWindowDragWithEvent 需要有实际位移才建立窗口拖拽会话；
   在 mousedown 当场调用会起一个零位移的拖拽，AppKit 立刻判定结束 ——
   表现为窗口闪一下就不动了。
   交互元素（按钮/输入框等）标了 .no-drag，命中它们时不拖拽，避免抢走点击。
   --------------------------------------------------------------------------- */
function useWindowDrag() {
  useEffect(() => {
    if (!window.juku?.startDrag) return
    let armed = false

    const draggable = (target) => {
      const el = target instanceof Element ? target : null
      if (!el) return false
      if (el.closest(".no-drag, button, input, textarea, select, a")) return false
      return Boolean(el.closest(".drag"))
    }

    const onDown = (e) => {
      // 双击不拖拽，留给系统做缩放/最大化
      armed = e.button === 0 && e.detail <= 1 && draggable(e.target)
    }

    const onMove = (e) => {
      if (!armed) return
      // 只启动一次；按住不放且有位移，窗口拖拽才能建立
      armed = false
      if (e.buttons === 0) return
      window.juku.startDrag().catch(() => {})
    }

    const onUp = () => { armed = false }

    window.addEventListener("mousedown", onDown, true)
    window.addEventListener("mousemove", onMove, true)
    window.addEventListener("mouseup", onUp, true)
    return () => {
      window.removeEventListener("mousedown", onDown, true)
      window.removeEventListener("mousemove", onMove, true)
      window.removeEventListener("mouseup", onUp, true)
    }
  }, [])
}

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
function SideFoot({ route, onNavigate }) {
  const on = route.path.startsWith("/settings")
  return (
    <div className="mt-auto flex items-center justify-between gap-2 p-2.5 shadow-[inset_0_1px_0_0_var(--hairline)]">
      <a href="#/settings" onClick={onNavigate} className={`${navItemClass(on)} flex-1`}>
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
  const { canGoBack, canGoForward, goBack, goForward } = useNavigation()
  useWindowDrag()
  const [ready, setReady] = useState(false)
  const [fatal, setFatal] = useState(null)
  const [status, setStatus] = useState(null)
  const [player, setPlayer] = useState(null)
  // 播放页收起为迷你窗时，底层页面要重新露出来，所以「是否迷你」由外壳持有
  const [mini, setMini] = useState(false)
  const [toasts, setToasts] = useState([])
  const [notices, setNotices] = useState([])
  const [onboarding, setOnboarding] = useState(() => localStorage.getItem("juku.onboarded") !== "1")
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => localStorage.getItem("juku.sidebar.collapsed") === "1")

  useEffect(() => {
    localStorage.setItem("juku.sidebar.collapsed", sidebarCollapsed ? "1" : "0")
  }, [sidebarCollapsed])

  // 快捷键 ⌘B / Ctrl+B 切换侧栏
  useEffect(() => {
    const onKey = (e) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === "b" || e.key === "B")) {
        e.preventDefault()
        setSidebarCollapsed((v) => !v)
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])

  useEffect(() => {
    setToastHandler((message, kind) => {
      const id = Math.random()
      setToasts((t) => [...t, { id, message, kind }])
      setNotices((n) => [{ id, message, kind, at: new Date() }, ...n].slice(0, 50))
      setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 3500)
    })
    setPlayerOpener((request) => { setMini(false); setPlayer(request) })
    initViewer().then(() => {
      setReady(true)
      loadFollowing()
    }).catch(setFatal)
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
  // 播放中（且未收成迷你窗）时，内容区整体让位给播放页
  const playing = Boolean(player) && !mini
  // 播放中点侧栏：不结束会话，收成迷你窗继续播，页面同时露出
  const leaveToPage = () => { if (player) setMini(true) }

  return (
    <div className="relative flex h-full bg-surface-0">
      {/* ---------- 侧栏：通高，无独立拖拽条 ---------- */}
      <aside
        className={`flex flex-none flex-col bg-surface-1 shadow-[inset_-1px_0_0_0_var(--hairline)] transition-all duration-200 ease-out overflow-hidden ${
          sidebarCollapsed ? "w-0 opacity-0 pointer-events-none" : "w-[var(--side-w)] opacity-100"
        }`}
      >
        {/* macOS 下红绿灯右侧放置折叠按钮（留足呼吸间距） */}
        <div className="sidebar-titlebar drag flex-none items-center pr-2">
          <IconButton
            className="no-drag text-ink-secondary hover:text-ink hover:bg-[var(--cds-fill-ghost-hover)]"
            size={28}
            round={false}
            onClick={() => setSidebarCollapsed(true)}
            title="折叠侧栏 (⌘B)"
          >
            <SidebarToggleIcon collapsed={false} className="size-[17px]" />
          </IconButton>
        </div>

        <nav className="flex flex-col gap-px px-2.5">
          {NAV.map(({ path, label, icon: Icon, badge }) => {
            const on = route.path.startsWith(path) || (path === "/home" && route.path === "/")
            const count = badge === "downloads" ? downloadCount : 0
            return (
              <a key={path} href={`#${path}`} onClick={leaveToPage} className={navItemClass(on)}>
                <Icon className={`size-[18px] flex-none ${on ? "text-clay" : ""}`} strokeWidth={1.7} />
                {label}
                {count > 0 ? (
                  <span className="ml-auto rounded-pill bg-clay px-1.5 text-[10.5px] font-medium leading-[18px] text-white">{count}</span>
                ) : null}
              </a>
            )
          })}
        </nav>

        <SideFoot route={route} onNavigate={leaveToPage} />
      </aside>

      {/* ---------- 主区：页面与播放页同处一层，播放页不是盖住整窗的浮层 ---------- */}
      <main className="relative flex min-w-0 flex-1 flex-col">
        {/* 播放中隐藏而非卸载：滚动位置、列表状态都留着；收成迷你窗后重新露出 */}
        <div className={playing ? "hidden" : "flex min-h-0 flex-1 flex-col"}>
          <header className={`glass drag relative z-20 flex h-[52px] flex-none items-center gap-2.5 px-[18px] transition-all duration-200 ${
            sidebarCollapsed ? "main-header-collapsed" : "pl-3.5"
          }`}>
            {sidebarCollapsed && (
              <IconButton
                className="no-drag text-ink-secondary hover:text-ink hover:bg-[var(--cds-fill-ghost-hover)] -ml-1 mr-0.5"
                size={28}
                round={false}
                onClick={() => setSidebarCollapsed(false)}
                title="展开侧栏 (⌘B)"
              >
                <SidebarToggleIcon collapsed={true} className="size-[17px]" />
              </IconButton>
            )}

            <IconButton
              className={`no-drag transition-opacity duration-150 ${
                canGoBack ? "text-ink hover:text-ink cursor-pointer" : "text-ink-muted opacity-30 cursor-not-allowed pointer-events-none"
              }`}
              onClick={goBack}
              disabled={!canGoBack}
              title={canGoBack ? "后退" : "无后退记录"}
            >
              <ChevronLeft className="size-[18px]" strokeWidth={1.7} />
            </IconButton>
            <IconButton
              className={`no-drag transition-opacity duration-150 ${
                canGoForward ? "text-ink hover:text-ink cursor-pointer" : "text-ink-muted opacity-30 cursor-not-allowed pointer-events-none"
              }`}
              onClick={goForward}
              disabled={!canGoForward}
              title={canGoForward ? "前进" : "无前进记录"}
            >
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
        </div>

        {/* 播放页与内容区同级：占据 main 的剩余空间，侧栏始终可见可点 */}
        {player ? (
          <Player
            request={player}
            mini={mini}
            onMiniChange={setMini}
            onClose={() => { setPlayer(null); setMini(false) }}
            sidebarCollapsed={sidebarCollapsed}
            onToggleSidebar={() => setSidebarCollapsed((v) => !v)}
          />
        ) : null}
      </main>

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