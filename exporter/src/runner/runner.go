// Package runner は、exporter の見回り(送る時刻になった転送を、順に動かす)を行う。
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"launchs/shared/apperror"
	"launchs/shared/exportlog"
	"launchs/shared/exportsend"
	"launchs/shared/googledrive"
	"launchs/shared/model"
	"launchs/shared/repository"
)

const (
	// webhookBatchSize は、webhook 1 回に送る行数の上限。
	webhookBatchSize = 1000

	// webhookBatchesPerRound は、1 回の見回りで、1 つの webhook に続けて送る回数の上限(多いときは、次の見回りに回す)。
	webhookBatchesPerRound = 5

	// driveBatchSize と driveBatchesPerRound は、Drive へ書く、1 回あたりの行数と、1 回の見回りで続ける回数。
	driveBatchSize       = 5000
	driveBatchesPerRound = 4

	// destinationTimeout は、1 つの転送を動かす時間の上限。
	destinationTimeout = 2 * time.Minute

	// parallelism は、同時に動かす転送の数の上限(転送どうしは、並行して動かす。1 つの転送の中は、順番)。
	parallelism = 4
)

// Store は、runner が使う、記録の読み書き。repository.ExportRepository が実装する。
type Store interface {
	ListRunnable(requestContext context.Context, now time.Time) ([]model.ExportDestination, error)
	ListDriveAwaitingSlot(requestContext context.Context, now time.Time) ([]model.ExportDestination, error)
	CountPendingLogs(requestContext context.Context, ingressID string, afterID int64, limit int) (int, error)
	ReadLogs(requestContext context.Context, ingressID string, afterID int64, limit int) ([]repository.ExportLogRow, error)
	SaveProgress(requestContext context.Context, destination *model.ExportDestination) error
	FindConnection(requestContext context.Context, userID string) (model.GoogleDriveConnection, error)
	MarkConnectionReauthRequired(requestContext context.Context, userID string, reason string) error
}

// Runner は、見回りを行う。
type Runner struct {
	// store は、記録の読み書き先。
	store Store

	// webhook は、webhook の送り手。
	webhook *exportsend.WebhookSender

	// drive は、Drive の書き手。nil なら、Drive の転送は動かさない(OAuth のクライアントが無いとき)。
	drive *exportsend.DriveWriter

	// flushRows は、Drive へ前倒しで書き出す、たまった行数。
	flushRows int

	// interval は、見回りの間隔。
	interval time.Duration

	// logger は、ログの出力先。
	logger *slog.Logger

	// now は、今の時刻を返す(試験で差し替える)。
	now func() time.Time
}

// New は、Runner を作る。
//
// 引数:
//   - store: 記録の読み書き先。
//   - webhook: webhook の送り手。
//   - drive: Drive の書き手(nil なら、Drive は動かさない)。
//   - flushRows: Drive へ前倒しで書き出す、たまった行数。
//   - interval: 見回りの間隔。
//   - logger: ログの出力先。
//
// 戻り値:
//   - 作った Runner。
func New(store Store, webhook *exportsend.WebhookSender, drive *exportsend.DriveWriter, flushRows int, interval time.Duration, logger *slog.Logger) *Runner {
	// 渡されたものを保持する。時刻は、本物。
	return &Runner{store: store, webhook: webhook, drive: drive, flushRows: flushRows, interval: interval, logger: logger, now: time.Now}
}

// SetNow は、今の時刻を返す関数を差し替える(試験用)。
//
// 引数:
//   - now: 差し替える関数。
func (runner *Runner) SetNow(now func() time.Time) {
	// そのまま差し替える。
	runner.now = now
}

// Run は、停止の合図まで、間隔ごとに見回る。
//
// 引数:
//   - runContext: 止める合図を伝える context。
func (runner *Runner) Run(runContext context.Context) {
	// すぐに 1 回行い、そのあとは、間隔ごとに行う。
	runner.RunOnce(runContext)
	ticker := time.NewTicker(runner.interval)
	defer ticker.Stop()
	for {
		select {
		case <-runContext.Done():
			return
		case <-ticker.C:
			runner.RunOnce(runContext)
		}
	}
}

// RunOnce は、見回りを 1 回行う。動かす時刻になった転送と、行がたまりすぎた Drive の転送を、動かす。
//
// 引数:
//   - runContext: 止める合図を伝える context。
func (runner *Runner) RunOnce(runContext context.Context) {
	// 動かす時刻になった転送を取る。
	now := runner.now()
	due, listError := runner.store.ListRunnable(runContext, now)
	if listError != nil {
		runner.logger.Error("転送の一覧を取得できませんでした", "error", listError)
		return
	}
	// Drive は、自分の枠の時刻を待っていても、行がたまりすぎたら、前倒しで動かす(ログの保存の上限で、消える前に)。
	if runner.drive != nil {
		due = append(due, runner.driveOverflowing(runContext, now, due)...)
	}
	// 転送どうしは、並行して動かす(同時に、parallelism 個まで)。
	var group sync.WaitGroup
	slots := make(chan struct{}, parallelism)
	for index := range due {
		destination := due[index]
		slots <- struct{}{}
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			runner.process(runContext, destination, now)
		}()
	}
	group.Wait()
}

// driveOverflowing は、自分の枠の時刻になっていない Drive の転送のうち、行がたまりすぎたものを返す。
//
// 引数:
//   - runContext: 止める合図を伝える context。
//   - now: 今の時刻。
//   - alreadyDue: すでに動かすことにした転送(重ねて入れない)。
//
// 戻り値:
//   - 前倒しで動かす転送。
func (runner *Runner) driveOverflowing(runContext context.Context, now time.Time, alreadyDue []model.ExportDestination) []model.ExportDestination {
	// 枠を待っている Drive の転送を取る。
	waiting, listError := runner.store.ListDriveAwaitingSlot(runContext, now)
	if listError != nil {
		runner.logger.Error("Drive の転送の一覧を取得できませんでした", "error", listError)
		return nil
	}
	already := map[string]bool{}
	for _, destination := range alreadyDue {
		already[destination.ID] = true
	}
	// たまった行が、前倒しの数に届いたものを選ぶ。
	var overflowing []model.ExportDestination
	for _, destination := range waiting {
		if already[destination.ID] {
			continue
		}
		pending, countError := runner.store.CountPendingLogs(runContext, destination.IngressID, destination.LastCursor, runner.flushRows)
		if countError != nil {
			runner.logger.Error("たまった行を数えられませんでした", "error", countError, "destination", destination.ID)
			continue
		}
		if pending >= runner.flushRows {
			overflowing = append(overflowing, destination)
		}
	}
	return overflowing
}

// process は、1 つの転送を動かす。panic しても、他の転送を止めない。
//
// 引数:
//   - runContext: 止める合図を伝える context。
//   - destination: 動かす転送。
//   - now: 今の時刻。
func (runner *Runner) process(runContext context.Context, destination model.ExportDestination, now time.Time) {
	// 1 つの転送の時間に、上限を付ける。panic は、ここで受けて、ログに残す。
	destinationContext, cancel := context.WithTimeout(runContext, destinationTimeout)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			runner.logger.Error("転送の処理で panic しました", "destination", destination.ID, "panic", fmt.Sprint(recovered))
		}
	}()
	var processError error
	switch destination.Kind {
	case model.ExportKindWebhook:
		processError = runner.processWebhook(destinationContext, &destination, now)
	case model.ExportKindGoogleDrive:
		processError = runner.processDrive(destinationContext, &destination, now)
	default:
		processError = errors.New("知らない種類の転送です")
	}
	if processError != nil {
		runner.logger.Warn("転送に失敗しました", "destination", destination.ID, "kind", destination.Kind, "reason", processError.Error())
	}
}

// fail は、失敗を記録する(失敗の回数と、次に動く時刻が決まる)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - destination: 失敗した転送。
//   - now: 今の時刻。
//   - reason: 利用者に見せる理由。
//
// 戻り値:
//   - 失敗の理由のエラー(呼び出し側が、ログに出す)。
func (runner *Runner) fail(requestContext context.Context, destination *model.ExportDestination, now time.Time, reason string) error {
	// 状態を更新して、保存する。保存に失敗しても、理由は、返す。
	destination.RecordFailure(now, reason)
	if saveError := runner.store.SaveProgress(context.WithoutCancel(requestContext), destination); saveError != nil {
		runner.logger.Error("進み具合を保存できませんでした", "error", saveError, "destination", destination.ID)
	}
	return errors.New(reason)
}

// processWebhook は、webhook の転送を動かす(まだ送っていないログを、まとめて送る)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - destination: 転送。
//   - now: 今の時刻。
//
// 戻り値:
//   - 失敗したとき、その理由。
func (runner *Runner) processWebhook(requestContext context.Context, destination *model.ExportDestination, now time.Time) error {
	// 続けて、何回か送る(多いときは、次の見回りに回す)。
	sentAny := false
	for batch := 0; batch < webhookBatchesPerRound; batch++ {
		rows, readError := runner.store.ReadLogs(requestContext, destination.IngressID, destination.LastCursor, webhookBatchSize)
		if readError != nil {
			return runner.fail(requestContext, destination, now, "ログを読めませんでした")
		}
		if len(rows) == 0 {
			break
		}
		lines := make([]exportlog.Line, 0, len(rows))
		for _, row := range rows {
			lines = append(lines, row.ToLine())
		}
		// 送れなければ、カーソルを進めずに、失敗を記録する(次の時刻に、同じ行を、もう一度送る)。
		if sendError := runner.webhook.Send(requestContext, *destination, lines); sendError != nil {
			return runner.fail(requestContext, destination, now, sendError.Error())
		}
		destination.RecordSuccess(now, rows[len(rows)-1].ID)
		sentAny = true
		if saveError := runner.store.SaveProgress(requestContext, destination); saveError != nil {
			return fmt.Errorf("進み具合を保存できませんでした: %w", saveError)
		}
		// 1 回分に満たなければ、もう無い。
		if len(rows) < webhookBatchSize {
			return nil
		}
	}
	// 送るものが無かったときは、次の時刻だけを決める。
	if !sentAny {
		destination.ScheduleNext(now)
		if saveError := runner.store.SaveProgress(requestContext, destination); saveError != nil {
			return fmt.Errorf("進み具合を保存できませんでした: %w", saveError)
		}
	}
	return nil
}

// processDrive は、Google Drive の転送を動かす(まだ書いていないログを、日ごとのファイルに書く)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - destination: 転送。
//   - now: 今の時刻。
//
// 戻り値:
//   - 失敗したとき、その理由。
func (runner *Runner) processDrive(requestContext context.Context, destination *model.ExportDestination, now time.Time) error {
	// OAuth のクライアントが無ければ、動かせない(次の見回りで、もう一度見る)。
	if runner.drive == nil {
		return runner.fail(requestContext, destination, now, "サーバーで Google Drive が設定されていません")
	}
	// アカウントの接続を読む。
	connection, connectionError := runner.store.FindConnection(requestContext, destination.UserID)
	if connectionError != nil {
		return runner.fail(requestContext, destination, now, "Google Drive につながっていません。アカウント設定で接続してください")
	}
	if !connection.IsActive() {
		// 再接続待ち。利用者が再接続するまで、動かさない(次に動く時刻を、先に送る)。
		destination.Status = model.ExportStatusReauthRequired
		destination.LastError = connection.LastError
		destination.NextAttemptAt = now.Add(time.Hour)
		_ = runner.store.SaveProgress(requestContext, destination)
		return nil
	}
	sentAny := false
	for batch := 0; batch < driveBatchesPerRound; batch++ {
		rows, readError := runner.store.ReadLogs(requestContext, destination.IngressID, destination.LastCursor, driveBatchSize)
		if readError != nil {
			return runner.fail(requestContext, destination, now, "ログを読めませんでした")
		}
		if len(rows) == 0 {
			break
		}
		// 書けなければ、カーソルを進めずに、失敗を記録する。許可が取り消されていたら、接続を、再接続待ちにする。
		if writeError := runner.drive.Write(requestContext, connection, *destination, rows); writeError != nil {
			if errors.Is(writeError, googledrive.ErrInvalidGrant) {
				reason := "Google の許可が無効です。アカウント設定で、Google Drive に接続し直してください"
				if markError := runner.store.MarkConnectionReauthRequired(context.WithoutCancel(requestContext), destination.UserID, reason); markError != nil {
					runner.logger.Error("再接続待ちにできませんでした", "error", markError, "user", destination.UserID)
				}
				return errors.New(reason)
			}
			return runner.fail(requestContext, destination, now, describeDriveError(writeError))
		}
		destination.RecordSuccess(now, rows[len(rows)-1].ID)
		sentAny = true
		if saveError := runner.store.SaveProgress(requestContext, destination); saveError != nil {
			return fmt.Errorf("進み具合を保存できませんでした: %w", saveError)
		}
		if len(rows) < driveBatchSize {
			return nil
		}
	}
	// 書くものが無かったときは、次の枠の時刻だけを決める。
	if !sentAny {
		destination.ScheduleNext(now)
		if saveError := runner.store.SaveProgress(requestContext, destination); saveError != nil {
			return fmt.Errorf("進み具合を保存できませんでした: %w", saveError)
		}
	}
	return nil
}

// describeDriveError は、Drive への書き込みの失敗を、利用者に見せる短い理由にする。
//
// 引数:
//   - writeError: 書き込みのエラー。
//
// 戻り値:
//   - 短い理由(内部のエラーの詳細は、含めない)。
func describeDriveError(writeError error) string {
	// 利用者向けのメッセージを持つエラーは、それを使う。それ以外は、一般的な理由にする。
	var appError *apperror.Error
	if errors.As(writeError, &appError) {
		return "Google Drive に書けませんでした"
	}
	return writeError.Error()
}
