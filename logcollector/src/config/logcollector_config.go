// Package config は、logcollector の設定を、環境変数から読み込む。
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	sharedconfig "launchs/shared/config"
	"launchs/shared/k8s"
	"launchs/shared/logger"
	"launchs/shared/model"
)

const (
	// defaultLogLevelText は、ログレベルの既定。
	defaultLogLevelText = "info"

	// defaultMaxStreams は、同時に追う Pod の数の上限の既定。
	defaultMaxStreams = 500

	// defaultIngestBytesPerSec は、Pod ごとの書き込みの速さの上限の既定(1MiB/秒)。
	defaultIngestBytesPerSec = 1024 * 1024

	// defaultReconcileInterval は、追う Pod を決め直す間隔の既定。
	defaultReconcileInterval = 2 * time.Second

	// defaultFlushInterval は、まとめて書く間隔の既定。
	defaultFlushInterval = time.Second
)

// LogcollectorConfig は、logcollector の設定。
type LogcollectorConfig struct {
	// DatabaseURL は、app データベースの接続先(環境変数 DATABASE_URL)。パスワードを含むので、ログに出さない。
	DatabaseURL string

	// LogLevel は、出力するログの最低のレベル(環境変数 LOG_LEVEL)。
	LogLevel slog.Level

	// Kubernetes は、k8s への接続の設定(環境変数 KUBERNETES_ACCESS、KUBECONFIG)。
	Kubernetes k8s.Settings

	// Retention は、ログの行を残す期間(環境変数 LOG_RETENTION)。
	Retention time.Duration

	// MaxBytesPerPod は、Pod ごとの、保存するログの大きさの上限(バイト。環境変数 LOG_MAX_BYTES_PER_POD)。
	MaxBytesPerPod int64

	// MaxLineBytes は、1行の長さの上限(バイト。環境変数 LOG_MAX_LINE_BYTES)。
	MaxLineBytes int

	// MaxStreams は、同時に追う Pod の数の上限(環境変数 LOG_MAX_STREAMS)。
	MaxStreams int

	// IngestBytesPerSec は、Pod ごとの書き込みの速さの上限(バイト/秒。環境変数 LOG_INGEST_BYTES_PER_SEC)。
	IngestBytesPerSec int

	// ReconcileInterval は、追う Pod を決め直す間隔(環境変数 LOG_RECONCILE_INTERVAL)。
	ReconcileInterval time.Duration

	// FlushInterval は、まとめて書く間隔(環境変数 LOG_FLUSH_INTERVAL)。
	FlushInterval time.Duration
}

// LoadLogcollectorConfig は、環境変数から、logcollector の設定を読み込む。
//
// 設定に問題があっても止まらず、見つかった全ての問題をまとめて報告する。
//
// 引数:
//   - reader: 環境変数を読む EnvironmentReader。
//
// 戻り値:
//   - 読み込んだ設定。問題があったときは、ゼロ値。
//   - エラー。問題があったとき、全ての問題の説明を含む。
func LoadLogcollectorConfig(reader *sharedconfig.EnvironmentReader) (LogcollectorConfig, error) {
	// 環境変数を読む。
	databaseURL := reader.RequiredString("DATABASE_URL")
	logLevelText := reader.OptionalString("LOG_LEVEL", defaultLogLevelText)
	kubernetesSettings := k8s.ReadSettings(reader)
	retention := reader.OptionalDuration("LOG_RETENTION", model.DefaultLogRetention)
	maxBytesPerPod := reader.OptionalInt("LOG_MAX_BYTES_PER_POD", int(model.DefaultMaxLogBytesPerPod))
	maxLineBytes := reader.OptionalInt("LOG_MAX_LINE_BYTES", model.DefaultMaxLogLineBytes)
	maxStreams := reader.OptionalInt("LOG_MAX_STREAMS", defaultMaxStreams)
	ingestBytesPerSec := reader.OptionalInt("LOG_INGEST_BYTES_PER_SEC", defaultIngestBytesPerSec)
	reconcileInterval := reader.OptionalDuration("LOG_RECONCILE_INTERVAL", defaultReconcileInterval)
	flushInterval := reader.OptionalDuration("LOG_FLUSH_INTERVAL", defaultFlushInterval)
	// ここまでの読み込みで見つかった問題を取り出す。
	loadError := reader.Err()
	// ログレベルを変換する。
	logLevel, levelError := logger.ParseLevel(logLevelText)
	if levelError != nil {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 LOG_LEVEL: %w", levelError))
	}
	// k8s の設定を確かめる。
	if validateError := kubernetesSettings.Validate(); validateError != nil {
		loadError = errors.Join(loadError, validateError)
	}
	// 回数、大きさ、時間は、正の値でなければならない(0 以下は、全てを消す・止めるなど、危険な動きになる)。
	for name, valid := range map[string]bool{
		"LOG_RETENTION": retention > 0, "LOG_MAX_BYTES_PER_POD": maxBytesPerPod > 0, "LOG_MAX_LINE_BYTES": maxLineBytes > 0, "LOG_MAX_STREAMS": maxStreams > 0,
		"LOG_INGEST_BYTES_PER_SEC": ingestBytesPerSec > 0, "LOG_RECONCILE_INTERVAL": reconcileInterval > 0, "LOG_FLUSH_INTERVAL": flushInterval > 0,
	} {
		if !valid {
			loadError = errors.Join(loadError, fmt.Errorf("環境変数 %s は、正の値でなければなりません", name))
		}
	}
	// 1行の長さの上限が、書き込みの速さの上限より大きいと、1行も書けない秒ができるので、拒否する。
	if maxLineBytes > ingestBytesPerSec && maxLineBytes > 0 && ingestBytesPerSec > 0 {
		loadError = errors.Join(loadError, errors.New("環境変数 LOG_MAX_LINE_BYTES は、LOG_INGEST_BYTES_PER_SEC 以下でなければなりません"))
	}
	// 問題があれば、ゼロ値とエラーを返す。
	if loadError != nil {
		return LogcollectorConfig{}, loadError
	}
	return LogcollectorConfig{
		DatabaseURL: databaseURL, LogLevel: logLevel, Kubernetes: kubernetesSettings, Retention: retention, MaxBytesPerPod: int64(maxBytesPerPod),
		MaxLineBytes: maxLineBytes, MaxStreams: maxStreams, IngestBytesPerSec: ingestBytesPerSec, ReconcileInterval: reconcileInterval, FlushInterval: flushInterval,
	}, nil
}
