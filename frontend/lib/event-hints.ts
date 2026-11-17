// k8s の Event の「理由(reason)」と本文から、初心者にも分かる、短い説明と、次にすることを作る。純粋関数。

/** 説明。 */
export type EventHint = {
  /** 何が起きたか(短い日本語)。 */
  summary: string
  /** 次に、確かめること(あれば)。 */
  action?: string
}

/** 理由ごとの説明(k8s の標準の理由)。 */
const HINTS: Record<string, EventHint> = {
  Scheduled: { summary: "Pod を、ノードに配置しました。" },
  Pulling: { summary: "イメージを、ダウンロードしています。" },
  Pulled: { summary: "イメージを、取得しました。" },
  Created: { summary: "コンテナを、作りました。" },
  Started: { summary: "コンテナを、起動しました。" },
  Killing: { summary: "コンテナを、止めています(更新や削除のため)。" },
  SuccessfulCreate: { summary: "Pod を、作りました。" },
  SuccessfulDelete: { summary: "Pod を、削除しました。" },
  ScalingReplicaSet: { summary: "台数(レプリカ数)を、変えています。" },
  BackOff: { summary: "失敗が続いているため、少し待ってから、やり直しています。", action: "ほかの Warning(Failed など)に、原因が出ています。" },
  Failed: { summary: "コンテナの起動、またはイメージの取得に失敗しました。", action: "メッセージを確認してください。イメージ名とタグが正しいか、確かめます。" },
  ErrImagePull: { summary: "イメージを取得できません。", action: "イメージ名とタグが正しいか、確かめてください。" },
  ImagePullBackOff: { summary: "イメージを取得できないため、待ってから、やり直しています。", action: "イメージ名とタグが正しいか、確かめてください。非公開のイメージは、取得に必要な認証が要ります。" },
  FailedScheduling: { summary: "Pod を、配置できるノードが、ありません。", action: "クラスターの空きが足りない可能性があります。スペックを小さくするか、台数を減らしてください。" },
  Unhealthy: { summary: "ヘルスチェックに失敗しています。", action: "アプリが、起動しているか、ポートが合っているか、確かめてください。" },
  FailedMount: { summary: "ボリュームを、マウントできません。", action: "ボリュームの状態と、マウント先のパスを、確かめてください。" },
  FailedAttachVolume: { summary: "ボリュームを、ノードにつなげません。", action: "ボリュームが、ほかの Pod に使われていないか、確かめてください。" },
  OOMKilled: { summary: "メモリが足りず、コンテナが止められました。", action: "スペックを大きくしてください。" },
  Evicted: { summary: "ノードの資源が足りず、Pod が追い出されました。" },
  FailedCreate: { summary: "Pod を作れませんでした。", action: "メッセージを確認してください。上限(クォータ)や、権限の可能性があります。" },
  ProvisioningFailed: { summary: "ボリュームを、用意できませんでした。", action: "ストレージの空きと、設定を確かめてください。" },
}

/**
 * Event の説明を返す。メッセージに、よくある原因が含まれていれば、より具体的な説明にする。
 *
 * @param reason k8s の Event の理由。
 * @param message k8s の Event の本文。
 * @returns 説明。知らない理由なら null。
 */
export function eventHint(reason: string, message: string): EventHint | null {
  // 本文に、メモリ不足やイメージの未発見が出ていれば、そちらの説明を優先する。
  const lower = message.toLowerCase()
  if (lower.includes("oomkilled")) return HINTS.OOMKilled ?? null
  if (lower.includes("not found") && (lower.includes("manifest") || lower.includes("pull"))) return { summary: "指定したイメージ(またはタグ)が、見つかりません。", action: "イメージ名とタグの綴りを、確かめてください。" }
  if (lower.includes("insufficient")) return { summary: "ノードの資源(CPU かメモリ)が、足りません。", action: "スペックを小さくするか、台数を減らしてください。" }
  return HINTS[reason] ?? null
}

/**
 * 時刻を、「3 分前」のような、相対の表現にする。読めない値は、空文字を返す。
 *
 * @param iso API が返した日時。
 * @param now いまの時刻(ミリ秒)。テストと、定期的な更新のために、引数にしている。
 * @returns 相対の表現。
 */
export function relativeTime(iso: string, now: number): string {
  // 読めない値は、空にする。
  const time = new Date(iso).getTime()
  if (Number.isNaN(time)) return ""
  // 経過の秒数で、単位を選ぶ。未来(時計のずれ)は、「たった今」にする。
  const seconds = Math.max(0, Math.round((now - time) / 1000))
  if (seconds < 10) return "たった今"
  if (seconds < 60) return `${seconds} 秒前`
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分前`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 時間前`
  return `${Math.floor(seconds / 86400)} 日前`
}
