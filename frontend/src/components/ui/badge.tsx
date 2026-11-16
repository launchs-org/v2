// shadcn/ui の Badge(淡い背景と細い枠の、小さなタグ)。
import { cva, type VariantProps } from "class-variance-authority";
import type { HTMLAttributes } from "react";
import { cn } from "../../lib/utils";

/** バッジの色の種類。 */
const badgeVariants = cva("inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-medium whitespace-nowrap", {
  variants: {
    variant: {
      default: "border-transparent bg-secondary text-secondary-foreground",
      success: "border-success/25 bg-success/10 text-success",
      warning: "border-warning/30 bg-warning/10 text-warning",
      destructive: "border-destructive/30 bg-destructive/10 text-destructive",
      outline: "border text-muted-foreground",
    },
  },
  defaultVariants: { variant: "default" },
});

/**
 * バッジ。
 *
 * @param props.variant 色の種類。
 */
export function Badge({ className, variant, ...props }: HTMLAttributes<HTMLSpanElement> & VariantProps<typeof badgeVariants>) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />;
}
