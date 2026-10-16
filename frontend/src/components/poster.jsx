// 海报卡片与区块组件 —— 七个页面共用。
// 海报图来自源站；失败时回退为剧名首字（程序化，无版权素材）。
import { useState } from "react"
import { Download, Plus, Play } from "lucide-react"
import { coverOf, episodesOf, titleOf } from "@/lib/format"
import { navigate } from "@/router"
import { cn } from "@/lib/utils"
import { ProgressBar, Tag } from "./cds"

/* ---------------------------------------------------------------------------
   Poster：海报卡。静态不显示操作按钮，悬停才浮现（不占版面）。
   --------------------------------------------------------------------------- */
export function Poster({ drama, badge, progress, subtitle, onPlay, onFollow, onDownload, rank, size = "md" }) {
  const [failed, setFailed] = useState(false)
  const cover = coverOf(drama)
  const total = episodesOf(drama)
  const title = titleOf(drama)

  const heights = { sm: "h-[132px]", md: "h-[212px]", lg: "h-[240px]" }
  const open = () => navigate(`/drama/${encodeURIComponent(drama.id)}`)

  return (
    <div className="group min-w-0">
      <button
        type="button"
        onClick={() => (onPlay ? onPlay() : open())}
        title={title}
        className={cn("relative block w-full overflow-hidden rounded-card bg-[#171614]", heights[size])}
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
          <span className="absolute inset-0 grid place-items-center text-2xl font-medium text-white/25">{title.slice(0, 1)}</span>
        )}

        {/* 底部渐变，保证标题可读 */}
        <span className="absolute inset-x-0 bottom-0 h-2/5 bg-gradient-to-t from-black/85 via-black/35 to-transparent" />

        {rank ? (
          <span
            className={cn(
              "absolute left-2 top-2 grid size-5 place-items-center rounded-xs text-[11px] font-semibold",
              rank === 1 ? "bg-clay/95 text-white" : "bg-white/20 text-white",
            )}
          >
            {rank}
          </span>
        ) : null}

        {total ? <Tag className="absolute right-2 top-2 bg-black/45 text-white backdrop-blur-sm">{total} 集</Tag> : null}
        {badge ? <Tag tone="clay" className="absolute left-2 bottom-8">{badge}</Tag> : null}

        <span className="absolute inset-x-2 bottom-2 line-clamp-2 text-left text-[12.5px] font-medium leading-snug text-white [text-shadow:0_1px_6px_rgba(0,0,0,.6)]">
          {title}
        </span>

        {/* 悬停操作层：静态不占版面 */}
        <span className="absolute inset-0 flex items-center justify-center gap-2 bg-black/45 opacity-0 backdrop-blur-[1px] transition-opacity duration-200 ease-out group-hover:opacity-100">
          <span className="flex items-center gap-1.5 rounded-xs bg-white/95 px-2.5 py-1 text-[11.5px] font-medium text-[#151515]">
            <Play className="size-3.5" fill="currentColor" strokeWidth={0} />
            {onPlay ? "播放" : "查看"}
          </span>
          {onFollow ? (
            <span
              onClick={(e) => { e.stopPropagation(); onFollow() }}
              className="grid size-6 place-items-center rounded-xs bg-black/60 text-white backdrop-blur-md"
              title="追剧"
            >
              <Plus className="size-3.5" />
            </span>
          ) : null}
          {onDownload ? (
            <span
              onClick={(e) => { e.stopPropagation(); onDownload() }}
              className="grid size-6 place-items-center rounded-xs bg-black/60 text-white backdrop-blur-md"
              title="下载"
            >
              <Download className="size-3.5" />
            </span>
          ) : null}
        </span>

        {progress != null ? (
          <ProgressBar value={progress} className="absolute inset-x-0 bottom-0 h-[3px] rounded-none bg-white/25" />
        ) : null}
      </button>

      {subtitle ? <div className="mt-1.5 truncate text-[11px] text-ink-muted">{subtitle}</div> : null}
    </div>
  )
}

/* ---------------------------------------------------------------------------
   PosterGrid：响应式海报网格
   --------------------------------------------------------------------------- */
export function PosterGrid({ children, className }) {
  return (
    <div className={cn("grid grid-cols-6 gap-x-3.5 gap-y-4", className)}>
      {children}
    </div>
  )
}

/* ---------------------------------------------------------------------------
   Row：横向走廊，右侧 7 段非线性淡出
   --------------------------------------------------------------------------- */
export function Row({ title, en, extra, action, children, itemWidth = 232 }) {
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