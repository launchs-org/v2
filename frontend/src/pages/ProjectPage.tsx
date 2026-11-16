// プロジェクト1つの画面(名前の変更、削除)。
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ApiError } from "../api/api_error";
import { deleteProject, getProject, renameProject } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ErrorMessage } from "../components/ErrorMessage";
import { projectStatusLabel, validateProjectName } from "../lib/project_name";

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
      <>
        <p>プロジェクトが見つかりません(削除されたか、存在しません)。</p>
        <button type="button" className="button" onClick={() => { void queryClient.invalidateQueries({ queryKey: ["projects"] }); navigate("/projects"); }}>プロジェクトの一覧へ</button>
      </>
    );
  }
  if (project.isPending) {
    return <p className="muted" role="status">読み込み中…</p>;
  }
  if (project.isError) {
    return <ErrorMessage error={project.error} />;
  }
  const data = project.data;
  // 削除中・削除失敗の間は、変更できない(サーバーも 409 を返す)。削除は、削除中でなければ、呼べる(失敗の後の、やり直しを含む)。
  const editable = data.status === "active";
  return (
    <>
      <p><Link to="/projects">← プロジェクトの一覧</Link></p>
      <h1>{data.name} <span className={`badge badge--${data.status}`}>{projectStatusLabel(data.status)}</span></h1>
      <p className="muted">ID: <code>{data.projectId}</code></p>
      {data.status === "deleting" && <p role="status">削除しています。終わると、一覧に戻ります…</p>}
      {data.status === "delete_failed" && <p className="error" role="alert">削除に失敗して、止まっています。もう一度、削除を試せます。</p>}

      <section className="card">
        <h2>名前の変更</h2>
        <form className="form form--inline" onSubmit={handleRename}>
          <label className="grow">
            新しい名前
            <input value={newName ?? data.name} onChange={(event) => setNewName(event.target.value)} maxLength={64} disabled={!editable} aria-invalid={validation !== null} />
          </label>
          <button type="submit" className="button" disabled={!editable || rename.isPending || newName === null || newName.trim() === data.name}>変更</button>
          {validation !== null && <p className="error" role="alert">{validation}</p>}
          <ErrorMessage error={rename.error} />
        </form>
      </section>

      <section className="card">
        <h2>削除</h2>
        <p className="muted">プロジェクトの deploy、ingress、volume(データごと)も、全て削除されます。元に戻せません。</p>
        <ErrorMessage error={remove.error} />
        <button type="button" className="button button--danger" onClick={() => setConfirmingDelete(true)} disabled={data.status === "deleting" || remove.isPending}>プロジェクトを削除</button>
      </section>

      <ConfirmDialog open={confirmingDelete} title="プロジェクトを削除しますか?" confirmLabel="削除する" busy={remove.isPending} onConfirm={() => remove.mutate()} onCancel={() => setConfirmingDelete(false)}>
        <p><strong>{data.name}</strong> と、配下の全てのものを削除します。元に戻せません。</p>
      </ConfirmDialog>
    </>
  );
}
