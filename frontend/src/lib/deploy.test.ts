import { describe, expect, it } from "vitest";
import { deployStatusLabel, deployStatusTone, formatDateTime, isDeployTransitioning, validateImageRef } from "./deploy";

describe("deploy の状態", () => {
  it("状態を、日本語の言葉と色にする(知らない値は、そのまま)", () => {
    expect(deployStatusLabel("active")).toBe("稼働中");
    expect(deployStatusLabel("scaled_zero")).toBe("停止(0台)");
    expect(deployStatusLabel("mystery")).toBe("mystery");
    expect(deployStatusTone("active")).toBe("success");
    expect(deployStatusTone("applying")).toBe("warning");
    expect(deployStatusTone("failed")).toBe("destructive");
    expect(deployStatusTone("scaled_zero")).toBe("default");
  });

  it("変わっていく途中の状態だけを、途中とみなす", () => {
    expect(["pending", "applying", "deleting"].every(isDeployTransitioning)).toBe(true);
    expect(["active", "failed", "scaled_zero", "delete_failed"].some(isDeployTransitioning)).toBe(false);
  });
});

describe("イメージの検査", () => {
  it("空と、空白を含むものを断る", () => {
    expect(validateImageRef("")).not.toBeNull();
    expect(validateImageRef("a b")).not.toBeNull();
    expect(validateImageRef("registry.example/app:1.0")).toBeNull();
  });
});

describe("日時", () => {
  it("JST で整形し、読めない値はそのまま返す", () => {
    expect(formatDateTime("2026-09-16T09:00:00+09:00")).toContain("09:00");
    expect(formatDateTime("not-a-date")).toBe("not-a-date");
  });
});
