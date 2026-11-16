// ページの見出し(パンくず、タイトル、説明、右側の操作)。
import { ChevronRight } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";

/** パンくずの1つ。to が無いものは、いまのページ(リンクにしない)。 */
export interface Crumb {
  label: string;
  to?: string;
}

/**
 * ページの見出し。
 *
 * @param props.title タイトル(h1)。
 * @param props.description 説明(薄い色)。
 * @param props.crumbs パンくず(上に出す)。
 * @param props.actions 右側に置く操作。
 */
export function PageHeader({ title, description, crumbs, actions }: { title: ReactNode; description?: ReactNode; crumbs?: Crumb[]; actions?: ReactNode }) {
  return (
    <div className="flex flex-col gap-3 pb-2">
      {crumbs !== undefined && crumbs.length > 0 && (
        <nav aria-label="パンくず" className="flex items-center gap-1.5 text-sm text-muted-foreground">
          {crumbs.map((crumb, index) => (
            <span key={`${crumb.label}-${index}`} className="flex min-w-0 items-center gap-1.5">
              {index > 0 && <ChevronRight aria-hidden="true" className="size-3.5 shrink-0" />}
              {crumb.to === undefined ? <span aria-current="page" className="truncate text-foreground">{crumb.label}</span> : <Link to={crumb.to} className="rounded-sm hover:text-foreground hover:underline">{crumb.label}</Link>}
            </span>
          ))}
        </nav>
      )}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
          {description !== undefined && <p className="mt-1 text-muted-foreground">{description}</p>}
        </div>
        {actions !== undefined && <div className="flex items-center gap-2">{actions}</div>}
      </div>
    </div>
  );
}
