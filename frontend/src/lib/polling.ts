// 画面を開いている間の、自動更新(ポーリング)の間隔。タブが裏にある間は、止まる(TanStack Query の既定)。

/** 状態が変わっていく途中(起動中・反映中・削除中)の、確かめ直す間隔(ミリ秒)。 */
export const POLL_FAST_MS = 2000;

/** 落ち着いているときの、確かめ直す間隔(ミリ秒)。他の人や watcher による変化に、気づけるようにする。 */
export const POLL_SLOW_MS = 5000;

/**
 * 途中の状態かどうかで、次に確かめるまでの間隔を選ぶ。
 *
 * @param transitioning 状態が変わっていく途中か。
 * @returns 途中なら短い間隔、落ち着いていれば、長い間隔。
 */
export function pollInterval(transitioning: boolean): number {
  return transitioning ? POLL_FAST_MS : POLL_SLOW_MS;
}
