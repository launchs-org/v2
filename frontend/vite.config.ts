/// <reference types="vitest/config" />
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Vite の設定。開発サーバーは、nginx の後ろで動く(ブラウザは nginx の HTTPS につなぐ)ので、HMR の接続先を、ブラウザのいるポートに合わせる。
export default defineConfig({
  plugins: [react()],
  server: {
    // nginx 経由のホスト名(localhost)を許可する。
    allowedHosts: true,
    // HMR の WebSocket は、nginx(HTTPS の 443 ではなく、ホストに公開したポート)を通る。ブラウザの URL に合わせて自動で決める。
    hmr: { clientPort: Number(process.env.VITE_HMR_CLIENT_PORT ?? 18443), protocol: "wss" },
    // コンテナの中のファイルの変更を、確実に検知する(ホストからマウントしたファイルは、イベントが届かないことがある)。
    watch: { usePolling: true },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    css: false,
  },
});
