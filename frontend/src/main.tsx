// 入口。
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { AuthSession } from "./auth/auth_session";
import { TokenStore, browserStorage } from "./auth/token_store";
import "./styles.css";

// ログインの状態の管理を作って、画面を描く。
const session = new AuthSession(new TokenStore(browserStorage()));
const root = document.getElementById("root");
if (root === null) {
  throw new Error("#root が見つかりません");
}
createRoot(root).render(
  <StrictMode>
    <App session={session} />
  </StrictMode>,
);
