// Package observer は、k8s の Pod と Deployment を観測し、実際の状態を DB に反映する。
package observer

import (
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"launchs/shared/k8s"
	"launchs/shared/model"
)

// unschedulableReason は、Pod をどのノードにも置けないときに、待っている理由として記録する値。
const unschedulableReason = "Unschedulable"

// BuildPodSnapshot は、k8s の Pod から、DB に保存する Pod の状態を作る。
//
// deploy の Pod かどうかは、deploy の ID のラベルで判定する。ユーザーのコンテナ(app)の状態を主に見る。
//   - status: 削除の処理に入っていれば terminating、phase が Running なら running、それ以外は pending
//   - 待っている理由: app コンテナの待ち(CrashLoopBackOff など)。まだノードに置かれていなければ Unschedulable
//   - 最後の終了: app コンテナの、最後に終了したときの理由と終了コード
//
// 引数:
//   - pod: k8s の Pod。
//   - observedAt: 観測した時刻。
//
// 戻り値:
//   - Pod の状態。
//   - deploy の ID のラベルが無いなど、対象でないときは false。
func BuildPodSnapshot(pod *corev1.Pod, observedAt time.Time) (model.DeployPod, bool) {
	// deploy の ID のラベルが無い Pod は、対象外。
	deployID := pod.Labels[k8s.DeployIDLabel]
	if deployID == "" {
		return model.DeployPod{}, false
	}

	// status を決める。
	status := model.PodStatusPending
	if pod.Status.Phase == corev1.PodRunning {
		status = model.PodStatusRunning
	}
	if pod.DeletionTimestamp != nil {
		status = model.PodStatusTerminating
	}

	// Ready の条件と、再起動の回数の合計。
	ready := false
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			ready = true
		}
	}
	restartCount := 0
	for _, containerStatus := range pod.Status.ContainerStatuses {
		restartCount += int(containerStatus.RestartCount)
	}

	snapshot := model.DeployPod{
		PodUID: string(pod.UID), DeployID: deployID, ProjectID: pod.Namespace, PodName: pod.Name,
		Status: status, Phase: string(pod.Status.Phase), Ready: ready, RestartCount: restartCount,
		NodeName: pod.Spec.NodeName, PodCreatedAt: pod.CreationTimestamp.Time, ObservedAt: observedAt,
	}

	// app コンテナの状態から、待っている理由、最後の終了、起動した時刻を読む。
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Name != k8s.AppContainerName {
			continue
		}
		if containerStatus.State.Waiting != nil {
			snapshot.WaitingReason = containerStatus.State.Waiting.Reason
		}
		if containerStatus.State.Running != nil {
			startedAt := containerStatus.State.Running.StartedAt.Time
			snapshot.StartedAt = &startedAt
		}
		// 最後に終了したときの状態。無ければ、いま終了している状態(再起動されない Pod など)を使う。
		terminated := containerStatus.LastTerminationState.Terminated
		if terminated == nil {
			terminated = containerStatus.State.Terminated
		}
		if terminated != nil {
			exitCode := int(terminated.ExitCode)
			snapshot.LastTerminationReason = terminated.Reason
			snapshot.LastExitCode = &exitCode
		}
	}
	// まだ app コンテナの状態が無く、ノードに置けない場合は、その理由を記録する。
	if snapshot.WaitingReason == "" && pod.Spec.NodeName == "" {
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse {
				snapshot.WaitingReason = unschedulableReason
			}
		}
	}
	return snapshot, true
}

// BuildDeploymentSnapshot は、k8s の Deployment から、DB に保存する観測値を作る。
//
// 引数:
//   - deployment: k8s の Deployment。
//   - observedAt: 観測した時刻。
//
// 戻り値:
//   - 観測値。
//   - deploy の ID のラベルが無いなど、対象でないときは false。
func BuildDeploymentSnapshot(deployment *appsv1.Deployment, observedAt time.Time) (model.DeployObservation, bool) {
	// deploy の ID のラベルが無い Deployment は、対象外。
	deployID := deployment.Labels[k8s.DeployIDLabel]
	if deployID == "" {
		return model.DeployObservation{}, false
	}
	// status の各レプリカ数を、そのまま写す。
	return model.DeployObservation{
		DeployID:           deployID,
		Generation:         deployment.Generation,
		ObservedGeneration: deployment.Status.ObservedGeneration,
		Replicas:           int(deployment.Status.Replicas),
		UpdatedReplicas:    int(deployment.Status.UpdatedReplicas),
		ReadyReplicas:      int(deployment.Status.ReadyReplicas),
		AvailableReplicas:  int(deployment.Status.AvailableReplicas),
		ObservedAt:         observedAt,
	}, true
}
