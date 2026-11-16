// テストの共通の準備: DOM の検査用の関数(toBeInTheDocument など)を使えるようにし、各テストの後に、描画を片付ける。
import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

afterEach(() => {
  cleanup();
});
