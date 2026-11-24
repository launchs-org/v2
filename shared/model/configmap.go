package model

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/datatypes"

	"launchs/shared/apperror"
)

// このファイルは、プロジェクトで共有する ConfigMap(dev-docs/configmap.md)の、モデルと、中身の検査を持つ。
// メソッドは、メモリ上の値だけを扱う(保存は repository が行う)。

const (
	// ConfigMapIDPrefix は、ConfigMap の ID の接頭辞。ID は、この後ろに UUID v4 を付けたもの。
	ConfigMapIDPrefix = "configmap-"

	// ConfigMapEncodingText は、内容を UTF-8 のテキストとして持つ(k8s の ConfigMap の data になる)。
	ConfigMapEncodingText = "text"

	// ConfigMapEncodingBase64 は、内容を base64 のバイナリとして持つ(k8s の ConfigMap の binaryData になる)。
	ConfigMapEncodingBase64 = "base64"

	// MaxConfigMapStoredBytes は、1つの ConfigMap の、保存する大きさの合計の上限(バイト)。
	// 数え方は、ファイルごとに「key の長さ + 保存する内容の長さ」の合計(base64 のファイルは、base64 の文字列の長さで数える)。
	// k8s の ConfigMap の上限(全体で 1MiB)を、必ず下回る。ファイル1つの上限も、これと同じ。
	MaxConfigMapStoredBytes = 1024 * 1024

	// MaxConfigMapFiles は、1つの ConfigMap のファイルの数の上限。
	MaxConfigMapFiles = 50

	// MaxConfigMapKeyLength は、ファイル名(key)の長さの上限。k8s の ConfigMap のキーの上限と同じ。
	MaxConfigMapKeyLength = 253

	// MaxConfigMapsPerProject は、1つのプロジェクトに作れる ConfigMap の数の上限。
	MaxConfigMapsPerProject = 50

	// MaxConfigMapRevisionsKept は、残しておく版の数(最新の版を含む直近の分)。これより古い版は、編集のときに消す。
	MaxConfigMapRevisionsKept = 10
)

// configMapKeyPattern は、ファイル名(key)に使える文字(k8s の ConfigMap のキーの規則)。
var configMapKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ConfigMap は、configmaps テーブルの1行。プロジェクトで共有する、設定ファイルの組。
type ConfigMap struct {
	// ID は、ConfigMap の ID("configmap-" + UUID v4)。
	ID string `gorm:"column:id;primaryKey"`

	// ProjectID は、属するプロジェクトの ID。
	ProjectID string `gorm:"column:project_id"`

	// UserID は、持ち主のユーザー ID。
	UserID string `gorm:"column:user_id"`

	// Name は、プロジェクトの中で一意の名前。
	Name string `gorm:"column:name"`

	// CurrentRevision は、最新の版の番号(1 から)。
	CurrentRevision int `gorm:"column:current_revision"`

	// CreatedAt は、作成日時。
	CreatedAt time.Time `gorm:"column:created_at"`

	// UpdatedAt は、更新日時。
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("configmaps")。
func (ConfigMap) TableName() string {
	// このモデルが対応するテーブルの名前を返す。
	return "configmaps"
}

// ConfigMapRevision は、configmap_revisions テーブルの1行。ConfigMap の、ある版の中身。追記だけで、更新しない。
type ConfigMapRevision struct {
	// ConfigMapID は、ConfigMap の ID。
	ConfigMapID string `gorm:"column:configmap_id;primaryKey"`

	// Revision は、版の番号(1 から)。
	Revision int `gorm:"column:revision;primaryKey"`

	// Files は、ファイルの JSON の配列(ConfigMapFile。key の昇順)。
	Files datatypes.JSON `gorm:"column:files"`

	// CreatedAt は、この版を作った日時。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName は、Gorm にテーブル名を教える。
//
// 戻り値:
//   - テーブル名("configmap_revisions")。
func (ConfigMapRevision) TableName() string {
	// このモデルが対応するテーブルの名前を返す。
	return "configmap_revisions"
}

// ConfigMapFile は、ConfigMap のファイル1つ。configmap_revisions.files の JSON の配列の要素。
type ConfigMapFile struct {
	// Key は、ファイル名(k8s の ConfigMap のキー)。
	Key string `json:"key"`

	// Encoding は、内容の持ち方。"text"(UTF-8 のテキスト)か "base64"(バイナリ)。
	Encoding string `json:"encoding"`

	// Content は、内容。Encoding が text ならそのままの文字列、base64 なら base64 の文字列。
	Content string `json:"content"`
}

// StoredBytes は、このファイルが、保存する大きさ(key の長さ + 内容の長さ)を返す。
//
// 戻り値:
//   - 保存する大きさ(バイト)。
func (file ConfigMapFile) StoredBytes() int {
	// key と、保存する形の内容の長さを足す(base64 は、base64 の文字列のまま数える)。
	return len(file.Key) + len(file.Content)
}

// DecodedBytes は、このファイルの、元のバイト数を返す(画面に出す大きさ)。
//
// 戻り値:
//   - text なら内容のバイト数。base64 なら、デコードしたバイト数(デコードできなければ 0)。
func (file ConfigMapFile) DecodedBytes() int {
	// text は、そのままのバイト数。
	if file.Encoding != ConfigMapEncodingBase64 {
		return len(file.Content)
	}
	// base64 は、デコードして数える。
	decoded, decodeError := base64.StdEncoding.DecodeString(file.Content)
	if decodeError != nil {
		return 0
	}
	return len(decoded)
}

// ValidateConfigMapFiles は、ConfigMap のファイルの組を検査し、key の昇順に並べて返す。
//
// 検査すること: 個数、key の書式と重複、encoding の値、text が UTF-8 で NUL を含まないこと、
// base64 が正しい形(標準の base64。改行や空白は不可)であること、ファイル1つと合計の大きさ。
//
// 引数:
//   - files: 検査するファイル。空でもよい(中身が空の ConfigMap)。
//
// 戻り値:
//   - key の昇順に並べたファイル(引数は変えない)。
//   - 不正なら invalid_request。
func ValidateConfigMapFiles(files []ConfigMapFile) ([]ConfigMapFile, error) {
	// 個数の上限を確かめる。
	if len(files) > MaxConfigMapFiles {
		return nil, apperror.New(apperror.KindInvalidRequest, "files は、"+itoa(MaxConfigMapFiles)+"個までです")
	}
	// 重複の検出用の入れ物と、合計の大きさ。
	seenKeys := map[string]bool{}
	totalBytes := 0
	for _, file := range files {
		// key の書式を確かめる(使える文字、長さ、「.」と「..」は不可)。
		if !configMapKeyPattern.MatchString(file.Key) || len(file.Key) > MaxConfigMapKeyLength || file.Key == "." || file.Key == ".." {
			return nil, apperror.New(apperror.KindInvalidRequest, "files の key は、英数字と . _ - だけの1〜253文字にしてください(「.」と「..」は不可): "+file.Key)
		}
		// 同じ key の重複を拒否する。
		if seenKeys[file.Key] {
			return nil, apperror.New(apperror.KindInvalidRequest, "files の key が重複しています: "+file.Key)
		}
		seenKeys[file.Key] = true
		// 内容の持ち方ごとに、内容の形を確かめる。
		switch file.Encoding {
		case ConfigMapEncodingText:
			// text は、UTF-8 で、NUL 文字を含まないこと(NUL を含むものは、base64 で送る)。
			if !utf8.ValidString(file.Content) || strings.ContainsRune(file.Content, 0) {
				return nil, apperror.New(apperror.KindInvalidRequest, "encoding が text の内容は、NUL 文字を含まない UTF-8 のテキストだけです(バイナリは base64 で送ってください): "+file.Key)
			}
		case ConfigMapEncodingBase64:
			// base64 は、標準の形(パディングあり、改行なし)であること。Go のデコーダは改行を読み飛ばすので、改行は先に拒否する。
			if strings.ContainsAny(file.Content, "\r\n") {
				return nil, apperror.New(apperror.KindInvalidRequest, "encoding が base64 の内容に、改行は含められません: "+file.Key)
			}
			if _, decodeError := base64.StdEncoding.Strict().DecodeString(file.Content); decodeError != nil {
				return nil, apperror.New(apperror.KindInvalidRequest, "encoding が base64 の内容が、正しい base64 ではありません: "+file.Key)
			}
		default:
			// 知らない encoding は拒否する。
			return nil, apperror.New(apperror.KindInvalidRequest, "files の encoding は、text か base64 です: "+file.Key)
		}
		// ファイル1つの大きさの上限を確かめる。
		if file.StoredBytes() > MaxConfigMapStoredBytes {
			return nil, apperror.New(apperror.KindInvalidRequest, "ファイル1つの大きさが上限(1MiB。base64 は、base64 にした後の大きさで数えます)を超えています: "+file.Key)
		}
		totalBytes += file.StoredBytes()
	}
	// 合計の大きさの上限を確かめる。
	if totalBytes > MaxConfigMapStoredBytes {
		return nil, apperror.New(apperror.KindInvalidRequest, "ファイルの大きさの合計が上限(1MiB。base64 は、base64 にした後の大きさで数えます)を超えています")
	}
	// 入力を変えないよう、写してから、key の昇順に並べる(同じ中身は、順番が違っても、同じ版として扱うため)。
	sortedFiles := append([]ConfigMapFile{}, files...)
	sort.Slice(sortedFiles, func(left int, right int) bool { return sortedFiles[left].Key < sortedFiles[right].Key })
	return sortedFiles, nil
}

// ParseConfigMapFiles は、版に保存された JSON のファイルの配列を読み込む。
//
// 引数:
//   - raw: JSON の配列(空なら、ファイル無し)。
//
// 戻り値:
//   - ファイルの配列。読めなければエラー。
func ParseConfigMapFiles(raw datatypes.JSON) ([]ConfigMapFile, error) {
	// 空は、ファイル無し。
	if len(raw) == 0 {
		return []ConfigMapFile{}, nil
	}
	// JSON を読み込む。
	files := []ConfigMapFile{}
	if parseError := json.Unmarshal(raw, &files); parseError != nil {
		return nil, parseError
	}
	return files, nil
}

// ConfigMapFilesTotalBytes は、ファイルの保存する大きさの合計を返す。
//
// 引数:
//   - files: ファイル。
//
// 戻り値:
//   - 合計(バイト)。
func ConfigMapFilesTotalBytes(files []ConfigMapFile) int {
	// 1つずつ足す。
	total := 0
	for _, file := range files {
		total += file.StoredBytes()
	}
	return total
}

// SameConfigMapFiles は、2つのファイルの組が、同じ中身かを返す(key の昇順に並べたもの同士を比べる)。
//
// 引数:
//   - left: 1つ目(key の昇順)。
//   - right: 2つ目(key の昇順)。
//
// 戻り値:
//   - 同じなら true。
func SameConfigMapFiles(left []ConfigMapFile, right []ConfigMapFile) bool {
	// 個数が違えば、違う。
	if len(left) != len(right) {
		return false
	}
	// 1つずつ比べる(key、encoding、内容の全てが同じ)。
	for index := range left {
		if left[index].Key != right[index].Key || left[index].Encoding != right[index].Encoding || !bytes.Equal([]byte(left[index].Content), []byte(right[index].Content)) {
			return false
		}
	}
	return true
}

// ConfigMapSummary は、ConfigMap の一覧の1件(中身は含めず、ファイルの数と大きさだけを持つ)。
type ConfigMapSummary struct {
	// ConfigMap は、ConfigMap の行。
	ConfigMap ConfigMap

	// FileCount は、最新の版のファイルの数。
	FileCount int

	// TotalBytes は、最新の版の、保存する大きさの合計(バイト)。
	TotalBytes int

	// UsedBy は、この ConfigMap を使っている deploy の数(pending のマウントも含む)。
	UsedBy int
}

// ConfigMapRevisionSummary は、版の一覧の1件(中身は含めない)。
type ConfigMapRevisionSummary struct {
	// Revision は、版の番号。
	Revision int

	// CreatedAt は、この版を作った日時。
	CreatedAt time.Time

	// FileCount は、この版のファイルの数。
	FileCount int

	// TotalBytes は、この版の、保存する大きさの合計(バイト)。
	TotalBytes int
}
