import { useEffect, useState } from "react"
import { Check, FolderOpen } from "lucide-react"
import { apiGet, apiPost } from "../api"
import { Button, StatusDot } from "../components/cds"
import { pickDirectory } from "../components/directory"

// 首次启动引导：欢迎 → 下载位置 → 网络检测。
// 不暴露底层实现：组件准备状态不出现在引导里，用户不应感知技术栈。
export default function Onboarding({ onDone }) {
  const [step, setStep] = useState(0)
  const [config, setConfig] = useState(null)
  const [network, setNetwork] = useState(null)
  const [error, setError] = useState("")

  useEffect(() => {
    apiGet("/api/ui/config").then(setConfig).catch((e) => setError(e.message))
  }, [])

  useEffect(() => {
    if (step !== 2) return
    apiPost("/api/ui/network/check", {})
      .then((r) => setNetwork(r.data || []))
      .catch((e) => setNetwork([{ name: "检测", error: e.message }]))
  }, [step])

  const changeDir = async () => {
    try {
      const path = await pickDirectory(config?.outputDirSetting)
      if (!path) return
      setConfig(await apiPost("/api/ui/config", {
        concurrency: config.concurrency,
        requestConcurrency: config.requestConcurrency,
        requestIntervalMs: config.requestIntervalMs,
        outputDir: path,
      }))
    } catch (e) {
      setError(e.message)
    }
  }

  const steps = [
    {
      title: "欢迎",
      body: (
        <>
          <p className="text-[12.5px] leading-relaxed text-ink-secondary">
            在线观看与下载短剧，支持续播、追剧清单与离线下载。
          </p>
          <p className="mt-3 text-[11.5px] leading-relaxed text-ink-muted">
            请仅下载你拥有权利或已获授权的内容。
          </p>
        </>
      ),
    },
    {
      title: "下载位置",
      body: (
        <>
          <p className="break-all rounded-sm bg-surface-3 px-3 py-2.5 font-mono text-[12px] text-ink-secondary">
            {config?.outputDirSetting || "读取中…"}
          </p>
          <Button size="sm" variant="outline" icon={FolderOpen} className="mt-3" onClick={changeDir}>更改位置</Button>
          {config?.restartRequired ? (
            <p className="mt-2 text-[11.5px] text-warn">新目录将在重启后生效</p>
          ) : null}
        </>
      ),
    },
    {
      title: "检测网络",
      body: (
        <>
          {network ? (
            <div className="flex flex-col gap-2">
              {network.map((n) => (
                <div key={n.name} className="flex items-center gap-2.5 text-[12.5px]">
                  <StatusDot tone={n.error ? "err" : "ok"} />
                  <span className="text-ink">{n.name}</span>
                  <span className="grow" />
                  <span className="text-ink-muted">{n.error ? n.error : (n.detail || n.status || "可用")}</span>
                </div>
              ))}
            </div>
          ) : (
            <div className="flex items-center gap-2.5 text-[12.5px] text-ink-muted">
              <span className="inline-flex gap-1">
                <i className="size-1 animate-wave rounded-full bg-clay" />
                <i className="size-1 animate-wave rounded-full bg-clay [animation-delay:.12s]" />
                <i className="size-1 animate-wave rounded-full bg-clay [animation-delay:.24s]" />
              </span>
              检测中…
            </div>
          )}
          <p className="mt-3 text-[11.5px] leading-relaxed text-ink-muted">
            如无法连接，可稍后在「设置 → 网络」中配置代理。
          </p>
        </>
      ),
    },
  ]

  return (
    <div className="grid h-full place-items-center bg-surface-0">
      <div className="w-[460px] rounded-dialog bg-surface-2 p-7 shadow-popover">
        {/* 步骤指示：陶土色短线 */}
        <div className="mb-5 flex items-center gap-1.5">
          {steps.map((_, i) => (
            <i
              key={i}
              className={`h-1 rounded-pill transition-all duration-300 ease-out ${
                i <= step ? "w-6 bg-clay" : "w-4 bg-[var(--cds-alpha-3)]"
              }`}
            />
          ))}
        </div>

        <h1 className="text-[20px] font-medium tracking-[-.02em] text-ink">{steps[step].title}</h1>
        <div className="mt-3.5">{steps[step].body}</div>

        {error ? <p className="mt-3 text-[12px] text-danger">{error}</p> : null}

        <div className="mt-7 flex items-center gap-2">
          {step > 0 ? <Button variant="ghost" onClick={() => setStep(step - 1)}>上一步</Button> : null}
          <span className="grow" />
          {step < steps.length - 1 ? (
            <Button variant="primary" onClick={() => setStep(step + 1)}>下一步</Button>
          ) : (
            <Button variant="primary" icon={Check} onClick={onDone}>开始使用</Button>
          )}
        </div>

        <button
          type="button"
          onClick={onDone}
          className="mt-3.5 w-full text-center text-[11.5px] text-ink-muted hover:text-ink-secondary"
        >
          跳过引导
        </button>
      </div>
    </div>
  )
}