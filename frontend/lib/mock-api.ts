import type {
  BuildListItem,
  ConfigMapFile,
  ConfigMapMount,
  DeployDetail,
  DeployListItem,
  IngressDetail,
  IngressListItem,
  Project,
  ServicePort,
  User,
  Volume,
} from "@/lib/types"

const now = () => new Date().toISOString()
const createdAt = "2026-11-16T09:24:00.000Z"

let currentUser: User = {
  userId: "usr_demo_operator",
  userName: "demo",
  status: "active",
  role: "admin",
  createdAt: "2025-06-12T08:30:00.000Z",
}

let projects: Project[] = [
  { projectId: "prj_northstar", name: "northstar-api", status: "active" },
  { projectId: "prj_storefront", name: "storefront", status: "active" },
]

let deployDetails: Record<string, DeployDetail> = {
  dep_northstar_api: {
    projectId: "prj_northstar",
    deployId: "dep_northstar_api",
    status: "active",
    spec: "medium",
    replicas: 3,
    imageRef: "ghcr.io/launchs/northstar-api:v2.8.1",
    createdAt,
    pods: [
      { podId: "northstar-api-7f8d9c-2kq4m", status: "running" },
      { podId: "northstar-api-7f8d9c-8rx1p", status: "running" },
      { podId: "northstar-api-7f8d9c-jm5wd", status: "running" },
    ],
    logPods: [
      { podId: "northstar-api-7f8d9c-2kq4m", running: true, startedAt: createdAt, endedAt: null },
      { podId: "northstar-api-7f8d9c-8rx1p", running: true, startedAt: createdAt, endedAt: null },
      { podId: "northstar-api-7f8d9c-jm5wd", running: true, startedAt: createdAt, endedAt: null },
    ],
    config: {
      env: { NODE_ENV: "production", PORT: "8080", LOG_LEVEL: "info" },
      pending_env: { NODE_ENV: "production", PORT: "8080", LOG_LEVEL: "debug" },
      command: null,
      pending_command: null,
      args: ["--config", "/app/config.yaml"],
      pending_args: ["--config", "/app/config.yaml"],
    },
    volumes: {
      mounts: [{ volumeId: "vol_northstar_data", mountPath: "/var/lib/northstar" }],
      pending_mounts: null,
    },
    service: {
      serviceId: "svc_northstar_api",
      ports: [{ name: "http", port: 8080, protocol: "tcp" }],
      pending_ports: null,
      status: "active",
      dnsName: "northstar-api.northstar.svc.cluster.local",
      clusterIP: "10.96.14.23",
    },
  },
  dep_northstar_worker: {
    projectId: "prj_northstar",
    deployId: "dep_northstar_worker",
    status: "scaling",
    spec: "small",
    replicas: 2,
    imageRef: "ghcr.io/launchs/event-worker:v1.4.0",
    createdAt: "2026-11-15T17:42:00.000Z",
    pods: [
      { podId: "event-worker-6bbf88-1mt8x", status: "running" },
      { podId: "event-worker-6bbf88-7k3dz", status: "running" },
    ],
    logPods: [
      { podId: "event-worker-6bbf88-1mt8x", running: true, startedAt: createdAt, endedAt: null },
      { podId: "event-worker-6bbf88-7k3dz", running: true, startedAt: createdAt, endedAt: null },
    ],
    config: { env: { QUEUE: "events", CONCURRENCY: "4" }, pending_env: null, command: null, pending_command: null, args: null, pending_args: null },
    volumes: { mounts: [], pending_mounts: null },
    service: null,
  },
  dep_storefront_web: {
    projectId: "prj_storefront",
    deployId: "dep_storefront_web",
    status: "active",
    spec: "small",
    replicas: 2,
    imageRef: "ghcr.io/launchs/storefront-web:v3.2.0",
    createdAt: "2026-11-14T11:08:00.000Z",
    pods: [
      { podId: "storefront-web-59d6c1-0rj8v", status: "running" },
      { podId: "storefront-web-59d6c1-9px2k", status: "running" },
    ],
    logPods: [
      { podId: "storefront-web-59d6c1-0rj8v", running: true, startedAt: createdAt, endedAt: null },
      { podId: "storefront-web-59d6c1-9px2k", running: true, startedAt: createdAt, endedAt: null },
    ],
    config: { env: { NODE_ENV: "production", PORT: "3000" }, pending_env: null, command: null, pending_command: null, args: null, pending_args: null },
    volumes: { mounts: [], pending_mounts: null },
    service: {
      serviceId: "svc_storefront_web",
      ports: [{ name: "http", port: 3000, protocol: "tcp" }],
      pending_ports: null,
      status: "active",
      dnsName: "storefront-web.storefront.svc.cluster.local",
      clusterIP: "10.96.22.18",
    },
  },
}

let volumesByProject: Record<string, Volume[]> = {
  prj_northstar: [
    { volumeId: "vol_northstar_data", name: "northstar-data", status: "active", sizeMB: 2048, pending_sizeMB: 4096, createdAt: "2026-10-28T12:00:00.000Z" },
    { volumeId: "vol_northstar_cache", name: "worker-cache", status: "active", sizeMB: 1024, pending_sizeMB: null, createdAt: "2026-11-02T15:30:00.000Z" },
  ],
  prj_storefront: [
    { volumeId: "vol_storefront_assets", name: "product-assets", status: "active", sizeMB: 5120, pending_sizeMB: null, createdAt: "2026-10-18T10:15:00.000Z" },
  ],
}

let ingressesByProject: Record<string, IngressDetail[]> = {
  prj_northstar: [
    {
      ingressId: "ing_northstar_public",
      host: "api.northstar.launchs.dev",
      rules: [{ path: "/api", serviceId: "svc_northstar_api", port: 8080 }],
      pending_rules: [{ path: "/v2", serviceId: "svc_northstar_api", port: 8080 }],
      status: "active",
    },
  ],
  prj_storefront: [],
}

let buildsByProject: Record<string, BuildListItem[]> = {
  prj_northstar: [
    { buildId: "bld_9f2a61c8", status: "success", imageRef: "ghcr.io/launchs/northstar-api:v2.8.1", createdAt: "2026-11-16T08:55:00.000Z" },
    { buildId: "bld_2d7c4e10", status: "success", imageRef: "ghcr.io/launchs/event-worker:v1.4.0", createdAt: "2026-11-15T17:20:00.000Z" },
    { buildId: "bld_8810ca4d", status: "building", imageRef: "ghcr.io/launchs/northstar-api:v2.8.2", createdAt: "2026-11-17T09:12:00.000Z" },
  ],
  prj_storefront: [
    { buildId: "bld_3ab4e971", status: "success", imageRef: "ghcr.io/launchs/storefront-web:v3.2.0", createdAt: "2026-11-14T10:40:00.000Z" },
  ],
}

/** モックの ConfigMap。版ごとに、ファイルを持つ(編集のたびに、版が増える)。 */
type MockConfigMap = { configMapId: string; projectId: string; name: string; currentRevision: number; revisions: Record<number, ConfigMapFile[]>; createdAt: string; updatedAt: string }

let configMapsById: Record<string, MockConfigMap> = {
  cm_northstar_config: {
    configMapId: "cm_northstar_config", projectId: "prj_northstar", name: "northstar-config", currentRevision: 2,
    revisions: {
      1: [{ key: "config.yaml", encoding: "text", content: "log_level: info\nport: 8080\n" }],
      2: [{ key: "config.yaml", encoding: "text", content: "log_level: debug\nport: 8080\n" }, { key: "logo.bin", encoding: "base64", content: "AAECAwQFBgc=" }],
    },
    createdAt: "2026-11-10T09:00:00.000Z", updatedAt: "2026-11-16T09:00:00.000Z",
  },
}

/** 保存する大きさ(key の長さ + 内容の長さ)を数える。 */
const storedSize = (files: ConfigMapFile[]) => files.reduce((sum, file) => sum + file.key.length + new TextEncoder().encode(file.content).length, 0)
/** 元のバイト数(base64 は、デコードした大きさ)。 */
const decodedSize = (file: ConfigMapFile) => file.encoding === "base64" ? atob(file.content).length : new TextEncoder().encode(file.content).length
const getConfigMapList = (projectId: string) => Object.values(configMapsById).filter((item) => item.projectId === projectId)
/** ConfigMap を使っている deploy のマウント(現在と pending)。 */
function configMapUsages(configMapId: string) {
  const usages: { deployId: string; revision: number; key: string; path: string; pending: boolean }[] = []
  for (const deploy of Object.values(deployDetails)) {
    for (const pending of [false, true]) {
      const mounts = pending ? deploy.configmapMounts?.pending_mounts : deploy.configmapMounts?.mounts
      for (const mount of mounts ?? []) if (mount.configMapId === configMapId) usages.push({ deployId: deploy.deployId, revision: mount.revision, key: mount.key, path: mount.path, pending })
    }
  }
  return usages
}
/** ConfigMap の取得のレスポンス(指定した版の中身つき)。 */
function configMapView(item: MockConfigMap, revision = item.currentRevision) {
  const files = item.revisions[revision]
  if (!files) throw new Error("ConfigMap の版が見つかりません")
  return { configMapId: item.configMapId, projectId: item.projectId, name: item.name, currentRevision: item.currentRevision, revision, files: [...files].sort((left, right) => left.key.localeCompare(right.key)).map((file) => ({ ...file, size: decodedSize(file) })), createdAt: item.createdAt, updatedAt: item.updatedAt }
}
/** 入力のファイルを検査して、key の昇順にする(encoding の省略は text)。 */
function checkConfigMapFiles(input: unknown): ConfigMapFile[] {
  const files = ((input ?? []) as Partial<ConfigMapFile>[]).map((file) => ({ key: String(file.key ?? ""), encoding: (file.encoding ?? "text") as ConfigMapFile["encoding"], content: String(file.content ?? "") }))
  if (files.length > 50) throw new Error("files は、50個までです")
  if (new Set(files.map((file) => file.key)).size !== files.length) throw new Error("files の key が重複しています")
  for (const file of files) if (!/^[A-Za-z0-9._-]+$/.test(file.key) || file.key === "." || file.key === "..") throw new Error(`files の key の形が正しくありません: ${file.key}`)
  if (storedSize(files) > 1024 * 1024) throw new Error("ファイルの大きさの合計が上限(1MiB)を超えています")
  return files.sort((left, right) => left.key.localeCompare(right.key))
}

let nextId = 1
const newId = (prefix: string) => `${prefix}_${nextId++}_${Math.random().toString(36).slice(2, 7)}`
const clone = <T,>(value: T): T => value === undefined ? value : JSON.parse(JSON.stringify(value)) as T
const getProjectDeploys = (projectId: string) => Object.values(deployDetails).filter((deploy) => deploy.projectId === projectId)
const getVolumeList = (projectId: string) => (volumesByProject[projectId] ??= [])
const getIngressList = (projectId: string) => (ingressesByProject[projectId] ??= [])
const getBuildList = (projectId: string) => (buildsByProject[projectId] ??= [])

function makePods(deploy: DeployDetail) {
  deploy.pods = Array.from({ length: deploy.replicas }, (_, index) => ({
    podId: `${deploy.deployId}-${String(index + 1).padStart(2, "0")}-${Math.random().toString(36).slice(2, 6)}`,
    status: "running",
  }))
  deploy.logPods = deploy.pods.map((pod) => ({ podId: pod.podId, running: true, startedAt: now(), endedAt: null }))
}

function makeDeployListItem(deploy: DeployDetail): DeployListItem {
  return {
    deployId: deploy.deployId,
    status: deploy.status,
    spec: deploy.spec,
    replicas: deploy.replicas,
    imageRef: deploy.imageRef,
    createdAt: deploy.createdAt,
  }
}

export async function mockRequest<T>(path: string, method: string, body?: unknown): Promise<T> {
  await new Promise((resolve) => setTimeout(resolve, 90))
  const url = new URL(path, "http://launchs.mock")
  const segments = url.pathname.split("/").filter(Boolean).map(decodeURIComponent)
  const payload = (body ?? {}) as Record<string, unknown>
  let result: unknown

  if (segments[0] === "auth") {
    if (segments[1] === "config") result = { turnstile: { enabled: false } }
    else if (segments[1] === "login" && method === "POST") {
      const userName = typeof payload.userName === "string" ? payload.userName : "demo"
      currentUser = { ...currentUser, userName, userId: `usr_${userName.toLowerCase().replace(/[^a-z0-9-]/g, "-") || "demo"}` }
      result = { token: "mock-refresh-token" }
    } else if (segments[1] === "refresh") result = { token: "mock-access-token" }
    else if (segments[1] === "me") result = currentUser
    else if (segments[1] === "logout") result = undefined
    else if (segments[1] === "login-history") {
      result = {
        history: [
          { historyId: "hist_current", ip: "127.0.0.1", userAgent: "Local preview · Demo session", createdAt: now() },
          { historyId: "hist_previous", ip: "192.0.2.24", userAgent: "Chrome · macOS", createdAt: "2026-11-15T08:34:00.000Z" },
          { historyId: "hist_older", ip: "192.0.2.24", userAgent: "Chrome · macOS", createdAt: "2026-11-12T16:10:00.000Z" },
        ],
        nextCursor: null,
      }
    }
  } else if (segments[0] === "api" && segments[1] === "v1") {
    if (segments[2] === "quota") {
      const allVolumes = Object.values(volumesByProject).flat()
      const volumeUsed = allVolumes.reduce((sum, volume) => sum + volume.sizeMB, 0)
      result = {
        maxReplicasPerDeploy: 5,
        maxVolumeSizeMB: 10240,
        projects: { used: projects.length, limit: 12 },
        deploys: { used: Object.keys(deployDetails).length, limit: 30 },
        volumes: { used: allVolumes.length, limit: 20 },
        volumeTotalMB: { used: volumeUsed, limit: 102400 },
        specs: { small: { used: 3, limit: 20 }, medium: { used: 1, limit: 10 }, large: { used: 0, limit: 4 } },
      }
    } else if (segments[2] === "projects") {
      const projectId = segments[3]
      const resource = segments[4]
      if (!resource) {
        if (method === "GET") result = { projects: clone(projects), nextCursor: null }
        else if (method === "POST") {
          const project: Project = { projectId: newId("prj"), name: String(payload.name ?? "new-project"), status: "active" }
          projects = [...projects, project]
          volumesByProject[project.projectId] = []
          ingressesByProject[project.projectId] = []
          buildsByProject[project.projectId] = []
          result = { projectId: project.projectId }
        } else if (method === "PATCH") {
          const project = projects.find((item) => item.projectId === projectId)
          if (project) project.name = String(payload.name ?? project.name)
          result = undefined
        } else if (method === "DELETE") {
          projects = projects.filter((project) => project.projectId !== projectId)
          for (const deploy of getProjectDeploys(projectId)) delete deployDetails[deploy.deployId]
          delete volumesByProject[projectId]
          delete ingressesByProject[projectId]
          delete buildsByProject[projectId]
          result = undefined
        }
      } else if (resource === "deploys") result = { deploys: getProjectDeploys(projectId).map(makeDeployListItem), nextCursor: null }
      else if (resource === "volumes") {
        const list = getVolumeList(projectId)
        if (segments.length === 5 && method === "GET") result = { volumes: clone(list), nextCursor: null }
        else if (segments.length === 5 && method === "POST") {
          const volume: Volume = { volumeId: newId("vol"), name: String(payload.name ?? "new-volume"), status: "active", sizeMB: Number(payload.sizeMB ?? 1024), pending_sizeMB: null, createdAt: now() }
          list.unshift(volume)
          result = { volumeId: volume.volumeId }
        } else {
          const volume = list.find((item) => item.volumeId === segments[5])
          if (!volume) throw new Error("ボリュームが見つかりません")
          if (segments.length === 6 && method === "PATCH") {
            volume.name = String(payload.name ?? volume.name)
            result = undefined
          } else if (segments[6] === "size" && method === "PATCH") {
            volume.pending_sizeMB = Number(payload.sizeMB)
            result = undefined
          } else if (segments[6] === "apply" && method === "POST") {
            if (volume.pending_sizeMB !== null) volume.sizeMB = volume.pending_sizeMB
            volume.pending_sizeMB = null
            volume.status = "active"
            result = undefined
          } else if (segments[6] === "discard" && method === "POST") {
            volume.pending_sizeMB = null
            result = undefined
          } else if (segments.length === 6 && method === "DELETE") {
            volumesByProject[projectId] = list.filter((item) => item.volumeId !== volume.volumeId)
            result = undefined
          }
        }
      } else if (resource === "configmaps") {
        if (method === "GET") {
          result = {
            configmaps: getConfigMapList(projectId).map((item) => ({ configMapId: item.configMapId, name: item.name, currentRevision: item.currentRevision, fileCount: item.revisions[item.currentRevision].length, totalBytes: storedSize(item.revisions[item.currentRevision]), usedBy: new Set(configMapUsages(item.configMapId).map((usage) => usage.deployId)).size, updatedAt: item.updatedAt })),
            nextCursor: null,
          }
        } else if (method === "POST") {
          const name = String(payload.name ?? "")
          if (!/^[A-Za-z0-9-]{1,64}$/.test(name)) throw new Error("name は、英数字とハイフンだけの1〜64文字でなければなりません")
          if (getConfigMapList(projectId).some((item) => item.name === name)) throw new Error("同じ名前の ConfigMap がすでにあります")
          const item: MockConfigMap = { configMapId: newId("cm"), projectId, name, currentRevision: 1, revisions: { 1: checkConfigMapFiles(payload.files) }, createdAt: now(), updatedAt: now() }
          configMapsById[item.configMapId] = item
          result = configMapView(item)
        }
      } else if (resource === "builds" && method === "GET") result = { builds: clone(getBuildList(projectId)), nextCursor: null }
      else if (resource === "ingresses") {
        const list = getIngressList(projectId)
        if (segments.length === 5 && method === "GET") result = { ingresses: clone(list.map(({ ingressId, host }) => ({ ingressId, host } satisfies IngressListItem))), nextCursor: null }
        else if (segments.length === 5 && method === "POST") {
          const ingress: IngressDetail = { ingressId: newId("ing"), host: `${newId("edge").toLowerCase()}.launchs.dev`, rules: [], pending_rules: null, status: "active" }
          list.unshift(ingress)
          result = { ingressId: ingress.ingressId, host: ingress.host }
        } else {
          const ingress = list.find((item) => item.ingressId === segments[5])
          if (!ingress) throw new Error("Ingress が見つかりません")
          if (segments.length === 6 && method === "GET") result = clone(ingress)
          else if (segments.length === 6 && method === "PUT") {
            ingress.pending_rules = clone((payload.rules ?? []) as IngressDetail["rules"])
            result = undefined
          } else if (segments[6] === "apply" && method === "POST") {
            if (ingress.pending_rules !== null) ingress.rules = ingress.pending_rules
            ingress.pending_rules = null
            ingress.status = "active"
            result = undefined
          } else if (segments[6] === "discard" && method === "POST") {
            ingress.pending_rules = null
            result = undefined
          } else if (segments.length === 6 && method === "DELETE") {
            ingressesByProject[projectId] = list.filter((item) => item.ingressId !== ingress.ingressId)
            result = undefined
          }
        }
      }
    } else if (segments[2] === "configmaps" && segments[3]) {
      const item = configMapsById[segments[3]]
      if (!item) throw new Error("ConfigMap が見つかりません")
      if (segments.length === 4 && method === "GET") result = configMapView(item, url.searchParams.get("revision") ? Number(url.searchParams.get("revision")) : undefined)
      else if (segments.length === 4 && method === "PUT") {
        // 全件置き換え。中身が同じなら、版は増えない。使っている版は、残す。
        const files = checkConfigMapFiles(payload.files)
        if (JSON.stringify(files) !== JSON.stringify(item.revisions[item.currentRevision])) {
          item.currentRevision += 1
          item.revisions[item.currentRevision] = files
          item.updatedAt = now()
          const inUse = new Set(configMapUsages(item.configMapId).map((usage) => usage.revision))
          for (const revision of Object.keys(item.revisions).map(Number)) if (revision <= item.currentRevision - 10 && !inUse.has(revision)) delete item.revisions[revision]
        }
        result = configMapView(item)
      } else if (segments.length === 4 && method === "PATCH") {
        const name = String(payload.name ?? "")
        if (!/^[A-Za-z0-9-]{1,64}$/.test(name)) throw new Error("name は、英数字とハイフンだけの1〜64文字でなければなりません")
        if (getConfigMapList(item.projectId).some((other) => other.name === name && other.configMapId !== item.configMapId)) throw new Error("同じ名前の ConfigMap がすでにあります")
        item.name = name
        item.updatedAt = now()
        result = undefined
      } else if (segments.length === 4 && method === "DELETE") {
        const users = Array.from(new Set(configMapUsages(item.configMapId).map((usage) => usage.deployId)))
        if (users.length > 0) throw new Error(`deploy が使っているため、ConfigMap を削除できません。使っている deploy: ${users.join(", ")}`)
        delete configMapsById[item.configMapId]
        result = undefined
      } else if (segments[4] === "revisions" && method === "GET") {
        result = { revisions: Object.keys(item.revisions).map(Number).sort((left, right) => right - left).map((revision) => ({ revision, fileCount: item.revisions[revision].length, totalBytes: storedSize(item.revisions[revision]), createdAt: item.updatedAt })) }
      } else if (segments[4] === "usage" && method === "GET") result = { usages: configMapUsages(item.configMapId) }
    } else if (segments[2] === "deploy" && segments.length === 3 && method === "POST") {
      const projectId = String(payload.projectId ?? "")
      const deployId = newId("dep")
      const imageRef = String(payload.imageRef ?? "ghcr.io/example/app:latest")
      const deploy: DeployDetail = {
        projectId,
        deployId,
        status: "active",
        spec: (payload.spec as DeployDetail["spec"]) ?? "small",
        replicas: 1,
        imageRef,
        createdAt: now(),
        pods: [],
        logPods: [],
        config: { env: {}, pending_env: null, command: null, pending_command: null, args: null, pending_args: null },
        volumes: { mounts: [], pending_mounts: null },
        service: null,
      }
      makePods(deploy)
      deployDetails[deployId] = deploy
      result = { deployId }
    } else if (segments[2] === "build" && method === "POST") {
      const projectId = String(payload.projectId ?? "")
      const git = payload.git as { repoUrl?: string; ref?: string } | undefined
      const repo = git?.repoUrl?.replace(/\.git$/, "") ?? "https://github.com/example/app"
      const repoName = repo.split("/").filter(Boolean).pop() ?? "app"
      const imageRef = `ghcr.io/launchs/${repoName}:${git?.ref || "main"}`
      getBuildList(projectId).unshift({ buildId: newId("bld"), status: "success", imageRef, createdAt: now() })
      result = { buildId: getBuildList(projectId)[0].buildId }
    } else if (segments[2] === "deploy" && segments[3]) {
      const deploy = deployDetails[segments[3]]
      if (!deploy) throw new Error("デプロイが見つかりません")
      const action = segments[4]
      if (!action && method === "GET") result = clone(deploy)
      else if (action === "scale" && method === "POST") {
        deploy.replicas = Math.max(0, Math.min(5, Number(payload.replicas ?? deploy.replicas)))
        deploy.status = "active"
        makePods(deploy)
        result = undefined
      } else if (action === "service") {
        if (segments.length === 5 && method === "GET") {
          if (!deploy.service) throw new Error("Service がありません")
          result = clone(deploy.service)
        } else if (segments.length === 5 && method === "POST") {
          deploy.service = {
            serviceId: newId("svc"),
            ports: clone((payload.ports ?? []) as ServicePort[]),
            pending_ports: null,
            status: "active",
            dnsName: `${deploy.deployId}.${deploy.projectId}.svc.cluster.local`,
            clusterIP: `10.96.${Math.floor(Math.random() * 200)}.${Math.floor(Math.random() * 200)}`,
          }
          result = undefined
        } else if (segments.length === 5 && method === "PATCH" && deploy.service) {
          deploy.service.pending_ports = clone((payload.ports ?? []) as ServicePort[])
          result = undefined
        } else if (segments[5] === "apply" && method === "POST" && deploy.service) {
          if (deploy.service.pending_ports !== null) deploy.service.ports = deploy.service.pending_ports
          deploy.service.pending_ports = null
          deploy.service.status = "active"
          result = undefined
        } else if (segments[5] === "discard" && method === "POST" && deploy.service) {
          deploy.service.pending_ports = null
          result = undefined
        }
      } else if (action === "config") {
        if (segments.length === 5 && method === "PUT") {
          deploy.config.pending_env = clone((payload.env ?? {}) as Record<string, string>)
          deploy.config.pending_command = (payload.command as string | null) ?? null
          deploy.config.pending_args = clone((payload.args ?? []) as string[])
          result = undefined
        } else if (segments[5] === "apply" && method === "POST") {
          const config = deploy.config
          if (config.pending_env !== null) config.env = config.pending_env
          if (config.pending_command !== null) config.command = config.pending_command
          if (config.pending_args !== null) config.args = config.pending_args
          config.pending_env = null
          config.pending_command = null
          config.pending_args = null
          result = undefined
        } else if (segments[5] === "discard" && method === "POST") {
          deploy.config.pending_env = null
          deploy.config.pending_command = null
          deploy.config.pending_args = null
          result = undefined
        }
      } else if (action === "mounts") {
        if (segments.length === 5 && method === "PUT") {
          deploy.volumes.pending_mounts = clone((payload.mounts ?? []) as DeployDetail["volumes"]["mounts"])
          result = undefined
        } else if (segments[5] === "apply" && method === "POST") {
          if (deploy.volumes.pending_mounts !== null) deploy.volumes.mounts = deploy.volumes.pending_mounts
          deploy.volumes.pending_mounts = null
          result = undefined
        } else if (segments[5] === "discard" && method === "POST") {
          deploy.volumes.pending_mounts = null
          result = undefined
        }
      } else if (action === "configmap-mounts") {
        const state = (deploy.configmapMounts ??= { mounts: [], pending_mounts: null })
        // 名前と、最新の版を付ける(GET だけ)。
        const decorate = (mounts: ConfigMapMount[]) => mounts.map((mount) => ({ ...mount, configMapName: configMapsById[mount.configMapId]?.name, latestRevision: configMapsById[mount.configMapId]?.currentRevision }))
        if (segments.length === 5 && method === "GET") result = { mounts: decorate(state.mounts), pending_mounts: state.pending_mounts === null ? null : decorate(state.pending_mounts) }
        else if (segments.length === 5 && method === "PUT") {
          const requested = (payload.mounts ?? []) as Partial<ConfigMapMount>[]
          if (requested.length > 30) throw new Error("mounts は、30件までです")
          const previous = new Map((state.pending_mounts ?? state.mounts).map((mount) => [`${mount.configMapId}\u0000${mount.key}`, mount.revision]))
          const resolved: ConfigMapMount[] = requested.map((mount) => {
            const item = configMapsById[String(mount.configMapId)]
            if (!item || item.projectId !== deploy.projectId) throw new Error("このプロジェクトに、この ConfigMap がありません")
            // 版は、指定が無ければ、すでにマウントしている版を保ち、無ければ最新。
            const revision = mount.revision || previous.get(`${mount.configMapId}\u0000${mount.key}`) || item.currentRevision
            if (!item.revisions[revision]?.some((file) => file.key === mount.key)) throw new Error("ConfigMap の版に、この key のファイルがありません")
            if (!/^\/[A-Za-z0-9._\-/]*$/.test(String(mount.path)) || String(mount.path).includes("..")) throw new Error("mounts の path の形が正しくありません")
            if (mount.mode && !/^0[0-7]{3}$/.test(mount.mode)) throw new Error("mounts の mode は、8進数の4桁の文字列にしてください")
            return { configMapId: item.configMapId, revision, key: String(mount.key), path: String(mount.path), mode: mount.mode || "0644" }
          })
          if (new Set(resolved.map((mount) => mount.path)).size !== resolved.length) throw new Error("mounts の path が重複しています")
          state.pending_mounts = JSON.stringify(resolved) === JSON.stringify(state.mounts) ? null : resolved
          result = undefined
        } else if (segments[5] === "apply" && method === "POST") {
          if (state.pending_mounts !== null) state.mounts = state.pending_mounts
          state.pending_mounts = null
          result = undefined
        } else if (segments[5] === "discard" && method === "POST") {
          state.pending_mounts = null
          result = undefined
        }
      } else if (action === "logs" && method === "GET") {
        const since = Number(url.searchParams.get("since") ?? 0)
        result = {
          logs: since ? "" : `[${new Date().toISOString()}] INFO  container started\n[${new Date().toISOString()}] INFO  health check passed\n[${new Date().toISOString()}] INFO  listening on 0.0.0.0\n`,
          nextSince: since + 3,
        }
      } else if (!action && method === "DELETE") {
        delete deployDetails[deploy.deployId]
        result = undefined
      }
    }
  }

  if (result === undefined && method !== "POST" && method !== "PUT" && method !== "PATCH" && method !== "DELETE") {
    throw new Error(`Mock API route not found: ${method} ${url.pathname}`)
  }
  return clone(result) as T
}
