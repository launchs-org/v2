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

/** refresh token を保存するキー(localStorage)。access token は、短命なので、メモリだけに置く。 */
const REFRESH_TOKEN_KEY = "launchs.refreshToken"

/**
 * 保存してある refresh token を、読む(保存できない環境、プライベートモードなどでは、無かったことにする)。
 *
 * @returns 保存してある refresh token。無ければ null。
 */
function loadStoredRefreshToken(): string | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage.getItem(REFRESH_TOKEN_KEY)
  } catch {
    return null
  }
}

/**
 * refresh token を、保存する(null なら、消す)。保存できなくても、画面は動く(再読み込みで、ログインが要るだけ)。
 *
 * @param token 保存する refresh token。消すときは null。
 */
function storeRefreshToken(token: string | null) {
  try {
    if (typeof window === "undefined") return
    if (token === null) window.localStorage.removeItem(REFRESH_TOKEN_KEY)
    else window.localStorage.setItem(REFRESH_TOKEN_KEY, token)
  } catch {
    // 保存できないときは、メモリだけで動かす。
  }
}

export function clearSessionTokens() {
  accessToken = null
  refreshToken = null
  refreshPromise = null
  storeRefreshToken(null)
}

/**
 * 画面を開いたとき、保存してある refresh token でログインの状態を戻す。
 * 保存が無い、期限切れ、無効(ログアウト済み、BAN)のときは、null を返し、保存も消す。
 *
 * @returns ログインしているユーザー。戻せなければ null。
 */
export async function restoreSession(): Promise<import("./types").User | null> {
  // live でないとき(見せかけのデータ)は、戻さない。
  if (process.env.NEXT_PUBLIC_LAUNCHS_API_MODE !== "live") return null
  const stored = loadStoredRefreshToken()
  if (!stored) return null
  refreshToken = stored
  try {
    // access token を取り直して、自分の情報を読む。
    await refreshAccessToken()
    return await request<import("./types").User>("/auth/me", "GET")
  } catch (error) {
    // サーバーに、無効と言われたときだけ、保存を消す。ネットワークの失敗では、消さない(後で、また戻せる)。
    if (error instanceof ApiError && error.status === 401) clearSessionTokens()
    else {
      accessToken = null
      refreshToken = null
    }
    return null
  }
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
      try {
        const data = process.env.NEXT_PUBLIC_LAUNCHS_API_MODE !== "live"
          ? await mockRequest<{ token: string }>("/auth/refresh", "POST")
          : await readResponse<{ token: string }>(await fetch("/auth/refresh", jsonInit("POST", undefined, refreshToken)))
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
  // 再読み込みしても、ログインが続くように、保存する。
  storeRefreshToken(result.token)
  await refreshAccessToken()
  return request<import("./types").User>("/auth/me", "GET")
}

/**
 * サインアップして、そのままログインの状態にする。
 *
 * @param userName ユーザー名。
 * @param password パスワード。
 * @param invitationCode 招待コード(招待制のときだけ)。
 * @param turnstileToken 人間であることの確認のトークン(有効なときだけ)。
 * @returns ログインしているユーザー。
 */
export async function signUp(userName: string, password: string, invitationCode?: string, turnstileToken?: string) {
  const result = await request<{ token: string }>("/auth/signup", "POST", {
    userName,
    password,
    ...(invitationCode ? { invitationCode } : {}),
    ...(turnstileToken ? { turnstileToken } : {}),
  }, false)
  refreshToken = result.token
  storeRefreshToken(result.token)
  await refreshAccessToken()
  return request<import("./types").User>("/auth/me", "GET")
}

/**
 * ログイン中の本人が、パスワードを変える。成功すると、古いセッションは全て無効になり、新しい refresh token で、ログインのままになる。
 *
 * @param currentPassword 現在のパスワード(一時パスワードでもよい)。
 * @param newPassword 新しいパスワード。
 * @returns 最新のユーザー情報(一時パスワードの要求は、外れている)。
 */
export async function changePassword(currentPassword: string, newPassword: string) {
  const result = await request<{ token: string }>("/auth/password", "POST", { currentPassword, newPassword })
  // 新しい refresh token に差し替えて、access token も取り直す。
  refreshToken = result.token
  storeRefreshToken(result.token)
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
