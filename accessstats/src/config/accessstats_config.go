// Package config は、accessstats の設定を、環境変数から読み込む。
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	sharedconfig "launchs/shared/config"
	"launchs/shared/k8s"
	"launchs/shared/logger"
)

const (
	// defaultTraefikNamespace は、Traefik の Pod がいる namespace の既定。
	defaultTraefikNamespace = "traefik"

	// defaultTraefikSelector は、Traefik の Pod を選ぶラベルの既定(Helm の chart の標準)。
	defaultTraefikSelector = "app.kubernetes.io/name=traefik"

	// defaultMinuteRetention と defaultHourRetention は、1 分ごと・1 時間ごとの集計を残す期間の既定。
	defaultMinuteRetention = 24 * time.Hour
	defaultHourRetention   = 30 * 24 * time.Hour

	// defaultReconcileInterval は、追う Pod を決め直す間隔の既定。
	defaultReconcileInterval = 10 * time.Second

	// defaultLogRetention と defaultLogMaxRows は、アクセスログを残す期間と、ingress ごとの行数の上限の既定。
	defaultLogRetention = 24 * time.Hour
	defaultLogMaxRows   = 5000
)

// AccessStatsConfig は、accessstats の設定。
type AccessStatsConfig struct {
	// DatabaseURL は、app データベースの接続先(環境変数 DATABASE_URL)。パスワードを含むので、ログに出さない。
	DatabaseURL string

	// LogLevel は、出力するログの最低のレベル(環境変数 LOG_LEVEL)。
	LogLevel slog.Level

	// Kubernetes は、k8s への接続の設定。
	Kubernetes k8s.Settings

	// TraefikNamespace は、Traefik の Pod がいる namespace(環境変数 ACCESS_STATS_TRAEFIK_NAMESPACE)。
	TraefikNamespace string

	// TraefikSelector は、Traefik の Pod を選ぶラベル(環境変数 ACCESS_STATS_TRAEFIK_SELECTOR)。
	TraefikSelector string

	// KeepClients は、クライアントの IP を保存するか(環境変数 ACCESS_STATS_KEEP_CLIENTS。既定 true)。
	KeepClients bool

	// MinuteRetention と HourRetention は、集計を残す期間(環境変数 ACCESS_STATS_MINUTE_RETENTION、ACCESS_STATS_HOUR_RETENTION)。
	MinuteRetention time.Duration
	HourRetention   time.Duration

	// ReconcileInterval は、追う Pod を決め直す間隔(環境変数 ACCESS_STATS_RECONCILE_INTERVAL)。
	ReconcileInterval time.Duration

	// LogRetention は、アクセスログを残す期間(環境変数 ACCESS_LOG_RETENTION。既定 24 時間)。
	LogRetention time.Duration

	// LogMaxRows は、ingress ごとに残すアクセスログの最大の行数(環境変数 ACCESS_LOG_MAX_ROWS。既定 5000)。
	LogMaxRows int
}

// LoadAccessStatsConfig は、環境変数から、accessstats の設定を読み込む。
//
// 設定に問題があっても止まらず、見つかった全ての問題をまとめて報告する。
//
// 引数:
//   - reader: 環境変数を読む EnvironmentReader。
//
// 戻り値:
//   - 読み込んだ設定。問題があったときは、ゼロ値。
//   - エラー。問題があったとき、全ての問題の説明を含む。
func LoadAccessStatsConfig(reader *sharedconfig.EnvironmentReader) (AccessStatsConfig, error) {
	// 環境変数を読む。
	databaseURL := reader.RequiredString("DATABASE_URL")
	logLevelText := reader.OptionalString("LOG_LEVEL", "info")
	kubernetesSettings := k8s.ReadSettings(reader)
	traefikNamespace := reader.OptionalString("ACCESS_STATS_TRAEFIK_NAMESPACE", defaultTraefikNamespace)
	traefikSelector := reader.OptionalString("ACCESS_STATS_TRAEFIK_SELECTOR", defaultTraefikSelector)
	keepClientsText := strings.ToLower(reader.OptionalString("ACCESS_STATS_KEEP_CLIENTS", "true"))
	minuteRetention := reader.OptionalDuration("ACCESS_STATS_MINUTE_RETENTION", defaultMinuteRetention)
	hourRetention := reader.OptionalDuration("ACCESS_STATS_HOUR_RETENTION", defaultHourRetention)
	reconcileInterval := reader.OptionalDuration("ACCESS_STATS_RECONCILE_INTERVAL", defaultReconcileInterval)
	logRetention := reader.OptionalDuration("ACCESS_LOG_RETENTION", defaultLogRetention)
	logMaxRows := reader.OptionalInt("ACCESS_LOG_MAX_ROWS", defaultLogMaxRows)
	loadError := reader.Err()
	// ログレベルと k8s の設定を確かめる。
	logLevel, levelError := logger.ParseLevel(logLevelText)
	if levelError != nil {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 LOG_LEVEL: %w", levelError))
	}
	if validateError := kubernetesSettings.Validate(); validateError != nil {
		loadError = errors.Join(loadError, validateError)
	}
	// 真偽の値は、true か false だけ。
	if keepClientsText != "true" && keepClientsText != "false" {
		loadError = errors.Join(loadError, errors.New("環境変数 ACCESS_STATS_KEEP_CLIENTS は、true か false にしてください"))
	}
	// 期間は、正の値でなければならない(0 以下は、全てを消す・止めるなど、危険な動きになる)。
	for name, valid := range map[string]bool{
		"ACCESS_STATS_MINUTE_RETENTION": minuteRetention > 0, "ACCESS_STATS_HOUR_RETENTION": hourRetention > 0, "ACCESS_STATS_RECONCILE_INTERVAL": reconcileInterval > 0, "ACCESS_LOG_RETENTION": logRetention > 0, "ACCESS_LOG_MAX_ROWS": logMaxRows > 0,
	} {
		if !valid {
			loadError = errors.Join(loadError, fmt.Errorf("環境変数 %s は、正の値でなければなりません", name))
		}
	}
	if loadError != nil {
		return AccessStatsConfig{}, loadError
	}
	return AccessStatsConfig{
		DatabaseURL: databaseURL, LogLevel: logLevel, Kubernetes: kubernetesSettings, TraefikNamespace: traefikNamespace, TraefikSelector: traefikSelector,
		KeepClients: keepClientsText == "true", MinuteRetention: minuteRetention, HourRetention: hourRetention, ReconcileInterval: reconcileInterval,
		LogRetention: logRetention, LogMaxRows: logMaxRows,
	}, nil
}
