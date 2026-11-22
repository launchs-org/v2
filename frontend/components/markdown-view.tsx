"use client"

import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"

/**
 * Markdown の文書を、画面の文字の大きさと色に合わせて見せる(テンプレートの使い方など)。
 * HTML は解釈しない(書かれた HTML タグは、そのまま文字として出す)。リンクは、別のタブで開く。
 *
 * @param props.source Markdown の文字列。
 */
export function MarkdownView({ source }: { source: string }) {
  return (
    <div className="md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          // リンクは、別のタブで、元の画面を残して開く(参照元を渡さない)。
          a: ({ href, children }) => <a href={href} target="_blank" rel="noopener noreferrer">{children}</a>,
        }}
      >
        {source}
      </ReactMarkdown>
    </div>
  )
}
