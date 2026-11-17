import { describe, expect, it } from "vitest";
import { argsToText, envToRows, parseArgs, rowsToEnv, validateEnvRows, validateMounts, validateServicePorts } from "./deploy_settings";

describe("環境変数", () => {
  it("行とオブジェクトを、行き来できる(空のキーの行は捨てる)", () => {
    expect(envToRows({ B: "2", A: "1" })).toEqual([{ key: "A", value: "1" }, { key: "B", value: "2" }]);
    expect(rowsToEnv([{ key: " A ", value: "1" }, { key: "", value: "" }])).toEqual({ A: "1" });
  });
  it("値だけの行、空白と = を含むキー、重複を断る", () => {
    expect(validateEnvRows([{ key: "", value: "x" }])).not.toBeNull();
    expect(validateEnvRows([{ key: "A B", value: "" }])).not.toBeNull();
    expect(validateEnvRows([{ key: "A=1", value: "" }])).not.toBeNull();
    expect(validateEnvRows([{ key: "A", value: "1" }, { key: "A", value: "2" }])).not.toBeNull();
    expect(validateEnvRows([{ key: "", value: "" }, { key: "A", value: "1" }])).toBeNull();
  });
});

describe("引数", () => {
  it("1行に1つ。空の行は無視し、1つも無ければ null", () => {
    expect(parseArgs("--a\n\n--b\r\n")).toEqual(["--a", "--b"]);
    expect(parseArgs("  \n")).toBeNull();
    expect(argsToText(["--a", "--b"])).toBe("--a\n--b");
    expect(argsToText(null)).toBe("");
  });
});

describe("サービスのポート", () => {
  const valid = { name: "web", port: 8080, protocol: "tcp" as const };
  it("正しいものを通す。同じ番号でも、プロトコルが違えば通す", () => {
    expect(validateServicePorts([valid, { name: "dns", port: 8080, protocol: "udp" }])).toBeNull();
  });
  it("0個、名前の規則違反、番号の範囲外、重複を断る", () => {
    expect(validateServicePorts([])).not.toBeNull();
    for (const name of ["", "Web", "-a", "a-", "a--b", "123", "a".repeat(16)]) {
      expect(validateServicePorts([{ ...valid, name }])).not.toBeNull();
    }
    expect(validateServicePorts([{ ...valid, port: 0 }])).not.toBeNull();
    expect(validateServicePorts([{ ...valid, port: 65536 }])).not.toBeNull();
    expect(validateServicePorts([valid, { ...valid, port: 9090 }])).not.toBeNull();
    expect(validateServicePorts([valid, { ...valid, name: "web2" }])).not.toBeNull();
  });
});

describe("マウント", () => {
  it("正しいものを通し、パスの規則違反と重複を断る", () => {
    expect(validateMounts([{ volumeId: "volume-a", mountPath: "/data" }])).toBeNull();
    expect(validateMounts([])).toBeNull();
    for (const mountPath of ["data", "/", "/a/../b", "/a b"]) {
      expect(validateMounts([{ volumeId: "volume-a", mountPath }])).not.toBeNull();
    }
    expect(validateMounts([{ volumeId: "", mountPath: "/data" }])).not.toBeNull();
    expect(validateMounts([{ volumeId: "volume-a", mountPath: "/a" }, { volumeId: "volume-a", mountPath: "/b" }])).not.toBeNull();
    expect(validateMounts([{ volumeId: "volume-a", mountPath: "/a" }, { volumeId: "volume-b", mountPath: "/a" }])).not.toBeNull();
  });
});
