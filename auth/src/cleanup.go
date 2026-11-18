package main

import (
	"context"
	"log/slog"
	"time"

	"launchs/auth/src/config"
	"launchs/auth/src/repository"
)

// runCleanup は、期限切れの refresh token、古いレート制限の記録、古いログイン失敗の記録、古いログイン履歴を、定期的に消す。
//
// 起動時に1回、そのあとは、cleanupInterval ごとに行う。context が終わったら、止まる。
//
// 引数:
//   - cleanupContext: 止める合図の context。
//   - appLogger: ログの出力先。
//   - refreshTokenRepository: refresh token の保存先。
//   - loginFailureRepository: ログイン失敗の保存先。
//   - rateLimitRepository: レート制限の保存先。
//   - loginEventRepository: ログイン履歴の保存先。
//   - authConfig: 設定(記録を残す時間を、時間枠から決める)。
func runCleanup(cleanupContext context.Context, appLogger *slog.Logger, refreshTokenRepository *repository.RefreshTokenRepository, loginFailureRepository *repository.LoginFailureRepository, rateLimitRepository *repository.RateLimitRepository, loginEventRepository *repository.LoginEventRepository, authConfig config.AuthConfig) {
	// レート制限の記録は、最も長い時間枠の2倍だけ残す。
	rateLimitRetention := 2 * max(authConfig.LoginIPWindow, authConfig.SignupIPWindow)
	// 定期的に実行する。
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		// 3つの掃除をする(失敗は、次の回に任せて、警告だけ残す)。
		if _, cleanError := refreshTokenRepository.DeleteExpired(cleanupContext); cleanError != nil {
			appLogger.Warn("期限切れの refresh token を消せなかった", "error", cleanError)
		}
		if _, cleanError := rateLimitRepository.DeleteOlderThan(cleanupContext, rateLimitRetention); cleanError != nil {
			appLogger.Warn("古いレート制限の記録を消せなかった", "error", cleanError)
		}
		if _, cleanError := loginFailureRepository.DeleteStale(cleanupContext, 24*time.Hour); cleanError != nil {
			appLogger.Warn("古いログイン失敗の記録を消せなかった", "error", cleanError)
		}
		if _, cleanError := loginEventRepository.DeleteOlderThan(cleanupContext, authConfig.LoginHistoryRetention); cleanError != nil {
			appLogger.Warn("古いログイン履歴を消せなかった", "error", cleanError)
		}
		// 次の回まで待つ(止める合図が来たら、終わる)。
		select {
		case <-cleanupContext.Done():
			return
		case <-ticker.C:
		}
	}
}
