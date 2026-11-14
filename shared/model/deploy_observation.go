package model

import "time"

const (
	// PodStatusPending は、まだ起動していない Pod。
	PodStatusPending = "pending"

	// PodStatusRunning は、起動している Pod。
	PodStatusRunning = "running"

	// PodStatusTerminating は、終了の処理に入っている Pod。
	PodStatusTerminating = "terminating"

	// PodEventCreated は、Pod が作られた(初めて観測された)ことを表すイベント。
	PodEventCreated = "created"

	// PodEventReady は、Pod が準備完了(Ready)になったことを表すイベント。
	PodEventReady = "ready"

	// PodEventNotReady は、Pod が準備完了でなくなったことを表すイベント。
	PodEventNotReady = "not_ready"

	// PodEventRestarted は、コンテナが再起動した(クラッシュなど)ことを表すイベント。理由と終了コードを持つ。
	PodEventRestarted = "restarted"

	// PodEventWaiting は、コンテナが、異常な理由(CrashLoopBackOff、ImagePullBackOff など)で待ち始めたことを表すイベント。
	PodEventWaiting = "waiting"

	// PodEventTerminating は、Pod が終了の処理に入ったことを表すイベント。
	PodEventTerminating = "terminating"

	// PodEventDeleted は、Pod が消えたことを表すイベント。
	PodEventDeleted = "deleted"
)

// DeployObservation は、deploy_observations テーブルの1行。k8s の Deployment の、観測した状態。
type DeployObservation struct {
	// DeployID は、deploy の ID(主キー)。
	DeployID string `gorm:"column:deploy_id;primaryKey"`

	// Generation は、Deployment の最新の設定の世代。
	Generation int64 `gorm:"column:generation"`

	// ObservedGeneration は、k8s が処理済みの設定の世代。
	ObservedGeneration int64 `gorm:"column:observed_generation"`

	// Replicas は、Pod の総数。
	Replicas int `gorm:"column:replicas"`

	// UpdatedReplicas は、最新の設定の Pod の数。
	UpdatedReplicas int `gorm:"column:updated_replicas"`

	// ReadyReplicas は、準備完了の Pod の数。
	ReadyReplicas int `gorm:"column:ready_replicas"`

	// AvailableReplicas は、使える状態の Pod の数。
	AvailableReplicas int `gorm:"column:available_replicas"`

	// ObservedAt は、watcher がこの値を観測した時刻。
	ObservedAt time.Time `gorm:"column:observed_at"`
}

// DeployPod は、deploy_pods テーブルの1行。Pod の、観測した現在の状態。
type DeployPod struct {
	// PodUID は、k8s の Pod の UID(主キー)。
	PodUID string `gorm:"column:pod_uid;primaryKey"`

	// DeployID は、属する deploy の ID。
	DeployID string `gorm:"column:deploy_id"`

	// ProjectID は、Pod の namespace(プロジェクトの ID)。
	ProjectID string `gorm:"column:project_id"`

	// PodName は、Pod の名前。
	PodName string `gorm:"column:pod_name"`

	// Status は、PodStatus... のどれか。
	Status string `gorm:"column:status"`

	// Phase は、k8s の Pod の phase。
	Phase string `gorm:"column:phase"`

	// Ready は、Ready の条件が True か。
	Ready bool `gorm:"column:ready"`

	// RestartCount は、コンテナの再起動の回数の合計。
	RestartCount int `gorm:"column:restart_count"`

	// WaitingReason は、いま待っている理由。待っていなければ空。
	WaitingReason string `gorm:"column:waiting_reason"`

	// LastTerminationReason は、最後に終了したときの理由。無ければ空。
	LastTerminationReason string `gorm:"column:last_termination_reason"`

	// LastExitCode は、最後に終了したときの終了コード。無ければ nil。
	LastExitCode *int `gorm:"column:last_exit_code"`

	// NodeName は、Pod が動くノードの名前。
	NodeName string `gorm:"column:node_name"`

	// PodCreatedAt は、k8s が Pod を作った時刻。
	PodCreatedAt time.Time `gorm:"column:pod_created_at"`

	// StartedAt は、コンテナが起動した時刻。まだなら nil。
	StartedAt *time.Time `gorm:"column:started_at"`

	// ObservedAt は、watcher がこの値を観測した時刻。
	ObservedAt time.Time `gorm:"column:observed_at"`
}

// DeployPodEvent は、deploy_pod_events テーブルの1行。Pod の状態変化のログ。
type DeployPodEvent struct {
	// ID は、イベントの連番(主キー)。
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`

	// DeployID は、属する deploy の ID。
	DeployID string `gorm:"column:deploy_id"`

	// PodUID は、Pod の UID。
	PodUID string `gorm:"column:pod_uid"`

	// PodName は、Pod の名前。
	PodName string `gorm:"column:pod_name"`

	// Type は、PodEvent... のどれか。
	Type string `gorm:"column:type"`

	// Reason は、理由(OOMKilled、CrashLoopBackOff など)。無ければ空。
	Reason string `gorm:"column:reason"`

	// Message は、補足(終了コードなど)。無ければ空。
	Message string `gorm:"column:message"`

	// OccurredAt は、変化を観測した時刻。
	OccurredAt time.Time `gorm:"column:occurred_at"`
}
