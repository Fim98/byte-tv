import { useEffect, useMemo, useState } from "react"
import { Check, Download, Heart, MoreHorizontal, Play, RefreshCw } from "lucide-react"
import { apiGet, apiPost } from "../api"
import { ErrorBox, Poster, Row, Skeleton, toast, useAsync } from "../components/common"
import { categoryOf, coverOf, formatTime, releaseText, titleOf } from "../lib/format"
import { download, follow, markCompleted, play } from "../actions"

const PAGE = 30

export default function Detail({ id, route }) {
  const state = useAsync((signal) => apiGet(`/api/desktop/episodes?id=${encodeURIComponent(id)}`, { signal }), [id])
  const [page, setPage] = useState(0)
  const [jump, setJump] = useState("")
  const [saved, setSaved] = useState(null)
  const [menu, setMenu] = useState(false)
  const data = state.data
  const drama = data?.drama
  const episodes = data?.episodes || []
  const progress = data?.progress

  // 打开详情即后台补资料（后端自带冷却），完成后静默刷新。
  useEffect(() => {
    apiPost("/api/ui/dramas/refresh", { dramaId: id }).then(() => setTimeout(() => state.reload(), 4000)).catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id])
  useEffect(() => {
    if (!data) return
    setSaved(Boolean(data.following?.saved))
    if (progress?.index) setPage(Math.floor((progress.index - 1) / PAGE))
    if (route.query.get("play") === "1") play({ dramaId: id, title: titleOf(drama), resume: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data])

  const pages = Math.max(1, Math.ceil(episodes.length / PAGE))
  const visible = useMemo(() => episodes.slice(page * PAGE, page * PAGE + PAGE), [episodes, page])
  if (state.loading && !data) return <div className="detail"><div className="grid"><Skeleton count={6} /></div></div>
  if (state.error) return <ErrorBox error={state.error} onRetry={state.reload} />
  if (!drama) return null
  const cover = coverOf(drama)
  const title = titleOf(drama)
  const finishedAll = progress?.completed && progress.index >= episodes.length
  const mainLabel = !progress ? "开始观看 第 1 集" : finishedAll ? "重新观看" : `继续播放 第 ${progress.episode || progress.index} 集 · ${formatTime(progress.position)}`
  const startEpisode = (ep) => play({ dramaId: id, title, episode: ep.index, taskId: undefined })
  const toggleFollow = async () => { if (await follow(id, !saved)) setSaved(!saved) }
  const meta = [categoryOf(drama), drama.onlineDate ? `${String(drama.onlineDate).slice(0, 10)} 上线` : "", drama.heat ? `热度 ${drama.heat}` : ""].filter(Boolean)
  return (
    <div className="detail">
      <section className="d-hero">
        {cover ? <div className="detail-bg" style={{ backgroundImage: `url("${cover}")` }} /> : null}
        <div className="detail-left">
          <div className="detail-poster">{cover ? <img src={cover} alt="" /> : <span className="poster-fallback">{title.slice(0, 1)}</span>}</div>
        </div>
        <div className="detail-main">
          <div className="chips">
            <span className="chip red">红果独播</span>
            {episodes.length ? <span className="chip">全 {drama.totalEpisode || episodes.length} 集</span> : null}
            {releaseText(drama.releaseStatus) ? <span className={`chip ${drama.releaseStatus === "completed" || releaseText(drama.releaseStatus) === "已完结" ? "green" : ""}`}>{releaseText(drama.releaseStatus)}</span> : null}
            {data.following?.knownEpisodes && episodes.length > data.following.knownEpisodes ? <span className="chip green">新增 {episodes.length - data.following.knownEpisodes} 集</span> : null}
            {drama.vip ? <span className="chip amber">VIP</span> : null}
          </div>
          <h1>{title}</h1>
          <div className="meta-row">
            {drama.score ? <span className="meta-score">{drama.score} 分</span> : null}
            {meta.map((m) => <span key={m}>{m}</span>)}
            {(drama.tags || []).slice(0, 4).map((t) => <span key={t} className="chip">{t}</span>)}
          </div>
          {drama.intro ? <p className="intro">{drama.intro}</p> : null}
          <div className="actions">
            <button className="btn primary" onClick={() => play({ dramaId: id, title, resume: Boolean(progress) && !finishedAll, episode: finishedAll ? 1 : undefined })}><Play size={15} fill="currentColor" />{mainLabel}</button>
            {progress && !finishedAll ? <button className="btn ghost" onClick={() => play({ dramaId: id, title, episode: 1 })}>从第 1 集开始</button> : null}
            <button className={`round ${saved ? "brand" : ""}`} title={saved ? "已追剧（点击取消）" : "追剧"} onClick={toggleFollow}>{saved ? <Check size={17} /> : <Heart size={17} />}</button>
            <button className="round" title="下载全剧" onClick={() => download(id)}><Download size={17} /></button>
            <div className="menu-wrap">
              <button className="round" title="更多" onClick={() => setMenu(!menu)}><MoreHorizontal size={17} /></button>
              {menu ? (
                <div className="menu" onMouseLeave={() => setMenu(false)}>
                  <button onClick={() => { markCompleted(id, true); setMenu(false) }}><Check size={13} /> 标为已看</button>
                  <button onClick={() => { apiGet(`/api/desktop/episodes?id=${encodeURIComponent(id)}&refresh=1`).then(state.setData).then(() => toast("资料与选集已刷新", "ok")).catch((e) => toast(e.message, "error")); setMenu(false) }}><RefreshCw size={13} /> 刷新资料与选集</button>
                </div>
              ) : null}
            </div>
          </div>
        </div>
      </section>
      <div className="detail-body">
        <section className="episodes">
          <header>
            <h3>分集列表 · 共 {episodes.length} 集</h3>
            {pages > 1 ? (
              <div className="seg">
                {Array.from({ length: pages }, (_, i) => <button key={i} className={i === page ? "on" : ""} onClick={() => setPage(i)}>{i * PAGE + 1}-{Math.min(episodes.length, (i + 1) * PAGE)}</button>)}
              </div>
            ) : null}
            <label className="jump">跳到第<input value={jump} onChange={(e) => setJump(e.target.value.replace(/\D/g, ""))} onKeyDown={(e) => { if (e.key === "Enter" && jump) { const n = Math.min(episodes.length, Math.max(1, Number(jump))); setPage(Math.floor((n - 1) / PAGE)); const ep = episodes[n - 1]; if (ep) startEpisode(ep) } }} />集</label>
            <span className="grow" />
            <span className="legend"><i className="lg-watched" />已看<i className="lg-current" />当前<i className="lg-local" />本地已下载</span>
          </header>
          {data.onlineError ? <p className="warn">{data.onlineError}{data.localCount ? "，以下为本地已下载分集" : ""}</p> : null}
          <div className="ep-grid">
            {visible.map((ep) => (
              <button key={ep.index} className={`ep ${ep.watched ? "watched" : ""} ${ep.current ? "current" : ""}`} onClick={() => startEpisode(ep)} title={ep.title}>
                第 {ep.episode || ep.index} 集{ep.current ? " · 播放中" : ep.watched ? " · 已看" : ep.duration ? ` · ${formatTime(ep.duration)}` : ""}
                {ep.local ? <i className="ep-local" /> : null}
                {ep.vip ? <i className="ep-vip">VIP</i> : null}
                {ep.current && ep.duration ? <i className="ep-bar" style={{ width: `${Math.min(100, (ep.position / ep.duration) * 100)}%` }} /> : null}
              </button>
            ))}
          </div>
          <footer className="muted">
            <button className="link" onClick={() => download(id)}><Download size={13} /> 下载全剧（按设置画质）</button>
            <span>“已看”依据观看记录推算：当前集之前视为已看</span>
          </footer>
        </section>
        <Similar />
      </div>
    </div>
  )
}

function Similar() {
  const state = useAsync((signal) => apiPost("/api/ui/recommendations", { genre: "short_play", offset: 0, sessionId: "", seen: [] }, { signal }), [])
  if (state.error) return null
  return (
    <Row title="同类推荐" hint="来自红果分类推荐">
      {state.loading ? <Skeleton count={6} /> : (state.data?.data || []).slice(0, 12).map((d) => <Poster key={d.id} drama={d} size="sm" />)}
    </Row>
  )
}
