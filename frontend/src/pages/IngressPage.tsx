// ingress 1つの画面(ホスト、path ルールの編集と適用、削除)。
import { pollInterval } from "../lib/polling";
import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2, X } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { ApiError } from "../api/api_error";
import { applyIngressRules, deleteIngress, discardIngressRules, getIngress, getService, listDeploys, updateIngressRules } from "../api/endpoints";
import type { IngressRule } from "../api/types";
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
import { Skeleton } from "../components/ui/skeleton";
import { Spinner } from "../components/ui/spinner";
import { buildPortChoices, ingressStatusLabel, ingressStatusTone, ruleChoiceValue, validateRulePath } from "../lib/ingress";

/** 編集中のルール1行(向き先は、「serviceId:port」の文字列。未選択は空)。 */
interface RuleDraft {
  path: string;
  choice: string;
}

/**
 * ルールを、編集中の行にする。
 *
 * @param rules ルールの一覧。
 * @returns 編集中の行。
 */
function toDrafts(rules: IngressRule[]): RuleDraft[] {
  return rules.map((rule) => ({ path: rule.path, choice: ruleChoiceValue(rule) }));
}

/**
 * ルールの一覧を、読みやすい1つの文字列にする(並べて見せるため)。
 *
 * @param rule ルール。
 * @returns 「path → serviceId:port」。
 */
function describeRule(rule: IngressRule): string {
  return `${rule.path} → ${rule.serviceId}:${rule.port}`;
}

/** ingress 1つの画面。 */
export function IngressPage() {
  const { projectId = "", ingressId = "" } = useParams();
  const session = useSession();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const crumbs = useProjectCrumbs(projectId);
  const [draft, setDraft] = useState<RuleDraft[] | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  // ingress を取る。反映中・削除中の間は、確かめ直す。削除が終わると、404 になる(下で、戻す道を出す)。
  const ingress = useQuery({
    queryKey: ["ingress", ingressId],
    queryFn: ({ signal }) => getIngress(session.http, projectId, ingressId, signal),
    refetchInterval: (query) => pollInterval(query.state.data?.status === "applying" || query.state.data?.status === "deleting"),
    retry: (failureCount, error) => !(error instanceof ApiError && error.status === 404) && failureCount < 2,
  });

  // プロジェクトの deploy を、全ページ取る(向き先の候補を作るため)。
  const deploys = useQuery({
    queryKey: ["ingress-deploys", projectId],
    queryFn: async ({ signal }) => {
      const all = [];
      let cursor: string | undefined;
      // 続きが無くなるまで、ページを取り続ける。
      do {
        const page = await listDeploys(session.http, projectId, cursor, signal);
        all.push(...page.deploys);
        cursor = page.nextCursor ?? undefined;
      } while (cursor !== undefined);
      return all;
    },
  });

  // それぞれの deploy の service を取る。作っていない(404)ものは、「サービス無し」として扱う。
  const services = useQueries({
    queries: (deploys.data ?? []).map((deploy) => ({
      queryKey: ["ingress-service", deploy.deployId],
      queryFn: async ({ signal }: { signal: AbortSignal }) => {
        try {
          return await getService(session.http, deploy.deployId, signal);
        } catch (caught) {
          // サービスが無いのは、エラーではない。
          if (caught instanceof ApiError && caught.status === 404) {
            return null;
          }
          throw caught;
        }
      },
    })),
  });
  const choices = buildPortChoices((deploys.data ?? []).map((deploy, index) => ({ imageRef: deploy.imageRef, service: services[index]?.data })));

  /** 変更のあとに、この ingress と、一覧を取り直す。 */
  async function refresh(): Promise<void> {
    // 画面と一覧の両方を、新しくする。
    await Promise.all([queryClient.invalidateQueries({ queryKey: ["ingress", ingressId] }), queryClient.invalidateQueries({ queryKey: ["ingresses", projectId] })]);
  }

  // 保存: pending として保存される。
  const save = useMutation({
    mutationFn: (rules: IngressRule[]) => updateIngressRules(session.http, projectId, ingressId, rules),
    onSuccess: async () => {
      setDraft(null);
      await refresh();
    },
  });

  // 適用: 反映が始まる(状態が applying になる)。
  const apply = useMutation({ mutationFn: () => applyIngressRules(session.http, projectId, ingressId), onSuccess: refresh });

  // 破棄: pending が消える。
  const discard = useMutation({
    mutationFn: () => discardIngressRules(session.http, projectId, ingressId),
    onSuccess: async () => {
      setDraft(null);
      await refresh();
    },
  });

  // 削除: 受け付けられたら、取り直す(状態が deleting になる)。
  const remove = useMutation({
    mutationFn: () => deleteIngress(session.http, projectId, ingressId),
    onSuccess: async () => {
      setConfirmingDelete(false);
      await refresh();
    },
    onError: () => setConfirmingDelete(false),
  });

  // 見つからない(他のユーザーのもの、削除が終わったもの)なら、戻る道を出す。
  if (ingress.error instanceof ApiError && ingress.error.status === 404) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center gap-4 py-16 text-center">
          <p className="font-medium">ingress が見つかりません(削除されたか、存在しません)。</p>
          <Button onClick={() => { void queryClient.invalidateQueries({ queryKey: ["ingresses", projectId] }); navigate(`/projects/${projectId}`); }}>プロジェクトへ戻る</Button>
        </CardContent>
      </Card>
    );
  }
  if (ingress.isPending) {
    return (
      <div role="status" aria-label="読み込み中" className="flex flex-col gap-4">
        <Skeleton className="h-4 w-40" />
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40 w-full rounded-xl" />
      </div>
    );
  }
  if (ingress.isError) {
    return <ErrorMessage error={ingress.error} />;
  }
  const data = ingress.data;
  // 反映中・削除中の間は、編集も適用も破棄も削除もできない(サーバーも 409 を返す)。
  const busy = data.status === "applying" || data.status === "deleting";
  // 編集フォームの行は、編集中のものか、pending(あれば)か、いまの rules。
  const rows = draft ?? toDrafts(data.pending_rules ?? data.rules);

  /**
   * 編集中の行を、1つ書き換える。
   *
   * @param index 行の位置。
   * @param change 書き換える項目。
   */
  function updateRow(index: number, change: Partial<RuleDraft>): void {
    // 該当の行だけを、書き換えた新しい配列にする。
    setDraft(rows.map((row, position) => (position === index ? { ...row, ...change } : row)));
  }

  /**
   * ルールのフォームが送信されたとき: 検査して、保存を頼む。
   *
   * @param event フォームの送信イベント。
   */
  function handleSave(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    // 行ごとに、path と向き先を検査する。
    for (const row of rows) {
      const pathProblem = validateRulePath(row.path);
      if (pathProblem !== null) {
        setProblem(pathProblem);
        return;
      }
      if (row.choice === "") {
        setProblem("向き先(サービスとポート)を選んでください。");
        return;
      }
    }
    setProblem(null);
    // 「serviceId:port」を、ルールに戻して送る(serviceId は、最後の「:」より前)。
    const rules = rows.map((row) => {
      const separator = row.choice.lastIndexOf(":");
      return { path: row.path, serviceId: row.choice.slice(0, separator), port: Number(row.choice.slice(separator + 1)) };
    });
    save.mutate(rules);
  }

  return (
    <>
      <PageHeader
        crumbs={[...crumbs, { label: data.host }]}
        title={<span className="flex flex-wrap items-center gap-3"><span className="break-all font-mono text-xl">{data.host}</span><Badge variant={ingressStatusTone(data.status)}>{ingressStatusLabel(data.status)}</Badge></span>}
        description={<>ID: <code className="break-all font-mono text-xs">{data.ingressId}</code></>}
      />
      {data.status === "applying" && <Alert variant="info">反映しています。終わるまで、お待ちください…</Alert>}
      {data.status === "deleting" && <Alert variant="info">削除しています。終わると、この画面は無くなります…</Alert>}
      {data.status === "failed" && <Alert>反映に失敗しました。ルールを直して保存し直すか、もう一度、適用するか、破棄できます。</Alert>}

      <Card>
        <CardHeader>
          <CardTitle>ルール</CardTitle>
          <CardDescription>path ごとに、届け先のサービスとポートを決めます。HTTP(tcp のポート)だけ選べます。保存しても、適用するまで反映されません。</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-5">
          {data.pending_rules !== null && (
            <div className="grid gap-4 md:grid-cols-2">
              <div>
                <h3 className="mb-2 text-sm font-medium">いま反映されているルール</h3>
                {data.rules.length === 0 ? <p className="text-sm text-muted-foreground">ルールはありません。</p> : <ul className="flex flex-col gap-1 font-mono text-xs">{data.rules.map((rule) => <li key={`${rule.path}-${ruleChoiceValue(rule)}`}>{describeRule(rule)}</li>)}</ul>}
              </div>
              <div>
                <h3 className="mb-2 text-sm font-medium">適用前のルール</h3>
                {data.pending_rules.length === 0 ? <p className="text-sm text-muted-foreground">ルールを全て外します。</p> : <ul className="flex flex-col gap-1 font-mono text-xs">{data.pending_rules.map((rule) => <li key={`${rule.path}-${ruleChoiceValue(rule)}`}>{describeRule(rule)}</li>)}</ul>}
              </div>
            </div>
          )}
          {data.pending_rules !== null && (
            <PendingActions description="適用すると、ルールが反映されます。" disabled={busy} applying={apply.isPending} discarding={discard.isPending} onApply={() => apply.mutate()} onDiscard={() => discard.mutate()} error={apply.error ?? discard.error} />
          )}

          <form className="flex flex-col gap-3" onSubmit={handleSave}>
            {rows.length === 0 && <p className="text-sm text-muted-foreground">ルールはありません。追加してください(空のまま保存すると、全て外します)。</p>}
            {rows.map((row, index) => (
              <div key={index} className="flex flex-wrap items-center gap-3">
                <Input aria-label={`path ${index + 1}`} className="w-48 font-mono" value={row.path} onChange={(event) => updateRow(index, { path: event.target.value })} placeholder="/" disabled={busy} />
                <select aria-label={`向き先 ${index + 1}`} value={row.choice} onChange={(event) => updateRow(index, { choice: event.target.value })} disabled={busy} className="h-9 min-w-64 max-w-full rounded-md border bg-background px-3 text-sm shadow-xs">
                  <option value="">向き先を選ぶ</option>
                  {/* いまのルールの向き先が、選べるものに無い(サービスが無くなったなど)ときも、表示だけはする。 */}
                  {row.choice !== "" && !choices.some((choice) => choice.value === row.choice) && <option value={row.choice}>(使えない向き先) {row.choice}</option>}
                  {choices.map((choice) => <option key={choice.value} value={choice.value}>{choice.label}</option>)}
                </select>
                <Button type="button" variant="ghost" size="icon" aria-label={`ルール ${index + 1} を外す`} onClick={() => setDraft(rows.filter((_, position) => position !== index))} disabled={busy}>
                  <X aria-hidden="true" />
                </Button>
              </div>
            ))}
            <div className="flex flex-wrap gap-2">
              <Button type="button" variant="outline" onClick={() => setDraft([...rows, { path: "/", choice: "" }])} disabled={busy}>
                <Plus aria-hidden="true" />
                ルールを足す
              </Button>
              <Button type="submit" variant="secondary" disabled={busy || save.isPending || draft === null}>
                {save.isPending && <Spinner />}
                保存
              </Button>
            </div>
            {problem !== null && <p role="alert" className="text-sm text-destructive">{problem}</p>}
            <ErrorMessage error={save.error} />
            <ErrorMessage error={deploys.error ?? services.find((service) => service.isError)?.error} />
          </form>
        </CardContent>
      </Card>

      <Card className="border-destructive/30">
        <CardHeader>
          <CardTitle className="text-destructive">削除</CardTitle>
          <CardDescription>この ingress と、ルールを削除します。ホスト名にも、届かなくなります。</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <ErrorMessage error={remove.error} />
          <Button variant="destructive-outline" className="w-fit" onClick={() => setConfirmingDelete(true)} disabled={busy || remove.isPending}>
            <Trash2 aria-hidden="true" />
            ingress を削除
          </Button>
        </CardContent>
      </Card>

      <ConfirmDialog open={confirmingDelete} title="ingress を削除しますか?" confirmLabel="削除する" busy={remove.isPending} onConfirm={() => remove.mutate()} onCancel={() => setConfirmingDelete(false)}>
        <p><strong className="text-foreground">{data.host}</strong> と、ルールを削除します。</p>
      </ConfirmDialog>
    </>
  );
}
