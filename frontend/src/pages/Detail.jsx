import { useEffect, useMemo, useState } from "react"
import { Check, Download, Heart, MoreHorizontal, Play, RefreshCw } from "lucide-react"
import { apiGet, apiPost } from "../api"
import { Button, Card, ErrorBox, IconButton, Segmented, SkeletonPoster, Tag, toast, useAsync } from "../components/cds"
import { Poster, Row } from "../components/poster"
import { categoryOf, coverOf, formatTime, releaseText, titleOf } from "../lib/format"
import { download, follow, markCompleted, play } from "../actions"
import { cn } from "../lib/utils"

const PAGE = 30

/* ---------------------------------------------------------------------------
   选集单元：已看集用字色 + 一道短线区分，不额外加边框（单笔画纪律）
   --------------------------------------------------------------------------- */
function EpisodeTile({ ep, onPlay }) {
  const label = ep.episode || ep.index
  return (
    <button
      type="button"
      onClick={() => onPlay(ep)}
      title={ep.title}
      className={`relative grid h-10 place-items-center rounded-sm text-[12.5px] transition-colors duration-[160ms] ease-out ${
        ep.current
          ? "bg-clay font-medium text-white shadow-[0_2px_10px_-3px_rgba(217,119,87,.7)]"
          : ep.watched
            ? "bg-[var(--cds-fill-secondary)] text-ink-muted hover:bg-[var(--cds-fill-secondary-hover)] hover:text-ink"
            : "bg-[var(--cds-fill-secondary)] text-ink-secondary hover:bg-[var(--cds-fill-secondary-hover)] hover:text-ink"
      }`}
    >
      <span className="tnum">{label}</span>
      {ep.watched && !ep.current ? (
        <i className="absolute bottom-[7px] h-0.5 w-4 rounded-pill bg-[var(--cds-alpha-3)]" />
      ) : null}
      {ep.local ? <i className="absolute right-1.5 top-1.5 size-1 rounded-full bg-clay" title="本地已下载" /> : null}
      {ep.vip ? <i className="absolute right-1 top-1 text-[9px] font-medium text-warn">VIP</i> : null}
      {ep.current && ep.duration ? (
        <i className="absolute inset-x-0 bottom-0 h-0.5 rounded-none bg-white/70" style={{ width: `${Math.min(100, (ep.position / ep.duration) * 100)}%` }} />
      ) : null}
    </button>
  )
}

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

  if (state.loading && !data) {
    return (
      <div className="flex flex-col gap-4">
        <SkeletonPoster className="h-[276px] rounded-lg" />
        <div className="grid grid-cols-10 gap-2.5">
          {Array.from({ length: 30 }, (_, i) => <SkeletonPoster key={i} className="h-10 rounded-sm" />)}
        </div>
      </div>
    )
  }
  if (state.error) return <ErrorBox error={state.error} onRetry={state.reload} />
  if (!drama) return null

  const cover = coverOf(drama)
  const title = titleOf(drama)
  const total = drama.totalEpisode || episodes.length
  const finishedAll = progress?.completed && progress.index >= episodes.length
  const mainLabel = !progress
    ? "开始观看 第 1 集"
    : finishedAll
      ? "重新观看"
      : `继续播放 第 ${progress.episode || progress.index} 集 · ${formatTime(progress.position)}`
  const startEpisode = (ep) => play({ dramaId: id, title, episode: ep.index })
  const toggleFollow = async () => { if (await follow(id, !saved)) setSaved(!saved) }
  const meta = [
    categoryOf(drama),
    drama.onlineDate ? `${String(drama.onlineDate).slice(0, 10)} 上线` : "",
    drama.heat ? `热度 ${drama.heat}` : "",
  ].filter(Boolean)
  const newCount = data.following?.knownEpisodes && episodes.length > data.following.knownEpisodes
    ? episodes.length - data.following.knownEpisodes
    : 0

  return (
    <div className="flex flex-col gap-5">
      {/* ---------- 资料头图：左侧竖版 9:16 封面 ---------- */}
      <section className="relative h-[276px] overflow-hidden rounded-lg bg-[#171614]">
        {cover ? <img src={cover} alt="" className="absolute inset-0 size-full scale-110 object-cover blur-xl" /> : null}
        {/* 遮罩：左侧贴近画布底色，右侧渐隐让头图透出 */}
        <div className="absolute inset-0 bg-gradient-to-r from-surface-0 via-surface-0/95 to-transparent" />
        <div className="absolute inset-0 bg-gradient-to-r from-transparent from-40% via-surface-0/70 via-62% to-surface-0/25" />

        <div className="relative flex h-full items-start gap-5 px-6 py-6">
          <div className="relative aspect-[9/16] h-[220px] flex-none overflow-hidden rounded-card bg-[#171614] shadow-[0_12px_30px_-8px_rgba(0,0,0,.55),0_0_0_1px_var(--cds-border-strong)]">
            {cover ? <img src={cover} alt="" className="size-full object-cover" /> : (
              <span className="grid size-full place-items-center text-2xl font-medium text-white/25">{title.slice(0, 1)}</span>
            )}
          </div>

          <div className="min-w-0 flex-1">
            <div className="mb-2.5 flex flex-wrap items-center gap-1.5">
              <Tag tone="clay" className="h-[22px]">红果独播</Tag>
              {episodes.length ? <Tag className="h-[22px]">更新至 {total} 集</Tag> : null}
              {releaseText(drama.releaseStatus) ? <Tag className="h-[22px]">{releaseText(drama.releaseStatus)}</Tag> : null}
              {newCount > 0 ? <Tag tone="clay" className="h-[22px]">新增 {newCount} 集</Tag> : null}
              {drama.vip ? <Tag className="h-[22px]">VIP</Tag> : null}
            </div>

            <h1 className="text-[25px] font-medium leading-[1.22] tracking-[-.028em] text-ink">{title}</h1>

            <div className="mt-2.5 flex flex-wrap items-center gap-2 text-[11.5px] text-ink-secondary">
              {drama.score ? <b className="tnum text-clay">{drama.score} 分</b> : null}
              {meta.map((m, i) => (
                <span key={m} className="flex items-center gap-2">
                  {i > 0 || drama.score ? <s className="block size-0.5 rounded-full bg-ink-muted no-underline" /> : null}
                  {m}
                </span>
              ))}
              {(drama.tags || []).slice(0, 4).map((t) => <Tag key={t}>{t}</Tag>)}
            </div>

            {drama.intro ? (
              <p className="mt-3 line-clamp-2 max-w-[560px] text-[12.5px] leading-relaxed text-ink-secondary">{drama.intro}</p>
            ) : null}

            <div className="mt-4 flex items-center gap-2">
              <Button
                variant="primary" size="lg"
                icon={Play}
                onClick={() => play({ dramaId: id, title, resume: Boolean(progress) && !finishedAll, episode: finishedAll ? 1 : undefined })}
              >
                {mainLabel}
              </Button>
              {progress && !finishedAll ? (
                <Button size="lg" variant="outline" onClick={() => play({ dramaId: id, title, episode: 1 })}>从第 1 集开始</Button>
              ) : null}
              <IconButton
                size={40} round={false}
                title={saved ? "已在追剧清单中（点击取消）" : "加入追剧清单"}
                onClick={toggleFollow}
                className={saved ? "bg-clay text-white hover:bg-clay-hover hover:text-white" : "shadow-[inset_0_0_0_1px_var(--cds-border-strong)]"}
              >
                <Heart className={cn("size-[17px]", saved && "fill-white text-white")} />
              </IconButton>
              <IconButton
                size={40} round={false} title="下载全剧"
                onClick={() => download(id)}
                className="shadow-[inset_0_0_0_1px_var(--cds-border-strong)]"
              >
                <Download className="size-[17px]" />
              </IconButton>
              <div className="relative">
                <IconButton
                  size={40} round={false} title="更多"
                  onClick={() => setMenu((v) => !v)}
                  className="shadow-[inset_0_0_0_1px_var(--cds-border-strong)]"
                >
                  <MoreHorizontal className="size-[17px]" />
                </IconButton>
                {menu ? (
                  <div className="absolute left-0 top-11 z-30 w-[188px] animate-fade-up rounded-lg bg-surface-popover p-1.5 shadow-popover" onMouseLeave={() => setMenu(false)}>
                    <button
                      type="button"
                      className="flex w-full items-center gap-2 rounded-sm px-2.5 py-2 text-left text-[12.5px] text-ink hover:bg-[var(--cds-fill-ghost-hover)]"
                      onClick={() => { markCompleted(id, true); setMenu(false) }}
                    >
                      <Check className="size-3.5" />标为已看
                    </button>
                    <button
                      type="button"
                      className="flex w-full items-center gap-2 rounded-sm px-2.5 py-2 text-left text-[12.5px] text-ink hover:bg-[var(--cds-fill-ghost-hover)]"
                      onClick={() => {
                        apiGet(`/api/desktop/episodes?id=${encodeURIComponent(id)}&refresh=1`).then(state.setData)
                          .then(() => toast("资料与选集已刷新", "ok")).catch((e) => toast(e.message, "error"))
                        setMenu(false)
                      }}
                    >
                      <RefreshCw className="size-3.5" />刷新资料与选集
                    </button>
                  </div>
                ) : null}
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* ---------- 选集面板 ---------- */}
      <section>
        <div className="mb-3 flex flex-wrap items-center gap-2.5">
          <h3 className="text-sm font-medium tracking-[-.015em] text-ink">选集</h3>
          <span className="text-[10.5px] uppercase tracking-[.08em] text-ink-muted">Episodes</span>
          <span className="text-[11.5px] text-ink-muted">共 {episodes.length} 集</span>
          <span className="grow" />
          {pages > 1 ? (
            <Segmented
              value={page}
              options={Array.from({ length: pages }, (_, i) => [
                i, `${i * PAGE + 1}-${Math.min(episodes.length, (i + 1) * PAGE)}`,
              ])}
              onChange={setPage}
            />
          ) : null}
          <label className="flex items-center gap-1.5 text-[11.5px] text-ink-muted">
            跳到第
            <input
              value={jump}
              onChange={(e) => setJump(e.target.value.replace(/\D/g, ""))}
              onKeyDown={(e) => {
                if (e.key !== "Enter" || !jump) return
                const n = Math.min(episodes.length, Math.max(1, Number(jump)))
                setPage(Math.floor((n - 1) / PAGE))
                const ep = episodes[n - 1]
                if (ep) startEpisode(ep)
              }}
              className="h-6 w-[52px] rounded-xs bg-surface-3 text-center text-[11.5px] text-ink shadow-[inset_0_0_0_1px_var(--cds-border)] outline-none focus-visible:shadow-focus"
            />
            集
          </label>
        </div>

        {data.onlineError ? (
          <p className="mb-2.5 text-[12px] text-warn">{data.onlineError}{data.localCount ? "，以下为本地已下载分集" : ""}</p>
        ) : null}

        <div className="grid grid-cols-10 gap-2.5">
          {visible.map((ep) => <EpisodeTile key={ep.index} ep={ep} onPlay={startEpisode} />)}
        </div>

        <div className="mt-3 flex items-center gap-3 text-[11.5px] text-ink-muted">
          <button type="button" className="flex items-center gap-1.5 text-clay hover:underline" onClick={() => download(id)}>
            <Download className="size-3" />下载全剧（按设置画质）
          </button>
          <span className="grow" />
          <span>「已看」依据观看记录推算：当前集之前视为已看</span>
        </div>
      </section>

      {/* ---------- 同类推荐 ---------- */}
      <Similar />
    </div>
  )
}

function Similar() {
  const state = useAsync(
    (signal) => apiPost("/api/ui/recommendations", { genre: "short_play", offset: 0, sessionId: "", seen: [] }, { signal }),
    [],
  )
  if (state.error) return null
  return (
    <Row title="同类推荐" en="Similar" extra="来自红果分类推荐">
      {state.loading
        ? Array.from({ length: 6 }, (_, i) => <SkeletonPoster key={i} className="aspect-[9/16]" />)
        : (state.data?.data || []).slice(0, 12).map((d) => (
            <Poster
              key={d.id} drama={d} size="sm"
              onFollow={() => follow(d.id)}
              onDownload={() => download(d.id)}
            />
          ))}
    </Row>
  )
}