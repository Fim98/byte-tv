import { useEffect, useState } from "react"
import { ChevronRight, Download, Heart, Play } from "lucide-react"
import { apiGet, apiPost } from "../api"
import { Empty, ErrorBox, Button, ProgressBar, Segmented, SkeletonPoster, Tag, useAsync } from "../components/cds"
import { PageHead, Poster, PosterGrid, Row } from "../components/poster"
import { categoryOf, coverOf, episodesOf, formatTime, releaseText, titleOf } from "../lib/format"
import { download, follow, isFollowing as checkIsFollowing, play, subscribeFollowing } from "../actions"
import { navigate } from "../router"

const BOARDS = [["hongguo-hot", "总热播"], ["hongguo-real", "真人剧"], ["hongguo-comic", "漫剧"], ["hongguo-ai", "AI 剧"]]
const GENRES = [["short_play", "真人剧"], ["comic_series", "漫剧"], ["ai_series", "AI 剧"]]

/* ---------------------------------------------------------------------------
   Hero：主推卡，承担「一键续播」。自动轮播，悬停暂停。
   --------------------------------------------------------------------------- */
function Hero({ items }) {
  const [index, setIndex] = useState(0)
  const [paused, setPaused] = useState(false)
  const [, setFollowTick] = useState(0)

  useEffect(() => {
    return subscribeFollowing(() => setFollowTick((t) => t + 1))
  }, [])

  useEffect(() => {
    if (paused || items.length < 2) return
    const timer = setInterval(() => setIndex((i) => (i + 1) % items.length), 6000)
    return () => clearInterval(timer)
  }, [paused, items.length])

  if (!items.length) return null
  const { drama, rank } = items[index % items.length]
  const cover = coverOf(drama)
  const title = titleOf(drama)
  const isHeroFollowing = checkIsFollowing(drama?.id) || Boolean(drama?.saved)

  return (
    <section
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      className="group/hero relative mb-5 h-[262px] overflow-hidden rounded-lg bg-[#171614] select-none"
    >
      {cover ? <img src={cover} alt="" className="absolute inset-0 size-full scale-110 object-cover blur-2xl transition-all duration-500" /> : null}
      {/* 遮罩：保证白字在两种主题下都够对比，不随主题变浅 */}
      <div className="absolute inset-0 bg-gradient-to-r from-black/95 via-black/85 to-black/25" />
      <div className="absolute inset-0 bg-gradient-to-t from-black/55 to-transparent" />

      {/* 主信息区 */}
      <div key={index} className="absolute inset-y-0 left-0 flex max-w-[560px] animate-fade-up flex-col justify-center gap-2.5 px-8">
        <div className="flex flex-wrap items-center gap-1.5">
          <Tag className="!bg-clay !text-white">红果热播榜</Tag>
          {rank ? <Tag className="!bg-white/15 !text-white/90">TOP {rank}</Tag> : null}
          {categoryOf(drama) ? <Tag className="!bg-white/15 !text-white/90">{categoryOf(drama)}</Tag> : null}
          {releaseText(drama.releaseStatus) ? <Tag className="!bg-white/15 !text-white/90">{releaseText(drama.releaseStatus)}</Tag> : null}
        </div>

        <h1 className="!text-white text-[28px] font-medium leading-tight tracking-[-.03em] truncate max-w-full" title={title}>{title}</h1>

        <p className="!text-white/60 text-[11.5px]">
          {[episodesOf(drama) ? `全 ${episodesOf(drama)} 集` : "", drama.heat ? `热度 ${drama.heat}` : ""].filter(Boolean).join(" · ")}
        </p>

        {(drama.intro || drama.desc) ? (
          <p className="!text-white/70 line-clamp-2 text-[12.5px] leading-relaxed">{drama.intro || drama.desc}</p>
        ) : null}

        <div className="mt-1.5 flex items-center gap-2">
          <Button
            variant="primary" size="lg" icon={Play}
            className="h-[38px] gap-1.5 shadow-[0_4px_16px_rgba(0,0,0,0.3)]"
            onClick={() => play({ dramaId: drama.id, title, resume: true })}
          >
            立即播放
          </Button>
          <Button
            size="lg"
            className={`backdrop-blur-md h-[38px] transition-all ${
              isHeroFollowing
                ? "!bg-clay !text-white shadow-md shadow-clay/40"
                : "!bg-white/15 !text-white hover:!bg-white/25 active:scale-95"
            }`}
            icon={Heart}
            onClick={() => follow(drama.id, !isHeroFollowing)}
          >
            {isHeroFollowing ? "已追剧" : "追剧"}
          </Button>
          <Button
            size="lg"
            className="!bg-white/15 backdrop-blur-md !text-white h-[38px] hover:!bg-white/25 active:scale-95 transition-all"
            icon={Download}
            onClick={() => download(drama.id)}
          >
            下载
          </Button>
        </div>
      </div>

      {/* 右侧海报预览 */}
      {cover ? (
        <button
          type="button"
          onClick={() => navigate(`/drama/${encodeURIComponent(drama.id)}`)}
          title={title}
          className="absolute right-14 top-1/2 hidden aspect-[9/16] h-[224px] -translate-y-1/2 overflow-hidden rounded-card shadow-film xl:block transition-transform hover:scale-[1.02] active:scale-95"
        >
          <img src={cover} alt="" className="size-full object-cover" />
        </button>
      ) : null}

      {/* 底部指示器：放大的舒适点击胶囊，无杂乱多余按键 */}
      <div className="absolute bottom-4 left-8 z-20 flex items-center gap-0.5">
        {items.map((item, i) => (
          <button
            key={i}
            type="button"
            title={`切换至第 ${i + 1} 部：${titleOf(item.drama)}`}
            onClick={() => setIndex(i)}
            className="group/ind flex h-6 items-center px-1 cursor-pointer"
          >
            <span
              className={`h-1.5 rounded-full transition-all duration-300 ${
                i === index
                  ? "w-7 bg-clay shadow-[0_0_8px_rgba(217,119,87,0.7)]"
                  : "w-2 bg-white/35 group-hover/ind:bg-white/80 group-hover/ind:w-3.5"
              }`}
            />
          </button>
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
   继续观看：横向走廊，竖版 9:16 封面 + 进度条为视觉主轴
   --------------------------------------------------------------------------- */
function ContinueCard({ item }) {
  const cover = coverOf(item)
  const pct = item.total ? (item.index / item.total) * 100 : 0
  return (
    <button
      type="button"
      onClick={() => play({ dramaId: item.id, title: item.title, resume: true, fromHistory: true })}
      className="group w-full text-left"
    >
      <span className="relative block aspect-[9/16] w-full overflow-hidden rounded-card bg-[#171614]">
        {cover ? <img src={cover} alt="" loading="lazy" className="absolute inset-0 size-full object-cover" /> : null}
        <span className="absolute inset-x-0 bottom-0 h-2/5 bg-gradient-to-t from-black/90 via-black/45 to-transparent" />
        <Tag className="absolute right-1.5 top-1.5 h-[18px] bg-black/50 px-1.5 text-[10px] text-white backdrop-blur-sm">
          第 {item.episode || item.index} 集
        </Tag>
        <span className="absolute inset-x-1.5 bottom-1.5">
          <span className="mb-1.5 block truncate text-[11.5px] font-medium text-white [text-shadow:0_1px_6px_rgba(0,0,0,.7)]">
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
   走廊渲染上限。
   注意这不是「可见数」：Row 的 mask-corridor 淡出区从容器宽度的 88% 开始，
   只有内容真的溢出才能看到「最后一张渐隐」的效果 —— 不溢出就是一排平卡。
   每张 150 + 间距 14 = 164px，需要几张才够溢出到淡出区（内容区 = 窗口 - 侧栏 228 - 页边距 52）：
     1024 → 5 张；1280 → 7 张；1440 → 8 张；1680 → 9 张；1920 → 10 张；2560 → 14 张
   取 16 张，2560 以内的窗口都能保证溢出；后端榜单单页 20 条、推荐 18 条，够用。
   不出现横向滚动条，更多内容走标题栏的「完整榜单 / 查看全部」。
   --------------------------------------------------------------------------- */
const ROW_ITEMS = 16

/* ---------------------------------------------------------------------------
   榜单走廊
   --------------------------------------------------------------------------- */
function RankingRow() {
  const [board, setBoard] = useState("hongguo-hot")
  const state = useAsync((signal) => apiGet(`/api/ui/rankings?board=${board}&page=1`, { signal }), [board])
  const items = (state.data?.items || []).slice(0, ROW_ITEMS)

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
        ? Array.from({ length: 8 }, (_, i) => <SkeletonPoster key={i} className="aspect-[9/16]" />)
        : items.map((item) => (
            <Poster
              key={item.drama.id} drama={item.drama} rank={item.rank}
              subtitle={item.metric ? item.metric : [episodesOf(item.drama) ? `全 ${episodesOf(item.drama)} 集` : ""].filter(Boolean).join(" · ")}
              onFollow={(next) => follow(item.drama.id, next)}
              onDownload={() => download(item.drama.id)}
            />
          ))}
    </Row>
  )
}

/* ---------------------------------------------------------------------------
   分类推荐
   渲染 ROW_ITEMS 张，靠遮罩自然渐隐（同榜单走廊）。
   更多内容交给「查看全部」跳剧库对应题材，不在发现页翻页。
   --------------------------------------------------------------------------- */
function RecommendRow() {
  const [genre, setGenre] = useState("short_play")
  const state = useAsync(
    (signal) => apiPost("/api/ui/recommendations", { genre, offset: 0, sessionId: "", seen: [] }, { signal }),
    [genre],
  )
  const items = (state.data?.data || []).slice(0, ROW_ITEMS)

  return (
    <Row
      title="分类推荐" en="For you" extra="来自红果分类推荐"
      action={
        <div className="flex items-center gap-2.5">
          <Segmented value={genre} options={GENRES} onChange={setGenre} />
          <button
            type="button"
            className="flex items-center gap-1 text-xs text-ink-secondary hover:text-clay"
            onClick={() => navigate(`/browse?type=${genre}`)}
          >
            查看全部<ChevronRight className="size-3" />
          </button>
        </div>
      }
    >
      {state.loading
        ? Array.from({ length: 8 }, (_, i) => <SkeletonPoster key={i} className="aspect-[9/16]" />)
        : state.error
          ? <p className="text-[12px] text-ink-muted">推荐暂不可用，可切换题材重试。</p>
          : items.map((d) => (
            <Poster
              key={d.id} drama={d}
              subtitle={[categoryOf(d), episodesOf(d) ? `共 ${episodesOf(d)} 集` : ""].filter(Boolean).join(" · ")}
              onFollow={(next) => follow(d.id, next)}
              onDownload={() => download(d.id)}
            />
          ))}
    </Row>
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
          {newItems.map((f) => (
            <Poster
              key={f.id} drama={f} isFollowing={true}
              badge={`新增 ${f.newEpisodes} 集`}
              onFollow={(next) => follow(f.id, next)}
              onDownload={() => download(f.id)}
            />
          ))}
        </Row>
      ) : null}

      <RankingRow />
      <RecommendRow />

      {(data.local || []).length ? (
        <section>
          <div className="mb-3 flex items-center gap-2.5">
            <h3 className="text-sm font-medium tracking-[-.015em] text-ink">本地已下载</h3>
            <span className="grow" />
            <a className="flex items-center gap-1 text-xs text-ink-secondary hover:text-clay" href="#/downloads">下载管理<ChevronRight className="size-3" /></a>
          </div>
          <PosterGrid>
            {data.local.slice(0, 12).map((d) => (
              <Poster
                key={d.id} drama={d}
                onFollow={(next) => follow(d.id, next)}
                onDownload={() => download(d.id)}
              />
            ))}
          </PosterGrid>
        </section>
      ) : null}

      {empty ? <Empty>剧库还是空的：可以从上方榜单和推荐开始，或点击顶栏的刷新按钮检查新内容。</Empty> : null}
    </div>
  )
}