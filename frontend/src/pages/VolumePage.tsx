// volume 1つの画面(名前の変更、拡大、マウントしている deploy、削除)。
import { pollInterval } from "../lib/polling";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ApiError } from "../api/api_error";
import { applyVolumeSize, deleteVolume, discardVolumeSize, getQuota, getVolume, renameVolume, updateVolumeSize } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ErrorMessage } from "../components/ErrorMessage";
import { PageHeader } from "../components/PageHeader";
import { PendingActions } from "../components/PendingActions";
import { useProjectCrumbs } from "../components/useProjectCrumbs";
import { Alert } from "../components/ui/alert";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Skeleton } from "../components/ui/skeleton";
import { Spinner } from "../components/ui/spinner";
import { formatMegabytes } from "../lib/format";
import { validateVolumeName, validateVolumeSize, volumeStatusLabel, volumeStatusTone } from "../lib/volume";

/** quota が取れないときに使う、大きさの上限(MB。サーバーの既定と同じ)。 */
const FALLBACK_MAX_SIZE_MB = 5120;

/** volume 1つの画面。 */
export function VolumePage() {
  const { projectId = "", volumeId = "" } = useParams();
  const session = useSession();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const crumbs = useProjectCrumbs(projectId);
  const [newName, setNewName] = useState<string | null>(null);
  const [newSize, setNewSize] = useState("");
  const [nameProblem, setNameProblem] = useState<string | null>(null);
  const [sizeProblem, setSizeProblem] = useState<string | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  // 上限を、quota から取る。
  const quota = useQuery({ queryKey: ["quota"], queryFn: ({ signal }) => getQuota(session.http, signal) });
  const maxSizeMB = quota.data?.maxVolumeSizeMB ?? FALLBACK_MAX_SIZE_MB;

  // volume を取る。拡張中・削除中の間は、確かめ直す。削除が終わると、404 になる(下で、戻す道を出す)。
  const volume = useQuery({
    queryKey: ["volume", volumeId],
    queryFn: ({ signal }) => getVolume(session.http, projectId, volumeId, signal),
    refetchInterval: (query) => pollInterval(query.state.data?.status === "applying" || query.state.data?.status === "deleting"),
    retry: (failureCount, error) => !(error instanceof ApiError && error.status === 404) && failureCount < 2,
  });

  /** 変更のあとに、この volume と、一覧を取り直す。 */
  async function refresh(): Promise<void> {
    // 画面と一覧の両方を、新しくする。
    await Promise.all([queryClient.invalidateQueries({ queryKey: ["volume", volumeId] }), queryClient.invalidateQueries({ queryKey: ["volumes", projectId] })]);
  }

  // 名前の変更: すぐに反映される。
  const rename = useMutation({
    mutationFn: (name: string) => renameVolume(session.http, projectId, volumeId, name),
    onSuccess: async () => {
      setNewName(null);
      await refresh();
    },
  });

  // 拡大: pending として保存される。
  const resize = useMutation({
    mutationFn: (sizeMB: number) => updateVolumeSize(session.http, projectId, volumeId, sizeMB),
    onSuccess: async () => {
      setNewSize("");
      await refresh();
    },
  });

  // 適用: 拡張が始まる(状態が applying になる)。
  const apply = useMutation({ mutationFn: () => applyVolumeSize(session.http, projectId, volumeId), onSuccess: refresh });

  // 破棄: pending が消える。
  const discard = useMutation({ mutationFn: () => discardVolumeSize(session.http, projectId, volumeId), onSuccess: refresh });

  // 削除: 受け付けられたら、取り直す(状態が deleting になる)。マウント中などは、サーバーが 409 を返す。
  const remove = useMutation({
    mutationFn: () => deleteVolume(session.http, projectId, volumeId),
    onSuccess: async () => {
      setConfirmingDelete(false);
      await Promise.all([refresh(), queryClient.invalidateQueries({ queryKey: ["quota"] })]);
    },
    onError: () => setConfirmingDelete(false),
  });

  /**
   * 名前の変更のフォームが送信されたとき: 検査して、変更を頼む。
   *
   * @param event フォームの送信イベント。
   */
  function handleRename(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    // 手元で検査してから送る。
    const trimmed = (newName ?? "").trim();
    const problem = validateVolumeName(trimmed);
    setNameProblem(problem);
    if (problem === null) {
      rename.mutate(trimmed);
    }
  }

  /**
   * 拡大のフォームが送信されたとき: 検査して、拡大を頼む(縮小は、ここで断る)。
   *
   * @param event フォームの送信イベント。
   * @param currentSizeMB いまの大きさ(MB)。
   */
  function handleResize(event: FormEvent<HTMLFormElement>, currentSizeMB: number): void {
    event.preventDefault();
    // 数値・上限・縮小を、手元で検査する。
    const problem = validateVolumeSize(newSize, maxSizeMB, currentSizeMB);
    setSizeProblem(problem);
    if (problem === null) {
      resize.mutate(Number(newSize));
    }
  }

  // 見つからない(他のユーザーのもの、削除が終わったもの)なら、戻る道を出す。
  if (volume.error instanceof ApiError && volume.error.status === 404) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center gap-4 py-16 text-center">
          <p className="font-medium">volume が見つかりません(削除されたか、存在しません)。</p>
          <Button onClick={() => { void queryClient.invalidateQueries({ queryKey: ["volumes", projectId] }); navigate(`/projects/${projectId}`); }}>プロジェクトへ戻る</Button>
        </CardContent>
      </Card>
    );
  }
  if (volume.isPending) {
    return (
      <div role="status" aria-label="読み込み中" className="flex flex-col gap-4">
        <Skeleton className="h-4 w-40" />
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40 w-full rounded-xl" />
      </div>
    );
  }
  if (volume.isError) {
    return <ErrorMessage error={volume.error} />;
  }
  const data = volume.data;
  const mountedBy = data.mountedBy ?? [];
  // 拡張中・削除中の間は、変更も削除もできない(サーバーも 409 を返す)。
  const busy = data.status === "applying" || data.status === "deleting";
  // マウント中の volume は削除できない(サーバーも 409 を返す)。
  const mounted = mountedBy.length > 0;
  return (
    <>
      <PageHeader
        crumbs={[...crumbs, { label: data.name }]}
        title={<span className="flex flex-wrap items-center gap-3"><span className="break-all">{data.name}</span><Badge variant={volumeStatusTone(data.status)}>{volumeStatusLabel(data.status)}</Badge></span>}
        description={<>ID: <code className="break-all font-mono text-xs">{data.volumeId}</code></>}
      />
      {data.status === "applying" && <Alert variant="info">拡張しています。終わるまで、お待ちください…</Alert>}
      {data.status === "deleting" && <Alert variant="info">削除しています。終わると、この画面は無くなります…</Alert>}
      {data.status === "failed" && <Alert>拡張に失敗しました。もう一度、適用するか、破棄できます。</Alert>}

      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <div className="flex flex-col gap-6">
          <Card>
            <CardHeader>
              <CardTitle>大きさ</CardTitle>
              <CardDescription>拡大だけできます(縮小はできません)。上限は {formatMegabytes(maxSizeMB)} です。適用するまで、変わりません。</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              {data.pending_sizeMB !== null && (
                <PendingActions
                  description={`${formatMegabytes(data.sizeMB)} → ${formatMegabytes(data.pending_sizeMB)} に拡大します。`}
                  disabled={busy}
                  applying={apply.isPending}
                  discarding={discard.isPending}
                  onApply={() => apply.mutate()}
                  onDiscard={() => discard.mutate()}
                  error={apply.error ?? discard.error}
                />
              )}
              <form className="flex flex-wrap items-end gap-3" onSubmit={(event) => handleResize(event, data.sizeMB)}>
                <div className="flex w-48 flex-col gap-2">
                  <Label htmlFor="volumeNewSize">新しい大きさ(MB)</Label>
                  <Input id="volumeNewSize" inputMode="numeric" value={newSize} onChange={(event) => setNewSize(event.target.value)} placeholder={String(data.sizeMB)} disabled={busy} aria-invalid={sizeProblem !== null} />
                </div>
                <Button type="submit" variant="secondary" disabled={busy || resize.isPending || newSize.trim() === ""}>
                  {resize.isPending && <Spinner />}
                  拡大を保存
                </Button>
              </form>
              {sizeProblem !== null && <p role="alert" className="text-sm text-destructive">{sizeProblem}</p>}
              <ErrorMessage error={resize.error} />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>名前の変更</CardTitle>
              <CardDescription>英数字とハイフンだけ、64文字まで。すぐに反映されます。</CardDescription>
            </CardHeader>
            <CardContent>
              <form className="flex flex-wrap items-end gap-3" onSubmit={handleRename}>
                <div className="flex w-full max-w-md flex-col gap-2">
                  <Label htmlFor="renameVolume">新しい名前</Label>
                  <Input id="renameVolume" value={newName ?? data.name} onChange={(event) => setNewName(event.target.value)} maxLength={64} disabled={data.status === "deleting"} aria-invalid={nameProblem !== null} />
                </div>
                <Button type="submit" variant="secondary" disabled={data.status === "deleting" || rename.isPending || newName === null || newName.trim() === data.name}>
                  {rename.isPending && <Spinner />}
                  変更
                </Button>
              </form>
              {nameProblem !== null && <p role="alert" className="mt-2 text-sm text-destructive">{nameProblem}</p>}
              <ErrorMessage error={rename.error} className="mt-3" />
            </CardContent>
          </Card>

          <Card className="border-destructive/30">
            <CardHeader>
              <CardTitle className="text-destructive">削除</CardTitle>
              <CardDescription>データごと削除されます。元に戻せません。マウントしている間は、削除できません。</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              {mounted && <p className="text-sm text-muted-foreground">deploy にマウントされているため、削除できません。先に、マウントを外してください。</p>}
              <ErrorMessage error={remove.error} />
              <Button variant="destructive-outline" className="w-fit" onClick={() => setConfirmingDelete(true)} disabled={busy || mounted || remove.isPending}>
                <Trash2 aria-hidden="true" />
                volume を削除
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
              <dd><Badge variant={volumeStatusTone(data.status)}>{volumeStatusLabel(data.status)}</Badge></dd>
              <dt className="text-muted-foreground">大きさ</dt>
              <dd>{formatMegabytes(data.sizeMB)}</dd>
              {data.pending_sizeMB !== null && (
                <>
                  <dt className="text-muted-foreground">未適用</dt>
                  <dd>{formatMegabytes(data.pending_sizeMB)}</dd>
                </>
              )}
            </dl>
            <div className="border-t pt-4">
              <h3 className="mb-2 text-sm font-medium">マウントしている deploy</h3>
              {mountedBy.length === 0 ? (
                <p className="text-sm text-muted-foreground">どの deploy にも、マウントされていません。</p>
              ) : (
                <ul className="flex flex-col gap-2">
                  {mountedBy.map((deployId) => (
                    <li key={deployId}>
                      <Link to={`/deploys/${deployId}`} state={{ projectId }} className="break-all font-mono text-xs underline-offset-2 hover:underline">{deployId}</Link>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </CardContent>
        </Card>
      </div>

      <ConfirmDialog open={confirmingDelete} title="volume を削除しますか?" confirmLabel="削除する" busy={remove.isPending} onConfirm={() => remove.mutate()} onCancel={() => setConfirmingDelete(false)}>
        <p><strong className="text-foreground">{data.name}</strong> と、中のデータを削除します。元に戻せません。</p>
      </ConfirmDialog>
    </>
  );
}
