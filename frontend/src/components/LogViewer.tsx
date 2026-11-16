// Pod 1つのログを、続きを取り続けて、見せる。
import { useEffect, useRef, useState } from "react";
import { getDeployLogs } from "../api/endpoints";
import { describeError } from "../api/api_error";
import type { DeployLogPod } from "../api/types";
import { useSession } from "../auth/AuthProvider";
import { Alert } from "./ui/alert";

/** 新しいログが無いとき、次に確かめるまでの間隔(ミリ秒)。 */
const IDLE_POLL_MS = 2000;

/** 画面に持つ、ログの最大の長さ(文字数)。超えたら、古い方を捨てる(ブラウザを重くしないため)。 */
const MAX_LOG_CHARACTERS = 500_000;

/**
 * ログを見せる。Pod を選ぶと、その Pod のログを先頭から取り、末尾に追いつくまで続けて取る。
 * 稼働中の Pod は、新しい行を、定期的に取り続ける。終わった Pod は、末尾まで読んだら止める。
 *
 * @param props.deployId deploy の ID。
 * @param props.pods ログのある Pod の一覧(新しい順)。
 */
export function LogViewer({ deployId, pods }: { deployId: string; pods: DeployLogPod[] }) {
  const session = useSession();
  const [selectedPodId, setSelectedPodId] = useState<string | null>(null);
  const [text, setText] = useState("");
  const [error, setError] = useState<string | null>(null);
  const outputRef = useRef<HTMLPreElement>(null);

  // 選んでいなければ、一番新しい Pod を使う。選んだ Pod が一覧から消えたら、新しい Pod に戻す。
  const selected = pods.find((pod) => pod.podId === selectedPodId) ?? pods[0];
  const podId = selected?.podId;
  const running = selected?.running ?? false;

  // Pod が決まったら、ログを取り始める。Pod か、稼働の状態が変わったら、やり直す(古い取得は、中断する)。
  useEffect(() => {
    if (podId === undefined) {
      return;
    }
    const controller = new AbortController();
    let since: number | undefined;
    let timer: ReturnType<typeof setTimeout> | undefined;
    setText("");
    setError(null);

    /** 続きを1回取り、まだあれば直ぐに、無ければ待ってから(稼働中のときだけ)、もう1回取る。 */
    async function pull(): Promise<void> {
      try {
        const chunk = await getDeployLogs(session.http, deployId, podId as string, since, controller.signal);
        since = chunk.nextSince;
        setError(null);
        if (chunk.logs.length > 0) {
          // 長くなりすぎたら、古い方を捨てて、つなぐ。
          setText((previous) => (previous + chunk.logs).slice(-MAX_LOG_CHARACTERS));
          // 続きがありそうなので、待たずに取る。
          void pull();
          return;
        }
        // 末尾まで読んだ。稼働中の Pod だけ、新しい行を待ち続ける。
        if (running) {
          timer = setTimeout(() => void pull(), IDLE_POLL_MS);
        }
      } catch (caught) {
        // 中断(画面を離れた、Pod を変えた)は、エラーにしない。
        if (controller.signal.aborted) {
          return;
        }
        setError(describeError(caught));
        // 一時的な失敗かもしれないので、稼働中なら、また試す。
        if (running) {
          timer = setTimeout(() => void pull(), IDLE_POLL_MS * 2);
        }
      }
    }
    void pull();

    // 片付け: 取得を中断し、待っている次の取得を取り消す。
    return () => {
      controller.abort();
      if (timer !== undefined) {
        clearTimeout(timer);
      }
    };
  }, [session, deployId, podId, running]);

  // ログが増えたら、末尾が見えるように、スクロールする。
  useEffect(() => {
    const output = outputRef.current;
    if (output !== null) {
      output.scrollTop = output.scrollHeight;
    }
  }, [text]);

  if (selected === undefined) {
    return <p className="text-sm text-muted-foreground">ログのある Pod は、まだありません(起動前、または、出力がありません)。</p>;
  }
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <label htmlFor="logPod" className="text-sm text-muted-foreground">Pod</label>
        <select id="logPod" value={selected.podId} onChange={(event) => setSelectedPodId(event.target.value)} className="h-9 max-w-full rounded-md border bg-background px-3 font-mono text-xs shadow-xs">
          {pods.map((pod) => <option key={pod.podId} value={pod.podId}>{pod.podId}{pod.running ? "(稼働中)" : "(終了)"}</option>)}
        </select>
      </div>
      {error !== null && <Alert>{error}</Alert>}
      <pre ref={outputRef} aria-label="ログの出力" tabIndex={0} className="h-96 overflow-auto rounded-lg border bg-zinc-950 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all text-zinc-100">
        {text.length > 0 ? text : "(まだログがありません)"}
      </pre>
    </div>
  );
}
