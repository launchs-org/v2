// 右のパネルに出す、deploy・ingress・volume の作成フォーム。
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Globe, HardDrive, Plus, Rocket } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useProjectCrumbs } from "./useProjectCrumbs";
import { Card, CardContent } from "./ui/card";
import { createDeploy, createIngress, createVolume, getQuota } from "../api/endpoints";
import { useSession } from "../auth/AuthProvider";
import { DEPLOY_SPECS, validateImageRef } from "../lib/deploy";
import { DEFAULT_VOLUME_SIZE_MB, validateVolumeName, validateVolumeSize } from "../lib/volume";
import { ErrorMessage } from "./ErrorMessage";
import { PageHeader } from "./PageHeader";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { Spinner } from "./ui/spinner";

/** quota が取れないときに使う、volume の大きさの上限(MB。サーバーの既定と同じ)。 */
const FALLBACK_MAX_SIZE_MB = 5120;

/** deploy の作成フォーム(イメージとスペック)。作ったら、その deploy のパネルを開く。 */
/** deploy の作成の画面(URL の projectId を使う)。 */
export function CreateDeployPage() {
  const { projectId = "" } = useParams();
  return <CreateDeployForm projectId={projectId} />;
}

function CreateDeployForm({ projectId }: { projectId: string }) {
  // パンくず用(プロジェクトまで)。
  const crumbs = useProjectCrumbs(projectId);
  const session = useSession();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [imageRef, setImageRef] = useState("");
  const [spec, setSpec] = useState<string>("small");
  const [validation, setValidation] = useState<string | null>(null);
  const create = useMutation({
    mutationFn: () => createDeploy(session.http, { projectId, imageRef: imageRef.trim(), spec }),
    onSuccess: async (created) => {
      await Promise.all([queryClient.invalidateQueries({ queryKey: ["deploys", projectId] }), queryClient.invalidateQueries({ queryKey: ["quota"] })]);
      navigate(`/projects/${projectId}/deploys/${created.deployId}`);
    },
  });

  /**
   * フォームが送信されたとき: 検査して、作成を頼む。
   *
   * @param event フォームの送信イベント。
   */
  function handleSubmit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    // 手元で検査してから送る(サーバーも確かめる)。
    const problem = validateImageRef(imageRef.trim());
    setValidation(problem);
    if (problem === null) {
      create.mutate();
    }
  }

  return (
    <>
      <PageHeader crumbs={[...crumbs, { label: "deploy", to: `/projects/${projectId}/deploys` }, { label: "作成" }]} title={<span className="flex items-center gap-2"><Rocket aria-hidden="true" className="size-5" />deploy を作る</span>} description="イメージから、コンテナを起動します。" />
      <Card className="max-w-xl"><CardContent className="p-6">
      <form className="flex flex-col gap-4" onSubmit={handleSubmit}>
        <div className="flex flex-col gap-2">
          <Label htmlFor="newDeployImage">イメージ</Label>
          <Input id="newDeployImage" value={imageRef} onChange={(event) => setImageRef(event.target.value)} placeholder="registry.example/app:1.0" aria-invalid={validation !== null} />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="newDeploySpec">スペック</Label>
          <select id="newDeploySpec" value={spec} onChange={(event) => setSpec(event.target.value)} className="h-9 w-fit rounded-md border bg-background px-3 text-sm shadow-xs">
            {DEPLOY_SPECS.map((candidate) => <option key={candidate} value={candidate}>{candidate}</option>)}
          </select>
        </div>
        {validation !== null && <p role="alert" className="text-sm text-destructive">{validation}</p>}
        <ErrorMessage error={create.error} />
        <Button type="submit" className="w-fit" disabled={create.isPending}>
          {create.isPending ? <Spinner /> : <Plus aria-hidden="true" />}
          {create.isPending ? "作成中…" : "deploy を作る"}
        </Button>
      </form>
      </CardContent></Card>
    </>
  );
}

/** ingress の作成(ホスト名は自動で払い出される)。作ったら、その ingress のパネルを開く。 */
/** ingress の作成の画面(URL の projectId を使う)。 */
export function CreateIngressPage() {
  const { projectId = "" } = useParams();
  return <CreateIngressForm projectId={projectId} />;
}

function CreateIngressForm({ projectId }: { projectId: string }) {
  // パンくず用(プロジェクトまで)。
  const crumbs = useProjectCrumbs(projectId);
  const session = useSession();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const create = useMutation({
    mutationFn: () => createIngress(session.http, projectId),
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: ["ingresses", projectId] });
      navigate(`/projects/${projectId}/ingresses/${created.ingressId}`);
    },
  });
  return (
    <>
      <PageHeader crumbs={[...crumbs, { label: "ingress", to: `/projects/${projectId}/ingresses` }, { label: "作成" }]} title={<span className="flex items-center gap-2"><Globe aria-hidden="true" className="size-5" />ingress を作る</span>} description="外から、HTTP で、サービスに届くようにします。ホスト名は、自動で決まります。" />
      <Card className="max-w-xl"><CardContent className="p-6">
      <div className="flex flex-col gap-4">
        <ErrorMessage error={create.error} />
        <Button className="w-fit" onClick={() => create.mutate()} disabled={create.isPending}>
          {create.isPending ? <Spinner /> : <Plus aria-hidden="true" />}
          {create.isPending ? "作成中…" : "ingress を作る"}
        </Button>
      </div>
      </CardContent></Card>
    </>
  );
}

/** volume の作成フォーム(名前と大きさ)。作ったら、その volume のパネルを開く。 */
/** volume の作成の画面(URL の projectId を使う)。 */
export function CreateVolumePage() {
  const { projectId = "" } = useParams();
  return <CreateVolumeForm projectId={projectId} />;
}

function CreateVolumeForm({ projectId }: { projectId: string }) {
  // パンくず用(プロジェクトまで)。
  const crumbs = useProjectCrumbs(projectId);
  const session = useSession();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [sizeText, setSizeText] = useState(String(DEFAULT_VOLUME_SIZE_MB));
  const [validation, setValidation] = useState<string | null>(null);
  // 上限(volume 1つの最大の大きさ)を、quota から取る。
  const quota = useQuery({ queryKey: ["quota"], queryFn: ({ signal }) => getQuota(session.http, signal) });
  const maxSizeMB = quota.data?.maxVolumeSizeMB ?? FALLBACK_MAX_SIZE_MB;
  const create = useMutation({
    mutationFn: () => createVolume(session.http, projectId, { name: name.trim(), sizeMB: Number(sizeText) }),
    onSuccess: async (created) => {
      await Promise.all([queryClient.invalidateQueries({ queryKey: ["volumes", projectId] }), queryClient.invalidateQueries({ queryKey: ["quota"] })]);
      navigate(`/projects/${projectId}/volumes/${created.volumeId}`);
    },
  });

  /**
   * フォームが送信されたとき: 名前と大きさを検査して、作成を頼む。
   *
   * @param event フォームの送信イベント。
   */
  function handleSubmit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    // 手元で検査してから送る(サーバーも確かめる)。
    const problem = validateVolumeName(name.trim()) ?? validateVolumeSize(sizeText, maxSizeMB);
    setValidation(problem);
    if (problem === null) {
      create.mutate();
    }
  }

  return (
    <>
      <PageHeader crumbs={[...crumbs, { label: "volume", to: `/projects/${projectId}/volumes` }, { label: "作成" }]} title={<span className="flex items-center gap-2"><HardDrive aria-hidden="true" className="size-5" />volume を作る</span>} description="データを残しておく、永続ストレージです。deploy を消しても残ります。" />
      <Card className="max-w-xl"><CardContent className="p-6">
      <form className="flex flex-col gap-4" onSubmit={handleSubmit}>
        <div className="flex flex-col gap-2">
          <Label htmlFor="newVolumeName">volume の名前</Label>
          <Input id="newVolumeName" value={name} onChange={(event) => setName(event.target.value)} maxLength={64} placeholder="db-data" aria-invalid={validation !== null} />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="newVolumeSize">大きさ(MB)</Label>
          <Input id="newVolumeSize" inputMode="numeric" value={sizeText} onChange={(event) => setSizeText(event.target.value)} className="w-40" />
          <p className="text-xs text-muted-foreground">大きさは、{maxSizeMB} MB まで。作ったあとは、拡大だけできます。</p>
        </div>
        {validation !== null && <p role="alert" className="text-sm text-destructive">{validation}</p>}
        <ErrorMessage error={create.error} />
        <Button type="submit" className="w-fit" disabled={create.isPending}>
          {create.isPending ? <Spinner /> : <Plus aria-hidden="true" />}
          {create.isPending ? "作成中…" : "volume を作る"}
        </Button>
      </form>
      </CardContent></Card>
    </>
  );
}
