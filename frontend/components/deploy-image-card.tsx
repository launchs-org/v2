"use client"

import useSWR from "swr"
import { authenticatedFetcher } from "@/lib/api"
import { shortDateTime } from "@/lib/format-date"
import type { ImageHistoryResponse } from "@/lib/types"
import { CopyButton } from "@/components/api-token-section"

/**
 * deploy の「イメージ」の欄。いま動いているイメージの URL と、イメージの更新履歴(新しい順)を見せる。
 * 履歴は、イメージを入れ替える反映(画面の起動設定、API、webhook による自動デプロイ)が終わるたびに増える。
 *
 * @param props.deployId 対象の deploy の ID。
 * @param props.imageRef いま動いている(最後に反映した)イメージ。
 * @param props.refreshInterval 自動更新の間隔(ミリ秒)。
 */
export function DeployImageCard({ deployId, imageRef, refreshInterval }: { deployId: string; imageRef: string; refreshInterval: number }) {
  // 更新履歴を取る(まだ入れ替えたことが無ければ、空)。
  const { data } = useSWR<ImageHistoryResponse>(`/api/v1/deploy/${encodeURIComponent(deployId)}/image-history`, authenticatedFetcher, { refreshInterval, revalidateOnFocus: true, refreshWhenHidden: false })
  return (
    <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <h2 className="h2">イメージ</h2>
      <div className="col" style={{ gap: 6 }}>
        <div className="faint" style={{ fontSize: 12 }}>実行中のイメージ</div>
        <div className="row" style={{ gap: 8 }}>
          <span className="mono grow" style={{ wordBreak: "break-all" }}>{imageRef}</span>
          <CopyButton text={imageRef} />
        </div>
      </div>
      <div className="faint" style={{ fontSize: 12 }}>更新履歴</div>
      {data && data.history.length === 0 && <div className="mut" style={{ fontSize: 12.5 }}>イメージを入れ替えた記録は、まだありません。</div>}
      {data?.history.map((item, index) => (
        <div key={`${item.at}-${index}`} className="col" style={{ gap: 2, borderTop: "1px solid var(--line)", paddingTop: 8 }}>
          <div className="row" style={{ gap: 8 }}>
            <span className={`badge ${item.result === "applied" ? "b-ok" : "b-bad"}`}><i />{item.result === "applied" ? "反映した" : "失敗"}</span>
            <span className="faint mono" style={{ fontSize: 11.5 }}>{shortDateTime(item.at)}</span>
          </div>
          <div className="mono" style={{ fontSize: 12, wordBreak: "break-all" }}>{item.imageRef}</div>
          <div className="faint mono" style={{ fontSize: 11.5, wordBreak: "break-all" }}>前: {item.previousImageRef}</div>
        </div>
      ))}
    </div>
  )
}
