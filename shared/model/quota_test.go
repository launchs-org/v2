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

// TestQuotaLimitsFor は、上書きがある項目だけ、既定を置き換えること(0 の上書きも有効)を確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaLimitsFor(testingT *testing.T) {
	// 既定に、2項目だけの上書きを重ねる。
	defaults := QuotaDefaults{MaxProjects: 10, MaxDeploys: 20, MaxCPUMillicores: 4000, MaxMemoryMiB: 8192, MaxVolumes: 10, MaxVolumeTotalMB: 20480}
	limits := defaults.LimitsFor(AccountQuota{MaxProjects: intPointer(0), MaxDeploys: intPointer(99)})
	// 上書きした項目だけが変わる。
	expected := QuotaLimits{MaxProjects: 0, MaxDeploys: 99, MaxCPUMillicores: 4000, MaxMemoryMiB: 8192, MaxVolumes: 10, MaxVolumeTotalMB: 20480}
	if limits != expected {
		testingT.Fatalf("上書きした項目だけ変わるはず: %+v", limits)
	}
}

// TestQuotaChecks は、各判定が、上限ちょうどは通し、超えるときは項目名と数を含む conflict にすることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaChecks(testingT *testing.T) {
	// 上限と、スペック(250 ミリコア、256 MiB)を用意する。
	limits := QuotaLimits{MaxProjects: 2, MaxDeploys: 2, MaxCPUMillicores: 500, MaxMemoryMiB: 512, MaxVolumes: 2, MaxVolumeTotalMB: 100}
	spec := DeploySpec{Name: "small", CPUMillicores: 250, MemoryMiB: 256}
	// ケースごとに、判定の結果と、メッセージに含まれる文字列(通るなら空)を並べる。
	testCases := []struct {
		name  string
		check error
		piece string
	}{
		{"プロジェクト: 上限未満", limits.CheckProjectCreate(QuotaUsage{Projects: 1}), ""},
		{"プロジェクト: 上限に達している", limits.CheckProjectCreate(QuotaUsage{Projects: 2}), "プロジェクトの数が上限(2)に達しています(使用中 2)"},
		{"deploy: ちょうど上限に収まる", limits.CheckDeployCreate(QuotaUsage{Deploys: 1, CPUMillicores: 250, MemoryMiB: 256}, spec, 1), ""},
		{"deploy: 数が上限", limits.CheckDeployCreate(QuotaUsage{Deploys: 2}, spec, 1), "deploy の数が上限(2)"},
		{"deploy: CPU が超える", limits.CheckDeployCreate(QuotaUsage{Deploys: 1, CPUMillicores: 300}, spec, 1), "CPU の合計が上限(500ミリコア)を超えます(使用中 300、追加 250)"},
		{"deploy: メモリが超える", limits.CheckDeployCreate(QuotaUsage{Deploys: 1, MemoryMiB: 300}, spec, 1), "メモリの合計が上限(512MiB)を超えます(使用中 300、追加 256)"},
		{"deploy: レプリカ数の分だけ増える", limits.CheckDeployCreate(QuotaUsage{}, spec, 3), "CPU の合計"},
		{"スケール: 収まる", limits.CheckScaleUp(QuotaUsage{CPUMillicores: 250, MemoryMiB: 256}, spec, 1), ""},
		{"スケール: 超える", limits.CheckScaleUp(QuotaUsage{CPUMillicores: 250, MemoryMiB: 256}, spec, 2), "CPU の合計"},
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

// TestQuotaUsageAdd は、deploy と volume を足して、数・CPU・メモリ・大きさ(予約)が求まることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaUsageAdd(testingT *testing.T) {
	// 2レプリカの small、0レプリカの large、60MB(pending 90)と 10MB の volume を足す。
	var usage QuotaUsage
	usage.AddDeploy(DeploySpec{CPUMillicores: 250, MemoryMiB: 256}, 2)
	usage.AddDeploy(DeploySpec{CPUMillicores: 1000, MemoryMiB: 1024}, 0)
	usage.AddVolume(&Volume{SizeMB: 60, PendingSizeMB: intPointer(90)})
	usage.AddVolume(&Volume{SizeMB: 10})
	// 数と合計が、期待どおりになる。
	if expected := (QuotaUsage{Deploys: 2, CPUMillicores: 500, MemoryMiB: 512, Volumes: 2, VolumeTotalMB: 100}); usage != expected {
		testingT.Fatalf("使用量が違う: %+v", usage)
	}
}
