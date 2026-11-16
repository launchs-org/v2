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
    <span className={cn("inline-flex items-center gap-2 font-semibold tracking-tight", large ? "text-xl" : "text-base", className)}>
      <span className={cn("grid place-items-center rounded-lg bg-primary text-primary-foreground", large ? "size-9" : "size-8")}>
        <Rocket aria-hidden="true" className={large ? "size-5" : "size-4"} />
      </span>
      launchs
    </span>
  );
}
