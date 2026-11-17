// ingress の状態の表示と、path ルールの入力の検査・選択肢の組み立て。
import type { DeployService, IngressRule } from "../api/types";
import type { StatusTone } from "./project_name";

/** ルールの path の最大の長さ(文字数)。 */
const MAX_PATH_LENGTH = 200;

/** ルールを選ぶときの、サービスのポート1つ分の選択肢。 */
export interface PortChoice {
  /** select の値(「serviceId:port」)。 */
  value: string;
  /** 表示する文(「deploy のイメージ / ポート名(番号)」)。 */
  label: string;
  serviceId: string;
  port: number;
}

/**
 * path の入力を検査する。
 *
 * @param path 入力された path。
 * @returns 問題の説明。合っていれば null。
 */
export function validateRulePath(path: string): string | null {
  // 「/」で始まらないものは断る。
  if (!path.startsWith("/")) {
    return "path は、「/」で始めてください。";
  }
  // 長すぎるものは断る。
  if (path.length > MAX_PATH_LENGTH) {
    return `path は、${MAX_PATH_LENGTH}文字以内にしてください。`;
  }
  // 空白を含むものは、path として成り立たない。
  if (/\s/.test(path)) {
    return "path に、空白は使えません。";
  }
  return null;
}

/**
 * サービスの一覧から、ルールの向き先に選べるポートの選択肢を作る。
 * 選べるのは、サービスが active で、protocol が tcp のポートだけ(ingress は HTTP のみ)。
 *
 * @param entries deploy のイメージと、そのサービス(無ければ null)の組。
 * @returns 選択肢(入力の順)。
 */
export function buildPortChoices(entries: { imageRef: string; service: DeployService | null | undefined }[]): PortChoice[] {
  const choices: PortChoice[] = [];
  // deploy ごとに、サービスのポートを見ていく。
  for (const entry of entries) {
    // サービスが無い、または、active でないものは、向き先にできない。
    if (entry.service === null || entry.service === undefined || entry.service.status !== "active") {
      continue;
    }
    // tcp のポートだけを、選択肢にする。
    for (const port of entry.service.ports) {
      if (port.protocol === "tcp") {
        choices.push({ value: `${entry.service.serviceId}:${port.port}`, label: `${entry.imageRef} / ${port.name}(${port.port})`, serviceId: entry.service.serviceId, port: port.port });
      }
    }
  }
  return choices;
}

/**
 * ルールの向き先を、select の値にする。
 *
 * @param rule ルール。
 * @returns 「serviceId:port」。
 */
export function ruleChoiceValue(rule: IngressRule): string {
  return `${rule.serviceId}:${rule.port}`;
}

/**
 * ingress の状態を、画面に出す言葉にする。
 *
 * @param status ingress の状態。
 * @returns 表示する言葉(知らない値は、そのまま)。
 */
export function ingressStatusLabel(status: string): string {
  // 仕様の4つの状態を、日本語にする。
  switch (status) {
    case "active":
      return "稼働中";
    case "applying":
      return "反映中";
    case "failed":
      return "反映に失敗";
    case "deleting":
      return "削除中";
    default:
      return status;
  }
}

/**
 * ingress の状態を、バッジの色にする。
 *
 * @param status ingress の状態。
 * @returns 色の種類。
 */
export function ingressStatusTone(status: string): StatusTone {
  // 進行中は黄、失敗は赤、稼働中は緑。
  switch (status) {
    case "active":
      return "success";
    case "applying":
    case "deleting":
      return "warning";
    case "failed":
      return "destructive";
    default:
      return "default";
  }
}
