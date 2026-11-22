"use client"

import dynamic from "next/dynamic"
import { loader } from "@monaco-editor/react"

// Monaco Editor は、CDN ではなく、自分のサーバー(public/monaco/vs。scripts/copy-monaco.mjs が作る)から読む。
loader.config({ paths: { vs: "/monaco/vs" }, "vs/nls": { availableLanguages: { "*": "ja" } } })

/** Monaco Editor(ブラウザでだけ読み込む。重いので、必要になったときに読む)。 */
const Editor = dynamic(() => import("@monaco-editor/react").then((module) => module.default), { ssr: false })

/**
 * YAML を、色分けと行番号つきで見せる(読み取り専用の Monaco Editor)。読み込み中や、読み込めないときは、ふつうの文字で見せる。
 *
 * @param props.value 表示する YAML。
 * @param props.height 高さ(ピクセル)。
 */
export function YamlViewer({ value, height = 320 }: { value: string; height?: number }) {
  return (
    <div style={{ border: "1px solid var(--line)", borderRadius: 8, overflow: "hidden", height }}>
      <Editor
        height="100%"
        language="yaml"
        value={value}
        theme="vs"
        loading={<pre className="codebox" style={{ margin: 0, height: "100%", border: 0, borderRadius: 0, overflow: "auto" }}>{value}</pre>}
        options={{
          readOnly: true,
          domReadOnly: true,
          minimap: { enabled: false },
          scrollBeyondLastLine: false,
          fontSize: 12.5,
          lineHeight: 20,
          lineNumbersMinChars: 3,
          renderLineHighlight: "none",
          wordWrap: "off",
          padding: { top: 10, bottom: 10 },
          scrollbar: { alwaysConsumeMouseWheel: false },
          contextmenu: false,
          folding: true,
        }}
      />
    </div>
  )
}
