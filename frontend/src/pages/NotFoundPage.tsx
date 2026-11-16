// 存在しない URL の画面。
import { Link } from "react-router-dom";
import { Brand } from "../components/Brand";
import { Button } from "../components/ui/button";
import { Card, CardContent } from "../components/ui/card";

/** 存在しない URL の画面。 */
export function NotFoundPage() {
  return (
    <main className="grid min-h-screen place-items-center px-4">
      <div className="flex w-full max-w-sm flex-col items-center gap-6">
        <Brand large />
        <Card className="w-full">
          <CardContent className="flex flex-col items-center gap-4 p-8 text-center">
            <h1 className="text-2xl font-normal">ページが見つかりません</h1>
            <Button asChild>
              <Link to="/projects">プロジェクトの一覧へ</Link>
            </Button>
          </CardContent>
        </Card>
      </div>
    </main>
  );
}
