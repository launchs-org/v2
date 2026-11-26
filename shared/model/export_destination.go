package model

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/fnv"
	"time"

	"gorm.io/datatypes"

	"launchs/shared/apperror"
)

const (
	// ExportDestinationIDPrefix は、転送の設定の ID の接頭辞。ID は、この後ろに UUID v4 を付けたもの。
	ExportDestinationIDPrefix = "export-"

	// MaxExportDestinationsPerUser は、1 つのアカウントが持てる、転送の設定の数の上限。
	MaxExportDestinationsPerUser = 10

	// ExportKindWebhook と ExportKindGoogleDrive は、転送の種類。
	ExportKindWebhook     = "webhook"
	ExportKindGoogleDrive = "google_drive"

	// ExportStatusActive などは、転送の状態。
	ExportStatusActive         = "active"
	ExportStatusFailing        = "failing"
	ExportStatusReauthRequired = "reauth_required"

	// DefaultWebhookIntervalSeconds は、webhook の送る間隔の既定(秒)。
	DefaultWebhookIntervalSeconds = 30

	// MaxExportBackoff は、失敗が続いたときの、再試行の間隔の上限。
	MaxExportBackoff = time.Hour

	// DriveSlotCount は、Drive の書き込みを散らすための、1 時間の枠の数(5 分ごと)。
	DriveSlotCount = 12

	// ExportLastErrorMaxLength は、画面に出す最後のエラーの長さの上限(文字数)。
	ExportLastErrorMaxLength = 300
)

// allowedWebhookIntervals は、webhook の送る間隔として選べる値(秒)。ログの元が 10 秒単位なので、これより短くはしない。
var allowedWebhookIntervals = map[int]bool{10: true, 30: true, 60: true, 300: true}

// ExportDestination は、export_destinations テーブルの 1 行。ある ingress のアクセスログを、ある送り先に送る設定。
type ExportDestination struct {
	// ID は、転送の設定の ID("export-" + UUID v4)。
	ID string `gorm:"column:id;primaryKey"`

	// UserID は、持ち主のユーザー ID。
	UserID string `gorm:"column:user_id"`

	// ProjectID は、プロジェクトの ID。
	ProjectID string `gorm:"column:project_id"`

	// IngressID は、対象の ingress の ID。
	IngressID string `gorm:"column:ingress_id"`

	// Kind は、種類(webhook / google_drive)。
	Kind string `gorm:"column:kind"`

	// Enabled は、有効かどうか。
	Enabled bool `gorm:"column:enabled"`

	// Config は、種類ごとの設定(JSON)。webhook は WebhookConfig。
	Config datatypes.JSON `gorm:"column:config"`

	// Credential は、webhook の署名の秘密(暗号化したもの)。Google Drive は空。
	Credential []byte `gorm:"column:credential"`

	// Status は、状態(active / failing / reauth_required)。
	Status string `gorm:"column:status"`

	// LastCursor は、送り済みの、ingress_access_logs.id の最後。
	LastCursor int64 `gorm:"column:last_cursor"`

	// LastSuccessAt は、最後に送れた日時。まだ送れていなければ nil。
	LastSuccessAt *time.Time `gorm:"column:last_success_at"`

	// LastError は、最後のエラー(利用者に見せる短い理由)。
	LastError string `gorm:"column:last_error"`

	// FailureCount は、続けて失敗した回数。
	FailureCount int `gorm:"column:failure_count"`

	// NextAttemptAt は、次に動く時刻。
	NextAttemptAt time.Time `gorm:"column:next_attempt_at"`

	// CreatedAt は、作った日時。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("export_destinations")。
func (ExportDestination) TableName() string {
	// このモデルが対応するテーブルの名前を返す。
	return "export_destinations"
}

// WebhookConfig は、webhook の設定(Config の JSON の形)。
type WebhookConfig struct {
	// URL は、送り先の URL。
	URL string `json:"url"`

	// IntervalSeconds は、まとめて送る間隔(秒)。
	IntervalSeconds int `json:"intervalSeconds"`
}

// ValidateWebhookInterval は、webhook の送る間隔が、選べる値かを確かめる。
//
// 引数:
//   - seconds: 間隔(秒)。
//
// 戻り値:
//   - 不正なら invalid_request。
func ValidateWebhookInterval(seconds int) error {
	// 選べる値(10、30、60、300)だけを受け付ける。
	if !allowedWebhookIntervals[seconds] {
		return apperror.New(apperror.KindInvalidRequest, "送る間隔は、10、30、60、300 秒のどれかにしてください")
	}
	return nil
}

// NewWebhookConfig は、webhook の設定を、保存する JSON にする。
//
// 引数:
//   - config: 設定。
//
// 戻り値:
//   - JSON。
func NewWebhookConfig(config WebhookConfig) datatypes.JSON {
	// 構造体を JSON にする(文字列と整数だけなので、失敗しない)。
	encoded, _ := json.Marshal(config)
	return datatypes.JSON(encoded)
}

// WebhookConfig は、webhook の設定を取り出す。
//
// 戻り値:
//   - 設定。webhook でない、または、壊れているときはエラー。
func (destination *ExportDestination) WebhookConfig() (WebhookConfig, error) {
	// webhook 以外には、無い。
	if destination.Kind != ExportKindWebhook {
		return WebhookConfig{}, errors.New("webhook ではありません")
	}
	// JSON を読む。間隔が無い(古い行など)ときは、既定にする。
	var config WebhookConfig
	if unmarshalError := json.Unmarshal(destination.Config, &config); unmarshalError != nil {
		return WebhookConfig{}, unmarshalError
	}
	if config.IntervalSeconds == 0 {
		config.IntervalSeconds = DefaultWebhookIntervalSeconds
	}
	return config, nil
}

// NewWebhookSecret は、webhook の署名に使う、推測できない秘密を作る。
//
// 戻り値:
//   - "whsec_" と、乱数の 24 バイト(16 進数)をつないだ文字列。乱数を得られなければエラー。
func NewWebhookSecret() (string, error) {
	// 暗号論的な乱数で、24 バイトを作る。
	randomBytes := make([]byte, 24)
	if _, readError := rand.Read(randomBytes); readError != nil {
		return "", apperror.Wrap(apperror.KindInternal, "秘密を作れませんでした", readError)
	}
	return "whsec_" + hex.EncodeToString(randomBytes), nil
}

// IntervalSeconds は、この転送を、どれくらいの間隔で動かすかを返す(失敗したときの再試行の、元になる間隔)。
//
// 戻り値:
//   - webhook は、選んだ間隔。Google Drive は、1 時間(枠の時刻に動く)。
func (destination *ExportDestination) IntervalSeconds() int {
	// webhook は、設定の間隔(読めなければ既定)。
	if destination.Kind == ExportKindWebhook {
		if config, configError := destination.WebhookConfig(); configError == nil {
			return config.IntervalSeconds
		}
		return DefaultWebhookIntervalSeconds
	}
	return 3600
}

// DriveSlot は、Drive の書き込みを、1 時間の中で散らすための枠の番号(0〜11)を返す。
// 転送の設定の ID から決まる(同じ時刻にまとめて作っても、偏らない)。
//
// 戻り値:
//   - 0〜11。枠 n は、毎時 5n 分。
func (destination *ExportDestination) DriveSlot() int {
	// ID のハッシュを、枠の数で割った余り。
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(destination.ID))
	return int(hasher.Sum32() % DriveSlotCount)
}

// NextDriveSlotTime は、これから先で、一番近い、この転送の枠の時刻を返す。
//
// 引数:
//   - after: この時刻より、後の枠を探す。
//
// 戻り値:
//   - 毎時 5n 分(n は枠の番号)の、after より後で、一番近い時刻。
func (destination *ExportDestination) NextDriveSlotTime(after time.Time) time.Time {
	// 時の始まりに、枠の分を足したものが、候補。
	hourStart := after.Truncate(time.Hour)
	candidate := hourStart.Add(time.Duration(destination.DriveSlot()*5) * time.Minute)
	// 過ぎていれば、次の時。
	if !candidate.After(after) {
		candidate = candidate.Add(time.Hour)
	}
	return candidate
}

// RecordSuccess は、送れたときの状態にする(カーソルを進め、失敗の記録を消し、次に動く時刻を決める)。
//
// 引数:
//   - now: 今の時刻。
//   - newCursor: 送り済みの、最後の id。
func (destination *ExportDestination) RecordSuccess(now time.Time, newCursor int64) {
	// 進める。失敗の記録は、消す。
	destination.LastCursor = newCursor
	destination.LastSuccessAt = &now
	destination.LastError = ""
	destination.FailureCount = 0
	destination.Status = ExportStatusActive
	// 次の時刻: Drive は、次の枠。webhook は、選んだ間隔のあと。
	if destination.Kind == ExportKindGoogleDrive {
		destination.NextAttemptAt = destination.NextDriveSlotTime(now)
		return
	}
	destination.NextAttemptAt = now.Add(time.Duration(destination.IntervalSeconds()) * time.Second)
}

// ScheduleNext は、送るものが無かったときに、次に動く時刻だけを決める(カーソルや、最後の成功の日時は、変えない)。
//
// 引数:
//   - now: 今の時刻。
func (destination *ExportDestination) ScheduleNext(now time.Time) {
	// Drive は、次の枠。webhook は、選んだ間隔のあと。
	if destination.Kind == ExportKindGoogleDrive {
		destination.NextAttemptAt = destination.NextDriveSlotTime(now)
		return
	}
	destination.NextAttemptAt = now.Add(time.Duration(destination.IntervalSeconds()) * time.Second)
}

// RecordFailure は、送れなかったときの状態にする(失敗を数え、間隔を倍々に延ばして、次に動く時刻を決める)。
//
// 引数:
//   - now: 今の時刻。
//   - reason: 利用者に見せる理由(長ければ切る)。
func (destination *ExportDestination) RecordFailure(now time.Time, reason string) {
	// 失敗を数えて、理由を残す。
	destination.FailureCount++
	destination.Status = ExportStatusFailing
	runes := []rune(reason)
	if len(runes) > ExportLastErrorMaxLength {
		runes = runes[:ExportLastErrorMaxLength]
	}
	destination.LastError = string(runes)
	// 選んだ間隔(Drive は 5 分)から始めて、失敗のたびに倍にし、上限で止める。
	base := time.Duration(destination.IntervalSeconds()) * time.Second
	if destination.Kind == ExportKindGoogleDrive {
		base = 5 * time.Minute
	}
	delay := base
	for attempt := 1; attempt < destination.FailureCount && delay < MaxExportBackoff; attempt++ {
		delay *= 2
	}
	if delay > MaxExportBackoff {
		delay = MaxExportBackoff
	}
	destination.NextAttemptAt = now.Add(delay)
}

// GoogleDriveConnection は、google_drive_connections テーブルの 1 行。アカウントの Google Drive の接続。
type GoogleDriveConnection struct {
	// UserID は、持ち主のユーザー ID。
	UserID string `gorm:"column:user_id;primaryKey"`

	// AccountEmail は、つないだ Google アカウントのメールアドレス(表示用)。
	AccountEmail string `gorm:"column:account_email"`

	// Credential は、暗号化した合鍵(リフレッシュトークン)。
	Credential []byte `gorm:"column:credential"`

	// RootFolderID は、launchs-access-logs フォルダの ID。
	RootFolderID string `gorm:"column:root_folder_id"`

	// Status は、状態(active / reauth_required)。
	Status string `gorm:"column:status"`

	// LastError は、最後のエラー(利用者に見せる短い理由)。
	LastError string `gorm:"column:last_error"`

	// CreatedAt は、つないだ日時。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("google_drive_connections")。
func (GoogleDriveConnection) TableName() string {
	// このモデルが対応するテーブルの名前を返す。
	return "google_drive_connections"
}

// IsActive は、接続が使える状態かを返す。
//
// 戻り値:
//   - 使えるなら true。
func (connection GoogleDriveConnection) IsActive() bool {
	// active のときだけ、使える。
	return connection.Status == ExportStatusActive
}
