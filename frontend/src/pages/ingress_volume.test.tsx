import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { errorResponse, jsonResponse, type RecordedRequest } from "../test/fake_server";
import { appServer, renderApp } from "../test/render_app";

const PROJECTS = [{ projectId: "project-a", name: "app-a", status: "active" }];

/** volume の API を持つ、偽のサーバーの、上書きを作る。 */
function volumeOverrides(initial: { status?: string; sizeMB?: number; pending?: number | null; mountedBy?: string[] } = {}) {
  const state = { status: initial.status ?? "active", sizeMB: initial.sizeMB ?? 1024, pending: initial.pending ?? null, mountedBy: initial.mountedBy ?? [], created: [] as unknown[] };
  const detail = () => ({ volumeId: "volume-1", name: "db-data", status: state.status, sizeMB: state.sizeMB, pending_sizeMB: state.pending, mountedBy: state.mountedBy });
  const overrides = {
    "GET /api/v1/projects/project-a/volumes": () => jsonResponse(200, { volumes: state.created, nextCursor: null }),
    "POST /api/v1/projects/project-a/volumes": (request: RecordedRequest) => {
      const body = request.body as { name: string; sizeMB: number };
      state.created = [{ volumeId: "volume-1", name: body.name, status: "active", sizeMB: body.sizeMB, pending_sizeMB: null }];
      return jsonResponse(200, { volumeId: "volume-1", name: body.name, status: "active", pending_sizeMB: null, sizeMB: body.sizeMB });
    },
    "GET /api/v1/projects/project-a/volumes/volume-1": () => jsonResponse(200, detail()),
    "PATCH /api/v1/projects/project-a/volumes/volume-1/size": (request: RecordedRequest) => {
      state.pending = (request.body as { sizeMB: number }).sizeMB;
      return jsonResponse(200, {});
    },
    "POST /api/v1/projects/project-a/volumes/volume-1/apply": () => {
      state.status = "applying";
      return jsonResponse(200, {});
    },
    "POST /api/v1/projects/project-a/volumes/volume-1/discard": () => {
      state.pending = null;
      return jsonResponse(200, {});
    },
    "DELETE /api/v1/projects/project-a/volumes/volume-1": () => errorResponse(409, "conflict", "マウント中の volume は削除できません"),
  };
  return { overrides, state };
}

/** ingress の API と、向き先の候補になる deploy・service を持つ、偽のサーバーの、上書きを作る。 */
function ingressOverrides(initial: { status?: string; rules?: unknown[]; pending?: unknown[] | null } = {}) {
  const state = { status: initial.status ?? "active", rules: initial.rules ?? [], pending: initial.pending ?? null, created: [] as unknown[] };
  const detail = () => ({ ingressId: "ingress-1", host: "swift-red-fox.example.test", rules: state.rules, pending_rules: state.pending, status: state.status });
  const overrides = {
    "GET /api/v1/projects/project-a/ingresses": () => jsonResponse(200, { ingresses: state.created, nextCursor: null }),
    "POST /api/v1/projects/project-a/ingresses": () => {
      state.created = [{ ingressId: "ingress-1", host: "swift-red-fox.example.test" }];
      return jsonResponse(200, { ingressId: "ingress-1", host: "swift-red-fox.example.test", rules: [], pending_rules: null, status: "active" });
    },
    "GET /api/v1/projects/project-a/ingresses/ingress-1": () => jsonResponse(200, detail()),
    "PUT /api/v1/projects/project-a/ingresses/ingress-1": (request: RecordedRequest) => {
      state.pending = (request.body as { rules: unknown[] }).rules;
      return jsonResponse(200, {});
    },
    "POST /api/v1/projects/project-a/ingresses/ingress-1/apply": () => {
      state.status = "applying";
      return jsonResponse(200, {});
    },
    "GET /api/v1/projects/project-a/deploys": () => jsonResponse(200, { deploys: [{ deployId: "deploy-1", status: "active", spec: "small", replicas: 1, imageRef: "app:1", createdAt: "2026-09-16T09:00:00+09:00" }], nextCursor: null }),
    "GET /api/v1/deploy/deploy-1/service": () => jsonResponse(200, { serviceId: "service-1", status: "active", ports: [{ name: "web", port: 8080, protocol: "tcp" }, { name: "dns", port: 53, protocol: "udp" }], pending_ports: null, dnsName: null, clusterIP: null }),
  };
  return { overrides, state };
}

describe("volume の画面", () => {
  it("プロジェクトの画面で、volume を作ると、その画面へ進む", async () => {
    const user = userEvent.setup();
    const { overrides } = volumeOverrides();
    const { requests } = renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/projects/project-a/volumes/new" });
    await user.type(await screen.findByLabelText("volume の名前"), "db-data");
    await user.clear(screen.getByLabelText("大きさ(MB)"));
    await user.type(screen.getByLabelText("大きさ(MB)"), "2048");
    await user.click(screen.getByRole("button", { name: "volume を作る" }));
    expect(await screen.findByLabelText("新しい大きさ(MB)")).toBeInTheDocument();
    expect(requests.find((request) => request.method === "POST" && request.path.endsWith("/volumes"))?.body).toEqual({ name: "db-data", sizeMB: 2048 });
  });

  it("名前と大きさが不正なら、送らずに、理由を出す(上限の超過も)", async () => {
    const user = userEvent.setup();
    const { overrides } = volumeOverrides();
    const { requests } = renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/projects/project-a/volumes/new" });
    await user.type(await screen.findByLabelText("volume の名前"), "bad name");
    await user.click(screen.getByRole("button", { name: "volume を作る" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("英数字とハイフン");
    await user.clear(screen.getByLabelText("volume の名前"));
    await user.type(screen.getByLabelText("volume の名前"), "ok");
    await user.clear(screen.getByLabelText("大きさ(MB)"));
    await user.type(screen.getByLabelText("大きさ(MB)"), "9999");
    await user.click(screen.getByRole("button", { name: "volume を作る" }));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("5120"));
    expect(requests.some((request) => request.method === "POST" && request.path.endsWith("/volumes"))).toBe(false);
  });

  it("拡大を保存すると pending になり、適用すると拡張中になる", async () => {
    const user = userEvent.setup();
    const { overrides } = volumeOverrides();
    const { requests } = renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/projects/project-a/volumes/volume-1" });
    await user.type(await screen.findByLabelText("新しい大きさ(MB)"), "2048");
    await user.click(screen.getByRole("button", { name: "拡大を保存" }));
    expect(await screen.findByText(/適用前の変更があります/)).toBeInTheDocument();
    expect(requests.find((request) => request.path.endsWith("/size"))?.body).toEqual({ sizeMB: 2048 });
    await user.click(screen.getByRole("button", { name: "適用" }));
    expect(await screen.findByText(/拡張しています/)).toBeInTheDocument();
    // 拡張中は、拡大も削除も押せない。
    expect(screen.getByRole("button", { name: "拡大を保存" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "volume を削除" })).toBeDisabled();
  });

  it("縮小は、送らずに断る", async () => {
    const user = userEvent.setup();
    const { overrides } = volumeOverrides({ sizeMB: 2048 });
    const { requests } = renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/projects/project-a/volumes/volume-1" });
    await user.type(await screen.findByLabelText("新しい大きさ(MB)"), "1024");
    await user.click(screen.getByRole("button", { name: "拡大を保存" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("縮小はできません");
    expect(requests.some((request) => request.path.endsWith("/size"))).toBe(false);
  });

  it("pending は、破棄できる", async () => {
    const user = userEvent.setup();
    const { overrides } = volumeOverrides({ pending: 4096 });
    renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/projects/project-a/volumes/volume-1" });
    await user.click(await screen.findByRole("button", { name: "破棄" }));
    await waitFor(() => expect(screen.queryByText(/適用前の変更があります/)).not.toBeInTheDocument());
  });

  it("マウント中は、削除を押せず、理由と、deploy へのリンクが出る", async () => {
    const { overrides } = volumeOverrides({ mountedBy: ["deploy-9"] });
    renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/projects/project-a/volumes/volume-1" });
    expect(await screen.findByRole("button", { name: "volume を削除" })).toBeDisabled();
    expect(screen.getByText(/マウントされているため、削除できません/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "deploy-9" })).toHaveAttribute("href", "/deploys/deploy-9");
  });

  it("削除が 409 なら、サーバーのメッセージを出す", async () => {
    const user = userEvent.setup();
    const { overrides } = volumeOverrides();
    renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/projects/project-a/volumes/volume-1" });
    await user.click(await screen.findByRole("button", { name: "volume を削除" }));
    await user.click(screen.getByRole("button", { name: "削除する" }));
    expect(await screen.findByText("マウント中の volume は削除できません")).toBeInTheDocument();
  });
});

describe("ingress の画面", () => {
  it("プロジェクトの画面で、ingress を作ると、その画面へ進む", async () => {
    const user = userEvent.setup();
    const { overrides } = ingressOverrides();
    renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/projects/project-a/ingresses/new" });
    await user.click(await screen.findByRole("button", { name: "ingress を作る" }));
    expect(await screen.findByRole("heading", { name: /swift-red-fox/ })).toBeInTheDocument();
  });

  it("ルールを足して保存すると pending になり、適用すると反映中になる。向き先は tcp だけ", async () => {
    const user = userEvent.setup();
    const { overrides } = ingressOverrides();
    const { requests } = renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/projects/project-a/ingresses/ingress-1" });
    await user.click(await screen.findByRole("button", { name: "ルールを足す" }));
    const select = await screen.findByLabelText("向き先 1");
    // udp のポートは、選択肢に無い。tcp のポートだけがある。
    await waitFor(() => expect(screen.getByRole("option", { name: "app:1 / web(8080)" })).toBeInTheDocument());
    expect(screen.queryByRole("option", { name: /dns/ })).not.toBeInTheDocument();
    await user.selectOptions(select, "service-1:8080");
    await user.click(screen.getByRole("button", { name: "保存" }));
    expect(await screen.findByText(/適用前の変更があります/)).toBeInTheDocument();
    expect(requests.find((request) => request.method === "PUT")?.body).toEqual({ rules: [{ path: "/", serviceId: "service-1", port: 8080 }] });
    await user.click(screen.getByRole("button", { name: "適用" }));
    expect(await screen.findByText(/反映しています/)).toBeInTheDocument();
  });

  it("path が「/」で始まらない、向き先が未選択なら、送らずに断る", async () => {
    const user = userEvent.setup();
    const { overrides } = ingressOverrides();
    const { requests } = renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/projects/project-a/ingresses/ingress-1" });
    await user.click(await screen.findByRole("button", { name: "ルールを足す" }));
    await user.click(screen.getByRole("button", { name: "保存" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("向き先");
    await user.clear(screen.getByLabelText("path 1"));
    await user.type(screen.getByLabelText("path 1"), "api");
    await user.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("「/」で始めて"));
    expect(requests.some((request) => request.method === "PUT")).toBe(false);
  });

  it("反映中は、編集・適用・削除を押せない", async () => {
    const { overrides } = ingressOverrides({ status: "applying", rules: [{ path: "/", serviceId: "service-1", port: 8080 }], pending: [{ path: "/", serviceId: "service-1", port: 8080 }, { path: "/api", serviceId: "service-1", port: 8080 }] });
    renderApp(appServer({ projects: PROJECTS }, overrides), { loggedIn: true, path: "/projects/project-a/ingresses/ingress-1" });
    expect(await screen.findByRole("button", { name: "適用" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "破棄" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "ルールを足す" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "ingress を削除" })).toBeDisabled();
    // 現在と pending が、並んで出る。
    expect(screen.getByText("いま反映されているルール")).toBeInTheDocument();
    expect(screen.getByText("適用前のルール")).toBeInTheDocument();
  });
});
