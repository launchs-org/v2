"use client"

import { useEffect, useState } from "react"
import useSWR from "swr"
import { api, authenticatedFetcher } from "@/lib/api"
import { ErrorState, LoadingState, ToastNotice } from "@/components/ui-kit"

type Defaults = { projects: number; deploys: number; volumes: number; volumeTotalMB: number; deploysBySpec: Record<string, number> }
type Item = { limit: number; used: number }
type UserQuota = {
  projects: Item; deploys: Item; volumes: Item; volumeTotalMB: Item; deploysBySpec: Record<string, Item>
  overrides: { projects: number | null; deploys: number | null; volumes: number | null; volumeTotalMB: number | null; deploysBySpec: Record<string, number> | null }
}
type UserList = { users: { userId: string; userName: string }[] }

/** 数の項目の、名前と表示。 */
const FIELDS = [
  { key: "projects", label: "プロジェクトの数" },
  { key: "deploys", label: "deploy の数(合計)" },
  { key: "volumes", label: "volume の数" },
  { key: "volumeTotalMB", label: "volume の合計(MB)" },
] as const

/** 入力の文字を、数にする(空は null。整数でなければ NaN)。 */
function toNumber(text: string): number | null {
  return text.trim() === "" ? null : Number(text)
}

/**
 * 全体のクォータ(全ユーザーの既定)の編集。保存すると、すぐに、新しい作成から効く(既にある分は、消さない)。
 */
function DefaultsEditor() {
  const { data, error, mutate } = useSWR<Defaults>("/admin-api/quota-defaults", authenticatedFetcher)
  // 入力中の値(文字)。
  const [form, setForm] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<{ message: string; type: "success" | "error" } | null>(null)

  // 取得したら、入力欄に写す。
  useEffect(() => {
    if (!data) return
    const next: Record<string, string> = {}
    for (const field of FIELDS) next[field.key] = String(data[field.key])
    for (const [spec, limit] of Object.entries(data.deploysBySpec)) next[`spec:${spec}`] = String(limit)
    setForm(next)
  }, [data])

  /** 入力を検査して、保存する。 */
  async function save() {
    if (!data) return
    // 全ての項目を、0 以上の整数にする。
    const numbers: Record<string, number> = {}
    for (const [key, text] of Object.entries(form)) {
      const value = toNumber(text)
      if (value === null || !Number.isInteger(value) || value < 0) { setToast({ message: "全ての項目に、0 以上の整数を入れてください", type: "error" }); return }
      numbers[key] = value
    }
    setBusy(true)
    try {
      const body: Defaults = {
        projects: numbers.projects, deploys: numbers.deploys, volumes: numbers.volumes, volumeTotalMB: numbers.volumeTotalMB,
        deploysBySpec: Object.fromEntries(Object.keys(data.deploysBySpec).map((spec) => [spec, numbers[`spec:${spec}`]])),
      }
      await api.put("/admin-api/quota-defaults", body)
      await mutate()
      setToast({ message: "全体のクォータを保存しました", type: "success" })
    } catch (saveError) {
      setToast({ message: saveError instanceof Error ? saveError.message : "保存できませんでした", type: "error" })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div><h2 className="h2">全体のクォータ(既定)</h2><div className="faint" style={{ fontSize: 12.5 }}>ユーザーごとの上書きが無い人に、効きます。既に上限を超えている分は、消えません(新しく作れなくなるだけ)。</div></div>
      {!data && !error && <LoadingState />}
      {error && !data && <ErrorState message="取得できませんでした" onRetry={() => void mutate()} />}
      {data && (
        <>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(min(220px, 100%), 1fr))", gap: 12, alignItems: "start" }}>
            {FIELDS.map((field) => <label key={field.key} className="lbl">{field.label}<input className="inp" inputMode="numeric" value={form[field.key] ?? ""} onChange={(event) => setForm({ ...form, [field.key]: event.target.value })} /></label>)}
            {Object.keys(data.deploysBySpec).sort().map((spec) => <label key={spec} className="lbl">{spec} の deploy の数<input className="inp" inputMode="numeric" value={form[`spec:${spec}`] ?? ""} onChange={(event) => setForm({ ...form, [`spec:${spec}`]: event.target.value })} /></label>)}
          </div>
          <div><button className="btn primary" type="button" disabled={busy} onClick={() => void save()}>保存</button></div>
        </>
      )}
      {toast && <ToastNotice message={toast.message} type={toast.type} onDismiss={() => setToast(null)} />}
    </div>
  )
}

/**
 * ユーザーごとのクォータ(上書き)の編集。ユーザーを選ぶと、上限と使用量が出る。空欄は、既定を使う。
 * backend の管理者 API(/api/v1/admin/users/:userId/quota)を使う。
 */
function UserQuotaEditor() {
  const { data: users } = useSWR<UserList>("/auth/admin/users?limit=100", authenticatedFetcher)
  const [userId, setUserId] = useState("")
  const { data, error, mutate } = useSWR<UserQuota>(userId ? `/api/v1/admin/users/${encodeURIComponent(userId)}/quota` : null, authenticatedFetcher)
  const [form, setForm] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const [toast, setToast] = useState<{ message: string; type: "success" | "error" } | null>(null)

  // 取得したら、上書きの値を入力欄に写す(上書きが無い項目は、空)。
  useEffect(() => {
    if (!data) return
    const next: Record<string, string> = {}
    for (const field of FIELDS) next[field.key] = data.overrides[field.key]?.toString() ?? ""
    for (const spec of Object.keys(data.deploysBySpec)) next[`spec:${spec}`] = data.overrides.deploysBySpec?.[spec]?.toString() ?? ""
    setForm(next)
  }, [data])

  /** 入力を検査して、一式で置き換える(空欄は、既定に戻る)。 */
  async function save() {
    if (!data) return
    const body: Record<string, unknown> = {}
    for (const field of FIELDS) {
      const value = toNumber(form[field.key] ?? "")
      if (value !== null && (!Number.isInteger(value) || value < 0)) { setToast({ message: "0 以上の整数か、空欄にしてください", type: "error" }); return }
      body[field.key] = value
    }
    const bySpec: Record<string, number> = {}
    for (const spec of Object.keys(data.deploysBySpec)) {
      const value = toNumber(form[`spec:${spec}`] ?? "")
      if (value === null) continue
      if (!Number.isInteger(value) || value < 0) { setToast({ message: "0 以上の整数か、空欄にしてください", type: "error" }); return }
      bySpec[spec] = value
    }
    body.deploysBySpec = bySpec
    setBusy(true)
    try {
      await api.put(`/api/v1/admin/users/${encodeURIComponent(userId)}/quota`, body)
      await mutate()
      setToast({ message: "このユーザーのクォータを保存しました", type: "success" })
    } catch (saveError) {
      setToast({ message: saveError instanceof Error ? saveError.message : "保存できませんでした", type: "error" })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="card pad" style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div><h2 className="h2">ユーザーごとのクォータ</h2><div className="faint" style={{ fontSize: 12.5 }}>空欄は、全体の既定を使います。保存は、一式の置き換えです。</div></div>
      <select className="inp" style={{ maxWidth: 360 }} value={userId} onChange={(event) => setUserId(event.target.value)} aria-label="ユーザーを選ぶ">
        <option value="">ユーザーを選んでください</option>
        {users?.users.map((user) => <option key={user.userId} value={user.userId}>{user.userName}</option>)}
      </select>
      {userId && !data && !error && <LoadingState />}
      {userId && error && !data && <ErrorState message="取得できませんでした" onRetry={() => void mutate()} />}
      {data && (
        <>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(min(240px, 100%), 1fr))", gap: 12, alignItems: "start" }}>
            {FIELDS.map((field) => (
              <label key={field.key} className="lbl">{field.label}
                <input className="inp" inputMode="numeric" placeholder="既定" value={form[field.key] ?? ""} onChange={(event) => setForm({ ...form, [field.key]: event.target.value })} />
                <span className="faint" style={{ fontSize: 11.5, fontWeight: 400 }}>上限 {data[field.key].limit} ・ 使用中 {data[field.key].used}</span>
              </label>
            ))}
            {Object.keys(data.deploysBySpec).sort().map((spec) => (
              <label key={spec} className="lbl">{spec} の deploy の数
                <input className="inp" inputMode="numeric" placeholder="既定" value={form[`spec:${spec}`] ?? ""} onChange={(event) => setForm({ ...form, [`spec:${spec}`]: event.target.value })} />
                <span className="faint" style={{ fontSize: 11.5, fontWeight: 400 }}>上限 {data.deploysBySpec[spec].limit} ・ 使用中 {data.deploysBySpec[spec].used}</span>
              </label>
            ))}
          </div>
          <div><button className="btn primary" type="button" disabled={busy} onClick={() => void save()}>保存</button></div>
        </>
      )}
      {toast && <ToastNotice message={toast.message} type={toast.type} onDismiss={() => setToast(null)} />}
    </div>
  )
}

/** クォータの管理(全体の既定と、ユーザーごとの上書き)。 */
export function AdminQuota() {
  return <><DefaultsEditor /><UserQuotaEditor /></>
}
