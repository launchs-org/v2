// プロジェクトの一覧と、作成。
import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Folder, FolderPlus, Plus } from "lucide-react";
import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { createProject, listProjects } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import { ErrorMessage } from "../components/ErrorMessage";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent } from "../components/ui/card";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Skeleton } from "../components/ui/skeleton";
import { Spinner } from "../components/ui/spinner";
import { projectStatusLabel, projectStatusTone, validateProjectName } from "../lib/project_name";

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
    <div className="flex flex-col gap-6">
      <h1 className="text-3xl font-normal tracking-tight">プロジェクト</h1>

      <Card>
        <CardContent className="p-5">
          <form className="flex flex-col gap-3 sm:flex-row sm:items-end" onSubmit={handleCreate}>
            <div className="flex flex-1 flex-col gap-2">
              <Label htmlFor="newProjectName">新しいプロジェクトの名前</Label>
              <Input id="newProjectName" value={name} onChange={(event) => setName(event.target.value)} maxLength={64} placeholder="my-app" aria-invalid={validation !== null} aria-describedby="newProjectHelp" />
            </div>
            <Button type="submit" disabled={create.isPending} className="sm:mb-px">
              {create.isPending ? <Spinner /> : <Plus aria-hidden="true" />}
              {create.isPending ? "作成中…" : "作成"}
            </Button>
          </form>
          <p id="newProjectHelp" className="mt-2 text-xs text-muted-foreground">英数字とハイフンだけ、64文字まで。</p>
          {validation !== null && <p role="alert" className="mt-2 text-sm text-destructive">{validation}</p>}
          <ErrorMessage error={create.error} className="mt-3" />
        </CardContent>
      </Card>

      {projects.isPending && (
        <Card role="status" aria-label="読み込み中" className="divide-y overflow-hidden">
          {[0, 1, 2].map((row) => (
            <div key={row} className="flex items-center gap-4 p-4">
              <Skeleton className="size-10 rounded-full" />
              <Skeleton className="h-4 w-40" />
            </div>
          ))}
        </Card>
      )}
      <ErrorMessage error={projects.error} />
      {projects.isSuccess && items.length === 0 && (
        <Card>
          <CardContent className="flex flex-col items-center gap-3 py-14 text-center">
            <span className="grid size-14 place-items-center rounded-full bg-secondary text-secondary-foreground">
              <FolderPlus aria-hidden="true" className="size-7" />
            </span>
            <p className="font-medium">プロジェクトはまだありません</p>
            <p className="text-sm text-muted-foreground">上のフォームから、最初のプロジェクトを作れます。</p>
          </CardContent>
        </Card>
      )}
      {items.length > 0 && (
        <Card className="overflow-hidden">
          <ul className="divide-y">
            {items.map((project) => (
              <li key={project.projectId} className="group relative flex items-center gap-4 px-4 py-3.5 transition-colors hover:bg-accent has-[a:focus-visible]:bg-accent">
                <span aria-hidden="true" className="grid size-10 shrink-0 place-items-center rounded-full bg-secondary text-secondary-foreground">
                  <Folder className="size-5" />
                </span>
                {/* リンクは、名前だけ(読み上げの名前が、状態と混ざらないように)。after で、行の全体を押せる範囲にする。 */}
                <Link to={`/projects/${project.projectId}`} className="min-w-0 flex-1 truncate font-medium outline-none after:absolute after:inset-0">
                  {project.name}
                </Link>
                <Badge variant={projectStatusTone(project.status)}>{projectStatusLabel(project.status)}</Badge>
                <ChevronRight aria-hidden="true" className="size-5 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
              </li>
            ))}
          </ul>
        </Card>
      )}
      {projects.hasNextPage && (
        <div className="flex justify-center">
          <Button variant="outline" onClick={() => void projects.fetchNextPage()} disabled={projects.isFetchingNextPage}>
            {projects.isFetchingNextPage && <Spinner />}
            {projects.isFetchingNextPage ? "読み込み中…" : "もっと見る"}
          </Button>
        </div>
      )}
    </div>
  );
}
