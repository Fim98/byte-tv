import { useMemo, useState } from "react"
import { Play, Trash2 } from "lucide-react"
import { apiGet, apiPost } from "../api"
import { Empty, ErrorBox, PageHead, Poster, Segmented, Skeleton, toast, useAsync } from "../components/common"
import { coverOf, dayLabel, formatTime, titleOf } from "../lib/format"
import { follow, markCompleted, play } from "../actions"
import { navigate } from "../router"

const FILTERS = [["all", "全部"], ["want", "想看"], ["watching", "在看"], ["done", "已看"]]

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
    history.forEach((h) => { const k = dayLabel(h.watchedAt); if (!map.has(k)) map.set(k, []); map.get(k).push(h) })
    return [...map.entries()]
  }, [history])
  const removeHistory = async (dramaId) => {
    try { await apiPost("/api/ui/playback/history/remove", dramaId ? { dramaId } : { all: true }); state.reload() } catch (e) { toast(e.message, "error") }
  }
  return (
    <>
      <PageHead title="追剧与历史" sub="追剧清单与观看进度保存在本机，可随时续播">
        {tab === "history" && history.length ? <button className="btn ghost sm" onClick={() => window.confirm("确定清空全部观看历史？此操作不可撤销。") && removeHistory("")}><Trash2 size={14} />清空历史记录</button> : null}
      </PageHead>
      <div className="toolbar">
        <Segmented value={tab} options={[["following", `追剧 ${following.length || ""}`.trim()], ["history", `观看历史 ${(data.continue || []).length || ""}`.trim()]]} onChange={(t) => navigate(`/${t}`)} />
        {tab === "following" ? <Segmented value={filter} options={FILTERS} onChange={setFilter} /> : null}
      </div>
      <ErrorBox error={state.error} onRetry={state.reload} />
      {state.loading && !state.data ? <div className="grid"><Skeleton /></div> : null}
      {tab === "following" ? (
        <>
          {newOnes.length ? <div className="banner green">{newOnes.length} 部追剧有新集<button className="btn ghost sm" onClick={ackAll}>全部标为已读</button></div> : null}
          <div className="grid">
            {filtered.map((f) => {
              const h = historyByID.get(f.id)
              const total = f.totalEpisode || f.knownEpisodes
              return (
                <div key={f.id} className="card-with-menu">
                  <Poster drama={f} badge={f.newEpisodes ? `新增 ${f.newEpisodes} 集` : f.completed ? "已看" : ""}
                    progress={h && total ? (h.index / total) * 100 : null}
                    subtitle={h ? `看到第 ${h.index} 集 · ${total ? `共 ${total} 集` : ""}` : "还没看"}
                    onPlay={() => play({ dramaId: f.id, title: f.title, resume: Boolean(h) })} />
                  <div className="card-actions">
                    <button className="link" onClick={async () => { await follow(f.id, false); state.reload() }}>取消追剧</button>
                    {!f.completed ? <button className="link" onClick={async () => { await markCompleted(f.id); state.reload() }}>标为已看</button> : null}
                  </div>
                </div>
              )
            })}
          </div>
          {!state.loading && filtered.length === 0 ? <Empty>还没有追剧。在详情页或卡片上点“追剧”即可加入。</Empty> : null}
        </>
      ) : (
        <>
          <div className="toolbar">
            <input className="input" placeholder="搜索观看历史" value={query} onChange={(e) => setQuery(e.target.value)} />
            <span className="muted">最多保留 500 部</span>
          </div>
          {groups.map(([day, items]) => (
            <section key={day} className="history-group">
              <h3>{day}</h3>
              {items.map((h) => (
                <div key={h.id} className="history-row">
                  <div className="thumb">{coverOf(h) ? <img src={coverOf(h)} alt="" /> : null}</div>
                  <div className="info">
                    <button className="link title" onClick={() => navigate(`/drama/${encodeURIComponent(h.id)}`)}>{titleOf(h)}</button>
                    <span className="muted">看到 第 {h.episode || h.index} 集 · {formatTime(h.position)} · {h.mode === "collection" ? "本地" : "在线"} · {new Date(h.watchedAt).toLocaleTimeString()}</span>
                    <div className="bar"><i style={{ width: `${h.total ? (h.index / h.total) * 100 : 0}%` }} /></div>
                  </div>
                  {h.total ? <span className={`pct ${h.index / h.total >= 0.8 ? "hi" : ""}`}>{Math.round((h.index / h.total) * 100)}%</span> : null}
                  <button className="btn ghost sm" onClick={() => play({ dramaId: h.id, title: h.title, resume: true, fromHistory: true })}><Play size={13} fill="currentColor" />继续</button>
                  <button className="icon-btn" title="从历史删除" onClick={() => removeHistory(h.id)}><Trash2 size={15} /></button>
                </div>
              ))}
            </section>
          ))}
          {!state.loading && history.length === 0 ? <Empty>暂无观看历史</Empty> : null}
        </>
      )}
    </>
  )
}
