// ロゴ(ロケットの印と、名前)。
import { Rocket } from "lucide-react";
import { cn } from "../lib/utils";

/**
 * ロゴ。
 *
 * @param props.className 追加のクラス。
 * @param props.large 大きく表示するか(ログイン画面用)。
 */
export function Brand({ className, large = false }: { className?: string; large?: boolean }) {
  return (
    <span className={cn("inline-flex items-center gap-2.5 font-medium tracking-tight", large ? "text-2xl" : "text-xl", className)}>
      <span className={cn("grid place-items-center rounded-xl bg-primary text-primary-foreground", large ? "size-11" : "size-9")}>
        <Rocket aria-hidden="true" className={large ? "size-6" : "size-5"} />
      </span>
      launchs
    </span>
  );
}
