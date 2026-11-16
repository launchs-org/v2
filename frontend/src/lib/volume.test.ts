import { describe, expect, it } from "vitest";
import { validateVolumeName, validateVolumeSize, volumeStatusLabel, volumeStatusTone } from "./volume";

describe("volume の入力の検査", () => {
  it("名前は、英数字とハイフンだけ", () => {
    expect(validateVolumeName("db-data")).toBeNull();
    expect(validateVolumeName("bad name")).not.toBeNull();
    expect(validateVolumeName("")).not.toBeNull();
  });

  it("大きさは、整数で、上限以下。拡大のときは、縮小を断る", () => {
    expect(validateVolumeSize("1024", 5120)).toBeNull();
    expect(validateVolumeSize("abc", 5120)).not.toBeNull();
    expect(validateVolumeSize("0", 5120)).not.toBeNull();
    expect(validateVolumeSize("5121", 5120)).toContain("5120");
    expect(validateVolumeSize("512", 5120, 1024)).toContain("縮小");
    expect(validateVolumeSize("1024", 5120, 1024)).toBeNull();
  });

  it("状態を、言葉と色にする", () => {
    expect(volumeStatusLabel("applying")).toBe("拡張中");
    expect(volumeStatusLabel("x")).toBe("x");
    expect(volumeStatusTone("failed")).toBe("destructive");
    expect(volumeStatusTone("active")).toBe("success");
  });
});
