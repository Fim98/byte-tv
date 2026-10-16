import { useEffect, useState } from "react"
import { ChevronRight, Download, Play, Plus } from "lucide-react"
import { apiGet, apiPost } from "../api"
import { Empty, ErrorBox, Button, ProgressBar, Segmented, SkeletonPoster, Tag, useAsync } from "../components/cds"
import { PageHead, Poster, PosterGrid, Row } from "../components/poster"
import { categoryOf, coverOf, episodesOf, formatTime, releaseText, titleOf } from "../lib/format"
import { download, follow, play } from "../actions"
import { navigate } from "../router"

const BOARDS = [["hongguo-hot", "总热播"], ["hongguo-real", "真人剧"], ["hongguo-comic", "漫剧"], ["hongguo-ai", "AI 剧"]]
const GENRES = [["short_play", "真人剧"], ["comic_series", "漫剧"], ["ai_series", "AI 剧"]]

/* ---------------------------------------------------------------------------
   Hero：主推卡，承担「一键续播」。自动轮播，悬停暂停。
   --------------------------------------------------------------------------- */
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
  const title = titleOf(drama)

  return (
    <section
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      className="relative mb-5 h-[262px] overflow-hidden rounded-lg bg-[#171614]"
    >
      {cover ? <img src={cover} alt="" className="absolute inset-0 size-full scale-110 object-cover blur-2xl" /> : null}
      {/* 遮罩：保证白字在两种主题下都够对比，不随主题变浅 */}
      <div className="absolute inset-0 bg-gradient-to-r from-black/95 via-black/85 to-black/25" />
      <div className="absolute inset-0 bg-gradient-to-t from-black/55 to-transparent" />

      <div key={index} className="absolute inset-y-0 left-0 flex max-w-[560px] animate-fade-up flex-col justify-center gap-2.5 px-7">
        <div className="flex flex-wrap items-center gap-1.5">
          <Tag className="!bg-clay !text-white">红果热播榜</Tag>
          {rank ? <Tag className="!bg-white/15 !text-white/90">TOP {rank}</Tag> : null}
          {categoryOf(drama) ? <Tag className="!bg-white/15 !text-white/90">{categoryOf(drama)}</Tag> : null}
          {releaseText(drama.releaseStatus) ? <Tag className="!bg-white/15 !text-white/90">{releaseText(drama.releaseStatus)}</Tag> : null}
        </div>

        <h1 className="!text-white text-[30px] font-medium leading-[1.2] tracking-[-.03em]">{title}</h1>

        <p className="!text-white/60 text-[11.5px]">
          {[episodesOf(drama) ? `全 ${episodesOf(drama)} 集` : "", drama.heat ? `热度 ${drama.heat}` : ""].filter(Boolean).join(" · ")}
        </p>

        {(drama.intro || drama.desc) ? (
          <p className="!text-white/70 line-clamp-2 text-[12.5px] leading-relaxed">{drama.intro || drama.desc}</p>
        ) : null}

        <div className="mt-1.5 flex items-center gap-2">
          <Button
            variant="primary" size="lg" icon={Play}
            className="h-[38px] gap-1.5"
            onClick={() => play({ dramaId: drama.id, title, resume: true })}
          >
            立即播放
          </Button>
          <Button
            size="lg"
            className="!bg-white/15 !text-white h-[38px] hover:!bg-white/25"
            icon={Plus}
            onClick={() => follow(drama.id)}
          >
            追剧
          </Button>
          <Button
            size="lg"
            className="!bg-white/15 !text-white h-[38px] hover:!bg-white/25"
            icon={Download}
            onClick={() => download(drama.id)}
          >
            下载
          </Button>
        </div>
      </div>

      {cover ? (
        <button
          type="button"
          onClick={() => navigate(`/drama/${encodeURIComponent(drama.id)}`)}
          title={title}
          className="absolute right-12 top-1/2 hidden h-[200px] w-[112px] -translate-y-1/2 overflow-hidden rounded-card shadow-film xl:block"
        >
          <img src={cover} alt="" className="size-full object-cover" />
        </button>
      ) : null}

      <div className="absolute bottom-4 left-7 flex items-center gap-1.5">
        {items.map((item, i) => (
          <button
            key={i}
            type="button"
            title={titleOf(item.drama)}
            onClick={() => setIndex(i)}
            className={`h-1 rounded-pill transition-all duration-300 ease-out ${
              i === index ? "w-5 bg-clay" : "w-1 bg-white/35 hover:bg-white/60"
            }`}
          />
        ))}
      </div>
    </section>
  )
}

function HeroSkeleton() {
  return (
    <div className="relative mb-5 h-[262px] overflow-hidden rounded-lg bg-surface-2">
      <div className="absolute inset-y-0 left-0 flex flex-col justify-center gap-3 px-7">
        <SkeletonPoster className="h-[22px] w-24 rounded-xs" />
        <SkeletonPoster className="h-8 w-56 rounded-sm" />
        <SkeletonPoster className="h-3 w-36 rounded-xs" />
        <SkeletonPoster className="h-12 w-72 rounded-xs" />
      </div>
    </div>
  )
}

/* ---------------------------------------------------------------------------
   继续观看：横向走廊，进度条为视觉主轴
   --------------------------------------------------------------------------- */
function ContinueCard({ item }) {
  const cover = coverOf(item)
  const pct = item.total ? (item.index / item.total) * 100 : 0
  return (
    <button
      type="button"
      onClick={() => play({ dramaId: item.id, title: item.title, resume: true, fromHistory: true })}
      className="w-full text-left"
    >
      <span className="relative block h-[130px] w-full overflow-hidden rounded-card bg-[#171614]">
        {cover ? <img src={cover} alt="" loading="lazy" className="absolute inset-0 size-full object-cover" /> : null}
        <span className="absolute inset-x-0 bottom-0 h-1/2 bg-gradient-to-t from-black/80 to-transparent" />
        <Tag className="absolute right-2 top-2 bg-black/50 text-white backdrop-blur-sm">
          第 {item.episode || item.index} 集
        </Tag>
        <span className="absolute inset-x-2 bottom-2">
          <span className="mb-1.5 block truncate text-xs font-medium text-white [text-shadow:0_1px_6px_rgba(0,0,0,.7)]">
            {titleOf(item)}
          </span>
          <ProgressBar value={pct} className="bg-white/30" />
        </span>
      </span>
      <span className="mt-1.5 block truncate text-[11px] text-ink-muted">
        {item.total ? `共 ${item.total} 集 · ` : ""}
        {item.position ? `看到 ${formatTime(item.position)}` : "继续观看"}
      </span>
    </button>
  )
}

/* ---------------------------------------------------------------------------
   榜单走廊
   --------------------------------------------------------------------------- */
function RankingRow() {
  const [board, setBoard] = useState("hongguo-hot")
  const state = useAsync((signal) => apiGet(`/api/ui/rankings?board=${board}&page=1`, { signal }), [board])
  const items = (state.data?.items || []).slice(0, 10)

  return (
    <Row
      title="红果热播榜" en="Trending"
      action={
        <div className="flex items-center gap-2.5">
          <Segmented value={board} options={BOARDS} onChange={setBoard} />
          <button
            type="button"
            className="flex items-center gap-1 text-xs text-ink-secondary hover:text-clay"
            onClick={() => navigate(`/rankings?board=${board}`)}
          >
            完整榜单<ChevronRight className="size-3" />
          </button>
        </div>
      }
    >
      {state.loading
        ? Array.from({ length: 5 }, (_, i) => <SkeletonPoster key={i} className="h-[132px]" />)
        : items.map((item) => (
            <Poster
              key={item.drama.id} drama={item.drama} rank={item.rank} size="sm"
              subtitle={item.metric ? item.metric : [episodesOf(item.drama) ? `全 ${episodesOf(item.drama)} 集` : ""].filter(Boolean).join(" · ")}
              onFollow={() => follow(item.drama.id)}
              onDownload={() => download(item.drama.id)}
            />
          ))}
    </Row>
  )
}

/* ---------------------------------------------------------------------------
   分类推荐
   --------------------------------------------------------------------------- */
function RecommendRow() {
  const [genre, setGenre] = useState("short_play")
  const [page, setPage] = useState({ items: [], offset: 0, sessionId: "", loading: false, error: null, hasMore: true })

  const load = async (reset) => {
    const base = reset ? { items: [], offset: 0, sessionId: "" } : page
    setPage((p) => ({ ...p, ...base, loading: true, error: null }))
    try {
      const r = await apiPost("/api/ui/recommendations", {
        genre, offset: base.offset, sessionId: base.sessionId,
        seen: base.items.map((d) => d.id).slice(-500),
      })
      setPage({
        items: [...base.items, ...(r.data || [])],
        offset: r.nextOffset || 0, sessionId: r.sessionId || "",
        hasMore: r.hasMore, loading: false, error: null,
      })
    } catch (error) {
      setPage((p) => ({ ...p, loading: false, error }))
    }
  }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { load(true) }, [genre])

  return (
    <Row
      title="分类推荐" en="For you" extra="来自红果分类推荐"
      action={
        <div className="flex items-center gap-2.5">
          <Segmented value={genre} options={GENRES} onChange={setGenre} />
          {page.hasMore ? (
            <Button size="xs" variant="outline" disabled={page.loading} onClick={() => load(false)}>继续推荐</Button>
          ) : null}
        </div>
      }
    >
      {page.loading && !page.items.length
        ? Array.from({ length: 5 }, (_, i) => <SkeletonPoster key={i} className="h-[212px]" />)
        : page.items.map((d) => (
            <Poster
              key={d.id} drama={d}
              subtitle={[categoryOf(d), episodesOf(d) ? `共 ${episodesOf(d)} 集` : ""].filter(Boolean).join(" · ")}
              onFollow={() => follow(d.id)}
              onDownload={() => download(d.id)}
            />
          ))}
    </Row>
  )
}

/* ---------------------------------------------------------------------------
   最新上线：两列紧凑列表
   --------------------------------------------------------------------------- */
function LatestGrid({ items }) {
  return (
    <div className="grid grid-cols-2 gap-2.5">
      {items.map((d) => {
        const cover = coverOf(d)
        const title = titleOf(d)
        return (
          <div key={d.id} className="oncard flex items-center gap-3 rounded-card bg-surface-2 p-2.5 shadow-panel">
            <button
              type="button"
              onClick={() => navigate(`/drama/${encodeURIComponent(d.id)}`)}
              className="relative h-14 w-[42px] flex-none overflow-hidden rounded-xs bg-[#171614]"
            >
              {cover ? <img src={cover} alt="" loading="lazy" className="size-full object-cover" /> : null}
            </button>
            <div className="min-w-0 flex-1">
              <div className="truncate text-[12.5px] font-medium text-ink">{title}</div>
              <div className="mt-0.5 truncate text-[11px] text-ink-muted">
                {[categoryOf(d), releaseText(d.releaseStatus), episodesOf(d) ? `共 ${episodesOf(d)} 集` : ""].filter(Boolean).join(" · ")}
              </div>
            </div>
            <Button size="xs" variant="outline" className="flex-none" icon={Plus} onClick={() => follow(d.id)}>
              追剧
            </Button>
          </div>
        )
      })}
    </div>
  )
}

export default function Home() {
  const home = useAsync((signal) => apiGet("/api/desktop/home", { signal }), [])
  const hot = useAsync((signal) => apiGet("/api/ui/rankings?board=hongguo-hot&page=1", { signal }), [])
  const data = home.data || {}
  const hotItems = hot.data?.items || []
  const heroItems = hotItems.slice(0, 5)
  const newItems = (data.following || []).filter((f) => f.newEpisodes > 0)
  const empty = !home.loading && !(data.continue || []).length && !data.library?.total

  return (
    <div className="flex flex-col gap-6">
      {heroItems.length ? <Hero items={heroItems} /> : <HeroSkeleton />}
      <ErrorBox error={home.error || hot.error} onRetry={() => { home.reload(); hot.reload() }} />

      {(data.continue || []).length ? (
        <Row
          title="继续观看" en="Resume"
          action={<a className="flex items-center gap-1 text-xs text-ink-secondary hover:text-clay" href="#/history">全部历史<ChevronRight className="size-3" /></a>}
        >
          {data.continue.map((item) => <ContinueCard key={item.id} item={item} />)}
        </Row>
      ) : null}

      {newItems.length ? (
        <Row
          title="追剧有新集" en="New"
          action={<a className="flex items-center gap-1 text-xs text-ink-secondary hover:text-clay" href="#/following">全部追剧<ChevronRight className="size-3" /></a>}
        >
          {newItems.map((f) => <Poster key={f.id} drama={f} size="sm" badge={`新增 ${f.newEpisodes} 集`} />)}
        </Row>
      ) : null}

      <RankingRow />
      <RecommendRow />

      {hotItems.length >= 12 ? (
        <section>
          <div className="mb-3 flex items-center gap-2.5">
            <h3 className="text-sm font-medium tracking-[-.015em] text-ink">最新上线</h3>
            <span className="text-[10.5px] uppercase tracking-[.08em] text-ink-muted">Latest</span>
          </div>
          <LatestGrid items={hotItems.slice(10, 16)} />
        </section>
      ) : null}

      {(data.local || []).length ? (
        <section>
          <div className="mb-3 flex items-center gap-2.5">
            <h3 className="text-sm font-medium tracking-[-.015em] text-ink">本地已下载</h3>
            <span className="grow" />
            <a className="flex items-center gap-1 text-xs text-ink-secondary hover:text-clay" href="#/downloads">下载管理<ChevronRight className="size-3" /></a>
          </div>
          <PosterGrid>
            {data.local.slice(0, 12).map((d) => <Poster key={d.id} drama={d} size="sm" />)}
          </PosterGrid>
        </section>
      ) : null}

      {empty ? <Empty>剧库还是空的：可以从上方榜单和推荐开始，或点击顶栏的刷新按钮检查新内容。</Empty> : null}
    </div>
  )
}