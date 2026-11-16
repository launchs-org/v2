// API のエラー(共通ルール docs/endpoints/common.md の形式: {"error": {"code": "...", "message": "..."}})を表す。

/** API がエラーを返したとき(または、接続できなかったとき)に投げられるエラー。 */
export class ApiError extends Error {
  /** HTTP のステータスコード。接続できなかったときは 0。 */
  readonly status: number;

  /** エラーの種類(invalid_request、unauthorized、conflict など)。接続できなかったときは network_error。 */
  readonly code: string;

  /** 429 のときの、待つ秒数(Retry-After ヘッダ)。無ければ null。 */
  readonly retryAfterSeconds: number | null;

  /**
   * ApiError を作る。
   *
   * @param status HTTP のステータスコード。
   * @param code エラーの種類。
   * @param message 利用者に見せてよいメッセージ(サーバーが返したもの)。
   * @param retryAfterSeconds 待つ秒数(無ければ null)。
   */
  constructor(status: number, code: string, message: string, retryAfterSeconds: number | null = null) {
    super(message);
    // クラス名を固定して、デバッグで見分けられるようにする。
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

/**
 * レスポンスのボディから、エラーのコードとメッセージを取り出す。形が違うときは、ステータスから補う。
 *
 * @param status HTTP のステータスコード。
 * @param body 読み取った JSON(読めなければ undefined)。
 * @returns コードとメッセージ。
 */
export function readErrorBody(status: number, body: unknown): { code: string; message: string } {
  // {"error": {"code", "message"}} の形なら、そのまま使う。
  if (typeof body === "object" && body !== null && "error" in body) {
    const inner = (body as { error: unknown }).error;
    if (typeof inner === "object" && inner !== null) {
      const { code, message } = inner as { code?: unknown; message?: unknown };
      if (typeof code === "string" && typeof message === "string") {
        return { code, message };
      }
    }
  }
  // 形が違う(プロキシの HTML のエラーなど)ときは、ステータスで決まる、汎用の文にする。
  return { code: status >= 500 ? "internal_error" : "unknown_error", message: `サーバーがエラーを返しました(${status})` };
}

/**
 * Retry-After ヘッダ(秒)を、数値にする。
 *
 * @param headerValue ヘッダの値(無ければ null)。
 * @returns 秒数。読めなければ null。
 */
export function parseRetryAfter(headerValue: string | null): number | null {
  // ヘッダが無ければ、待つ秒数は分からない。
  if (headerValue === null) {
    return null;
  }
  // 整数の秒だけを受け付ける(日付の形式は、このシステムの API は使わない)。
  const seconds = Number(headerValue);
  return Number.isInteger(seconds) && seconds >= 0 ? seconds : null;
}

/**
 * エラーを、画面に出す文にする。
 *
 * @param error 捕まえたエラー。
 * @returns 画面に出す文。
 */
export function describeError(error: unknown): string {
  // API のエラーは、状況に合わせて、一言を足す。
  if (error instanceof ApiError) {
    if (error.code === "network_error") {
      return "サーバーに接続できません。ネットワークを確認して、もう一度試してください。";
    }
    if (error.status === 429) {
      return error.retryAfterSeconds === null ? "リクエストが多すぎます。しばらくしてから、もう一度試してください。" : `リクエストが多すぎます。${formatWait(error.retryAfterSeconds)}後に、もう一度試してください。`;
    }
    return error.message;
  }
  // それ以外(想定外のエラー)は、詳細を見せない。
  return "予期しないエラーが起きました。";
}

/**
 * 待つ秒数を、読みやすい言葉にする(60 秒未満は秒、それ以上は、切り上げた分)。
 *
 * @param seconds 待つ秒数。
 * @returns 例: 「30 秒」「5 分」「36 分」。
 */
export function formatWait(seconds: number): string {
  // 1分未満は、秒のまま。
  if (seconds < 60) {
    return `${seconds} 秒`;
  }
  // 1分以上は、切り上げた分にする(「あと 1 秒」を、「0 分」にしないため)。
  return `${Math.ceil(seconds / 60)} 分`;
}
