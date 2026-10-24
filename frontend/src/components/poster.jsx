// 海报卡片与区块组件 —— 七个页面共用。
// 海报图来自源站；失败时回退为剧名首字（程序化，无版权素材）。
import { useEffect, useState } from "react"
import { Check, Download, Heart, Play } from "lucide-react"
import { coverOf, episodesOf, titleOf } from "@/lib/format"
import { navigate } from "@/router"
import { cn } from "@/lib/utils"
import { follow as globalFollow, isFollowing as checkIsFollowing, subscribeFollowing } from "@/actions"
import { ProgressBar, Tag } from "./cds"

/* ---------------------------------------------------------------------------
   Poster：海报卡。
   短剧封面竖版 9:16（源站 640×914 左右），卡片按 9:16 呈现。
   - 交互升级：现代悬浮毛玻璃胶囊（Glass Pill Dock），居中悬停于剧名上方
   - 动作：追剧（实时高亮切换）与下载（下沉触觉动效与完成反馈）
   - 稳定排版：剧名底部常驻不跳动，封面中央优雅浮现轻质播放指示
   --------------------------------------------------------------------------- */
export function Poster({
  drama,
  badge,
  progress,
  subtitle,
  onPlay,
  onFollow,
  onDownload,
  rank,
  size = "md",
  isFollowing,
}) {
  const [failed, setFailed] = useState(false)
  const [, setFollowTick] = useState(0)
  const [downloading, setDownloading] = useState(false)

  // 监听全局追剧状态变更，全站卡片即时响应
  useEffect(() => {
    return subscribeFollowing(() => setFollowTick((t) => t + 1))
  }, [])

  const dramaId = drama?.id ? String(drama.id) : ""
  const followed = Boolean(
    isFollowing ?? (dramaId ? checkIsFollowing(dramaId) : false) ?? drama?.saved ?? drama?.isFollowing
  )

  const cover = coverOf(drama)
  const total = episodesOf(drama)
  const title = titleOf(drama)
  const open = () => navigate(`/drama/${encodeURIComponent(drama.id)}`)

  const handleFollow = async (e) => {
    e.stopPropagation()
    const next = !followed
    await globalFollow(dramaId, next)
    if (onFollow) {
      try {
        await onFollow(next)
      } catch {
        // ignore
      }
    }
  }

  const handleDownload = async (e) => {
    e.stopPropagation()
    if (!onDownload) return
    setDownloading(true)
    try {
      await onDownload()
      setTimeout(() => setDownloading(false), 2000)
    } catch {
      setDownloading(false)
    }
  }

  return (
    <div className="group min-w-0">
      <button
        type="button"
        onClick={() => (onPlay ? onPlay() : open())}
        title={title}
        className="relative block aspect-[9/16] w-full overflow-hidden rounded-card bg-surface-2 ring-1 ring-border/80 transition-all duration-300 ease-out group-hover:ring-border-strong group-hover:shadow-panel-hover"
      >
        {cover && !failed ? (
          <img
            src={cover}
            alt=""
            loading="lazy"
            onError={() => setFailed(true)}
            className="absolute inset-0 size-full object-cover transition-transform duration-500 ease-out group-hover:scale-[1.03]"
          />
        ) : (
          <span className="absolute inset-0 grid place-items-center text-2xl font-medium text-ink-muted/30 bg-surface-2">
            {title.slice(0, 1)}
          </span>
        )}

        {/* 悬停微暗氛围层 */}
        <span className="pointer-events-none absolute inset-0 bg-black/0 transition-colors duration-300 group-hover:bg-black/25" />

        {/* 底部电影级渐变：保证标题文字无论深浅色、封面明暗都清晰高保真 */}
        <span className="pointer-events-none absolute inset-x-0 bottom-0 h-1/2 bg-gradient-to-t from-black/95 via-black/50 to-transparent" />

        {rank ? (
          <span
            className={cn(
              "absolute left-1.5 top-1.5 grid size-5 place-items-center rounded-xs text-[11px] font-semibold",
              rank === 1 ? "bg-clay/95 text-white" : "bg-black/60 text-white backdrop-blur-sm",
            )}
          >
            {rank}
          </span>
        ) : null}

        {total ? (
          <Tag className="absolute right-1.5 top-1.5 h-[18px] bg-black/55 px-1.5 text-[10px] text-white backdrop-blur-sm border-0">
            {total} 集
          </Tag>
        ) : null}

        {/* 已加入追剧时，右上角常驻红心标识 */}
        {followed && (
          <span
            className={cn(
              "absolute z-10 flex size-5 items-center justify-center rounded-full bg-clay text-white shadow-md shadow-clay/30 backdrop-blur-sm animate-fade-up",
              total ? "right-1.5 top-7" : "right-1.5 top-1.5"
            )}
            title="已在追剧清单中"
          >
            <Heart className="size-2.5 fill-white text-white" />
          </span>
        )}

        {badge ? (
          <Tag tone="clay" className={cn("absolute left-1.5 h-[18px] px-1.5 text-[10px]", rank ? "top-8" : "top-1.5")}>
            {badge}
          </Tag>
        ) : null}

        {/* 悬停快捷微操作层（深浅色自适应毛玻璃浮岛） */}
        {(onFollow || onDownload) ? (
          <div
            className={cn(
              "pointer-events-none absolute inset-x-0 bottom-14 z-10 flex justify-center",
              "transition-all duration-200 ease-snappy",
              "translate-y-2 opacity-0 scale-95",
              "group-hover:pointer-events-auto group-hover:translate-y-0 group-hover:opacity-100 group-hover:scale-100",
              "group-focus-within:pointer-events-auto group-focus-within:translate-y-0 group-focus-within:opacity-100 group-focus-within:scale-100",
            )}
          >
            <div
              className={cn(
                "pointer-events-auto inline-flex items-center gap-1.5 rounded-full p-1",
                "bg-surface-1/90 text-ink backdrop-blur-xl border border-border-strong shadow-lg",
                "dark:bg-black/75 dark:text-white dark:border-white/20 dark:shadow-[0_8px_24px_rgba(0,0,0,0.55)]",
              )}
            >
              {onFollow ? (
                <span
                  role="button"
                  tabIndex={-1}
                  title={followed ? "已在追剧清单中（点击取消）" : "加入追剧清单"}
                  aria-label="追剧"
                  onClick={handleFollow}
                  className={cn(
                    "group/btn grid size-7 place-items-center rounded-full cursor-pointer select-none",
                    "transition-all duration-150 ease-out active:scale-90",
                    followed
                      ? "bg-clay text-white shadow-sm shadow-clay/30 hover:bg-clay-hover"
                      : "text-ink-secondary hover:text-ink hover:bg-surface-3 dark:text-white/85 dark:hover:text-white dark:hover:bg-white/20",
                  )}
                >
                  <Heart
                    className={cn(
                      "size-3.5 transition-transform duration-200 group-hover/btn:scale-115",
                      followed && "fill-white text-white",
                    )}
                    strokeWidth={2.2}
                  />
                </span>
              ) : null}

              {onFollow && onDownload ? (
                <div className="h-3 w-[1px] bg-border-strong dark:bg-white/20 self-center" />
              ) : null}

              {onDownload ? (
                <span
                  role="button"
                  tabIndex={-1}
                  title={downloading ? "已加入下载" : "下载本剧"}
                  aria-label="下载"
                  onClick={handleDownload}
                  className={cn(
                    "group/btn grid size-7 place-items-center rounded-full cursor-pointer select-none",
                    "text-ink-secondary hover:text-ink hover:bg-surface-3 dark:text-white/85 dark:hover:text-white dark:hover:bg-white/20",
                    "transition-all duration-150 ease-out active:scale-90",
                    downloading && "text-emerald-500 dark:text-emerald-400 bg-emerald-500/15",
                  )}
                >
                  {downloading ? (
                    <Check className="size-3.5 stroke-[2.5] text-emerald-500 dark:text-emerald-400" />
                  ) : (
                    <Download className="size-3.5 stroke-[2.2] transition-transform duration-200 group-hover/btn:translate-y-0.5" />
                  )}
                </span>
              ) : null}
            </div>
          </div>
        ) : null}

        {/* 标题保持在底部，距离浮岛有充足安全间隔，单行截断保证排版高度稳定 */}
        <span className="pointer-events-none absolute inset-x-2 bottom-2 z-10 truncate text-left text-[12px] font-medium leading-snug text-white [text-shadow:0_1px_6px_rgba(0,0,0,.9)]" title={title}>
          {title}
        </span>

        {progress != null ? (
          <ProgressBar value={progress} className="absolute inset-x-0 bottom-0 h-[3px] rounded-none bg-white/25 z-10" />
        ) : null}
      </button>

      {subtitle ? <div className="mt-1.5 truncate text-[11px] text-ink-muted">{subtitle}</div> : null}
    </div>
  )
}

/* ---------------------------------------------------------------------------
   PosterGrid：响应式海报网格
   列数由宽度决定：竖版 9:16 卡片最小 150px，宽屏自动多一列，不拉伸变形。
   --------------------------------------------------------------------------- */
export function PosterGrid({ children, className }) {
  return (
    <div className={cn("grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-x-3.5 gap-y-5", className)}>
      {children}
    </div>
  )
}

/* ---------------------------------------------------------------------------
   Row：横向走廊，右侧 7 段非线性淡出。
   不提供横向滚动：发现页只展示一屏能放下的这几张，更多内容交给标题栏的
   「完整榜单 / 查看全部」跳剧库，避免出现横向滚动条。
   超出可视区的卡片会被裁掉（而不是滚出来），所以调用方只需传适量条数。
   --------------------------------------------------------------------------- */
export function Row({ title, en, extra, action, children, itemWidth = 150 }) {
  return (
    <section>
      <div className="mb-3 flex items-center gap-2.5">
        <h3 className="text-sm font-medium tracking-[-.015em] text-ink">{title}</h3>
        {en ? <span className="text-[10.5px] uppercase tracking-[.08em] text-ink-muted">{en}</span> : null}
        {extra ? <span className="text-[11.5px] text-ink-muted">{extra}</span> : null}
        <span className="grow" />
        {action}
      </div>
      <div className="mask-corridor flex gap-3.5 overflow-hidden">
        {Array.isArray(children)
          ? children.map((child, i) => (
              <div key={i} className="flex-none" style={{ width: itemWidth }}>{child}</div>
            ))
          : children}
      </div>
    </section>
  )
}

/* ---------------------------------------------------------------------------
   PageHead：页面标题 + 右侧工具区
   --------------------------------------------------------------------------- */
export function PageHead({ title, sub, children }) {
  return (
    <div className="mb-4 flex items-start gap-4">
      <div className="min-w-0">
        <h1 className="text-[22px] font-medium leading-tight tracking-[-.02em] text-ink">{title}</h1>
        {sub ? <p className="mt-1 text-[12.5px] text-ink-secondary">{sub}</p> : null}
      </div>
      {children ? <div className="ml-auto flex flex-wrap items-center gap-2">{children}</div> : null}
    </div>
  )
}