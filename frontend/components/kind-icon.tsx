// リソースの種類ごとの、色つきのアイコン(deploy は紫、volume は黄、ingress は青、service は緑)。

/** リソースの種類。 */
export type Kind = "deploy" | "volume" | "ingress" | "service" | "configmap"

/** アイコンの形(線)。 */
const PATH: Record<Kind, string> = {
  deploy: "M12 3l8 4.5v9L12 21l-8-4.5v-9zM12 12l8-4.5M12 12v9M12 12L4 7.5",
  volume: "M4 6c0-1.7 3.6-3 8-3s8 1.3 8 3-3.6 3-8 3-8-1.3-8-3zM4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3",
  ingress: "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18zM3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18",
  configmap: "M6 3h8l4 4v14H6zM14 3v4h4M9 12h6M9 16h6",
  service: "M12 3a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM5 15a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM19 15a3 3 0 1 0 0 6 3 3 0 0 0 0-6zM12 9v3M12 12l-5.5 3M12 12l5.5 3",
}

/**
 * 色つきのアイコン(角丸の四角に、白い線の絵)。
 *
 * @param props.kind リソースの種類。
 * @param props.size 四角の大きさ(px)。既定は 32。
 */
export function KindIcon({ kind, size = 32 }: { kind: Kind; size?: number }) {
  return (
    <span className={`kind-icon k-${kind}`} style={{ width: size, height: size }} aria-hidden="true">
      <svg viewBox="0 0 24 24" width={Math.round(size * 0.58)} height={Math.round(size * 0.58)} fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"><path d={PATH[kind]} /></svg>
    </span>
  )
}
