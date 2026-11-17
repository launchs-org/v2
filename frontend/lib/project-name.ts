/**
 * プロジェクト名の検査(英数字とハイフンだけ、1〜64文字。サーバーも同じ規則で確かめる)。
 *
 * @param name 前後の空白を除いた名前。
 * @returns 問題の説明。正しければ null。
 */
export function validateProjectName(name: string): string | null {
  // 空は受け付けない。
  if (name.length === 0) return "名前を入力してください。"
  // 長さを確かめる。
  if (name.length > 64) return "名前は、64文字以内にしてください。"
  // 使える文字を確かめる。
  if (!/^[A-Za-z0-9-]+$/.test(name)) return "名前には、英数字とハイフンだけを使えます。"
  return null
}
