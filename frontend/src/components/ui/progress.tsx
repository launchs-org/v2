// shadcn/ui の Progress(使用量などの、割合の棒)。読み上げには、値と最大が伝わる。
import { cn } from "../../lib/utils";

/**
 * 割合の棒。
 *
 * @param props.value 現在の値。
 * @param props.max 最大の値(0 のときは、満杯として扱う)。
 * @param props.label 読み上げ用の名前。
 * @param props.className 追加のクラス。
 */
export function Progress({ value, max, label, className }: { value: number; max: number; label: string; className?: string }) {
  // 割合を求める(最大が 0 なら、満杯)。
  const ratio = max <= 0 ? 1 : Math.min(Math.max(value / max, 0), 1);
  // 満杯に近づくと、色を変えて、知らせる。
  const tone = ratio >= 1 ? "bg-destructive" : ratio >= 0.8 ? "bg-warning" : "bg-primary";
  return (
    <div role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={max} aria-valuenow={value} className={cn("h-2 w-full overflow-hidden rounded-full bg-muted", className)}>
      <div className={cn("h-full rounded-full transition-[width]", tone)} style={{ width: `${Math.round(ratio * 100)}%` }} />
    </div>
  );
}
