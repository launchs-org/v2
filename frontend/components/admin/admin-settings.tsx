"use client"

import useSWR from "swr"
import { authenticatedFetcher } from "@/lib/api"
import { ErrorState, LoadingState } from "@/components/ui-kit"

type Setting = { name: string; value: string }

/**
 * システムの設定(読むだけ)。環境変数(秘密でないもの)の、いまの値。空は、未設定(既定で動いている)。
 * 変えるときは、デプロイの設定(deploy/settings/config.yaml、Argo CD の上書き)を直して、反映する。
 *
 * @param props.refreshInterval 自動更新の間隔(ミリ秒)。
 */
export function AdminSettings({ refreshInterval }: { refreshInterval: number }) {
  const { data, error, mutate } = useSWR<{ settings: Setting[] }>("/admin-api/settings", authenticatedFetcher, { refreshInterval, revalidateOnFocus: true })
  const cols = { "--cols": "300px 1fr" } as React.CSSProperties
  return (
    <div className="card tscroll"><div className="tinner" style={{ minWidth: 560 }}>
      <div className="pad" style={{ paddingBottom: 8 }}>
        <h2 className="h2">システムの設定</h2>
        <div className="faint" style={{ fontSize: 12.5 }}>読むだけです。変えるときは、デプロイの設定を直して、反映します(秘密は、出しません)。</div>
      </div>
      <div className="tr th" style={cols}><span>名前</span><span>値</span></div>
      {!data && !error && <LoadingState />}
      {error && !data && <div className="pad"><ErrorState message="設定を取得できませんでした" onRetry={() => void mutate()} /></div>}
      {data?.settings.map((setting) => (
        <div key={setting.name} className="tr" style={cols}>
          <span className="mono">{setting.name}</span>
          <span className="mono" style={{ wordBreak: "break-all" }}>{setting.value || <span className="faint">(未設定。既定)</span>}</span>
        </div>
      ))}
    </div></div>
  )
}
