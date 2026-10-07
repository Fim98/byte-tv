(async () => {
  const log = (o) => fetch("/api/desktop/native/log", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(o) })
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
  try {
    location.hash = "#/home"; await sleep(6000)
    await log({ step: "shot", name: "03-home" }); await sleep(2500)
    const settings = await window.juku.getSettings()
    const set = await window.juku.setSettings({ closeToTray: true })
    const upd = await window.juku.checkForUpdates()
    let notify; try { notify = await window.juku.notify("Byte TV", "测试通知") } catch (e) { notify = String(e) }
    const prefs = await (await fetch("/api/desktop/prefs")).json()
    await log({ step: "native", settings, set, upd, notify, prefKeys: Object.keys(prefs), theme: localStorage.getItem("juku.theme") })
    location.hash = "#/settings"; await sleep(4000)
    await log({ step: "shot", name: "04-settings" }); await sleep(2500)
    await log({ step: "menu-wait", hash: location.hash })
    const t = Date.now(); while (Date.now() - t < 90000 && !location.hash.includes("downloads")) await sleep(200)
    await log({ step: "menu-nav", hash: location.hash }); await sleep(3000)
    await log({ step: "shot", name: "05-downloads-via-menu" }); await sleep(2500)
    await log({ step: "done" })
  } catch (e) { await log({ step: "error", message: String(e) }) }
})()
