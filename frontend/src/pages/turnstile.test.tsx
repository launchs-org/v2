import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { TurnstileApi, TurnstileRenderOptions } from "../auth/turnstile";
import { errorResponse, jsonResponse } from "../test/fake_server";
import { appServer, renderApp } from "../test/render_app";

/** 偽の Turnstile: render で受けた設定を覚え、テストから、トークンの発行・期限切れを、起こせる。 */
interface FakeTurnstile {
  api: TurnstileApi;
  renders: TurnstileRenderOptions[];
  resets: string[];
  removed: string[];
}

/**
 * 偽の Turnstile を、window に置く(スクリプトを、読み込まずに済む)。
 *
 * @returns 偽の Turnstile。
 */
function installFakeTurnstile(): FakeTurnstile {
  const fake: FakeTurnstile = { api: undefined as unknown as TurnstileApi, renders: [], resets: [], removed: [] };
  fake.api = {
    render: (container, options) => {
      fake.renders.push(options);
      container.setAttribute("data-rendered", options.sitekey);
      return `widget-${fake.renders.length}`;
    },
    reset: (widgetId) => void fake.resets.push(widgetId),
    remove: (widgetId) => void fake.removed.push(widgetId),
  };
  window.turnstile = fake.api;
  return fake;
}

afterEach(() => {
  delete window.turnstile;
  vi.restoreAllMocks();
});

/**
 * ユーザー名とパスワードを入力する。
 *
 * @param user userEvent の操作。
 */
async function fillCredentials(user: ReturnType<typeof userEvent.setup>): Promise<void> {
  await user.type(await screen.findByLabelText("ユーザー名"), "alice");
  await user.type(screen.getByLabelText("パスワード"), "correct horse battery");
}

describe("人間であることの確認(Cloudflare Turnstile)", () => {
  it("無効なとき: ウィジェットを出さず、トークンを送らずに、ログインできる", async () => {
    const fake = installFakeTurnstile();
    const user = userEvent.setup();
    const { requests } = renderApp(appServer({ projects: [] }), { path: "/login" });
    await fillCredentials(user);
    await user.click(screen.getByRole("button", { name: "ログイン" }));
    expect(await screen.findByRole("heading", { name: "プロジェクト" })).toBeInTheDocument();
    expect(fake.renders).toHaveLength(0);
    expect(screen.queryByTestId("turnstile-container")).not.toBeInTheDocument();
    expect(requests.find((request) => request.path === "/auth/login")?.body).toEqual({ userName: "alice", password: "correct horse battery" });
  });

  it("有効なとき: サイトキーでウィジェットを描き、トークンができるまで、送信できない。できたら、トークンを送る", async () => {
    const fake = installFakeTurnstile();
    const user = userEvent.setup();
    const { requests } = renderApp(appServer({ projects: [], turnstileSiteKey: "site-key-1" }), { path: "/login" });
    await fillCredentials(user);
    await waitFor(() => expect(fake.renders).toHaveLength(1));
    expect(fake.renders[0]).toMatchObject({ sitekey: "site-key-1", theme: "auto", language: "ja", size: "flexible" });
    // トークンが無い間は、送信できない。
    const submit = screen.getByRole("button", { name: "ログイン" });
    expect(submit).toBeDisabled();
    // ウィジェットが、トークンを渡すと、送信できる。
    fake.renders[0]!.callback("token-abc");
    await waitFor(() => expect(submit).toBeEnabled());
    await user.click(submit);
    expect(await screen.findByRole("heading", { name: "プロジェクト" })).toBeInTheDocument();
    expect(requests.find((request) => request.path === "/auth/login")?.body).toEqual({ userName: "alice", password: "correct horse battery", turnstileToken: "token-abc" });
  });

  it("トークンの期限切れ・エラーのときは、また送信できなくなる", async () => {
    const fake = installFakeTurnstile();
    renderApp(appServer({ projects: [], turnstileSiteKey: "site-key-1" }), { path: "/login" });
    await waitFor(() => expect(fake.renders).toHaveLength(1));
    const user = userEvent.setup();
    await fillCredentials(user);
    fake.renders[0]!.callback("token-abc");
    await waitFor(() => expect(screen.getByRole("button", { name: "ログイン" })).toBeEnabled());
    fake.renders[0]!["expired-callback"]();
    await waitFor(() => expect(screen.getByRole("button", { name: "ログイン" })).toBeDisabled());
    fake.renders[0]!.callback("token-def");
    await waitFor(() => expect(screen.getByRole("button", { name: "ログイン" })).toBeEnabled());
    fake.renders[0]!["error-callback"]();
    await waitFor(() => expect(screen.getByRole("button", { name: "ログイン" })).toBeDisabled());
  });

  it("送信に失敗したら、トークン(1回しか使えない)を捨てて、ウィジェットをやり直させる。エラーも出す", async () => {
    const fake = installFakeTurnstile();
    const user = userEvent.setup();
    renderApp(appServer({ projects: [], turnstileSiteKey: "site-key-1" }, { "POST /auth/login": () => errorResponse(400, "invalid_request", "人間であることの確認に失敗しました。もう一度試してください") }), { path: "/login" });
    await fillCredentials(user);
    await waitFor(() => expect(fake.renders).toHaveLength(1));
    fake.renders[0]!.callback("used-token");
    await user.click(await screen.findByRole("button", { name: "ログイン" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("人間であることの確認に失敗しました");
    expect(fake.resets).toEqual(["widget-1"]);
    // 新しいトークンができるまで、また送信できない。
    expect(screen.getByRole("button", { name: "ログイン" })).toBeDisabled();
  });

  it("サインアップでも、同じ: トークンが要り、送られる", async () => {
    const fake = installFakeTurnstile();
    const user = userEvent.setup();
    const { requests } = renderApp(appServer({ projects: [], turnstileSiteKey: "site-key-1" }), { path: "/signup" });
    await fillCredentials(user);
    await waitFor(() => expect(fake.renders).toHaveLength(1));
    expect(screen.getByRole("button", { name: "作成してログイン" })).toBeDisabled();
    fake.renders[0]!.callback("signup-token");
    await user.click(await screen.findByRole("button", { name: "作成してログイン" }));
    expect(await screen.findByRole("heading", { name: "プロジェクト" })).toBeInTheDocument();
    expect(requests.find((request) => request.path === "/auth/signup")?.body).toEqual({ userName: "alice", password: "correct horse battery", turnstileToken: "signup-token" });
  });

  it("ログインして画面を離れると、ウィジェットを取り除く。ログインとサインアップの切り替えでは、取り除かず、描き直さない", async () => {
    const fake = installFakeTurnstile();
    const user = userEvent.setup();
    renderApp(appServer({ projects: [], turnstileSiteKey: "site-key-1" }), { path: "/login" });
    await waitFor(() => expect(fake.renders).toHaveLength(1));
    // 同じフォームの部品のまま、サインアップに切り替わる(ウィジェットは、そのまま)。
    await user.click(screen.getByRole("link", { name: "アカウントを作る" }));
    expect(await screen.findByRole("heading", { name: "アカウントを作る" })).toBeInTheDocument();
    expect(fake.renders).toHaveLength(1);
    expect(fake.removed).toHaveLength(0);
    // ログインに成功して、別の画面へ移ると、取り除かれる。
    await fillCredentials(user);
    fake.renders[0]!.callback("token-1");
    await user.click(await screen.findByRole("button", { name: "作成してログイン" }));
    expect(await screen.findByRole("heading", { name: "プロジェクト" })).toBeInTheDocument();
    await waitFor(() => expect(fake.removed).toContain("widget-1"));
  });

  it("設定が取れないとき(認証の API の障害)でも、フォームは使える(確認が必要なら、サーバーが断る)", async () => {
    installFakeTurnstile();
    const user = userEvent.setup();
    renderApp(appServer({ projects: [] }, { "GET /auth/config": () => errorResponse(500, "internal_error", "boom") }), { path: "/login" });
    await fillCredentials(user);
    await waitFor(() => expect(screen.getByRole("button", { name: "ログイン" })).toBeEnabled(), { timeout: 5000 });
  });

  it("サーバーが、確認が必要と答えたのに、トークンが無かったときの、400 を、そのまま出す", async () => {
    const user = userEvent.setup();
    renderApp(appServer({ projects: [] }, { "POST /auth/login": () => errorResponse(400, "invalid_request", "人間であることの確認(turnstileToken)が必要です") }), { path: "/login" });
    await fillCredentials(user);
    await user.click(await screen.findByRole("button", { name: "ログイン" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("turnstileToken");
  });

  it("確認のサービスに届かない(503)ときは、そのメッセージを出す", async () => {
    const user = userEvent.setup();
    renderApp(appServer({ projects: [] }, { "POST /auth/login": () => jsonResponse(503, { error: { code: "service_unavailable", message: "人間であることの確認のサービスに接続できません。しばらくしてから、もう一度試してください" } }) }), { path: "/login" });
    await fillCredentials(user);
    await user.click(await screen.findByRole("button", { name: "ログイン" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("接続できません");
  });
});

describe("loadTurnstile(スクリプトの読み込み)", () => {
  it("1回だけ読み込み、すでに window.turnstile があれば、読み込まない。失敗したら、次に、やり直せる", async () => {
    vi.resetModules();
    const { loadTurnstile, TURNSTILE_SCRIPT_URL } = await import("../auth/turnstile");
    const scripts = () => Array.from(document.head.querySelectorAll<HTMLScriptElement>("script")).filter((script) => script.src === TURNSTILE_SCRIPT_URL);
    // 1回目の呼び出しで、script が1つ足され、2回目は、同じ約束を返す(増えない)。
    const first = loadTurnstile();
    const second = loadTurnstile();
    expect(scripts()).toHaveLength(1);
    // 読み込みに失敗すると、失敗し、script を取り除く。
    scripts()[0]!.onerror?.(new Event("error"));
    await expect(first).rejects.toThrow("読み込めませんでした");
    await expect(second).rejects.toThrow();
    expect(scripts()).toHaveLength(0);
    // 失敗の後に呼ぶと、もう一度、読み込む。読み込めたが、入口が無いときは、失敗する。
    const third = loadTurnstile();
    expect(scripts()).toHaveLength(1);
    scripts()[0]!.onload?.(new Event("load"));
    await expect(third).rejects.toThrow("使える状態になりません");
    // 入口ができていれば、成功する。
    const fourth = loadTurnstile();
    const api = installFakeTurnstile().api;
    scripts().at(-1)!.onload?.(new Event("load"));
    await expect(fourth).resolves.toBe(api);
    // すでに window.turnstile があれば、script を足さない。
    const before = scripts().length;
    await expect(loadTurnstile()).resolves.toBe(api);
    expect(scripts()).toHaveLength(before);
  });
});
