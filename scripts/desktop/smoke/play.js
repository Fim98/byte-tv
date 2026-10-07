(async () => {
  const log = (o) => fetch("/api/desktop/native/log", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(o) })
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
  const waitFor = async (fn, ms) => { const t = Date.now(); while (Date.now() - t < ms) { try { const v = fn(); if (v) return v } catch {} await sleep(300) } return null }
  try {
    localStorage.setItem("juku.onboarded", "1")
    await log({ step: "boot", shell: window.juku?.shell, platform: window.juku?.platform, hash: location.hash, version: await window.juku?.getVersion?.() })
    const skip = [...document.querySelectorAll("button,a")].find((b) => b.textContent.trim() === "跳过引导"); if (skip) skip.click(); await sleep(800)
    location.hash = "#/drama/hongguo:7689666942688316478"
    const btn = await waitFor(() => document.querySelector(".detail .btn.primary"), 60000)
    await log({ step: "detail", found: !!btn, label: btn?.textContent })
    const first = [...document.querySelectorAll(".detail .btn.ghost")].find((b) => b.textContent.includes("从第 1 集开始"))
    ;(first || btn).click()
    const vid = await waitFor(() => { const v = document.querySelector("video"); return v && v.readyState >= 1 && v }, 60000)
    await sleep(1500)
    ;["play","playing","pause","waiting","error"].forEach((n) => vid.addEventListener(n, () => log({ ev: n, t: vid.currentTime })))
    await log({ step: "gesture-wait", paused: vid.paused })
    await waitFor(() => !vid.paused && vid.currentTime > 0.5, 30000)
    const t0 = vid?.currentTime
    await sleep(6000)
    await log({ step: "advance", t0, t1: vid?.currentTime, paused: vid?.paused, readyState: vid?.readyState, w: vid?.videoWidth, h: vid?.videoHeight, ep: document.querySelector(".player-top .muted")?.textContent })
    const v = await waitFor(() => { const v = document.querySelector("video"); return v && v.currentTime > 3 && v }, 90000)
    const video = document.querySelector("video")
    await log({ step: "playing", ok: !!v, currentTime: video?.currentTime, w: video?.videoWidth, h: video?.videoHeight, fit: video && getComputedStyle(video).objectFit, src: video?.currentSrc?.slice(0, 40), rate: video?.playbackRate, err: document.querySelector(".player-error")?.textContent })
    await sleep(4000)
    await log({ step: "shot-ready", currentTime: document.querySelector("video")?.currentTime })
  } catch (e) { await log({ step: "error", message: String(e) }) }
})()
