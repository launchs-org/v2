// deploy のサービス(ポートの公開)の、作成・編集・削除。ポートの変更は pending になり、適用しても Pod は再起動しない。
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2, X } from "lucide-react";
import { useState, type FormEvent } from "react";
import type { DeployDetail, DeployService, ServicePort } from "../api/types";
import { applyServicePorts, createService, deleteService, discardServicePorts, updateServicePorts } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import { validateServicePorts } from "../lib/deploy_settings";
import { ConfirmDialog } from "./ConfirmDialog";
import { ErrorMessage } from "./ErrorMessage";
import { PendingActions } from "./PendingActions";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "./ui/card";
import { Input } from "./ui/input";
import { Spinner } from "./ui/spinner";

/** 新しいポートの行の、初期値。 */
const NEW_PORT: ServicePort = { name: "", port: 8080, protocol: "tcp" };

/**
 * サービスの状態を、画面に出す言葉と、バッジの色にする。
 *
 * @param status サービスの状態。
 * @returns 言葉と色。
 */
function serviceStatusView(status: string): { label: string; tone: "success" | "warning" | "destructive" | "default" } {
  // 払い出し中と削除中は黄、失敗は赤、稼働中は緑。
  switch (status) {
    case "active":
      return { label: "稼働中", tone: "success" };
    case "pending":
      return { label: "作成中", tone: "warning" };
    case "deleting":
      return { label: "削除中", tone: "warning" };
    case "failed":
      return { label: "失敗", tone: "destructive" };
    default:
      return { label: status, tone: "default" };
  }
}

/**
 * サービスの、カード。サービスが無ければ作成のフォーム、あれば、内容と編集を出す。
 *
 * @param props.deploy deploy の詳細。
 * @param props.locked 反映中・削除中など、変更できない間は true。
 */
export function ServiceSection({ deploy, locked }: { deploy: DeployDetail; locked: boolean }) {
  const service = deploy.service;
  return (
    <Card>
      <CardHeader>
        <CardTitle>サービス</CardTitle>
        <CardDescription>コンテナのポートを、クラスターの中に公開します(TCP / UDP)。ingress から参照するには、TCP のポートが要ります。</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {service === null ? <ServiceCreator deployId={deploy.deployId} locked={locked} /> : <ServiceDetail deployId={deploy.deployId} service={service} locked={locked} />}
      </CardContent>
    </Card>
  );
}

/** サービスを作るフォーム。 */
function ServiceCreator({ deployId, locked }: { deployId: string; locked: boolean }) {
  const session = useSession();
  const queryClient = useQueryClient();
  const [ports, setPorts] = useState<ServicePort[]>([{ ...NEW_PORT, name: "web" }]);
  const [validation, setValidation] = useState<string | null>(null);
  const create = useMutation({ mutationFn: (next: ServicePort[]) => createService(session.http, deployId, next), onSuccess: () => queryClient.invalidateQueries({ queryKey: ["deploy", deployId] }) });

  /**
   * フォームが送信されたとき: 検査して、作成を頼む。
   *
   * @param event フォームの送信イベント。
   */
  function handleCreate(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    // 手元で検査してから送る。
    const problem = validateServicePorts(ports);
    setValidation(problem);
    if (problem === null) {
      create.mutate(ports);
    }
  }

  return (
    <form className="flex flex-col gap-3" onSubmit={handleCreate}>
      <p className="text-sm text-muted-foreground">サービスは、まだありません。公開するポートを決めて、作れます。</p>
      <PortsEditor ports={ports} onChange={setPorts} disabled={locked} />
      {validation !== null && <p role="alert" className="text-sm text-destructive">{validation}</p>}
      <ErrorMessage error={create.error} />
      <Button type="submit" className="w-fit" disabled={locked || create.isPending}>
        {create.isPending && <Spinner />}
        サービスを作る
      </Button>
    </form>
  );
}

/** 作ってあるサービスの、内容・ポートの編集・適用/破棄・削除。 */
function ServiceDetail({ deployId, service, locked }: { deployId: string; service: DeployService; locked: boolean }) {
  const view = serviceStatusView(service.status);
  // ポートを変えられるのは、サービスが稼働中のときだけ(サーバーも、それ以外は 409)。
  const editable = service.status === "active" && !locked;
  const pending = service.pending_ports !== null;
  const source = service.pending_ports ?? service.ports;
  return (
    <>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
        <dt className="text-muted-foreground">状態</dt>
        <dd><Badge variant={view.tone}>{view.label}</Badge></dd>
        <dt className="text-muted-foreground">サービス ID</dt>
        <dd><code className="break-all font-mono text-xs">{service.serviceId}</code></dd>
        <dt className="text-muted-foreground">DNS 名</dt>
        <dd>{service.dnsName === null ? <span className="text-muted-foreground">(払い出し中)</span> : <code className="break-all font-mono text-xs">{service.dnsName}</code>}</dd>
        <dt className="text-muted-foreground">Cluster IP</dt>
        <dd>{service.clusterIP === null ? <span className="text-muted-foreground">(払い出し中)</span> : <code className="font-mono text-xs">{service.clusterIP}</code>}</dd>
      </dl>
      {service.status === "failed" && <p className="text-sm text-destructive">サービスの作成か削除に失敗しています。削除して、作り直せます。</p>}
      {/* サーバーの値が変わったら、編集欄を作り直す。 */}
      <PortsForm key={JSON.stringify(source)} deployId={deployId} initial={source} pending={pending} editable={editable} locked={locked} />
      <ServiceDeleter deployId={deployId} status={service.status} locked={locked} />
    </>
  );
}

/** ポートの編集フォームと、適用・破棄。 */
function PortsForm({ deployId, initial, pending, editable, locked }: { deployId: string; initial: ServicePort[]; pending: boolean; editable: boolean; locked: boolean }) {
  const session = useSession();
  const queryClient = useQueryClient();
  const [ports, setPorts] = useState<ServicePort[]>(initial);
  const [validation, setValidation] = useState<string | null>(null);
  // 保存・適用・破棄のあとは、詳細を取り直す。
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["deploy", deployId] });
  const save = useMutation({ mutationFn: (next: ServicePort[]) => updateServicePorts(session.http, deployId, next), onSuccess: refresh });
  const apply = useMutation({ mutationFn: () => applyServicePorts(session.http, deployId), onSuccess: refresh });
  const discard = useMutation({ mutationFn: () => discardServicePorts(session.http, deployId), onSuccess: refresh });

  /**
   * フォームが送信されたとき: 検査して、全件を送る。
   *
   * @param event フォームの送信イベント。
   */
  function handleSave(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    // 手元で検査してから送る。
    const problem = validateServicePorts(ports);
    setValidation(problem);
    if (problem === null) {
      save.mutate(ports);
    }
  }

  return (
    <>
      {pending && <PendingActions description="適用しても、Pod は再起動しません。" disabled={!editable} applying={apply.isPending} discarding={discard.isPending} onApply={() => apply.mutate()} onDiscard={() => discard.mutate()} error={apply.error ?? discard.error} />}
      <form className="flex flex-col gap-3" onSubmit={handleSave}>
        <PortsEditor ports={ports} onChange={setPorts} disabled={!editable} />
        {validation !== null && <p role="alert" className="text-sm text-destructive">{validation}</p>}
        <ErrorMessage error={save.error} />
        <Button type="submit" variant="secondary" className="w-fit" disabled={!editable || locked || save.isPending}>
          {save.isPending && <Spinner />}
          保存(適用前の変更にする)
        </Button>
      </form>
    </>
  );
}

/** サービスの削除ボタンと、確認。 */
function ServiceDeleter({ deployId, status, locked }: { deployId: string; status: string; locked: boolean }) {
  const session = useSession();
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const remove = useMutation({
    mutationFn: () => deleteService(session.http, deployId),
    onSuccess: async () => {
      setConfirming(false);
      await queryClient.invalidateQueries({ queryKey: ["deploy", deployId] });
    },
    onError: () => setConfirming(false),
  });
  // 作成中と削除中は、削除できない(サーバーも 409)。失敗したものは、やり直せる。
  const canDelete = !locked && status !== "pending" && status !== "deleting";
  return (
    <div className="flex flex-col gap-2 border-t pt-4">
      <ErrorMessage error={remove.error} />
      <Button variant="destructive-outline" className="w-fit" disabled={!canDelete || remove.isPending} onClick={() => setConfirming(true)}>
        <Trash2 aria-hidden="true" />
        サービスを削除
      </Button>
      <ConfirmDialog open={confirming} title="サービスを削除しますか?" confirmLabel="削除する" busy={remove.isPending} onConfirm={() => remove.mutate()} onCancel={() => setConfirming(false)}>
        <p>サービスと、このサービスを参照している ingress の rules が、削除されます。</p>
      </ConfirmDialog>
    </div>
  );
}

/** ポートの行の編集(名前、番号、プロトコル)。 */
function PortsEditor({ ports, onChange, disabled }: { ports: ServicePort[]; onChange: (next: ServicePort[]) => void; disabled: boolean }) {
  return (
    <fieldset className="flex flex-col gap-2" disabled={disabled}>
      <legend className="sr-only">ポート</legend>
      {ports.map((port, index) => (
        <div key={index} className="flex items-center gap-2">
          <Input aria-label={`ポートの名前 ${index + 1}`} value={port.name} onChange={(event) => onChange(ports.map((candidate, at) => (at === index ? { ...candidate, name: event.target.value } : candidate)))} placeholder="web" className="max-w-40 font-mono text-xs" />
          <Input aria-label={`ポート番号 ${index + 1}`} type="number" min={1} max={65535} value={Number.isNaN(port.port) ? "" : port.port} onChange={(event) => onChange(ports.map((candidate, at) => (at === index ? { ...candidate, port: event.target.value === "" ? Number.NaN : Number(event.target.value) } : candidate)))} className="max-w-28 font-mono text-xs" />
          <select aria-label={`プロトコル ${index + 1}`} value={port.protocol} onChange={(event) => onChange(ports.map((candidate, at) => (at === index ? { ...candidate, protocol: event.target.value as "tcp" | "udp" } : candidate)))} className="h-9 rounded-md border bg-background px-3 text-sm shadow-xs">
            <option value="tcp">tcp</option>
            <option value="udp">udp</option>
          </select>
          <Button type="button" variant="ghost" size="icon" aria-label={`ポート ${index + 1} を消す`} onClick={() => onChange(ports.filter((_, at) => at !== index))}><X aria-hidden="true" /></Button>
        </div>
      ))}
      <Button type="button" variant="outline" size="sm" className="w-fit" onClick={() => onChange([...ports, { ...NEW_PORT }])}><Plus aria-hidden="true" />ポートを足す</Button>
    </fieldset>
  );
}
