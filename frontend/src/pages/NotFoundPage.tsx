// 存在しない URL の画面。
import { Link } from "react-router-dom";

/** 存在しない URL の画面。 */
export function NotFoundPage() {
  return (
    <main className="auth-page">
      <div className="card">
        <h1>ページが見つかりません</h1>
        <p><Link to="/projects">プロジェクトの一覧へ</Link></p>
      </div>
    </main>
  );
}
