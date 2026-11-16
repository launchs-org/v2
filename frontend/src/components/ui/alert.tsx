// shadcn/ui の Alert。エラーなどの、目立たせたいメッセージ(読み上げにも伝わる)。
import { cva, type VariantProps } from "class-variance-authority";
import { AlertCircle, Info } from "lucide-react";
import type { HTMLAttributes } from "react";
import { cn } from "../../lib/utils";

/** アラートの種類。 */
const alertVariants = cva("flex items-start gap-3 rounded-lg border px-4 py-3 text-sm", {
  variants: {
    variant: {
      destructive: "border-destructive/30 bg-destructive/5 text-destructive",
      info: "bg-card text-foreground",
    },
  },
  defaultVariants: { variant: "destructive" },
});

/**
 * アラート。destructive は、role="alert" で、すぐに読み上げられる。
 *
 * @param props.variant 種類。
 */
export function Alert({ className, variant, children, ...props }: HTMLAttributes<HTMLDivElement> & VariantProps<typeof alertVariants>) {
  const Icon = variant === "info" ? Info : AlertCircle;
  return (
    <div role={variant === "info" ? "status" : "alert"} className={cn(alertVariants({ variant }), className)} {...props}>
      <Icon className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}
