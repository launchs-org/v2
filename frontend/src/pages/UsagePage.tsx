// 使用量の画面(自分の上限と、いまの使用量。GET /api/v1/quota)。
import { useQuery } from "@tanstack/react-query";
import { useSession } from "../auth/AuthProvider";
import { getQuota } from "../api/endpoints";
import type { QuotaItem } from "../api/types";
import { ErrorMessage } from "../components/ErrorMessage";
import { PageHeader } from "../components/PageHeader";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Progress } from "../components/ui/progress";
import { Skeleton } from "../components/ui/skeleton";
import { formatMegabytes } from "../lib/format";

/**
 * 使用量の1項目(名前、使用量/上限、棒)。
 *
 * @param props.title 項目の名前。
 * @param props.description 補足。
 * @param props.item 使用量と上限。
 * @param props.format 数の表示のしかた(既定は、そのままの数)。
 */
function UsageCard({ title, description, item, format = (value) => value.toLocaleString("ja-JP") }: { title: string; description?: string; item: QuotaItem; format?: (value: number) => string }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        {description !== undefined && <CardDescription>{description}</CardDescription>}
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <p className="text-3xl font-semibold tracking-tight">
          {format(item.used)}
          <span className="ml-1 text-base font-normal text-muted-foreground">/ {format(item.limit)}</span>
        </p>
        <Progress value={item.used} max={item.limit} label={`${title}の使用量`} />
      </CardContent>
    </Card>
  );
}

/** 使用量の画面。 */
export function UsagePage() {
  const session = useSession();
  const quota = useQuery({ queryKey: ["quota"], queryFn: ({ signal }) => getQuota(session.http, signal) });
  return (
    <>
      <PageHeader title="使用量" description="あなたのアカウントの上限と、いまの使用量です。上限を超える作成は、できません。" />
      <ErrorMessage error={quota.error} />
      {quota.isPending && (
        <div role="status" aria-label="読み込み中" className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {[0, 1, 2].map((card) => <Skeleton key={card} className="h-36 rounded-xl" />)}
        </div>
      )}
      {quota.data && (
        <>
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            <UsageCard title="プロジェクト" description="削除中のものは、数えません。" item={quota.data.projects} />
            <UsageCard title="deploy(合計)" description="削除中のものは、数えません。" item={quota.data.deploys} />
            <UsageCard title="volume" item={quota.data.volumes} />
            <UsageCard title="volume の容量(合計)" description="拡張の予約も、数えます。" item={quota.data.volumeTotalMB} format={formatMegabytes} />
          </div>
          <section className="flex flex-col gap-3">
            <h2 className="text-lg font-semibold tracking-tight">スペックごとの deploy</h2>
            <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
              {Object.entries(quota.data.deploysBySpec).sort(([first], [second]) => first.localeCompare(second)).map(([spec, item]) => (
                <UsageCard key={spec} title={spec} item={item} />
              ))}
            </div>
          </section>
          <Card>
            <CardContent className="grid gap-4 p-6 sm:grid-cols-2">
              <div>
                <p className="text-sm text-muted-foreground">volume 1つの大きさの上限</p>
                <p className="text-xl font-semibold">{formatMegabytes(quota.data.maxVolumeSizeMB)}</p>
              </div>
              <div>
                <p className="text-sm text-muted-foreground">1つの deploy の replicas の最大</p>
                <p className="text-xl font-semibold">{quota.data.maxReplicas}</p>
              </div>
            </CardContent>
          </Card>
        </>
      )}
    </>
  );
}
