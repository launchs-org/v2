// deploy のボリュームのマウントの編集。保存すると pending になり、適用でコンテナが再起動する。
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, X } from "lucide-react";
import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { applyMounts, discardMounts, listVolumes, updateMounts } from "../api/endpoints";
import type { DeployDetail, Mount, Volume } from "../api/types";
import { useSession } from "../auth/AuthProvider";
import { validateMounts } from "../lib/deploy_settings";
import { ErrorMessage } from "./ErrorMessage";
import { PendingActions } from "./PendingActions";
import { Button } from "./ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "./ui/card";
import { Input } from "./ui/input";
import { Spinner } from "./ui/spinner";

/**
 * 外から数えて、全ページ分の volume を取る(マウントの選択肢に使う)。
 *
 * @param http API の呼び出し道具を持つ、ログインの状態。
 * @param projectId プロジェクトの ID。
 * @param signal 中断の合図。
 * @returns volume の全件。
 */
async function listAllVolumes(http: ReturnType<typeof useSession>["http"], projectId: string, signal: AbortSignal): Promise<Volume[]> {
  // nextCursor が無くなるまで、続きを取る(安全のため、最大 50 ページまで)。
  const volumes: Volume[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < 50; page += 1) {
    const result = await listVolumes(http, projectId, cursor, signal);
    volumes.push(...result.volumes);
    if (result.nextCursor === null) {
      break;
    }
    cursor = result.nextCursor;
  }
  return volumes;
}

/**
 * マウントの、カード。
 *
 * @param props.deploy deploy の詳細。
 * @param props.locked 反映中・削除中など、変更できない間は true。
 */
export function MountsSection({ deploy, locked }: { deploy: DeployDetail; locked: boolean }) {
  const session = useSession();
  // プロジェクトの volume(選択肢と、名前の表示に使う)。
  const volumes = useQuery({ queryKey: ["volumes-all", deploy.projectId], queryFn: ({ signal }) => listAllVolumes(session.http, deploy.projectId, signal) });
  const pending = deploy.volumes.pending_mounts !== null;
  const source = deploy.volumes.pending_mounts ?? deploy.volumes.mounts;
  return (
    <Card>
      <CardHeader>
        <CardTitle>ボリュームのマウント</CardTitle>
        <CardDescription>同じプロジェクトの volume を、パスにマウントします。適用のとき、コンテナが再起動します(短い停止があります)。</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <ErrorMessage error={volumes.error} />
        {/* サーバーの値が変わったら、編集欄を作り直す。volume の一覧が届くまでは、出さない。 */}
        {volumes.isSuccess && <MountsEditor key={JSON.stringify(source)} deployId={deploy.deployId} projectId={deploy.projectId} initial={source} volumes={volumes.data} pending={pending} locked={locked} />}
      </CardContent>
    </Card>
  );
}

/** マウントの編集欄と、適用・破棄。 */
function MountsEditor({ deployId, projectId, initial, volumes, pending, locked }: { deployId: string; projectId: string; initial: Mount[]; volumes: Volume[]; pending: boolean; locked: boolean }) {
  const session = useSession();
  const queryClient = useQueryClient();
  const [mounts, setMounts] = useState<Mount[]>(initial);
  const [validation, setValidation] = useState<string | null>(null);

  // 保存・適用・破棄のあとは、詳細を取り直す。
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["deploy", deployId] });
  const save = useMutation({ mutationFn: (next: Mount[]) => updateMounts(session.http, deployId, next), onSuccess: refresh });
  const apply = useMutation({ mutationFn: () => applyMounts(session.http, deployId), onSuccess: refresh });
  const discard = useMutation({ mutationFn: () => discardMounts(session.http, deployId), onSuccess: refresh });

  /**
   * 保存のフォームが送信されたとき: 検査して、全件を送る。
   *
   * @param event フォームの送信イベント。
   */
  function handleSave(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    // 手元で検査してから送る。
    const problem = validateMounts(mounts);
    setValidation(problem);
    if (problem === null) {
      save.mutate(mounts);
    }
  }

  return (
    <>
      {pending && <PendingActions description="適用すると、コンテナが再起動します。" disabled={locked} applying={apply.isPending} discarding={discard.isPending} onApply={() => apply.mutate()} onDiscard={() => discard.mutate()} error={apply.error ?? discard.error} />}
      <form className="flex flex-col gap-3" onSubmit={handleSave}>
        <fieldset className="flex flex-col gap-2" disabled={locked}>
          <legend className="sr-only">マウント</legend>
          {mounts.length === 0 && <p className="text-sm text-muted-foreground">マウントは、ありません。</p>}
          {mounts.map((mount, index) => (
            <div key={index} className="flex items-center gap-2">
              <select aria-label={`マウントする volume ${index + 1}`} value={mount.volumeId} onChange={(event) => setMounts(mounts.map((candidate, at) => (at === index ? { ...candidate, volumeId: event.target.value } : candidate)))} className="h-9 max-w-64 rounded-md border bg-background px-3 text-sm shadow-xs">
                <option value="">volume を選ぶ</option>
                {volumes.map((volume) => <option key={volume.volumeId} value={volume.volumeId}>{volume.name}</option>)}
                {/* 一覧に無い volume(削除中など)でも、いまの選択を、消さない。 */}
                {mount.volumeId !== "" && !volumes.some((volume) => volume.volumeId === mount.volumeId) && <option value={mount.volumeId}>{mount.volumeId}</option>}
              </select>
              <Input aria-label={`マウント先のパス ${index + 1}`} value={mount.mountPath} onChange={(event) => setMounts(mounts.map((candidate, at) => (at === index ? { ...candidate, mountPath: event.target.value } : candidate)))} placeholder="/data" className="font-mono text-xs" />
              <Button type="button" variant="ghost" size="icon" aria-label={`マウント ${index + 1} を消す`} onClick={() => setMounts(mounts.filter((_, at) => at !== index))}><X aria-hidden="true" /></Button>
            </div>
          ))}
          <Button type="button" variant="outline" size="sm" className="w-fit" onClick={() => setMounts([...mounts, { volumeId: "", mountPath: "" }])}><Plus aria-hidden="true" />マウントを足す</Button>
        </fieldset>
        {volumes.length === 0 && <p className="text-xs text-muted-foreground">このプロジェクトには、まだ volume がありません。<Link to={`/projects/${projectId}`} className="underline">プロジェクトの画面</Link>で作れます。</p>}
        {validation !== null && <p role="alert" className="text-sm text-destructive">{validation}</p>}
        <ErrorMessage error={save.error} />
        <Button type="submit" variant="secondary" className="w-fit" disabled={locked || save.isPending}>
          {save.isPending && <Spinner />}
          保存(適用前の変更にする)
        </Button>
      </form>
    </>
  );
}
