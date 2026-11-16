// shadcn/ui の Badge。Google のチップのように、淡い背景の、丸いタグ。
import { cva, type VariantProps } from "class-variance-authority";
import type { HTMLAttributes } from "react";
import { cn } from "../../lib/utils";

/** バッジの色の種類。 */
const badgeVariants = cva("inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-xs font-medium whitespace-nowrap", {
  variants: {
    variant: {
      default: "bg-secondary text-secondary-foreground",
      success: "bg-success/12 text-success",
      warning: "bg-warning/15 text-warning",
      destructive: "bg-destructive/12 text-destructive",
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
