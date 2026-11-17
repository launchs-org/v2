// プロジェクトの画面に置く、volume の一覧と、作成のフォーム。
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, HardDrive, Plus } from "lucide-react";
import { useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { createVolume, getQuota, listVolumes } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import { formatMegabytes } from "../lib/format";
import { DEFAULT_VOLUME_SIZE_MB, validateVolumeName, validateVolumeSize, volumeStatusLabel, volumeStatusTone } from "../lib/volume";
import { ErrorMessage } from "./ErrorMessage";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "./ui/card";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Skeleton } from "./ui/skeleton";
import { Spinner } from "./ui/spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";

/** 状態が変わっていく volume があるとき、確かめ直す間隔(ミリ秒)。 */
const POLL_INTERVAL_MS = 3000;

/** quota が取れないときに使う、大きさの上限(MB。サーバーの既定と同じ)。 */
const FALLBACK_MAX_SIZE_MB = 5120;

/**
 * volume の一覧と、作成のフォーム。
 *
 * @param props.projectId プロジェクトの ID。
 * @param props.projectName プロジェクトの名前(volume の画面の、パンくずに使う)。
 * @param props.canCreate 作成できるか(プロジェクトが稼働中のときだけ)。
 */
export function VolumesSection({ projectId, projectName, canCreate }: { projectId: string; projectName: string; canCreate: boolean }) {
  const session = useSession();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [sizeText, setSizeText] = useState(String(DEFAULT_VOLUME_SIZE_MB));
  const [validation, setValidation] = useState<string | null>(null);

  // 上限(volume 1つの最大の大きさ)を、quota から取る。
  const quota = useQuery({ queryKey: ["quota"], queryFn: ({ signal }) => getQuota(session.http, signal) });
  const maxSizeMB = quota.data?.maxVolumeSizeMB ?? FALLBACK_MAX_SIZE_MB;

  // 一覧は、新しい順に、cursor で続きを取る。拡張中のものがあれば、定期的に確かめる。
  const volumes = useInfiniteQuery({
    queryKey: ["volumes", projectId],
    queryFn: ({ pageParam, signal }) => listVolumes(session.http, projectId, pageParam, signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    refetchInterval: (query) => (query.state.data?.pages.some((page) => page.volumes.some((volume) => volume.status === "applying" || volume.status === "deleting")) ? POLL_INTERVAL_MS : false),
  });

  // 作成: 成功したら、一覧と使用量を取り直して、その volume の画面へ進む。
  const create = useMutation({
    mutationFn: () => createVolume(session.http, projectId, { name: name.trim(), sizeMB: Number(sizeText) }),
    onSuccess: async (created) => {
      setName("");
      await Promise.all([queryClient.invalidateQueries({ queryKey: ["volumes", projectId] }), queryClient.invalidateQueries({ queryKey: ["quota"] })]);
      navigate(`/projects/${projectId}/volumes/${created.volumeId}`, { state: { projectId, projectName } });
    },
  });

  /**
   * 作成のフォームが送信されたとき: 名前と大きさを検査して、作成を頼む。
   *
   * @param event フォームの送信イベント。
   */
  function handleCreate(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    // 手元で検査してから送る(サーバーも、確かめる)。
    const problem = validateVolumeName(name.trim()) ?? validateVolumeSize(sizeText, maxSizeMB);
    setValidation(problem);
    if (problem === null) {
      create.mutate();
    }
  }

  const items = volumes.data?.pages.flatMap((page) => page.volumes) ?? [];
  return (
    <Card>
      <CardHeader>
        <CardTitle>volume</CardTitle>
        <CardDescription>データを残しておく、永続ストレージです。deploy を消しても残ります。</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        {canCreate && (
          <form className="flex flex-wrap items-end gap-3" onSubmit={handleCreate}>
            <div className="flex min-w-48 flex-1 flex-col gap-2">
              <Label htmlFor="newVolumeName">volume の名前</Label>
              <Input id="newVolumeName" value={name} onChange={(event) => setName(event.target.value)} maxLength={64} placeholder="db-data" aria-invalid={validation !== null} />
            </div>
            <div className="flex w-40 flex-col gap-2">
              <Label htmlFor="newVolumeSize">大きさ(MB)</Label>
              <Input id="newVolumeSize" inputMode="numeric" value={sizeText} onChange={(event) => setSizeText(event.target.value)} aria-describedby="newVolumeHelp" />
            </div>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? <Spinner /> : <Plus aria-hidden="true" />}
              {create.isPending ? "作成中…" : "volume を作る"}
            </Button>
          </form>
        )}
        {canCreate && <p id="newVolumeHelp" className="-mt-3 text-xs text-muted-foreground">大きさは、{maxSizeMB} MB まで。作ったあとは、拡大だけできます。</p>}
        {validation !== null && <p role="alert" className="text-sm text-destructive">{validation}</p>}
        <ErrorMessage error={create.error} />

        {volumes.isPending && (
          <div role="status" aria-label="volume を読み込み中" className="flex flex-col gap-3">
            <Skeleton className="h-8 w-full" />
          </div>
        )}
        <ErrorMessage error={volumes.error} />
        {volumes.isSuccess && items.length === 0 && (
          <div className="flex flex-col items-center gap-2 py-8 text-center">
            <span className="grid size-10 place-items-center rounded-full bg-muted text-muted-foreground"><HardDrive aria-hidden="true" className="size-5" /></span>
            <p className="font-medium">volume はまだありません</p>
          </div>
        )}
        {items.length > 0 && (
          <div className="overflow-hidden rounded-lg border">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>名前</TableHead>
                  <TableHead className="w-32">状態</TableHead>
                  <TableHead className="w-44">大きさ</TableHead>
                  <TableHead className="w-10"><span className="sr-only">開く</span></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((volume) => (
                  <TableRow key={volume.volumeId} className="group">
                    <TableCell className="font-medium">
                      {/* リンクは、名前だけ。after で、行の全体を押せる範囲にする。 */}
                      <Link to={`/projects/${projectId}/volumes/${volume.volumeId}`} state={{ projectId, projectName }} className="outline-none after:absolute after:inset-0 focus-visible:underline">
                        {volume.name}
                      </Link>
                    </TableCell>
                    <TableCell><Badge variant={volumeStatusTone(volume.status)}>{volumeStatusLabel(volume.status)}</Badge></TableCell>
                    <TableCell>
                      {formatMegabytes(volume.sizeMB)}
                      {volume.pending_sizeMB !== null && <span className="ml-2 text-muted-foreground">→ {formatMegabytes(volume.pending_sizeMB)}(未適用)</span>}
                    </TableCell>
                    <TableCell><ChevronRight aria-hidden="true" className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5" /></TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
        {volumes.hasNextPage && (
          <div className="flex justify-center">
            <Button variant="outline" onClick={() => void volumes.fetchNextPage()} disabled={volumes.isFetchingNextPage}>
              {volumes.isFetchingNextPage && <Spinner />}
              さらに読み込む
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
