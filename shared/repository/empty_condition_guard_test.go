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
		{name: "deploy 取り消し: ID が空(pending の全ての行を消さない)", call: func() error { return deployRepository.DeleteUnstarted(requestContext, "") }, expectedKind: apperror.KindInternal},
		{name: "deploy 状態変更: ID が空", call: func() error {
			_, err := deployRepository.UpdateStatus(requestContext, "", "pending", "active")
			return err
		}, expectedKind: apperror.KindInternal},
		{name: "deploy 状態変更: 期待する状態が空", call: func() error {
			_, err := deployRepository.UpdateStatus(requestContext, "deploy-00000001", "", "active")
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
