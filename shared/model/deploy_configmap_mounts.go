package model

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"

	"gorm.io/datatypes"

	"launchs/shared/apperror"
)

// このファイルは、deploy が共有の ConfigMap のファイルをマウントする構成(dev-docs/configmap.md §2、§5)の、
// 形の検査と、状態を変える操作を持つ。メソッドは、メモリ上の値だけを変える(保存は repository が行う)。

const (
	// MaxConfigMapMounts は、1つの deploy の ConfigMap のマウントの数の上限。
	MaxConfigMapMounts = 30

	// MaxFilesAndConfigMapMounts は、1つの deploy の、(ConfigMap のマウント + files)の数の合計の上限。Deployment の大きさのため。
	MaxFilesAndConfigMapMounts = 40

	// DefaultConfigMapMountMode は、mode を省略したときの権限。
	DefaultConfigMapMountMode = "0644"
)

// configMapModePattern は、mode の書式(8進数の4桁の文字列。"0000" から "0777")。
var configMapModePattern = regexp.MustCompile(`^0[0-7]{3}$`)

// ConfigMapMountSpec は、共有の ConfigMap の 1 ファイルを、1 つの path にマウントする指定。
// deploys.configmap_mounts と deploys.pending_configmap_mounts の JSON の配列の要素。
type ConfigMapMountSpec struct {
	// ConfigMapID は、マウントする ConfigMap の ID(同じプロジェクトのもの)。
	ConfigMapID string `json:"configMapId"`

	// Revision は、固定する版の番号。保存するとき、省略(0)なら、サーバーが決める。
	Revision int `json:"revision"`

	// Key は、ConfigMap の中のファイル名。
	Key string `json:"key"`

	// Path は、コンテナの中のファイルの path。
	Path string `json:"path"`

	// Mode は、ファイルの権限(8進数の4桁の文字列)。省略は "0644"。
	Mode string `json:"mode"`
}

// ConfigMapUsage は、ConfigMap を使っている deploy の、1 つのマウント。
type ConfigMapUsage struct {
	// DeployID は、使っている deploy の ID。
	DeployID string

	// Revision は、使っている版。
	Revision int

	// Key は、マウントしているファイル名。
	Key string

	// Path は、マウント先の path。
	Path string

	// Pending は、まだ適用していない(pending の)マウントなら true。
	Pending bool
}

// ConfigMapRevisionResourceName は、k8s の ConfigMap の名前(版ごとに作る、immutable な ConfigMap)を返す。
//
// 引数:
//   - configMapID: ConfigMap の ID。
//   - revision: 版の番号。
//
// 戻り値:
//   - "<configMapId>-r<revision>"。
func ConfigMapRevisionResourceName(configMapID string, revision int) string {
	// ID に、版の番号を付ける。
	return configMapID + "-r" + strconv.Itoa(revision)
}

// ModeBits は、mode の文字列(8進数)を、数値にする。
//
// 戻り値:
//   - 権限の数値。読めなければ 0o644。
func (mount ConfigMapMountSpec) ModeBits() int32 {
	// 8進数として読む。読めなければ、既定の 0644 にする(検査済みなので、通常は起きない)。
	parsed, parseError := strconv.ParseInt(mount.Mode, 8, 32)
	if parseError != nil {
		return 0o644
	}
	return int32(parsed)
}

// ValidateConfigMapMounts は、ConfigMap のマウント構成の形(個数、ID と key の形、path の書式、mode、重複)を確かめ、
// 省略された mode を既定にして返す。ConfigMap が実在するか、key があるか、などは、データベースを見る必要があるので、ここでは確かめない。
//
// 引数:
//   - mounts: 確かめる構成(空でもよい。空は、全て外す)。
//
// 戻り値:
//   - mode を埋めた構成(引数は変えない)。
//   - 不正なら invalid_request。
func ValidateConfigMapMounts(mounts []ConfigMapMountSpec) ([]ConfigMapMountSpec, error) {
	// 個数の上限を確かめる。
	if len(mounts) > MaxConfigMapMounts {
		return nil, apperror.New(apperror.KindInvalidRequest, "mounts は、"+strconv.Itoa(MaxConfigMapMounts)+"件までです")
	}
	// 結果と、path の重複を調べるための入れ物。
	validated := make([]ConfigMapMountSpec, 0, len(mounts))
	usedPaths := map[string]bool{}
	for _, mount := range mounts {
		// ConfigMap の ID の形を確かめる。
		if len(mount.ConfigMapID) <= len(ConfigMapIDPrefix) || mount.ConfigMapID[:len(ConfigMapIDPrefix)] != ConfigMapIDPrefix {
			return nil, apperror.New(apperror.KindInvalidRequest, "configMapId の形が正しくありません: "+mount.ConfigMapID)
		}
		// key の書式を確かめる(ConfigMap のファイル名と同じ規則)。
		if !configMapKeyPattern.MatchString(mount.Key) || len(mount.Key) > MaxConfigMapKeyLength || mount.Key == "." || mount.Key == ".." {
			return nil, apperror.New(apperror.KindInvalidRequest, "mounts の key の形が正しくありません: "+mount.Key)
		}
		// 版は、省略(0)か、1以上。
		if mount.Revision < 0 {
			return nil, apperror.New(apperror.KindInvalidRequest, "mounts の revision は、1以上でなければなりません")
		}
		// path の書式を確かめる(deploy のファイルと同じ規則)。
		if !IsValidFilePath(mount.Path) {
			return nil, apperror.New(apperror.KindInvalidRequest, "mounts の path は、「/」で始まる、英数字と . _ - / だけのファイルの path にしてください(「..」、「//」、末尾の「/」は不可。200文字まで): "+mount.Path)
		}
		// 同じ path の重複を拒否する(同じファイルを、別の path に置くのは、よい)。
		if usedPaths[mount.Path] {
			return nil, apperror.New(apperror.KindInvalidRequest, "mounts の path が重複しています: "+mount.Path)
		}
		usedPaths[mount.Path] = true
		// mode は、省略なら既定。書式は、8進数の4桁。
		if mount.Mode == "" {
			mount.Mode = DefaultConfigMapMountMode
		}
		if !configMapModePattern.MatchString(mount.Mode) {
			return nil, apperror.New(apperror.KindInvalidRequest, "mounts の mode は、8進数の4桁の文字列(\"0000\" から \"0777\")にしてください: "+mount.Mode)
		}
		validated = append(validated, mount)
	}
	return validated, nil
}

// ValidateConfigMapMountPaths は、ConfigMap のマウントの path が、同じ deploy の、ボリュームのマウント先と、ファイルの path と、
// まったく同じにならないこと、および、ファイルとの数の合計が上限以内であることを確かめる。
// マウント先の**下**は、許す(subPath のマウントで、共存できる)。
//
// 引数:
//   - configMapMounts: ConfigMap のマウント。
//   - files: 同じ deploy のファイル。
//   - volumeMounts: 同じ deploy のボリュームのマウント。
//
// 戻り値:
//   - 不正なら invalid_request。
func ValidateConfigMapMountPaths(configMapMounts []ConfigMapMountSpec, files []FileSpec, volumeMounts []MountSpec) error {
	// (ConfigMap のマウント + files)の数の合計の上限を確かめる。
	if len(configMapMounts)+len(files) > MaxFilesAndConfigMapMounts {
		return apperror.New(apperror.KindInvalidRequest, "ConfigMap のマウントと files の数の合計は、"+strconv.Itoa(MaxFilesAndConfigMapMounts)+"個までです")
	}
	// 他の path を、集める。
	otherPaths := map[string]string{}
	for _, file := range files {
		otherPaths[file.Path] = "files の path"
	}
	for _, volumeMount := range volumeMounts {
		otherPaths[volumeMount.MountPath] = "ボリュームのマウント先"
	}
	// ConfigMap のマウントの path が、他と、まったく同じでないことを確かめる。
	for _, mount := range configMapMounts {
		if owner, exists := otherPaths[mount.Path]; exists {
			return apperror.New(apperror.KindInvalidRequest, "ConfigMap のマウントの path が、"+owner+"と同じです: "+mount.Path)
		}
	}
	return nil
}

// ParseConfigMapMounts は、JSON の ConfigMap のマウント構成を読み込む。
//
// 引数:
//   - raw: JSON の配列(空なら、マウント無し)。
//
// 戻り値:
//   - マウント構成。読めなければエラー。
func ParseConfigMapMounts(raw datatypes.JSON) ([]ConfigMapMountSpec, error) {
	// 空(NULL)は、マウント無し。
	if len(raw) == 0 {
		return []ConfigMapMountSpec{}, nil
	}
	// JSON を読み込む。
	mounts := []ConfigMapMountSpec{}
	if parseError := json.Unmarshal(raw, &mounts); parseError != nil {
		return nil, parseError
	}
	return mounts, nil
}

// SortConfigMapMounts は、マウント構成を、path の順に並べた写しを返す(Deployment の内容を、毎回、同じ順番にするため)。
//
// 引数:
//   - mounts: マウント構成。
//
// 戻り値:
//   - path の昇順に並べた写し。
func SortConfigMapMounts(mounts []ConfigMapMountSpec) []ConfigMapMountSpec {
	// 写してから並べる。
	sorted := append([]ConfigMapMountSpec(nil), mounts...)
	sort.Slice(sorted, func(first int, second int) bool { return sorted[first].Path < sorted[second].Path })
	return sorted
}

// HasPendingConfigMapMounts は、まだ適用していない ConfigMap のマウント構成があるかを返す(空の配列も、「全て外す」変更としてある扱い)。
//
// 戻り値:
//   - あれば true。
func (deploy Deploy) HasPendingConfigMapMounts() bool {
	// pending の列に、値があるか。
	return len(deploy.PendingConfigMapMounts) > 0
}

// BeginConfigMapMountsApply は、ConfigMap のマウントの反映を始める(状態を applying にする)。
//
// 戻り値:
//   - 受け付ける前の状態(開始に失敗したとき、元に戻すために使う)。
//   - 設定を操作できない状態(反映中、削除の途中)、起動中、または適用する値が無いときは conflict。
func (deploy *Deploy) BeginConfigMapMountsApply() (string, error) {
	// 反映中や削除の途中は、操作できない。
	if !deploy.CanEditSettings() {
		return "", apperror.New(apperror.KindConflict, "deploy は "+deploy.Status+" のため、ConfigMap のマウントを操作できません")
	}
	// 作成の起動中は、反映を始められない。
	if deploy.IsStarting() {
		return "", apperror.New(apperror.KindConflict, "deploy は起動中のため、ConfigMap のマウントを適用できません")
	}
	// 適用する値が無いときは、反映する意味が無い。
	if !deploy.HasPendingConfigMapMounts() {
		return "", apperror.New(apperror.KindConflict, "適用する ConfigMap のマウントの変更がありません")
	}
	// 前の状態を覚えて、反映中にする。
	previousStatus := deploy.Status
	deploy.Status = DeployStatusApplying
	return previousStatus, nil
}

// CommitConfigMapMounts は、ConfigMap のマウントの反映の結果を確定する。
//
//   - 成功: マウントを pending の内容にし、pending を空に戻し、状態を落ち着いた状態にする。
//   - 失敗: マウントと pending は、そのまま。状態を failed にする。
//
// 引数:
//   - succeeded: 反映に成功したか。
//
// 戻り値:
//   - 確定したら true。applying でないときは、何も変えず false。
func (deploy *Deploy) CommitConfigMapMounts(succeeded bool) bool {
	// 反映中でなければ、確定するものが無い。
	if !deploy.IsApplying() {
		return false
	}
	// 失敗のときは、状態だけを failed にする。
	if !succeeded {
		deploy.Status = DeployStatusFailed
		return true
	}
	// 成功のときは、pending があるときだけ、マウントにして pending を空に戻す。
	if deploy.HasPendingConfigMapMounts() {
		deploy.ConfigMapMounts = deploy.PendingConfigMapMounts
		deploy.PendingConfigMapMounts = nil
	}
	// 落ち着いた状態にする。
	deploy.Status = deploy.SettledStatus()
	return true
}
