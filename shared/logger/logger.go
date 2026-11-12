// Package logger は、全てのコンテナで共通の、構造化ログ(JSON)のロガーを作る。
//
// 標準の log/slog を使い、標準出力に1行1件の JSON で書く。
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// ParseLevel は、ログレベルの文字列を slog.Level に変換する。
//
// 引数:
//   - text: "debug"、"info"、"warn"、"error" のどれか。大文字小文字は区別しない。
//     空文字(環境変数が未設定のとき)は "info" として扱う。
//
// 戻り値:
//   - 変換したログレベル。
//   - エラー。未知の文字列のとき。その場合のログレベルは info。
func ParseLevel(text string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("未知のログレベル %q(debug、info、warn、error のどれか)", text)
	}
}

// New は、JSON で出力するロガーを作る。
//
// 作ったロガーは、全ての行に、どのコンテナのログかを表す component を付ける。
// 呼び出し側は、With を使って request_id や user_id などのフィールドを足して使う。
//
// 引数:
//   - output: 書き込み先。通常は os.Stdout。
//   - component: コンテナの名前(例: "backend"、"worker")。全ての行の component になる。
//   - level: 出力する最低のレベル。これより低いレベルは出力しない。
//
// 戻り値:
//   - 作成したロガー。
func New(output io.Writer, component string, level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level})
	return slog.New(handler).With("component", component)
}
