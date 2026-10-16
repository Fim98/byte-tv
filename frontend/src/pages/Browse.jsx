import { useEffect, useMemo, useRef, useState } from "react"
import { ChevronDown, CheckSquare, Search } from "lucide-react"
import { apiGet } from "../api"
import { Button, Empty, ErrorBox, Segmented, SkeletonPoster, useAsync } from "../components/cds"
import { Poster, PosterGrid } from "../components/poster"
import { categoryOf, episodesOf, titleOf } from "../lib/format"
import { download, follow } from "../actions"
import { navigate } from "../router"

const TYPES = [["", "全部"], ["short_play", "真人剧"], ["comic_series", "漫剧"], ["ai_series", "AI剧"]]
const STATUS = [["", "全部"], ["finished", "已完结"], ["ongoing", "连载中"]]
const SORTS = [["", "相关度"], ["heat", "热度"], ["date", "最新"], ["views", "播放最多"], ["name", "名称"]]

function num(v) {
  const s = String(v || "").trim()
  const n = parseFloat(s)
  if (!Number.isFinite(n)) return 0
  return s.includes("亿") ? n * 1e8 : s.includes("万") ? n * 1e4 : n
}

function typeOf(d) {
  const text = [d.category, d.categoryName, d.typeName, d.channelName].join(" ")
  if (/漫|comic/i.test(text)) return "comic_series"
  if (/AI|ai_series/.test(text)) return "ai_series"
  return "short_play"
}

/* 下拉选择器：低对比填充，不加边框 */
function Select({ value, options, onChange, label }) {
  return (
    <div className="relative">
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="h-8 cursor-pointer appearance-none rounded-sm bg-[var(--cds-fill-secondary)] pl-3 pr-7 text-[12.5px] text-ink-secondary outline-none transition-colors duration-[180ms] ease-out hover:text-ink focus-visible:shadow-focus"
      >
        {options.map(([key, name]) => <option key={key} value={key}>{name}</option>)}
      </select>
      <ChevronDown className="pointer-events-none absolute right-2 top-1/2 size-3 -translate-y-1/2 text-ink-muted" />
    </div>
  )
}

export default function Browse({ route }) {
  const q = route.query.get("q") || ""
  const type = route.query.get("type") || ""
  const tag = route.query.get("tag") || ""
  const status = route.query.get("status") || ""
  const sort = route.query.get("sort") || ""

  const set = (key, value) => {
    const params = new URLSearchParams(route.query)
    if (value) params.set(key, value)
    else params.delete(key)
    navigate(`/browse?${params}`)
  }

  const library = useAsync((signal) => apiGet("/api/ui/dramas", { signal }), [])
  const [online, setOnline] = useState({ loading: false, items: [], error: null })

  useEffect(() => {
    if (!q) return setOnline({ loading: false, items: [], error: null })
    const controller = new AbortController()
    setOnline({ loading: true, items: [], error: null })
    apiGet(`/api/ui/search?q=${encodeURIComponent(q)}`, { signal: controller.signal })
      .then((r) => setOnline({ loading: false, items: r.data || [], error: r.warning ? new Error(r.warning) : null }))
      .catch((error) => error.name !== "AbortError" && setOnline({ loading: false, items: [], error }))
    return () => controller.abort()
  }, [q])

  const all = library.data?.data || []

  const tags = useMemo(() => {
    const counts = new Map()
    all.forEach((d) => (d.tags || []).forEach((t) => counts.set(t, (counts.get(t) || 0) + 1)))
    return [...counts.entries()].sort((a, b) => b[1] - a[1]).slice(0, 10).map(([t]) => [t, t])
  }, [all])

  const results = useMemo(() => {
    const keyword = q.toLowerCase()
    const seen = new Set()
    let list = []
    for (const d of [...online.items, ...all]) {
      if (seen.has(d.id)) continue
      seen.add(d.id)
      if (keyword && ![titleOf(d), d.intro, d.desc, (d.tags || []).join(" ")].join(" ").toLowerCase().includes(keyword)) {
        if (!online.items.some((o) => o.id === d.id)) continue
      }
      if (type && typeOf(d) !== type) continue
      if (tag && !(d.tags || []).includes(tag)) continue
      if (status && d.releaseStatus !== status) continue
      list.push(d)
    }
    const by = {
      heat: (a, b) => num(b.heat) - num(a.heat),
      views: (a, b) => num(b.views) - num(a.views),
      date: (a, b) => String(b.onlineDate || "").localeCompare(String(a.onlineDate || "")),
      name: (a, b) => titleOf(a).localeCompare(titleOf(b), "zh-CN"),
    }[sort]
    if (by) list = [...list].sort(by)
    return list
  }, [all, online.items, q, type, tag, status, sort])

  const [limit, setLimit] = useState(60)
  const sentinel = useRef(null)
  useEffect(() => setLimit(60), [q, type, tag, status, sort])
  useEffect(() => {
    if (!sentinel.current) return
    const observer = new IntersectionObserver((e) => e[0].isIntersecting && setLimit((l) => l + 60), { rootMargin: "600px" })
    observer.observe(sentinel.current)
    return () => observer.disconnect()
  }, [results.length])

  const hiddenOnline = q && online.items.length > 0 && (type || tag || status)
    && !results.some((d) => online.items.some((o) => o.id === d.id))

  const searching = !q && !library.loading && results.length === 0

  return (
    <div className="flex flex-col gap-3.5">
      {/* ---------- 筛选条：四条操作压成一行 ---------- */}
      <div className="flex items-center gap-2.5 rounded-lg bg-surface-2 p-3 shadow-panel">
        <div className="flex h-8 w-[300px] flex-none items-center gap-2 rounded-sm bg-surface-3 pl-2.5 pr-2 shadow-[inset_0_0_0_1px_var(--cds-border)] focus-within:shadow-focus">
          <Search className="size-3.5 flex-none text-ink-muted" strokeWidth={1.7} />
          <input
            defaultValue={q}
            placeholder="在剧库中搜索"
            className="min-w-0 flex-1 bg-transparent text-[12.5px] text-ink outline-none placeholder:text-ink-muted"
            onKeyDown={(e) => {
              if (e.key !== "Enter") return
              // 输入法合成期间不触发搜索
              if (e.nativeEvent.isComposing || e.keyCode === 229) return
              const value = e.currentTarget.value.trim()
              navigate(value ? `/browse?q=${encodeURIComponent(value)}` : "/browse")
            }}
          />
          <kbd className="flex-none rounded-xs bg-[var(--cds-fill-secondary)] px-1.5 py-0.5 text-[10px] text-ink-muted">⌘K</kbd>
        </div>

        <Select value={type} options={TYPES} onChange={(v) => set("type", v)} />
        <Select value={status} options={STATUS} onChange={(v) => set("status", v)} />
        <Segmented value={sort} options={SORTS} onChange={(v) => set("sort", v)} />

        <span className="grow" />

        {q ? (
          <Button size="xs" variant="ghost" onClick={() => navigate("/browse")}>清除搜索</Button>
        ) : null}
        <span className="text-[11.5px] text-ink-muted">
          共 <b className="tnum font-medium text-ink-secondary">{results.length}</b> 部
        </span>
        <Button size="xs" variant="outline" icon={CheckSquare}>批量管理</Button>
      </div>

      {/* ---------- 题材胶囊 ---------- */}
      {tags.length ? (
        <div className="flex flex-wrap items-center gap-1.5">
          <button
            type="button"
            onClick={() => set("tag", "")}
            className={`h-[26px] rounded-pill px-3 text-xs transition-colors duration-[180ms] ease-out ${
              !tag ? "bg-clay-light font-medium text-clay" : "bg-[var(--cds-fill-secondary)] text-ink-secondary hover:text-ink"
            }`}
          >
            全部
          </button>
          {tags.map(([key, name]) => {
            const on = key === tag
            return (
              <button
                key={key}
                type="button"
                onClick={() => set("tag", on ? "" : key)}
                className={`h-[26px] rounded-pill px-3 text-xs transition-colors duration-[180ms] ease-out ${
                  on ? "bg-clay-light font-medium text-clay" : "bg-[var(--cds-fill-secondary)] text-ink-secondary hover:text-ink"
                }`}
              >
                {name}
              </button>
            )
          })}
        </div>
      ) : null}

      {/* ---------- 状态提示 ---------- */}
      {q && online.loading ? (
        <p className="flex items-center gap-2 text-[12.5px] text-ink-muted">
          <span className="inline-flex gap-1">
            <i className="size-1 animate-wave rounded-full bg-clay" />
            <i className="size-1 animate-wave rounded-full bg-clay [animation-delay:.12s]" />
            <i className="size-1 animate-wave rounded-full bg-clay [animation-delay:.24s]" />
          </span>
          正在联网搜索红果…
        </p>
      ) : null}
      <ErrorBox error={online.error} />
      {hiddenOnline ? (
        <p className="text-[12.5px] text-ink-muted">
          分类筛选隐藏了联网结果，
          <button type="button" className="text-clay hover:underline" onClick={() => navigate(`/browse?q=${encodeURIComponent(q)}`)}>
            取消分类筛选
          </button>
        </p>
      ) : null}
      <ErrorBox error={library.error} onRetry={library.reload} />

      {/* ---------- 海报网格 ---------- */}
      <PosterGrid>
        {library.loading && !all.length
          ? Array.from({ length: 12 }, (_, i) => <SkeletonPoster key={i} className="h-[212px]" />)
          : results.slice(0, limit).map((d) => (
              <Poster
                key={d.id} drama={d}
                subtitle={[categoryOf(d), episodesOf(d) ? `${episodesOf(d)} 集` : ""].filter(Boolean).join(" · ")}
                onFollow={() => follow(d.id)}
                onDownload={() => download(d.id)}
              />
            ))}
      </PosterGrid>

      <div ref={sentinel} />

      {searching ? (
        <Empty>没有符合条件的短剧{q ? "，可换个关键词" : "，可点击顶栏的刷新按钮更新剧库"}</Empty>
      ) : null}
    </div>
  )
}