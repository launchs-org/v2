import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { jsonResponse } from "../test/fake_server";
import { appServer, renderApp } from "../test/render_app";

const PROJECTS = [{ projectId: "project-a", name: "app-a", status: "active" }];

/** キャンバスに、ingress 1つ・deploy 1つ・volume 1つが、つながっている状態の、偽のサーバー。 */
function canvasOverrides() {
  const service = { serviceId: "service-1", ports: [{ name: "web", port: 8080, protocol: "tcp" }], pending_ports: null, status: "active", dnsName: "d", clusterIP: "1.1.1.1" };
  return {
    "GET /api/v1/projects/project-a/deploys": () => jsonResponse(200, { deploys: [{ deployId: "deploy-1", status: "active", spec: "small", replicas: 2, imageRef: "registry.example/web:1.0", createdAt: "2026-09-16T09:00:00+09:00" }], nextCursor: null }),
    "GET /api/v1/deploy/deploy-1": () => jsonResponse(200, { deployId: "deploy-1", projectId: "project-a", status: "active", spec: "small", replicas: 2, pods: [], logPods: [], imageRef: "registry.example/web:1.0", createdAt: "2026-09-16T09:00:00+09:00", config: { env: {}, pending_env: null, command: null, pending_command: null, args: null, pending_args: null }, volumes: { mounts: [{ volumeId: "volume-1", mountPath: "/data" }], pending_mounts: null }, service }),
    "GET /api/v1/projects/project-a/ingresses": () => jsonResponse(200, { ingresses: [{ ingressId: "ingress-1", host: "red-fox.example.org" }], nextCursor: null }),
    "GET /api/v1/projects/project-a/ingresses/ingress-1": () => jsonResponse(200, { ingressId: "ingress-1", host: "red-fox.example.org", rules: [{ path: "/api", serviceId: "service-1", port: 8080 }], pending_rules: null, status: "active" }),
    "GET /api/v1/projects/project-a/volumes": () => jsonResponse(200, { volumes: [{ volumeId: "volume-1", name: "db-data", status: "active", sizeMB: 1024, pending_sizeMB: null }], nextCursor: null }),
  };
}

describe("プロジェクトのキャンバス", () => {
  it("ingress・deploy・volume のカードが並び、つながりの言葉(path、マウント先)が線に添えられる", async () => {
    renderApp(appServer({ projects: PROJECTS }, canvasOverrides()), { loggedIn: true, path: "/projects/project-a" });
    const canvas = await screen.findByTestId("canvas");
    expect(await within(canvas).findByText("red-fox.example.org")).toBeInTheDocument();
    expect(within(canvas).getByText("web:1.0")).toBeInTheDocument();
    expect(within(canvas).getByText("db-data")).toBeInTheDocument();
    // 線の言葉は、詳細が届いたあとに出る。
    expect(await within(canvas).findByText("/api")).toBeInTheDocument();
    expect(await within(canvas).findByText("/data")).toBeInTheDocument();
  });

  it("deploy のカードを押すと、deploy の画面へ進み、タブで項目を切り替えられる", async () => {
    const user = userEvent.setup();
    renderApp(appServer({ projects: PROJECTS }, canvasOverrides()), { loggedIn: true, path: "/projects/project-a" });
    await user.click(await screen.findByRole("link", { name: /web:1.0/ }));
    // 概要が開いていて、他の項目は、タブを押すまで出ない。
    expect(await screen.findByRole("tab", { name: "概要", selected: true })).toBeInTheDocument();
    expect(window.location.pathname).toBe("/projects/project-a/deploys/deploy-1");
    expect(screen.queryByLabelText("起動コマンド")).not.toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "起動設定" }));
    expect(await screen.findByLabelText("起動コマンド")).toBeInTheDocument();
  });

  it("何も無いプロジェクトでも、deploy を作る入口がある", async () => {
    renderApp(appServer({ projects: PROJECTS }), { loggedIn: true, path: "/projects/project-a" });
    const links = await screen.findAllByRole("link", { name: /deploy を作る/ });
    expect(links[0]).toHaveAttribute("href", "/projects/project-a/deploys/new");
  });

  it("上のナビゲーションから、一覧と設定の画面へ行ける", async () => {
    renderApp(appServer({ projects: PROJECTS }), { loggedIn: true, path: "/projects/project-a" });
    const nav = await screen.findByRole("navigation", { name: "プロジェクトのナビゲーション" });
    expect(within(nav).getByRole("link", { name: /Ingress/ })).toHaveAttribute("href", "/projects/project-a/ingresses");
    expect(within(nav).getByRole("link", { name: /Volume/ })).toHaveAttribute("href", "/projects/project-a/volumes");
    expect(within(nav).getByRole("link", { name: /設定/ })).toHaveAttribute("href", "/projects/project-a/settings");
  });
});
