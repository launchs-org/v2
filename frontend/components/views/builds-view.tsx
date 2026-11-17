"use client"

import type { ProjectViewProps } from "@/lib/nav"
import { EmptyState } from "@/components/ui-kit"

/**
 * ビルドの画面。backend のビルド機能(/upload、/build)が未実装なので、案内だけを出す。
 *
 * @param _props 共通の引数(使わない)。
 */
export function BuildsView(_props: ProjectViewProps) {
  return (
    <>
      <div><h1 className="h1 disp">ビルド</h1></div>
      <EmptyState title="ビルド機能は、まだ使えません" description="いまは、ビルド済みのイメージを指定して、デプロイを作ります。" />
    </>
  )
}
