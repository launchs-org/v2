// プロジェクト1つの画面(名前の変更、削除)。
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { ApiError } from "../api/api_error";
import { deleteProject, getProject, renameProject } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ErrorMessage } from "../components/ErrorMessage";
import { PageHeader } from "../components/PageHeader";
import { Alert } from "../components/ui/alert";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Skeleton } from "../components/ui/skeleton";
import { Spinner } from "../components/ui/spinner";
import { projectStatusLabel, projectStatusTone, validateProjectName } from "../lib/project_name";

/** 削除中の間、状態を確かめ直す間隔(ミリ秒)。 */
const POLL_INTERVAL_MS = 3000;

/** プロジェクト1つの画面。 */
export function ProjectPage() {
  const { projectId = "" } = useParams();
  const session = useSession();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [newName, setNewName] = useState<string | null>(null);
  const [validation, setValidation] = useState<string | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  // プロジェクトを取る。削除中の間は、確かめ直す。削除が終わると、404 になる(下で、一覧へ戻す)。
  const project = useQuery({
    queryKey: ["project", projectId],
    queryFn: ({ signal }) => getProject(session.http, projectId, signal),
    refetchInterval: (query) => (query.state.data?.status === "deleting" ? POLL_INTERVAL_MS : false),
    retry: (failureCount, error) => !(error instanceof ApiError && error.status === 404) && failureCount < 2,
  });

  // 名前の変更: 成功したら、取り直す。
  const rename = useMutation({
    mutationFn: (name: string) => renameProject(session.http, projectId, name),
    onSuccess: async () => {
      setNewName(null);
      await Promise.all([queryClient.invalidateQueries({ queryKey: ["project", projectId] }), queryClient.invalidateQueries({ queryKey: ["projects"] })]);
    },
  });

  // 削除: 受け付けられたら、状態が deleting になるので、取り直して、進み具合を見せる。
  const remove = useMutation({
    mutationFn: () => deleteProject(session.http, projectId),
    onSuccess: async () => {
      setConfirmingDelete(false);
      await queryClient.invalidateQueries({ queryKey: ["project", projectId] });
      await queryClient.invalidateQueries({ queryKey: ["quota"] });
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
    const problem = validateProjectName(trimmed);
    setValidation(problem);
    if (problem === null) {
      rename.mutate(trimmed);
    }
  }

  // 見つからない(他のユーザーのもの、削除が終わったもの)なら、一覧へ戻る道を出す。
  if (project.error instanceof ApiError && project.error.status === 404) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center gap-4 py-16 text-center">
          <p className="font-medium">プロジェクトが見つかりません(削除されたか、存在しません)。</p>
          <Button onClick={() => { void queryClient.invalidateQueries({ queryKey: ["projects"] }); navigate("/projects"); }}>プロジェクトの一覧へ</Button>
        </CardContent>
      </Card>
    );
  }
  if (project.isPending) {
    return (
      <div role="status" aria-label="読み込み中" className="flex flex-col gap-4">
        <Skeleton className="h-4 w-40" />
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40 w-full rounded-xl" />
      </div>
    );
  }
  if (project.isError) {
    return <ErrorMessage error={project.error} />;
  }
  const data = project.data;
  // 削除中・削除失敗の間は、変更できない(サーバーも 409 を返す)。削除は、削除中でなければ、呼べる(失敗の後の、やり直しを含む)。
  const editable = data.status === "active";
  return (
    <>
      <PageHeader
        crumbs={[{ label: "プロジェクト", to: "/projects" }, { label: data.name }]}
        title={<span className="flex flex-wrap items-center gap-3"><span className="break-all">{data.name}</span><Badge variant={projectStatusTone(data.status)}>{projectStatusLabel(data.status)}</Badge></span>}
        description={<>ID: <code className="break-all font-mono text-xs">{data.projectId}</code></>}
      />
      {data.status === "deleting" && <Alert variant="info">削除しています。終わると、一覧に戻ります…</Alert>}
      {data.status === "delete_failed" && <Alert>削除に失敗して、止まっています。もう一度、削除を試せます。</Alert>}

      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <div className="flex flex-col gap-6">
          <Card>
            <CardHeader>
              <CardTitle>名前の変更</CardTitle>
              <CardDescription>英数字とハイフンだけ、64文字まで。</CardDescription>
            </CardHeader>
            <CardContent>
              <form className="flex flex-wrap items-end gap-3" onSubmit={handleRename}>
                <div className="flex w-full max-w-md flex-col gap-2">
                  <Label htmlFor="renameProject">新しい名前</Label>
                  <Input id="renameProject" value={newName ?? data.name} onChange={(event) => setNewName(event.target.value)} maxLength={64} disabled={!editable} aria-invalid={validation !== null} />
                </div>
                <Button type="submit" variant="secondary" disabled={!editable || rename.isPending || newName === null || newName.trim() === data.name}>
                  {rename.isPending && <Spinner />}
                  変更
                </Button>
              </form>
              {validation !== null && <p role="alert" className="mt-2 text-sm text-destructive">{validation}</p>}
              <ErrorMessage error={rename.error} className="mt-3" />
            </CardContent>
          </Card>

          <Card className="border-destructive/30">
            <CardHeader>
              <CardTitle className="text-destructive">削除</CardTitle>
              <CardDescription>プロジェクトの deploy、ingress、volume(データごと)も、全て削除されます。元に戻せません。</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <ErrorMessage error={remove.error} />
              <Button variant="destructive-outline" className="w-fit" onClick={() => setConfirmingDelete(true)} disabled={data.status === "deleting" || remove.isPending}>
                <Trash2 aria-hidden="true" />
                プロジェクトを削除
              </Button>
            </CardContent>
          </Card>
        </div>

        <Card>
          <CardHeader>
            <CardTitle>詳細</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-3 text-sm">
              <dt className="text-muted-foreground">状態</dt>
              <dd><Badge variant={projectStatusTone(data.status)}>{projectStatusLabel(data.status)}</Badge></dd>
              <dt className="text-muted-foreground">名前</dt>
              <dd className="break-all font-medium">{data.name}</dd>
              <dt className="text-muted-foreground">k8s の namespace</dt>
              <dd><code className="break-all font-mono text-xs">{data.projectId}</code></dd>
            </dl>
            <p className="mt-4 border-t pt-4 text-xs text-muted-foreground">deploy、ingress、volume の画面は、これから追加します。</p>
          </CardContent>
        </Card>
      </div>

      <ConfirmDialog open={confirmingDelete} title="プロジェクトを削除しますか?" confirmLabel="削除する" busy={remove.isPending} onConfirm={() => remove.mutate()} onCancel={() => setConfirmingDelete(false)}>
        <p><strong className="text-foreground">{data.name}</strong> と、配下の全てのものを削除します。元に戻せません。</p>
      </ConfirmDialog>
    </>
  );
}
