// cursor つきの一覧を、最後のページまで、続けて取る(キャンバスは、全件を並べるため)。

/** 取るページ数の上限(サーバーが、いつまでも nextCursor を返し続けても、止まるようにする)。 */
const MAX_PAGES = 50;

/**
 * 一覧を、nextCursor が無くなるまで取って、全件をつなげる。
 *
 * @param fetchPage 1ページを取る関数。cursor を渡し、そのページの項目と、次の cursor(無ければ null)を返す。
 * @returns 全ページの項目をつなげたもの。
 */
export async function fetchAllPages<Item>(fetchPage: (cursor: string | undefined) => Promise<{ items: Item[]; next: string | null }>): Promise<Item[]> {
  const all: Item[] = [];
  let cursor: string | undefined;
  // 上限のページ数まで、続きを取る。
  for (let page = 0; page < MAX_PAGES; page += 1) {
    const result = await fetchPage(cursor);
    all.push(...result.items);
    // 続きが無ければ、終わる。
    if (result.next === null) {
      break;
    }
    cursor = result.next;
  }
  return all;
}
