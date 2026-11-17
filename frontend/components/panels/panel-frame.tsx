// 右のパネルの、共通の枠(見出し、閉じるボタン、中身)。deploy・ingress・volume のパネルが、これを使う。
import type { ReactNode } from "react"
import { KindIcon, type Kind } from "@/components/kind-icon"

/**
 * パネルの枠。
 *
 * @param props.kind リソースの種類(色つきのアイコンを出す)。
 * @param props.title 題(ID や名前)。
 * @param props.subtitle 題の下の、補足(イメージ名など)。
 * @param props.badge 題の横に出すもの(状態のバッジなど)。
 * @param props.actions 右上の操作(削除など)。閉じるボタンの左に並ぶ。
 * @param props.onClose 閉じるとき。
 * @param props.children 本文(タブなど)。
 */
export function PanelFrame(props: { kind: Kind; title: string; subtitle?: string; badge?: ReactNode; actions?: ReactNode; onClose: () => void; children: ReactNode }) {
  return (
    <div className="panel-frame">
      <header className="panel-head">
        <KindIcon kind={props.kind} size={36} />
        <div className="panel-title">
          <div className="row" style={{ gap: 8, flexWrap: "nowrap" }}><h2 className="mono" title={props.title}>{props.title}</h2>{props.badge}</div>
          {props.subtitle && <div className="mut mono" style={{ fontSize: 12, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={props.subtitle}>{props.subtitle}</div>}
        </div>
        {props.actions}
        <button className="btn ico ghost" type="button" onClick={props.onClose} aria-label="パネルを閉じる"><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18" /></svg></button>
      </header>
      <div className="panel-body">{props.children}</div>
    </div>
  )
}
