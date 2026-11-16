// 画面に出す数の整形。

/**
 * 容量(MB)を、読みやすい単位にする。MB は、実際は MiB(1024 単位)。
 *
 * @param megabytes 容量(MB)。
 * @returns 1024 以上なら GB(小数1桁まで)、未満なら MB。
 */
export function formatMegabytes(megabytes: number): string {
  // 1024MB 以上は、GB にする(整数なら、小数を付けない)。
  if (megabytes >= 1024) {
    const gigabytes = megabytes / 1024;
    return `${Number.isInteger(gigabytes) ? gigabytes : gigabytes.toFixed(1)} GB`;
  }
  return `${megabytes.toLocaleString("ja-JP")} MB`;
}
