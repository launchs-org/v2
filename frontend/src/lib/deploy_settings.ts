// deploy の設定(起動設定・マウント・サービスのポート)の、入力の検査と、画面の形との変換。
// 検査は、サーバーと同じ規則で、送る前に手元で行う(最終の判断は、サーバーが行う)。
import type { Mount, ServicePort } from "../api/types";

/** 環境変数の入力欄の1行。 */
export interface EnvRow {
  key: string;
  value: string;
}

/**
 * 環境変数のオブジェクトを、入力欄の行にする(キーの順に並べる)。
 *
 * @param env 環境変数。
 * @returns 入力欄の行。
 */
export function envToRows(env: Record<string, string>): EnvRow[] {
  // キーの順に並べて、行にする。
  return Object.keys(env).sort().map((key) => ({ key, value: env[key] ?? "" }));
}

/**
 * 入力欄の行を、環境変数のオブジェクトにする(キーが空の行は、無いものとして扱う)。
 *
 * @param rows 入力欄の行。
 * @returns 環境変数。
 */
export function rowsToEnv(rows: EnvRow[]): Record<string, string> {
  // 空のキーの行は捨てて、残りを、オブジェクトにする。
  const env: Record<string, string> = {};
  for (const row of rows) {
    if (row.key.trim() !== "") {
      env[row.key.trim()] = row.value;
    }
  }
  return env;
}

/**
 * 環境変数の入力欄を検査する。
 *
 * @param rows 入力欄の行。
 * @returns 問題の説明。合っていれば null。
 */
export function validateEnvRows(rows: EnvRow[]): string | null {
  // 使ったキーを覚えて、重複を見つける。
  const seen = new Set<string>();
  for (const row of rows) {
    const key = row.key.trim();
    // 値だけがある行は、キーが無いので、断る(全部空の行は、無視する)。
    if (key === "") {
      if (row.value !== "") {
        return "値だけの行があります。キーを入力するか、行を消してください。";
      }
      continue;
    }
    // 環境変数の名前に使えない文字を断る。
    if (/[\s=]/.test(key)) {
      return `キー「${key}」に、空白と = は使えません。`;
    }
    // 重複を断る。
    if (seen.has(key)) {
      return `キー「${key}」が、重複しています。`;
    }
    seen.add(key);
  }
  return null;
}

/**
 * 引数の入力(1行に1つ)を、配列にする。空の行は無視する。
 *
 * @param text 入力された文字列。
 * @returns 引数の配列。1つも無ければ null(イメージの既定の引数を使う)。
 */
export function parseArgs(text: string): string[] | null {
  // 行に分け、空の行を除く。
  const lines = text.split("\n").map((line) => line.replace(/\r$/, "")).filter((line) => line.trim() !== "");
  return lines.length === 0 ? null : lines;
}

/**
 * 引数の配列を、入力欄の文字列(1行に1つ)にする。
 *
 * @param args 引数。null なら空。
 * @returns 入力欄の文字列。
 */
export function argsToText(args: string[] | null): string {
  return (args ?? []).join("\n");
}

/** サービスのポートの name の規則(小文字の英数字とハイフン。先頭・末尾のハイフンと、連続するハイフンは不可)。 */
const PORT_NAME_PATTERN = /^[a-z0-9]+(-[a-z0-9]+)*$/;

/**
 * サービスのポートの一覧を検査する。
 *
 * @param ports ポートの一覧。
 * @returns 問題の説明。合っていれば null。
 */
export function validateServicePorts(ports: ServicePort[]): string | null {
  // 個数は、1〜20個。
  if (ports.length === 0) {
    return "ポートを、1つ以上入れてください。";
  }
  if (ports.length > 20) {
    return "ポートは、20個までです。";
  }
  const names = new Set<string>();
  const pairs = new Set<string>();
  for (const port of ports) {
    // name は、15文字まで、規則に合い、英字を1つ以上含む。
    if (port.name.length === 0 || port.name.length > 15 || !PORT_NAME_PATTERN.test(port.name) || !/[a-z]/.test(port.name)) {
      return `名前「${port.name}」は、小文字の英数字とハイフンで、15文字まで、英字を1つ以上含めてください(先頭と末尾のハイフン、連続するハイフンは不可)。`;
    }
    // port は、1〜65535 の整数。
    if (!Number.isInteger(port.port) || port.port < 1 || port.port > 65535) {
      return `ポート番号は、1〜65535 の整数にしてください(名前: ${port.name})。`;
    }
    // name の重複と、同じ番号とプロトコルの組の重複を断る。
    if (names.has(port.name)) {
      return `名前「${port.name}」が、重複しています。`;
    }
    names.add(port.name);
    const pair = `${port.protocol}/${port.port}`;
    if (pairs.has(pair)) {
      return `${port.protocol} の ${port.port} 番が、重複しています。`;
    }
    pairs.add(pair);
  }
  return null;
}

/**
 * マウントの一覧を検査する。
 *
 * @param mounts マウントの一覧。
 * @returns 問題の説明。合っていれば null。
 */
export function validateMounts(mounts: Mount[]): string | null {
  // 10件まで。
  if (mounts.length > 10) {
    return "マウントは、10件までです。";
  }
  const volumeIds = new Set<string>();
  const paths = new Set<string>();
  for (const mount of mounts) {
    // volume が選ばれていること。
    if (mount.volumeId === "") {
      return "volume を選んでください。";
    }
    // パスは、「/」で始まり、英数字と . _ - / だけの、200文字以内。「/」だけと「..」は不可。
    if (!/^\/[A-Za-z0-9._\-/]*$/.test(mount.mountPath) || mount.mountPath.length > 200 || mount.mountPath === "/" || mount.mountPath.includes("..")) {
      return `マウント先「${mount.mountPath}」は、「/」で始まる、英数字と . _ - / だけのパスにしてください(「/」だけと「..」は不可。200文字まで)。`;
    }
    // 同じ volume と、同じパスの重複を断る。
    if (volumeIds.has(mount.volumeId)) {
      return "同じ volume を、2回マウントできません。";
    }
    volumeIds.add(mount.volumeId);
    if (paths.has(mount.mountPath)) {
      return `マウント先「${mount.mountPath}」が、重複しています。`;
    }
    paths.add(mount.mountPath);
  }
  return null;
}
