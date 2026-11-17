// Package server は、バックエンドの Echo のサーバーを組み立てる。
//
// ミドルウェアとエラーハンドラー(shared/httpmiddleware と、backend の middlewares パッケージ)を登録し、ルーティングを登録する。
package server

import (
	"launchs/backend/src/template"
	"log/slog"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"gorm.io/gorm"

	"launchs/backend/src/service"
	"launchs/shared/httpmiddleware"
)

// Dependencies は、サーバーが外から受け取る、依存するものの一式。
type Dependencies struct {
	// Database は、app データベースの接続。
	Database *gorm.DB

	// Authentication は、/api/v1 に付ける認証のミドルウェア(access token の検証、または開発用の仮の認証)。
	Authentication echo.MiddlewareFunc

	// DeployWorkflowStarter は、deploy の起動(k8s への作成)の Workflow を開始するもの。
	DeployWorkflowStarter service.DeployWorkflowStarter

	// ServiceWorkflowStarter は、service の作成・削除の Workflow を開始するもの。
	ServiceWorkflowStarter service.ServiceWorkflowStarter

	// IngressWorkflowStarter は、ingress のルールの反映・削除の Workflow を開始するもの。
	IngressWorkflowStarter service.IngressWorkflowStarter

	// IngressHostGenerator は、ingress に払い出すホスト名を作るもの。
	IngressHostGenerator service.IngressHostGenerator

	// VolumeWorkflowStarter は、volume の拡張の反映・削除の Workflow を開始するもの。
	VolumeWorkflowStarter service.VolumeWorkflowStarter

	// TemplateRegistry は、読み込み済みのテンプレート(docs/templates.md)。nil のときは、同梱のものを読み込む(テスト用)。
	TemplateRegistry *template.Registry

	// VolumeMaxSizeMB は、volume の大きさの上限(MB)。0 のときは、既定(5120)を使う。
	VolumeMaxSizeMB int

	// ProjectDeletionStarter は、プロジェクトの実体の削除(Workflow)を開始するもの。
	ProjectDeletionStarter service.ProjectDeletionStarter
}

// New は、バックエンドの Echo のサーバーを組み立てる。
//
// ミドルウェアは、外側から次の順に登録する。
//  1. リクエスト ID(ヘッダ X-Request-Id が無ければ作り、レスポンスにも付ける)
//  2. アクセスログ(リクエスト ID を出すため、1 のあと)
//  3. パニックからの復帰(ハンドラーがパニックしても、500 を返してサーバーを止めない)
//
// エラーは、共通ルールの形式のレスポンスに変換する。
//
// 引数:
//   - appLogger: アクセスログ、エラーの記録、Echo 自身のログに使うロガー。
//   - dependencies: データベースなど、ルートが使うもの。
//
// 戻り値:
//   - 組み立てた Echo のサーバー。起動は呼び出し側が行う。
func New(appLogger *slog.Logger, dependencies Dependencies) *echo.Echo {
	// ロガーと、エラーを共通の形式に変換するハンドラーを設定して、サーバーを作る。
	echoServer := echo.NewWithConfig(echo.Config{
		Logger:           appLogger,
		HTTPErrorHandler: httpmiddleware.NewErrorHandler(appLogger),
	})

	// 一番外側で、リクエスト ID を付ける。
	echoServer.Use(middleware.RequestID())
	// リクエスト ID を出すため、その内側にアクセスログを置く。
	echoServer.Use(httpmiddleware.NewAccessLogMiddleware(appLogger))
	// ハンドラーのパニックを 500 に変えるため、一番内側に置く。
	echoServer.Use(middleware.Recover())

	// ルートを登録する。
	registerRoutes(echoServer, dependencies)
	// 組み立てたサーバーを返す。
	return echoServer
}
