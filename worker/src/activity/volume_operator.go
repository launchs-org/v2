package activity

import (
	"context"
	"errors"
)

// ErrInvalidVolumeName は、PVC の名前が volume の ID の形ではないときのエラー。
var ErrInvalidVolumeName = errors.New("PVC の名前が volume の ID の形ではありません")

// ErrVolumeExpandTimeout は、PVC の拡張が、待つ時間の内に反映されなかったときのエラー。
var ErrVolumeExpandTimeout = errors.New("PVC の拡張が、期限までに反映されませんでした")

// VolumeClaimDefinition は、作る PVC の内容。
type VolumeClaimDefinition struct {
	// Namespace は、PVC を作る namespace(プロジェクトの ID)。
	Namespace string

	// Name は、PVC の名前(volume の ID)。
	Name string

	// SizeMB は、大きさ(MB)。
	SizeMB int
}

// VolumeOperator は、k8s の PVC を操作するもの。
// 実際に k8s を操作する実装と、操作せずログに残すだけの実装(開発用)を、差し替えられるようにする。
type VolumeOperator interface {
	// EnsureVolumeClaim は、PVC が無ければ作る。すでにあれば何もしない(冪等。大きさは変えない)。
	EnsureVolumeClaim(requestContext context.Context, definition VolumeClaimDefinition) error

	// ExpandVolumeClaim は、PVC があれば、大きさを広げ、容量が増えるのを待つ。無ければ、何もせず成功する(まだマウントされていない)。
	// すでにその大きさ以上なら、何もしない(冪等。縮めない)。待つ時間を超えたら ErrVolumeExpandTimeout。
	// 待っている間は、heartbeat で生きていることを伝える。
	ExpandVolumeClaim(requestContext context.Context, definition VolumeClaimDefinition, heartbeat func()) error

	// DeleteVolumeClaim は、PVC を削除する(無くてもエラーにしない。冪等)。
	DeleteVolumeClaim(requestContext context.Context, namespaceName string, claimName string) error
}
