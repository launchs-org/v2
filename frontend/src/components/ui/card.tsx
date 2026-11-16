// shadcn/ui の Card(カードと、その見出し・本文)。
import type { HTMLAttributes } from "react";
import { cn } from "../../lib/utils";

/** カード。白い面に、細い枠と、わずかな影。 */
export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("rounded-xl border bg-card text-card-foreground shadow-sm", className)} {...props} />;
}

/** カードの見出しの部分。 */
export function CardHeader({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex flex-col gap-1.5 p-6 pb-0", className)} {...props} />;
}

/** カードの見出し(h2)。 */
export function CardTitle({ className, ...props }: HTMLAttributes<HTMLHeadingElement>) {
  return <h2 className={cn("font-semibold leading-none tracking-tight", className)} {...props} />;
}

/** カードの説明(薄い色)。 */
export function CardDescription({ className, ...props }: HTMLAttributes<HTMLParagraphElement>) {
  return <p className={cn("text-sm text-muted-foreground", className)} {...props} />;
}

/** カードの本文の部分。 */
export function CardContent({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("p-6", className)} {...props} />;
}
