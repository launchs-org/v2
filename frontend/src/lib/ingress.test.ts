import { describe, expect, it } from "vitest";
import { buildPortChoices, ingressStatusLabel, ingressStatusTone, ruleChoiceValue, validateRulePath } from "./ingress";

describe("ingress の入力の検査", () => {
  it("path は、「/」で始まり、空白を含まない", () => {
    expect(validateRulePath("/api")).toBeNull();
    expect(validateRulePath("api")).not.toBeNull();
    expect(validateRulePath("/a b")).not.toBeNull();
    expect(validateRulePath(`/${"a".repeat(200)}`)).not.toBeNull();
  });

  it("向き先の選択肢は、active のサービスの tcp のポートだけ", () => {
    const choices = buildPortChoices([
      { imageRef: "app:1", service: { serviceId: "service-1", status: "active", ports: [{ name: "web", port: 8080, protocol: "tcp" }, { name: "dns", port: 53, protocol: "udp" }], pending_ports: null, dnsName: null, clusterIP: null } },
      { imageRef: "app:2", service: { serviceId: "service-2", status: "pending", ports: [{ name: "web", port: 80, protocol: "tcp" }], pending_ports: null, dnsName: null, clusterIP: null } },
      { imageRef: "app:3", service: null },
    ]);
    expect(choices).toEqual([{ value: "service-1:8080", label: "app:1 / web(8080)", serviceId: "service-1", port: 8080 }]);
    expect(ruleChoiceValue({ path: "/", serviceId: "service-1", port: 8080 })).toBe("service-1:8080");
  });

  it("状態を、言葉と色にする", () => {
    expect(ingressStatusLabel("applying")).toBe("反映中");
    expect(ingressStatusTone("failed")).toBe("destructive");
  });
});
