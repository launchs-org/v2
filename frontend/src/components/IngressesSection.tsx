// プロジェクトの画面に置く、ingress の一覧と、作成のボタン。
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Globe, Plus } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { createIngress, listIngresses } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import { ErrorMessage } from "./ErrorMessage";
import { Button } from "./ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "./ui/card";
import { Skeleton } from "./ui/skeleton";
import { Spinner } from "./ui/spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";

/**
 * ingress の一覧と、作成のボタン(ホスト名は、自動で払い出される)。
 *
 * @param props.projectId プロジェクトの ID。
 * @param props.projectName プロジェクトの名前(ingress の画面の、パンくずに使う)。
 * @param props.canCreate 作成できるか(プロジェクトが稼働中のときだけ)。
 */
export function IngressesSection({ projectId, projectName, canCreate }: { projectId: string; projectName: string; canCreate: boolean }) {
  const session = useSession();
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  // 一覧は、新しい順に、cursor で続きを取る。
  const ingresses = useInfiniteQuery({
    queryKey: ["ingresses", projectId],
    queryFn: ({ pageParam, signal }) => listIngresses(session.http, projectId, pageParam, signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
  });

  // 作成: 成功したら、一覧を取り直して、その ingress の画面へ進む。
  const create = useMutation({
    mutationFn: () => createIngress(session.http, projectId),
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: ["ingresses", projectId] });
      navigate(`/projects/${projectId}/ingresses/${created.ingressId}`, { state: { projectId, projectName } });
    },
  });

  const items = ingresses.data?.pages.flatMap((page) => page.ingresses) ?? [];
  return (
    <Card>
      <CardHeader>
        <CardTitle>ingress</CardTitle>
        <CardDescription>外から、HTTP で、サービスに届くようにします。ホスト名は、自動で決まります。</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        {canCreate && (
          <Button className="w-fit" onClick={() => create.mutate()} disabled={create.isPending}>
            {create.isPending ? <Spinner /> : <Plus aria-hidden="true" />}
            {create.isPending ? "作成中…" : "ingress を作る"}
          </Button>
        )}
        <ErrorMessage error={create.error} />
        {ingresses.isPending && (
          <div role="status" aria-label="ingress を読み込み中" className="flex flex-col gap-3">
            <Skeleton className="h-8 w-full" />
          </div>
        )}
        <ErrorMessage error={ingresses.error} />
        {ingresses.isSuccess && items.length === 0 && (
          <div className="flex flex-col items-center gap-2 py-8 text-center">
            <span className="grid size-10 place-items-center rounded-full bg-muted text-muted-foreground"><Globe aria-hidden="true" className="size-5" /></span>
            <p className="font-medium">ingress はまだありません</p>
          </div>
        )}
        {items.length > 0 && (
          <div className="overflow-hidden rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>ホスト</TableHead>
                  <TableHead className="w-10"><span className="sr-only">開く</span></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((ingress) => (
                  <TableRow key={ingress.ingressId} className="group">
                    <TableCell className="font-mono text-xs">
                      {/* リンクは、ホストだけ。after で、行の全体を押せる範囲にする。 */}
                      <Link to={`/projects/${projectId}/ingresses/${ingress.ingressId}`} state={{ projectId, projectName }} className="outline-none after:absolute after:inset-0 focus-visible:underline">
                        {ingress.host}
                      </Link>
                    </TableCell>
                    <TableCell><ChevronRight aria-hidden="true" className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5" /></TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
        {ingresses.hasNextPage && (
          <div className="flex justify-center">
            <Button variant="outline" onClick={() => void ingresses.fetchNextPage()} disabled={ingresses.isFetchingNextPage}>
              {ingresses.isFetchingNextPage && <Spinner />}
              さらに読み込む
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
