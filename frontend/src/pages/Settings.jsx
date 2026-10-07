import { useEffect, useState } from "react"
import { apiGet, apiPost, native } from "../api"
import { ErrorBox, toast } from "../components/common"
import { pickDirectory, revealPath } from "../components/directory"
import { navigate } from "../router"

const SECTIONS = [["general", "通用"], ["playback", "播放"], ["storage", "下载与存储"], ["network", "网络与代理"], ["ffmpeg", "组件 · FFmpeg"], ["data", "数据与备份"], ["about", "关于与更新"]]

function Toggle({ value, onChange }) {
  return <button className={`toggle ${value ? "on" : ""}`} onClick={() => onChange(!value)}><i /></button>
}

function Item({ title, desc, children }) {
  return (
    <div className="set-item">
      <div><b>{title}</b>{desc ? <p className="muted small">{desc}</p> : null}</div>
      <div className="set-ctrl">{children}</div>
    </div>
  )
}

function useLocal(key, initial) {
  const [value, setValue] = useState(() => { const v = localStorage.getItem(key); return v == null ? initial : v === "1" })
  return [value, (v) => { localStorage.setItem(key, v ? "1" : "0"); setValue(v) }]
}

export default function Settings({ section = "general", status, onChanged }) {
  const [config, setConfig] = useState(null)
  const [error, setError] = useState(null)
  const [ffmpeg, setFFmpeg] = useState(null)
  const [network, setNetwork] = useState(null)
  const [proxy, setProxy] = useState("")
  const [autoNext, setAutoNext] = useLocal("juku.autonext", true)
  const [danmaku, setDanmaku] = useLocal("juku.danmaku", true)
  const [closeToTray, setCloseToTray] = useState(true)
  const [version, setVersion] = useState("")
  useEffect(() => {
    apiGet("/api/ui/config").then((c) => { setConfig(c); setProxy(c.proxyURL || "") }).catch(setError)
    apiGet("/api/ui/ffmpeg").then(setFFmpeg).catch(() => {})
    native?.getSettings?.().then((s) => setCloseToTray(s?.closeToTray !== false)).catch(() => {})
    native?.getVersion?.().then(setVersion).catch(() => {})
  }, [])
  const save = async (patch) => {
    try {
      const next = await apiPost("/api/ui/config", { concurrency: config.concurrency, requestConcurrency: config.requestConcurrency, requestIntervalMs: config.requestIntervalMs, ...patch })
      setConfig(next)
      onChanged?.()
      toast(next.restartRequired ? "已保存，新下载目录将在重启后生效" : "设置已保存", "ok")
    } catch (e) { toast(e.message, "error") }
  }
  const body = () => {
    if (!config) return <ErrorBox error={error} />
    switch (section) {
      case "playback":
        return (
          <section className="panel">
            <Item title="自动播放下一集"><Toggle value={autoNext} onChange={setAutoNext} /></Item>
            <Item title="默认开启弹幕"><Toggle value={danmaku} onChange={setDanmaku} /></Item>
            <Item title="预缓存下一集" desc="由后端“播放与资源限制”控制，单机默认开启">自动</Item>
          </section>
        )
      case "storage":
        return (
          <section className="panel">
            <Item title="下载目录" desc={config.restartRequired ? `新目录重启后生效；当前 ${config.outputDir}` : config.outputDir}>
              <button className="btn ghost sm" onClick={async () => { try { const p = await pickDirectory(config.outputDirSetting); if (p) save({ outputDir: p }) } catch (e) { toast(e.message, "error") } }}>更改</button>
              <button className="btn ghost sm" onClick={() => revealPath(config.outputDir)}>显示</button>
            </Item>
            <Item title="同时下载数" desc="1–32"><input className="input sm" type="number" min={1} max={32} value={config.concurrency} onChange={(e) => setConfig({ ...config, concurrency: Number(e.target.value) })} onBlur={() => save({})} /></Item>
            <Item title="按站源分类保存"><Toggle value={config.groupBySource} onChange={(v) => save({ groupBySource: v })} /></Item>
          </section>
        )
      case "network":
        return (
          <section className="panel">
            <Item title="当前网络" desc={config.network}>{config.proxyMode === "auto" ? "自动" : config.proxyMode}</Item>
            <Item title="自定义代理" desc="留空为自动；支持 http:// https:// socks5://">
              <input className="input" value={proxy} placeholder="例如 http://127.0.0.1:7890" onChange={(e) => setProxy(e.target.value)} />
              <button className="btn ghost sm" onClick={() => save({ proxyURL: proxy })}>保存</button>
            </Item>
            <Item title="剧库请求并发 / 同站间隔(ms)">
              <input className="input sm" type="number" value={config.requestConcurrency} onChange={(e) => setConfig({ ...config, requestConcurrency: Number(e.target.value) })} onBlur={() => save({})} />
              <input className="input sm" type="number" value={config.requestIntervalMs} onChange={(e) => setConfig({ ...config, requestIntervalMs: Number(e.target.value) })} onBlur={() => save({})} />
            </Item>
            <Item title="检测当前连接">
              <button className="btn ghost sm" onClick={() => { setNetwork("检测中…"); apiPost("/api/ui/network/check", {}).then((r) => setNetwork((r.data || []).map((x) => `${x.name}：${x.error || x.detail || x.status}`).join("；"))).catch((e) => setNetwork(e.message)) }}>检测</button>
            </Item>
            {network ? <p className="muted small">{network}</p> : null}
          </section>
        )
      case "ffmpeg":
        return (
          <section className="panel">
            <Item title="状态" desc={ffmpeg?.detail}>{ffmpeg?.status === "ready" ? "已就绪" : ffmpeg?.status || "未知"}</Item>
            <Item title="路径">{ffmpeg?.path || "—"}</Item>
            {ffmpeg?.totalBytes ? <Item title="下载进度">{Math.round((ffmpeg.downloadedBytes / ffmpeg.totalBytes) * 100)}%</Item> : null}
            <Item title="重新检查"><button className="btn ghost sm" onClick={() => apiPost("/api/ui/ffmpeg", {}).then(setFFmpeg).catch((e) => toast(e.message, "error"))}>重新检查</button></Item>
          </section>
        )
      case "data":
        return (
          <section className="panel">
            <Item title="数据目录" desc={config.dataDir}><button className="btn ghost sm" onClick={() => revealPath(config.dataDir)}>打开</button></Item>
            <Item title="诊断日志" desc="logs/app.log"><button className="btn ghost sm" onClick={() => revealPath(`${config.dataDir}/logs/app.log`)}>显示</button></Item>
            <Item title="数据迁移包" desc="导入/导出网页版或 VPS 版的历史与追剧（阶段 4 接入界面）">待接入</Item>
          </section>
        )
      case "about":
        return (
          <section className="panel">
            <Item title="版本">{version || "开发版"}</Item>
            <Item title="检查更新" desc="Electron 自动更新（electron-updater，需配置发布地址）"><button className="btn ghost sm" disabled={!native?.checkForUpdates} onClick={() => native.checkForUpdates().then((r) => toast(r?.message || "已检查", "ok")).catch((e) => toast(e.message, "error"))}>检查更新</button></Item>
            <Item title="重新运行首次引导"><button className="btn ghost sm" onClick={() => { localStorage.removeItem("juku.onboarded"); location.reload() }}>运行</button></Item>
          </section>
        )
      default:
        return (
          <section className="panel">
            <Item title="关闭窗口时" desc="开启后关闭窗口保留在后台（菜单栏/托盘），下载与合并不中断">
              <Toggle value={closeToTray} onChange={(v) => { setCloseToTray(v); native?.setSettings?.({ closeToTray: v }) }} />
            </Item>
            <Item title="客户端模式" desc={status?.clientMode ? "本机所有者（无需登录）" : "非客户端模式（网页/服务器版）"}>{status?.clientMode ? "✓" : "—"}</Item>
          </section>
        )
    }
  }
  return (
    <div className="settings">
      <nav className="set-nav">{SECTIONS.map(([key, label]) => <button key={key} className={key === section ? "on" : ""} onClick={() => navigate(`/settings/${key}`)}>{label}</button>)}</nav>
      <div className="set-body"><h1 className="page-title">{SECTIONS.find(([k]) => k === section)?.[1] || "通用"}</h1>{body()}</div>
    </div>
  )
}
