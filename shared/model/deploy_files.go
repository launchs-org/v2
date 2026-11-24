package model

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"gorm.io/datatypes"

	"launchs/shared/apperror"
)

// このファイルは、deploy の「ファイル」(設定ファイルなどを、ConfigMap としてマウントする。dev-docs/templates.md の段階 A)の、
// 形の検査と、状態を変える操作を持つ。メソッドは、メモリ上の値だけを変える(保存は repository が行う)。

const (
	// MaxFilesPerDeploy は、1つの deploy に持たせられるファイルの数の上限。
	MaxFilesPerDeploy = 20

	// MaxFileContentBytes は、ファイル1つの内容の大きさの上限(バイト)。
	MaxFileContentBytes = 64 * 1024

	// MaxFilesTotalBytes は、1つの deploy のファイルの内容の合計の上限(バイト)。ConfigMap の上限(1MiB)に、余裕を持たせる。
	MaxFilesTotalBytes = 256 * 1024

	// MaxFilePathLength は、ファイルの path の長さの上限。
	MaxFilePathLength = 200
)

// filePathPattern は、ファイルの path に使える文字(「/」で始まり、英数字と . _ - / だけ)。
var filePathPattern = regexp.MustCompile(`^/[A-Za-z0-9._\-/]*$`)

// FileSpec は、deploy がコンテナに置くファイル1つ。deploys.files と deploys.pending_files の JSON の配列の要素。
type FileSpec struct {
	// Path は、コンテナの中の、ファイルの path(「/」で始まる)。
	Path string `json:"path"`

	// Content は、ファイルの内容(UTF-8 のテキスト)。
	Content string `json:"content"`
}

// ValidateFiles は、ファイルの構成の形(個数、path の書式、内容の大きさ、重複、マウントとの重なり)を確かめる。
// 他の deploy や、データベースを見る必要のあることは、ここでは確かめない。
//
// 引数:
//   - files: 確かめるファイルの構成。空は、「全て外す」ので、許す。
//   - mounts: 同じ deploy のボリュームのマウント。ファイルの path が、マウント先と**まったく同じ**だと、ボリュームを、ファイルで覆ってしまうので、拒否する。
//     マウント先の**下**(例: ボリュームを /data に、ファイルを /data/init.sql に置く)は、許す(subPath のマウントで、共存できることを、実機で確認済み)。
//
// 戻り値:
//   - 不正なら invalid_request。
func ValidateFiles(files []FileSpec, mounts []MountSpec) error {
	// 個数の上限を確かめる。
	if len(files) > MaxFilesPerDeploy {
		return apperror.New(apperror.KindInvalidRequest, "files は、"+itoa(MaxFilesPerDeploy)+"個までです")
	}
	seenPaths := map[string]bool{}
	totalBytes := 0
	for _, file := range files {
		// path の書式を確かめる(「/」で始まる、使える文字だけ、長さ、「/」だけは不可、「..」と「//」と末尾の「/」は不可)。
		if !IsValidFilePath(file.Path) {
			return apperror.New(apperror.KindInvalidRequest, "files の path は、「/」で始まる、英数字と . _ - / だけのファイルの path にしてください(「..」、「//」、末尾の「/」は不可。200文字まで): "+file.Path)
		}
		// 同じ path の重複を拒否する。
		if seenPaths[file.Path] {
			return apperror.New(apperror.KindInvalidRequest, "files の path が重複しています: "+file.Path)
		}
		seenPaths[file.Path] = true
		// 内容は、UTF-8 のテキストだけ(バイナリは扱わない。NUL 文字も拒否する)。
		if !utf8.ValidString(file.Content) || strings.ContainsRune(file.Content, 0) {
			return apperror.New(apperror.KindInvalidRequest, "files の内容は、UTF-8 のテキストだけです: "+file.Path)
		}
		// 1つの大きさの上限を確かめる。
		if len(file.Content) > MaxFileContentBytes {
			return apperror.New(apperror.KindInvalidRequest, "files の内容は、1つ64KiBまでです: "+file.Path)
		}
		totalBytes += len(file.Content)
		// マウント先と、まったく同じ path は、拒否する(下は許す)。
		for _, mount := range mounts {
			if file.Path == mount.MountPath {
				return apperror.New(apperror.KindInvalidRequest, "files の path が、ボリュームのマウント先と同じです: "+file.Path)
			}
		}
	}
	// 合計の大きさの上限を確かめる。
	if totalBytes > MaxFilesTotalBytes {
		return apperror.New(apperror.KindInvalidRequest, "files の内容の合計は、256KiBまでです")
	}
	return nil
}

// IsValidFilePath は、コンテナの中のファイルの path として使える書式かを返す(deploy のファイルと、ConfigMap のマウントで共通)。
//
// 「/」で始まり、英数字と . _ - / だけ、200文字まで。「/」だけ、「..」、「//」、末尾の「/」は不可。
//
// 引数:
//   - filePath: 確かめる path。
//
// 戻り値:
//   - 使える書式なら true。
func IsValidFilePath(filePath string) bool {
	// 文字の種類と長さ、「/」だけ、「..」、「//」、末尾の「/」、正規化して変わるものを、全て拒否する。
	return filePathPattern.MatchString(filePath) && len(filePath) <= MaxFilePathLength && filePath != "/" &&
		!strings.Contains(filePath, "..") && !strings.Contains(filePath, "//") && !strings.HasSuffix(filePath, "/") && path.Clean(filePath) == filePath
}

// itoa は、整数を、10進の文字列にする(エラーメッセージ用)。
//
// 引数:
//   - value: 整数。
//
// 戻り値:
//   - 10進の文字列。
func itoa(value int) string {
	// JSON の数値の書式が、10進の整数と同じなので、それを使う。
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// ParseFiles は、JSON のファイルの構成を読み込む。
//
// 引数:
//   - raw: JSON の配列(空なら、ファイル無し)。
//
// 戻り値:
//   - ファイルの構成。読めなければエラー。
func ParseFiles(raw datatypes.JSON) ([]FileSpec, error) {
	// 空(NULL)は、ファイル無し。
	if len(raw) == 0 {
		return []FileSpec{}, nil
	}
	// JSON を読み込む。
	files := []FileSpec{}
	if parseError := json.Unmarshal(raw, &files); parseError != nil {
		return nil, parseError
	}
	return files, nil
}

// HasPendingFiles は、まだ適用していないファイルの構成があるかを返す(空の配列も、「全て外す」変更としてある扱い)。
//
// 戻り値:
//   - あれば true。
func (deploy Deploy) HasPendingFiles() bool {
	// pending の列に、値があるか。
	return len(deploy.PendingFiles) > 0
}

// BeginFilesApply は、ファイルの反映を始める(状態を applying にする)。
//
// 戻り値:
//   - 受け付ける前の状態(開始に失敗したとき、元に戻すために使う)。
//   - 設定を操作できない状態(反映中、削除の途中)、起動中、または適用する値が無いときは conflict。
func (deploy *Deploy) BeginFilesApply() (string, error) {
	// 反映中や削除の途中は、操作できない。
	if !deploy.CanEditSettings() {
		return "", apperror.New(apperror.KindConflict, "deploy は "+deploy.Status+" のため、ファイルを操作できません")
	}
	// 作成の起動中は、反映を始められない。
	if deploy.IsStarting() {
		return "", apperror.New(apperror.KindConflict, "deploy は起動中のため、ファイルを適用できません")
	}
	// 適用する値が無いときは、反映する意味が無い。
	if !deploy.HasPendingFiles() {
		return "", apperror.New(apperror.KindConflict, "適用するファイルの変更がありません")
	}
	// 前の状態を覚えて、反映中にする。
	previousStatus := deploy.Status
	deploy.Status = DeployStatusApplying
	return previousStatus, nil
}

// CommitFiles は、ファイルの反映の結果を確定する。
//
//   - 成功: ファイルを pending の内容にし、pending を空に戻し、状態を落ち着いた状態にする。
//   - 失敗: ファイルと pending は、そのまま。状態を failed にする。
//
// 引数:
//   - succeeded: 反映に成功したか。
//
// 戻り値:
//   - 確定したら true。applying でないときは、何も変えず false。
func (deploy *Deploy) CommitFiles(succeeded bool) bool {
	// 反映中でなければ、確定するものが無い。
	if !deploy.IsApplying() {
		return false
	}
	// 失敗のときは、状態だけを failed にする。
	if !succeeded {
		deploy.Status = DeployStatusFailed
		return true
	}
	// 成功のときは、pending があるときだけ、ファイルにして pending を空に戻す。
	if deploy.HasPendingFiles() {
		deploy.Files = deploy.PendingFiles
		deploy.PendingFiles = nil
	}
	// 落ち着いた状態にする。
	deploy.Status = deploy.SettledStatus()
	return true
}
