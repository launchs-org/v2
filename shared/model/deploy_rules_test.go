package model

import "testing"

// TestDeployRules は、deploy の状態ごとに、操作の可否の規則が仕様どおりであることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestDeployRules(testing *testing.T) {
	// 状態ごとの期待値の表。
	cases := []struct {
		status          string
		canEdit         bool
		canStartApply   bool
		canDelete       bool
		canAttach       bool
		canStopForCrash bool
	}{
		{DeployStatusPending, true, false, false, true, false},
		{DeployStatusActive, true, true, true, true, true},
		{DeployStatusApplying, false, false, false, true, false},
		{DeployStatusFailed, true, true, true, true, false},
		{DeployStatusScaledZero, true, true, true, true, false},
		{DeployStatusDeleting, false, false, false, false, false},
		{DeployStatusDeleteFailed, false, false, true, false, false},
	}
	// 全ての状態を確かめる。
	for _, testCase := range cases {
		// 確かめる deploy を作る。
		deploy := Deploy{Status: testCase.status}
		// 設定を操作できるか。
		if deploy.CanEditSettings() != testCase.canEdit {
			testing.Errorf("%s: CanEditSettings が違う", testCase.status)
		}
		// 反映を始められるか。
		if deploy.CanStartApply() != testCase.canStartApply {
			testing.Errorf("%s: CanStartApply が違う", testCase.status)
		}
		// 削除を受け付けられるか。
		if deploy.CanDelete() != testCase.canDelete {
			testing.Errorf("%s: CanDelete が違う", testCase.status)
		}
		// service を作れるか。
		if deploy.CanAttachService() != testCase.canAttach {
			testing.Errorf("%s: CanAttachService が違う", testCase.status)
		}
		// crash-loop で止められるか。
		if deploy.CanStopForCrashLoop() != testCase.canStopForCrash {
			testing.Errorf("%s: CanStopForCrashLoop が違う", testCase.status)
		}
	}
}

// TestServiceRules は、service の状態ごとに、操作の可否の規則が仕様どおりであることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestServiceRules(testing *testing.T) {
	// 状態ごとの期待値の表。
	cases := []struct {
		status         string
		canChangePorts bool
		canDelete      bool
	}{
		{ServiceStatusPending, false, false},
		{ServiceStatusActive, true, true},
		{ServiceStatusFailed, false, true},
		{ServiceStatusDeleting, false, false},
	}
	// 全ての状態を確かめる。
	for _, testCase := range cases {
		// 確かめる service を作る。
		service := Service{Status: testCase.status}
		// ポートを変えられるか。
		if service.CanChangePorts() != testCase.canChangePorts {
			testing.Errorf("%s: CanChangePorts が違う", testCase.status)
		}
		// 削除を受け付けられるか。
		if service.CanDelete() != testCase.canDelete {
			testing.Errorf("%s: CanDelete が違う", testCase.status)
		}
	}
}
