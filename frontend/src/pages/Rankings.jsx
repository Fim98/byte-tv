import { useState } from "react"
import { apiGet } from "../api"
import { Button, ErrorBox, Segmented, SkeletonPoster, Tag, useAsync } from "../components/cds"
import { Poster, PosterGrid } from "../components/poster"
import { download, follow } from "../actions"

const BOARDS = [["hongguo-hot", "总热播榜"], ["hongguo-real", "真人剧榜"], ["hongguo-comic", "漫剧榜"], ["hongguo-ai", "AI剧榜"]]

export default function Rankings({ route }) {
  const [board, setBoard] = useState(route.query.get("board") || "hongguo-hot")
  const [page, setPage] = useState(1)
  const state = useAsync((signal) => apiGet(`/api/ui/rankings?board=${board}&page=${page}`, { signal }), [board, page])
  const items = state.data?.items || []

  return (
    <div className="flex flex-col gap-3.5">
      <div className="flex items-center gap-2.5">
        <Segmented value={board} options={BOARDS} onChange={(b) => { setBoard(b); setPage(1) }} />
        <span className="grow" />
        {state.data?.updatedText ? <span className="text-[11.5px] text-ink-muted">更新于 {state.data.updatedText}</span> : null}
        {state.data?.stale ? <Tag>缓存数据</Tag> : null}
      </div>

      <ErrorBox error={state.error} onRetry={state.reload} />

      <PosterGrid>
        {state.loading
          ? Array.from({ length: 12 }, (_, i) => <SkeletonPoster key={i} className="h-[212px]" />)
          : items.map((item) => (
              <Poster
                key={item.drama.id}
                drama={item.drama}
                rank={item.rank}
                subtitle={item.metric || ""}
                onFollow={() => follow(item.drama.id)}
                onDownload={() => download(item.drama.id)}
              />
            ))}
      </PosterGrid>

      <div className="flex items-center justify-center gap-3">
        <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</Button>
        <span className="tnum text-[12px] text-ink-muted">第 {page} 页</span>
        <Button size="sm" variant="outline" disabled={!state.data?.hasMore} onClick={() => setPage(page + 1)}>下一页</Button>
      </div>
    </div>
  )
}