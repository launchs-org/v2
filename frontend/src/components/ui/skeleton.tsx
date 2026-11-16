// shadcn/ui の Skeleton。読み込み中の、灰色の仮の表示。
import type { HTMLAttributes } from "react";
import { cn } from "../../lib/utils";

/** 読み込み中の、脈打つ灰色の帯。 */
export function Skeleton({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div aria-hidden="true" className={cn("animate-pulse rounded-md bg-muted", className)} {...props} />;
}
