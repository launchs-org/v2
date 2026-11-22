"use client"

import { useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import type { ApiTokenCreateResponse, ApiTokenListResponse } from "@/lib/types"
import { buildGithubActionsWorkflow, ghcrPackageName, normalizeFolder, type WorkflowBuildMethod } from "@/lib/github-actions-workflow"
import { CopyButton, tokenState } from "@/components/api-token-section"
import { GithubPushGuide } from "@/components/github-push-guide"
import { Modal } from "@/components/ui-kit"
import { YamlViewer } from "@/components/yaml-viewer"

/** 手順の見出し(上のバーに出す)。 */
const STEPS = ["YAML を作る", "GitHub に置く", "Secrets を登録", "ghcr を公開"] as const

/**
 * GitHub Actions で自動デプロイするための案内(3 ステップ)。
 *   1. ブランチ、ビルドの対象フォルダ、ビルドの方法(Railpack / Dockerfile)を決めて、YAML を作る(コピー、ダウンロード)。
 *   2. YAML を、GitHub のリポジトリに置く(絵つきの手順)。
 *   3. GitHub の Secrets(LAUNCHS_URL、LAUNCHS_TOKEN)を登録する。
 *   4. 最初の実行のあと、ghcr.io のパッケージを公開にする。
 *
 * @param props.deployId 対象の deploy の ID。
 * @param props.onClose 閉じるとき。
 */
export function GithubActionsGuide({ deployId, onClose }: { deployId: string; onClose: () => void }) {
  const [step, setStep] = useState(0)
  const [branch, setBranch] = useState("main")
  const [folder, setFolder] = useState(".")
  const [build, setBuild] = useState<WorkflowBuildMethod>("railpack")
  const packageName = ghcrPackageName(deployId)
  const yaml = buildGithubActionsWorkflow({ deployId, build, branch, folder })

  /** YAML を、ファイルとして保存する(.github/workflows/launchs-deploy.yml に置く)。 */
  function download() {
    const url = URL.createObjectURL(new Blob([yaml], { type: "text/yaml" }))
    const anchor = document.createElement("a")
    anchor.href = url
    anchor.download = "launchs-deploy.yml"
    anchor.click()
    URL.revokeObjectURL(url)
  }

  return (
    <Modal wide title="GitHub Actions で自動デプロイ" description="push するだけで、イメージを作り、この deploy を入れ替えます。4 つの手順で、設定できます。" onClose={onClose}
      footer={<>
        {step > 0 && <button className="btn" type="button" onClick={() => setStep(step - 1)}>← 戻る</button>}
        {step < STEPS.length - 1 ? <button className="btn primary" type="button" onClick={() => setStep(step + 1)}>次へ: {STEPS[step + 1]} →</button> : <button className="btn primary" type="button" onClick={onClose}>完了</button>}
      </>}>
      <div className="col" style={{ gap: 18 }}>
        <div className="stepbar">{STEPS.map((label, index) => <button key={label} type="button" className={`${step === index ? "on" : ""} ${index < step ? "done" : ""}`} onClick={() => setStep(index)}><span className="num">{index < step ? "✓" : index + 1}</span>{label}</button>)}</div>

        {step === 0 && (
          <div className="col" style={{ gap: 16 }}>
            <div className="col" style={{ gap: 8 }}>
              <span className="lbl" style={{ gap: 0 }}>ビルドの方法</span>
              <div className="seg"><button type="button" className={build === "railpack" ? "on" : ""} onClick={() => setBuild("railpack")}>Railpack(Dockerfile なし)</button><button type="button" className={build === "dockerfile" ? "on" : ""} onClick={() => setBuild("dockerfile")}>Dockerfile</button></div>
              <div className="faint" style={{ fontSize: 12.5 }}>{build === "railpack" ? "コードを見て、自動でイメージを作ります。Dockerfile は要りません(Node、Python、Go などに対応)。" : "指定したフォルダの Dockerfile で、イメージを作ります。"}</div>
            </div>
            <div className="row" style={{ gap: 12, flexWrap: "wrap" }}>
              <label className="lbl" style={{ flex: "1 1 200px" }}>対象のブランチ(push で動く)<input className="inp" value={branch} onChange={(event) => setBranch(event.target.value)} placeholder="main" /></label>
              <label className="lbl" style={{ flex: "1 1 200px" }}>ビルドするフォルダ<input className="inp mono" value={folder} onChange={(event) => setFolder(event.target.value)} placeholder="." /></label>
            </div>
            <div className="faint" style={{ fontSize: 12.5, marginTop: -8 }}>フォルダは、リポジトリの直下からの場所です(直下なら <span className="mono">.</span>、例: <span className="mono">apps/web</span>)。いまは <span className="mono">{normalizeFolder(folder)}</span> を、ビルドします。</div>
            <div className="col" style={{ gap: 6 }}>
              <div className="row"><b style={{ fontSize: 13 }}>launchs-deploy.yml</b><span className="grow" /><CopyButton text={yaml} /><button className="btn sm primary" type="button" onClick={download}>ダウンロード</button></div>
              <YamlViewer value={yaml} height={340} />
              <div className="faint" style={{ fontSize: 12.5 }}>この YAML は、どのリポジトリに置いても動きます。イメージの名前は、YAML の中で、そのリポジトリの持ち主から作ります(<span className="mono">ghcr.io/&lt;持ち主&gt;/{packageName}</span>)。deploy ごとに違う名前なので、他のものと重なりません。</div>
            </div>
          </div>
        )}

        {step === 1 && <GithubPushGuide branch={branch} />}

        {step === 2 && <SecretsStep deployId={deployId} />}

        {step === 3 && (
          <div className="col" style={{ gap: 14 }}>
            <div className="note" style={{ padding: "12px 14px", background: "var(--panel2)", borderRadius: 8, fontSize: 13.5 }}>
              <b>なぜ、公開にするのか</b><div className="mut">GitHub のパッケージ(ghcr.io のイメージ)は、最初は<b>非公開</b>です。launchs は、非公開のイメージを、まだ取得できません。最初の実行でパッケージができたあと、<b>1 度だけ</b>、公開(Public)にしてください。</div>
            </div>
            <ol style={{ margin: 0, paddingLeft: 20, display: "flex", flexDirection: "column", gap: 12, fontSize: 14 }}>
              <li>push した GitHub Actions が、<b>成功する</b>のを待ちます(リポジトリの <b>Actions</b> タブ)。</li>
              <li>GitHub の、自分(または Organization)のページを開き、<b>Packages</b> タブを選びます。</li>
              <li>パッケージ <span className="row" style={{ display: "inline-flex", gap: 6, verticalAlign: "middle" }}><span className="mono" style={{ background: "var(--panel2)", padding: "1px 6px", borderRadius: 4 }}>{packageName}</span><CopyButton text={packageName} /></span> を開きます。</li>
              <li>右側の <b>Package settings</b> を開きます。</li>
              <li>いちばん下の <b>Danger Zone → Change visibility</b> を押し、<b>Public</b> を選びます。確認のために、パッケージの名前を入力して、確定します。</li>
              <li>公開にしたあと、deploy が、イメージの取得に失敗していたら、もう一度 push するか、webhook を呼びます。</li>
            </ol>
            <div className="faint" style={{ fontSize: 12.5 }}>公開にできないとき: Organization のパッケージは、Organization の設定(<b>Settings → Packages</b>)で、公開を許可している必要があります。</div>
          </div>
        )}
      </div>
    </Modal>
  )
}

/** ステップ 2: GitHub の Secrets に登録する値(LAUNCHS_URL、LAUNCHS_TOKEN)を、見やすく並べる。トークンは、ここで、発行・表示・コピーできる。 */
function SecretsStep({ deployId }: { deployId: string }) {
  const { data, mutate } = useSWR<ApiTokenListResponse>("/api/v1/api-tokens", authenticatedFetcher, { revalidateOnFocus: true })
  const [token, setToken] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState("")
  const mine = (data?.tokens ?? []).filter((item) => item.scope.length === 1 && item.scope[0] === "deploy:image" && item.deploys.includes(deployId) && tokenState(item) === "active")
  const origin = typeof window === "undefined" ? "" : window.location.origin

  /** この deploy 用のトークンの本体を取り出す(発行済みなら、作り直して表示。無ければ、発行する)。 */
  async function loadToken() {
    setBusy(true)
    setMessage("")
    try {
      if (mine.length > 0) {
        const result = await api.get<{ token: string }>(`/api/v1/api-tokens/${encodeURIComponent(mine[0].tokenId)}/reveal`)
        setToken(result.token)
      } else {
        const result = await api.post<ApiTokenCreateResponse>("/api/v1/api-tokens", { name: `webhook ${deployId.slice(0, 15)}`, scope: ["deploy:image"], deploys: [deployId] })
        setToken(result.token)
        await mutate()
      }
    } catch (loadError) {
      setMessage(loadError instanceof Error ? loadError.message : "トークンを取得できませんでした")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="col" style={{ gap: 14 }}>
      <div className="mut" style={{ fontSize: 13.5 }}>GitHub に、次の 2 つの <b>Secrets</b> を登録します。トークンは、パスワードと同じ秘密なので、YAML ではなく、Secrets に入れます。</div>
      <div className="col" style={{ gap: 8 }}>
        <div className="secretrow"><span className="nm">LAUNCHS_URL</span><span className="vl">{origin}</span><CopyButton text={origin} label="値をコピー" /></div>
        <div className="secretrow">
          <span className="nm">LAUNCHS_TOKEN</span>
          <span className="vl">{token ?? (mine.length > 0 ? "発行済み(「トークンを表示」で、取り出せます)" : "まだ発行していません")}</span>
          {token ? <CopyButton text={token} label="値をコピー" /> : <button className="btn sm primary" type="button" disabled={busy} onClick={() => void loadToken()}>{mine.length > 0 ? "トークンを表示" : "トークンを発行"}</button>}
        </div>
      </div>
      {message && <div className="err" role="alert">{message}</div>}
      <div className="col" style={{ gap: 8 }}>
        <b style={{ fontSize: 13 }}>GitHub での登録のしかた</b>
        <ol style={{ margin: 0, paddingLeft: 20, display: "flex", flexDirection: "column", gap: 8, fontSize: 13.5 }}>
          <li>GitHub で、対象のリポジトリを開きます。</li>
          <li><b>Settings</b> → 左のメニューの <b>Secrets and variables</b> → <b>Actions</b> を選びます。</li>
          <li><b>New repository secret</b> を押します。</li>
          <li><b>Name</b> に <span className="mono">LAUNCHS_URL</span>、<b>Secret</b> に上の値を貼って、<b>Add secret</b> を押します。</li>
          <li>もう一度 <b>New repository secret</b> を押して、<span className="mono">LAUNCHS_TOKEN</span> も、同じように登録します。</li>
        </ol>
        <div className="codebox">{`# GitHub CLI(gh)が使えるなら、コマンドでも登録できます\ngh secret set LAUNCHS_URL --body "${origin}"\ngh secret set LAUNCHS_TOKEN`}</div>
        <div className="faint" style={{ fontSize: 12.5 }}><span className="mono">gh secret set LAUNCHS_TOKEN</span> は、値を聞かれるので、上のトークンを貼ります(履歴に残りません)。</div>
      </div>
    </div>
  )
}
