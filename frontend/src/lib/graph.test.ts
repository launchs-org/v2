import { describe, expect, it } from "vitest";
import type { Ingress, Volume } from "../api/types";
import { buildGraph, edgePath, nodeKey, NODE_HEIGHT, NODE_WIDTH, type GraphDeploy } from "./graph";

const service = { serviceId: "service-1", ports: [{ name: "web", port: 80, protocol: "tcp" as const }], pending_ports: null, status: "active" as const, dnsName: "d", clusterIP: "1.1.1.1" };
const deploy: GraphDeploy = { deployId: "deploy-1", imageRef: "nginx", status: "active", replicas: 1, service, mounts: [{ volumeId: "volume-1", mountPath: "/data" }], pendingMounts: null };
const volume: Volume = { volumeId: "volume-1", name: "data", status: "active", sizeMB: 512, pending_sizeMB: null };
const ingress: Ingress = { ingressId: "ingress-1", host: "a.example", rules: [{ path: "/", serviceId: "service-1", port: 80 }], pending_rules: null, status: "active" };

describe("グラフの組み立て", () => {
  it("3つの列に並べ、ingress → deploy と deploy → volume の線を作る", () => {
    const graph = buildGraph({ ingresses: [ingress], deploys: [deploy], volumes: [volume] });
    expect(graph.nodes.map((node) => node.key)).toEqual(["ingress:ingress-1", "deploy:deploy-1", "volume:volume-1"]);
    expect(graph.nodes[0]!.x).toBeLessThan(graph.nodes[1]!.x);
    expect(graph.nodes[1]!.x).toBeLessThan(graph.nodes[2]!.x);
    expect(graph.edges.map((edge) => [edge.from, edge.to, edge.label, edge.pending])).toEqual([
      ["ingress:ingress-1", "deploy:deploy-1", "/", false],
      ["deploy:deploy-1", "volume:volume-1", "/data", false],
    ]);
  });

  it("適用前の rules とマウントは、点線(pending)の線になり、同じ組は1本にまとまる", () => {
    const graph = buildGraph({
      ingresses: [{ ...ingress, rules: [], pending_rules: [{ path: "/", serviceId: "service-1", port: 80 }, { path: "/", serviceId: "service-1", port: 80 }] }],
      deploys: [{ ...deploy, mounts: [], pendingMounts: [{ volumeId: "volume-1", mountPath: "/data" }] }],
      volumes: [volume],
    });
    expect(graph.edges.map((edge) => edge.pending)).toEqual([true, true]);
    expect(graph.edges).toHaveLength(2);
  });

  it("流す先が無い rules と、一覧に無い volume は、線にしない", () => {
    const graph = buildGraph({ ingresses: [{ ...ingress, rules: [{ path: "/", serviceId: "service-gone", port: 80 }] }], deploys: [{ ...deploy, service: null, mounts: [{ volumeId: "volume-gone", mountPath: "/x" }] }], volumes: [] });
    expect(graph.edges).toHaveLength(0);
  });

  it("空でも、大きさは 0 にならない", () => {
    const graph = buildGraph({ ingresses: [], deploys: [], volumes: [] });
    expect(graph.nodes).toHaveLength(0);
    expect(graph.height).toBeGreaterThan(0);
  });

  it("線は、左のカードの右端の中央から、右のカードの左端の中央へ引く", () => {
    const graph = buildGraph({ ingresses: [ingress], deploys: [deploy], volumes: [volume] });
    const path = edgePath(graph.nodes[0]!, graph.nodes[1]!);
    expect(path.startsWith(`M ${graph.nodes[0]!.x + NODE_WIDTH} ${graph.nodes[0]!.y + NODE_HEIGHT / 2}`)).toBe(true);
    expect(path.endsWith(`${graph.nodes[1]!.x} ${graph.nodes[1]!.y + NODE_HEIGHT / 2}`)).toBe(true);
    expect(nodeKey("volume", "v")).toBe("volume:v");
  });
});
