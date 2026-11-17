// 画面のキャッシュ(SWR)を、プロジェクト単位で、すぐに取り直す。
// 削除や反映の直後に呼ぶと、キャンバスのカード、反映待ち、一覧が、次の自動更新(最大 5 秒)を待たずに、新しい状態になる。
import { mutate } from "swr"

/**
 * プロジェクトに関する、全ての問い合わせを、すぐに取り直す。
 *
 * @param projectId プロジェクトの ID。
 * @returns 取り直しが、全て終わると解決する Promise。
 */
export function revalidateProject(projectId: string): Promise<unknown> {
  // プロジェクトのパスと、deploy の詳細(usePending が、まとめて持つ)を含むキーを、全て対象にする。
  // データを undefined にせず(キャッシュを残したまま)、取り直す。undefined にすると、取り直しが終わるまで、画面が空になる。
  return mutate((key) => typeof key === "string" && (key.includes(projectId) || key.startsWith("/api/v1/deploy/")))
}
