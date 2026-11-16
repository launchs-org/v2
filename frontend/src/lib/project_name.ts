// プロジェクトの名前の検査(仕様: 英数字とハイフンのみ、1〜64文字。サーバーも、同じ規則で確かめる)。

/** 名前の最大の長さ。 */
export const MAX_PROJECT_NAME_LENGTH = 64;

/**
 * プロジェクトの名前が、規則に合うかを調べる。
 *
 * @param name 入力された名前(前後の空白は、呼び出し側で除く)。
 * @returns 問題の説明。合っていれば null。
 */
export function validateProjectName(name: string): string | null {
  // 空は、受け付けない。
  if (name.length === 0) {
    return "名前を入力してください。";
  }
  // 長さを確かめる。
  if (name.length > MAX_PROJECT_NAME_LENGTH) {
    return `名前は、${MAX_PROJECT_NAME_LENGTH}文字以内にしてください。`;
  }
  // 英数字とハイフンだけを許す。
  if (!/^[A-Za-z0-9-]+$/.test(name)) {
    return "名前には、英数字とハイフンだけを使えます。";
  }
  return null;
}

/**
 * 状態を、画面に出す言葉にする。
 *
 * @param status プロジェクトの状態。
 * @returns 表示する言葉。
 */
export function projectStatusLabel(status: string): string {
  // 仕様の3つの状態。知らない値は、そのまま見せる。
  switch (status) {
    case "active":
      return "稼働中";
    case "deleting":
      return "削除中";
    case "delete_failed":
      return "削除に失敗";
    default:
      return status;
  }
}

/** バッジの色の種類(components/ui/badge.tsx の variant)。 */
export type StatusTone = "success" | "warning" | "destructive" | "default";

/**
 * 状態を、バッジの色の種類にする。
 *
 * @param status プロジェクトの状態。
 * @returns 色の種類(稼働中は緑、削除中は黄、削除に失敗は赤、知らない値は標準)。
 */
export function projectStatusTone(status: string): StatusTone {
  // 仕様の3つの状態を、色に対応させる。
  switch (status) {
    case "active":
      return "success";
    case "deleting":
      return "warning";
    case "delete_failed":
      return "destructive";
    default:
      return "default";
  }
}
