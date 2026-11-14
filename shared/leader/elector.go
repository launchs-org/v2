// Package leader は、PostgreSQL の advisory lock を使って、複数のコンテナのうち、1つだけに仕事をさせる。
//
// watcher のように、同時に1つだけが動くべきものを、2つ以上起動しても、1つだけがリーダーになる。
// リーダーが止まる(プロセスの終了、接続の切断)と、ロックが自動で外れ、待っていた別のものが引き継ぐ。
package leader

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"gorm.io/gorm"
)

// Elector は、advisory lock を取り合って、リーダーになったものだけに、仕事をさせる。
type Elector struct {
	sqlDatabase       *sql.DB
	lockKey           int64
	retryInterval     time.Duration
	heartbeatInterval time.Duration
	appLogger         *slog.Logger
}

// NewElector は、Elector を作る。
//
// 引数:
//   - database: 使うデータベースの接続(PostgreSQL)。
//   - lockKey: 取り合うロックの番号。同じ番号を使うもの同士の中で、リーダーが1つ決まる。
//   - retryInterval: リーダーになれなかったとき(または仕事が終わったとき)に、次にロックを試すまでの待ち時間。
//   - heartbeatInterval: リーダーの間、接続が生きているかを確かめる間隔。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した Elector。
//   - データベースの接続を取り出せなかったときのエラー。
func NewElector(database *gorm.DB, lockKey int64, retryInterval time.Duration, heartbeatInterval time.Duration, appLogger *slog.Logger) (*Elector, error) {
	// gorm の接続から、標準の接続のプールを取り出す。
	sqlDatabase, sqlError := database.DB()
	if sqlError != nil {
		return nil, fmt.Errorf("データベースの接続を取り出せませんでした: %w", sqlError)
	}
	// 設定を持たせて返す。
	return &Elector{
		sqlDatabase: sqlDatabase, lockKey: lockKey, retryInterval: retryInterval,
		heartbeatInterval: heartbeatInterval, appLogger: appLogger,
	}, nil
}

// Run は、リーダーになれるまでロックを試し、リーダーの間だけ leaderWork を実行する。
//
// 処理の流れ:
//  1. 専用の接続で、ロックを試す(取れなければ、retryInterval 待って、やり直す)
//  2. 取れたら、leaderWork を実行する。渡す context は、リーダーでなくなったとき(接続が切れたとき)に、キャンセルされる
//  3. leaderWork が終わったら、ロックを手放す。ctx が生きていれば、1 に戻る(エラーで終わったときも、再び試す)
//
// 引数:
//   - ctx: 止めるための context。キャンセルされると、リーダーの仕事も止めて、戻る。
//   - leaderWork: リーダーの間に行う仕事。渡された context がキャンセルされたら、止まること。
//
// 戻り値:
//   - ctx がキャンセルされて止まったときは nil(Run は、それ以外では戻らない)。
func (elector *Elector) Run(ctx context.Context, leaderWork func(leaderContext context.Context) error) error {
	// ctx が生きている間、リーダーになることを試し続ける。
	for ctx.Err() == nil {
		// 1回分のリーダーの期間を実行する。
		elector.runOnce(ctx, leaderWork)
		// 次を試すまで待つ(止める合図が来たら、すぐに終わる)。
		select {
		case <-ctx.Done():
		case <-time.After(elector.retryInterval):
		}
	}
	return nil
}

// runOnce は、ロックを1回試し、取れたら、手放すまでリーダーの仕事をする。
//
// 引数:
//   - ctx: 止めるための context。
//   - leaderWork: リーダーの間に行う仕事。
func (elector *Elector) runOnce(ctx context.Context, leaderWork func(leaderContext context.Context) error) {
	// ロックを持つ専用の接続を借りる。ロックは接続につくので、仕事の間、この接続を持ち続ける。
	connection, connectionError := elector.sqlDatabase.Conn(ctx)
	if connectionError != nil {
		elector.appLogger.Warn("リーダー選出の接続を開けませんでした", "error", connectionError)
		return
	}
	// 終わるとき、ロックを手放して、接続を返す。ロックは、接続が閉じても外れる。
	defer func() {
		_, _ = connection.ExecContext(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", elector.lockKey)
		_ = connection.Close()
	}()

	// ロックを試す(待たずに、取れたかどうかだけを返す)。
	var acquired bool
	if scanError := connection.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", elector.lockKey).Scan(&acquired); scanError != nil {
		elector.appLogger.Warn("リーダー選出のロックを試せませんでした", "error", scanError)
		return
	}
	// 取れなかったときは、別のものがリーダーなので、何もしない。
	if !acquired {
		return
	}
	elector.appLogger.Info("リーダーになった", "lock_key", elector.lockKey)

	// リーダーの間、接続が生きているかを確かめ、切れたら仕事を止める。
	leaderContext, cancelLeader := context.WithCancel(ctx)
	var heartbeat sync.WaitGroup
	heartbeat.Add(1)
	go func() {
		defer heartbeat.Done()
		elector.watchConnection(leaderContext, cancelLeader, connection)
	}()

	// 仕事を実行する。エラーで終わっても、リーダーでなくなって、再び試す。
	if workError := leaderWork(leaderContext); workError != nil {
		elector.appLogger.Error("リーダーの仕事がエラーで終わりました", "error", workError)
	}
	// 仕事が終わったので、確認の処理も止めて、終わるのを待つ。
	cancelLeader()
	heartbeat.Wait()
	elector.appLogger.Info("リーダーをやめた", "lock_key", elector.lockKey)
}

// watchConnection は、ロックを持つ接続が生きているかを、一定の間隔で確かめる。
// 確かめに失敗したら、ロックを失った可能性があるので、リーダーの仕事を止める。
//
// 引数:
//   - leaderContext: リーダーの期間の context。キャンセルされたら、確認をやめる。
//   - cancelLeader: リーダーの仕事を止める関数。
//   - connection: ロックを持つ接続。
func (elector *Elector) watchConnection(leaderContext context.Context, cancelLeader context.CancelFunc, connection *sql.Conn) {
	// 一定の間隔で確かめる。
	ticker := time.NewTicker(elector.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-leaderContext.Done():
			return
		case <-ticker.C:
			// 同じ接続で、簡単な問い合わせをする。失敗したら、接続が切れている。
			var one int
			if scanError := connection.QueryRowContext(leaderContext, "SELECT 1").Scan(&one); scanError != nil && leaderContext.Err() == nil {
				elector.appLogger.Warn("リーダーの接続が切れたので、リーダーをやめる", "error", scanError)
				cancelLeader()
				return
			}
		}
	}
}
