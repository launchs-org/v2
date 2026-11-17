"use client"

import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from "react"

/** 幅の下限と上限(px)。 */
const MIN_WIDTH = 360
const MAX_WIDTH = 1200
/** 既定の幅(px)。 */
const DEFAULT_WIDTH = 780
/** 幅を覚えるキー(localStorage)。 */
const WIDTH_KEY = "launchs.panelWidth"
/** キーボードで、1回に変える幅(px)。 */
const KEY_STEP = 40

/**
 * 値を、範囲に収める。
 *
 * @param value 値。
 * @param max 上限(画面の幅に応じて、さらに小さくなる)。
 * @returns 下限と上限の間に収めた値。
 */
function clamp(value: number, max: number): number {
  return Math.min(Math.max(value, MIN_WIDTH), max)
}

/**
 * 右のパネルの幅を、状態として持つ(保存した幅から始める。保存できない環境では、既定の幅)。
 *
 * @returns 幅(px)と、幅を変える関数。変えると、保存もする。
 */
export function usePanelWidth(): [number, (next: number) => void] {
  const [width, setWidth] = useState(DEFAULT_WIDTH)
  // 保存してある幅を、画面が出てから、一度だけ読む(サーバー側の描画と、ずれないようにする)。
  useEffect(() => {
    try {
      const stored = Number(window.localStorage.getItem(WIDTH_KEY))
      if (Number.isFinite(stored) && stored > 0) setWidth(clamp(stored, MAX_WIDTH))
    } catch { /* 読めないときは、既定の幅 */ }
  }, [])
  const update = useCallback((next: number) => {
    const bounded = clamp(next, Math.min(MAX_WIDTH, window.innerWidth - 80))
    setWidth(bounded)
    try { window.localStorage.setItem(WIDTH_KEY, String(Math.round(bounded))) } catch { /* 保存できなくても、動く */ }
  }, [])
  return [width, update]
}

/**
 * パネルの左端の、つまみ。ドラッグか、左右の矢印キーで、幅を変える。ダブルクリックで、既定の幅に戻す。
 *
 * @param props.width いまの幅(px)。
 * @param props.onChange 幅を変えるとき。
 */
export function ResizeHandle({ width, onChange }: { width: number; onChange: (next: number) => void }) {
  const dragging = useRef(false)
  const [active, setActive] = useState(false)

  /** ドラッグを始める(ポインターを、つまみに、つかませる)。 */
  function start(event: PointerEvent<HTMLDivElement>) {
    event.preventDefault()
    event.currentTarget.setPointerCapture(event.pointerId)
    dragging.current = true
    setActive(true)
  }

  /** ドラッグ中: パネルは右端に付いているので、幅は、画面の右端から、ポインターまでの距離。 */
  function move(event: PointerEvent<HTMLDivElement>) {
    if (!dragging.current) return
    onChange(window.innerWidth - event.clientX)
  }

  /** ドラッグを終える。 */
  function end(event: PointerEvent<HTMLDivElement>) {
    dragging.current = false
    setActive(false)
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId)
  }

  /** 矢印キー: 左で広げ、右で狭める。Home で既定の幅。 */
  function key(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === "ArrowLeft") { event.preventDefault(); onChange(width + KEY_STEP) }
    else if (event.key === "ArrowRight") { event.preventDefault(); onChange(width - KEY_STEP) }
    else if (event.key === "Home") { event.preventDefault(); onChange(DEFAULT_WIDTH) }
  }

  return (
    <div
      className={`panel-resize ${active ? "on" : ""}`}
      role="separator"
      aria-orientation="vertical"
      aria-label="パネルの幅を変える(左右の矢印キーでも変えられます)"
      aria-valuemin={MIN_WIDTH}
      aria-valuemax={MAX_WIDTH}
      aria-valuenow={Math.round(width)}
      tabIndex={0}
      onPointerDown={start}
      onPointerMove={move}
      onPointerUp={end}
      onPointerCancel={end}
      onDoubleClick={() => onChange(DEFAULT_WIDTH)}
      onKeyDown={key}
    />
  )
}
