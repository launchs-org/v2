"use client"

import type { ReactNode } from "react"

/** 絵の中の色(GitHub の画面に、雰囲気を寄せたもの)。 */
const COLOR = { bg: "#F6F8FA", line: "#D0D7DE", text: "#24292F", mut: "#6E7781", green: "#1F883D", white: "#FFFFFF", mark: "#E8590C", markBg: "rgba(232,89,12,.12)" }

/** 絵の枠(ブラウザの窓)。中身は、座標 (0,28)〜(480,170) に描く。 */
function Frame({ url, children }: { url: string; children: ReactNode }) {
  return (
    <svg viewBox="0 0 480 176" role="img" style={{ width: "100%", height: "auto", borderRadius: 8, border: `1px solid ${COLOR.line}`, background: COLOR.white, display: "block" }} fontFamily="'Noto Sans JP','Roboto',sans-serif">
      <rect width="480" height="28" fill={COLOR.bg} />
      <circle cx="14" cy="14" r="4" fill="#FF5F57" /><circle cx="28" cy="14" r="4" fill="#FEBC2E" /><circle cx="42" cy="14" r="4" fill="#28C840" />
      <rect x="62" y="6" width="320" height="16" rx="8" fill={COLOR.white} stroke={COLOR.line} />
      <text x="74" y="18" fontSize="9.5" fill={COLOR.mut}>{url}</text>
      {children}
    </svg>
  )
}

/** 「ここを押す」の丸と、番号と、ひとこと。 */
function Mark({ x, y, width, height, label, labelX, labelY, anchor = "start" }: { x: number; y: number; width: number; height: number; label: string; labelX: number; labelY: number; anchor?: "start" | "end" | "middle" }) {
  return (
    <g>
      <rect x={x} y={y} width={width} height={height} rx="6" fill={COLOR.markBg} stroke={COLOR.mark} strokeWidth="2" />
      <text x={labelX} y={labelY} fontSize="11" fontWeight="700" fill={COLOR.mark} textAnchor={anchor}>{label}</text>
    </g>
  )
}

/** 手順 1: Add file → Create new file。 */
function PictureAddFile() {
  return (
    <Frame url="github.com/あなた/リポジトリ">
      <text x="20" y="54" fontSize="14" fontWeight="700" fill={COLOR.text}>あなた / リポジトリ</text>
      <rect x="304" y="40" width="72" height="24" rx="6" fill={COLOR.white} stroke={COLOR.line} /><text x="340" y="56" textAnchor="middle" fontSize="11" fill={COLOR.text}>Add file ▾</text>
      <rect x="384" y="40" width="80" height="24" rx="6" fill={COLOR.green} /><text x="424" y="56" textAnchor="middle" fontSize="11" fill="#fff">&lt;&gt; Code ▾</text>
      <rect x="290" y="68" width="150" height="62" rx="8" fill={COLOR.white} stroke={COLOR.line} />
      <text x="302" y="88" fontSize="11" fill={COLOR.text}>Create new file</text>
      <line x1="290" y1="100" x2="440" y2="100" stroke={COLOR.line} />
      <text x="302" y="118" fontSize="11" fill={COLOR.mut}>Upload files</text>
      <rect x="20" y="82" width="200" height="8" rx="4" fill={COLOR.bg} /><rect x="20" y="100" width="260" height="8" rx="4" fill={COLOR.bg} /><rect x="20" y="118" width="160" height="8" rx="4" fill={COLOR.bg} />
      <Mark x={294} y={72} width={142} height={24} label="② Create new file" labelX={270} labelY={88} anchor="end" />
      <Mark x={300} y={36} width={80} height={32} label="① Add file" labelX={290} labelY={30} anchor="end" />
    </Frame>
  )
}

/** 手順 2: ファイル名の入力。 */
function PictureFileName() {
  return (
    <Frame url="github.com/あなた/リポジトリ/new/main">
      <text x="20" y="56" fontSize="11" fill={COLOR.mut}>リポジトリ /</text>
      <rect x="100" y="42" width="288" height="22" rx="5" fill={COLOR.white} stroke={COLOR.line} />
      <text x="108" y="57" fontSize="11" fill={COLOR.text} fontFamily="'Roboto Mono',monospace">.github/workflows/launchs-deploy.yml</text>
      <Mark x={94} y={36} width={300} height={34} label="ファイル名を、ここに入力" labelX={96} labelY={88} />
      <text x="96" y="106" fontSize="10.5" fill={COLOR.mut}>「/」を打つと、自動で、フォルダになります</text>
      <rect x="20" y="118" width="440" height="44" rx="6" fill={COLOR.bg} stroke={COLOR.line} />
    </Frame>
  )
}

/** 手順 3: 中身を貼る。 */
function PictureContent() {
  return (
    <Frame url="github.com/あなた/リポジトリ/new/main">
      <rect x="20" y="40" width="440" height="116" rx="6" fill={COLOR.white} stroke={COLOR.line} />
      <rect x="20" y="40" width="440" height="22" rx="6" fill={COLOR.bg} /><text x="30" y="55" fontSize="10.5" fill={COLOR.mut}>Edit new file</text>
      {[0, 1, 2, 3, 4].map((row) => <rect key={row} x="34" y={72 + row * 15} width={[220, 300, 180, 260, 140][row]} height="7" rx="3" fill="#CFD8E3" />)}
      <Mark x={24} y={64} width={432} height={88} label="コピーした YAML を、全部、貼る" labelX={240} labelY={170} anchor="middle" />
    </Frame>
  )
}

/** 手順 4: Commit changes。 */
function PictureCommit() {
  return (
    <Frame url="github.com/あなた/リポジトリ/new/main">
      <rect x="316" y="38" width="144" height="24" rx="6" fill={COLOR.green} /><text x="388" y="54" textAnchor="middle" fontSize="11" fill="#fff">Commit changes...</text>
      <Mark x={312} y={34} width={152} height={32} label="① 押す" labelX={298} labelY={54} anchor="end" />
      <rect x="120" y="76" width="340" height="94" rx="8" fill={COLOR.white} stroke={COLOR.line} />
      <text x="134" y="94" fontSize="11" fontWeight="700" fill={COLOR.text}>Commit changes</text>
      <circle cx="140" cy="112" r="5" fill="none" stroke={COLOR.green} strokeWidth="2" /><circle cx="140" cy="112" r="2.5" fill={COLOR.green} />
      <text x="152" y="116" fontSize="10.5" fill={COLOR.text}>Commit directly to the main branch</text>
      <circle cx="140" cy="130" r="5" fill="none" stroke={COLOR.line} strokeWidth="2" />
      <text x="152" y="134" fontSize="10.5" fill={COLOR.mut}>Create a new branch …</text>
      <rect x="346" y="142" width="104" height="22" rx="6" fill={COLOR.green} /><text x="398" y="157" textAnchor="middle" fontSize="10.5" fill="#fff">Commit changes</text>
      <Mark x={130} y={100} width={230} height={22} label="② ここを選ぶ" labelX={128} labelY={98} anchor="end" />
      <Mark x={342} y={138} width={112} height={30} label="③ 押す" labelX={338} labelY={158} anchor="end" />
    </Frame>
  )
}

/** 手順 5: Actions タブで、動いたことを確かめる。 */
function PictureActions() {
  return (
    <Frame url="github.com/あなた/リポジトリ/actions">
      {["Code", "Issues", "Pull requests", "Actions", "Settings"].map((tab, index) => <text key={tab} x={[50, 120, 215, 330, 420][index]} y="56" textAnchor="middle" fontSize="11" fill={tab === "Actions" ? COLOR.text : COLOR.mut} fontWeight={tab === "Actions" ? 700 : 400}>{tab}</text>)}
      <line x1="0" y1="66" x2="480" y2="66" stroke={COLOR.line} />
      <Mark x={290} y={40} width={80} height={24} label="① Actions を開く" labelX={330} labelY={34} anchor="middle" />
      <rect x="20" y="82" width="440" height="30" rx="6" fill={COLOR.white} stroke={COLOR.line} />
      <circle cx="38" cy="97" r="7" fill={COLOR.green} /><text x="34" y="101" fontSize="10" fill="#fff">✓</text>
      <text x="54" y="101" fontSize="11" fill={COLOR.text}>Deploy to launchs</text>
      <text x="400" y="101" fontSize="10" fill={COLOR.mut}>main</text>
      <Mark x={24} y={86} width={432} height={22} label="緑のチェック ✓ が付けば、成功です" labelX={240} labelY={134} anchor="middle" />
      <text x="240" y="152" fontSize="10.5" fill={COLOR.mut} textAnchor="middle">赤い × のときは、その行を開いて、エラーの内容を確かめます</text>
    </Frame>
  )
}

/**
 * 「作った YAML を、GitHub のリポジトリに置く」手順を、絵つきで見せる(コマンドは使わない。GitHub の画面だけで行う)。
 * 絵は、実際の画面ではなく、操作する場所を丸で囲んだ、イメージ図。
 *
 * @param props.branch YAML を置く(push する)ブランチ。
 */
export function GithubPushGuide({ branch }: { branch: string }) {
  const target = branch.trim() || "main"
  const steps: { title: ReactNode; body: ReactNode; picture: ReactNode }[] = [
    { title: "リポジトリの「Add file」→「Create new file」", body: <>GitHub で、YAML を置きたいリポジトリを開きます。右上の <b>Add file</b> を押して、<b>Create new file</b> を選びます。</>, picture: <PictureAddFile /> },
    { title: "ファイル名を入力する", body: <>名前の欄に、次の文字を入力します。<code className="mono">/</code> を打つと、自動でフォルダになります。<div className="row" style={{ marginTop: 6 }}><span className="mono" style={{ background: "var(--panel2)", padding: "2px 8px", borderRadius: 4 }}>.github/workflows/launchs-deploy.yml</span></div></>, picture: <PictureFileName /> },
    { title: "YAML を貼る", body: <>上の「YAML」の<b>コピー</b>ボタンで、中身を全部コピーして、大きな入力欄に貼ります。(ダウンロードしたファイルを、テキストエディタで開いて、全部コピーしても同じです)</>, picture: <PictureContent /> },
    { title: <>「Commit changes」で、<span className="mono">{target}</span> に保存する</>, body: <>右上の <b>Commit changes...</b> を押します。出てきた画面で、<b>Commit directly to the <span className="mono">{target}</span> branch</b> を選び、<b>Commit changes</b> を押します。これで、YAML が、リポジトリに置かれます。<br /><span className="faint" style={{ fontSize: 12.5 }}>YAML の「対象のブランチ」と、ここで保存するブランチを、同じにします。違うブランチに保存すると、動きません。</span></>, picture: <PictureCommit /> },
    { title: "「Actions」タブで、動いたことを確かめる", body: <>リポジトリの <b>Actions</b> タブを開きます。保存した直後に、<b>Deploy to launchs</b> が動き始めます。<b>緑のチェック</b>が付けば、成功です(数分かかります)。</>, picture: <PictureActions /> },
  ]
  return (
    <div className="col" style={{ gap: 14 }}>
      <div className="mut" style={{ fontSize: 13.5 }}>作った YAML を、GitHub のリポジトリに置きます。<b>GitHub の画面だけ</b>で、できます(コマンドは要りません)。置いた直後から、自動デプロイが動き始めます。</div>
      {steps.map((step, index) => (
        <div key={index} className="col" style={{ gap: 8, padding: 12, border: "1px solid var(--line)", borderRadius: 8 }}>
          <div className="row" style={{ gap: 10, alignItems: "flex-start" }}>
            <span style={{ display: "grid", placeItems: "center", width: 24, height: 24, borderRadius: "50%", background: "var(--accent)", color: "#fff", fontSize: 12, fontWeight: 700, flex: "none" }}>{index + 1}</span>
            <div className="col" style={{ gap: 4 }}><b style={{ fontSize: 14 }}>{step.title}</b><div className="mut" style={{ fontSize: 13.5 }}>{step.body}</div></div>
          </div>
          {step.picture}
        </div>
      ))}
      <div className="faint" style={{ fontSize: 12 }}>絵は、操作する場所を示す、イメージ図です。GitHub の実際の画面とは、細かい見た目が違うことがあります。</div>
      <details>
        <summary className="mut" style={{ cursor: "pointer", fontSize: 13 }}>Git に慣れている方: コマンドで置く場合</summary>
        <div className="codebox" style={{ marginTop: 8 }}>{`mkdir -p .github/workflows\nmv ~/Downloads/launchs-deploy.yml .github/workflows/\ngit add .github/workflows/launchs-deploy.yml\ngit commit -m "launchs に自動デプロイする"\ngit push origin ${target}`}</div>
      </details>
    </div>
  )
}
