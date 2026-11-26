"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import type { IngressDomain } from "@/lib/types"

/** 1 つの ingress に付けられる、追加のホスト名の数の上限(サーバーと同じ)。 */
const MAX_DOMAINS = 5

/** 入力した名前の後ろに、必ず付く固定の文字列(サーバーと同じ。api、www など、こちらで使う名前と重ならないようにするため)。 */
const NAME_SUFFIX = "-run"

/**
 * ingress の「好きな名前のホスト名」のカード(追加、一覧、削除)。
 *
 * 払い出されたホスト名(例: sunny-jade-cat.launchs.org)のほかに、<好きな名前>-run.<ベースドメイン> でも、
 * この ingress を開けるようにする。ベースドメインの下の名前は、すぐに有効になる(DNS の設定は要らない)。
 * 自分のドメイン(外部のドメイン)は、まだ使えない。
 *
 * @param props.basePath この ingress の API のパス(`/api/v1/projects/.../ingresses/...`)。
 * @param props.locked 反映中・削除中などで、操作できないとき。
 * @param props.notify 通知を出す関数。
 */
export function IngressDomains({ basePath, locked, notify }: { basePath: string; locked: boolean; notify: (message: string, isError?: boolean) => void }) {
  const domainsPath = `${basePath}/domains`
  const { data, error, mutate } = useSWR<{ baseDomain: string; domains: IngressDomain[] }>(domainsPath, authenticatedFetcher, { revalidateOnFocus: true })
  const [subdomain, setSubdomain] = useState("")
  const [busy, setBusy] = useState("")
  const domains = data?.domains ?? []
  const baseDomain = data?.baseDomain ?? ""

  /**
   * 操作を実行し、終わったら取り直す(失敗は通知する)。
   *
   * @param key 実行中の印(ボタンの無効化に使う)。
   * @param action 実行する処理。
   */
  async function run(key: string, action: () => Promise<void>) {
    // 実行中の印を付け、成功なら取り直し、失敗なら通知する。
    setBusy(key)
    try { await action(); await mutate() } catch (caught) { notify(caught instanceof Error ? caught.message : "操作に失敗しました", true) } finally { setBusy("") }
  }

  /** 入力した名前を追加する(すぐに有効になる)。 */
  async function add() {
    // 空なら何もしない。
    if (!subdomain.trim()) return
    await run("add", async () => {
      const created = await api.post<IngressDomain>(domainsPath, { subdomain })
      setSubdomain("")
      notify(`${created.hostname} を追加しました`)
    })
  }

  /**
   * ホスト名を外す。
   *
   * @param domain 外すホスト名。
   */
  async function remove(domain: IngressDomain) {
    await run(domain.domainId, async () => { await api.delete(`${domainsPath}/${encodeURIComponent(domain.domainId)}`); notify(`${domain.hostname} を外しました`) })
  }

  return (
    <div className="card" style={{ overflow: "hidden" }}>
      <div className="row pad" style={{ paddingBottom: 4 }}><h2 className="h2 grow">好きな名前のホスト名</h2><span className="faint" style={{ fontSize: 12.5 }}>{domains.length} / {MAX_DOMAINS}</span></div>
      <div className="mut" style={{ fontSize: 12.5, padding: "0 18px 10px" }}>
        払い出されたホスト名のほかに、好きな名前({baseDomain ? `例: myapp${NAME_SUFFIX}.${baseDomain}` : "例: myapp"})でも、この ingress を開けるようにします。追加すると、すぐに使えます。
        自分のドメイン(example.com など)は、まだ使えません。
      </div>
      {error && !data && <div className="tr mut" style={{ ["--cols" as string]: "1fr" }}>ホスト名を取得できませんでした。</div>}
      {data && domains.length === 0 && <div className="tr mut" style={{ ["--cols" as string]: "1fr" }}>追加したホスト名はありません。</div>}
      {domains.map((domain) => (
        <div key={domain.domainId} className="row" style={{ padding: "12px 18px", borderTop: "1px solid var(--line)", gap: 8, flexWrap: "wrap" }}>
          <a className="mono grow" style={{ fontWeight: 500, overflowWrap: "anywhere", color: "var(--accent)" }} href={`https://${domain.hostname}`} target="_blank" rel="noreferrer">{domain.hostname}</a>
          <span className="badge b-ok"><i />有効</span>
          <button className="btn sm danger" type="button" disabled={locked || busy !== ""} onClick={() => void remove(domain)}>外す</button>
        </div>
      ))}
      <div className="row" style={{ padding: "12px 18px 14px", borderTop: "1px solid var(--line)", gap: 8 }}>
        <input className="inp mono grow" value={subdomain} onChange={(event) => setSubdomain(event.target.value.toLowerCase())} onKeyDown={(event) => { if (event.key === "Enter") void add() }}
          placeholder="myapp" aria-label="追加する名前" maxLength={59} disabled={locked || domains.length >= MAX_DOMAINS} />
        <span className="mono mut" style={{ whiteSpace: "nowrap" }}>{NAME_SUFFIX}{baseDomain && `.${baseDomain}`}</span>
        <button className="btn sm primary" type="button" disabled={locked || busy !== "" || !subdomain.trim() || domains.length >= MAX_DOMAINS} onClick={() => void add()}>追加</button>
      </div>
      <div className="faint" style={{ fontSize: 12, padding: "0 18px 12px" }}>名前は、3〜59 文字の、小文字の英数字とハイフンです。後ろには、自動で「-run」が付きます。ほかの人が使っている名前は、使えません。</div>
    </div>
  )
}
