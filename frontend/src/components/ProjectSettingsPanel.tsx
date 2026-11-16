// 右のパネルに出す、プロジェクトの設定(名前の変更、削除)。
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useState, type FormEvent } from "react";
import { deleteProject, renameProject } from "../api/endpoints";
import type { Project } from "../api/types";
import { useSession } from "../auth/AuthProvider";
import { validateProjectName } from "../lib/project_name";
import { useProjectCrumbs } from "./useProjectCrumbs";
import { ConfirmDialog } from "./ConfirmDialog";
import { ErrorMessage } from "./ErrorMessage";
import { PageHeader } from "./PageHeader";
import { Button } from "./ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "./ui/card";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Spinner } from "./ui/spinner";

/**
 * プロジェクトの設定。
 *
 * @param props.project プロジェクト。
 */
export function ProjectSettingsPanel({ project }: { project: Project }) {
  const crumbs = useProjectCrumbs(project.projectId);
  const session = useSession();
  const queryClient = useQueryClient();
  const [newName, setNewName] = useState<string | null>(null);
  const [validation, setValidation] = useState<string | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  // 削除中・削除失敗の間は、変更できない(サーバーも 409 を返す)。削除は、削除中でなければ、呼べる。
  const editable = project.status === "active";

  // 名前の変更: 成功したら、取り直す。
  const rename = useMutation({
    mutationFn: (name: string) => renameProject(session.http, project.projectId, name),
    onSuccess: async () => {
      setNewName(null);
      await Promise.all([queryClient.invalidateQueries({ queryKey: ["project", project.projectId] }), queryClient.invalidateQueries({ queryKey: ["projects"] })]);
    },
  });
  // 削除: 受け付けられたら、状態が deleting になるので、取り直す。
  const remove = useMutation({
    mutationFn: () => deleteProject(session.http, project.projectId),
    onSuccess: async () => {
      setConfirmingDelete(false);
      await queryClient.invalidateQueries({ queryKey: ["project", project.projectId] });
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

  return (
    <>
      <PageHeader crumbs={[...crumbs, { label: "設定" }]} title="プロジェクトの設定" description={<>ID: <code className="break-all font-mono text-xs">{project.projectId}</code></>} />
      <Card>
        <CardHeader>
          <CardTitle>名前の変更</CardTitle>
          <CardDescription>英数字とハイフンだけ、64文字まで。</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="flex flex-wrap items-end gap-3" onSubmit={handleRename}>
            <div className="flex w-full flex-col gap-2">
              <Label htmlFor="renameProject">新しい名前</Label>
              <Input id="renameProject" value={newName ?? project.name} onChange={(event) => setNewName(event.target.value)} maxLength={64} disabled={!editable} aria-invalid={validation !== null} />
            </div>
            <Button type="submit" variant="secondary" disabled={!editable || rename.isPending || newName === null || newName.trim() === project.name}>
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
          <Button variant="destructive-outline" className="w-fit" onClick={() => setConfirmingDelete(true)} disabled={project.status === "deleting" || remove.isPending}>
            <Trash2 aria-hidden="true" />
            プロジェクトを削除
          </Button>
        </CardContent>
      </Card>
      <ConfirmDialog open={confirmingDelete} title="プロジェクトを削除しますか?" confirmLabel="削除する" busy={remove.isPending} onConfirm={() => remove.mutate()} onCancel={() => setConfirmingDelete(false)}>
        <p><strong className="text-foreground">{project.name}</strong> と、配下の全てのものを削除します。元に戻せません。</p>
      </ConfirmDialog>
    </>
  );
}
