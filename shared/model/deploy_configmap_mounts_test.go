package model

import (
	"strings"
	"testing"

	"launchs/shared/apperror"
)

// TestValidateConfigMapMounts は、ConfigMap のマウント構成の形の検査(個数、ID、key、path、mode、重複)を確認する。
func TestValidateConfigMapMounts(testingT *testing.T) {
	validID := ConfigMapIDPrefix + "00000000-0000-4000-8000-000000000001"
	mount := func(path string) ConfigMapMountSpec {
		return ConfigMapMountSpec{ConfigMapID: validID, Key: "app.conf", Path: path}
	}
	tooMany := []ConfigMapMountSpec{}
	for index := 0; index < MaxConfigMapMounts+1; index++ {
		tooMany = append(tooMany, mount("/etc/f"+itoa(index)))
	}
	for _, testCase := range []struct {
		name      string
		mounts    []ConfigMapMountSpec
		wantError bool
	}{
		{"空は、全て外す変更として許す", []ConfigMapMountSpec{}, false},
		{"1件は許す", []ConfigMapMountSpec{mount("/etc/app.conf")}, false},
		{"30件は許す", tooMany[:MaxConfigMapMounts], false},
		{"31件は不可", tooMany, true},
		{"ID の接頭辞が違うと不可", []ConfigMapMountSpec{{ConfigMapID: "volume-1", Key: "k", Path: "/p"}}, true},
		{"ID が接頭辞だけだと不可", []ConfigMapMountSpec{{ConfigMapID: ConfigMapIDPrefix, Key: "k", Path: "/p"}}, true},
		{"key が不正(/)なら不可", []ConfigMapMountSpec{{ConfigMapID: validID, Key: "a/b", Path: "/p"}}, true},
		{"key が .. なら不可", []ConfigMapMountSpec{{ConfigMapID: validID, Key: "..", Path: "/p"}}, true},
		{"revision が負なら不可", []ConfigMapMountSpec{{ConfigMapID: validID, Key: "k", Path: "/p", Revision: -1}}, true},
		{"path が相対パスなら不可", []ConfigMapMountSpec{mount("etc/a")}, true},
		{"path に .. があると不可", []ConfigMapMountSpec{mount("/etc/../a")}, true},
		{"path が / だけなら不可", []ConfigMapMountSpec{mount("/")}, true},
		{"path の末尾が / なら不可", []ConfigMapMountSpec{mount("/etc/")}, true},
		{"path が 201 文字なら不可", []ConfigMapMountSpec{mount("/" + strings.Repeat("a", 200))}, true},
		{"path が重複すると不可", []ConfigMapMountSpec{mount("/p"), mount("/p")}, true},
		{"同じファイルを別の path に置くのは許す", []ConfigMapMountSpec{mount("/a"), mount("/b")}, false},
		{"mode が 0999 なら不可", []ConfigMapMountSpec{{ConfigMapID: validID, Key: "k", Path: "/p", Mode: "0999"}}, true},
		{"mode が 3桁なら不可", []ConfigMapMountSpec{{ConfigMapID: validID, Key: "k", Path: "/p", Mode: "644"}}, true},
		{"mode が 0777 なら許す", []ConfigMapMountSpec{{ConfigMapID: validID, Key: "k", Path: "/p", Mode: "0777"}}, false},
		{"mode が 0000 なら許す", []ConfigMapMountSpec{{ConfigMapID: validID, Key: "k", Path: "/p", Mode: "0000"}}, false},
	} {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			_, validateError := ValidateConfigMapMounts(testCase.mounts)
			if testCase.wantError && (validateError == nil || apperror.KindOf(validateError) != apperror.KindInvalidRequest) {
				subTest.Errorf("invalid_request のはず: %v", validateError)
			}
			if !testCase.wantError && validateError != nil {
				subTest.Errorf("許されるはず: %v", validateError)
			}
		})
	}
	testingT.Run("mode の省略は 0644 になり、引数は変わらない", func(subTest *testing.T) {
		input := []ConfigMapMountSpec{mount("/p")}
		validated, validateError := ValidateConfigMapMounts(input)
		if validateError != nil || validated[0].Mode != "0644" || input[0].Mode != "" || validated[0].ModeBits() != 0o644 {
			subTest.Errorf("0644 になり、引数は変わらないはず: %+v %+v %v", validated, input, validateError)
		}
	})
}

// TestValidateConfigMapMountPaths は、ファイルとボリュームのマウント先との重なり、数の合計の上限を確認する。
func TestValidateConfigMapMountPaths(testingT *testing.T) {
	configMapMounts := []ConfigMapMountSpec{{Path: "/etc/app.conf"}}
	testingT.Run("まったく同じ path のファイルとは重なる", func(subTest *testing.T) {
		if ValidateConfigMapMountPaths(configMapMounts, []FileSpec{{Path: "/etc/app.conf"}}, nil) == nil {
			subTest.Error("エラーのはず")
		}
	})
	testingT.Run("ボリュームのマウント先とは重なる", func(subTest *testing.T) {
		if ValidateConfigMapMountPaths(configMapMounts, nil, []MountSpec{{MountPath: "/etc/app.conf"}}) == nil {
			subTest.Error("エラーのはず")
		}
	})
	testingT.Run("マウント先の下は許す", func(subTest *testing.T) {
		if validateError := ValidateConfigMapMountPaths([]ConfigMapMountSpec{{Path: "/data/app.conf"}}, nil, []MountSpec{{MountPath: "/data"}}); validateError != nil {
			subTest.Errorf("許すはず: %v", validateError)
		}
	})
	testingT.Run("ConfigMap のマウントと files の合計が 40 個を超えると不可、40 個なら許す", func(subTest *testing.T) {
		files := make([]FileSpec, 0, 11)
		for index := 0; index < 11; index++ {
			files = append(files, FileSpec{Path: "/f/" + itoa(index)})
		}
		mounts := make([]ConfigMapMountSpec, 0, 30)
		for index := 0; index < 30; index++ {
			mounts = append(mounts, ConfigMapMountSpec{Path: "/m/" + itoa(index)})
		}
		if ValidateConfigMapMountPaths(mounts, files, nil) == nil || ValidateConfigMapMountPaths(mounts, files[:10], nil) != nil {
			subTest.Error("41 個は不可、40 個は許すはず")
		}
	})
}

// TestConfigMapMountsTransitions は、反映の開始と確定の、状態の規則を確認する。
func TestConfigMapMountsTransitions(testingT *testing.T) {
	testingT.Run("pending が無いと開始できない(conflict)", func(subTest *testing.T) {
		deploy := Deploy{Status: DeployStatusActive}
		if _, beginError := deploy.BeginConfigMapMountsApply(); beginError == nil || apperror.KindOf(beginError) != apperror.KindConflict {
			subTest.Errorf("conflict のはず: %v", beginError)
		}
	})
	testingT.Run("pending があれば applying になり、元の状態を返す。反映中は開始できない", func(subTest *testing.T) {
		deploy := Deploy{Status: DeployStatusActive, PendingConfigMapMounts: []byte(`[]`)}
		previous, beginError := deploy.BeginConfigMapMountsApply()
		if beginError != nil || previous != DeployStatusActive || deploy.Status != DeployStatusApplying {
			subTest.Fatalf("applying になるはず: %v %s %s", beginError, previous, deploy.Status)
		}
		if _, secondError := deploy.BeginConfigMapMountsApply(); secondError == nil {
			subTest.Error("反映中は開始できないはず")
		}
	})
	testingT.Run("成功なら pending がマウントになり、失敗なら変わらず failed", func(subTest *testing.T) {
		succeeded := Deploy{Status: DeployStatusApplying, Replicas: 1, ConfigMapMounts: []byte(`[]`), PendingConfigMapMounts: []byte(`[{"key":"k"}]`)}
		if !succeeded.CommitConfigMapMounts(true) || string(succeeded.ConfigMapMounts) != `[{"key":"k"}]` || succeeded.PendingConfigMapMounts != nil || succeeded.Status != DeployStatusActive {
			subTest.Errorf("昇格して active のはず: %+v", succeeded)
		}
		failed := Deploy{Status: DeployStatusApplying, ConfigMapMounts: []byte(`[]`), PendingConfigMapMounts: []byte(`[{"key":"k"}]`)}
		if !failed.CommitConfigMapMounts(false) || string(failed.ConfigMapMounts) != `[]` || failed.PendingConfigMapMounts == nil || failed.Status != DeployStatusFailed {
			subTest.Errorf("変わらず failed のはず: %+v", failed)
		}
		idle := Deploy{Status: DeployStatusActive}
		if idle.CommitConfigMapMounts(true) {
			subTest.Error("applying でなければ、確定しないはず")
		}
	})
}
