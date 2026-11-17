// 画面の切り替え(ナビゲーション)の型。外枠(console-shell)が持ち、各画面に渡す。
import type { Project } from "@/lib/types"

/** 画面の種類。プロジェクトを選ばなくても開ける画面(projects、quota、account)と、プロジェクトの中の画面。 */
export type View = "projects" | "quota" | "account" | "overview" | "deploys" | "builds" | "services" | "ingresses" | "volumes" | "settings"

/** deploy の詳細のタブ。 */
export type DeployTab = "overview" | "config" | "mounts" | "service" | "logs"

/** 画面を移るための操作。 */
export type Nav = {
  /** 画面を切り替える(deploy の詳細を開いていれば、閉じる)。 */
  go: (view: View) => void
  /** deploy の詳細を開く(タブを指定できる。既定は概要)。 */
  openDeploy: (deployId: string, tab?: DeployTab) => void
  /** deploy の詳細を閉じて、一覧に戻る。 */
  closeDeploy: () => void
  /** プロジェクトを選んで、概要を開く。 */
  openProject: (project: Project) => void
}

/** プロジェクトの中の画面が受け取る、共通の引数。 */
export type ProjectViewProps = {
  project: Project
  /** 自動更新の間隔(ミリ秒)。0 なら止める。 */
  refreshInterval: number
  nav: Nav
  /** 上のバーの、検索の文字。 */
  search: string
  /** 上のバーの「作成」ボタンが押された回数。増えたら、作成のダイアログを開く。 */
  createTick: number
}
