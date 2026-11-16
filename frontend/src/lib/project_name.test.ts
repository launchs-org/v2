import { describe, expect, it } from "vitest";
import { MAX_PROJECT_NAME_LENGTH, projectStatusLabel, validateProjectName } from "./project_name";

describe("validateProjectName", () => {
  it.each(["a", "my-app", "App-1", "a".repeat(MAX_PROJECT_NAME_LENGTH), "-", "123"])("%s は、通る", (name) => {
    expect(validateProjectName(name)).toBeNull();
  });
  it.each([
    ["", "入力"],
    ["a".repeat(MAX_PROJECT_NAME_LENGTH + 1), "64文字"],
    ["my app", "英数字とハイフン"],
    ["my_app", "英数字とハイフン"],
    ["アプリ", "英数字とハイフン"],
    ["a/b", "英数字とハイフン"],
    ["<script>", "英数字とハイフン"],
  ])("%s は、拒否する(%s)", (name, piece) => {
    expect(validateProjectName(name)).toContain(piece);
  });
});

describe("projectStatusLabel", () => {
  it("3つの状態を、言葉にする。知らない値は、そのまま", () => {
    expect(projectStatusLabel("active")).toBe("稼働中");
    expect(projectStatusLabel("deleting")).toBe("削除中");
    expect(projectStatusLabel("delete_failed")).toBe("削除に失敗");
    expect(projectStatusLabel("something")).toBe("something");
  });
});
