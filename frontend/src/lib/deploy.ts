// deploy の状態の表示と、入力の検査。
import type { DeployStatus } from "../api/types";
import type { StatusTone } from "./project_name";

/** 選べるスペック(小さい順)。サーバーは、この3つだけを受け付ける。 */
export const DEPLOY_SPECS = ["small", "medium", "large"] as const;

/** スケールで選べる、レプリカ数の上限(仕様: 0〜5。サーバーの上限は、GET /quota の maxReplicas)。 */
export const DEFAULT_MAX_REPLICAS = 5;

/**
 * deploy の状態を、画面に出す言葉にする。
 *
 * @param status deploy の状態。
 * @returns 表示する言葉(知らない値は、そのまま)。
 */
export function deployStatusLabel(status: string): string {
  // 仕様の状態を、日本語にする。
  switch (status) {
    case "pending":
      return "起動中";
    case "active":
      return "稼働中";
    case "applying":
      return "反映中";
    case "failed":
      return "失敗";
    case "scaled_zero":
      return "停止(0台)";
    case "deleting":
      return "削除中";
    case "delete_failed":
      return "削除に失敗";
    default:
      return status;
  }
}

/**
 * deploy の状態を、バッジの色にする。
 *
 * @param status deploy の状態。
 * @returns 色の種類(稼働中は緑、進行中は黄、失敗は赤、停止などは標準)。
 */
export function deployStatusTone(status: string): StatusTone {
  // 進行中は黄、失敗は赤、稼働中は緑。
  switch (status as DeployStatus) {
    case "active":
      return "success";
    case "pending":
    case "applying":
    case "deleting":
      return "warning";
    case "failed":
    case "delete_failed":
      return "destructive";
    default:
      return "default";
  }
}

/**
 * 状態が、変わっていく途中か(途中なら、画面が、確かめ直し続ける)。
 *
 * @param status deploy の状態。
 * @returns pending、applying、deleting なら true。
 */
export function isDeployTransitioning(status: string): boolean {
  return status === "pending" || status === "applying" || status === "deleting";
}

/**
 * deploy を作るときの、イメージの入力を検査する。
 *
 * @param imageRef 入力されたイメージ(前後の空白は、呼び出し側で除く)。
 * @returns 問題の説明。合っていれば null。
 */
export function validateImageRef(imageRef: string): string | null {
  // 空は受け付けない。
  if (imageRef.length === 0) {
    return "イメージを入力してください。";
  }
  // 空白を含むものは、イメージの参照として成り立たない。
  if (/\s/.test(imageRef)) {
    return "イメージに、空白は使えません。";
  }
  return null;
}

/**
 * 日時(JST の ISO 8601)を、画面に出す形にする。
 *
 * @param iso API が返した日時。
 * @returns 「2026/09/16 09:00」の形。読めない値は、そのまま。
 */
export function formatDateTime(iso: string): string {
  // 読めない値は、そのまま見せる。
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return iso;
  }
  return date.toLocaleString("ja-JP", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", timeZone: "Asia/Tokyo" });
}
