// プロジェクトの一覧と、作成。
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { createProject, listProjects } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import { ErrorMessage } from "../components/ErrorMessage";
import { projectStatusLabel, validateProjectName } from "../lib/project_name";

/** 削除中のプロジェクトがあるとき、状態を確かめ直す間隔(ミリ秒)。 */
const POLL_INTERVAL_MS = 3000;

/** プロジェクトの一覧の画面。 */
export function ProjectsPage() {
  const session = useSession();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [validation, setValidation] = useState<string | null>(null);

  // 一覧は、作成日時の新しい順に、cursor で続きを取る。削除中のものがあれば、状態が変わるのを、定期的に確かめる。
  const projects = useInfiniteQuery({
    queryKey: ["projects"],
    queryFn: ({ pageParam, signal }) => listProjects(session.http, pageParam, signal),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    refetchInterval: (query) => (query.state.data?.pages.some((page) => page.projects.some((project) => project.status === "deleting")) ? POLL_INTERVAL_MS : false),
  });

  // 作成: 成功したら、一覧と、使用量を取り直す。
  const create = useMutation({
    mutationFn: (projectName: string) => createProject(session.http, projectName),
    onSuccess: async () => {
      setName("");
      await Promise.all([queryClient.invalidateQueries({ queryKey: ["projects"] }), queryClient.invalidateQueries({ queryKey: ["quota"] })]);
    },
  });

  /**
   * 作成のフォームが送信されたとき: 名前を検査して、作成を頼む。
   *
   * @param event フォームの送信イベント。
   */
  function handleCreate(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    // サーバーに送る前に、手元で検査する(サーバーも、同じ規則で、確かめる)。
    const trimmed = name.trim();
    const problem = validateProjectName(trimmed);
    setValidation(problem);
    if (problem === null) {
      create.mutate(trimmed);
    }
  }

  const items = projects.data?.pages.flatMap((page) => page.projects) ?? [];
  return (
    <>
      <h1>プロジェクト</h1>
      <form className="card form form--inline" onSubmit={handleCreate}>
        <label className="grow">
          新しいプロジェクトの名前
          <input value={name} onChange={(event) => setName(event.target.value)} maxLength={64} placeholder="my-app" aria-invalid={validation !== null} />
        </label>
        <button type="submit" className="button" disabled={create.isPending}>{create.isPending ? "作成中…" : "作成"}</button>
        {validation !== null && <p className="error" role="alert">{validation}</p>}
        <ErrorMessage error={create.error} />
      </form>

      {projects.isPending && <p className="muted" role="status">読み込み中…</p>}
      <ErrorMessage error={projects.error} />
      {projects.isSuccess && items.length === 0 && <p className="muted">プロジェクトはまだありません。上のフォームから作れます。</p>}
      {items.length > 0 && (
        <ul className="list">
          {items.map((project) => (
            <li key={project.projectId} className="list__item">
              <Link to={`/projects/${project.projectId}`}>{project.name}</Link>
              <span className={`badge badge--${project.status}`}>{projectStatusLabel(project.status)}</span>
            </li>
          ))}
        </ul>
      )}
      {projects.hasNextPage && (
        <button type="button" className="button button--ghost" onClick={() => void projects.fetchNextPage()} disabled={projects.isFetchingNextPage}>
          {projects.isFetchingNextPage ? "読み込み中…" : "もっと見る"}
        </button>
      )}
    </>
  );
}
