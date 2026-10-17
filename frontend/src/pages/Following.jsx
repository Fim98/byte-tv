import { useMemo, useState } from "react"
import { Play, Search, Trash2 } from "lucide-react"
import { apiGet, apiPost } from "../api"
import { Button, Empty, ErrorBox, IconButton, ProgressBar, Segmented, Tag, toast, useAsync } from "../components/cds"
import { Poster, PosterGrid } from "../components/poster"
import { coverOf, dayLabel, formatTime, titleOf } from "../lib/format"
import { follow, markCompleted, play } from "../actions"
import { navigate } from "../router"

const FILTERS = [["all", "全部"], ["want", "想看"], ["watching", "在看"], ["done", "已看"]]

/* ---------------------------------------------------------------------------
   观看历史行：进度条为视觉主轴
   --------------------------------------------------------------------------- */
function HistoryRow({ item, onPlay, onRemove }) {
  const pct = item.total ? (item.index / item.total) * 100 : 0
  const cover = coverOf(item)
  return (
    <div className="group flex items-center gap-3.5 rounded-card bg-surface-2 px-3.5 py-2.5 shadow-panel transition-shadow duration-200 ease-out hover:shadow-panel-hover">
      <button
        type="button"
        onClick={() => navigate(`/drama/${encodeURIComponent(item.id)}`)}
        className="relative aspect-[9/16] w-10 flex-none overflow-hidden rounded-xs bg-[#171614]"
      >
        {cover ? <img src={cover} alt="" loading="lazy" className="size-full object-cover" /> : null}
      </button>

      <div className="min-w-0 flex-1">
        <button
          type="button"
          onClick={() => navigate(`/drama/${encodeURIComponent(item.id)}`)}
          className="block max-w-full truncate text-left text-[13px] font-medium text-ink hover:text-clay"
        >
          {titleOf(item)}
        </button>
        <div className="mt-0.5 text-[11px] text-ink-muted">
          看到第 {item.episode || item.index} 集
          {item.total ? ` · 共 ${item.total} 集` : ""}
          {item.position ? ` · ${formatTime(item.position)}` : ""}
          {` · ${item.mode === "collection" ? "本地" : "在线"}`}
          {` · ${new Date(item.watchedAt).toLocaleTimeString()}`}
        </div>
        <ProgressBar value={pct} className="mt-2 max-w-[300px]" />
      </div>

      {item.total ? (
        <span className={`tnum w-9 flex-none text-right text-[11.5px] ${pct >= 80 ? "text-clay" : "text-ink-muted"}`}>
          {Math.round(pct)}%
        </span>
      ) : null}

      <Button size="xs" variant="primary" icon={Play} onClick={onPlay}>继续</Button>
      <IconButton size={26} title="从历史删除" onClick={onRemove} className="opacity-0 transition-opacity group-hover:opacity-100">
        <Trash2 className="size-[15px]" strokeWidth={1.7} />
      </IconButton>
    </div>
  )
}

export default function Following({ tab }) {
  const state = useAsync((signal) => apiGet("/api/desktop/home?full=1", { signal }), [])
  const [filter, setFilter] = useState("all")
  const [query, setQuery] = useState("")

  const data = state.data || {}
  const historyByID = useMemo(() => new Map((data.continue || []).map((h) => [h.id, h])), [data.continue])
  const following = (data.following || []).filter((f) => f.saved || f.completed)

  const filtered = following.filter((f) => {
    const h = historyByID.get(f.id)
    if (filter === "done") return f.completed
    if (filter === "watching") return !f.completed && h
    if (filter === "want") return !f.completed && !h
    return true
  })

  const newOnes = following.filter((f) => f.newEpisodes > 0)

  const ackAll = async () => {
    await Promise.all(newOnes.map((f) => apiPost("/api/ui/following", { dramaId: f.id, acknowledge: true }).catch(() => {})))
    toast("已全部标为已读", "ok")
    state.reload()
  }

  const history = (data.continue || []).filter((h) => !query || titleOf(h).includes(query))
  const groups = useMemo(() => {
    const map = new Map()
    history.forEach((h) => {
      const k = dayLabel(h.watchedAt)
      if (!map.has(k)) map.set(k, [])
      map.get(k).push(h)
    })
    return [...map.entries()]
  }, [history])

  const removeHistory = async (dramaId) => {
    try {
      await apiPost("/api/ui/playback/history/remove", dramaId ? { dramaId } : { all: true })
      state.reload()
    } catch (e) {
      toast(e.message, "error")
    }
  }

  return (
    <div className="flex flex-col gap-3.5">
      {/* ---------- 页签：下划线式，选中态用陶土色 2px 短线 ---------- */}
      <div className="flex items-center gap-6 shadow-[inset_0_-1px_0_0_var(--hairline)]">
        {[["following", "追剧"], ["history", "观看历史"]].map(([key, label]) => {
          const on = tab === key
          const count = key === "following" ? following.length : (data.continue || []).length
          return (
            <button
              key={key}
              type="button"
              onClick={() => navigate(`/${key}`)}
              className={`relative flex items-center gap-1.5 pb-3 text-[13px] transition-colors duration-[180ms] ease-out ${
                on ? "font-medium text-ink" : "text-ink-secondary hover:text-ink"
              }`}
            >
              {label}
              <em className={`rounded-pill px-1.5 py-px text-[10.5px] not-italic ${
                on ? "bg-clay-light text-clay" : "bg-[var(--cds-fill-secondary)] text-ink-muted"
              }`}>
                {count}
              </em>
              {on ? <i className="absolute inset-x-0 -bottom-px h-0.5 rounded-pill bg-clay" /> : null}
            </button>
          )
        })}
        <span className="grow" />
        {tab === "following" && newOnes.length ? (
          <span className="flex items-center gap-2.5 pb-3 text-[11.5px] text-ink-muted">
            其中 <b className="text-clay">{newOnes.length} 部</b>有新集
            <Button size="xs" variant="outline" onClick={ackAll}>全部标为已读</Button>
          </span>
        ) : null}
        {tab === "history" && history.length ? (
          <span className="pb-3">
            <Button
              size="xs" variant="outline" icon={Trash2}
              onClick={() => window.confirm("确定清空全部观看历史？此操作不可撤销。") && removeHistory("")}
            >
              清空历史记录
            </Button>
          </span>
        ) : null}
      </div>

      <ErrorBox error={state.error} onRetry={state.reload} />

      {tab === "following" ? (
        <>
          <div className="flex items-center gap-2.5">
            <Segmented value={filter} options={FILTERS} onChange={setFilter} />
          </div>

          <PosterGrid>
            {filtered.map((f) => {
              const h = historyByID.get(f.id)
              const total = f.totalEpisode || f.knownEpisodes
              return (
                <div key={f.id} className="min-w-0">
                  <Poster
                    drama={f}
                    isFollowing={true}
                    badge={f.newEpisodes ? `新增 ${f.newEpisodes} 集` : f.completed ? "已看" : ""}
                    progress={h && total ? (h.index / total) * 100 : null}
                    subtitle={h ? `看到第 ${h.index} 集${total ? ` · 共 ${total} 集` : ""}` : "还没看"}
                    onPlay={() => play({ dramaId: f.id, title: f.title, resume: Boolean(h) })}
                    onFollow={async () => { await follow(f.id, false); state.reload() }}
                    onDownload={() => download(f.id)}
                  />
                  <div className="mt-1 flex items-center gap-3">
                    <button
                      type="button"
                      className="text-[11px] text-ink-muted hover:text-danger"
                      onClick={async () => { await follow(f.id, false); state.reload() }}
                    >
                      取消追剧
                    </button>
                    {!f.completed ? (
                      <button
                        type="button"
                        className="text-[11px] text-ink-muted hover:text-clay"
                        onClick={async () => { await markCompleted(f.id); state.reload() }}
                      >
                        标为已看
                      </button>
                    ) : null}
                  </div>
                </div>
              )
            })}
          </PosterGrid>

          {!state.loading && filtered.length === 0 ? (
            <Empty>还没有追剧。在详情页或卡片上点「追剧」即可加入。</Empty>
          ) : null}
        </>
      ) : (
        <>
          <div className="flex items-center gap-3">
            <div className="flex h-8 w-[300px] items-center gap-2 rounded-sm bg-surface-2 pl-2.5 pr-2 shadow-[inset_0_0_0_1px_var(--cds-border)] focus-within:shadow-focus">
              <Search className="size-3.5 flex-none text-ink-muted" strokeWidth={1.7} />
              <input
                value={query}
                placeholder="搜索观看历史"
                onChange={(e) => setQuery(e.target.value)}
                className="min-w-0 flex-1 bg-transparent text-[12.5px] text-ink outline-none placeholder:text-ink-muted"
              />
            </div>
            <span className="text-[11.5px] text-ink-muted">最多保留 500 部</span>
          </div>

          {groups.map(([day, items]) => (
            <section key={day}>
              <div className="mb-2 flex items-center gap-2.5">
                <h3 className="text-[13px] font-medium text-ink">{day}</h3>
                <span className="text-[11px] text-ink-muted">{items.length} 部</span>
              </div>
              <div className="flex flex-col gap-2">
                {items.map((h) => (
                  <HistoryRow
                    key={h.id}
                    item={h}
                    onPlay={() => play({ dramaId: h.id, title: h.title, resume: true, fromHistory: true })}
                    onRemove={() => removeHistory(h.id)}
                  />
                ))}
              </div>
            </section>
          ))}

          {!state.loading && history.length === 0 ? <Empty>暂无观看历史</Empty> : null}
        </>
      )}

      {/* 追剧清单为空时给个切入指引 */}
      {tab === "following" && !state.loading && following.length > 0 && filtered.length === 0 ? (
        <Empty>当前筛选下没有剧集，换个筛选条件看看。</Empty>
      ) : null}
      {tab === "following" && !state.loading && following.length === 0 ? (
        <div className="flex items-center gap-2">
          <Tag>提示</Tag>
          <span className="text-[12px] text-ink-muted">还没有任何追剧记录</span>
        </div>
      ) : null}
    </div>
  )
}