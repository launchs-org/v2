// プロジェクトの、deploy・ingress・volume・service の一覧の画面(表と、作成のボタン)。
import { useQueries } from "@tanstack/react-query";
import { ChevronRight, Globe, HardDrive, Network, Plus, Rocket } from "lucide-react";
import { type ReactNode } from "react";
import { Link, useParams } from "react-router-dom";
import { ApiError } from "../api/api_error";
import { getDeploy, getService } from "../api/endpoints";
import type { DeployDetail, DeployService } from "../api/types";
import { useSession } from "../auth/AuthProvider";
import { ErrorMessage } from "../components/ErrorMessage";
import { PageHeader } from "../components/PageHeader";
import { ServiceSection } from "../components/ServiceSection";
import { useProjectResources } from "../components/useProjectResources";
import { useProjectCrumbs } from "../components/useProjectCrumbs";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent } from "../components/ui/card";
import { Skeleton } from "../components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../components/ui/table";
import { deployStatusLabel, deployStatusTone, formatDateTime } from "../lib/deploy";
import { formatMegabytes } from "../lib/format";
import { volumeStatusLabel, volumeStatusTone } from "../lib/volume";

/**
 * 一覧の画面の、共通の枠(見出し、作成のボタン、読み込み中、エラー、空の案内)。
 */
function ListFrame(props: {
  projectId: string;
  title: string;
  description: string;
  createLabel?: string;
  createTo?: string;
  loading: boolean;
  error: unknown;
  empty: boolean;
  emptyIcon: typeof Rocket;
  emptyText: string;
  children: ReactNode;
}) {
  const crumbs = useProjectCrumbs(props.projectId);
  return (
    <>
      <PageHeader
        crumbs={[...crumbs, { label: props.title }]}
        title={props.title}
        description={props.description}
        actions={
          props.createLabel && props.createTo ? (
            <Button asChild>
              <Link to={props.createTo}>
                <Plus aria-hidden="true" />
                {props.createLabel}
              </Link>
            </Button>
          ) : undefined
        }
      />
      {props.loading && (
        <Card role="status" aria-label="読み込み中" className="divide-y overflow-hidden">
          {[0, 1, 2].map((row) => (
            <div key={row} className="flex items-center gap-4 px-4 py-3.5">
              <Skeleton className="h-4 w-48" />
              <Skeleton className="ml-auto h-5 w-16" />
            </div>
          ))}
        </Card>
      )}
      <ErrorMessage error={props.error} />
      {!props.loading && !props.error && props.empty && (
        <Card>
          <CardContent className="flex flex-col items-center gap-3 py-16 text-center">
            <span className="grid size-12 place-items-center rounded-full bg-muted text-muted-foreground">
              <props.emptyIcon aria-hidden="true" className="size-6" />
            </span>
            <p className="font-medium">{props.emptyText}</p>
            {props.createLabel && props.createTo && (
              <Button asChild>
                <Link to={props.createTo}>
                  <Plus aria-hidden="true" />
                  {props.createLabel}
                </Link>
              </Button>
            )}
          </CardContent>
        </Card>
      )}
      {!props.empty && <Card className="overflow-hidden">{props.children}</Card>}
    </>
  );
}

/** deploy の一覧の画面。 */
export function DeployListPage() {
  const { projectId = "" } = useParams();
  const { deploys } = useProjectResources(projectId);
  const items = deploys.data ?? [];
  return (
    <ListFrame
      projectId={projectId}
      title="deploy"
      description="イメージから起動しているコンテナです。"
      createLabel="deploy を作る"
      createTo={`/projects/${projectId}/deploys/new`}
      loading={deploys.isPending}
      error={deploys.error}
      empty={items.length === 0}
      emptyIcon={Rocket}
      emptyText="deploy はまだありません"
    >
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead>イメージ</TableHead>
            <TableHead className="w-28">状態</TableHead>
            <TableHead className="w-24">スペック</TableHead>
            <TableHead className="w-20">台数</TableHead>
            <TableHead className="w-40">作成日時</TableHead>
            <TableHead className="w-10">
              <span className="sr-only">開く</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((deploy) => (
            <TableRow key={deploy.deployId} className="group">
              <TableCell className="max-w-96 truncate font-mono text-xs">
                <Link
                  to={`/projects/${projectId}/deploys/${deploy.deployId}`}
                  className="outline-none after:absolute after:inset-0 focus-visible:underline"
                >
                  {deploy.imageRef}
                </Link>
              </TableCell>
              <TableCell>
                <Badge variant={deployStatusTone(deploy.status)}>
                  {deployStatusLabel(deploy.status)}
                </Badge>
              </TableCell>
              <TableCell>{deploy.spec}</TableCell>
              <TableCell>{deploy.replicas}</TableCell>
              <TableCell className="text-muted-foreground">{formatDateTime(deploy.createdAt)}</TableCell>
              <TableCell>
                <ChevronRight aria-hidden="true" className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </ListFrame>
  );
}

/** サービス (Service) の一覧の画面。 */
export function ServiceListPage() {
  const { projectId = "" } = useParams();
  const session = useSession();
  const { deploys } = useProjectResources(projectId);
  const crumbs = useProjectCrumbs(projectId);
  const deployList = deploys.data ?? [];

  // 各デプロイの詳細を並列取得 (config・volumes など ServiceSection に必要)
  const deployDetails = useQueries({
    queries: deployList.map((deploy) => ({
      queryKey: ["deploy", deploy.deployId],
      queryFn: ({ signal }: { signal: AbortSignal }) => getDeploy(session.http, deploy.deployId, signal),
    })),
  });

  // バックエンドは getDeploy の service フィールドを常に null で返すため、
  // getService() を各デプロイに対して個別に呼び出す。
  // 404 → サービス未作成(null)、それ以外のエラーはそのまま再スロー。
  const serviceQueries = useQueries({
    queries: deployList.map((deploy) => ({
      queryKey: ["service", deploy.deployId],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        getService(session.http, deploy.deployId, signal).catch((e: unknown) => {
          if (e instanceof ApiError && e.status === 404) return null;
          throw e;
        }),
    })),
  });

  const loading =
    deploys.isPending ||
    deployDetails.some((q) => q.isPending) ||
    serviceQueries.some((q) => q.isPending);
  const error =
    deploys.error ||
    deployDetails.find((q) => q.error)?.error ||
    serviceQueries.find((q) => q.error)?.error;

  // DeployDetail に getService の結果をマージしたリスト
  type EnrichedDeploy = {
    deploy: (typeof deployList)[0];
    detail: DeployDetail;
    service: DeployService | null;
  };

  const enriched: EnrichedDeploy[] = deployList
    .map((deploy, i) => {
      const detail = deployDetails[i]?.data;
      // serviceQueries[i].data は DeployService | null | undefined
      // isPending の間は undefined なのでスキップ
      const serviceData = serviceQueries[i]?.data;
      if (!detail || serviceData === undefined) return null;
      return { deploy, detail: { ...detail, service: serviceData }, service: serviceData };
    })
    .filter((item): item is EnrichedDeploy => item !== null);

  // サービスを持っているデプロイ
  const servicesWithDeploy = enriched.filter((item): item is EnrichedDeploy & { service: DeployService } => item.service !== null);

  // サービスをまだ持っていないデプロイ
  const deploysWithoutService = enriched.filter((item) => item.service === null);

  return (
    <>
      <PageHeader
        crumbs={[...crumbs, { label: "サービス" }]}
        title="サービス (Services)"
        description="コンテナのポートをクラスター内に公開し、Ingress や他コンテナと通信できるようにします。"
        actions={
          deploysWithoutService.length > 0 ? (
            <Button asChild>
              <Link to={`/projects/${projectId}/deploys/${deploysWithoutService[0]?.deploy.deployId}?tab=network`}>
                <Plus aria-hidden="true" />
                サービスを作成
              </Link>
            </Button>
          ) : undefined
        }
      />

      {loading && (
        <Card role="status" aria-label="読み込み中" className="divide-y overflow-hidden">
          {[0, 1, 2].map((row) => (
            <div key={row} className="flex items-center gap-4 px-4 py-3.5">
              <Skeleton className="h-4 w-48" />
              <Skeleton className="ml-auto h-5 w-16" />
            </div>
          ))}
        </Card>
      )}

      <ErrorMessage error={error} />

      {!loading && !error && servicesWithDeploy.length === 0 && (
        <Card>
          <CardContent className="flex flex-col items-center gap-3 py-16 text-center">
            <span className="grid size-12 place-items-center rounded-full bg-muted text-muted-foreground">
              <Network aria-hidden="true" className="size-6" />
            </span>
            <p className="font-medium">公開されているサービスはまだありません</p>
            <p className="text-xs text-muted-foreground max-w-sm">
              deploy の「ネットワーク」設定からポートを公開すると、内部 DNS とクラスタ IP が払い出され、Ingress からのルーティングが可能になります。
            </p>
            {deployList.length > 0 ? (
              <Button asChild className="mt-2">
                <Link to={`/projects/${projectId}/deploys/${deployList[0]?.deployId}?tab=network`}>
                  <Plus aria-hidden="true" />
                  ポートを公開する
                </Link>
              </Button>
            ) : (
              <Button asChild className="mt-2">
                <Link to={`/projects/${projectId}/deploys/new`}>
                  <Plus aria-hidden="true" />
                  deploy を作る
                </Link>
              </Button>
            )}
          </CardContent>
        </Card>
      )}

      {!loading && !error && servicesWithDeploy.length > 0 && (
        <div className="flex flex-col gap-6">
          {/* サービスカード一覧 (各サービスの情報表示・インライン編集・適用・削除) */}
          <div className="flex flex-col gap-6">
            {servicesWithDeploy.map(({ deploy, detail, service }) => (
              <div key={service.serviceId} className="flex flex-col gap-3">
                <div className="flex flex-wrap items-center justify-between gap-2 px-1">
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-xs font-semibold text-foreground">
                      {deploy.imageRef.split("/").pop()}
                    </span>
                    <span className="text-xs text-muted-foreground">に紐づくサービス</span>
                  </div>
                  <Link
                    to={`/projects/${projectId}/deploys/${deploy.deployId}`}
                    className="text-xs text-muted-foreground hover:text-foreground hover:underline"
                  >
                    deploy の詳細へ &rarr;
                  </Link>
                </div>
                {/* サービス自体の詳細・ポート編集・適用/破棄・削除UI */}
                <ServiceSection deploy={detail} locked={deploy.status === "deleting"} />
              </div>
            ))}
          </div>

          {/* まだサービスが設定されていないデプロイがある場合 */}
          {deploysWithoutService.length > 0 && (
            <Card className="border-dashed bg-muted/10">
              <CardContent className="flex flex-col gap-3 p-5">
                <div className="flex items-center gap-2">
                  <span className="flex size-2 rounded-full bg-muted-foreground/60" />
                  <span className="text-xs font-semibold text-foreground">
                    ポート未公開のデプロイ ({deploysWithoutService.length} 件)
                  </span>
                </div>
                <p className="text-xs text-muted-foreground">
                  以下のデプロイはまだクラスター内にポートを公開していません。クリックするとポート公開（Service作成）画面が開きます。
                </p>
                <div className="flex flex-wrap gap-2 pt-1">
                  {deploysWithoutService.map(({ deploy }) => (
                    <Link
                      key={deploy.deployId}
                      to={`/projects/${projectId}/deploys/${deploy.deployId}?tab=network`}
                      className="inline-flex items-center gap-2 rounded-lg border border-border bg-card px-3.5 py-2 text-xs font-mono text-foreground hover:border-primary transition-colors shadow-xs"
                    >
                      <Rocket className="size-3.5 text-muted-foreground" />
                      <span className="font-medium">{deploy.imageRef.split("/").pop()}</span>
                      <span className="rounded bg-primary/10 px-1.5 py-0.5 text-[10px] font-sans text-primary">
                        + サービス作成
                      </span>
                    </Link>
                  ))}
                </div>
              </CardContent>
            </Card>
          )}
        </div>
      )}
    </>
  );
}

/** ingress の一覧の画面。 */
export function IngressListPage() {
  const { projectId = "" } = useParams();
  const { ingresses } = useProjectResources(projectId);
  const items = ingresses.data ?? [];
  return (
    <ListFrame
      projectId={projectId}
      title="ingress"
      description="外から、HTTP で、サービスに届くようにする入口です。"
      createLabel="ingress を作る"
      createTo={`/projects/${projectId}/ingresses/new`}
      loading={ingresses.isPending}
      error={ingresses.error}
      empty={items.length === 0}
      emptyIcon={Globe}
      emptyText="ingress はまだありません"
    >
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead>ホスト</TableHead>
            <TableHead className="w-10">
              <span className="sr-only">開く</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((ingress) => (
            <TableRow key={ingress.ingressId} className="group">
              <TableCell className="font-mono text-xs">
                <Link
                  to={`/projects/${projectId}/ingresses/${ingress.ingressId}`}
                  className="outline-none after:absolute after:inset-0 focus-visible:underline"
                >
                  {ingress.host}
                </Link>
              </TableCell>
              <TableCell>
                <ChevronRight aria-hidden="true" className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </ListFrame>
  );
}

/** volume の一覧の画面。 */
export function VolumeListPage() {
  const { projectId = "" } = useParams();
  const { volumes } = useProjectResources(projectId);
  const items = volumes.data ?? [];
  return (
    <ListFrame
      projectId={projectId}
      title="volume"
      description="データを残しておく、永続ストレージです。deploy を消しても残ります。"
      createLabel="volume を作る"
      createTo={`/projects/${projectId}/volumes/new`}
      loading={volumes.isPending}
      error={volumes.error}
      empty={items.length === 0}
      emptyIcon={HardDrive}
      emptyText="volume はまだありません"
    >
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead>名前</TableHead>
            <TableHead className="w-32">状態</TableHead>
            <TableHead className="w-56">大きさ</TableHead>
            <TableHead className="w-10">
              <span className="sr-only">開く</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((volume) => (
            <TableRow key={volume.volumeId} className="group">
              <TableCell className="font-medium">
                <Link
                  to={`/projects/${projectId}/volumes/${volume.volumeId}`}
                  className="outline-none after:absolute after:inset-0 focus-visible:underline"
                >
                  {volume.name}
                </Link>
              </TableCell>
              <TableCell>
                <Badge variant={volumeStatusTone(volume.status)}>
                  {volumeStatusLabel(volume.status)}
                </Badge>
              </TableCell>
              <TableCell>
                {formatMegabytes(volume.sizeMB)}
                {volume.pending_sizeMB !== null && (
                  <span className="ml-2 text-muted-foreground">
                    → {formatMegabytes(volume.pending_sizeMB)}(未適用)
                  </span>
                )}
              </TableCell>
              <TableCell>
                <ChevronRight aria-hidden="true" className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </ListFrame>
  );
}
