// 画面の切り替え(ナビゲーション)の型。外枠(console-shell)が持ち、各画面に渡す。
import type { Project } from "@/lib/types"

/** 画面の種類。プロジェクトを選ばなくても開ける画面(projects、quota、account)と、プロジェクトの中の画面(canvas、logs、settings)。 */
export type View = "projects" | "quota" | "account" | "canvas" | "logs" | "settings"

/** deploy の詳細のタブ。 */
export type DeployTab = "overview" | "config" | "mounts" | "service" | "logs"

/** キャンバスで選んだカード(右のパネルに、詳細を出す対象)。 */
export type Selection =
  | { kind: "deploy"; id: string; tab?: DeployTab }
  | { kind: "ingress"; id: string }
  | { kind: "volume"; id: string }

/** 画面を移るための操作。 */
export type Nav = {
  /** 画面を切り替える(右のパネルは、閉じる)。 */
  go: (view: View) => void
  /** キャンバスのカードを選んで、右のパネルを開く(キャンバスの画面へ移る)。 */
  select: (selection: Selection) => void
  /** deploy のパネルを、タブを指定して開く(select の、deploy 用の短縮)。 */
  openDeploy: (deployId: string, tab?: DeployTab) => void
  /** 右のパネルを閉じる。 */
  closePanel: () => void
  /** プロジェクトを選んで、キャンバスを開く。 */
  openProject: (project: Project) => void
  /** 最初のデプロイのウィザードを開く。 */
  openWizard: () => void
}

/** プロジェクトの中の画面とパネルが受け取る、共通の引数。 */
export type ProjectViewProps = {
  project: Project
  /** 自動更新の間隔(ミリ秒)。0 なら止める。 */
  refreshInterval: number
  nav: Nav
}
