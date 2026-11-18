// テンプレートから作るウィザード用の、入力フォームの、定数と、純粋関数。
// 検査の規則は、サーバー(shared/template/values.go)と同じにする。サーバーも確かめるが、送る前に、すぐ知らせる。
import type { Template, TemplateApplyStep, TemplateVariable } from "@/lib/types"

/** カテゴリの、画面に出す名前(API の category の値から)。 */
export const CATEGORY_LABELS: Record<string, string> = { database: "データベース", web: "Web", cache: "キャッシュ", tool: "ツール" }

/** ステップの種類の、画面に出す名前。 */
export const STEP_KIND_LABELS: Record<TemplateApplyStep["kind"], string> = {
  volume: "ボリューム",
  configmap: "ConfigMap",
  deploy: "デプロイ",
  service: "公開ポート(Service)",
  ingress: "公開 URL(Ingress)",
}

/** 入力欄 1 つの状態。password は、「自動で作る」(auto)にできる。 */
export type FieldState = { value: string; auto: boolean }

/**
 * テンプレートの変数から、入力欄の初期の状態を作る。
 *
 * @param template テンプレート。
 * @returns 変数名 → 状態。既定値があれば、それが入り、自動生成の password は「自動で作る」になる。
 */
export function initialFields(template: Template): Record<string, FieldState> {
  // 変数ごとに、既定値(無ければ空)と、自動かどうかを決める。
  const fields: Record<string, FieldState> = {}
  for (const variable of template.variables) {
    fields[variable.name] = { value: variable.default ?? (variable.type === "boolean" ? "false" : ""), auto: variable.type === "password" && variable.generate }
  }
  return fields
}

/**
 * 変数 1 つの入力を検査する。サーバー(validateValue)と同じ規則。
 *
 * @param variable 変数の定義。
 * @param field 入力の状態。
 * @returns 問題の説明。問題がなければ空文字。
 */
export function validateField(variable: TemplateVariable, field: FieldState): string {
  // 自動で作る password は、入力が無い(サーバーが作る)。
  if (variable.type === "password" && field.auto) return ""
  const value = field.value
  // 空のとき: サーバーは「入力が無い」と扱い、既定値か自動生成で補う。補うものが無ければ、入力が必要。
  if (value === "") {
    const filledByServer = variable.default !== null || (variable.type === "password" && variable.generate)
    return filledByServer ? "" : `${variable.label}を入力してください。`
  }
  switch (variable.type) {
    case "integer": {
      if (!/^-?\d+$/.test(value)) return "整数で入力してください。"
      const number = Number(value)
      if (variable.min !== null && number < variable.min) return `${variable.min} 以上で入力してください。`
      if (variable.max !== null && number > variable.max) return `${variable.max} 以下で入力してください。`
      return ""
    }
    case "choice":
      return variable.options.some((option) => option.value === value) ? "" : "選択肢から選んでください。"
    case "boolean":
      return value === "true" || value === "false" ? "" : "選んでください。"
    default: {
      // string と password: 長さ、形式、改行。
      const length = Array.from(value).length
      if (variable.minLength !== null && length < variable.minLength) return `${variable.minLength} 文字以上で入力してください。`
      if (variable.maxLength !== null && length > variable.maxLength) return `${variable.maxLength} 文字以下で入力してください。`
      if (/[\r\n\0]/.test(value)) return "改行は使えません。"
      if (variable.pattern) {
        try {
          if (!new RegExp(`^(?:${variable.pattern})$`).test(value)) return "形式が合いません。"
        } catch {
          // ブラウザで解釈できない正規表現は、サーバーの検査に任せる。
        }
      }
      return ""
    }
  }
}

/**
 * テンプレートの、全ての入力を検査する。
 *
 * @param template テンプレート。
 * @param fields 入力の状態。
 * @returns 変数名 → 問題の説明(問題のあるものだけ)。
 */
export function validateFields(template: Template, fields: Record<string, FieldState>): Record<string, string> {
  // 変数ごとに検査して、問題のあるものを集める。
  const problems: Record<string, string> = {}
  for (const variable of template.variables) {
    const problem = validateField(variable, fields[variable.name] ?? { value: "", auto: false })
    if (problem) problems[variable.name] = problem
  }
  return problems
}

/**
 * API に送る values を作る。自動で作る password と、空の値は、送らない(サーバーが、既定値か自動生成で補う)。
 *
 * @param template テンプレート。
 * @param fields 入力の状態。
 * @returns 変数名 → 値。
 */
export function buildValues(template: Template, fields: Record<string, FieldState>): Record<string, string> {
  // 自動と空を除いて集める。
  const values: Record<string, string> = {}
  for (const variable of template.variables) {
    const field = fields[variable.name]
    if (!field || (variable.type === "password" && field.auto) || field.value === "") continue
    values[variable.name] = field.value
  }
  return values
}

/**
 * テンプレートが何を作るかを、短い文にする。
 *
 * @param template テンプレート。
 * @returns 「デプロイ 2 · ボリューム 2 · ConfigMap 1 · 公開 URL あり」の形。
 */
export function summaryText(template: Template): string {
  // 0 でないものだけを並べる。
  const parts = [`デプロイ ${template.summary.deploys}`]
  if (template.summary.volumes > 0) parts.push(`ボリューム ${template.summary.volumes}`)
  if (template.summary.configmaps > 0) parts.push(`ConfigMap ${template.summary.configmaps}`)
  if (template.summary.ingress) parts.push("公開 URL あり")
  return parts.join(" · ")
}

/**
 * 進み具合のステップを、画面に出す文にする。
 *
 * @param step ステップ。
 * @returns 「デプロイ db」の形。
 */
export function stepLabel(step: Pick<TemplateApplyStep, "kind" | "name">): string {
  // service は、deploy の name なので、「(deploy の name)の公開ポート」の形にする。
  return step.kind === "ingress" ? STEP_KIND_LABELS.ingress : `${STEP_KIND_LABELS[step.kind]} ${step.name}`
}
