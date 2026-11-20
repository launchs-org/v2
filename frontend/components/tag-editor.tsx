"use client"

import { useState } from "react"

/**
 * タグの編集(deploy、volume、service、ingress の画面で共通)。タグを、チップで見せ、× で外し、入力欄に書いて Enter(か「追加」)で付ける。
 * 保存は、タグ全体の置き換え。保存の中身(どの API を呼ぶか)は、呼び出し側が決める。
 *
 * @param props.tags いまのタグ。
 * @param props.save 新しいタグの全てを保存する関数(失敗したら、例外を投げる。成功したら、画面を取り直す)。
 * @param props.onError 保存に失敗したときに、メッセージを知らせる関数(省略すると、画面の下に出す)。
 */
export function TagEditor({ tags, save, onError }: { tags: string[]; save: (tags: string[]) => Promise<unknown>; onError?: (message: string) => void }) {
  const [draft, setDraft] = useState("")
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState("")

  /** 新しいタグの組を保存する(失敗は、メッセージにして出す)。 */
  async function commit(next: string[]) {
    setBusy(true)
    setProblem("")
    try {
      await save(next)
    } catch (caught) {
      const message = caught instanceof Error ? caught.message : "タグを保存できませんでした"
      if (onError) onError(message)
      else setProblem(message)
    } finally {
      setBusy(false)
    }
  }

  /** 入力したタグを足して、保存する(重複は、サーバーが 1 つにまとめる)。 */
  function add() {
    const tag = draft.trim()
    if (!tag) return
    setDraft("")
    void commit([...tags, tag])
  }

  return (
    <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 10 }}>
      <h2 className="h2">タグ</h2>
      <div className="row" style={{ gap: 6, flexWrap: "wrap" }}>
        {tags.map((tag) => <span key={tag} className="tagchip" style={{ display: "inline-flex", gap: 6, alignItems: "center" }}>{tag}<button type="button" className="btn ico ghost" style={{ padding: 0, minWidth: 16 }} disabled={busy} aria-label={`タグ ${tag} を外す`} onClick={() => void commit(tags.filter((item) => item !== tag))}>×</button></span>)}
        {tags.length === 0 && <span className="mut">タグはありません。</span>}
      </div>
      <div className="row" style={{ gap: 8 }}>
        <input className="inp mono grow" value={draft} onChange={(event) => setDraft(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") add() }} placeholder="例: prod、team-a(小文字の英数字と . _ : -)" aria-label="タグを追加" />
        <button className="btn" type="button" onClick={add} disabled={busy || !draft.trim() || tags.length >= 10}>追加</button>
      </div>
      {problem && <div style={{ color: "var(--bad)", fontSize: 12.5 }}>{problem}</div>}
      <div className="faint" style={{ fontSize: 12 }}>タグは 10 個まで。「タグ」タブで、同じタグのものを、まとめて削除できます。</div>
    </div>
  )
}
