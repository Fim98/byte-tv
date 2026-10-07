import { useEffect, useRef, useState } from "react"
import { apiGet, apiPost, initViewer } from "./api"
import Player from "./Player"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"

const pageSize = 60

function formatPosition(value) {
  if (!Number.isFinite(value) || value < 0) return "0:00"
  const total = Math.floor(value)
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`
}

export default function App() {
  const [view, setView] = useState("library")
  const [query, setQuery] = useState("")
  const [dramas, setDramas] = useState([])
  const [visible, setVisible] = useState(pageSize)
  const [detail, setDetail] = useState(null)
  const [history, setHistory] = useState([])
  const [following, setFollowing] = useState([])
  const [opened, setOpened] = useState(null)
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(false)
  const catalogRequest = useRef(0)

  useEffect(() => {
    let stopped = false
    initViewer()
      .then(() => loadCatalog(""))
      .catch((err) => {
        if (!stopped) setError(err.message || "无法连接剧库")
      })
    return () => {
      stopped = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function loadCatalog(nextQuery) {
    const requestID = ++catalogRequest.current
    setLoading(true)
    try {
      const path = nextQuery ? `/api/desktop/catalog?q=${encodeURIComponent(nextQuery)}` : "/api/desktop/catalog"
      const result = await apiGet(path)
      if (requestID !== catalogRequest.current) return
      setDramas(result.data || [])
      setVisible(pageSize)
      setError("")
    } catch (err) {
      if (requestID === catalogRequest.current) setError(err.message || "剧库加载失败")
    } finally {
      if (requestID === catalogRequest.current) setLoading(false)
    }
  }

  async function openDetail(id) {
    setLoading(true)
    setView("detail")
    try {
      const [drama, saved] = await Promise.all([
        apiGet(`/api/desktop/drama?id=${encodeURIComponent(id)}`),
        apiGet("/api/ui/following"),
      ])
      const followed = (saved.data || []).some((item) => item.dramaId === id && item.saved)
      setDetail({ ...drama, followed })
      setError("")
    } catch (err) {
      setError(err.message || "无法打开短剧")
    } finally {
      setLoading(false)
    }
  }

  async function openPlayback(body) {
    setLoading(true)
    try {
      const result = await apiPost("/api/ui/playback/open", body)
      setOpened(result)
      setView("player")
      setError("")
    } catch (err) {
      setError(err.message || "无法开始播放")
    } finally {
      setLoading(false)
    }
  }

  async function toggleFollow() {
    if (!detail?.drama?.id) return
    const saved = !detail.followed
    const result = await apiPost("/api/ui/following", { dramaId: detail.drama.id, saved })
    if (result.ok) setDetail({ ...detail, followed: saved })
  }

  async function showHistory() {
    setView("history")
    const result = await apiGet("/api/ui/playback/history")
    setHistory(result.data || [])
  }

  async function showFollowing() {
    setView("following")
    const result = await apiGet("/api/ui/following")
    setFollowing((result.data || []).filter((item) => item.saved))
  }

  const shown = dramas.slice(0, visible)

  return (
    <main className="flex h-screen flex-col">
      <header className="flex items-center gap-3 border-b px-4 py-3">
        <h1 className="text-xl font-semibold">剧库</h1>
        <Button variant={view === "library" || view === "detail" ? "default" : "ghost"} onClick={() => { setView("library"); loadCatalog(query) }}>剧库</Button>
        <Button variant={view === "history" ? "default" : "ghost"} onClick={showHistory}>继续观看</Button>
        <Button variant={view === "following" ? "default" : "ghost"} onClick={showFollowing}>追剧</Button>
        <div className="ml-auto w-full max-w-sm">
          <Input
            value={query}
            placeholder="搜索剧名"
            aria-label="搜索剧名"
            onChange={(event) => {
              const value = event.target.value
              setQuery(value)
              setView("library")
              loadCatalog(value.trim())
            }}
          />
        </div>
      </header>
      {error ? <p className="px-4 pt-3 text-sm text-primary">{error}</p> : null}
      <div className="min-h-0 flex-1 p-4">
        {view === "player" && opened ? (
          <Player opened={opened} onClose={() => { setOpened(null); setView("library") }} />
        ) : null}
        {view === "detail" && detail?.drama ? (
          <div className="grid gap-4 md:grid-cols-[220px_minmax(0,1fr)]">
            <Card>
              {detail.drama.cover ? <img src={detail.drama.cover} alt="" className="aspect-[3/4] w-full object-cover" /> : <div className="aspect-[3/4] bg-muted" />}
              <CardHeader>
                <CardTitle>{detail.drama.title}</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-2">
                {detail.drama.category ? <Badge>{detail.drama.category}</Badge> : null}
                <Button variant={detail.followed ? "secondary" : "default"} onClick={toggleFollow}>{detail.followed ? "已追剧" : "追剧"}</Button>
                {detail.episodes?.length ? null : (
                  <Button onClick={() => openPlayback({ dramaId: detail.drama.id })}>在线播放</Button>
                )}
              </CardContent>
            </Card>
            <ScrollArea className="h-[calc(100vh-8rem)] rounded-lg border">
              <div className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-2 p-3">
                {(detail.episodes || []).map((episode) => (
                  <Button key={episode.taskId || episode.index} variant="secondary" onClick={() => openPlayback({ taskId: episode.taskId })}>
                    {episode.title || `第${episode.episode}集`}
                  </Button>
                ))}
                {!detail.episodes?.length ? <p className="text-sm text-muted-foreground">本地还没有完成的分集，可以在线播放。</p> : null}
              </div>
            </ScrollArea>
          </div>
        ) : null}
        {view === "history" ? (
          <ScrollArea className="h-full">
            <div className="grid gap-3">
              {history.length === 0 ? <p className="text-sm text-muted-foreground">还没有观看记录。</p> : null}
              {history.map((item) => (
                <Card key={item.dramaId}>
                  <CardHeader>
                    <CardTitle>{item.title}</CardTitle>
                  </CardHeader>
                  <CardContent className="flex items-center justify-between gap-3">
                    <p className="text-sm text-muted-foreground">第{item.episode}集 · {formatPosition(item.position)}</p>
                    <Button onClick={() => openPlayback({ dramaId: item.dramaId, resume: true, fromHistory: true })}>继续观看</Button>
                  </CardContent>
                </Card>
              ))}
            </div>
          </ScrollArea>
        ) : null}
        {view === "following" ? (
          <ScrollArea className="h-full">
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {following.length === 0 ? <p className="text-sm text-muted-foreground">还没有追剧。</p> : null}
              {following.map((item) => (
                <Card key={item.dramaId}>
                  <CardHeader>
                    <CardTitle>{item.title}</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <Button onClick={() => openDetail(item.dramaId)}>打开</Button>
                  </CardContent>
                </Card>
              ))}
            </div>
          </ScrollArea>
        ) : null}
        {view === "library" ? (
          <ScrollArea className="h-full">
            {loading && dramas.length === 0 ? <p className="text-sm text-muted-foreground">正在读取剧库…</p> : null}
            {!loading && dramas.length === 0 ? <p className="text-sm text-muted-foreground">没有找到短剧。</p> : null}
            <div className="grid grid-cols-[repeat(auto-fill,minmax(180px,1fr))] gap-3">
              {shown.map((drama) => (
                <Card key={drama.id} className="cursor-pointer overflow-hidden" onClick={() => openDetail(drama.id)}>
                  {drama.cover ? (
                    <img src={drama.cover} alt="" className="aspect-[3/4] w-full object-cover" />
                  ) : (
                    <div className="aspect-[3/4] bg-muted" />
                  )}
                  <CardHeader>
                    <CardTitle className="line-clamp-2 text-sm">{drama.title}</CardTitle>
                  </CardHeader>
                  <CardContent className="flex gap-2">
                    {drama.local ? <Badge>本地</Badge> : null}
                    {drama.totalEpisode ? <Badge variant="outline">{drama.totalEpisode}集</Badge> : null}
                  </CardContent>
                </Card>
              ))}
            </div>
            {visible < dramas.length ? (
              <Button className="mt-4" variant="secondary" onClick={() => setVisible((count) => count + pageSize)}>加载更多</Button>
            ) : null}
          </ScrollArea>
        ) : null}
      </div>
    </main>
  )
}
