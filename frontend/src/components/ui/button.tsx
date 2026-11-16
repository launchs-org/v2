// shadcn/ui の Button。Google のボタンに合わせて、丸い(pill)形にしている。
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import type { ButtonHTMLAttributes } from "react";
import { cn } from "../../lib/utils";

/** ボタンの見た目の種類と大きさ。 */
export const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-full text-sm font-medium transition-colors disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0 cursor-pointer",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground shadow-xs hover:bg-primary/90",
        tonal: "bg-secondary text-secondary-foreground hover:bg-secondary/80",
        outline: "border border-border bg-card text-primary hover:bg-secondary/60",
        ghost: "text-foreground hover:bg-accent",
        destructive: "bg-destructive text-destructive-foreground shadow-xs hover:bg-destructive/90",
        "destructive-outline": "border border-destructive/40 bg-card text-destructive hover:bg-destructive/10",
        link: "text-primary underline-offset-4 hover:underline",
      },
      size: {
        default: "h-10 px-6",
        sm: "h-8 px-4 text-[0.8125rem]",
        icon: "size-10",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

/** Button の引数。asChild が true なら、子の要素(リンクなど)に、ボタンの見た目を付ける。 */
export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

/**
 * ボタン。
 *
 * @param props.variant 見た目の種類。
 * @param props.size 大きさ。
 * @param props.asChild 子の要素に、見た目を付けるか。
 */
export function Button({ className, variant, size, asChild = false, type = "button", ...props }: ButtonProps) {
  // asChild のときは、子の要素そのものを、ボタンとして描く。
  const Component = asChild ? Slot : "button";
  return <Component className={cn(buttonVariants({ variant, size }), className)} {...(asChild ? {} : { type })} {...props} />;
}
