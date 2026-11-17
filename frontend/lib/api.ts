import { mockRequest } from "@/lib/mock-api"

type ApiErrorPayload = { error?: { message?: string; code?: string } }

export class ApiError extends Error {
  status: number
  code?: string

  constructor(message: string, status: number, code?: string) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.code = code
  }
}

let accessToken: string | null = null
let refreshToken: string | null = null
let refreshPromise: Promise<string> | null = null

export function clearSessionTokens() {
  accessToken = null
  refreshToken = null
  refreshPromise = null
}

async function readResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    let payload: ApiErrorPayload = {}
    try {
      payload = await response.json()
    } catch {
      // Some reverse proxies return a plain-text error response.
    }
    throw new ApiError(payload.error?.message || `リクエストに失敗しました (${response.status})`, response.status, payload.error?.code)
  }
  if (response.status === 204) return undefined as T
  const text = await response.text()
  return (text ? JSON.parse(text) : undefined) as T
}

function jsonInit(method: string, body?: unknown, token?: string | null): RequestInit {
  const headers = new Headers({ Accept: "application/json" })
  if (body !== undefined) headers.set("Content-Type", "application/json")
  if (token) headers.set("Authorization", `Bearer ${token}`)
  return {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    cache: "no-store",
  }
}

async function refreshAccessToken() {
  if (!refreshToken) throw new ApiError("セッションの有効期限が切れました。再度ログインしてください。", 401, "unauthorized")
  if (!refreshPromise) {
    refreshPromise = (async () => {
      const response = await fetch("/auth/refresh", jsonInit("POST", undefined, refreshToken))
      try {
        const data = await readResponse<{ token: string }>(response)
        accessToken = data.token
        return data.token
      } catch (error) {
        clearSessionTokens()
        if (typeof window !== "undefined") window.dispatchEvent(new Event("launchs:session-expired"))
        throw error
      } finally {
        refreshPromise = null
      }
    })()
  }
  return refreshPromise
}

async function request<T>(path: string, method: string, body?: unknown, authenticated = true, tokenOverride?: string | null): Promise<T> {
  if (process.env.NEXT_PUBLIC_LAUNCHS_API_MODE !== "live") return mockRequest<T>(path, method, body)

  let token = authenticated ? accessToken : tokenOverride ?? null
  let response = await fetch(path, jsonInit(method, body, token))
  if (response.status === 401 && authenticated && refreshToken) {
    token = await refreshAccessToken()
    response = await fetch(path, jsonInit(method, body, token))
  }
  return readResponse<T>(response)
}

export async function signIn(userName: string, password: string, turnstileToken?: string) {
  const result = await request<{ token: string }>("/auth/login", "POST", {
    userName,
    password,
    ...(turnstileToken ? { turnstileToken } : {}),
  }, false)
  refreshToken = result.token
  await refreshAccessToken()
  return request<import("./types").User>("/auth/me", "GET")
}

export async function signOut() {
  const token = refreshToken
  try {
    if (token) await request("/auth/logout", "POST", undefined, false, token)
  } finally {
    clearSessionTokens()
  }
}

export const api = {
  get: <T>(path: string) => request<T>(path, "GET"),
  post: <T>(path: string, body?: unknown) => request<T>(path, "POST", body),
  put: <T>(path: string, body?: unknown) => request<T>(path, "PUT", body),
  patch: <T>(path: string, body?: unknown) => request<T>(path, "PATCH", body),
  delete: <T>(path: string) => request<T>(path, "DELETE"),
}

export async function publicFetcher<T>(path: string): Promise<T> {
  return request<T>(path, "GET", undefined, false)
}

export async function authenticatedFetcher<T>(path: string): Promise<T> {
  return api.get<T>(path)
}
