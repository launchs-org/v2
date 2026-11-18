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
	specs := []DeploySpec{{Name: "small", DefaultMaxDeploys: 20}, {Name: "large", DefaultMaxDeploys: 5}}
	limits := defaults.LimitsFor(AccountQuota{MaxProjects: intPointer(0), MaxDeploys: intPointer(99)}, specs, []AccountSpecQuota{{Spec: "large", MaxDeploys: 1}})
	// 上書きした項目だけが変わる。
	if limits.MaxProjects != 0 || limits.MaxDeploys != 99 || limits.MaxVolumes != 10 || limits.MaxVolumeTotalMB != 20480 {
		testingT.Fatalf("上書きした項目だけ変わるはず: %+v", limits)
	}
	// スペックごとの上限は、large だけが上書きされる(small は、スペックの既定)。
	if limits.MaxDeploysBySpec["small"] != 20 || limits.MaxDeploysBySpec["large"] != 1 || len(limits.MaxDeploysBySpec) != 2 {
		testingT.Fatalf("large だけ上書きされるはず: %v", limits.MaxDeploysBySpec)
	}
}

// TestQuotaChecks は、各判定が、上限ちょうどは通し、超えるときは項目名と数を含む conflict にすることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaChecks(testingT *testing.T) {
	// 上限(deploy は合計 4 個、small は 3 個まで、large は 1 個まで)を用意する。
	limits := QuotaLimits{MaxProjects: 2, MaxDeploys: 4, MaxVolumes: 2, MaxVolumeTotalMB: 100, MaxDeploysBySpec: map[string]int{"small": 3, "large": 1}}
	// ケースごとに、判定の結果と、メッセージに含まれる文字列(通るなら空)を並べる。
	testCases := []struct {
		name  string
		check error
		piece string
	}{
		{"プロジェクト: 上限未満", limits.CheckProjectCreate(QuotaUsage{Projects: 1}), ""},
		{"プロジェクト: 上限に達している", limits.CheckProjectCreate(QuotaUsage{Projects: 2}), "プロジェクトの数が上限(2)に達しています(使用中 2)"},
		{"deploy: 最後の1つは作れる", limits.CheckDeployCreate(QuotaUsage{Deploys: 3, DeploysBySpec: map[string]int{"small": 2}}, "small"), ""},
		{"deploy: 合計の数が上限", limits.CheckDeployCreate(QuotaUsage{Deploys: 4, DeploysBySpec: map[string]int{"small": 1}}, "small"), "deploy の数が上限(4)に達しています(使用中 4)"},
		{"deploy: スペックの数が上限", limits.CheckDeployCreate(QuotaUsage{Deploys: 1, DeploysBySpec: map[string]int{"large": 1}}, "large"), "large の deploy の数が上限(1)に達しています(使用中 1)"},
		{"deploy: 他のスペックの使用量は影響しない", limits.CheckDeployCreate(QuotaUsage{Deploys: 1, DeploysBySpec: map[string]int{"large": 1}}, "small"), ""},
		{"deploy: 上限が無いスペックは 0 個", limits.CheckDeployCreate(QuotaUsage{}, "huge"), "huge の deploy の数が上限(0)"},
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

// TestQuotaUsageAdd は、deploy と volume を足して、数・スペックごとの deploy の数・大きさ(予約)が求まることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaUsageAdd(testingT *testing.T) {
	// small を2つ、large を1つ、60MB(pending 90)と 10MB の volume を足す。
	var usage QuotaUsage
	usage.AddDeploy("small")
	usage.AddDeploy("small")
	usage.AddDeploy("large")
	usage.AddVolume(&Volume{SizeMB: 60, PendingSizeMB: intPointer(90)})
	usage.AddVolume(&Volume{SizeMB: 10})
	// 数と合計が、期待どおりになる。
	if usage.Deploys != 3 || usage.DeploysBySpec["small"] != 2 || usage.DeploysBySpec["large"] != 1 || usage.Volumes != 2 || usage.VolumeTotalMB != 100 {
		testingT.Fatalf("使用量が違う: %+v", usage)
	}
}

// TestQuotaOverridesValidate は、上書きの値の範囲(0〜MaxQuotaValue)と、存在しないスペックの拒否を、項目名つきのエラーで確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaOverridesValidate(testingT *testing.T) {
	// 存在するスペックを用意し、各ケースの上書きと、エラーに含まれるはずの文字列(通るなら空)を並べる。
	known := []string{"small", "large"}
	testCases := []struct {
		name      string
		overrides QuotaOverrides
		piece     string
	}{
		{"空は通る", QuotaOverrides{}, ""},
		{"端の値は通る", QuotaOverrides{MaxProjects: intPointer(0), MaxVolumeTotalMB: intPointer(MaxQuotaValue), MaxDeploysBySpec: map[string]int{"large": 0}}, ""},
		{"projects が負", QuotaOverrides{MaxProjects: intPointer(-1)}, "projects"},
		{"deploys が上限超え", QuotaOverrides{MaxDeploys: intPointer(MaxQuotaValue + 1)}, "deploys"},
		{"volumes が負", QuotaOverrides{MaxVolumes: intPointer(-5)}, "volumes"},
		{"volumeTotalMB が上限超え", QuotaOverrides{MaxVolumeTotalMB: intPointer(MaxQuotaValue + 1)}, "volumeTotalMB"},
		{"存在しないスペック", QuotaOverrides{MaxDeploysBySpec: map[string]int{"huge": 1}}, "huge"},
		{"スペックの値が負", QuotaOverrides{MaxDeploysBySpec: map[string]int{"small": -1}}, "small"},
		{"スペックの値が上限超え", QuotaOverrides{MaxDeploysBySpec: map[string]int{"small": MaxQuotaValue + 1}}, "small"},
	}
	// 全てのケースを確かめる。
	for _, testCase := range testCases {
		validateError := testCase.overrides.Validate(known)
		if testCase.piece == "" {
			if validateError != nil {
				testingT.Errorf("%s: 通るはず: %v", testCase.name, validateError)
			}
			continue
		}
		if apperror.KindOf(validateError) != apperror.KindInvalidRequest || !strings.Contains(validateError.Error(), testCase.piece) {
			testingT.Errorf("%s: %q を含む invalid_request のはず: %v", testCase.name, testCase.piece, validateError)
		}
	}
}

// TestQuotaOverridesOf は、保存されている行から、上書き一式が作られ、AccountQuota で行に戻せることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaOverridesOf(testingT *testing.T) {
	// 行から上書きを作る。
	overrides := QuotaOverridesOf(AccountQuota{UserID: "user-a", MaxProjects: intPointer(3)}, []AccountSpecQuota{{UserID: "user-a", Spec: "large", MaxDeploys: 2}})
	if overrides.MaxProjects == nil || *overrides.MaxProjects != 3 || overrides.MaxDeploys != nil || overrides.MaxDeploysBySpec["large"] != 2 {
		testingT.Fatalf("行の内容が写るはず: %+v", overrides)
	}
	// 行に戻すと、項目が同じになる(スペックごとの上書きは、含まない)。
	if row := overrides.AccountQuota("user-b"); row.UserID != "user-b" || row.MaxProjects == nil || *row.MaxProjects != 3 || row.MaxVolumes != nil {
		testingT.Fatalf("行に戻せるはず: %+v", row)
	}
}

// TestCheckBatch は、まとめて作るものが、全体で、上限に収まるかの確認(数、スペックごと、volume の数と大きさ)を確認する。
func TestCheckBatch(testingT *testing.T) {
	limits := QuotaLimits{MaxDeploys: 5, MaxVolumes: 3, MaxVolumeTotalMB: 4096, MaxDeploysBySpec: map[string]int{"small": 3, "medium": 2}}
	usage := QuotaUsage{Deploys: 2, DeploysBySpec: map[string]int{"small": 1, "medium": 1}, Volumes: 1, VolumeTotalMB: 1024}
	for _, testCase := range []struct {
		name      string
		needs     BatchNeeds
		wantError bool
	}{
		{"収まる", BatchNeeds{DeploysBySpec: map[string]int{"small": 2, "medium": 1}, Volumes: 2, VolumeTotalMB: 3072}, false},
		{"何も作らない", BatchNeeds{}, false},
		{"deploy の合計が、ちょうど上限", BatchNeeds{DeploysBySpec: map[string]int{"small": 2, "medium": 1}}, false},
		{"deploy の合計が、上限を 1 超える", BatchNeeds{DeploysBySpec: map[string]int{"small": 2, "medium": 2}}, true},
		{"スペックごとの数が、上限を超える(合計は収まる)", BatchNeeds{DeploysBySpec: map[string]int{"medium": 2}}, true},
		{"上限のないスペックは、作れない", BatchNeeds{DeploysBySpec: map[string]int{"large": 1}}, true},
		{"volume の数が、上限を超える", BatchNeeds{Volumes: 3, VolumeTotalMB: 100}, true},
		{"volume の大きさの合計が、上限を超える", BatchNeeds{Volumes: 1, VolumeTotalMB: 3073}, true},
		{"volume の大きさが、ちょうど上限", BatchNeeds{Volumes: 1, VolumeTotalMB: 3072}, false},
	} {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			checkError := limits.CheckBatch(usage, testCase.needs)
			if testCase.wantError && (checkError == nil || apperror.KindOf(checkError) != apperror.KindConflict) {
				subTest.Errorf("conflict のはず: %v", checkError)
			}
			if !testCase.wantError && checkError != nil {
				subTest.Errorf("収まるはず: %v", checkError)
			}
		})
	}
}
