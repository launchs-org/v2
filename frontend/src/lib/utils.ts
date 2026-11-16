// shadcn/ui の補助: Tailwind のクラス名を、条件つきで組み合わせ、競合するものは、後のものを優先する。
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/**
 * クラス名を組み合わせる(偽の値は無視し、Tailwind の競合するクラスは、後のものが勝つ)。
 *
 * @param inputs クラス名、条件つきのクラス名、配列など。
 * @returns 組み合わせたクラス名。
 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
