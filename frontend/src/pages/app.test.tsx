import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { REFRESH_TOKEN_KEY } from "../auth/token_store";
import { errorResponse, jsonResponse } from "../test/fake_server";
import { appServer, renderApp } from "../test/render_app";

describe("ログインとガード", () => {
  it("ログインしていないと、保護された画面から、ログイン画面へ送られる(サーバーの API は呼ばない)", async () => {
    const { requests } = renderApp(appServer({ projects: [] }), { path: "/projects" });
    expect(await screen.findByRole("heading", { name: "ログイン" })).toBeInTheDocument();
    expect(requests.filter((request) => request.path.startsWith("/api/"))).toHaveLength(0);
  });

  it("ログインすると、元の画面(プロジェクトの一覧)へ進み、ユーザー名と使用量が出る", async () => {
    const user = userEvent.setup();
    const { storage } = renderApp(appServer({ projects: [{ projectId: "project-a", name: "app-a", status: "active" }] }), { path: "/login" });
    await user.type(await screen.findByLabelText("ユーザー名"), "alice");
    await user.type(screen.getByLabelText("パスワード"), "correct horse battery");
    await user.click(screen.getByRole("button", { name: "ログイン" }));
    expect(await screen.findByRole("heading", { name: "プロジェクト" })).toBeInTheDocument();
    expect(await screen.findByText("app-a")).toBeInTheDocument();
    expect(screen.getByText("alice")).toBeInTheDocument();
    expect(await screen.findByText("プロジェクト 1/10")).toBeInTheDocument();
    expect(storage.getItem(REFRESH_TOKEN_KEY)).toBe("refresh-1");
  });

  it("ログインに失敗したら、サーバーのメッセージを出して、画面に留まる", async () => {
    const user = userEvent.setup();
    renderApp(appServer({ projects: [] }, { "POST /auth/login": () => errorResponse(401, "unauthorized", "userName または password が正しくありません") }), { path: "/login" });
    await user.type(await screen.findByLabelText("ユーザー名"), "alice");
    await user.type(screen.getByLabelText("パスワード"), "wrong");
    await user.click(screen.getByRole("button", { name: "ログイン" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("userName または password が正しくありません");
    expect(screen.getByRole("heading", { name: "ログイン" })).toBeInTheDocument();
  });

  it("レート制限(429)のときは、待つ秒数を出す", async () => {
    const user = userEvent.setup();
    renderApp(appServer({ projects: [] }, { "POST /auth/login": () => errorResponse(429, "too_many_requests", "多すぎます", { "Retry-After": "12" }) }), { path: "/login" });
    await user.type(await screen.findByLabelText("ユーザー名"), "alice");
    await user.type(screen.getByLabelText("パスワード"), "pw");
    await user.click(screen.getByRole("button", { name: "ログイン" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("12 秒後");
  });

  it("サインアップ: 招待コードの欄があり、成功するとログインの状態で一覧へ進む", async () => {
    const user = userEvent.setup();
    const { requests } = renderApp(appServer({ projects: [] }), { path: "/signup" });
    await user.type(await screen.findByLabelText("ユーザー名"), "alice");
    await user.type(screen.getByLabelText("パスワード"), "correct horse battery");
    await user.type(screen.getByLabelText(/招待コード/), "invite-1");
    await user.click(screen.getByRole("button", { name: "作成してログイン" }));
    expect(await screen.findByRole("heading", { name: "プロジェクト" })).toBeInTheDocument();
    expect(requests.find((request) => request.path === "/auth/signup")?.body).toEqual({ userName: "alice", password: "correct horse battery", invitationCode: "invite-1" });
  });

  it("保存してある refresh token で、起動時にログインの状態が戻る", async () => {
    renderApp(appServer({ projects: [] }), { loggedIn: true, path: "/projects" });
    expect(await screen.findByRole("heading", { name: "プロジェクト" })).toBeInTheDocument();
  });

  it("ログアウトすると、ログイン画面へ戻り、保存が消える", async () => {
    const user = userEvent.setup();
    const { storage } = renderApp(appServer({ projects: [] }), { loggedIn: true, path: "/projects" });
    await user.click(await screen.findByRole("button", { name: "ログアウト" }));
    expect(await screen.findByRole("heading", { name: "ログイン" })).toBeInTheDocument();
    expect(storage.getItem(REFRESH_TOKEN_KEY)).toBeNull();
  });

  it("外部の URL へ飛ばされない(from が、サイト内のパスでなければ、無視する)", async () => {
    window.history.pushState({ from: "//evil.example/x" }, "", "/login");
    const user = userEvent.setup();
    renderApp(appServer({ projects: [] }), { path: "/login" });
    await user.type(await screen.findByLabelText("ユーザー名"), "alice");
    await user.type(screen.getByLabelText("パスワード"), "pw");
    await user.click(screen.getByRole("button", { name: "ログイン" }));
    await screen.findByRole("heading", { name: "プロジェクト" });
    expect(window.location.pathname).toBe("/projects");
  });
});

describe("プロジェクトの一覧", () => {
  it("空のときは、案内を出す", async () => {
    renderApp(appServer({ projects: [] }), { loggedIn: true, path: "/projects" });
    expect(await screen.findByText(/プロジェクトはまだありません/)).toBeInTheDocument();
  });

  it("表に、名前、状態の言葉、プロジェクト ID を出す。名前が、詳細へのリンクになる", async () => {
    renderApp(appServer({ projects: [{ projectId: "project-a", name: "app-a", status: "active" }, { projectId: "project-b", name: "app-b", status: "deleting" }] }), { loggedIn: true, path: "/projects" });
    const rows = await screen.findAllByRole("row");
    // 先頭は、見出しの行。
    expect(within(rows[0]!).getByText("プロジェクト ID")).toBeInTheDocument();
    expect(within(rows[1]!).getByText("稼働中")).toBeInTheDocument();
    expect(within(rows[1]!).getByText("project-a")).toBeInTheDocument();
    expect(within(rows[2]!).getByText("削除中")).toBeInTheDocument();
    expect(within(rows[1]!).getByRole("link", { name: "app-a" })).toHaveAttribute("href", "/projects/project-a");
  });

  it("「もっと見る」で、次のページ(cursor)を取って、続けて表示する。最後のページでは、ボタンが消える", async () => {
    const state = { projects: [1, 2, 3].map((number) => ({ projectId: `project-${number}`, name: `app-${number}`, status: "active" })), pageSize: 2 };
    const user = userEvent.setup();
    const { requests } = renderApp(appServer(state), { loggedIn: true, path: "/projects" });
    await screen.findByText("app-2");
    expect(screen.queryByText("app-3")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "もっと見る" }));
    expect(await screen.findByText("app-3")).toBeInTheDocument();
    expect(requests.some((request) => request.path === "/api/v1/projects?cursor=2")).toBe(true);
    expect(screen.queryByRole("button", { name: "もっと見る" })).not.toBeInTheDocument();
  });

  it("作成: 名前を検査して、問題があれば、サーバーに送らない。成功すると、一覧に出る", async () => {
    const state = { projects: [] as { projectId: string; name: string; status: string }[] };
    const user = userEvent.setup();
    const { requests } = renderApp(appServer(state), { loggedIn: true, path: "/projects" });
    const input = await screen.findByLabelText("新しいプロジェクトの名前");
    await user.type(input, "my app");
    await user.click(screen.getByRole("button", { name: "作成" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("英数字とハイフン");
    expect(requests.some((request) => request.method === "POST" && request.path === "/api/v1/projects")).toBe(false);
    await user.clear(input);
    await user.type(input, "my-app");
    await user.click(screen.getByRole("button", { name: "作成" }));
    expect(await screen.findByText("my-app")).toBeInTheDocument();
    expect(requests.find((request) => request.method === "POST" && request.path === "/api/v1/projects")?.body).toEqual({ name: "my-app" });
  });

  it("作成が、サーバーに拒否されたら(上限を超えた 409)、そのメッセージを出す", async () => {
    const user = userEvent.setup();
    renderApp(appServer({ projects: [] }, { "POST /api/v1/projects": () => errorResponse(409, "conflict", "プロジェクトの数が上限(10)に達しています(使用中 10)") }), { loggedIn: true, path: "/projects" });
    await user.type(await screen.findByLabelText("新しいプロジェクトの名前"), "my-app");
    await user.click(screen.getByRole("button", { name: "作成" }));
    expect(await screen.findByText(/上限\(10\)に達しています/)).toBeInTheDocument();
  });

  it("一覧の取得に失敗したら、エラーを出す", async () => {
    renderApp(appServer({ projects: [] }, { "GET /api/v1/projects": () => errorResponse(500, "internal_error", "サーバー内部のエラー") }), { loggedIn: true, path: "/projects" });
    expect(await screen.findByRole("alert")).toHaveTextContent("サーバー内部のエラー");
  });

  it("access token が切れていたら(401)、更新して、画面は、そのまま表示される", async () => {
    let calls = 0;
    renderApp(appServer({ projects: [{ projectId: "project-a", name: "app-a", status: "active" }] }, {
      "GET /api/v1/projects": (request) => (++calls === 1 ? errorResponse(401, "unauthorized", "認証が必要です") : jsonResponse(200, { projects: [{ projectId: "project-a", name: "app-a", status: "active" }], nextCursor: null, seen: request.path })),
    }), { loggedIn: true, path: "/projects" });
    expect(await screen.findByText("app-a")).toBeInTheDocument();
  });
});

describe("サイドバーと使用量", () => {
  it("サイドバーに、ナビゲーションと、ユーザー名があり、いまの画面が分かる(aria-current)", async () => {
    renderApp(appServer({ projects: [] }), { loggedIn: true, path: "/projects" });
    const navigation = await screen.findByRole("navigation", { name: "メインメニュー" });
    expect(within(navigation).getByRole("link", { name: "プロジェクト" })).toHaveAttribute("aria-current", "page");
    expect(within(navigation).getByRole("link", { name: "使用量" })).not.toHaveAttribute("aria-current");
    expect(await screen.findByText("alice")).toBeInTheDocument();
  });

  it("「使用量」を押すと、上限と使用量の画面へ移り、スペックごとの deploy も出る", async () => {
    const user = userEvent.setup();
    renderApp(appServer({ projects: [{ projectId: "project-a", name: "app-a", status: "active" }] }, {
      "GET /api/v1/quota": () => jsonResponse(200, {
        projects: { limit: 10, used: 9 }, deploys: { limit: 20, used: 3 }, deploysBySpec: { small: { limit: 20, used: 2 }, large: { limit: 5, used: 1 } },
        volumes: { limit: 10, used: 1 }, volumeTotalMB: { limit: 20480, used: 1536 }, maxVolumeSizeMB: 5120, maxReplicas: 5,
      }),
    }), { loggedIn: true, path: "/projects" });
    await user.click(await screen.findByRole("link", { name: "使用量" }));
    expect(await screen.findByRole("heading", { name: "使用量" })).toBeInTheDocument();
    // 容量は GB で出る。
    expect(await screen.findByText("1.5 GB")).toBeInTheDocument();
    expect(screen.getByText("/ 20 GB")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "スペックごとの deploy" })).toBeInTheDocument();
    expect(screen.getByRole("progressbar", { name: "largeの使用量" })).toHaveAttribute("aria-valuenow", "1");
    // 上限に近い(9/10)プロジェクトの棒は、値と最大が、読み上げに伝わる。
    const projectBar = screen.getAllByRole("progressbar", { name: "プロジェクトの使用量" }).at(-1)!;
    expect(projectBar).toHaveAttribute("aria-valuenow", "9");
    expect(projectBar).toHaveAttribute("aria-valuemax", "10");
    expect(screen.getByText("5 GB")).toBeInTheDocument();
  });

  it("使用量が取れないときは、エラーを出す", async () => {
    renderApp(appServer({ projects: [] }, { "GET /api/v1/quota": () => errorResponse(500, "internal_error", "サーバー内部のエラー") }), { loggedIn: true, path: "/usage" });
    expect(await screen.findByRole("alert")).toHaveTextContent("サーバー内部のエラー");
  });
});

describe("プロジェクトの画面", () => {
  const baseState = () => ({ projects: [{ projectId: "project-a", name: "app-a", status: "active" }] });

  it("見つからないとき(404)は、案内と、一覧へ戻るボタンを出す", async () => {
    renderApp(appServer(baseState()), { loggedIn: true, path: "/projects/project-nothing" });
    expect(await screen.findByText(/見つかりません/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "プロジェクトの一覧へ" })).toBeInTheDocument();
  });

  it("名前の変更: 変更が無いうちは押せず、不正な名前は、送らず、正しければ、送って反映される", async () => {
    const user = userEvent.setup();
    const { requests } = renderApp(appServer(baseState()), { loggedIn: true, path: "/projects/project-a" });
    expect(await screen.findByRole("heading", { name: /app-a/ })).toBeInTheDocument();
    // パンくずに、一覧へ戻るリンクと、いまの名前がある。
    expect(within(screen.getByRole("navigation", { name: "パンくず" })).getByRole("link", { name: "プロジェクト" })).toHaveAttribute("href", "/projects");
    const submit = screen.getByRole("button", { name: "変更" });
    expect(submit).toBeDisabled();
    const input = screen.getByLabelText("新しい名前");
    await user.clear(input);
    await user.type(input, "bad name");
    await user.click(submit);
    expect(await screen.findByRole("alert")).toHaveTextContent("英数字とハイフン");
    expect(requests.some((request) => request.method === "PATCH")).toBe(false);
    await user.clear(input);
    await user.type(input, "app-renamed");
    await user.click(submit);
    expect(await screen.findByRole("heading", { name: /app-renamed/ })).toBeInTheDocument();
    expect(requests.find((request) => request.method === "PATCH")?.body).toEqual({ name: "app-renamed" });
  });

  it("名前の変更が、重複(409)なら、そのメッセージを出す", async () => {
    const user = userEvent.setup();
    renderApp(appServer(baseState(), { "PATCH /api/v1/projects/project-a": () => errorResponse(409, "conflict", "同じ名前のプロジェクトがすでにあります") }), { loggedIn: true, path: "/projects/project-a" });
    const input = await screen.findByLabelText("新しい名前");
    await user.clear(input);
    await user.type(input, "taken");
    await user.click(screen.getByRole("button", { name: "変更" }));
    expect(await screen.findByText("同じ名前のプロジェクトがすでにあります")).toBeInTheDocument();
  });

  it("削除: 確認のあとで、削除を頼み、「削除中」になる。確認をやめれば、頼まない", async () => {
    const user = userEvent.setup();
    const { requests } = renderApp(appServer(baseState()), { loggedIn: true, path: "/projects/project-a" });
    await user.click(await screen.findByRole("button", { name: "プロジェクトを削除" }));
    await user.click(await screen.findByRole("button", { name: "やめる" }));
    expect(requests.some((request) => request.method === "DELETE")).toBe(false);
    await user.click(screen.getByRole("button", { name: "プロジェクトを削除" }));
    await user.click(await screen.findByRole("button", { name: "削除する" }));
    expect(await screen.findByText(/削除しています/)).toBeInTheDocument();
    expect(requests.filter((request) => request.method === "DELETE")).toHaveLength(1);
    // 削除中は、名前の変更も、削除のボタンも、押せない。
    expect(screen.getByLabelText("新しい名前")).toBeDisabled();
    expect(screen.getByRole("button", { name: "プロジェクトを削除" })).toBeDisabled();
  });

  it("削除に失敗して止まっている(delete_failed)ときは、もう一度、削除できる", async () => {
    renderApp(appServer({ projects: [{ projectId: "project-a", name: "app-a", status: "delete_failed" }] }), { loggedIn: true, path: "/projects/project-a" });
    expect(await screen.findByText(/削除に失敗して、止まっています/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "プロジェクトを削除" })).toBeEnabled();
    await waitFor(() => expect(screen.getByLabelText("新しい名前")).toBeDisabled());
  });
});
