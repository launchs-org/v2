package model

import (
	"strings"
	"testing"

	"launchs/shared/apperror"
)

// intPointer は、整数へのポインタを作る。
//
// 引数:
//   - value: 整数。
//
// 戻り値:
//   - ポインタ。
func intPointer(value int) *int {
	// 引数のコピーのアドレスを返す。
	return &value
}

// TestQuotaLimitsFor は、上書きがある項目だけ既定を置き換えること(0 の上書きも有効)と、
// スペックごとの上限が、スペックの既定に、ユーザーの上書きを重ねたものになることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaLimitsFor(testingT *testing.T) {
	// 既定に、2項目だけの上書きと、large だけの上書きを重ねる。
	defaults := QuotaDefaults{MaxProjects: 10, MaxDeploys: 20, MaxVolumes: 10, MaxVolumeTotalMB: 20480}
	specs := []DeploySpec{{Name: "small", DefaultMaxInstances: 20}, {Name: "large", DefaultMaxInstances: 5}}
	limits := defaults.LimitsFor(AccountQuota{MaxProjects: intPointer(0), MaxDeploys: intPointer(99)}, specs, []AccountSpecQuota{{Spec: "large", MaxInstances: 1}})
	// 上書きした項目だけが変わる。
	if limits.MaxProjects != 0 || limits.MaxDeploys != 99 || limits.MaxVolumes != 10 || limits.MaxVolumeTotalMB != 20480 {
		testingT.Fatalf("上書きした項目だけ変わるはず: %+v", limits)
	}
	// スペックごとの上限は、large だけが上書きされる(small は、スペックの既定)。
	if limits.MaxInstancesBySpec["small"] != 20 || limits.MaxInstancesBySpec["large"] != 1 || len(limits.MaxInstancesBySpec) != 2 {
		testingT.Fatalf("large だけ上書きされるはず: %v", limits.MaxInstancesBySpec)
	}
}

// TestQuotaChecks は、各判定が、上限ちょうどは通し、超えるときは項目名と数を含む conflict にすることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaChecks(testingT *testing.T) {
	// 上限(small は 3 個まで、large は 1 個まで)を用意する。
	limits := QuotaLimits{MaxProjects: 2, MaxDeploys: 2, MaxVolumes: 2, MaxVolumeTotalMB: 100, MaxInstancesBySpec: map[string]int{"small": 3, "large": 1}}
	// ケースごとに、判定の結果と、メッセージに含まれる文字列(通るなら空)を並べる。
	testCases := []struct {
		name  string
		check error
		piece string
	}{
		{"プロジェクト: 上限未満", limits.CheckProjectCreate(QuotaUsage{Projects: 1}), ""},
		{"プロジェクト: 上限に達している", limits.CheckProjectCreate(QuotaUsage{Projects: 2}), "プロジェクトの数が上限(2)に達しています(使用中 2)"},
		{"deploy: ちょうど上限に収まる", limits.CheckDeployCreate(QuotaUsage{Deploys: 1, InstancesBySpec: map[string]int{"small": 2}}, "small", 1), ""},
		{"deploy: 数が上限", limits.CheckDeployCreate(QuotaUsage{Deploys: 2}, "small", 1), "deploy の数が上限(2)"},
		{"deploy: スペックの数が超える", limits.CheckDeployCreate(QuotaUsage{Deploys: 1, InstancesBySpec: map[string]int{"large": 1}}, "large", 1), "large のインスタンスの合計が上限(1個)を超えます(使用中 1、追加 1)"},
		{"deploy: 他のスペックの使用量は影響しない", limits.CheckDeployCreate(QuotaUsage{Deploys: 1, InstancesBySpec: map[string]int{"large": 1}}, "small", 1), ""},
		{"deploy: レプリカ数の分だけ増える", limits.CheckDeployCreate(QuotaUsage{}, "small", 4), "small のインスタンスの合計"},
		{"deploy: 上限が無いスペックは 0 個", limits.CheckDeployCreate(QuotaUsage{}, "huge", 1), "huge のインスタンスの合計が上限(0個)"},
		{"deploy: レプリカ 0 なら、上限 0 でも作れる", limits.CheckDeployCreate(QuotaUsage{}, "huge", 0), ""},
		{"スケール: 収まる", limits.CheckScaleUp(QuotaUsage{InstancesBySpec: map[string]int{"small": 2}}, "small", 1), ""},
		{"スケール: 超える", limits.CheckScaleUp(QuotaUsage{InstancesBySpec: map[string]int{"small": 2}}, "small", 2), "small のインスタンスの合計"},
		{"volume: 収まる", limits.CheckVolumeCreate(QuotaUsage{Volumes: 1, VolumeTotalMB: 60}, 40), ""},
		{"volume: 数が上限", limits.CheckVolumeCreate(QuotaUsage{Volumes: 2}, 1), "volume の数が上限(2)"},
		{"volume: 大きさが超える", limits.CheckVolumeCreate(QuotaUsage{Volumes: 1, VolumeTotalMB: 60}, 41), "volume の大きさの合計が上限(100MB)を超えます(使用中 60、追加 41)"},
	}
	// 全てのケースを確かめる。
	for _, testCase := range testCases {
		if testCase.piece == "" {
			if testCase.check != nil {
				testingT.Errorf("%s: 通るはず: %v", testCase.name, testCase.check)
			}
			continue
		}
		if apperror.KindOf(testCase.check) != apperror.KindConflict || !strings.Contains(testCase.check.Error(), testCase.piece) {
			testingT.Errorf("%s: %q を含む conflict のはず: %v", testCase.name, testCase.piece, testCase.check)
		}
	}
}

// TestVolumeReservedMB は、クォータで数える大きさが、現在の大きさと pending の大きい方になることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestVolumeReservedMB(testingT *testing.T) {
	// pending が無い、大きい、現在より小さい(通常は無い)の3つを確かめる。
	if reserved := (&Volume{SizeMB: 60}).ReservedMB(); reserved != 60 {
		testingT.Errorf("pending が無ければ現在の大きさのはず: %d", reserved)
	}
	if reserved := (&Volume{SizeMB: 60, PendingSizeMB: intPointer(90)}).ReservedMB(); reserved != 90 {
		testingT.Errorf("pending が大きければ pending のはず: %d", reserved)
	}
	if reserved := (&Volume{SizeMB: 60, PendingSizeMB: intPointer(50)}).ReservedMB(); reserved != 60 {
		testingT.Errorf("pending が小さければ現在の大きさのはず: %d", reserved)
	}
}

// TestQuotaUsageAdd は、deploy と volume を足して、数・スペックごとのインスタンス数・大きさ(予約)が求まることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaUsageAdd(testingT *testing.T) {
	// 2レプリカの small、1レプリカの small、0レプリカの large、60MB(pending 90)と 10MB の volume を足す。
	var usage QuotaUsage
	usage.AddDeploy("small", 2)
	usage.AddDeploy("small", 1)
	usage.AddDeploy("large", 0)
	usage.AddVolume(&Volume{SizeMB: 60, PendingSizeMB: intPointer(90)})
	usage.AddVolume(&Volume{SizeMB: 10})
	// 数と合計が、期待どおりになる(large は、レプリカ 0 なので、0 個)。
	if usage.Deploys != 3 || usage.InstancesBySpec["small"] != 3 || usage.InstancesBySpec["large"] != 0 || usage.Volumes != 2 || usage.VolumeTotalMB != 100 {
		testingT.Fatalf("使用量が違う: %+v", usage)
	}
}
