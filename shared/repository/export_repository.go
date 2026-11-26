package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"launchs/shared/apperror"
	"launchs/shared/exportlog"
	"launchs/shared/model"
)

// ExportRepository は、アクセスログの外への書き出し(転送の設定、Google Drive の接続、進み具合)を読み書きする。
// backend(設定の操作)と exporter(送る処理)が使う。
type ExportRepository struct {
	// database は、Gorm のデータベース接続。
	database *gorm.DB
}

// NewExportRepository は、ExportRepository を作る。
//
// 引数:
//   - database: Gorm のデータベース接続。
//
// 戻り値:
//   - 作った repository。
func NewExportRepository(database *gorm.DB) *ExportRepository {
	// 接続を保持した repository を返す。
	return &ExportRepository{database: database}
}

// ExportLogRow は、送るログの 1 行(ingress のホスト名つき)。
type ExportLogRow struct {
	// ID は、ingress_access_logs.id。
	ID int64 `gorm:"column:id"`

	// IngressID は、ingress の ID。
	IngressID string `gorm:"column:ingress_id"`

	// Host は、ingress のホスト名。
	Host string `gorm:"column:host"`

	// RequestedAt は、リクエストを受けた時刻。
	RequestedAt time.Time `gorm:"column:requested_at"`

	// Method は、メソッド。
	Method string `gorm:"column:method"`

	// URL は、パスとクエリ。
	URL string `gorm:"column:url"`

	// Status は、ステータス。
	Status int `gorm:"column:status"`

	// DurationMs は、応答にかかった時間(ミリ秒)。
	DurationMs float64 `gorm:"column:duration_ms"`

	// ClientIP は、クライアントの IP。
	ClientIP string `gorm:"column:client_ip"`

	// Bytes は、応答の大きさ(バイト)。
	Bytes int64 `gorm:"column:bytes"`
}

// ToLine は、書き出す 1 行の形にする。
//
// 戻り値:
//   - 書き出す行。
func (row ExportLogRow) ToLine() exportlog.Line {
	// 時刻を、書き出す形にして、各項目を写す。
	return exportlog.Line{ID: row.ID, IngressID: row.IngressID, Host: row.Host, Time: exportlog.FormatTime(row.RequestedAt), Method: row.Method, URL: row.URL,
		Status: row.Status, DurationMs: row.DurationMs, ClientIP: row.ClientIP, Bytes: row.Bytes}
}

// DriveFile は、export_drive_files の 1 行(Drive に書いた、1 日分のファイル)。
type DriveFile struct {
	// DestinationID は、転送の設定の ID。
	DestinationID string `gorm:"column:destination_id;primaryKey"`

	// Day は、日本時間の日付(2026-11-26)。
	Day string `gorm:"column:day;primaryKey"`

	// Part は、分割の番号(1 から)。
	Part int `gorm:"column:part;primaryKey"`

	// FileID は、Drive のファイルの ID。
	FileID string `gorm:"column:file_id"`

	// Bytes は、今のファイルの大きさ。
	Bytes int64 `gorm:"column:bytes"`

	// LogRows は、今のファイルの行数。
	LogRows int64 `gorm:"column:log_rows"`

	// LastLogID は、このファイルに書いた、ログの id の最後(やり直したときに、同じ行を二重に書かないため)。
	LastLogID int64 `gorm:"column:last_log_id"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("export_drive_files")。
func (DriveFile) TableName() string {
	// このモデルが対応するテーブルの名前を返す。
	return "export_drive_files"
}

// ---- 利用者の操作(backend) ----

// CreateDestination は、ingress に、転送の設定を作る。作った時点の、ログの最後の id から始める(過去のログは送らない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - destination: 作る設定(ID、UserID、ProjectID、IngressID、Kind、Config、Credential を入れておく)。
//
// 戻り値:
//   - ingress が無い(他人のものを含む)ときは not_found。アカウントの上限(10)、Google Drive の重複は conflict。
func (exportRepository *ExportRepository) CreateDestination(requestContext context.Context, destination *model.ExportDestination) error {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(destination.ID, destination.UserID, destination.ProjectID, destination.IngressID, destination.Kind) {
		return newEmptyConditionError()
	}
	return exportRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 持ち主の ingress を、ロックする。
		if lockError := lockOwnedIngress(transaction, destination.UserID, destination.ProjectID, destination.IngressID); lockError != nil {
			return lockError
		}
		// アカウントごとの上限の数え直しが、別の ingress で同時に起きても、ずれないように、アカウント単位で直列にする。
		if lockError := transaction.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "export-destinations:"+destination.UserID).Error; lockError != nil {
			return apperror.Wrap(apperror.KindInternal, "ロックを取れませんでした", lockError)
		}
		var count int64
		if countError := transaction.Model(&model.ExportDestination{}).Where(&model.ExportDestination{UserID: destination.UserID}).Count(&count).Error; countError != nil {
			return apperror.Wrap(apperror.KindInternal, "転送の設定を数えられませんでした", countError)
		}
		if count >= model.MaxExportDestinationsPerUser {
			return apperror.New(apperror.KindConflict, "転送の設定は、1 つのアカウントで 10 個までです")
		}
		// Google Drive への転送は、1 つの ingress に 1 つまで。
		if destination.Kind == model.ExportKindGoogleDrive {
			var driveCount int64
			if driveError := transaction.Model(&model.ExportDestination{}).Where("ingress_id = ? AND kind = ?", destination.IngressID, model.ExportKindGoogleDrive).Count(&driveCount).Error; driveError != nil {
				return apperror.Wrap(apperror.KindInternal, "転送の設定を数えられませんでした", driveError)
			}
			if driveCount > 0 {
				return apperror.New(apperror.KindConflict, "この ingress には、すでに Google Drive への転送があります")
			}
		}
		// 今の、ログの最後の id から始める(これより前のログは、送らない)。
		var lastID int64
		if cursorError := transaction.Raw("SELECT COALESCE(MAX(id), 0) FROM "+accessLogsTable+" WHERE ingress_id = ?", destination.IngressID).Scan(&lastID).Error; cursorError != nil {
			return apperror.Wrap(apperror.KindInternal, "ログの位置を取得できませんでした", cursorError)
		}
		destination.LastCursor = lastID
		// 設定が無い種類(Google Drive)は、空のオブジェクトにする(列は NULL を許さない)。
		if len(destination.Config) == 0 {
			destination.Config = datatypes.JSON("{}")
		}
		destination.Enabled = true
		destination.Status = model.ExportStatusActive
		destination.NextAttemptAt = time.Now()
		if createError := transaction.Create(destination).Error; createError != nil {
			return apperror.Wrap(apperror.KindInternal, "転送の設定を保存できませんでした", createError)
		}
		return nil
	})
}

// ListDestinations は、ingress の転送の設定を、作った順に返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//
// 戻り値:
//   - 設定の一覧(無ければ空)。ingress が無い(他人のものを含む)ときは not_found。
func (exportRepository *ExportRepository) ListDestinations(requestContext context.Context, userID string, projectID string, ingressID string) ([]model.ExportDestination, error) {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID, ingressID) {
		return nil, apperror.New(apperror.KindNotFound, "ingress が見つかりません")
	}
	// ingress が、持ち主のものかを確かめる。
	var ingressCount int64
	if countError := exportRepository.database.WithContext(requestContext).Model(&model.Ingress{}).
		Where(&model.Ingress{ID: ingressID, ProjectID: projectID, UserID: userID}).Count(&ingressCount).Error; countError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ingress を取得できませんでした", countError)
	}
	if ingressCount == 0 {
		return nil, apperror.New(apperror.KindNotFound, "ingress が見つかりません")
	}
	// 作った順に、取得する。
	var destinations []model.ExportDestination
	if findError := exportRepository.database.WithContext(requestContext).Where(&model.ExportDestination{IngressID: ingressID}).
		Order("created_at ASC, id ASC").Find(&destinations).Error; findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "転送の設定を取得できませんでした", findError)
	}
	return destinations, nil
}

// FindDestination は、持ち主の転送の設定を 1 件返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - destinationID: 転送の設定の ID。
//
// 戻り値:
//   - 設定。無い(他人のものを含む)ときは not_found。
func (exportRepository *ExportRepository) FindDestination(requestContext context.Context, userID string, projectID string, ingressID string, destinationID string) (model.ExportDestination, error) {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID, ingressID, destinationID) {
		return model.ExportDestination{}, apperror.New(apperror.KindNotFound, "転送の設定が見つかりません")
	}
	// 持ち主の、その ingress のものだけを取得する。
	var destination model.ExportDestination
	findError := exportRepository.database.WithContext(requestContext).
		Where(&model.ExportDestination{ID: destinationID, UserID: userID, ProjectID: projectID, IngressID: ingressID}).Take(&destination).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.ExportDestination{}, apperror.Wrap(apperror.KindNotFound, "転送の設定が見つかりません", findError)
	}
	if findError != nil {
		return model.ExportDestination{}, apperror.Wrap(apperror.KindInternal, "転送の設定を取得できませんでした", findError)
	}
	return destination, nil
}

// UpdateDestination は、転送の設定の、利用者が変えられる項目(有効・無効、設定、秘密)を保存する。
// 有効にしたとき、または設定を変えたときは、すぐに動けるようにする(次に動く時刻を今にし、失敗の記録を消す)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - destination: 変えたあとの設定(FindDestination で取得したものを、変えたもの)。
//
// 戻り値:
//   - 保存できなければ internal。
func (exportRepository *ExportRepository) UpdateDestination(requestContext context.Context, destination *model.ExportDestination) error {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(destination.ID) {
		return newEmptyConditionError()
	}
	// 変えられる項目だけを更新する(カーソルなどの進み具合は、exporter だけが更新する)。
	updateError := exportRepository.database.WithContext(requestContext).Model(&model.ExportDestination{}).Where("id = ?", destination.ID).Updates(map[string]any{
		"enabled": destination.Enabled, "config": destination.Config, "credential": destination.Credential,
		"status": destination.Status, "last_error": destination.LastError, "failure_count": destination.FailureCount, "next_attempt_at": destination.NextAttemptAt,
	}).Error
	if updateError != nil {
		return apperror.Wrap(apperror.KindInternal, "転送の設定を更新できませんでした", updateError)
	}
	return nil
}

// DeleteDestination は、転送の設定を削除する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - destinationID: 転送の設定の ID。
//
// 戻り値:
//   - 無い(他人のものを含む)ときは not_found。
func (exportRepository *ExportRepository) DeleteDestination(requestContext context.Context, userID string, projectID string, ingressID string, destinationID string) error {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID, ingressID, destinationID) {
		return apperror.New(apperror.KindNotFound, "転送の設定が見つかりません")
	}
	// 持ち主の、その ingress のものだけを、削除する。
	result := exportRepository.database.WithContext(requestContext).
		Where(&model.ExportDestination{ID: destinationID, UserID: userID, ProjectID: projectID, IngressID: ingressID}).Delete(&model.ExportDestination{})
	if result.Error != nil {
		return apperror.Wrap(apperror.KindInternal, "転送の設定を削除できませんでした", result.Error)
	}
	if result.RowsAffected == 0 {
		return apperror.New(apperror.KindNotFound, "転送の設定が見つかりません")
	}
	return nil
}

// ---- Google Drive の接続(アカウント) ----

// FindConnection は、アカウントの Google Drive の接続を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: ユーザー ID。
//
// 戻り値:
//   - 接続。つないでいなければ not_found。
func (exportRepository *ExportRepository) FindConnection(requestContext context.Context, userID string) (model.GoogleDriveConnection, error) {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(userID) {
		return model.GoogleDriveConnection{}, apperror.New(apperror.KindNotFound, "Google Drive につながっていません")
	}
	var connection model.GoogleDriveConnection
	findError := exportRepository.database.WithContext(requestContext).Where(&model.GoogleDriveConnection{UserID: userID}).Take(&connection).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.GoogleDriveConnection{}, apperror.Wrap(apperror.KindNotFound, "Google Drive につながっていません", findError)
	}
	if findError != nil {
		return model.GoogleDriveConnection{}, apperror.Wrap(apperror.KindInternal, "Google Drive の接続を取得できませんでした", findError)
	}
	return connection, nil
}

// SaveConnection は、Google Drive の接続を保存する(つなぎ直したときは、上書きする)。
// つなぎ直したときは、再接続待ちだった転送を、動ける状態に戻す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - connection: 保存する接続。
//
// 戻り値:
//   - 保存できなければ internal。
func (exportRepository *ExportRepository) SaveConnection(requestContext context.Context, connection *model.GoogleDriveConnection) error {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(connection.UserID) || len(connection.Credential) == 0 {
		return newEmptyConditionError()
	}
	return exportRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 同じユーザーの接続は、上書きする。
		upsertError := transaction.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"account_email", "credential", "root_folder_id", "status", "last_error"}),
		}).Create(connection).Error
		if upsertError != nil {
			return apperror.Wrap(apperror.KindInternal, "Google Drive の接続を保存できませんでした", upsertError)
		}
		// 再接続待ちだった転送を、すぐに動けるようにする。
		resumeError := transaction.Model(&model.ExportDestination{}).Where("user_id = ? AND kind = ? AND status = ?", connection.UserID, model.ExportKindGoogleDrive, model.ExportStatusReauthRequired).
			Updates(map[string]any{"status": model.ExportStatusActive, "last_error": "", "failure_count": 0, "next_attempt_at": time.Now()}).Error
		if resumeError != nil {
			return apperror.Wrap(apperror.KindInternal, "転送を再開できませんでした", resumeError)
		}
		return nil
	})
}

// SetConnectionRootFolder は、接続の、書き出し先の大元のフォルダの ID を保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: ユーザー ID。
//   - folderID: フォルダの ID。
//
// 戻り値:
//   - 保存できなければ internal。
func (exportRepository *ExportRepository) SetConnectionRootFolder(requestContext context.Context, userID string, folderID string) error {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(userID, folderID) {
		return newEmptyConditionError()
	}
	if updateError := exportRepository.database.WithContext(requestContext).Model(&model.GoogleDriveConnection{}).Where("user_id = ?", userID).Update("root_folder_id", folderID).Error; updateError != nil {
		return apperror.Wrap(apperror.KindInternal, "フォルダを保存できませんでした", updateError)
	}
	return nil
}

// MarkConnectionReauthRequired は、合鍵が効かなくなった接続を、再接続待ちにし、その接続を使う転送も、止める。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: ユーザー ID。
//   - reason: 利用者に見せる理由。
//
// 戻り値:
//   - 保存できなければ internal。
func (exportRepository *ExportRepository) MarkConnectionReauthRequired(requestContext context.Context, userID string, reason string) error {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(userID) {
		return newEmptyConditionError()
	}
	return exportRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 接続を、再接続待ちにする。
		if updateError := transaction.Model(&model.GoogleDriveConnection{}).Where("user_id = ?", userID).
			Updates(map[string]any{"status": model.ExportStatusReauthRequired, "last_error": reason}).Error; updateError != nil {
			return apperror.Wrap(apperror.KindInternal, "接続を更新できませんでした", updateError)
		}
		// その接続を使う転送も、再接続待ちにする(再接続するまで、動かさない)。
		if updateError := transaction.Model(&model.ExportDestination{}).Where("user_id = ? AND kind = ?", userID, model.ExportKindGoogleDrive).
			Updates(map[string]any{"status": model.ExportStatusReauthRequired, "last_error": reason}).Error; updateError != nil {
			return apperror.Wrap(apperror.KindInternal, "転送を更新できませんでした", updateError)
		}
		return nil
	})
}

// DeleteConnection は、Google Drive の接続を削除する。その接続を使う転送は、削除する(つなぎ直しても、自動では戻らない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: ユーザー ID。
//
// 戻り値:
//   - 削除する前の接続(合鍵の取り消しに使う)。つないでいなければ not_found。
func (exportRepository *ExportRepository) DeleteConnection(requestContext context.Context, userID string) (model.GoogleDriveConnection, error) {
	// 先に取得する(取り消しに、合鍵が要る)。
	connection, findError := exportRepository.FindConnection(requestContext, userID)
	if findError != nil {
		return model.GoogleDriveConnection{}, findError
	}
	transactionError := exportRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// この接続を使う転送を、削除する。
		if deleteError := transaction.Where("user_id = ? AND kind = ?", userID, model.ExportKindGoogleDrive).Delete(&model.ExportDestination{}).Error; deleteError != nil {
			return apperror.Wrap(apperror.KindInternal, "転送を削除できませんでした", deleteError)
		}
		// プロジェクトのフォルダの記録と、接続を削除する。
		if deleteError := transaction.Exec("DELETE FROM export_drive_folders WHERE user_id = ?", userID).Error; deleteError != nil {
			return apperror.Wrap(apperror.KindInternal, "フォルダの記録を削除できませんでした", deleteError)
		}
		if deleteError := transaction.Where("user_id = ?", userID).Delete(&model.GoogleDriveConnection{}).Error; deleteError != nil {
			return apperror.Wrap(apperror.KindInternal, "接続を削除できませんでした", deleteError)
		}
		return nil
	})
	if transactionError != nil {
		return model.GoogleDriveConnection{}, transactionError
	}
	return connection, nil
}

// ---- 送る処理(exporter) ----

// ListRunnable は、今、動かす転送(有効で、次に動く時刻を過ぎていて、再接続待ちでないもの)を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - now: 今の時刻。
//
// 戻り値:
//   - 動かす転送の一覧(次に動く時刻の順)。
func (exportRepository *ExportRepository) ListRunnable(requestContext context.Context, now time.Time) ([]model.ExportDestination, error) {
	var destinations []model.ExportDestination
	findError := exportRepository.database.WithContext(requestContext).
		Where("enabled AND status <> ? AND next_attempt_at <= ?", model.ExportStatusReauthRequired, now).Order("next_attempt_at ASC").Limit(200).Find(&destinations).Error
	if findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "転送の設定を取得できませんでした", findError)
	}
	return destinations, nil
}

// ListDriveAwaitingSlot は、有効な Google Drive の転送のうち、まだ自分の枠の時刻になっていないものを返す(行がたまりすぎていないかを見るため)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - now: 今の時刻。
//
// 戻り値:
//   - 転送の一覧。
func (exportRepository *ExportRepository) ListDriveAwaitingSlot(requestContext context.Context, now time.Time) ([]model.ExportDestination, error) {
	var destinations []model.ExportDestination
	findError := exportRepository.database.WithContext(requestContext).
		Where("enabled AND kind = ? AND status = ? AND next_attempt_at > ?", model.ExportKindGoogleDrive, model.ExportStatusActive, now).Limit(500).Find(&destinations).Error
	if findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "転送の設定を取得できませんでした", findError)
	}
	return destinations, nil
}

// CountPendingLogs は、まだ送っていないログの数を、上限(limit)まで数える。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - ingressID: ingress の ID。
//   - afterID: 送り済みの、最後の id。
//   - limit: 数える上限(これ以上は、数えない)。
//
// 戻り値:
//   - 数(limit を超えるときは limit)。
func (exportRepository *ExportRepository) CountPendingLogs(requestContext context.Context, ingressID string, afterID int64, limit int) (int, error) {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(ingressID) {
		return 0, newEmptyConditionError()
	}
	// 上限までの行を数える(多いときも、全部は数えない)。
	var count int64
	countError := exportRepository.database.WithContext(requestContext).Raw(
		"SELECT COUNT(*) FROM (SELECT 1 FROM "+accessLogsTable+" WHERE ingress_id = ? AND id > ? LIMIT ?) AS pending", ingressID, afterID, limit).Scan(&count).Error
	if countError != nil {
		return 0, apperror.Wrap(apperror.KindInternal, "ログを数えられませんでした", countError)
	}
	return int(count), nil
}

// ReadLogs は、まだ送っていないログを、id の昇順で読む。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - ingressID: ingress の ID。
//   - afterID: 送り済みの、最後の id。
//   - limit: 読む行数の上限。
//
// 戻り値:
//   - ログの行(id の昇順)。
func (exportRepository *ExportRepository) ReadLogs(requestContext context.Context, ingressID string, afterID int64, limit int) ([]ExportLogRow, error) {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(ingressID) {
		return nil, newEmptyConditionError()
	}
	// ホスト名を付けて、id の昇順で読む。
	var rows []ExportLogRow
	readError := exportRepository.database.WithContext(requestContext).Raw(
		"SELECT l.id, l.ingress_id, i.host, l.requested_at, l.method, l.url, l.status, l.duration_ms, l.client_ip, l.bytes "+
			"FROM "+accessLogsTable+" l JOIN ingresses i ON i.id = l.ingress_id WHERE l.ingress_id = ? AND l.id > ? ORDER BY l.id ASC LIMIT ?",
		ingressID, afterID, limit).Scan(&rows).Error
	if readError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ログを読めませんでした", readError)
	}
	return rows, nil
}

// SaveProgress は、転送の進み具合(カーソル、状態、エラー、次に動く時刻)を保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - destination: 進み具合を入れた設定。
//
// 戻り値:
//   - 保存できなければ internal。
func (exportRepository *ExportRepository) SaveProgress(requestContext context.Context, destination *model.ExportDestination) error {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(destination.ID) {
		return newEmptyConditionError()
	}
	// 進み具合の項目だけを更新する(利用者が変える項目は、触らない)。
	updateError := exportRepository.database.WithContext(requestContext).Model(&model.ExportDestination{}).Where("id = ?", destination.ID).Updates(map[string]any{
		"last_cursor": destination.LastCursor, "last_success_at": destination.LastSuccessAt, "last_error": destination.LastError,
		"failure_count": destination.FailureCount, "status": destination.Status, "next_attempt_at": destination.NextAttemptAt,
	}).Error
	if updateError != nil {
		return apperror.Wrap(apperror.KindInternal, "進み具合を保存できませんでした", updateError)
	}
	return nil
}

// ProjectName は、プロジェクトの名前を返す(Drive のフォルダの名前に使う)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - projectID: プロジェクトの ID。
//
// 戻り値:
//   - 名前。無ければ not_found。
func (exportRepository *ExportRepository) ProjectName(requestContext context.Context, projectID string) (string, error) {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(projectID) {
		return "", newEmptyConditionError()
	}
	var project model.Project
	findError := exportRepository.database.WithContext(requestContext).Select("id", "name").Where(&model.Project{ID: projectID}).Take(&project).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return "", apperror.Wrap(apperror.KindNotFound, "プロジェクトが見つかりません", findError)
	}
	if findError != nil {
		return "", apperror.Wrap(apperror.KindInternal, "プロジェクトを取得できませんでした", findError)
	}
	return project.Name, nil
}

// FindProjectFolder は、プロジェクトの、Drive のフォルダの ID を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: ユーザー ID。
//   - projectID: プロジェクトの ID。
//
// 戻り値:
//   - フォルダの ID。まだ作っていなければ空。
func (exportRepository *ExportRepository) FindProjectFolder(requestContext context.Context, userID string, projectID string) (string, error) {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID) {
		return "", newEmptyConditionError()
	}
	var folderIDs []string
	if findError := exportRepository.database.WithContext(requestContext).Raw("SELECT folder_id FROM export_drive_folders WHERE user_id = ? AND project_id = ?", userID, projectID).Scan(&folderIDs).Error; findError != nil {
		return "", apperror.Wrap(apperror.KindInternal, "フォルダを取得できませんでした", findError)
	}
	if len(folderIDs) == 0 {
		return "", nil
	}
	return folderIDs[0], nil
}

// SaveProjectFolder は、プロジェクトの、Drive のフォルダの ID を保存する(作り直したときは、上書きする)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: ユーザー ID。
//   - projectID: プロジェクトの ID。
//   - folderID: フォルダの ID。
//
// 戻り値:
//   - 保存できなければ internal。
func (exportRepository *ExportRepository) SaveProjectFolder(requestContext context.Context, userID string, projectID string, folderID string) error {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID, folderID) {
		return newEmptyConditionError()
	}
	saveError := exportRepository.database.WithContext(requestContext).Exec(
		"INSERT INTO export_drive_folders (user_id, project_id, folder_id) VALUES (?, ?, ?) ON CONFLICT (user_id, project_id) DO UPDATE SET folder_id = EXCLUDED.folder_id",
		userID, projectID, folderID).Error
	if saveError != nil {
		return apperror.Wrap(apperror.KindInternal, "フォルダを保存できませんでした", saveError)
	}
	return nil
}

// FindDriveFile は、その日の、一番新しい分割のファイルを返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - destinationID: 転送の設定の ID。
//   - day: 日本時間の日付(2026-11-26)。
//
// 戻り値:
//   - ファイルの記録。まだ無ければ nil。
func (exportRepository *ExportRepository) FindDriveFile(requestContext context.Context, destinationID string, day string) (*DriveFile, error) {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(destinationID, day) {
		return nil, newEmptyConditionError()
	}
	var files []DriveFile
	// 日付は、2026-11-26 の形の文字列で受け取る(date 型のままだと、時刻つきの形になる)。
	if findError := exportRepository.database.WithContext(requestContext).Select("destination_id, to_char(day, 'YYYY-MM-DD') AS day, part, file_id, bytes, log_rows, last_log_id").
		Where("destination_id = ? AND day = ?", destinationID, day).Order("part DESC").Limit(1).Find(&files).Error; findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ファイルの記録を取得できませんでした", findError)
	}
	if len(files) == 0 {
		return nil, nil
	}
	return &files[0], nil
}

// SaveDriveFile は、Drive に書いたファイルの記録を保存する(続きを書いたときは、上書きする)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - file: ファイルの記録。
//
// 戻り値:
//   - 保存できなければ internal。
func (exportRepository *ExportRepository) SaveDriveFile(requestContext context.Context, file *DriveFile) error {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(file.DestinationID, file.Day, file.FileID) || file.Part < 1 {
		return newEmptyConditionError()
	}
	saveError := exportRepository.database.WithContext(requestContext).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "destination_id"}, {Name: "day"}, {Name: "part"}},
		DoUpdates: clause.AssignmentColumns([]string{"file_id", "bytes", "log_rows", "last_log_id"}),
	}).Create(file).Error
	if saveError != nil {
		return apperror.Wrap(apperror.KindInternal, "ファイルの記録を保存できませんでした", saveError)
	}
	return nil
}
