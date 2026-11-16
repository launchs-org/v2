// プロジェクトの画面に置く、deploy の一覧と、作成のフォーム。
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Plus, Rocket } from "lucide-react";
import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { createDeploy, listDeploys } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import { DEPLOY_SPECS, deployStatusLabel, deployStatusTone, formatDateTime, isDeployTransitioning, validateImageRef } from "../lib/deploy";
import { ErrorMessage } from "./ErrorMessage";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "./ui/card";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Skeleton } from "./ui/skeleton";
import { Spinner } from "./ui/spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";

/** 状態が変わっていく deploy があるとき、確かめ直す間隔(ミリ秒)。 */
const POLL_INTERVAL_MS = 3000;

/**
 * deploy の一覧と、作成のフォーム。
 *
 * @param props.projectId プロジェクトの ID。
 * @param props.projectName プロジェクトの名前(deploy の画面の、パンくずに使う)。
 * @param props.canCreate 作成できるか(プロジェクトが稼働中のときだけ)。
 */
export function DeploysSection({ projectId, projectName, canCreate }: { projectId: string; projectName: string; canCreate: boolean }) {
  const session = useSession();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [imageRef, setImageRef] = useState("");
  const [spec, setSpec] = useState<string>("small");
  const [validation, setValidation] = useState<string | null>(null);

  // 一覧は、新しい順に、cursor で続きを取る。状態が変わっていく deploy があれば、定期的に確かめる。
  const deploys = useInfiniteQuery({
    queryKey: ["deploys", projectId],
    queryFn: ({ pageParam, signal }) => listDeploys(session.http, projectId, pageParam, signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    refetchInterval: (query) => (query.state.data?.pages.some((page) => page.deploys.some((deploy) => isDeployTransitioning(deploy.status))) ? POLL_INTERVAL_MS : false),
  });

  // 作成: 成功したら、使用量を取り直して、その deploy の画面へ進む。
  const create = useMutation({
    mutationFn: () => createDeploy(session.http, { projectId, imageRef: imageRef.trim(), spec }),
    onSuccess: async (created) => {
      setImageRef("");
      await Promise.all([queryClient.invalidateQueries({ queryKey: ["deploys", projectId] }), queryClient.invalidateQueries({ queryKey: ["quota"] })]);
      navigate(`/deploys/${created.deployId}`, { state: { projectId, projectName } });
    },
  });

  /**
   * 作成のフォームが送信されたとき: 検査して、作成を頼む。
   *
   * @param event フォームの送信イベント。
   */
  function handleCreate(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    // 手元で検査してから送る(サーバーも、確かめる)。
    const problem = validateImageRef(imageRef.trim());
    setValidation(problem);
    if (problem === null) {
      create.mutate();
    }
  }

  const items = deploys.data?.pages.flatMap((page) => page.deploys) ?? [];
  return (
    <Card>
      <CardHeader>
        <CardTitle>deploy</CardTitle>
        <CardDescription>イメージから、コンテナを起動します。</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        {canCreate && (
          <form className="flex flex-wrap items-end gap-3" onSubmit={handleCreate}>
            <div className="flex min-w-64 flex-1 flex-col gap-2">
              <Label htmlFor="newDeployImage">イメージ</Label>
              <Input id="newDeployImage" value={imageRef} onChange={(event) => setImageRef(event.target.value)} placeholder="registry.example/app:1.0" aria-invalid={validation !== null} />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="newDeploySpec">スペック</Label>
              <select id="newDeploySpec" value={spec} onChange={(event) => setSpec(event.target.value)} className="h-9 rounded-md border bg-background px-3 text-sm shadow-xs">
                {DEPLOY_SPECS.map((candidate) => <option key={candidate} value={candidate}>{candidate}</option>)}
              </select>
            </div>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? <Spinner /> : <Plus aria-hidden="true" />}
              {create.isPending ? "作成中…" : "deploy を作る"}
            </Button>
          </form>
        )}
        {validation !== null && <p role="alert" className="text-sm text-destructive">{validation}</p>}
        <ErrorMessage error={create.error} />

        {deploys.isPending && (
          <div role="status" aria-label="deploy を読み込み中" className="flex flex-col gap-3">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
          </div>
        )}
        <ErrorMessage error={deploys.error} />
        {deploys.isSuccess && items.length === 0 && (
          <div className="flex flex-col items-center gap-2 py-8 text-center">
            <span className="grid size-10 place-items-center rounded-full bg-muted text-muted-foreground"><Rocket aria-hidden="true" className="size-5" /></span>
            <p className="font-medium">deploy はまだありません</p>
          </div>
        )}
        {items.length > 0 && (
          <div className="overflow-hidden rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>イメージ</TableHead>
                  <TableHead className="w-28">状態</TableHead>
                  <TableHead className="w-24">スペック</TableHead>
                  <TableHead className="w-20">台数</TableHead>
                  <TableHead className="w-40">作成日時</TableHead>
                  <TableHead className="w-10"><span className="sr-only">開く</span></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((deploy) => (
                  <TableRow key={deploy.deployId} className="group">
                    <TableCell className="max-w-80 truncate font-mono text-xs">
                      {/* リンクは、イメージだけ(読み上げの名前が混ざらないように)。after で、行の全体を押せる範囲にする。 */}
                      <Link to={`/deploys/${deploy.deployId}`} state={{ projectId, projectName }} className="outline-none after:absolute after:inset-0 focus-visible:underline">
                        {deploy.imageRef}
                      </Link>
                    </TableCell>
                    <TableCell><Badge variant={deployStatusTone(deploy.status)}>{deployStatusLabel(deploy.status)}</Badge></TableCell>
                    <TableCell>{deploy.spec}</TableCell>
                    <TableCell>{deploy.replicas}</TableCell>
                    <TableCell className="text-muted-foreground">{formatDateTime(deploy.createdAt)}</TableCell>
                    <TableCell><ChevronRight aria-hidden="true" className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5" /></TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
        {deploys.hasNextPage && (
          <div className="flex justify-center">
            <Button variant="outline" onClick={() => void deploys.fetchNextPage()} disabled={deploys.isFetchingNextPage}>
              {deploys.isFetchingNextPage && <Spinner />}
              さらに読み込む
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
