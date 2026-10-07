import { useEffect, useState } from "react"
import { apiGet, apiPost } from "../api"
import { pickDirectory } from "../components/directory"

// 首次启动引导：欢迎 → 下载位置 → FFmpeg → 网络检测
export default function Onboarding({ onDone }) {
  const [step, setStep] = useState(0)
  const [config, setConfig] = useState(null)
  const [ffmpeg, setFFmpeg] = useState(null)
  const [network, setNetwork] = useState(null)
  const [error, setError] = useState("")
  useEffect(() => { apiGet("/api/ui/config").then(setConfig).catch((e) => setError(e.message)) }, [])
  useEffect(() => {
    if (step !== 2) return
    const load = () => apiGet("/api/ui/ffmpeg").then(setFFmpeg).catch(() => {})
    load()
    const timer = setInterval(load, 1000)
    return () => clearInterval(timer)
  }, [step])
  useEffect(() => {
    if (step !== 3) return
    apiPost("/api/ui/network/check", {}).then((r) => setNetwork(r.data || [])).catch((e) => setNetwork([{ name: "检测", error: e.message }]))
  }, [step])
  const changeDir = async () => {
    try {
      const path = await pickDirectory(config?.outputDirSetting)
      if (!path) return
      setConfig(await apiPost("/api/ui/config", { concurrency: config.concurrency, requestConcurrency: config.requestConcurrency, requestIntervalMs: config.requestIntervalMs, outputDir: path }))
    } catch (e) { setError(e.message) }
  }
  const steps = [
    <><h1>欢迎使用果果剧库</h1><p>在线观看与下载红果短剧。请仅下载你拥有权利或已获授权的内容。</p></>,
    <><h1>选择下载位置</h1><p className="path">{config?.outputDirSetting || "读取中…"}</p><button className="btn ghost" onClick={changeDir}>更改…</button>{config?.restartRequired ? <p className="muted small">新目录将在重启后生效</p> : null}</>,
    <><h1>准备 FFmpeg</h1><p>{ffmpeg?.detail || "检查中…"}</p>{ffmpeg?.totalBytes ? <div className="bar big"><i style={{ width: `${(ffmpeg.downloadedBytes / ffmpeg.totalBytes) * 100}%` }} /></div> : null}<p className="muted small">不需要 FFmpeg 的内容可以先播放，不必等待。</p>{ffmpeg?.status === "failed" ? <button className="btn ghost" onClick={() => apiPost("/api/ui/ffmpeg", {}).then(setFFmpeg)}>重试</button> : null}</>,
    <><h1>检测网络</h1>{network ? network.map((n) => <p key={n.name}>{n.name}：{n.error ? `❌ ${n.error}` : `✅ ${n.detail || n.status}`}</p>) : <p>检测中…</p>}<p className="muted small">如无法连接，可稍后在“设置 → 网络与代理”中配置代理。</p></>,
  ]
  return (
    <div className="onboarding">
      <div className="ob-card">
        <div className="ob-steps">{steps.map((_, i) => <i key={i} className={i <= step ? "on" : ""} />)}</div>
        {steps[step]}
        {error ? <p className="warn">{error}</p> : null}
        <div className="actions">
          {step > 0 ? <button className="btn ghost" onClick={() => setStep(step - 1)}>上一步</button> : null}
          <span className="grow" />
          {step < steps.length - 1 ? <button className="btn primary" onClick={() => setStep(step + 1)}>下一步</button> : <button className="btn primary" onClick={onDone}>开始使用</button>}
        </div>
        <button className="link small" onClick={onDone}>跳过引导</button>
      </div>
    </div>
  )
}
