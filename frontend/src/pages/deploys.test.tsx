import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { errorResponse, jsonResponse, type RecordedRequest } from "../test/fake_server";
import { appServer, renderApp } from "../test/render_app";

/** deploy の API を持つ、偽のサーバーの、上書きを作る。 */
function deployOverrides(options: { status?: string; replicas?: number; logs?: string[] } = {}) {
  const state = { status: options.status ?? "active", replicas: options.replicas ?? 1, deploys: [] as unknown[], config: { env: {}, pending_env: null, command: null, pending_command: null, args: null, pending_args: null } as Record<string, unknown>, volumes: { mounts: [], pending_mounts: null } as Record<string, unknown>, service: null as Record<string, unknown> | null };
  const logs = [...(options.logs ?? [])];
  const detail = () => ({
    deployId: "deploy-1",
    projectId: "project-a",
    status: state.status,
    spec: "small",
    replicas: state.replicas,
    pods: [{ podId: "pod-aaa", status: "running" }],
    logPods: [{ podId: "pod-aaa", running: false, startedAt: "2026-09-16T09:00:00+09:00", endedAt: "2026-09-16T09:30:00+09:00" }],
    imageRef: "registry.example/app:1.0",
    createdAt: "2026-09-16T09:00:00+09:00",
    config: state.config,
    volumes: state.volumes,
    service: state.service,
  });
  const overrides = {
    "GET /api/v1/projects/project-a/deploys": () => jsonResponse(200, { deploys: state.deploys, nextCursor: null }),
    "POST /api/v1/deploy": (request: RecordedRequest) => {
      state.deploys = [{ deployId: "deploy-1", status: "pending", spec: (request.body as { spec: string }).spec, replicas: 1, imageRef: (request.body as { imageRef: string }).imageRef, createdAt: "2026-09-16T09:00:00+09:00" }];
      return jsonResponse(200, { deployId: "deploy-1" });
    },
    "GET /api/v1/deploy/deploy-1": () => jsonResponse(200, detail()),
    "POST /api/v1/deploy/deploy-1/scale": (request: RecordedRequest) => {
      state.replicas = (request.body as { replicas: number }).replicas;
      state.status = "applying";
      return jsonResponse(200, { deployId: "deploy-1", replicas: state.replicas });
    },
    "DELETE /api/v1/deploy/deploy-1": () => {
      state.status = "deleting";
      return jsonResponse(200, {});
    },
    "GET /api/v1/deploy/deploy-1/logs": () => {
      // 1回目で本文を返し、その後は空(末尾に追いついた)を返す。
      const next = logs.shift();
      return jsonResponse(200, { logs: next ?? "", nextSince: 1 });
    },
  };
  return { overrides, state };
}

const PROJECTS = [{ projectId: "project-a", name: "app-a", status: "active" }];

describe("deploy の画面", () => {
  it("プロジェクトの画面で、deploy を作ると、その deploy の画面へ進む", async () => {
    const user = userEvent.setup();
    const { overrides } = deployOverrides();
    const { requests } = renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/projects/project-a" });
    expect(await screen.findByText("deploy はまだありません")).toBeInTheDocument();
    await user.type(screen.getByLabelText("イメージ"), "registry.example/app:1.0");
    await user.selectOptions(screen.getByLabelText("スペック"), "medium");
    await user.click(screen.getByRole("button", { name: "deploy を作る" }));
    expect(await screen.findByRole("heading", { level: 1, name: /deploy/ })).toBeInTheDocument();
    expect(requests.find((request) => request.path === "/api/v1/deploy" && request.method === "POST")?.body).toEqual({ projectId: "project-a", imageRef: "registry.example/app:1.0", spec: "medium" });
    // パンくずに、元のプロジェクトの名前が出る。
    expect(screen.getByRole("link", { name: "app-a" })).toBeInTheDocument();
  });

  it("イメージが空なら、送らずに、理由を出す", async () => {
    const user = userEvent.setup();
    const { overrides } = deployOverrides();
    const { requests } = renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/projects/project-a" });
    await user.click(await screen.findByRole("button", { name: "deploy を作る" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("イメージを入力してください");
    expect(requests.some((request) => request.method === "POST" && request.path === "/api/v1/deploy")).toBe(false);
  });

  it("上限を超えたなど、作成の失敗(409)は、サーバーのメッセージを出す", async () => {
    const user = userEvent.setup();
    const { overrides } = deployOverrides();
    renderApp(appServer({ projects: PROJECTS }, { ...overrides, "POST /api/v1/deploy": () => errorResponse(409, "conflict", "deploy の数が上限です") }), { loggedIn: true, path: "/projects/project-a" });
    await user.type(await screen.findByLabelText("イメージ"), "app:1");
    await user.click(screen.getByRole("button", { name: "deploy を作る" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("deploy の数が上限です");
  });

  it("詳細: 状態、Pod、ログが出る", async () => {
    const { overrides } = deployOverrides({ logs: ["2026-09-16T09:00:01.000+09:00 started\n"] });
    renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/deploys/deploy-1" });
    expect((await screen.findAllByText("稼働中")).length).toBeGreaterThan(0);
    expect(screen.getByText("pod-aaa", { selector: "code" })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByLabelText("ログ")).toHaveTextContent("started"));
  });

  it("スケール: 台数を選んで変更すると、サーバーに頼み、反映中になる", async () => {
    const user = userEvent.setup();
    const { overrides } = deployOverrides();
    const { requests } = renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/deploys/deploy-1" });
    await user.selectOptions(await screen.findByLabelText("台数"), "3");
    await user.click(screen.getByRole("button", { name: "変更" }));
    await waitFor(() => expect(requests.find((request) => request.path.endsWith("/scale"))?.body).toEqual({ replicas: 3 }));
    expect(await screen.findByText("状態が変わるのを待っています…")).toBeInTheDocument();
    // 反映中は、スケールも、削除もできない。
    expect(screen.getByLabelText("台数")).toBeDisabled();
    expect(screen.getByRole("button", { name: "deploy を削除" })).toBeDisabled();
  });

  it("削除: 確認してから頼み、削除中になる", async () => {
    const user = userEvent.setup();
    const { overrides } = deployOverrides();
    const { requests } = renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/deploys/deploy-1" });
    await user.click(await screen.findByRole("button", { name: "deploy を削除" }));
    // 確認の前に、頼まない。
    expect(requests.some((request) => request.method === "DELETE")).toBe(false);
    await user.click(screen.getByRole("button", { name: "削除する" }));
    expect(await screen.findByText(/削除しています/)).toBeInTheDocument();
    expect(requests.some((request) => request.method === "DELETE" && request.path === "/api/v1/deploy/deploy-1")).toBe(true);
  });

  it("存在しない deploy は、見つからない旨と、戻る道を出す", async () => {
    renderApp(appServer({ projects: PROJECTS }, { "GET /api/v1/deploy/deploy-x": () => errorResponse(404, "not_found", "no") }), { loggedIn: true, path: "/deploys/deploy-x" });
    expect(await screen.findByText(/deploy が見つかりません/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "プロジェクトの一覧へ" })).toBeInTheDocument();
  });

  it("起動設定: 環境変数を足して保存すると、全件を送り、適用前の変更として、適用と破棄が出る", async () => {
    const user = userEvent.setup();
    const { overrides, state } = deployOverrides();
    const { requests } = renderApp(
      appServer({ projects: PROJECTS }, {
        ...overrides,
        "PUT /api/v1/deploy/deploy-1/config": (request: RecordedRequest) => {
          const body = request.body as { env: Record<string, string>; command: string | null; args: string[] | null };
          state.config = { env: {}, pending_env: body.env, command: null, pending_command: body.command, args: null, pending_args: body.args };
          return jsonResponse(200, {});
        },
        "POST /api/v1/deploy/deploy-1/config/apply": () => jsonResponse(200, {}),
      }),
      { loggedIn: true, path: "/deploys/deploy-1" },
    );
    await user.click(await screen.findByRole("button", { name: "環境変数を足す" }));
    await user.type(screen.getByLabelText("環境変数のキー 1"), "PORT");
    await user.type(screen.getByLabelText("環境変数の値 1"), "8080");
    await user.type(screen.getByLabelText("起動コマンド"), "/app/server");
    await user.type(screen.getByLabelText(/引数/), "--verbose");
    await user.click(screen.getAllByRole("button", { name: "保存(適用前の変更にする)" })[0] as HTMLElement);
    await waitFor(() => expect(requests.find((request) => request.method === "PUT" && request.path.endsWith("/config"))?.body).toEqual({ env: { PORT: "8080" }, command: "/app/server", args: ["--verbose"] }));
    // 保存のあと、適用と破棄が出る。適用すると、サーバーに頼む。
    await user.click(await screen.findByRole("button", { name: "適用" }));
    await waitFor(() => expect(requests.some((request) => request.path.endsWith("/config/apply"))).toBe(true));
    expect(screen.getByRole("button", { name: "破棄" })).toBeInTheDocument();
  });

  it("起動設定: 環境変数のキーが重複していたら、送らずに、理由を出す", async () => {
    const user = userEvent.setup();
    const { overrides } = deployOverrides();
    const { requests } = renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/deploys/deploy-1" });
    const add = await screen.findByRole("button", { name: "環境変数を足す" });
    await user.click(add);
    await user.click(add);
    await user.type(screen.getByLabelText("環境変数のキー 1"), "A");
    await user.type(screen.getByLabelText("環境変数のキー 2"), "A");
    await user.click(screen.getAllByRole("button", { name: "保存(適用前の変更にする)" })[0] as HTMLElement);
    expect(await screen.findByRole("alert")).toHaveTextContent("重複");
    expect(requests.some((request) => request.method === "PUT")).toBe(false);
  });

  it("反映中は、設定の編集も、保存もできない", async () => {
    const { overrides } = deployOverrides({ status: "applying" });
    renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/deploys/deploy-1" });
    expect(await screen.findByLabelText("起動コマンド")).toBeDisabled();
    for (const button of screen.getAllByRole("button", { name: "保存(適用前の変更にする)" })) {
      expect(button).toBeDisabled();
    }
  });

  it("サービス: 無ければ作成フォームが出て、ポートを送って作る。作ったあとは、DNS 名などが出る", async () => {
    const user = userEvent.setup();
    const { overrides, state } = deployOverrides();
    const { requests } = renderApp(
      appServer({ projects: PROJECTS }, {
        ...overrides,
        "POST /api/v1/deploy/deploy-1/service": () => {
          state.service = { serviceId: "service-1", ports: [{ name: "web", port: 8080, protocol: "tcp" }], pending_ports: null, status: "active", dnsName: "service-1.project-a.svc.local", clusterIP: "10.0.0.1" };
          return jsonResponse(200, state.service);
        },
      }),
      { loggedIn: true, path: "/deploys/deploy-1" },
    );
    await user.click(await screen.findByRole("button", { name: "サービスを作る" }));
    await waitFor(() => expect(requests.find((request) => request.method === "POST" && request.path.endsWith("/service"))?.body).toEqual({ ports: [{ name: "web", port: 8080, protocol: "tcp" }] }));
    expect(await screen.findByText("service-1.project-a.svc.local")).toBeInTheDocument();
  });

  it("サービス: ポートの名前が規則に合わなければ、送らない。pending があれば、適用と破棄が出る", async () => {
    const user = userEvent.setup();
    const { overrides, state } = deployOverrides();
    state.service = { serviceId: "service-1", ports: [{ name: "web", port: 8080, protocol: "tcp" }], pending_ports: [{ name: "web", port: 9090, protocol: "tcp" }], status: "active", dnsName: "d", clusterIP: "1.1.1.1" };
    const { requests } = renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/deploys/deploy-1" });
    // pending の内容(9090)が、編集欄の初期値になる。
    expect(await screen.findByDisplayValue("9090")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "適用" }).length).toBeGreaterThan(0);
    const name = screen.getByLabelText("ポートの名前 1");
    await user.clear(name);
    await user.type(name, "Bad_Name");
    await user.click(screen.getAllByRole("button", { name: "保存(適用前の変更にする)" }).at(-1) as HTMLElement);
    expect(await screen.findByRole("alert")).toHaveTextContent("Bad_Name");
    expect(requests.some((request) => request.method === "PATCH")).toBe(false);
  });

  it("マウント: volume を選んでパスを入れて保存すると、全件を送る", async () => {
    const user = userEvent.setup();
    const { overrides } = deployOverrides();
    const { requests } = renderApp(
      appServer({ projects: PROJECTS }, {
        ...overrides,
        "GET /api/v1/projects/project-a/volumes": () => jsonResponse(200, { volumes: [{ volumeId: "volume-1", name: "db-data", status: "active", sizeMB: 1024, pending_sizeMB: null }], nextCursor: null }),
        "PUT /api/v1/deploy/deploy-1/mounts": () => jsonResponse(200, {}),
      }),
      { loggedIn: true, path: "/deploys/deploy-1" },
    );
    await user.click(await screen.findByRole("button", { name: "マウントを足す" }));
    await user.selectOptions(screen.getByLabelText("マウントする volume 1"), "volume-1");
    await user.type(screen.getByLabelText("マウント先のパス 1"), "/data");
    const saves = screen.getAllByRole("button", { name: "保存(適用前の変更にする)" });
    await user.click(saves[1] as HTMLElement);
    await waitFor(() => expect(requests.find((request) => request.method === "PUT" && request.path.endsWith("/mounts"))?.body).toEqual({ mounts: [{ volumeId: "volume-1", mountPath: "/data" }] }));
  });
});

