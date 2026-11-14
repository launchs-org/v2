package repository

import (
	"testing"
	"time"

	"launchs/shared/model"
)

// TestIsPodStable は、Ready のまま、コンテナが十分長く動き続けているときだけ、安定とみなすことを確認する。
func TestIsPodStable(testingT *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	startedAgo := func(duration time.Duration) *time.Time {
		startedAt := now.Add(-duration)
		return &startedAt
	}
	testCases := []struct {
		name     string
		pod      model.DeployPod
		expected bool
	}{
		{"Ready で、5分以上動いていれば安定", model.DeployPod{Ready: true, StartedAt: startedAgo(5 * time.Minute)}, true},
		{"Ready でも、動き始めて4分59秒なら、まだ安定ではない", model.DeployPod{Ready: true, StartedAt: startedAgo(5*time.Minute - time.Second)}, false},
		{"長く動いていても、Ready でなければ安定ではない", model.DeployPod{Ready: false, StartedAt: startedAgo(time.Hour)}, false},
		{"コンテナがまだ起動していなければ安定ではない", model.DeployPod{Ready: true}, false},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			if got := IsPodStable(testCase.pod, now); got != testCase.expected {
				subTest.Errorf("期待 %v、実際 %v", testCase.expected, got)
			}
		})
	}
}

// TestNextUnhealthySince は、不健全になった時刻を、「安定すれば消える」「前から続いていれば保つ」
// 「前が安定していて、いま崩れたなら、今から数え直す」の規則で決めることを確認する。
func TestNextUnhealthySince(testingT *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	ago := func(duration time.Duration) *time.Time {
		moment := now.Add(-duration)
		return &moment
	}
	stableNow := model.DeployPod{Ready: true, StartedAt: ago(time.Hour), ObservedAt: now}
	crashing := model.DeployPod{Ready: false, StartedAt: ago(time.Second), ObservedAt: now}

	testingT.Run("いま安定していれば、nil(不健全ではない)", func(subTest *testing.T) {
		previous := model.DeployPod{UnhealthySince: ago(time.Hour)}
		if got := NextUnhealthySince(&previous, stableNow); got != nil {
			subTest.Errorf("nil のはず: %v", got)
		}
	})
	testingT.Run("前の状態が無ければ、今(初めて観測した時刻)から数える", func(subTest *testing.T) {
		if got := NextUnhealthySince(nil, crashing); got == nil || !got.Equal(now) {
			subTest.Errorf("今のはず: %v", got)
		}
	})
	testingT.Run("前は不健全ではなかった(nil)なら、今から数える", func(subTest *testing.T) {
		previous := model.DeployPod{Ready: true, StartedAt: ago(time.Minute), ObservedAt: now.Add(-time.Minute)}
		if got := NextUnhealthySince(&previous, crashing); got == nil || !got.Equal(now) {
			subTest.Errorf("今のはず: %v", got)
		}
	})
	testingT.Run("前から不健全で、その間ずっと安定していなければ、前の時刻のまま", func(subTest *testing.T) {
		since := now.Add(-20 * time.Minute)
		previous := model.DeployPod{Ready: false, UnhealthySince: &since, ObservedAt: now.Add(-time.Minute)}
		if got := NextUnhealthySince(&previous, crashing); got == nil || !got.Equal(since) {
			subTest.Errorf("前の時刻のはず: %v", got)
		}
	})
	testingT.Run("前の状態が、いまの時刻まで安定して動いていたなら(不健全の印が古いまま残っていても)、今から数え直す", func(subTest *testing.T) {
		staleSince := now.Add(-3 * time.Hour)
		previous := model.DeployPod{Ready: true, StartedAt: ago(2 * time.Hour), UnhealthySince: &staleSince, ObservedAt: now.Add(-2 * time.Hour)}
		if got := NextUnhealthySince(&previous, crashing); got == nil || !got.Equal(now) {
			subTest.Errorf("今から数え直すはず(3時間前のままにしない): %v", got)
		}
	})
	testingT.Run("一瞬 Ready になっただけの前の状態は、安定とはみなさず、時刻を保つ", func(subTest *testing.T) {
		since := now.Add(-40 * time.Minute)
		previous := model.DeployPod{Ready: true, StartedAt: ago(10 * time.Second), UnhealthySince: &since, ObservedAt: now.Add(-10 * time.Second)}
		if got := NextUnhealthySince(&previous, crashing); got == nil || !got.Equal(since) {
			subTest.Errorf("前の時刻のままのはず: %v", got)
		}
	})
}
