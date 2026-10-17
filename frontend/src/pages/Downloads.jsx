import { useEffect, useMemo, useState } from "react"
import {
  ChevronDown, ChevronRight, FolderOpen, Pause, Play, RotateCcw, X,
} from "lucide-react"
import { apiGet, apiPost } from "../api"
import { Button, Card, Empty, ErrorBox, IconButton, ProgressBar, Segmented, Tag, toast } from "../components/cds"
import { pickDirectory, revealPath } from "../components/directory"
import { coverOf, formatBytes } from "../lib/format"
import { play } from "../actions"
import { navigate } from "../router"

const FILTERS = [["all", "全部"], ["active", "进行中"], ["done", "已完成"], ["stopped", "暂停·失败"]]
const ACTIVE = new Set(["queued", "parsing", "running"])

// 任务状态 → 展示语义。不暴露底层实现（转码/封装/合并引擎一律不说）。
function groupState(tasks, merge) {
  if (merge && ["running", "queued"].includes(merge.status)) return ["merging", "合并中"]
  const counts = tasks.reduce((c, t) => ((c[t.status] = (c[t.status] || 0) + 1), c), {})
  if (counts.running || counts.parsing) return ["running", "下载中"]
  if (counts.queued) return ["running", "排队中"]
  if (counts.failed) return ["failed", "部分失败"]
  if (counts.paused) return ["paused", "已暂停"]
  if (counts.success === tasks.length) return ["success", "已完成"]
  return ["paused", "已停止"]
}

const STATE_TONE = {
  running: "text-clay",
  merging: "text-clay",
  success: "text-ok",
  failed: "text-danger",
  paused: "text-ink-muted",
}

function StatePill({ state, label, pct }) {
  const running = state === "running" || state === "merging"
  return (
    <span className={`inline-flex items-center gap-1.5 text-[11.5px] ${STATE_TONE[state] || "text-ink-secondary"}`}>
      {running ? <i className="size-1.5 animate-pulse-dot rounded-full bg-clay" /> : <i className="size-1.5 rounded-full bg-current opacity-70" />}
      {label}{running ? ` ${pct}%` : ""}
    </span>
  )
}

export default function Downloads({ status }) {
  const [data, setData] = useState(null)
  const [error, setError] = useState(null)
  const [filter, setFilter] = useState("all")
  const [open, setOpen] = useState({})
  const [config, setConfig] = useState(null)
  const [covers, setCovers] = useState({})

  const load = () => apiGet("/api/ui/tasks").then((r) => { setData(r); setError(null) }).catch(setError)

  useEffect(() => {
    load()
    apiGet("/api/ui/config").then(setConfig).catch(() => {})
    // 任务记录不带封面：从本机剧库/历史/追剧里按剧 ID 取封面作缩略图。
    apiGet("/api/desktop/home?full=1").then((h) => {
      const map = {}
      for (const d of [...(h.local || []), ...(h.continue || []), ...(h.following || [])]) {
        if (d?.id && coverOf(d)) map[d.id] = coverOf(d)
      }
      setCovers(map)
    }).catch(() => {})
    const timer = setInterval(load, 1500)
    return () => clearInterval(timer)
  }, [])

  const groups = useMemo(() => {
    const map = new Map()
    for (const t of data?.data || []) {
      if (!map.has(t.dramaId)) map.set(t.dramaId, { id: t.dramaId, title: t.dramaTitle, tasks: [] })
      map.get(t.dramaId).tasks.push(t)
    }
    return [...map.values()].map((g) => {
      g.tasks.sort((a, b) => (a.index || 0) - (b.index || 0))
      g.merge = data?.merges?.[g.id]
      ;[g.state, g.label] = groupState(g.tasks, g.merge)
      g.done = g.tasks.filter((t) => t.status === "success").length
      g.failed = g.tasks.filter((t) => t.status === "failed")
      g.bytes = g.tasks.reduce((s, t) => s + (t.downloadedBytes || 0), 0)
      g.current = g.tasks.find((t) => t.status === "running")
      return g
    })
  }, [data])

  const visible = groups.filter((g) =>
    filter === "all"
    || (filter === "active" && (g.state === "running" || g.state === "merging"))
    || (filter === "done" && g.state === "success")
    || (filter === "stopped" && (g.state === "paused" || g.state === "failed")))

  const act = async (path, body, okText) => {
    try {
      await apiPost(path, body)
      if (okText) toast(okText, "ok")
      load()
    } catch (e) {
      toast(e.message, "error")
    }
  }

  const saveConfig = async (patch) => {
    try {
      const next = await apiPost("/api/ui/config", {
        concurrency: config.concurrency,
        requestConcurrency: config.requestConcurrency,
        requestIntervalMs: config.requestIntervalMs,
        ...patch,
      })
      setConfig(next)
      toast(next.restartRequired ? "已保存，新下载目录将在重启后生效" : "设置已保存", "ok")
    } catch (e) {
      toast(e.message, "error")
    }
  }

  const anyActive = groups.some((g) => g.state === "running")
  const activeCount = groups.filter((g) => g.state === "running" || g.state === "merging").length
  const doneCount = groups.filter((g) => g.state === "success").length
  const pausedCount = groups.filter((g) => g.state === "paused").length
  const failedCount = groups.filter((g) => g.state === "failed").length
  const totalBytes = groups.reduce((sum, g) => sum + g.bytes, 0)

  return (
    <div className="flex flex-col gap-4">
      {/* ---------- 状态计数 ---------- */}
      <div className="flex gap-3">
        <Card className="flex-1 px-3.5 py-3">
          <b className="block text-[19px] font-medium tracking-[-.02em] text-clay tnum">{activeCount}</b>
          <span className="mt-0.5 block text-[11px] text-ink-muted">进行中</span>
        </Card>
        <Card className="flex-1 px-3.5 py-3">
          <b className="block text-[19px] font-medium tracking-[-.02em] text-ink tnum">{doneCount}</b>
          <span className="mt-0.5 block text-[11px] text-ink-muted">已完成</span>
        </Card>
        <Card className="flex-1 px-3.5 py-3">
          <b className="block text-[19px] font-medium tracking-[-.02em] text-ink tnum">{pausedCount}</b>
          <span className="mt-0.5 block text-[11px] text-ink-muted">已暂停</span>
        </Card>
        <Card className="flex-1 px-3.5 py-3">
          <b className={`block text-[19px] font-medium tracking-[-.02em] tnum ${failedCount ? "text-danger" : "text-ink"}`}>{failedCount}</b>
          <span className="mt-0.5 block text-[11px] text-ink-muted">失败</span>
        </Card>
        <Card className="flex-[1.4] px-3.5 py-3">
          <b className="block text-[19px] font-medium tracking-[-.02em] text-ink tnum">{formatBytes(totalBytes)}</b>
          <span className="mt-0.5 block text-[11px] text-ink-muted">已下载总大小</span>
        </Card>
      </div>

      {/* ---------- 筛选与批量操作 ---------- */}
      <div className="flex items-center gap-2.5 rounded-lg bg-surface-2 p-3 shadow-panel">
        <Segmented
          value={filter}
          options={FILTERS.map(([k, label]) => [k, k === "all" ? `${label} ${groups.length}` : label])}
          onChange={setFilter}
        />
        <span className="grow" />
        {anyActive ? (
          <Button size="xs" variant="outline" icon={Pause} onClick={() => act("/api/ui/tasks/pause", { dramaIds: groups.map((g) => g.id) }, "已全部暂停")}>
            全部暂停
          </Button>
        ) : null}
        <Button
          size="xs" variant="outline"
          title="只清理已完成/失败/取消的记录，保留视频文件"
          onClick={() => act("/api/ui/tasks/clear", { ids: (data?.data || []).filter((t) => !ACTIVE.has(t.status)).map((t) => t.id) }, "已清理任务记录（视频保留）")}
        >
          清理任务记录
        </Button>
      </div>

      <ErrorBox error={error} onRetry={load} />

      {/* ---------- 任务列表 ---------- */}
      <div className="flex flex-col gap-2.5">
        {visible.map((g) => {
          const pct = g.merge && g.state === "merging"
            ? g.merge.progress
            : g.tasks.length ? Math.round((g.done / g.tasks.length) * 100) : 0
          const detail = g.state === "merging"
            ? g.merge.detail || "正在合并"
            : g.current
              ? `正在下载第 ${g.current.episode} 集 · ${formatBytes(g.current.speedBytesPerSecond)}/s`
              : g.failed.length
                ? `${g.failed.length} 集失败：${g.failed[0].error || "原因未知"}`
                : g.merge?.status === "success" ? "已合并为完整视频" : ""

          return (
            <Card key={g.id} className="p-3.5">
              <div className="flex items-center gap-3">
                <IconButton size={26} title="展开分集" onClick={() => setOpen({ ...open, [g.id]: !open[g.id] })}>
                  {open[g.id] ? <ChevronDown className="size-4" strokeWidth={1.7} /> : <ChevronRight className="size-4" strokeWidth={1.7} />}
                </IconButton>

                <span className="relative aspect-[9/16] w-[38px] flex-none overflow-hidden rounded-xs bg-[#171614]">
                  {covers[g.id] ? <img src={covers[g.id]} alt="" loading="lazy" className="size-full object-cover" /> : null}
                </span>

                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5">
                    <button
                      type="button"
                      onClick={() => navigate(`/drama/${encodeURIComponent(g.id)}`)}
                      className="max-w-full truncate text-left text-[12.5px] font-medium text-ink hover:text-clay"
                    >
                      {g.title}
                    </button>
                    {g.tasks[0]?.downloadQuality ? <Tag>{(String(g.tasks[0].downloadQuality)).toUpperCase()}</Tag> : null}
                  </div>
                  <div className="mt-0.5 truncate text-[11px] text-ink-muted">
                    第 {g.tasks[0]?.episode}–{g.tasks[g.tasks.length - 1]?.episode} 集 · {formatBytes(g.bytes)}
                    {detail ? ` · ${detail}` : ""}
                  </div>
                  <ProgressBar
                    value={pct}
                    className="mt-2.5 h-[5px]"
                    trackClass={g.state === "success" ? "bg-[var(--cds-alpha-2)]" : undefined}
                  />
                </div>

                <div className="w-[92px] flex-none text-right">
                  <StatePill state={g.state} label={g.label} pct={pct} />
                  <div className="mt-0.5 text-[11px] text-ink-muted tnum">{g.done} / {g.tasks.length} 集</div>
                </div>

                <div className="flex flex-none items-center gap-1">
                  {g.state === "running" ? (
                    <IconButton size={28} title="暂停" onClick={() => act("/api/ui/tasks/pause", { dramaIds: [g.id] })}>
                      <Pause className="size-[15px]" strokeWidth={1.7} />
                    </IconButton>
                  ) : null}
                  {g.state === "paused" ? (
                    <IconButton size={28} title="继续（会重下未完成分集）" onClick={() => act("/api/ui/tasks/resume", { dramaIds: [g.id] })}>
                      <Play className="size-[15px]" strokeWidth={1.7} />
                    </IconButton>
                  ) : null}
                  {g.failed.length ? (
                    <Button size="xs" variant="outline" icon={RotateCcw} onClick={() => act("/api/ui/tasks/retry", { ids: g.failed.map((t) => t.id) }, "已重试失败分集")}>
                      重试
                    </Button>
                  ) : null}
                  {g.done > 0 ? (
                    <Button size="xs" variant="outline" icon={Play} onClick={() => play({ taskId: g.merge?.playbackTaskId || g.tasks.find((t) => t.playable)?.id, title: g.title })}>
                      播放
                    </Button>
                  ) : null}
                  {g.state === "success" && g.merge?.status !== "success" ? (
                    <Button size="xs" variant="secondary" onClick={() => act("/api/ui/merge", { dramaIds: [g.id], deleteEpisodes: false, async: true }, "已开始合并")}>
                      合并全集
                    </Button>
                  ) : null}
                  {g.state === "merging" ? (
                    <Button size="xs" variant="outline" onClick={() => act("/api/ui/merge/cancel", { ids: [g.id] })}>取消合并</Button>
                  ) : null}
                  {g.state === "success" ? (
                    <Button size="xs" variant="outline" onClick={() => act("/api/ui/update", { ids: [g.id] }, "已检查并补充新分集")}>更新本剧</Button>
                  ) : null}
                  {g.tasks.find((t) => t.path) ? (
                    <IconButton size={28} title="在文件夹中显示" onClick={() => revealPath(g.merge?.outputPath || g.tasks.find((t) => t.path).path)}>
                      <FolderOpen className="size-[15px]" strokeWidth={1.7} />
                    </IconButton>
                  ) : null}
                  {g.state !== "success" ? (
                    <IconButton
                      size={28} title="取消下载"
                      onClick={() => window.confirm(`取消《${g.title}》的下载？已完成的分集会保留。`) && act("/api/ui/tasks/cancel", { dramaIds: [g.id] })}
                    >
                      <X className="size-[15px]" strokeWidth={1.7} />
                    </IconButton>
                  ) : null}
                </div>
              </div>

              {open[g.id] ? (
                <div className="mt-3 flex flex-col gap-px border-t border-hairline pt-3">
                  {g.tasks.map((t) => (
                    <div key={t.id} className="flex items-center gap-3 py-1.5 text-[12px]">
                      <span className="w-16 flex-none text-ink-secondary tnum">第 {t.episode} 集</span>
                      <span className={`w-14 flex-none text-[11px] ${t.status === "running" ? "text-clay" : t.status === "failed" ? "text-danger" : t.status === "success" ? "text-ink-muted" : "text-ink-muted"}`}>
                        {t.status === "success" ? "完成" : t.status === "failed" ? "失败" : t.status === "paused" ? "暂停" : t.status === "running" ? `${t.progress}%` : "排队"}
                      </span>
                      <span className="min-w-0 flex-1 truncate text-[11px] text-ink-muted">{t.error || formatBytes(t.downloadedBytes)}</span>
                      {t.status === "failed" ? (
                        <button type="button" className="text-[11px] text-clay hover:underline" onClick={() => act("/api/ui/tasks/retry", { ids: [t.id] })}>
                          重试
                        </button>
                      ) : null}
                    </div>
                  ))}
                </div>
              ) : null}
            </Card>
          )
        })}

        {data && visible.length === 0 ? <Empty>没有下载任务。在详情页点击「下载」开始。</Empty> : null}
      </div>

      {/* ---------- 存储设置 ---------- */}
      <Card className="p-4">
        <h3 className="mb-2 text-[13px] font-medium text-ink">下载位置</h3>
        <p className="text-[11px] text-ink-muted">当前目录</p>
        <p className="mt-1 break-all font-mono text-[12px] text-ink-secondary">{config?.outputDirSetting || status?.outputDir || "—"}</p>
        {config?.restartRequired ? (
          <p className="mt-1.5 text-[11.5px] text-warn">新目录将在重启后生效（当前：{config.outputDir}）</p>
        ) : null}
        <div className="mt-3 flex items-center gap-2">
          <Button
            size="sm" variant="outline"
            onClick={async () => {
              try {
                const p = await pickDirectory(config?.outputDirSetting)
                if (p) saveConfig({ outputDir: p })
              } catch (e) {
                toast(e.message, "error")
              }
            }}
          >
            更改目录
          </Button>
          <Button size="sm" variant="ghost" icon={FolderOpen} onClick={() => revealPath(config?.outputDir)}>在文件夹中显示</Button>
        </div>
        <p className="mt-3 text-[11px] text-ink-muted">暂停后继续会重下未完成分集；清理任务记录会保留已下载的视频文件。</p>
      </Card>
    </div>
  )
}