import { useEffect, useState } from "react"
import { Download, Play, Plus } from "lucide-react"
import { apiGet, apiPost } from "../api"
import { Empty, ErrorBox, Poster, Row, Segmented, Skeleton, useAsync } from "../components/common"
import { categoryOf, coverOf, episodesOf, releaseText, titleOf } from "../lib/format"
import { download, follow, play } from "../actions"
import { navigate } from "../router"

const BOARDS = [["hongguo-hot", "总热播"], ["hongguo-real", "真人剧"], ["hongguo-comic", "漫剧"], ["hongguo-ai", "AI 剧"]]
const GENRES = [["short_play", "真人剧"], ["comic_series", "漫剧"], ["ai_series", "AI 剧"]]

function Hero({ items }) {
  const [index, setIndex] = useState(0)
  const [paused, setPaused] = useState(false)
  useEffect(() => {
    if (paused || items.length < 2) return
    const timer = setInterval(() => setIndex((i) => (i + 1) % items.length), 6000)
    return () => clearInterval(timer)
  }, [paused, items.length])
  if (!items.length) return null
  const { drama, rank } = items[index % items.length]
  const cover = coverOf(drama)
  return (
    <section className="hero" onMouseEnter={() => setPaused(true)} onMouseLeave={() => setPaused(false)}>
      {cover ? <div className="hero-bg" style={{ backgroundImage: `url("${cover}")` }} /> : null}
      <div className="hero-body">
        <div className="chips">
          <span className="chip red">总热播榜 No.{rank}</span>
          {categoryOf(drama) ? <span className="chip">{categoryOf(drama)}</span> : null}
          {episodesOf(drama) ? <span className="chip">{episodesOf(drama)} 集{releaseText(drama.releaseStatus) ? " · " + releaseText(drama.releaseStatus) : ""}</span> : null}
        </div>
        <h1>{titleOf(drama)}</h1>
        <p className="intro">{drama.intro || drama.desc || ""}</p>
        <div className="actions">
          <button className="btn white" onClick={() => play({ dramaId: drama.id, title: titleOf(drama), resume: true })}><Play size={16} />立即播放</button>
          <button className="btn ghost" onClick={() => follow(drama.id)}><Plus size={16} />加入追剧</button>
          <button className="btn ghost" onClick={() => download(drama.id)}><Download size={16} />下载</button>
        </div>
        <div className="dots">{items.map((_, i) => <i key={i} className={i === index ? "on" : ""} onClick={() => setIndex(i)} />)}</div>
      </div>
      <div className="hero-poster" onClick={() => navigate(`/drama/${encodeURIComponent(drama.id)}`)}>{cover ? <img src={cover} alt="" /> : null}</div>
      <div className="hero-thumbs">
        {items.map((it, i) => <button key={it.drama.id} className={i === index ? "on" : ""} onClick={() => setIndex(i)}>{coverOf(it.drama) ? <img src={coverOf(it.drama)} alt="" /> : null}</button>)}
      </div>
    </section>
  )
}

function RankingRow() {
  const [board, setBoard] = useState("hongguo-hot")
  const state = useAsync((signal) => apiGet(`/api/ui/rankings?board=${board}&page=1`, { signal }), [board])
  return (
    <Row title="热播榜" action={<><Segmented value={board} options={BOARDS} onChange={setBoard} /><a className="more" href={`#/rankings?board=${board}`}>完整榜单 ›</a></>}>
      {state.loading ? <Skeleton /> : null}
      <ErrorBox error={state.error} onRetry={state.reload} />
      {(state.data?.items || []).slice(0, 10).map((item) => (
        <Poster key={item.drama.id} drama={item.drama} rank={item.rank} subtitle={item.metric ? `🔥 ${item.metric}` : ""} onFollow={() => follow(item.drama.id)} onDownload={() => download(item.drama.id)} />
      ))}
    </Row>
  )
}

function RecommendRow() {
  const [genre, setGenre] = useState("short_play")
  const [page, setPage] = useState({ items: [], offset: 0, sessionId: "", loading: false, error: null, hasMore: true })
  const load = async (reset) => {
    const base = reset ? { items: [], offset: 0, sessionId: "" } : page
    setPage((p) => ({ ...p, ...base, loading: true, error: null }))
    try {
      const r = await apiPost("/api/ui/recommendations", { genre, offset: base.offset, sessionId: base.sessionId, seen: base.items.map((d) => d.id).slice(-500) })
      setPage({ items: [...base.items, ...(r.data || [])], offset: r.nextOffset || 0, sessionId: r.sessionId || "", hasMore: r.hasMore, loading: false, error: null })
    } catch (error) {
      setPage((p) => ({ ...p, loading: false, error }))
    }
  }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { load(true) }, [genre])
  return (
    <Row title="分类推荐" hint="来自红果分类推荐，非个性化" action={<><Segmented value={genre} options={GENRES} onChange={setGenre} />{page.hasMore ? <button className="more" onClick={() => load(false)} disabled={page.loading}>继续推荐 ›</button> : null}</>}>
      {page.loading && !page.items.length ? <Skeleton /> : null}
      <ErrorBox error={page.error} onRetry={() => load(page.items.length === 0)} />
      {page.items.map((d) => <Poster key={d.id} drama={d} onFollow={() => follow(d.id)} onDownload={() => download(d.id)} />)}
    </Row>
  )
}

export default function Home() {
  const home = useAsync((signal) => apiGet("/api/desktop/home", { signal }), [])
  const hot = useAsync((signal) => apiGet("/api/ui/rankings?board=hongguo-hot&page=1", { signal }), [])
  const data = home.data || {}
  const heroItems = (hot.data?.items || []).slice(0, 5)
  return (
    <div className="home">
      {heroItems.length ? <Hero items={heroItems} /> : <div className="hero placeholder">{hot.error ? <ErrorBox error={hot.error} onRetry={hot.reload} /> : <span className="muted">正在读取总热播榜…</span>}</div>}
      <ErrorBox error={home.error} onRetry={home.reload} />
      {(data.continue || []).length ? (
        <Row title="继续观看" hint="按分集与进度续播" action={<a className="more" href="#/history">全部历史 ›</a>}>
          {data.continue.map((item) => (
            <Poster key={item.id} drama={item}
              progress={item.total ? (item.index / item.total) * 100 : 0}
              subtitle={`第 ${item.index} / ${item.total || "?"} 集`}
              onPlay={() => play({ dramaId: item.id, title: item.title, resume: true, fromHistory: true })} />
          ))}
        </Row>
      ) : null}
      {data.followingNew ? (
        <Row title="追剧有新集" action={<a className="more" href="#/following">全部追剧 ›</a>}>
          {data.following.filter((f) => f.newEpisodes > 0).map((f) => <Poster key={f.id} drama={f} badge={`新增 ${f.newEpisodes} 集`} />)}
        </Row>
      ) : null}
      <RankingRow />
      <RecommendRow />
      {(data.local || []).length ? (
        <Row title="本地已下载" action={<a className="more" href="#/downloads">下载管理 ›</a>}>
          {data.local.map((d) => <Poster key={d.id} drama={d} />)}
        </Row>
      ) : null}
      {!home.loading && !(data.continue || []).length && !(data.library?.total) ? <Empty>剧库还是空的：可以从上方榜单和推荐开始，或点击“检查新内容”。</Empty> : null}
    </div>
  )
}
