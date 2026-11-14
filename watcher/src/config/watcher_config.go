// Package config は、watcher の設定を環境変数から読む。
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	sharedconfig "launchs/shared/config"
	"launchs/shared/k8s"
	"launchs/shared/logger"
	"launchs/shared/repository"
)

const (
	// defaultLogLevelText は、LOG_LEVEL が無いときに使うログレベル。
	defaultLogLevelText = "info"

	// defaultTemporalAddress は、TEMPORAL_ADDRESS が無いときに使う Temporal サーバーの接続先。
	defaultTemporalAddress = "temporal:7233"

	// defaultTemporalNamespace は、TEMPORAL_NAMESPACE が無いときに使う Temporal の namespace。
	defaultTemporalNamespace = "default"

	// defaultCrashLoopUnhealthyFor は、Pod が安定して動いていない状態が、これだけ続いたら、deploy を止める(既定)。
	defaultCrashLoopUnhealthyFor = 30 * time.Minute

	// defaultCrashLoopMaxRestarts は、コンテナの再起動がこの回数に達し、不健全が続いたら、deploy を止める(既定)。
	defaultCrashLoopMaxRestarts = 20

	// defaultCrashLoopRestartMinUnhealthy は、再起動の回数で止めるときに、最低限必要な、不健全が続いた時間(既定)。
	defaultCrashLoopRestartMinUnhealthy = 10 * time.Minute
)

// WatcherConfig は、watcher の設定。
type WatcherConfig struct {
	// DatabaseURL は、app データベースの接続先(環境変数 DATABASE_URL)。パスワードを含むので、ログに出さない。
	DatabaseURL string

	// LogLevel は、出力するログの最低のレベル(環境変数 LOG_LEVEL)。
	LogLevel slog.Level

	// Kubernetes は、k8s への接続の設定(環境変数 KUBERNETES_ACCESS、KUBECONFIG)。読み方は shared/k8s にある。
	Kubernetes k8s.Settings

	// TemporalAddress は、Temporal サーバーの接続先(環境変数 TEMPORAL_ADDRESS)。deploy を止める Workflow の開始に使う。
	TemporalAddress string

	// TemporalNamespace は、使う Temporal の namespace(環境変数 TEMPORAL_NAMESPACE)。
	TemporalNamespace string

	// CrashLoop は、異常な状態が続く deploy を止める判断の上限(環境変数 CRASH_LOOP_...)。
	CrashLoop repository.CrashLoopLimits
}

// LoadWatcherConfig は、環境変数から watcher の設定を読む。
//
// 読む環境変数:
//   - DATABASE_URL: app データベースの接続先(必須)。
//   - KUBERNETES_ACCESS、KUBECONFIG: k8s への接続の設定(読み方と意味は shared/k8s の ReadSettings)。
//   - TEMPORAL_ADDRESS、TEMPORAL_NAMESPACE: Temporal の接続先。省略すると temporal:7233、default。
//   - CRASH_LOOP_UNHEALTHY_FOR: 安定して動いていない状態が、これだけ続いたら止める(既定 30m)。
//   - CRASH_LOOP_MAX_RESTARTS: 再起動がこの回数に達し、不健全が続いたら止める(既定 20)。
//   - CRASH_LOOP_RESTART_MIN_UNHEALTHY: 再起動の回数で止めるときの、最低限の不健全の長さ(既定 10m)。
//   - LOG_LEVEL: ログレベル(debug、info、warn、error)。省略すると info。
//
// 設定に問題があっても止まらず、見つかった全ての問題をまとめて報告する。
//
// 引数:
//   - reader: 環境変数を読む EnvironmentReader。
//
// 戻り値:
//   - 読み込んだ設定。問題があったときは、ゼロ値。
//   - 問題があったときのエラー。
func LoadWatcherConfig(reader *sharedconfig.EnvironmentReader) (WatcherConfig, error) {
	// 各環境変数を読む。
	databaseURL := reader.RequiredString("DATABASE_URL")
	logLevelText := reader.OptionalString("LOG_LEVEL", defaultLogLevelText)
	kubernetesSettings := k8s.ReadSettings(reader)
	temporalAddress := reader.OptionalString("TEMPORAL_ADDRESS", defaultTemporalAddress)
	temporalNamespace := reader.OptionalString("TEMPORAL_NAMESPACE", defaultTemporalNamespace)
	crashLoopLimits := repository.CrashLoopLimits{
		UnhealthyFor:        reader.OptionalDuration("CRASH_LOOP_UNHEALTHY_FOR", defaultCrashLoopUnhealthyFor),
		MaxRestarts:         reader.OptionalInt("CRASH_LOOP_MAX_RESTARTS", defaultCrashLoopMaxRestarts),
		RestartMinUnhealthy: reader.OptionalDuration("CRASH_LOOP_RESTART_MIN_UNHEALTHY", defaultCrashLoopRestartMinUnhealthy),
	}

	// 読み込み中に見つかった問題を集める。
	loadError := reader.Err()

	// ログレベルの文字列を変換し、未知の値なら問題に加える。
	logLevel, levelError := logger.ParseLevel(logLevelText)
	if levelError != nil {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 LOG_LEVEL: %w", levelError))
	}
	// k8s への接続の設定の値を検査する。
	if validateError := kubernetesSettings.Validate(); validateError != nil {
		loadError = errors.Join(loadError, validateError)
	}
	// 上限は、0 以下だと、全ての deploy を止めてしまうので、拒否する。
	if crashLoopLimits.UnhealthyFor <= 0 || crashLoopLimits.MaxRestarts < 1 || crashLoopLimits.RestartMinUnhealthy <= 0 {
		loadError = errors.Join(loadError, errors.New("環境変数 CRASH_LOOP_UNHEALTHY_FOR、CRASH_LOOP_MAX_RESTARTS、CRASH_LOOP_RESTART_MIN_UNHEALTHY は、正の値でなければなりません"))
	}
	// 問題があったときは、まとめて返す。
	if loadError != nil {
		return WatcherConfig{}, loadError
	}
	return WatcherConfig{
		DatabaseURL: databaseURL, LogLevel: logLevel, Kubernetes: kubernetesSettings,
		TemporalAddress: temporalAddress, TemporalNamespace: temporalNamespace, CrashLoop: crashLoopLimits,
	}, nil
}
