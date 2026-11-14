package repository

import (
	"context"
	"launchs/shared/apperror"
	"launchs/shared/model"
	"time"
)

// CrashLoopCandidate は、異常な状態が続いていて、止める候補になった deploy と、その根拠になった Pod。
type CrashLoopCandidate struct {
	// DeployID は、止める候補の deploy の ID。
	DeployID string
	// PodUID と PodName は、根拠になった(最も長く不健全な)Pod。
	PodUID  string
	PodName string
	// RestartCount は、その Pod のコンテナの再起動の回数。
	RestartCount int
	// WaitingReason は、その Pod がいま待っている理由(CrashLoopBackOff など)。無ければ空。
	WaitingReason string
	// UnhealthyFor は、その Pod が、安定して動いていない状態が続いている時間。
	UnhealthyFor time.Duration
}

// CrashLoopLimits は、異常な状態が続く deploy を止める判断の上限。
type CrashLoopLimits struct {
	// UnhealthyFor は、Pod が安定して動いていない状態が、この時間以上続いたら、止める。
	// 一時的な障害(依存先の短い停止など)は、k8s のバックオフに乗り越えさせ、本当に壊れているものだけを止めるための猶予。
	// 「安定して動いている」の判定(model.PodStableAfter)より、長くすること(短いと、起動したばかりの正常な Pod を止めてしまう)。
	UnhealthyFor time.Duration
}

// ListCrashLoopCandidates は、異常な状態が続いていて、止めるべき deploy を、1つにつき1件返す。
//
// 対象は、次を全て満たす deploy。
//   - status が active で、希望するレプリカ数が 1 以上(作成中・反映中・削除中などには、触らない)
//   - 終了処理中でない Pod の中に、安定して動いている Pod が1つも無い(一部の Pod だけが不調で、他が正常なら、止めない)
//   - Pod のどれかが、安定して動いていない状態が、limits.UnhealthyFor 以上続いている
//
// 時間の判定は、データベースの時計で行う。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - limits: 上限。
//
// 戻り値:
//   - 止める候補(deploy ごとに、最も長く不健全な Pod を根拠にする)。無ければ空。
//   - 上限が不正なとき(0 以下)、またはデータベースのエラー。
func (observationRepository *DeployObservationRepository) ListCrashLoopCandidates(requestContext context.Context, limits CrashLoopLimits) ([]CrashLoopCandidate, error) {
	// 0 以下の上限だと、全ての deploy が対象になってしまうので、先に拒否する。
	if limits.UnhealthyFor <= 0 {
		return nil, newEmptyConditionError()
	}
	// データベースの時計の現在時刻を取る(Pod の時刻と、同じ時計で比べるため)。
	var databaseNow time.Time
	if nowError := observationRepository.database.WithContext(requestContext).Raw("SELECT now()").Scan(&databaseNow).Error; nowError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "データベースの時刻を取得できませんでした", nowError)
	}
	// 稼働中でレプリカがあり、不健全な Pod を持つ deploy を、Pod ごと読み込む。
	var deploys []model.Deploy
	queryError := observationRepository.database.WithContext(requestContext).
		Preload("Pods").
		Where("status = ? AND replicas > 0", model.DeployStatusActive).
		Where("EXISTS (SELECT 1 FROM deploy_pods WHERE deploy_pods.deploy_id = deploys.id AND deploy_pods.unhealthy_since IS NOT NULL)").
		Find(&deploys).Error
	if queryError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "止める候補の deploy を取得できませんでした", queryError)
	}
	// 止めるかどうかの判断は、deploy(モデル)に任せる。
	candidates := make([]CrashLoopCandidate, 0, len(deploys))
	for _, deploy := range deploys {
		// 止める理由になる Pod が無ければ、候補にしない。
		pod, shouldStop := deploy.CrashLoopingPod(databaseNow, limits.UnhealthyFor)
		if !shouldStop {
			continue
		}
		candidates = append(candidates, CrashLoopCandidate{
			DeployID: deploy.ID, PodUID: pod.PodUID, PodName: pod.PodName, RestartCount: pod.RestartCount,
			WaitingReason: pod.WaitingReason, UnhealthyFor: databaseNow.Sub(*pod.UnhealthySince),
		})
	}
	return candidates, nil
}
