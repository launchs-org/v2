// exporter は、ingress のアクセスログを、利用者が設定した送り先(webhook と Google Drive)に、書き出す(dev-docs/log-export.md)。
//
// 同時に1つだけ動く(PostgreSQL の advisory lock でリーダーを決める。他は、リーダーが止まるまで待つ)。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"launchs/exporter/src/config"
	"launchs/exporter/src/runner"
	sharedconfig "launchs/shared/config"
	"launchs/shared/credential"
	"launchs/shared/database"
	"launchs/shared/exportsend"
	"launchs/shared/googledrive"
	"launchs/shared/leader"
	"launchs/shared/logger"
	"launchs/shared/repository"
)

const (
	// leaderLockKey は、リーダー選出の advisory lock の鍵。watcher(7100001)、logcollector(7100002)、accessstats(7100003)とは、別の値にする。
	leaderLockKey int64 = 7100004

	// leaderRetryInterval は、リーダーになれなかったときの、取り直しの間隔。
	leaderRetryInterval = 5 * time.Second

	// leaderHeartbeatInterval は、リーダーが、ロックの接続の生存を確かめる間隔。
	leaderHeartbeatInterval = 5 * time.Second
)

// main は、run を呼んで、失敗したときは、原因を表示して、異常終了する。
func main() {
	// 動かして、失敗したら、標準エラー出力に書いて、終了コード 1 で終わる。
	if runError := run(); runError != nil {
		fmt.Fprintln(os.Stderr, runError)
		os.Exit(1)
	}
}

// run は、設定を読み、リーダーになったら、見回りを始める。停止の合図(SIGINT、SIGTERM)で、安全に止まる。
//
// 戻り値:
//   - 起動できなかったときのエラー。正常に止まったときは nil。
func run() error {
	// 設定とロガーを用意する。
	exporterConfig, configError := config.LoadExporterConfig(sharedconfig.NewProcessEnvironmentReader())
	if configError != nil {
		return fmt.Errorf("設定を読み込めませんでした: %w", configError)
	}
	appLogger := logger.New(os.Stdout, "exporter", exporterConfig.LogLevel)
	// 停止の合図で、終わる context を作る。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// 暗号化の鍵が無ければ、機能は無効(秘密を扱えない)。停止の合図まで待つ。
	if exporterConfig.CredentialKey == "" {
		appLogger.Warn("EXPORT_CREDENTIAL_KEY が空のため、エクスポートは無効です。停止の合図まで待ちます")
		<-ctx.Done()
		return nil
	}
	cipher, cipherError := credential.NewCipher(exporterConfig.CredentialKey)
	if cipherError != nil {
		return fmt.Errorf("EXPORT_CREDENTIAL_KEY が正しくありません: %w", cipherError)
	}
	// データベースに接続する。
	appDatabase, databaseError := database.Open(exporterConfig.DatabaseURL)
	if databaseError != nil {
		return databaseError
	}
	defer func() { _ = database.Close(appDatabase) }()
	exportRepository := repository.NewExportRepository(appDatabase)
	// 送り手を用意する。Drive は、OAuth のクライアントが設定されているときだけ。
	webhookSender := exportsend.NewWebhookSender(cipher, exporterConfig.SafeOptions)
	var driveWriter *exportsend.DriveWriter
	if googleClient := googledrive.NewClient(exporterConfig.Google, nil); googleClient.CanWrite() {
		driveWriter = exportsend.NewDriveWriter(googleClient, exportRepository, cipher, 0)
	} else {
		appLogger.Warn("GOOGLE_OAUTH_CLIENT_ID / GOOGLE_OAUTH_CLIENT_SECRET が空のため、Google Drive への転送は動かしません")
	}
	// リーダーになったものだけが、見回りを動かす。
	elector, electorError := leader.NewElector(appDatabase, leaderLockKey, leaderRetryInterval, leaderHeartbeatInterval, appLogger)
	if electorError != nil {
		return electorError
	}
	appLogger.Info("exporter を起動する(リーダーになるまで待つ)")
	return elector.Run(ctx, func(leaderContext context.Context) error {
		// リーダーでなくなる(または、止める合図)まで、見回る。
		runner.New(exportRepository, webhookSender, driveWriter, exporterConfig.DriveFlushRows, exporterConfig.CheckInterval, appLogger).Run(leaderContext)
		return nil
	})
}
