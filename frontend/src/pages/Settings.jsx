import { useEffect, useRef, useState } from "react"
import { FolderOpen } from "lucide-react"
import { apiGet, apiPost, native } from "../api"
import { Button, Card, ErrorBox, Segmented, StatusDot, Switch, Tag, toast } from "../components/cds"
import { pickDirectory, revealPath } from "../components/directory"
import { getTheme, setTheme } from "../lib/theme"

// 设置分节：只保留用户能理解的项。底层实现（转码引擎、会话上限、并发额度）不出现在界面。
const SECTIONS = [
  { key: "general", label: "外观与窗口" },
  { key: "playback", label: "播放" },
  { key: "storage", label: "下载与存储" },
  { key: "network", label: "网络" },
  { key: "about", label: "关于" },
]
const THEMES = [["dark", "深色"], ["light", "浅色"], ["system", "跟随系统"]]

/* 设置行：左侧标题与说明，右侧控件 */
function Item({ title, desc, children, last }) {
  return (
    <div className={`flex items-center gap-4 py-3.5 ${last ? "" : "border-b border-hairline"}`}>
      <div className="min-w-0 flex-1">
        <b className="block text-[12.5px] font-medium text-ink">{title}</b>
        {desc ? <p className="mt-1 max-w-[560px] text-[11px] leading-relaxed text-ink-muted">{desc}</p> : null}
      </div>
      <div className="flex flex-none items-center gap-2">{children}</div>
    </div>
  )
}

function useLocalToggle(key, initial) {
  const [value, setValue] = useState(() => {
    const v = localStorage.getItem(key)
    return v == null ? initial : v === "1"
  })
  return [value, (v) => { localStorage.setItem(key, v ? "1" : "0"); setValue(v) }]
}

export default function Settings({ section = "", status, onChanged }) {
  const [config, setConfig] = useState(null)
  const [error, setError] = useState(null)
  const [network, setNetwork] = useState(null)
  const [proxy, setProxy] = useState("")
  const [autoNext, setAutoNext] = useLocalToggle("juku.autonext", true)
  const [danmaku, setDanmaku] = useLocalToggle("juku.danmaku", true)
  const [closeToTray, setCloseToTray] = useState(true)
  const [version, setVersion] = useState("")
  const [theme, setThemeState] = useState(getTheme)
  const gridRef = useRef(null)

  useEffect(() => {
    apiGet("/api/ui/config").then((c) => { setConfig(c); setProxy(c.proxyURL || "") }).catch(setError)
    native?.getSettings?.().then((s) => setCloseToTray(s?.closeToTray !== false)).catch(() => {})
    native?.getVersion?.().then(setVersion).catch(() => {})
  }, [])

  const save = async (patch) => {
    try {
      const next = await apiPost("/api/ui/config", {
        concurrency: config.concurrency,
        requestConcurrency: config.requestConcurrency,
        requestIntervalMs: config.requestIntervalMs,
        ...patch,
      })
      setConfig(next)
      onChanged?.()
      toast(next.restartRequired ? "已保存，新下载目录将在重启后生效" : "设置已保存", "ok")
    } catch (e) {
      toast(e.message, "error")
    }
  }

  useEffect(() => {
    if (!config || !section) return
    gridRef.current?.querySelector(`#set-${section}`)?.scrollIntoView({ block: "start", behavior: "smooth" })
  }, [config, section])

  const body = (key) => {
    switch (key) {
      case "playback":
        return (
          <>
            <Item title="自动播放下一集">
              <Switch checked={autoNext} onChange={setAutoNext} />
            </Item>
            <Item title="默认开启弹幕" desc="按原始时间分段加载；暂停、倍速、跳转与切集保持同步。">
              <Switch checked={danmaku} onChange={setDanmaku} />
            </Item>
            <Item title="预缓存下一集" desc="当前集缓存余量足够时提前准备好下一集，切集无需等待。" last>
              <Tag tone="clay">默认开启</Tag>
            </Item>
          </>
        )

      case "storage":
        return (
          <>
            <Item title="下载目录" desc={config.restartRequired ? `新目录将在重启后生效；当前 ${config.outputDir}` : config.outputDir}>
              <Button
                size="sm" variant="outline"
                onClick={async () => {
                  try {
                    const p = await pickDirectory(config.outputDirSetting)
                    if (p) save({ outputDir: p })
                  } catch (e) {
                    toast(e.message, "error")
                  }
                }}
              >
                更改
              </Button>
              <Button size="sm" variant="ghost" icon={FolderOpen} onClick={() => revealPath(config.outputDir)}>显示</Button>
            </Item>
            <Item title="同时下载数" desc="同时进行的下载任务数量，1–32。">
              <input
                type="number" min={1} max={32}
                value={config.concurrency}
                onChange={(e) => setConfig({ ...config, concurrency: Number(e.target.value) })}
                onBlur={() => save({})}
                className="tnum h-7 w-[84px] rounded-sm bg-surface-3 text-center text-[12.5px] text-ink shadow-[inset_0_0_0_1px_var(--cds-border)] outline-none focus-visible:shadow-focus"
              />
            </Item>
            <Item title="按站源分类保存" desc="开启后新下载保存为「下载目录 / 站源 / 剧名 / 分集」。已有下载不受影响。" last>
              <Switch checked={config.groupBySource} onChange={(v) => save({ groupBySource: v })} />
            </Item>
          </>
        )

      case "network":
        return (
          <>
            <Item title="当前网络" desc="站点请求、媒体与密钥均使用此配置。">
              <span className="text-[12.5px] text-ink-secondary">{config.network}</span>
              {config.proxyMode === "auto" ? <Tag>自动</Tag> : null}
            </Item>
            <Item title="自定义代理" desc="留空为自动检测系统代理；支持 http:// https:// socks5://">
              <input
                value={proxy}
                placeholder="例如 http://127.0.0.1:7890"
                onChange={(e) => setProxy(e.target.value)}
                className="h-7 w-[220px] rounded-sm bg-surface-3 px-2.5 text-[12px] text-ink shadow-[inset_0_0_0_1px_var(--cds-border)] outline-none placeholder:text-ink-muted focus-visible:shadow-focus"
              />
              <Button size="sm" variant="outline" onClick={() => save({ proxyURL: proxy })}>保存</Button>
            </Item>
            <Item title="剧库请求并发 / 同站间隔" desc="同一来源的元数据请求会串行执行，间隔用于降低被限流概率。">
              <input
                type="number"
                value={config.requestConcurrency}
                onChange={(e) => setConfig({ ...config, requestConcurrency: Number(e.target.value) })}
                onBlur={() => save({})}
                className="tnum h-7 w-[64px] rounded-sm bg-surface-3 text-center text-[12.5px] text-ink shadow-[inset_0_0_0_1px_var(--cds-border)] outline-none focus-visible:shadow-focus"
              />
              <input
                type="number"
                value={config.requestIntervalMs}
                onChange={(e) => setConfig({ ...config, requestIntervalMs: Number(e.target.value) })}
                onBlur={() => save({})}
                className="tnum h-7 w-[84px] rounded-sm bg-surface-3 text-center text-[12.5px] text-ink shadow-[inset_0_0_0_1px_var(--cds-border)] outline-none focus-visible:shadow-focus"
              />
            </Item>
            <Item title="检测当前连接" desc={network || "检查各站源的可达性。"} last>
              <Button
                size="sm" variant="outline"
                onClick={() => {
                  setNetwork("检测中…")
                  apiPost("/api/ui/network/check", {})
                    .then((r) => setNetwork((r.data || []).map((x) => `${x.name}：${x.error || x.detail || x.status}`).join("；")))
                    .catch((e) => setNetwork(e.message))
                }}
              >
                检测
              </Button>
            </Item>
          </>
        )

      case "about":
        return (
          <>
            <Item title="版本">
              <span className="tnum text-[12.5px] text-ink-secondary">{version || "开发版"}</span>
            </Item>
            <Item title="检查更新">
              <Button
                size="sm" variant="outline"
                disabled={!native?.checkForUpdates}
                onClick={() => native.checkForUpdates().then((r) => toast(r?.message || "已检查", "ok")).catch((e) => toast(e.message, "error"))}
              >
                检查更新
              </Button>
            </Item>
            <Item title="数据目录" desc={config.dataDir}>
              <Button size="sm" variant="ghost" onClick={() => revealPath(config.dataDir)}>打开</Button>
            </Item>
            <Item title="诊断日志" desc="logs/app.log">
              <Button size="sm" variant="ghost" onClick={() => revealPath(`${config.dataDir}/logs/app.log`)}>显示</Button>
            </Item>
            <Item title="重新运行首次引导" last>
              <Button
                size="sm" variant="outline"
                onClick={() => { localStorage.removeItem("juku.onboarded"); location.reload() }}
              >
                运行
              </Button>
            </Item>
          </>
        )

      default:
        return (
          <>
            <Item title="主题" desc="界面配色，跟随系统时读取 macOS / Windows 的外观设置。">
              <Segmented value={theme} options={THEMES} onChange={(t) => { setTheme(t); setThemeState(t) }} />
            </Item>
            <Item title="关闭窗口时" desc="开启后关闭窗口保留在后台，下载与合并不中断。" last>
              <Switch
                checked={closeToTray}
                onChange={(v) => { setCloseToTray(v); native?.setSettings?.({ closeToTray: v }) }}
              />
            </Item>
          </>
        )
    }
  }

  if (!config) return <ErrorBox error={error} />

  return (
    <div className="flex flex-col gap-4" ref={gridRef}>
      {SECTIONS.map(({ key, label }) => (
        <section
          key={key}
          id={`set-${key}`}
          className={`scroll-mt-4 ${key === section ? "rounded-lg shadow-[0_0_0_1px_var(--cds-border-strong),var(--cds-shadow-sm)]" : ""}`}
        >
          <Card className="px-4 py-1">
            <div className="flex items-baseline gap-2.5 pb-1 pt-4">
              <h3 className="text-sm font-medium tracking-[-.015em] text-ink">{label}</h3>
            </div>
            {body(key)}
          </Card>
        </section>
      ))}

      <div className="flex items-center gap-2 px-1 text-[11px] text-ink-muted">
        <StatusDot tone={status?.clientMode ? "ok" : "warn"} />
        {status?.clientMode ? "本机所有者模式 · 无需登录" : "非客户端模式"}
        <span className="grow" />
        设置保存在本机数据目录，修改后立即生效
      </div>
    </div>
  )
}