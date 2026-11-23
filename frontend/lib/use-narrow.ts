"use client"

import { useEffect, useState } from "react"

/** スマホとみなす画面の幅(px)。globals.css の、スマホ向けの境目と同じ。 */
const NARROW_MAX_WIDTH = 720

/**
 * 画面が、スマホのように狭いかを返す(幅が変わると、追従する)。
 *
 * サーバーでの描画と、最初の描画では false(広い)で、ブラウザで幅を測ってから、本当の値にする。
 *
 * @returns 狭ければ true。
 */
export function useNarrow(): boolean {
  // 最初は、広い画面として描く(サーバーとブラウザの、最初の描画をそろえるため)。
  const [narrow, setNarrow] = useState(false)
  useEffect(() => {
    // 幅の条件を監視し、変わったら、状態を更新する。
    const query = window.matchMedia(`(max-width: ${NARROW_MAX_WIDTH}px)`)
    const update = () => setNarrow(query.matches)
    update()
    query.addEventListener("change", update)
    return () => query.removeEventListener("change", update)
  }, [])
  return narrow
}
