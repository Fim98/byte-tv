// CDS UI 原语 —— 对齐 open-claude-ui/code-examples/ 的组件阶梯。
// 所有样式用 Tailwind 工具类表达，令牌取自 tailwind.config.js。
import * as React from "react"
import { useEffect, useRef, useState } from "react"
import { cn } from "@/lib/utils"

/* ---------------------------------------------------------------------------
   Button：4 级尺寸阶梯（XS 24 / SM 28 / MD 30 / LG 40）
   5 种变体：Primary / Secondary / Ghost / Outline / Danger
   动效：SPRING_SNAPPY 等价 —— 按下 scale(.977)，过渡曲线用 ease-snappy
   --------------------------------------------------------------------------- */
const BTN_SIZE = {
  xs: "h-6 px-2 text-[11.5px] gap-1.5 rounded-xs [&_svg]:size-3.5",
  sm: "h-7 px-2.5 text-xs gap-1.5 rounded-sm [&_svg]:size-4",
  md: "h-[30px] px-3 text-[12.5px] gap-1.5 rounded-sm [&_svg]:size-[15px]",
  lg: "h-10 px-[18px] text-[13.5px] gap-2 rounded-lg [&_svg]:size-[17px]",
}

const BTN_VARIANT = {
  primary: "bg-clay text-white hover:bg-clay-hover shadow-[inset_0_1px_0_rgba(255,255,255,.2)]",
  secondary: "bg-[var(--cds-fill-secondary)] text-ink hover:bg-[var(--cds-fill-secondary-hover)]",
  ghost: "text-ink-secondary hover:bg-[var(--cds-fill-ghost-hover)] hover:text-ink",
  outline: "text-ink shadow-[inset_0_0_0_1px_var(--cds-border-strong)] hover:bg-[var(--cds-fill-ghost-hover)]",
  danger: "text-danger hover:bg-[color-mix(in_srgb,var(--cds-fill-danger)_12%,transparent)]",
}

export const Button = React.forwardRef(function Button(
  { className, variant = "ghost", size = "md", icon: Icon, children, ...props }, ref,
) {
  return (
    <button
      ref={ref}
      className={cn(
        "inline-flex items-center justify-center whitespace-nowrap font-medium",
        "transition-[background-color,color,box-shadow,transform] duration-[160ms] ease-out",
        "active:scale-[.977] disabled:pointer-events-none",
        "focus-visible:outline-none focus-visible:shadow-focus",
        BTN_SIZE[size], BTN_VARIANT[variant], className,
      )}
      {...props}
    >
      {Icon ? <Icon /> : null}
      {children}
    </button>
  )
})

/* ---------------------------------------------------------------------------
   SegmentedControl：9999px 胶囊轨道 + 2px 内距，选中态用 layoutId 等价物
   --------------------------------------------------------------------------- */
export function Segmented({ value, options, onChange, className }) {
  return (
    <div className={cn("inline-flex items-center gap-0.5 rounded-pill bg-[var(--cds-segmented-track)] p-0.5", className)}>
      {options.map(([key, label]) => {
        const on = key === value
        return (
          <button
            key={key}
            type="button"
            onClick={() => onChange(key)}
            className={cn(
              "h-6 rounded-pill px-[11px] text-xs transition-all duration-[220ms] ease-snappy",
              "focus-visible:outline-none",
              on
                ? "bg-[var(--cds-segmented-thumb)] font-medium text-ink shadow-[0_1px_2px_rgba(0,0,0,.14),0_0_0_1px_var(--cds-border)]"
                : "text-ink-secondary hover:text-ink",
            )}
          >
            {label}
          </button>
        )
      })}
    </div>
  )
}

/* ---------------------------------------------------------------------------
   Tag：低对比标签；clay 变体用于陶土强调
   --------------------------------------------------------------------------- */
export function Tag({ className, tone = "default", children, ...props }) {
  const tones = {
    default: "bg-[var(--cds-fill-secondary)] text-ink-secondary",
    clay: "bg-clay-light text-clay",
    outline: "shadow-[inset_0_0_0_1px_var(--cds-border)] text-ink-secondary",
  }
  return (
    <span className={cn("inline-flex h-5 items-center gap-1.5 rounded-xs px-2 text-[11px]", tones[tone], className)} {...props}>
      {children}
    </span>
  )
}

/* ---------------------------------------------------------------------------
   StatusDot：陶土呼吸 / 就绪 / 警告 / 失败
   --------------------------------------------------------------------------- */
export function StatusDot({ tone = "ok", breathe = false, className }) {
  const tones = {
    ok: "bg-ok shadow-[0_0_0_2px_rgba(63,185,80,.16)]",
    warn: "bg-warn shadow-[0_0_0_2px_rgba(227,160,8,.16)]",
    err: "bg-danger shadow-[0_0_0_2px_rgba(239,68,68,.16)]",
    live: "bg-clay",
  }
  return <span className={cn("inline-block size-1.5 flex-none rounded-full", tones[tone], breathe && "animate-breathe", className)} />
}

/* ---------------------------------------------------------------------------
   Switch：22px 高胶囊，SPRING_BOUNCY 等价动效
   --------------------------------------------------------------------------- */
export function Switch({ checked, onChange, disabled, className }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onChange?.(!checked)}
      className={cn(
        "relative h-[22px] w-[38px] flex-none rounded-pill transition-colors duration-200 ease-snappy",
        "focus-visible:outline-none focus-visible:shadow-focus",
        checked ? "bg-clay" : "bg-[var(--cds-alpha-3)]",
        className,
      )}
    >
      <span
        className={cn(
          "absolute top-0.5 left-0.5 size-[18px] rounded-full bg-white shadow-[0_1px_3px_rgba(0,0,0,.35)]",
          "transition-transform duration-300 ease-snappy",
          checked && "translate-x-4",
        )}
      />
    </button>
  )
}

/* ---------------------------------------------------------------------------
   Card：面板表面 + 双环阴影（容器，不做交互）
   --------------------------------------------------------------------------- */
export function Card({ className, interactive = false, children, ...props }) {
  return (
    <div
      className={cn(
        "rounded-card bg-surface-2 shadow-panel",
        interactive && "transition-[box-shadow,transform] duration-200 ease-out hover:-translate-y-px hover:shadow-panel-hover",
        className,
      )}
      {...props}
    >
      {children}
    </div>
  )
}

/* ---------------------------------------------------------------------------
   ProgressBar：轨道 + 已缓冲 + 已播（clay）
   --------------------------------------------------------------------------- */
export function ProgressBar({ value = 0, buffered, className, trackClass }) {
  return (
    <div className={cn("relative h-[3px] overflow-hidden rounded-pill bg-[var(--rail)]", className, trackClass)}>
      {buffered != null ? (
        <span className="absolute inset-y-0 left-0 rounded-pill bg-[var(--cds-alpha-3)]" style={{ width: `${buffered}%` }} />
      ) : null}
      <span className="absolute inset-y-0 left-0 rounded-pill bg-clay" style={{ width: `${Math.max(0, Math.min(100, value))}%` }} />
    </div>
  )
}

/* ---------------------------------------------------------------------------
   SectionHead：区块标题（中文标题 + 英文小标 + 右侧动作）
   --------------------------------------------------------------------------- */
export function SectionHead({ title, en, extra, action, className }) {
  return (
    <div className={cn("mb-3 flex items-center gap-2.5", className)}>
      <h3 className="text-sm font-medium tracking-[-.015em] text-ink">{title}</h3>
      {en ? <span className="text-[10.5px] uppercase tracking-[.08em] text-ink-muted">{en}</span> : null}
      {extra ? <span className="text-[11.5px] text-ink-muted">{extra}</span> : null}
      <span className="grow" />
      {action}
    </div>
  )
}

/* ---------------------------------------------------------------------------
   IconButton：圆形/方形图标按钮（顶栏、播放器控件）
   --------------------------------------------------------------------------- */
export function IconButton({ className, size = 28, round = true, children, ...props }) {
  return (
    <button
      type="button"
      className={cn(
        "grid flex-none place-items-center text-ink-secondary transition-[background-color,color] duration-[160ms] ease-out",
        "hover:bg-[var(--cds-fill-ghost-hover)] hover:text-ink active:scale-95",
        "focus-visible:outline-none focus-visible:shadow-focus",
        round ? "rounded-pill" : "rounded-sm",
        className,
      )}
      style={{ width: size, height: size }}
      {...props}
    >
      {children}
    </button>
  )
}

/* ---------------------------------------------------------------------------
   Empty / ErrorBox / Skeleton：状态三件套
   --------------------------------------------------------------------------- */
export function Empty({ children, className }) {
  return <div className={cn("grid place-items-center px-6 py-14 text-[13px] text-ink-muted", className)}>{children}</div>
}

export function ErrorBox({ error, onRetry, className }) {
  if (!error) return null
  return (
    <div className={cn("flex items-center gap-3 rounded-card bg-surface-2 px-4 py-3 text-[12.5px] text-ink-secondary shadow-panel", className)}>
      <span className="min-w-0 flex-1">{String(error.message || error)}</span>
      {onRetry ? <Button size="sm" variant="secondary" onClick={onRetry}>重试</Button> : null}
    </div>
  )
}

export function SkeletonPoster({ className }) {
  return (
    <div className={cn("relative overflow-hidden rounded-card bg-[var(--cds-fill-secondary)]", className)}>
      <span className="absolute inset-0 animate-sweep bg-gradient-to-r from-transparent via-white/25 to-transparent dark:via-white/10" />
    </div>
  )
}

/* ---------------------------------------------------------------------------
   Toast：全局单例 handler（非视觉工具，与 UI 原语同层）
   --------------------------------------------------------------------------- */
let toastHandler = null
export function setToastHandler(fn) { toastHandler = fn }
export function toast(message, kind = "info") { toastHandler?.(message, kind) }

/* ---------------------------------------------------------------------------
   useAsync：带取消与重试的数据加载
   --------------------------------------------------------------------------- */
export function useAsync(fn, deps) {
  const [state, setState] = useState({ loading: true, data: null, error: null })
  const [tick, setTick] = useState(0)
  const alive = useRef(0)
  useEffect(() => {
    const id = ++alive.current
    const controller = new AbortController()
    setState((s) => ({ ...s, loading: true, error: null }))
    Promise.resolve(fn(controller.signal))
      .then((data) => id === alive.current && setState({ loading: false, data, error: null }))
      .catch((error) => id === alive.current && error.name !== "AbortError" && setState({ loading: false, data: null, error }))
    return () => controller.abort()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick])
  return { ...state, reload: () => setTick((t) => t + 1), setData: (data) => setState((s) => ({ ...s, data })) }
}