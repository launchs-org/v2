package repository

import (
	"gorm.io/datatypes"

	"context"
	"testing"
	"time"

	"launchs/shared/apperror"
	"launchs/shared/database/testdatabase"
	"launchs/shared/model"
)

// TestRepositoriesRejectEmptyConditions は、条件に使う値(ID やユーザー ID)が空のとき、
// 他の行を対象にせず、エラーになり、データが1行も変わらないことを確認する。
//
// Gorm の構造体による条件は、空の値を無視するので、守りが無いと、空の ID で他人の行が
// 取得されたり、削除中の全ての行が消されたりする。
func TestRepositoriesRejectEmptyConditions(testingT *testing.T) {
	// active、deleting のプロジェクトと、deploy を用意する。
	database := testdatabase.New(testingT)
	projectRepository := NewProjectRepository(database)
	deployRepository := NewDeployRepository(database)
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))
	deletingProject := newTestProject(2, "user-b", time.Now())
	deletingProject.Status = model.ProjectStatusDeleting
	mustCreate(testingT, database, deletingProject)
	if createError := deployRepository.Create(context.Background(), newTestDeploy(1, "project-00000001", "user-a")); createError != nil {
		testingT.Fatalf("deploy を作れませんでした: %v", createError)
	}
	requestContext := context.Background()

	// 空の値を渡す呼び出しと、期待するエラーの種類(空なら、エラーにならず、空の結果になる呼び出し)。
	testCases := []struct {
		name         string
		call         func() error
		expectedKind apperror.Kind
	}{
		{name: "プロジェクト取得: ユーザー ID が空", call: func() error { _, err := projectRepository.FindByID(requestContext, "", "project-00000001"); return err }, expectedKind: apperror.KindNotFound},
		{name: "プロジェクト取得: ID が空", call: func() error { _, err := projectRepository.FindByID(requestContext, "user-a", ""); return err }, expectedKind: apperror.KindNotFound},
		{name: "プロジェクト一覧: ユーザー ID が空", call: func() error { _, _, err := projectRepository.ListByUser(requestContext, "", 10, nil); return err }, expectedKind: apperror.KindInternal},
		{name: "プロジェクト名変更: ID が空", call: func() error { return projectRepository.Rename(requestContext, "user-a", "", "x") }, expectedKind: apperror.KindInternal},
		{name: "削除中にする: ID が空", call: func() error { _, err := projectRepository.MarkDeleting(requestContext, "user-a", ""); return err }, expectedKind: apperror.KindNotFound},
		{name: "削除中を戻す: ID が空", call: func() error { return projectRepository.RevertDeleting(requestContext, "user-a", "", "active") }, expectedKind: apperror.KindInternal},
		{name: "削除失敗にする: ID が空", call: func() error { return projectRepository.MarkDeleteFailed(requestContext, "") }, expectedKind: apperror.KindInternal},
		{name: "行を削除する: ID が空(削除中の全ての行を消さない)", call: func() error { return projectRepository.DeleteRecord(requestContext, "") }, expectedKind: apperror.KindInternal},
		{name: "スペック取得: 名前が空(先頭の1件を返さない)", call: func() error { _, err := deployRepository.FindSpec(requestContext, ""); return err }, expectedKind: apperror.KindInvalidRequest},
		{name: "deploy 作成: プロジェクト ID が空", call: func() error { return deployRepository.Create(requestContext, newTestDeploy(9, "", "user-a")) }, expectedKind: apperror.KindInternal},
		{name: "deploy 取得: ユーザー ID が空", call: func() error { _, err := deployRepository.FindByID(requestContext, "", "deploy-00000001"); return err }, expectedKind: apperror.KindNotFound},
		{name: "deploy 取得(ワーカー): ID が空", call: func() error { _, err := deployRepository.FindForWorker(requestContext, ""); return err }, expectedKind: apperror.KindNotFound},
		{name: "起動設定の保存: ID が空", call: func() error {
			return deployRepository.SavePendingConfig(requestContext, "user-a", "", datatypes.JSON(`{}`), nil, nil)
		}, expectedKind: apperror.KindNotFound},
		{name: "起動設定の保存: ユーザー ID が空", call: func() error {
			return deployRepository.SavePendingConfig(requestContext, "", "deploy-00000001", datatypes.JSON(`{}`), nil, nil)
		}, expectedKind: apperror.KindNotFound},
		{name: "起動設定の破棄: ID が空", call: func() error { return deployRepository.DiscardPendingConfig(requestContext, "user-a", "") }, expectedKind: apperror.KindNotFound},
		{name: "スケーリング開始: ID が空", call: func() error { _, err := deployRepository.BeginScale(requestContext, "user-a", "", 2); return err }, expectedKind: apperror.KindNotFound},
		{name: "スケーリングの巻き戻し: ID が空", call: func() error {
			return deployRepository.RevertScale(requestContext, "", ScaleStart{PreviousStatus: "active"})
		}, expectedKind: apperror.KindInternal},
		{name: "deploy 削除受付: ID が空", call: func() error { _, err := deployRepository.MarkDeleting(requestContext, "user-a", ""); return err }, expectedKind: apperror.KindNotFound},
		{name: "deploy 削除の巻き戻し: ID が空", call: func() error { return deployRepository.RevertDeleting(requestContext, "", "active") }, expectedKind: apperror.KindInternal},
		{name: "deploy 削除失敗の記録: ID が空", call: func() error { return deployRepository.MarkDeleteFailed(requestContext, "") }, expectedKind: apperror.KindInternal},
		{name: "deploy レコード削除: ID が空(削除中の全ての行を消さない)", call: func() error { return deployRepository.DeleteRecord(requestContext, "") }, expectedKind: apperror.KindInternal},
		{name: "service 作成: deploy の ID が空", call: func() error {
			return NewServiceRepository(database).Create(requestContext, newTestService(9, "", "user-a"))
		}, expectedKind: apperror.KindInternal},
		{name: "service 取得: ユーザー ID が空", call: func() error {
			_, _, err := NewServiceRepository(database).FindByDeployID(requestContext, "", "deploy-00000001")
			return err
		}, expectedKind: apperror.KindNotFound},
		{name: "service 取得(ワーカー): ID が空(先頭の1件を返さない)", call: func() error { _, err := NewServiceRepository(database).FindForWorker(requestContext, ""); return err }, expectedKind: apperror.KindNotFound},
		{name: "service を active に: ID が空(全ての pending を変えない)", call: func() error {
			_, err := NewServiceRepository(database).CompleteCreation(requestContext, "", true, "dns", "ip")
			return err
		}, expectedKind: apperror.KindInternal},
		{name: "service の状態変更: ID が空", call: func() error {
			_, err := NewServiceRepository(database).CompleteCreation(requestContext, "", false, "", "")
			return err
		}, expectedKind: apperror.KindInternal},
		{name: "service の削除の受付: deploy の ID が空", call: func() error {
			_, _, err := NewServiceRepository(database).MarkDeleting(requestContext, "user-a", "")
			return err
		}, expectedKind: apperror.KindNotFound},
		{name: "service のレコード削除: ID が空(削除中の全ての行を消さない)", call: func() error { return NewServiceRepository(database).DeleteRecord(requestContext, "") }, expectedKind: apperror.KindInternal},
		{name: "service の作成の取り消し: ID が空(pending の全ての行を消さない)", call: func() error { return NewServiceRepository(database).DeleteUnstarted(requestContext, "") }, expectedKind: apperror.KindInternal},
		{name: "service のポートの保存: deploy の ID が空", call: func() error {
			return NewServiceRepository(database).SavePendingPorts(requestContext, "user-a", "", []byte(`[]`))
		}, expectedKind: apperror.KindNotFound},
		{name: "service のポートの破棄: ユーザー ID が空", call: func() error {
			return NewServiceRepository(database).DiscardPendingPorts(requestContext, "", "deploy-00000001")
		}, expectedKind: apperror.KindNotFound},
		{name: "service のポートの適用の開始: deploy の ID が空", call: func() error {
			_, _, err := NewServiceRepository(database).BeginPortsApply(requestContext, "user-a", "")
			return err
		}, expectedKind: apperror.KindNotFound},
		{name: "service のポートの適用の巻き戻し: ID が空(applying の全ての deploy を変えない)", call: func() error { return NewServiceRepository(database).RevertPortsApply(requestContext, "", "active") }, expectedKind: apperror.KindInternal},
		{name: "service のポートの適用の確定: ID が空", call: func() error {
			_, err := NewServiceRepository(database).CommitPortsApply(requestContext, "", true)
			return err
		}, expectedKind: apperror.KindInternal},
		{name: "ingress 取得: ユーザー ID が空", call: func() error {
			_, err := NewIngressRepository(database).FindByID(requestContext, "", "project-00000001", "ingress-1")
			return err
		}, expectedKind: apperror.KindNotFound},
		{name: "ingress 取得(ワーカー): ID が空(先頭の1件を返さない)", call: func() error { _, err := NewIngressRepository(database).FindForWorker(requestContext, ""); return err }, expectedKind: apperror.KindNotFound},
		{name: "ingress の一覧: プロジェクトの ID が空", call: func() error {
			_, _, err := NewIngressRepository(database).ListByProject(requestContext, "user-a", "", 20, nil)
			return err
		}, expectedKind: apperror.KindNotFound},
		{name: "ingress の作成: ID が空", call: func() error { return NewIngressRepository(database).Create(requestContext, &model.Ingress{}) }, expectedKind: apperror.KindInternal},
		{name: "ingress の削除の受付: ingress の ID が空", call: func() error {
			_, err := NewIngressRepository(database).MarkDeleting(requestContext, "user-a", "project-00000001", "")
			return err
		}, expectedKind: apperror.KindNotFound},
		{name: "ingress のレコード削除: ID が空(削除中の全ての行を消さない)", call: func() error { return NewIngressRepository(database).DeleteRecord(requestContext, "") }, expectedKind: apperror.KindInternal},
		{name: "ingress の削除の巻き戻し: ID が空", call: func() error { return NewIngressRepository(database).RevertDeleting(requestContext, "", "active") }, expectedKind: apperror.KindInternal},
		{name: "ingress の削除失敗の記録: ID が空(削除中の全ての行を変えない)", call: func() error { return NewIngressRepository(database).MarkDeleteFailed(requestContext, "") }, expectedKind: apperror.KindInternal},
		{name: "ingress のルールの保存: ingress の ID が空", call: func() error {
			return NewIngressRepository(database).SavePendingRules(requestContext, "user-a", "project-00000001", "", nil)
		}, expectedKind: apperror.KindNotFound},
		{name: "ingress のルールの破棄: ユーザー ID が空", call: func() error {
			return NewIngressRepository(database).DiscardPendingRules(requestContext, "", "project-00000001", "ingress-1")
		}, expectedKind: apperror.KindNotFound},
		{name: "ingress の反映の開始: ingress の ID が空", call: func() error {
			_, err := NewIngressRepository(database).BeginRulesApply(requestContext, "user-a", "project-00000001", "")
			return err
		}, expectedKind: apperror.KindNotFound},
		{name: "ingress の反映の巻き戻し: ID が空(applying の全ての行を変えない)", call: func() error { return NewIngressRepository(database).RevertApply(requestContext, "", "active") }, expectedKind: apperror.KindInternal},
		{name: "ingress の反映の確定: ID が空", call: func() error {
			_, err := NewIngressRepository(database).CommitRulesApply(requestContext, "", true)
			return err
		}, expectedKind: apperror.KindInternal},
		{name: "deploy 取り消し: ID が空(pending の全ての行を消さない)", call: func() error { return deployRepository.DeleteUnstarted(requestContext, "") }, expectedKind: apperror.KindInternal},
		{name: "deploy 作成の確定: ID が空", call: func() error {
			_, err := deployRepository.CompleteCreation(requestContext, "", true)
			return err
		}, expectedKind: apperror.KindInternal},
		{name: "deploy スケーリングの確定: ID が空", call: func() error {
			_, err := deployRepository.CompleteScale(requestContext, "", true)
			return err
		}, expectedKind: apperror.KindInternal},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// 空の値で呼ぶと、決まった種類のエラーになる。
			if actualKind := apperror.KindOf(testCase.call()); actualKind != testCase.expectedKind {
				subTest.Errorf("エラーの種類が違う: 期待 %q、実際 %q", testCase.expectedKind, actualKind)
			}
		})
	}

	// 全ての呼び出しのあとも、データが1行も変わっていないこと。
	var projectCount, deletingCount, pendingDeployCount int64
	database.Model(&model.Project{}).Count(&projectCount)
	database.Model(&model.Project{}).Where("status = ?", model.ProjectStatusDeleting).Count(&deletingCount)
	database.Model(&model.Deploy{}).Where("status = ?", model.DeployStatusPending).Count(&pendingDeployCount)
	if projectCount != 2 || deletingCount != 1 || pendingDeployCount != 1 {
		testingT.Errorf("データが変わっている: プロジェクト %d(期待 2)、削除中 %d(期待 1)、pending の deploy %d(期待 1)", projectCount, deletingCount, pendingDeployCount)
	}
}
