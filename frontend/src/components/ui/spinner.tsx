// 回る印(送信中など)。
import { Loader2 } from "lucide-react";
import { cn } from "../../lib/utils";

/** 回る印。装飾なので、読み上げには含めない(状態は、ボタンの文などで伝える)。 */
export function Spinner({ className }: { className?: string }) {
  return <Loader2 aria-hidden="true" className={cn("size-4 animate-spin", className)} />;
}
