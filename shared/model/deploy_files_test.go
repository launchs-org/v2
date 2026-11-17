package model

import (
	"strings"
	"testing"

	"launchs/shared/apperror"
)

// TestValidateFiles は、ファイルの構成の検査(個数、path の書式、内容、重複、マウント先との重なり)を確認する。
func TestValidateFiles(testingT *testing.T) {
	// 検査する構成と、期待(通る/拒否)の表。
	testCases := []struct {
		name    string
		files   []FileSpec
		mounts  []MountSpec
		allowed bool
	}{
		{"空は、全て外す変更として、通る", []FileSpec{}, nil, true},
		{"普通のファイル", []FileSpec{{Path: "/etc/app/app.conf", Content: "key=value\n"}}, nil, true},
		{"ボリュームのマウント先の下には、置ける", []FileSpec{{Path: "/data/init.sql", Content: "SELECT 1;"}}, []MountSpec{{VolumeID: "volume-a", MountPath: "/data"}}, true},
		{"内容が空でもよい", []FileSpec{{Path: "/etc/empty", Content: ""}}, nil, true},
		{"日本語の内容", []FileSpec{{Path: "/etc/msg.txt", Content: "こんにちは"}}, nil, true},
		{"/ で始まらない", []FileSpec{{Path: "etc/app.conf"}}, nil, false},
		{"/ だけ", []FileSpec{{Path: "/"}}, nil, false},
		{".. を含む", []FileSpec{{Path: "/etc/../passwd"}}, nil, false},
		{"// を含む", []FileSpec{{Path: "/etc//app.conf"}}, nil, false},
		{"末尾が /", []FileSpec{{Path: "/etc/"}}, nil, false},
		{"空白を含む", []FileSpec{{Path: "/etc/my app.conf"}}, nil, false},
		{"日本語の path", []FileSpec{{Path: "/etc/設定"}}, nil, false},
		{"./ を含む(正規の形でない)", []FileSpec{{Path: "/etc/./app.conf"}}, nil, false},
		{"path が重複", []FileSpec{{Path: "/a"}, {Path: "/a"}}, nil, false},
		{"path が長すぎる(201文字)", []FileSpec{{Path: "/" + strings.Repeat("a", 200)}}, nil, false},
		{"path が200文字ちょうど", []FileSpec{{Path: "/" + strings.Repeat("a", 199)}}, nil, true},
		{"マウント先と、まったく同じ", []FileSpec{{Path: "/data"}}, []MountSpec{{VolumeID: "volume-a", MountPath: "/data"}}, false},
		{"UTF-8 でない内容", []FileSpec{{Path: "/a", Content: string([]byte{0xff, 0xfe})}}, nil, false},
		{"NUL 文字を含む内容", []FileSpec{{Path: "/a", Content: "a\x00b"}}, nil, false},
		{"内容が64KiBちょうど", []FileSpec{{Path: "/a", Content: strings.Repeat("x", MaxFileContentBytes)}}, nil, true},
		{"内容が64KiBを超える", []FileSpec{{Path: "/a", Content: strings.Repeat("x", MaxFileContentBytes+1)}}, nil, false},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			validationError := ValidateFiles(testCase.files, testCase.mounts)
			if testCase.allowed && validationError != nil {
				subTest.Errorf("通るはず: %v", validationError)
			}
			if !testCase.allowed && apperror.KindOf(validationError) != apperror.KindInvalidRequest {
				subTest.Errorf("invalid_request のはず: %v", validationError)
			}
		})
	}
	testingT.Run("個数は20個まで(21個は拒否)", func(subTest *testing.T) {
		build := func(count int) []FileSpec {
			files := make([]FileSpec, 0, count)
			for index := 0; index < count; index++ {
				files = append(files, FileSpec{Path: "/f" + itoa(index)})
			}
			return files
		}
		if validationError := ValidateFiles(build(MaxFilesPerDeploy), nil); validationError != nil {
			subTest.Errorf("20個は通るはず: %v", validationError)
		}
		if apperror.KindOf(ValidateFiles(build(MaxFilesPerDeploy+1), nil)) != apperror.KindInvalidRequest {
			subTest.Error("21個は拒否されるはず")
		}
	})
	testingT.Run("合計は256KiBまで", func(subTest *testing.T) {
		// 64KiB を 4 つで、ちょうど 256KiB(通る)。もう 1 つ足すと、超える(拒否)。
		four := []FileSpec{{Path: "/a", Content: strings.Repeat("x", MaxFileContentBytes)}, {Path: "/b", Content: strings.Repeat("x", MaxFileContentBytes)}, {Path: "/c", Content: strings.Repeat("x", MaxFileContentBytes)}, {Path: "/d", Content: strings.Repeat("x", MaxFileContentBytes)}}
		if validationError := ValidateFiles(four, nil); validationError != nil {
			subTest.Errorf("256KiB ちょうどは通るはず: %v", validationError)
		}
		if apperror.KindOf(ValidateFiles(append(four, FileSpec{Path: "/e", Content: "x"}), nil)) != apperror.KindInvalidRequest {
			subTest.Error("256KiB を超えたら拒否されるはず")
		}
	})
}

// TestDeployFilesState は、ファイルの反映の開始と確定(成功・失敗)が、マウントと同じ規則で動くことを確認する。
func TestDeployFilesState(testingT *testing.T) {
	testingT.Run("pending が無いと、反映を始められない(conflict)", func(subTest *testing.T) {
		deploy := Deploy{Status: DeployStatusActive}
		if _, beginError := deploy.BeginFilesApply(); apperror.KindOf(beginError) != apperror.KindConflict {
			subTest.Errorf("conflict のはず: %v", beginError)
		}
	})
	testingT.Run("空の配列の pending は、「全て外す」変更として、反映できる", func(subTest *testing.T) {
		deploy := Deploy{Status: DeployStatusActive, PendingFiles: []byte("[]")}
		previous, beginError := deploy.BeginFilesApply()
		if beginError != nil || previous != DeployStatusActive || deploy.Status != DeployStatusApplying {
			subTest.Errorf("反映中になるはず: %q %v %s", previous, beginError, deploy.Status)
		}
	})
	testingT.Run("反映中や削除中は、操作できない", func(subTest *testing.T) {
		for _, status := range []string{DeployStatusApplying, DeployStatusDeleting} {
			deploy := Deploy{Status: status, PendingFiles: []byte(`[{"path":"/a","content":""}]`)}
			if _, beginError := deploy.BeginFilesApply(); apperror.KindOf(beginError) != apperror.KindConflict {
				subTest.Errorf("%s は conflict のはず: %v", status, beginError)
			}
		}
	})
	testingT.Run("成功すると、files が pending の内容になり、pending は空になる", func(subTest *testing.T) {
		deploy := Deploy{Status: DeployStatusActive, Replicas: 1, Files: []byte(`[{"path":"/old","content":""}]`), PendingFiles: []byte(`[{"path":"/new","content":"x"}]`)}
		if _, beginError := deploy.BeginFilesApply(); beginError != nil {
			subTest.Fatal(beginError)
		}
		if !deploy.CommitFiles(true) || string(deploy.Files) != `[{"path":"/new","content":"x"}]` || deploy.PendingFiles != nil || deploy.Status != DeployStatusActive {
			subTest.Errorf("確定されるはず: %s %v %s", deploy.Files, deploy.PendingFiles, deploy.Status)
		}
	})
	testingT.Run("失敗すると、files と pending は、そのまま。状態は failed", func(subTest *testing.T) {
		deploy := Deploy{Status: DeployStatusActive, Replicas: 1, Files: []byte(`[]`), PendingFiles: []byte(`[{"path":"/new","content":""}]`)}
		if _, beginError := deploy.BeginFilesApply(); beginError != nil {
			subTest.Fatal(beginError)
		}
		if !deploy.CommitFiles(false) || deploy.Status != DeployStatusFailed || deploy.PendingFiles == nil {
			subTest.Errorf("failed で、pending が残るはず: %s %v", deploy.Status, deploy.PendingFiles)
		}
	})
	testingT.Run("反映中でなければ、確定しない", func(subTest *testing.T) {
		deploy := Deploy{Status: DeployStatusActive, PendingFiles: []byte(`[]`)}
		if deploy.CommitFiles(true) {
			subTest.Error("何も確定しないはず")
		}
	})
}
