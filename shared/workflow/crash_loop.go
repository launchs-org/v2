package workflow

import (
	"time"

	"github.com/google/uuid"
)

const (
	// StopCrashLoopWorkflowName は、異常な状態が続く deploy を止める(レプリカを 0 にする) Workflow の名前。
	StopCrashLoopWorkflowName = "StopCrashLoopWorkflow"

	// BeginCrashLoopStopActivityName は、止める準備(状態を applying にし、理由のイベントを記録する) Activity の名前。
	BeginCrashLoopStopActivityName = "BeginCrashLoopStop"

	// ScaleToZeroActivityName は、k8s の Deployment のレプリカを 0 にする Activity の名前。DB の replicas は変えない。
	ScaleToZeroActivityName = "ScaleToZero"

	// FinishCrashLoopStopActivityName は、止め終えた deploy を failed にする Activity の名前。
	FinishCrashLoopStopActivityName = "FinishCrashLoopStop"

	// StopCrashLoopWorkflowTimeout は、止める Workflow 全体の最長の実行時間(仮)。
	StopCrashLoopWorkflowTimeout = 15 * time.Minute
)

// StopCrashLoopInput は、StopCrashLoopWorkflow の入力。
type StopCrashLoopInput struct {
	// DeployID は、止める deploy の ID。
	DeployID string

	// PodUID と PodName は、止める根拠になった Pod。イベントに記録する。
	PodUID  string
	PodName string

	// Reason は、止める理由の短い名前(イベントの reason)。
	Reason string

	// Message は、止める理由の補足(再起動の回数、続いた時間など。イベントの message)。
	Message string
}

// BeginCrashLoopStopInput は、BeginCrashLoopStop Activity の入力。
type BeginCrashLoopStopInput struct {
	// Stop は、止める deploy と理由。
	Stop StopCrashLoopInput
}

// BeginCrashLoopStopResult は、BeginCrashLoopStop Activity の結果。
type BeginCrashLoopStopResult struct {
	// Started は、止める準備ができたか。deploy が active でなかった(別の処理が進んでいる)ときは false。
	Started bool
}

// ScaleToZeroInput は、ScaleToZero Activity の入力。
type ScaleToZeroInput struct {
	// DeployID は、レプリカを 0 にする deploy の ID。
	DeployID string
}

// FinishCrashLoopStopInput は、FinishCrashLoopStop Activity の入力。
type FinishCrashLoopStopInput struct {
	// DeployID は、止め終えた deploy の ID。
	DeployID string
}

// NewStopCrashLoopWorkflowID は、止める Workflow の ID を作る。形式は `{操作}-{リソースID}-{uuid}`。
//
// 引数:
//   - deployID: 止める deploy の ID。
//
// 戻り値:
//   - Workflow ID。
func NewStopCrashLoopWorkflowID(deployID string) string {
	// 操作ごとに一意にするため、末尾に UUID を付ける。
	return "stop-crash-loop-" + deployID + "-" + uuid.NewString()
}
