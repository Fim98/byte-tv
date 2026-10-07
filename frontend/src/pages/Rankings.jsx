import { useState } from "react"
import { apiGet } from "../api"
import { ErrorBox, Poster, Segmented, Skeleton, useAsync } from "../components/common"
import { download, follow } from "../actions"

const BOARDS = [["hongguo-hot", "总热播榜"], ["hongguo-real", "真人剧榜"], ["hongguo-comic", "漫剧榜"], ["hongguo-ai", "AI剧榜"]]

export default function Rankings({ route }) {
  const [board, setBoard] = useState(route.query.get("board") || "hongguo-hot")
  const [page, setPage] = useState(1)
  const state = useAsync((signal) => apiGet(`/api/ui/rankings?board=${board}&page=${page}`, { signal }), [board, page])
  return (
    <div>
      <h1 className="page-title">榜单</h1>
      <div className="toolbar">
        <Segmented value={board} options={BOARDS} onChange={(b) => { setBoard(b); setPage(1) }} />
        {state.data?.updatedText ? <span className="muted">{state.data.updatedText}</span> : null}
        {state.data?.stale ? <span className="muted">（缓存数据）</span> : null}
      </div>
      <ErrorBox error={state.error} onRetry={state.reload} />
      <div className="grid">
        {state.loading ? <Skeleton count={12} /> : null}
        {(state.data?.items || []).map((item) => <Poster key={item.drama.id} drama={item.drama} rank={item.rank} subtitle={item.metric ? `🔥 ${item.metric}` : ""} onFollow={() => follow(item.drama.id)} onDownload={() => download(item.drama.id)} />)}
      </div>
      <div className="pager">
        <button className="btn ghost sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</button>
        <span>第 {page} 页</span>
        <button className="btn ghost sm" disabled={!state.data?.hasMore} onClick={() => setPage(page + 1)}>下一页</button>
      </div>
    </div>
  )
}
