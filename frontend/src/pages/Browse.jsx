import { useEffect, useMemo, useRef, useState } from "react"
import { apiGet } from "../api"
import { Empty, ErrorBox, Poster, Skeleton, useAsync } from "../components/common"
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

function FilterLine({ label, value, options, onChange }) {
  return (
    <div className="filter-line">
      <span className="label">{label}</span>
      {options.map(([key, name]) => <button key={key} className={key === value ? "on" : ""} onClick={() => onChange(key)}>{name}</button>)}
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
    return [...counts.entries()].sort((a, b) => b[1] - a[1]).slice(0, 18).map(([t]) => [t, t])
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
    const observer = new IntersectionObserver((entries) => entries[0].isIntersecting && setLimit((l) => l + 60), { rootMargin: "600px" })
    observer.observe(sentinel.current)
    return () => observer.disconnect()
  }, [results.length])
  const hiddenOnline = q && online.items.length > 0 && (type || tag || status) && !results.some((d) => online.items.some((o) => o.id === d.id))
  return (
    <div className="browse">
      <div className="page-head"><div className="tg"><h1 className="page-title">{q ? `搜索「${q}」` : "剧库"}</h1><p className="page-sub">{q ? "本地剧库与红果联网结果合并显示" : "按类型、题材、状态筛选本机剧库"} · 共 {results.length} 部{library.data?.loading ? " · 剧库更新中" : ""}</p></div></div>
      <div className="filters">
        <FilterLine label="类型" value={type} options={TYPES} onChange={(v) => set("type", v)} />
        {tags.length ? <FilterLine label="题材" value={tag} options={[["", "全部"], ...tags]} onChange={(v) => set("tag", v)} /> : null}
        <FilterLine label="状态" value={status} options={STATUS} onChange={(v) => set("status", v)} />
        <FilterLine label="排序" value={sort} options={SORTS} onChange={(v) => set("sort", v)} />
      </div>
      {q && online.loading ? <p className="muted">正在联网搜索红果…</p> : null}
      <ErrorBox error={online.error} />
      {hiddenOnline ? <p className="muted">分类筛选隐藏了联网结果，<button className="link" onClick={() => navigate(`/browse?q=${encodeURIComponent(q)}`)}>取消分类筛选</button></p> : null}
      <ErrorBox error={library.error} onRetry={library.reload} />
      <div className="grid">
        {library.loading && !all.length ? <Skeleton count={12} /> : null}
        {results.slice(0, limit).map((d) => (
          <Poster key={d.id} drama={d} subtitle={[categoryOf(d), episodesOf(d) ? `${episodesOf(d)}集` : ""].filter(Boolean).join(" · ")} onFollow={() => follow(d.id)} onDownload={() => download(d.id)} />
        ))}
      </div>
      <div ref={sentinel} />
      {!library.loading && !online.loading && results.length === 0 ? <Empty>没有符合条件的短剧{q ? "，可换个关键词" : "，可点击右上角“检查新内容”更新剧库"}</Empty> : null}
    </div>
  )
}
