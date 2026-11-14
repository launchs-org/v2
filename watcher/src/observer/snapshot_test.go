package observer

import (
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"launchs/shared/k8s"
	"launchs/shared/model"
)

// newTestPodObject は、deploy の Pod の、テスト用のオブジェクトを作る。
//
// 引数:
//   - modify: 作ったあとに、内容を変える関数(変えないなら nil)。
//
// 戻り値:
//   - 起動して Ready の、app コンテナ1つの Pod。
func newTestPodObject(modify func(pod *corev1.Pod)) *corev1.Pod {
	startedAt := metav1.NewTime(time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-1", Namespace: "project-1", UID: "uid-1",
			Labels:            map[string]string{k8s.DeployIDLabel: "deploy-1"},
			CreationTimestamp: startedAt,
		},
		Spec: corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: k8s.AppContainerName, Ready: true,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: startedAt}},
			}},
		},
	}
	if modify != nil {
		modify(pod)
	}
	return pod
}

// TestBuildPodSnapshot は、k8s の Pod から、status・Ready・再起動・待ち・最後の終了が読み取れることを確認する。
func TestBuildPodSnapshot(testingT *testing.T) {
	observedAt := time.Date(2026, 11, 14, 11, 0, 0, 0, time.UTC)

	testingT.Run("起動して Ready の Pod", func(subTest *testing.T) {
		snapshot, relevant := BuildPodSnapshot(newTestPodObject(nil), observedAt)
		if !relevant || snapshot.PodUID != "uid-1" || snapshot.DeployID != "deploy-1" || snapshot.ProjectID != "project-1" ||
			snapshot.Status != model.PodStatusRunning || !snapshot.Ready || snapshot.NodeName != "node-1" || snapshot.StartedAt == nil || !snapshot.ObservedAt.Equal(observedAt) {
			subTest.Errorf("読み取りが違う: %+v", snapshot)
		}
	})
	testingT.Run("deploy の ID のラベルが無い Pod は対象外", func(subTest *testing.T) {
		pod := newTestPodObject(func(pod *corev1.Pod) { pod.Labels = map[string]string{} })
		if _, relevant := BuildPodSnapshot(pod, observedAt); relevant {
			subTest.Errorf("対象外のはず")
		}
	})
	testingT.Run("削除の処理に入った Pod は terminating", func(subTest *testing.T) {
		deletedAt := metav1.Now()
		snapshot, _ := BuildPodSnapshot(newTestPodObject(func(pod *corev1.Pod) { pod.DeletionTimestamp = &deletedAt }), observedAt)
		if snapshot.Status != model.PodStatusTerminating {
			subTest.Errorf("terminating のはず: %q", snapshot.Status)
		}
	})
	testingT.Run("phase が Pending なら pending で、Ready ではない", func(subTest *testing.T) {
		snapshot, _ := BuildPodSnapshot(newTestPodObject(func(pod *corev1.Pod) {
			pod.Status.Phase = corev1.PodPending
			pod.Status.Conditions = nil
			pod.Status.ContainerStatuses = nil
		}), observedAt)
		if snapshot.Status != model.PodStatusPending || snapshot.Ready {
			subTest.Errorf("pending で Ready ではないはず: %+v", snapshot)
		}
	})
	testingT.Run("クラッシュを繰り返している Pod は、待ちの理由・再起動・最後の終了を読み取る", func(subTest *testing.T) {
		snapshot, _ := BuildPodSnapshot(newTestPodObject(func(pod *corev1.Pod) {
			pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}}
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
				Name: k8s.AppContainerName, RestartCount: 4,
				State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
				LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}},
			}}
		}), observedAt)
		if snapshot.Ready || snapshot.RestartCount != 4 || snapshot.WaitingReason != "CrashLoopBackOff" ||
			snapshot.LastTerminationReason != "OOMKilled" || snapshot.LastExitCode == nil || *snapshot.LastExitCode != 137 {
			subTest.Errorf("読み取りが違う: %+v", snapshot)
		}
	})
	testingT.Run("ノードに置けない Pod は Unschedulable で待っている", func(subTest *testing.T) {
		snapshot, _ := BuildPodSnapshot(newTestPodObject(func(pod *corev1.Pod) {
			pod.Spec.NodeName = ""
			pod.Status.Phase = corev1.PodPending
			pod.Status.ContainerStatuses = nil
			pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse}}
		}), observedAt)
		if snapshot.WaitingReason != "Unschedulable" {
			subTest.Errorf("Unschedulable のはず: %q", snapshot.WaitingReason)
		}
	})
	testingT.Run("app 以外のコンテナの状態は、待ちの理由に使わない", func(subTest *testing.T) {
		snapshot, _ := BuildPodSnapshot(newTestPodObject(func(pod *corev1.Pod) {
			pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{
				Name: "sidecar", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			})
		}), observedAt)
		if snapshot.WaitingReason != "" {
			subTest.Errorf("待ちの理由は空のはず: %q", snapshot.WaitingReason)
		}
	})
}

// TestBuildDeploymentSnapshot は、Deployment の status が、観測値に写ることを確認する。
func TestBuildDeploymentSnapshot(testingT *testing.T) {
	observedAt := time.Now()
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "deploy-1", Generation: 3, Labels: map[string]string{k8s.DeployIDLabel: "deploy-1"}},
		Status:     appsv1.DeploymentStatus{ObservedGeneration: 2, Replicas: 3, UpdatedReplicas: 2, ReadyReplicas: 1, AvailableReplicas: 1},
	}
	testingT.Run("status の各値が写る", func(subTest *testing.T) {
		snapshot, relevant := BuildDeploymentSnapshot(deployment, observedAt)
		if !relevant || snapshot.DeployID != "deploy-1" || snapshot.Generation != 3 || snapshot.ObservedGeneration != 2 ||
			snapshot.Replicas != 3 || snapshot.UpdatedReplicas != 2 || snapshot.ReadyReplicas != 1 || snapshot.AvailableReplicas != 1 {
			subTest.Errorf("写り方が違う: %+v", snapshot)
		}
	})
	testingT.Run("deploy の ID のラベルが無ければ対象外", func(subTest *testing.T) {
		deployment.Labels = nil
		if _, relevant := BuildDeploymentSnapshot(deployment, observedAt); relevant {
			subTest.Errorf("対象外のはず")
		}
	})
}
