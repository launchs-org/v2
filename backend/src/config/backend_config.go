// Package config は、バックエンドの設定を環境変数から読む。
//
// 環境変数を読む部品は shared/config にあり、このパッケージは、
// バックエンドが必要とする環境変数の名前と、既定値を決める。
package config

import (
	"errors"
	"fmt"
	"log/slog"

	sharedconfig "launchs/shared/config"
	"launchs/shared/logger"
)

const (
	// defaultHTTPPort は、HTTP_PORT が設定されていないときに使うポート番号。
	defaultHTTPPort = 8080

	// defaultLogLevelText は、LOG_LEVEL が設定されていないときに使うログレベル。
	defaultLogLevelText = "info"

	// minimumPort と maximumPort は、ポート番号として使える範囲。
	minimumPort = 1
	maximumPort = 65535
)

// BackendConfig は、バックエンドの設定。
type BackendConfig struct {
	// HTTPPort は、API サーバーが待ち受けるポート番号(環境変数 HTTP_PORT)。
	HTTPPort int

	// LogLevel は、出力するログの最低のレベル(環境変数 LOG_LEVEL)。
	LogLevel slog.Level

	// DatabaseURL は、app データベースの接続先(環境変数 DATABASE_URL)。パスワードを含むので、ログに出さない。
	DatabaseURL string

	// DevUserID は、認証のコンテナができるまで、全てのリクエストのユーザーとして扱う ID(環境変数 DEV_USER_ID)。
	// 省略すると空になり、全ての API が 401 になる。認証のコンテナを作るときに、この設定は無くす。
	DevUserID string
}

// LoadBackendConfig は、環境変数からバックエンドの設定を読む。
//
// 読む環境変数:
//   - DATABASE_URL: app データベースの接続先(必須)。
//   - DEV_USER_ID: 開発用の仮の認証で使うユーザー ID。省略すると、全ての API が 401 になる。
//   - HTTP_PORT: 待ち受けるポート番号(1〜65535)。省略すると 8080。
//   - LOG_LEVEL: ログレベル(debug、info、warn、error)。省略すると info。
//
// 設定に問題があっても止まらず、見つかった全ての問題をまとめて報告する。
//
// 引数:
//   - reader: 環境変数を読む EnvironmentReader。
//
// 戻り値:
//   - 読み込んだ設定。問題があったときは、ゼロ値。
//   - エラー。問題があったとき、全ての問題の説明を含む。
func LoadBackendConfig(reader *sharedconfig.EnvironmentReader) (BackendConfig, error) {
	// 必須の環境変数を読む。無いときは、reader が問題として覚えておく(ここでは止まらない)。
	databaseURL := reader.RequiredString("DATABASE_URL")
	// 開発用のユーザー ID を読む。省略は許すので、既定値は空にする。
	devUserID := reader.OptionalString("DEV_USER_ID", "")
	// ポート番号を整数で読む。整数でないときは、reader が問題として覚えておく。
	httpPort := reader.OptionalInt("HTTP_PORT", defaultHTTPPort)
	// ログレベルを文字列のまま読む。意味の確認は、あとで ParseLevel で行う。
	logLevelText := reader.OptionalString("LOG_LEVEL", defaultLogLevelText)

	// ここまでの読み込みで見つかった問題を取り出し、以降の確認の問題と、まとめて報告できるようにする。
	loadError := reader.Err()

	// ポート番号が使える範囲の外のときは、問題として加える。
	if httpPort < minimumPort || httpPort > maximumPort {
		// 範囲と、実際の値を含むエラーを作る。
		portError := fmt.Errorf("環境変数 HTTP_PORT は %d〜%d でなければならない: %d", minimumPort, maximumPort, httpPort)
		// 見つかった問題を、これまでの問題にまとめる。
		loadError = errors.Join(loadError, portError)
	}

	// ログレベルの文字列を、slog のレベルに変換する。
	logLevel, levelError := logger.ParseLevel(logLevelText)
	// 未知のレベルのときは、どの環境変数の問題か分かるようにして、問題に加える。
	if levelError != nil {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 LOG_LEVEL: %w", levelError))
	}

	// 1つでも問題があれば、中途半端な設定を使わせないため、ゼロ値とエラーを返す。
	if loadError != nil {
		return BackendConfig{}, loadError
	}
	// 問題が無いので、読み込んだ値で設定を作って返す。
	return BackendConfig{HTTPPort: httpPort, LogLevel: logLevel, DatabaseURL: databaseURL, DevUserID: devUserID}, nil
}
