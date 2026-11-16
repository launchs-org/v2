// shadcn/ui の Label(Radix 版。入力欄と、クリックで結び付く)。
import * as LabelPrimitive from "@radix-ui/react-label";
import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

/**
 * 入力欄のラベル。
 *
 * @param props 通常の label の引数。
 */
export function Label({ className, ...props }: ComponentProps<typeof LabelPrimitive.Root>) {
  return <LabelPrimitive.Root className={cn("text-sm font-medium leading-none text-foreground", className)} {...props} />;
}
