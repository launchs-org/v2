"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import type { IngressDomain, IngressDomainVerifyResult } from "@/lib/types"
import { CopyButton } from "@/components/api-token-section"

/** 1 つの ingress に付けられるカスタムドメインの数の上限(サーバーと同じ)。 */
const MAX_DOMAINS = 5

/**
 * カスタムドメインの 1 行の下に出す、DNS の設定の案内(TXT か CNAME のどちらか)。
 *
 * @param props.domain 確認待ちのドメイン。
 */
function DnsInstructions({ domain }: { domain: IngressDomain }) {
  const { txtName, txtValue, cnameValue } = domain.challenge
  return (
    <div className="col" style={{ gap: 10, fontSize: 13 }}>
      <div className="mut">ドメインを管理している場所(DNS)で、<b>次のどちらか 1 つ</b>を設定してから、「確認する」を押してください。</div>
      <div className="col" style={{ gap: 4 }}>
        <b>方法 1: TXT レコード(おすすめ。いまのサイトを止めずに確認できます)</b>
        <div className="row" style={{ gap: 8, flexWrap: "wrap" }}><span className="faint">名前</span><code className="mono" style={{ overflowWrap: "anywhere" }}>{txtName}</code><CopyButton text={txtName} /></div>
        <div className="row" style={{ gap: 8, flexWrap: "wrap" }}><span className="faint">値</span><code className="mono" style={{ overflowWrap: "anywhere" }}>{txtValue}</code><CopyButton text={txtValue} /></div>
      </div>
      <div className="col" style={{ gap: 4 }}>
        <b>方法 2: CNAME レコード</b>
        <div className="row" style={{ gap: 8, flexWrap: "wrap" }}><code className="mono" style={{ overflowWrap: "anywhere" }}>{domain.hostname}</code><span className="faint">→</span><code className="mono" style={{ overflowWrap: "anywhere" }}>{cnameValue}</code><CopyButton text={cnameValue} /></div>
      </div>
      <div className="faint">DNS の反映には、数分から数時間かかることがあります。</div>
    </div>
  )
}

/**
 * ingress のカスタムドメインのカード(追加、確認待ちの案内、確認、削除)。
 *
 * 処理の流れ:
 *  1. ドメインを追加すると、確認待ちになり、DNS に設定する内容(TXT または CNAME)が出る。
 *  2. DNS を設定して「確認する」を押すと、サーバーが DNS を引いて、持ち主であることを確かめる。
 *  3. 確認できたドメインは、公開のルーティングに入る(この ingress のルールが、そのドメインでも効く)。
 *
 * @param props.basePath この ingress の API のパス(`/api/v1/projects/.../ingresses/...`)。
 * @param props.locked 反映中・削除中などで、操作できないとき。
 * @param props.notify 通知を出す関数。
 */
export function IngressDomains({ basePath, locked, notify }: { basePath: string; locked: boolean; notify: (message: string, isError?: boolean) => void }) {
  const domainsPath = `${basePath}/domains`
  const { data, error, mutate } = useSWR<{ domains: IngressDomain[] }>(domainsPath, authenticatedFetcher, { revalidateOnFocus: true })
  const [hostname, setHostname] = useState("")
  const [busy, setBusy] = useState("")
  const [reasons, setReasons] = useState<Record<string, string>>({})
  const domains = data?.domains ?? []

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

  /** ドメインを追加する(確認待ちになる)。 */
  async function add() {
    // 空なら何もしない。
    if (!hostname.trim()) return
    await run("add", async () => { await api.post(domainsPath, { hostname }); setHostname(""); notify("ドメインを追加しました。DNS を設定して、確認してください") })
  }

  /**
   * DNS を見て、ドメインの持ち主であることを確かめる。
   *
   * @param domain 確認するドメイン。
   */
  async function verify(domain: IngressDomain) {
    await run(domain.domainId, async () => {
      const result = await api.post<IngressDomainVerifyResult>(`${domainsPath}/${encodeURIComponent(domain.domainId)}/verify`)
      // 合わなかったときは、理由を、行の下に出す。
      setReasons((current) => ({ ...current, [domain.domainId]: result.verified ? "" : result.reason }))
      if (result.verified) notify(`${domain.hostname} を確認しました。公開に反映しています`)
    })
  }

  /**
   * ドメインを外す。
   *
   * @param domain 外すドメイン。
   */
  async function remove(domain: IngressDomain) {
    await run(domain.domainId, async () => { await api.delete(`${domainsPath}/${encodeURIComponent(domain.domainId)}`); notify(`${domain.hostname} を外しました`) })
  }

  return (
    <div className="card" style={{ overflow: "hidden" }}>
      <div className="row pad" style={{ paddingBottom: 4 }}><h2 className="h2 grow">カスタムドメイン</h2><span className="faint" style={{ fontSize: 12.5 }}>{domains.length} / {MAX_DOMAINS}</span></div>
      <div className="mut" style={{ fontSize: 12.5, padding: "0 18px 10px" }}>自分のドメイン(例: app.example.com)でも、この ingress を開けるようにします。DNS で持ち主であることを確かめたドメインだけが、有効になります。</div>
      {error && !data && <div className="tr mut" style={{ ["--cols" as string]: "1fr" }}>ドメインを取得できませんでした。</div>}
      {data && domains.length === 0 && <div className="tr mut" style={{ ["--cols" as string]: "1fr" }}>カスタムドメインはありません。</div>}
      {domains.map((domain) => (
        <div key={domain.domainId} style={{ padding: "12px 18px", borderTop: "1px solid var(--line)", display: "flex", flexDirection: "column", gap: 10 }}>
          <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
            <a className="mono grow" style={{ fontWeight: 500, overflowWrap: "anywhere", color: domain.status === "verified" ? "var(--accent)" : "inherit" }} href={domain.status === "verified" ? `https://${domain.hostname}` : undefined} target="_blank" rel="noreferrer">{domain.hostname}</a>
            <span className={`badge ${domain.status === "verified" ? "b-ok" : "b-warn"}`}><i />{domain.status === "verified" ? "確認済み" : "確認待ち"}</span>
            {domain.status === "pending" && <button className="btn sm primary" type="button" disabled={locked || busy !== ""} onClick={() => void verify(domain)}>{busy === domain.domainId ? "確認中…" : "確認する"}</button>}
            <button className="btn sm danger" type="button" disabled={busy !== ""} onClick={() => void remove(domain)}>外す</button>
          </div>
          {domain.status === "pending" && reasons[domain.domainId] && <div style={{ color: "var(--bad)", fontSize: 12.5 }}>{reasons[domain.domainId]}</div>}
          {domain.status === "pending" && <DnsInstructions domain={domain} />}
          {domain.status === "verified" && <div className="faint" style={{ fontSize: 12.5 }}>このドメインを、DNS で、この ingress のホスト名({domain.challenge.cnameValue})に向けると、開けます。</div>}
        </div>
      ))}
      <div className="row" style={{ padding: "12px 18px 14px", borderTop: "1px solid var(--line)", gap: 8 }}>
        <input className="inp mono grow" value={hostname} onChange={(event) => setHostname(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") void add() }}
          placeholder="app.example.com" aria-label="追加するドメイン" disabled={locked || domains.length >= MAX_DOMAINS} />
        <button className="btn sm primary" type="button" disabled={locked || busy !== "" || !hostname.trim() || domains.length >= MAX_DOMAINS} onClick={() => void add()}>追加</button>
      </div>
    </div>
  )
}
