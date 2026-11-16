// プロジェクトの構成を、カードと線で見せるキャンバス(ingress → deploy → volume)。
// カードを押すと、右のパネルが開く。ingress / deploy のカードの右端の点から、つなぎたいカードへ線を引くと、つなぐ設定を作れる。
import { pollInterval } from "../lib/polling";
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query";
import { Globe, HardDrive, Plus, Rocket } from "lucide-react";
import { useMemo, useRef, useState, type PointerEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { getDeploy, getIngress, updateIngressRules, updateMounts } from "../api/endpoints";
import type { DeployDetail, Ingress, Volume } from "../api/types";
import { useSession } from "../auth/AuthProvider";
import { deployStatusLabel, deployStatusTone, isDeployTransitioning } from "../lib/deploy";
import { validateMounts } from "../lib/deploy_settings";
import { formatMegabytes } from "../lib/format";
import { buildGraph, COLUMN_STEP, CANVAS_PADDING, edgeMidpoint, edgePath, NODE_HEIGHT, NODE_WIDTH, nodeKey, type GraphDeploy, type GraphNode } from "../lib/graph";
import { ingressStatusLabel, ingressStatusTone, validateRulePath } from "../lib/ingress";
import { cn } from "../lib/utils";
import { volumeStatusLabel, volumeStatusTone } from "../lib/volume";
import { ConnectDialog } from "./ConnectDialog";
import { useProjectResources } from "./useProjectResources";
import { ErrorMessage } from "./ErrorMessage";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Skeleton } from "./ui/skeleton";

/** つなぐ操作の、内容。 */
type Connection = { kind: "ingress-deploy"; ingressId: string; deployId: string } | { kind: "deploy-volume"; deployId: string; volumeId: string };

/** 線を引いている途中の状態(元のカードと、いまのマウスの位置)。 */
interface DragState {
  fromKey: string;
  x: number;
  y: number;
}

/**
 * イメージの参照から、カードに出す短い名前を作る(レジストリのホストを除く)。
 *
 * @param imageRef イメージの参照。
 * @returns 最後の「/」より後ろ。
 */
function shortImageName(imageRef: string): string {
  const slash = imageRef.lastIndexOf("/");
  return slash === -1 ? imageRef : imageRef.slice(slash + 1);
}

/**
 * キャンバス。
 *
 * @param props.projectId プロジェクトの ID。
 * @param props.selectedKey いま、右のパネルで開いているカードのキー(無ければ null)。
 * @param props.canEdit 作成や接続ができるか(プロジェクトが稼働中のときだけ)。
 */
export function ProjectCanvas({ projectId, selectedKey, canEdit }: { projectId: string; selectedKey?: string | null; canEdit: boolean }) {
  const session = useSession();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const surfaceRef = useRef<HTMLDivElement>(null);
  const [drag, setDrag] = useState<DragState | null>(null);
  const [connection, setConnection] = useState<Connection | null>(null);
  const [path, setPath] = useState("/");
  const [portChoice, setPortChoice] = useState("");
  const [mountPath, setMountPath] = useState("/data");
  const [validation, setValidation] = useState<string | null>(null);

  // 3種類の一覧を、全件取る。状態が変わっていくものがあれば、定期的に確かめる。
  const { deploys, ingresses, volumes } = useProjectResources(projectId);

  // deploy と ingress の詳細(つながりの線を引くのに、サービスとマウント、rules が要る)を、1件ずつ取る。
  const deployDetails = useQueries({
    queries: (deploys.data ?? []).map((deploy) => ({
      queryKey: ["deploy", deploy.deployId],
      queryFn: ({ signal }: { signal: AbortSignal }) => getDeploy(session.http, deploy.deployId, signal),
      refetchInterval: (query: { state: { data: DeployDetail | undefined } }) => pollInterval(query.state.data !== undefined && (isDeployTransitioning(query.state.data.status) || query.state.data.service?.status === "pending" || query.state.data.service?.status === "deleting")),
    })),
  });
  const ingressDetails = useQueries({
    queries: (ingresses.data ?? []).map((ingress) => ({
      queryKey: ["ingress", ingress.ingressId],
      queryFn: ({ signal }: { signal: AbortSignal }) => getIngress(session.http, projectId, ingress.ingressId, signal),
      refetchInterval: (query: { state: { data: Ingress | undefined } }) => pollInterval(query.state.data?.status === "applying" || query.state.data?.status === "deleting"),
    })),
  });

  // 詳細を、ID で引ける表にする。まだ届いていないものは、一覧の情報だけで描く。
  const deployDetailById = new Map<string, DeployDetail>();
  (deploys.data ?? []).forEach((deploy, index) => {
    const detail = deployDetails[index]?.data;
    if (detail !== undefined) {
      deployDetailById.set(deploy.deployId, detail);
    }
  });
  const ingressById = new Map<string, Ingress>();
  (ingresses.data ?? []).forEach((summary, index) => {
    ingressById.set(summary.ingressId, ingressDetails[index]?.data ?? { ingressId: summary.ingressId, host: summary.host, rules: [], pending_rules: null, status: "active" });
  });
  const volumeById = new Map<string, Volume>((volumes.data ?? []).map((volume) => [volume.volumeId, volume]));

  // グラフ(位置と線)を作る。詳細が届くたびに、作り直す。
  const graph = useMemo(() => {
    const graphDeploys: GraphDeploy[] = (deploys.data ?? []).map((deploy) => {
      const detail = deployDetailById.get(deploy.deployId);
      return { deployId: deploy.deployId, imageRef: deploy.imageRef, status: detail?.status ?? deploy.status, replicas: detail?.replicas ?? deploy.replicas, service: detail?.service ?? null, mounts: detail?.volumes.mounts ?? [], pendingMounts: detail?.volumes.pending_mounts ?? null };
    });
    return buildGraph({ ingresses: [...ingressById.values()], deploys: graphDeploys, volumes: volumes.data ?? [] });
    // 詳細の表は、毎回作り直されるので、元のデータの更新を見て、作り直す。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deploys.data, ingresses.data, volumes.data, deployDetails.map((query) => query.dataUpdatedAt).join(","), ingressDetails.map((query) => query.dataUpdatedAt).join(",")]);
  const nodeByKey = new Map(graph.nodes.map((node) => [node.key, node]));

  // つなぐ操作を保存する(rules / マウントは、全件で置き換えて、pending にする)。
  const connect = useMutation({
    mutationFn: async () => {
      if (connection === null) {
        return;
      }
      if (connection.kind === "ingress-deploy") {
        // ingress の rules(適用前があればそれ)に、新しい rule を足して送る。
        const ingress = ingressById.get(connection.ingressId);
        const service = deployDetailById.get(connection.deployId)?.service;
        if (ingress === undefined || service === null || service === undefined) {
          return;
        }
        await updateIngressRules(session.http, projectId, connection.ingressId, [...(ingress.pending_rules ?? ingress.rules), { path, serviceId: service.serviceId, port: Number(portChoice) }]);
        return;
      }
      // deploy のマウント(適用前があればそれ)に、新しいマウントを足して送る。
      const detail = deployDetailById.get(connection.deployId);
      if (detail === undefined) {
        return;
      }
      await updateMounts(session.http, connection.deployId, [...(detail.volumes.pending_mounts ?? detail.volumes.mounts), { volumeId: connection.volumeId, mountPath }]);
    },
    onSuccess: async () => {
      // 取り直して、変更の内容が分かる、パネルを開く。
      const done = connection;
      setConnection(null);
      if (done === null) {
        return;
      }
      if (done.kind === "ingress-deploy") {
        await queryClient.invalidateQueries({ queryKey: ["ingress", done.ingressId] });
        navigate(`/projects/${projectId}/ingresses/${done.ingressId}`);
      } else {
        await queryClient.invalidateQueries({ queryKey: ["deploy", done.deployId] });
        navigate(`/projects/${projectId}/deploys/${done.deployId}?tab=volumes`);
      }
    },
  });

  /**
   * 線を引き始める(カードの右端の点を押したとき)。
   *
   * @param event ポインターのイベント。
   * @param fromKey 元のカードのキー。
   */
  function startDrag(event: PointerEvent<HTMLButtonElement>, fromKey: string): void {
    // カードのリンクを押したことにしない。
    event.preventDefault();
    event.stopPropagation();
    const rect = surfaceRef.current?.getBoundingClientRect();
    setDrag({ fromKey, x: event.clientX - (rect?.left ?? 0), y: event.clientY - (rect?.top ?? 0) });
  }

  /**
   * 線を引いている間、マウスの位置を追う。
   *
   * @param event ポインターのイベント。
   */
  function moveDrag(event: PointerEvent<HTMLDivElement>): void {
    if (drag === null) {
      return;
    }
    const rect = surfaceRef.current?.getBoundingClientRect();
    setDrag({ ...drag, x: event.clientX - (rect?.left ?? 0), y: event.clientY - (rect?.top ?? 0) });
  }

  /**
   * 線を離したとき: 離した場所のカードが、つなげる相手なら、入力のダイアログを開く。
   *
   * @param event ポインターのイベント。
   */
  function endDrag(event: PointerEvent<HTMLDivElement>): void {
    const from = drag;
    setDrag(null);
    if (from === null || typeof document.elementFromPoint !== "function") {
      return;
    }
    // 離した場所にあるカードのキーを調べる。
    const targetKey = document.elementFromPoint(event.clientX, event.clientY)?.closest("[data-node-key]")?.getAttribute("data-node-key");
    if (targetKey === null || targetKey === undefined) {
      return;
    }
    const [fromKind, fromId] = from.fromKey.split(":") as [string, string];
    const [targetKind, targetId] = targetKey.split(":") as [string, string];
    setValidation(null);
    // ingress → deploy と、deploy → volume だけが、つなげる組み合わせ。
    if (fromKind === "ingress" && targetKind === "deploy") {
      const service = deployDetailById.get(targetId)?.service;
      const firstTcp = service?.ports.find((port) => port.protocol === "tcp");
      setPath("/");
      setPortChoice(firstTcp === undefined ? "" : String(firstTcp.port));
      setConnection({ kind: "ingress-deploy", ingressId: fromId, deployId: targetId });
    } else if (fromKind === "deploy" && targetKind === "volume") {
      setMountPath("/data");
      setConnection({ kind: "deploy-volume", deployId: fromId, volumeId: targetId });
    }
  }

  /**
   * つなぐダイアログの送信: 検査して、保存する。
   */
  function submitConnection(): void {
    // 手元で検査してから送る(サーバーも確かめる)。
    if (connection?.kind === "ingress-deploy") {
      const problem = validateRulePath(path);
      setValidation(problem);
      if (problem === null) {
        connect.mutate();
      }
    } else if (connection?.kind === "deploy-volume") {
      const detail = deployDetailById.get(connection.deployId);
      const problem = validateMounts([...(detail?.volumes.pending_mounts ?? detail?.volumes.mounts ?? []), { volumeId: connection.volumeId, mountPath }]);
      setValidation(problem);
      if (problem === null) {
        connect.mutate();
      }
    }
  }

  // 読み込み中は、仮の帯を出す。
  if (deploys.isPending || ingresses.isPending || volumes.isPending) {
    return <div role="status" aria-label="読み込み中" className="p-6"><Skeleton className="h-64 w-full rounded-xl" /></div>;
  }
  const loadError = deploys.error ?? ingresses.error ?? volumes.error;
  const empty = graph.nodes.length === 0;

  // つなぐダイアログの、中身を決める。
  const connectDeploy = connection === null ? undefined : deployDetailById.get(connection.deployId);
  const tcpPorts = connectDeploy?.service?.status === "active" ? connectDeploy.service.ports.filter((port) => port.protocol === "tcp") : [];
  const connectIngress = connection?.kind === "ingress-deploy" ? ingressById.get(connection.ingressId) : undefined;
  // 反映中・削除中のものには、つなげない(サーバーも 409)。
  const blockedReason = (() => {
    if (connection?.kind === "ingress-deploy") {
      if (connectIngress?.status === "applying" || connectIngress?.status === "deleting") {
        return "この ingress は、反映中か削除中なので、いまは変更できません。";
      }
      if (tcpPorts.length === 0) {
        return "この deploy には、稼働中のサービスの TCP ポートがありません。先に、deploy のネットワークで、サービスを作ってください。";
      }
    }
    if (connection?.kind === "deploy-volume" && (connectDeploy?.status === "applying" || connectDeploy?.status === "deleting")) {
      return "この deploy は、反映中か削除中なので、いまは変更できません。";
    }
    return null;
  })();

  return (
    <div className="relative h-full min-h-0 overflow-auto bg-dot-grid bg-muted/10">
      <ErrorMessage error={loadError} className="m-4" />
      <div
        ref={surfaceRef}
        data-testid="canvas"
        className="relative"
        style={{ width: Math.max(graph.width, 760), height: Math.max(graph.height, 360), minWidth: "100%", minHeight: "100%" }}
        onPointerMove={moveDrag}
        onPointerUp={endDrag}
      >
        {/* 列の見出し。 */}
        {[{ title: "公開 (Ingress)", icon: Globe }, { title: "デプロイ (Service)", icon: Rocket }, { title: "ストレージ (Volume)", icon: HardDrive }].map((column, index) => (
          <div
            key={column.title}
            className="absolute flex items-center gap-1.5 rounded-md border border-border/80 bg-card/90 px-2.5 py-1 text-[11px] font-medium text-muted-foreground shadow-xs backdrop-blur-xs"
            style={{ left: CANVAS_PADDING + index * COLUMN_STEP, top: CANVAS_PADDING - 6 }}
          >
            <column.icon aria-hidden="true" className="size-3.5" />
            <span>{column.title}</span>
          </div>
        ))}

        {/* つながりの線。 */}
        <svg className="pointer-events-none absolute inset-0" width={Math.max(graph.width, 760)} height={Math.max(graph.height, 360)} aria-hidden="true">
          <defs>
            <linearGradient id="edge-ingress-deploy" x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" stopColor="#10b981" stopOpacity="0.8" />
              <stop offset="100%" stopColor="#6366f1" stopOpacity="0.8" />
            </linearGradient>
            <linearGradient id="edge-deploy-volume" x1="0%" y1="0%" x2="100%" y2="0%">
              <stop offset="0%" stopColor="#6366f1" stopOpacity="0.8" />
              <stop offset="100%" stopColor="#a855f7" stopOpacity="0.8" />
            </linearGradient>
          </defs>
          {graph.edges.map((edge) => {
            const from = nodeByKey.get(edge.from);
            const to = nodeByKey.get(edge.to);
            if (from === undefined || to === undefined) return null;
            const isIngressEdge = from.kind === "ingress";
            return (
              <g key={edge.key}>
                {/* 背景の静的ライン */}
                <path
                  d={edgePath(from, to)}
                  fill="none"
                  strokeWidth={2}
                  stroke="color-mix(in srgb, var(--border) 80%, transparent)"
                />
                {/* 流れるアニメーションライン */}
                <path
                  d={edgePath(from, to)}
                  fill="none"
                  strokeWidth={2}
                  stroke={isIngressEdge ? "url(#edge-ingress-deploy)" : "url(#edge-deploy-volume)"}
                  strokeDasharray={edge.pending ? "4 4" : undefined}
                  className={cn(!edge.pending && "canvas-flow-line", edge.pending && "opacity-60")}
                />
              </g>
            );
          })}
          {drag !== null && (() => {
            const from = nodeByKey.get(drag.fromKey);
            return from === undefined ? null : (
              <path
                d={`M ${from.x + NODE_WIDTH} ${from.y + NODE_HEIGHT / 2} L ${drag.x} ${drag.y}`}
                fill="none"
                strokeWidth={2}
                strokeDasharray="4 4"
                className="stroke-primary animate-pulse"
              />
            );
          })()}
        </svg>

        {/* 線の言葉(path、マウント先)。 */}
        {graph.edges.map((edge) => {
          const from = nodeByKey.get(edge.from);
          const to = nodeByKey.get(edge.to);
          if (from === undefined || to === undefined) {
            return null;
          }
          const middle = edgeMidpoint(from, to);
          return (
            <span
              key={`${edge.key}-label`}
              className={cn(
                "pointer-events-none absolute -translate-x-1/2 -translate-y-1/2 rounded-full border border-border/90 bg-card/95 px-2 py-0.5 font-mono text-[10px] text-muted-foreground shadow-xs backdrop-blur-xs",
                edge.pending && "border-amber-500/60 bg-amber-950/20 text-amber-400 font-semibold"
              )}
              style={{ left: middle.x, top: middle.y }}
            >
              {edge.label}
              {edge.pending && " (未適用)"}
            </span>
          );
        })}

        {/* カード。 */}
        {graph.nodes.map((node) => (
          <NodeCard
            key={node.key}
            node={node}
            projectId={projectId}
            selected={(selectedKey ?? null) === node.key}
            canConnect={canEdit && node.kind !== "volume"}
            onStartDrag={startDrag}
            ingress={ingressById.get(node.id)}
            deploy={(deploys.data ?? []).find((candidate) => candidate.deployId === node.id)}
            deployDetail={deployDetailById.get(node.id)}
            volume={volumeById.get(node.id)}
            dropTarget={drag !== null && drag.fromKey.split(":")[0] === "ingress" ? node.kind === "deploy" : drag !== null && drag.fromKey.split(":")[0] === "deploy" ? node.kind === "volume" : false}
          />
        ))}

        {/* 何も無いとき。 */}
        {empty && (
          <div className="absolute inset-x-0 top-24 mx-auto flex w-fit flex-col items-center gap-3 rounded-xl border border-border bg-card/90 p-8 text-center shadow-lg backdrop-blur-sm">
            <div className="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <Rocket className="size-5" />
            </div>
            <p className="font-semibold text-foreground">まだ何もありません</p>
            <p className="text-xs text-muted-foreground">deploy を作成すると、ここにカードが配置され、視覚的に連携できます。</p>
            {canEdit && (
              <Button asChild size="sm" className="mt-1 shadow-sm">
                <Link to={`/projects/${projectId}/deploys/new`}><Plus aria-hidden="true" />deploy を作る</Link>
              </Button>
            )}
          </div>
        )}
      </div>

      <ConnectDialog
        open={connection !== null}
        title={connection?.kind === "deploy-volume" ? "volume をマウントする" : "ingress から、deploy へ、つなぐ"}
        description={connection?.kind === "deploy-volume" ? "保存すると、適用前の変更になります。deploy のパネルで「適用」すると、コンテナが再起動します。" : "保存すると、適用前の変更になります。ingress のパネルで「適用」すると、公開されます。"}
        submitLabel="つなぐ(保存)"
        canSubmit={blockedReason === null}
        busy={connect.isPending}
        error={connect.error}
        onSubmit={submitConnection}
        onCancel={() => { setConnection(null); connect.reset(); }}
      >
        {blockedReason !== null && <p role="alert" className="text-sm text-destructive">{blockedReason}</p>}
        {blockedReason === null && connection?.kind === "ingress-deploy" && (
          <>
            <div className="flex flex-col gap-2">
              <Label htmlFor="connectPath">path</Label>
              <Input id="connectPath" value={path} onChange={(event) => setPath(event.target.value)} className="font-mono text-xs" />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="connectPort">ポート(TCP)</Label>
              <select id="connectPort" value={portChoice} onChange={(event) => setPortChoice(event.target.value)} className="h-9 rounded-md border bg-background px-3 text-sm shadow-xs">
                {tcpPorts.map((port) => <option key={port.name} value={port.port}>{port.name}({port.port})</option>)}
              </select>
            </div>
          </>
        )}
        {blockedReason === null && connection?.kind === "deploy-volume" && (
          <div className="flex flex-col gap-2">
            <Label htmlFor="connectMountPath">マウント先のパス</Label>
            <Input id="connectMountPath" value={mountPath} onChange={(event) => setMountPath(event.target.value)} className="font-mono text-xs" />
          </div>
        )}
        {validation !== null && <p role="alert" className="text-sm text-destructive">{validation}</p>}
      </ConnectDialog>
    </div>
  );
}

/**
 * カード1枚。押すと、右のパネルが開く。ingress と deploy には、右端に、線を引き始める点がある。
 *
 * @param props.node カードの位置と種類。
 * @param props.projectId プロジェクトの ID。
 * @param props.selected いま開いているか。
 * @param props.canConnect 線を引き始められるか。
 * @param props.dropTarget 線を引いている最中で、つなげる相手か(目印を付ける)。
 * @param props.onStartDrag 線を引き始めるとき。
 * @param props.ingress ingress の情報(ingress のカードのとき)。
 * @param props.deploy deploy の一覧の1件(deploy のカードのとき)。
 * @param props.deployDetail deploy の詳細(届いていれば)。
 * @param props.volume volume の情報(volume のカードのとき)。
 */
function NodeCard(props: {
  node: GraphNode;
  projectId: string;
  selected: boolean;
  canConnect: boolean;
  dropTarget: boolean;
  onStartDrag: (event: PointerEvent<HTMLButtonElement>, fromKey: string) => void;
  ingress: Ingress | undefined;
  deploy: { deployId: string; imageRef: string; status: string; spec: string; replicas: number } | undefined;
  deployDetail: DeployDetail | undefined;
  volume: Volume | undefined;
}) {
  const { node } = props;
  // 種類ごとに、リンク先、アイコン、題、補足、状態を決める。
  let to = "";
  let title = "";
  let subtitle = "";
  let badge: { label: string; tone: "success" | "warning" | "destructive" | "default" } = { label: "", tone: "default" };
  let Icon = Rocket;
  let iconTheme = "bg-muted text-muted-foreground border-border";
  let activeLed = false;
  let extraTag: string | null = null;

  if (node.kind === "ingress" && props.ingress !== undefined) {
    to = `/projects/${props.projectId}/ingresses/${node.id}`;
    title = props.ingress.host;
    subtitle = `ルール ${props.ingress.rules.length} 件${props.ingress.pending_rules !== null ? "(未適用あり)" : ""}`;
    badge = { label: ingressStatusLabel(props.ingress.status), tone: ingressStatusTone(props.ingress.status) };
    Icon = Globe;
    iconTheme = "bg-emerald-500/10 text-emerald-500 border-emerald-500/20";
    activeLed = props.ingress.status === "active";
    extraTag = "SSL";
  } else if (node.kind === "deploy" && props.deploy !== undefined) {
    const status = props.deployDetail?.status ?? props.deploy.status;
    const service = props.deployDetail?.service;
    to = `/projects/${props.projectId}/deploys/${node.id}`;
    title = shortImageName(props.deploy.imageRef);
    subtitle = service && service.ports.length > 0
      ? `${props.deploy.spec} · ${props.deployDetail?.replicas ?? props.deploy.replicas}台 · :${service.ports.map((p) => p.port).join(",")}`
      : `${props.deploy.spec} · ${props.deployDetail?.replicas ?? props.deploy.replicas} 台`;
    badge = { label: deployStatusLabel(status), tone: deployStatusTone(status) };
    Icon = Rocket;
    iconTheme = "bg-indigo-500/10 text-indigo-400 border-indigo-500/20";
    activeLed = status === "active";
    if (service && service.ports.length > 0) {
      extraTag = `${service.ports[0]?.port}/${service.ports[0]?.protocol}`;
    }
  } else if (node.kind === "volume" && props.volume !== undefined) {
    to = `/projects/${props.projectId}/volumes/${node.id}`;
    title = props.volume.name;
    subtitle = `${formatMegabytes(props.volume.sizeMB)}${props.volume.pending_sizeMB !== null ? ` → ${formatMegabytes(props.volume.pending_sizeMB)}` : ""}`;
    badge = { label: volumeStatusLabel(props.volume.status), tone: volumeStatusTone(props.volume.status) };
    Icon = HardDrive;
    iconTheme = "bg-purple-500/10 text-purple-400 border-purple-500/20";
    activeLed = props.volume.status === "active";
  }

  return (
    <div
      data-node-key={nodeKey(node.kind, node.id)}
      className={cn(
        "group absolute rounded-xl border border-border/80 bg-card text-card-foreground shadow-xs transition-all duration-200 hover:border-zinc-500/60 hover:shadow-md",
        props.selected && "ring-2 ring-primary border-primary/50 shadow-md",
        props.dropTarget && "ring-2 ring-indigo-500/60 border-indigo-500/40"
      )}
      style={{ left: node.x, top: node.y, width: NODE_WIDTH, height: NODE_HEIGHT }}
    >
      <Link to={to} className="flex h-full flex-col justify-between rounded-xl p-3 outline-none focus-visible:ring-[3px] focus-visible:ring-ring/40">
        <div className="flex items-start justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <span className={cn("grid size-7 shrink-0 place-items-center rounded-lg border", iconTheme)}>
              <Icon aria-hidden="true" className="size-3.5" />
            </span>
            <span className={cn("min-w-0 truncate text-xs font-semibold tracking-tight", (node.kind === "ingress" || node.kind === "deploy") && "font-mono")}>
              {title}
            </span>
          </div>

          <div className="flex shrink-0 items-center gap-1.5 pt-0.5">
            {extraTag && (
              <span className="rounded bg-emerald-950/60 border border-emerald-900/60 px-1 py-0.2 font-mono text-[9px] text-emerald-400 font-medium">
                {extraTag}
              </span>
            )}
            {activeLed && (
              <span className="relative flex size-2">
                <span className="absolute inline-flex size-full animate-ping rounded-full bg-emerald-400 opacity-60" />
                <span className="relative inline-flex size-2 rounded-full bg-emerald-500" />
              </span>
            )}
          </div>
        </div>

        <div className="flex items-center justify-between gap-2 pt-2 border-t border-border/40">
          <span className="truncate font-mono text-[11px] text-muted-foreground">{subtitle}</span>
          <Badge variant={badge.tone} className="shrink-0 text-[10px] px-1.5 py-0">
            {badge.label}
          </Badge>
        </div>
      </Link>

      {props.canConnect && (
        <button
          type="button"
          aria-label={`${title} から、線を引いてつなぐ`}
          onPointerDown={(event) => props.onStartDrag(event, nodeKey(node.kind, node.id))}
          className="absolute top-1/2 -right-2 size-4 -translate-y-1/2 cursor-crosshair touch-none rounded-full border-2 border-primary bg-background shadow-xs opacity-0 transition-all duration-150 hover:scale-125 group-hover:opacity-100 focus-visible:opacity-100"
        />
      )}
    </div>
  );
}
