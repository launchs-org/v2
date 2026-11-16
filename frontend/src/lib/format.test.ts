import { describe, expect, it } from "vitest";
import { formatMegabytes } from "./format";

describe("formatMegabytes", () => {
  it.each([
    [0, "0 MB"],
    [512, "512 MB"],
    [1023, "1,023 MB"],
    [1024, "1 GB"],
    [1536, "1.5 GB"],
    [20480, "20 GB"],
  ])("%d は、%s", (megabytes, expected) => {
    expect(formatMegabytes(megabytes)).toBe(expected);
  });
});
