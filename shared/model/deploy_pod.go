package model

import "time"

// このファイルは、Pod(DeployPod)自身が持つ規則を置く。Pod は、deploy が持つ。

// IsStable は、Pod が、時刻 at の時点で「安定して動いている」かを返す。
//
// Ready のままで、コンテナが PodStableAfter 以上動き続けているとき。クラッシュを繰り返す Pod は、
// 一瞬 Ready になることがあるが、コンテナの起動から日が浅いので、安定とはみなさない。
//
// 引数:
//   - pod: Pod の状態。
//   - at: 判定する時刻。
//
// 戻り値:
//   - 安定して動いていれば true。
func (pod DeployPod) IsStable(at time.Time) bool {
	// Ready で、コンテナが起動していて、その起動から十分に時間が経っているとき。
	return pod.Ready && pod.StartedAt != nil && at.Sub(*pod.StartedAt) >= PodStableAfter
}

// NextUnhealthySince は、Pod の新しい状態に対する、「安定して動いている状態でなくなった時刻」を決める。
//
// 規則:
//   - 今、安定している: nil(不健全ではない)
//   - 前の状態が無い: 今(初めて観測した時刻。以前から不健全だったかは分からないので、ここから数える)
//   - 前から不健全で、その間ずっと安定していなかった: 前の時刻のまま
//   - 前の状態は、今の時刻まで安定して動いていた(いま、崩れ始めた): 今(ここから数え直す)
//
// 最後の規則は、安定して動いている間に、再観測のイベントが無く、不健全の印が古いまま残っていても、
// 一度のクラッシュで、すぐ「長く不健全」とみなさないためにある。
//
// 引数:
//   - previous: DB にある前の状態。無ければ nil。
//   - next: 観測した今の状態。
//
// 戻り値:
//   - 不健全になった時刻。安定していれば nil。
func (next DeployPod) NextUnhealthySince(previous *DeployPod) *time.Time {
	// 今、安定していれば、不健全ではない。
	if next.IsStable(next.ObservedAt) {
		return nil
	}
	// 前の状態が無い、または、前は不健全ではなかったときは、今から数える。
	if previous == nil || previous.UnhealthySince == nil {
		since := next.ObservedAt
		return &since
	}
	// 前の状態が、今の時刻まで安定して動いていたなら(いま崩れ始めた)、今から数え直す。
	if previous.IsStable(next.ObservedAt) {
		since := next.ObservedAt
		return &since
	}
	// それ以外は、前から続いている不健全なので、時刻はそのまま。
	return previous.UnhealthySince
}

// ReadyPodCount は、deploy が持つ Pod のうち、Ready のものの数を返す(Pods を読み込んであるとき)。
//
// 戻り値:
//   - Ready の Pod の数。
func (deploy Deploy) ReadyPodCount() int {
	// Ready の Pod を数える。
	count := 0
	for _, pod := range deploy.Pods {
		if pod.Ready {
			count++
		}
	}
	return count
}

// UnhealthyPods は、時刻 at の時点で、安定して動いていない Pod を返す(Pods を読み込んであるとき)。
//
// 引数:
//   - at: 判定する時刻。
//
// 戻り値:
//   - 安定していない Pod の一覧。
func (deploy Deploy) UnhealthyPods(at time.Time) []DeployPod {
	// 安定していないものを集める。
	unhealthy := []DeployPod{}
	for _, pod := range deploy.Pods {
		if !pod.IsStable(at) {
			unhealthy = append(unhealthy, pod)
		}
	}
	return unhealthy
}
