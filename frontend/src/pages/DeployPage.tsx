// deploy 1つの画面(状態、Pod、スケール、ログ、削除)。
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useState } from "react";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import { ApiError } from "../api/api_error";
import { deleteDeploy, getDeploy, scaleDeploy } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import { ConfigSection } from "../components/ConfigSection";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ErrorMessage } from "../components/ErrorMessage";
import { LogViewer } from "../components/LogViewer";
import { MountsSection } from "../components/MountsSection";
import { PageHeader } from "../components/PageHeader";
import { useProjectCrumbs } from "../components/useProjectCrumbs";
import type { DeployDetail } from "../api/types";
import { Alert } from "../components/ui/alert";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Label } from "../components/ui/label";
import { Skeleton } from "../components/ui/skeleton";
import { ServiceSection } from "../components/ServiceSection";
import { Spinner } from "../components/ui/spinner";
import { DEFAULT_MAX_REPLICAS, deployStatusLabel, deployStatusTone, formatDateTime, isDeployTransitioning } from "../lib/deploy";

/** 状態が変わっていく間、確かめ直す間隔(ミリ秒)。 */
const POLL_INTERVAL_MS = 2000;

/** 一覧から来たときに渡される、元のプロジェクト(パンくずを、元のプロジェクトにするため)。 */
interface FromProject {
  projectId: string;
  projectName: string;
}

/**
 * location.state が、元のプロジェクトの情報か調べる。
 *
 * @param state 画面遷移で渡された値(何でも入りうる)。
 * @returns 元のプロジェクト。形が違えば null。
 */
function readFromProject(state: unknown): FromProject | null {
  // オブジェクトで、2つの項目が文字列のときだけ使う。
  if (typeof state === "object" && state !== null) {
    const candidate = state as Record<string, unknown>;
    if (typeof candidate.projectId === "string" && typeof candidate.projectName === "string") {
      return { projectId: candidate.projectId, projectName: candidate.projectName };
    }
  }
  return null;
}

/** deploy 1つの画面。 */
export function DeployPage() {
  const { deployId = "" } = useParams();
  const session = useSession();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const from = readFromProject(location.state);
  const [replicas, setReplicas] = useState<number | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  // 詳細を取る。状態が変わっていく間は、確かめ直す。削除が終わると、404 になる(下で、戻す道を出す)。
  const deploy = useQuery({
    queryKey: ["deploy", deployId],
    queryFn: ({ signal }) => getDeploy(session.http, deployId, signal),
    refetchInterval: (query) => (query.state.data !== undefined && (isDeployTransitioning(query.state.data.status) || query.state.data.service?.status === "pending" || query.state.data.service?.status === "deleting") ? POLL_INTERVAL_MS : false),
    retry: (failureCount, error) => !(error instanceof ApiError && error.status === 404) && failureCount < 2,
  });

  // スケール: 受け付けられたら、取り直す(状態が applying になり、ポーリングが始まる)。
  const scale = useMutation({
    mutationFn: (count: number) => scaleDeploy(session.http, deployId, count),
    onSuccess: async () => {
      setReplicas(null);
      await queryClient.invalidateQueries({ queryKey: ["deploy", deployId] });
    },
  });

  // 削除: 受け付けられたら、取り直す(状態が deleting になる)。
  const remove = useMutation({
    mutationFn: () => deleteDeploy(session.http, deployId),
    onSuccess: async () => {
      setConfirmingDelete(false);
      await Promise.all([queryClient.invalidateQueries({ queryKey: ["deploy", deployId] }), queryClient.invalidateQueries({ queryKey: ["quota"] })]);
    },
    onError: () => setConfirmingDelete(false),
  });

  // 元のプロジェクトが分かれば、そこへ戻る道を作る。分からなければ、一覧へ。
  const backTo = from === null ? "/projects" : `/projects/${from.projectId}`;
  const backLabel = from === null ? "プロジェクトの一覧へ" : "プロジェクトへ戻る";

  // 見つからない(他のユーザーのもの、削除が終わったもの)なら、戻る道を出す。
  if (deploy.error instanceof ApiError && deploy.error.status === 404) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center gap-4 py-16 text-center">
          <p className="font-medium">deploy が見つかりません(削除されたか、存在しません)。</p>
          <Button onClick={() => { void queryClient.invalidateQueries({ queryKey: ["deploys"] }); navigate(backTo); }}>{backLabel}</Button>
        </CardContent>
      </Card>
    );
  }
  if (deploy.isPending) {
    return (
      <div role="status" aria-label="読み込み中" className="flex flex-col gap-4">
        <Skeleton className="h-4 w-40" />
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40 w-full rounded-xl" />
      </div>
    );
  }
  if (deploy.isError) {
    return <ErrorMessage error={deploy.error} />;
  }
  const data = deploy.data;
  // スケールは、反映中・削除中・起動中・削除失敗の間は、サーバーが 409 を返す。
  const canScale = data.status === "active" || data.status === "failed" || data.status === "scaled_zero";
  // 削除は、反映中・起動中・削除中の間は、できない(削除失敗は、やり直せる)。
  const canDelete = data.status !== "applying" && data.status !== "pending" && data.status !== "deleting";
  // 設定の変更は、反映中と削除中の間は、サーバーが 409 を返す。
  const locked = data.status === "applying" || data.status === "deleting";
  const wanted = replicas ?? data.replicas;
  return (
    <>
      <DeployHeader data={data} />
      {isDeployTransitioning(data.status) && <Alert variant="info">{data.status === "deleting" ? "削除しています。終わると、この画面は無くなります…" : "状態が変わるのを待っています…"}</Alert>}
      {data.status === "failed" && <Alert>起動に失敗したか、安定して動かなかったため、止まっています。同じ台数でスケールし直すと、やり直せます。</Alert>}
      {data.status === "delete_failed" && <Alert>削除に失敗して、止まっています。もう一度、削除を試せます。</Alert>}

      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <div className="flex flex-col gap-6">
          <Card>
            <CardHeader>
              <CardTitle>ログ</CardTitle>
              <CardDescription>Pod ごとのログです。終了した Pod のログも、保持期間(7日)の間は見られます。</CardDescription>
            </CardHeader>
            <CardContent>
              <LogViewer deployId={data.deployId} pods={data.logPods} />
            </CardContent>
          </Card>

          <ConfigSection deploy={data} locked={locked} />
          <MountsSection deploy={data} locked={locked} />
          <ServiceSection deploy={data} locked={locked} />

          <Card>
            <CardHeader>
              <CardTitle>スケール</CardTitle>
              <CardDescription>台数(レプリカ数)を変えます。0 にすると止まります。</CardDescription>
            </CardHeader>
            <CardContent>
              <form className="flex flex-wrap items-end gap-3" onSubmit={(event) => { event.preventDefault(); scale.mutate(wanted); }}>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="scaleReplicas">台数</Label>
                  <select id="scaleReplicas" value={wanted} onChange={(event) => setReplicas(Number(event.target.value))} disabled={!canScale} className="h-9 rounded-md border bg-background px-3 text-sm shadow-xs">
                    {Array.from({ length: DEFAULT_MAX_REPLICAS + 1 }, (_, count) => <option key={count} value={count}>{count}</option>)}
                  </select>
                </div>
                <Button type="submit" variant="secondary" disabled={!canScale || scale.isPending || (wanted === data.replicas && data.status !== "failed")}>
                  {scale.isPending && <Spinner />}
                  変更
                </Button>
              </form>
              <ErrorMessage error={scale.error} className="mt-3" />
            </CardContent>
          </Card>

          <Card className="border-destructive/30">
            <CardHeader>
              <CardTitle className="text-destructive">削除</CardTitle>
              <CardDescription>この deploy と、ログ、service を削除します。volume は、残ります。元に戻せません。</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <ErrorMessage error={remove.error} />
              <Button variant="destructive-outline" className="w-fit" onClick={() => setConfirmingDelete(true)} disabled={!canDelete || remove.isPending}>
                <Trash2 aria-hidden="true" />
                deploy を削除
              </Button>
            </CardContent>
          </Card>
        </div>

        <Card>
          <CardHeader>
            <CardTitle>詳細</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-3 text-sm">
              <dt className="text-muted-foreground">状態</dt>
              <dd><Badge variant={deployStatusTone(data.status)}>{deployStatusLabel(data.status)}</Badge></dd>
              <dt className="text-muted-foreground">スペック</dt>
              <dd>{data.spec}</dd>
              <dt className="text-muted-foreground">台数</dt>
              <dd>{data.replicas}</dd>
              <dt className="text-muted-foreground">作成日時</dt>
              <dd>{formatDateTime(data.createdAt)}</dd>
              <dt className="text-muted-foreground">イメージ</dt>
              <dd><code className="break-all font-mono text-xs">{data.imageRef}</code></dd>
            </dl>
            <div className="border-t pt-4">
              <h3 className="mb-2 text-sm font-medium">稼働中の Pod</h3>
              {data.pods.length === 0 ? (
                <p className="text-sm text-muted-foreground">稼働中の Pod はありません。</p>
              ) : (
                <ul className="flex flex-col gap-2">
                  {data.pods.map((pod) => (
                    <li key={pod.podId} className="flex items-center justify-between gap-2 text-sm">
                      <code className="truncate font-mono text-xs">{pod.podId}</code>
                      <Badge variant={pod.status === "running" ? "success" : "warning"}>{pod.status}</Badge>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </CardContent>
        </Card>
      </div>

      <ConfirmDialog open={confirmingDelete} title="deploy を削除しますか?" confirmLabel="削除する" busy={remove.isPending} onConfirm={() => remove.mutate()} onCancel={() => setConfirmingDelete(false)}>
        <p>この deploy と、そのログを削除します。元に戻せません。</p>
      </ConfirmDialog>
    </>
  );
}

/**
 * deploy の画面の見出し。パンくずに、属するプロジェクトを出す(名前は、遷移で渡されたか、取りに行く)。
 *
 * @param props.data deploy の詳細。
 */
function DeployHeader({ data }: { data: DeployDetail }) {
  const crumbs = useProjectCrumbs(data.projectId);
  return (
    <PageHeader
      crumbs={[...crumbs, { label: "deploy" }]}
      title={<span className="flex flex-wrap items-center gap-3"><span>deploy</span><Badge variant={deployStatusTone(data.status)}>{deployStatusLabel(data.status)}</Badge></span>}
      description={<>ID: <code className="break-all font-mono text-xs">{data.deployId}</code></>}
    />
  );
}
