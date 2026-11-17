// deploy の起動設定(環境変数・起動コマンド・引数)の編集。保存すると pending になり、適用でコンテナが再起動する。
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, X } from "lucide-react";
import { useState, type FormEvent } from "react";
import { applyConfig, discardConfig, updateConfig } from "../api/endpoints";
import type { DeployConfig, DeployDetail } from "../api/types";
import { useSession } from "../auth/AuthProvider";
import { argsToText, envToRows, parseArgs, rowsToEnv, validateEnvRows, type EnvRow } from "../lib/deploy_settings";
import { ErrorMessage } from "./ErrorMessage";
import { PendingActions } from "./PendingActions";
import { Button } from "./ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "./ui/card";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Spinner } from "./ui/spinner";

/**
 * 起動設定に、適用前の変更があるか。env・command・args は3つセットで置き換えるので、どれか1つでも入っていれば、変更がある。
 *
 * @param config 起動設定。
 * @returns 適用前の変更があれば true。
 */
export function hasPendingConfig(config: DeployConfig): boolean {
  return config.pending_env !== null || config.pending_command !== null || config.pending_args !== null;
}

/**
 * 起動設定の、カード。
 *
 * @param props.deploy deploy の詳細。
 * @param props.locked 反映中・削除中など、変更できない間は true。
 */
export function ConfigSection({ deploy, locked }: { deploy: DeployDetail; locked: boolean }) {
  const config = deploy.config;
  const pending = hasPendingConfig(config);
  // 編集の初期値: 適用前の変更があればそれ、無ければ、いまの値。
  const source = pending ? { env: config.pending_env ?? {}, command: config.pending_command, args: config.pending_args } : { env: config.env, command: config.command, args: config.args };
  return (
    <Card>
      <CardHeader>
        <CardTitle>起動設定</CardTitle>
        <CardDescription>環境変数、起動コマンド、引数。保存しただけでは反映されず、「適用」でコンテナが再起動します。</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {/* サーバーの値が変わったら(保存・適用・破棄の後)、編集欄を作り直して、最新の値から始める。 */}
        <ConfigEditor key={JSON.stringify(source)} deployId={deploy.deployId} initial={source} pending={pending} locked={locked} />
      </CardContent>
    </Card>
  );
}

/** 起動設定の編集欄と、適用・破棄。 */
function ConfigEditor({ deployId, initial, pending, locked }: { deployId: string; initial: { env: Record<string, string>; command: string | null; args: string[] | null }; pending: boolean; locked: boolean }) {
  const session = useSession();
  const queryClient = useQueryClient();
  const [rows, setRows] = useState<EnvRow[]>(envToRows(initial.env));
  const [command, setCommand] = useState(initial.command ?? "");
  const [argsText, setArgsText] = useState(argsToText(initial.args));
  const [validation, setValidation] = useState<string | null>(null);

  // 保存・適用・破棄のあとは、詳細を取り直す。
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["deploy", deployId] });
  const save = useMutation({ mutationFn: (config: { env: Record<string, string>; command: string | null; args: string[] | null }) => updateConfig(session.http, deployId, config), onSuccess: refresh });
  const apply = useMutation({ mutationFn: () => applyConfig(session.http, deployId), onSuccess: refresh });
  const discard = useMutation({ mutationFn: () => discardConfig(session.http, deployId), onSuccess: refresh });

  /**
   * 保存のフォームが送信されたとき: 検査して、全件を送る。
   *
   * @param event フォームの送信イベント。
   */
  function handleSave(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    // 手元で検査してから送る。
    const problem = validateEnvRows(rows);
    setValidation(problem);
    if (problem === null) {
      // コマンドが空なら、null(イメージの既定)にする。
      save.mutate({ env: rowsToEnv(rows), command: command.trim() === "" ? null : command.trim(), args: parseArgs(argsText) });
    }
  }

  return (
    <>
      {pending && <PendingActions description="適用すると、コンテナが再起動します。" disabled={locked} applying={apply.isPending} discarding={discard.isPending} onApply={() => apply.mutate()} onDiscard={() => discard.mutate()} error={apply.error ?? discard.error} />}
      <form className="flex flex-col gap-4" onSubmit={handleSave}>
        <fieldset className="flex flex-col gap-2" disabled={locked}>
          <legend className="mb-1 text-sm font-medium">環境変数</legend>
          {rows.map((row, index) => (
            <div key={index} className="flex items-center gap-2">
              <Input aria-label={`環境変数のキー ${index + 1}`} value={row.key} onChange={(event) => setRows(rows.map((candidate, at) => (at === index ? { ...candidate, key: event.target.value } : candidate)))} placeholder="KEY" className="max-w-56 font-mono text-xs" />
              <Input aria-label={`環境変数の値 ${index + 1}`} value={row.value} onChange={(event) => setRows(rows.map((candidate, at) => (at === index ? { ...candidate, value: event.target.value } : candidate)))} placeholder="value" className="font-mono text-xs" />
              <Button type="button" variant="ghost" size="icon" aria-label={`環境変数 ${index + 1} を消す`} onClick={() => setRows(rows.filter((_, at) => at !== index))}><X aria-hidden="true" /></Button>
            </div>
          ))}
          <Button type="button" variant="outline" size="sm" className="w-fit" onClick={() => setRows([...rows, { key: "", value: "" }])}><Plus aria-hidden="true" />環境変数を足す</Button>
        </fieldset>
        <div className="flex flex-col gap-2">
          <Label htmlFor="configCommand">起動コマンド</Label>
          <Input id="configCommand" value={command} onChange={(event) => setCommand(event.target.value)} disabled={locked} placeholder="(空ならイメージの既定)" className="font-mono text-xs" />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="configArgs">引数(1行に1つ)</Label>
          <textarea id="configArgs" value={argsText} onChange={(event) => setArgsText(event.target.value)} disabled={locked} rows={3} placeholder="(空ならイメージの既定)" className="rounded-md border bg-background px-3 py-2 font-mono text-xs shadow-xs outline-none focus-visible:ring-[3px] focus-visible:ring-ring/40 disabled:opacity-50" />
        </div>
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
