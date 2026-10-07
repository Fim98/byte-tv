import { useEffect, useMemo, useState } from "react"
import { ChevronDown, ChevronRight, FolderOpen, Pause, Play, RotateCcw, X } from "lucide-react"
import { apiGet, apiPost } from "../api"
import { Empty, ErrorBox, PageHead, Segmented, toast } from "../components/common"
import { pickDirectory, revealPath } from "../components/directory"
import { coverOf, formatBytes } from "../lib/format"
import { play } from "../actions"
import { navigate } from "../router"

const FILTERS = [["all", "全部"], ["active", "进行中"], ["done", "已完成"], ["stopped", "暂停·失败"]]
const ACTIVE = new Set(["queued", "parsing", "running"])

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
      for (const d of [...(h.local || []), ...(h.continue || []), ...(h.following || [])]) if (d?.id && coverOf(d)) map[d.id] = coverOf(d)
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
  const visible = groups.filter((g) => filter === "all" || (filter === "active" && (g.state === "running" || g.state === "merging")) || (filter === "done" && g.state === "success") || (filter === "stopped" && (g.state === "paused" || g.state === "failed")))
  const act = async (path, body, okText) => {
    try { await apiPost(path, body); if (okText) toast(okText, "ok"); load() } catch (e) { toast(e.message, "error") }
  }
  const saveConfig = async (patch) => {
    try {
      const next = await apiPost("/api/ui/config", { concurrency: config.concurrency, requestConcurrency: config.requestConcurrency, requestIntervalMs: config.requestIntervalMs, ...patch })
      setConfig(next)
      toast(next.restartRequired ? "已保存，新下载目录将在重启后生效" : "设置已保存", "ok")
    } catch (e) { toast(e.message, "error") }
  }
  const anyActive = groups.some((g) => g.state === "running")
  const activeCount = groups.filter((g) => g.state === "running" || g.state === "merging").length
  const doneCount = groups.filter((g) => g.state === "success").length
  const totalBytes = groups.reduce((sum, g) => sum + g.bytes, 0)
  return (
    <>
      <PageHead title="下载与任务" sub="下载、合并共用一套任务队列，关掉页面也会继续">
        {anyActive ? <button className="btn ghost sm" onClick={() => act("/api/ui/tasks/pause", { dramaIds: groups.map((g) => g.id) }, "已全部暂停")}><Pause size={14} />全部暂停</button> : null}
        <button className="btn ghost sm" title="只清理已完成/失败/取消的记录，保留视频文件" onClick={() => act("/api/ui/tasks/clear", { ids: (data?.data || []).filter((t) => !ACTIVE.has(t.status)).map((t) => t.id) }, "已清理任务记录（视频保留）")}>清理任务记录</button>
      </PageHead>
      <div className="ov-row">
        <div className="ov-card"><span className="ov-label">进行中</span><span className="ov-value brand">{activeCount}<small>部</small></span></div>
        <div className="ov-card"><span className="ov-label">已完成</span><span className="ov-value">{doneCount}<small>部</small></span></div>
        <div className="ov-card"><span className="ov-label">已下载</span><span className="ov-value">{formatBytes(totalBytes)}</span></div>
      </div>
      <div className="dl-layout">
      <div className="dl-main">
        <div className="sec-head toolbar">
          <h2 className="sec-title">任务列表</h2>
          <span className="grow" />
          <Segmented value={filter} options={FILTERS} onChange={setFilter} />
        </div>
        <ErrorBox error={error} onRetry={load} />
        {visible.map((g) => {
          const pct = g.merge && g.state === "merging" ? g.merge.progress : g.tasks.length ? Math.round((g.done / g.tasks.length) * 100) : 0
          const detail = g.state === "merging" ? g.merge.detail || "正在合并" : g.current ? `正在下载第 ${g.current.episode} 集 · ${formatBytes(g.current.speedBytesPerSecond)}/s` : g.failed.length ? `${g.failed.length} 集失败：${g.failed[0].error || "原因未知"}` : g.merge?.status === "success" ? "已合并为完整视频" : ""
          return (
            <div key={g.id} className="dl-row">
              <div className="dl-head">
                <button className="icon-btn" title="展开分集" onClick={() => setOpen({ ...open, [g.id]: !open[g.id] })}>{open[g.id] ? <ChevronDown size={16} /> : <ChevronRight size={16} />}</button>
                <span className="dl-thumb">{covers[g.id] ? <img src={covers[g.id]} alt="" loading="lazy" /> : null}</span>
                <div className="dl-info">
                  <div><button className="link title" onClick={() => navigate(`/drama/${encodeURIComponent(g.id)}`)}>{g.title}</button>{g.tasks[0]?.downloadQuality ? <span className="muted"> · {g.tasks[0].downloadQuality}P</span> : null}</div>
                  <span className="muted">第 {g.tasks[0]?.episode}–{g.tasks[g.tasks.length - 1]?.episode} 集 · {formatBytes(g.bytes)}{detail ? ` · ${detail}` : ""}</span>
                  <div className="bar"><i className={g.state} style={{ width: `${pct}%` }} /></div>
                </div>
                <div className="dl-meta"><span className={`pill-state ${g.state}`}>{g.label}{g.state === "running" || g.state === "merging" ? ` ${pct}%` : ""}</span><br /><span className="muted">{g.done} / {g.tasks.length} 集</span></div>
                <div className="dl-actions">
                  {g.state === "running" ? <button className="icon-btn" title="暂停" onClick={() => act("/api/ui/tasks/pause", { dramaIds: [g.id] })}><Pause size={15} /></button> : null}
                  {g.state === "paused" ? <button className="icon-btn" title="继续（会重下未完成分集）" onClick={() => act("/api/ui/tasks/resume", { dramaIds: [g.id] })}><Play size={15} /></button> : null}
                  {g.failed.length ? <button className="btn ghost sm" onClick={() => act("/api/ui/tasks/retry", { ids: g.failed.map((t) => t.id) }, "已重试失败分集")}><RotateCcw size={13} />重试失败</button> : null}
                  {g.done > 0 ? <button className="btn ghost sm" onClick={() => play({ taskId: g.merge?.playbackTaskId || g.tasks.find((t) => t.playable)?.id, title: g.title })}><Play size={13} />播放</button> : null}
                  {g.state === "success" && g.merge?.status !== "success" ? <button className="btn ghost sm" disabled={data?.ffmpeg?.status !== "ready"} onClick={() => act("/api/ui/merge", { dramaIds: [g.id], deleteEpisodes: false, async: true }, "已开始合并")}>合并</button> : null}
                  {g.state === "merging" ? <button className="btn ghost sm" onClick={() => act("/api/ui/merge/cancel", { ids: [g.id] })}>取消合并</button> : null}
                  {g.state === "success" ? <button className="btn ghost sm" onClick={() => act("/api/ui/update", { ids: [g.id] }, "已检查并补充新分集")}>更新本剧</button> : null}
                  {g.tasks.find((t) => t.path) ? <button className="icon-btn" title="在文件夹中显示" onClick={() => revealPath(g.merge?.outputPath || g.tasks.find((t) => t.path).path)}><FolderOpen size={15} /></button> : null}
                  {g.state !== "success" ? <button className="icon-btn" title="取消下载" onClick={() => window.confirm(`取消《${g.title}》的下载？已完成的分集会保留。`) && act("/api/ui/tasks/cancel", { dramaIds: [g.id] })}><X size={15} /></button> : null}
                </div>
              </div>
              {open[g.id] ? (
                <div className="dl-episodes">
                  {g.tasks.map((t) => (
                    <div key={t.id} className="dl-ep">
                      <span>第 {t.episode} 集</span><span className={`pill-state ${t.status}`}>{t.status === "success" ? "完成" : t.status === "failed" ? "失败" : t.status === "paused" ? "暂停" : t.status === "running" ? `${t.progress}%` : "排队"}</span>
                      <span className="muted grow">{t.error || formatBytes(t.downloadedBytes)}</span>
                      {t.status === "failed" ? <button className="link" onClick={() => act("/api/ui/tasks/retry", { ids: [t.id] })}>重试</button> : null}
                    </div>
                  ))}
                </div>
              ) : null}
            </div>
          )
        })}
        {data && visible.length === 0 ? <Empty>没有下载任务。在详情页点击“下载”开始。</Empty> : null}
      </div>
      <aside className="dl-side">
        <section className="panel">
          <h3>存储</h3>
          <p className="muted small">下载目录</p>
          <p className="path">{config?.outputDirSetting || status?.outputDir || "—"}</p>
          {config?.restartRequired ? <p className="warn small">新目录将在重启后生效（当前：{config.outputDir}）</p> : null}
          <div className="actions">
            <button className="btn ghost sm" onClick={async () => { try { const p = await pickDirectory(config?.outputDirSetting); if (p) saveConfig({ outputDir: p }) } catch (e) { toast(e.message, "error") } }}>更改目录</button>
            <button className="btn ghost sm" onClick={() => revealPath(config?.outputDir)}>在文件夹中显示</button>
          </div>
        </section>
        <section className="panel">
          <h3>下载设置</h3>
          <label className="field">同时下载数<input type="number" min={1} max={32} value={config?.concurrency || 2} onChange={(e) => setConfig({ ...config, concurrency: Number(e.target.value) })} onBlur={() => saveConfig({})} /></label>
          <p className="muted small">FFmpeg：{data?.ffmpeg?.status === "ready" ? "已就绪" : data?.ffmpeg?.detail || "准备中"}</p>
          <p className="muted small">暂停后继续会重下未完成分集；清理记录保留视频。</p>
        </section>
      </aside>
    </div>
    </>
  )
}
