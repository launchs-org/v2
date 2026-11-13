package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/datatypes"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// fakeDeployStore は、テスト用の DeployStore。受け取った引数を覚え、決めたエラーを返す。
type fakeDeployStore struct {
	specError       error
	createError     error
	createdDeploy   *model.Deploy
	cancelError     error
	canceledDeploys []string
}

// FindSpec は、決めたエラーを返す。
func (fakeStore *fakeDeployStore) FindSpec(_ context.Context, specName string) (model.DeploySpec, error) {
	return model.DeploySpec{Name: specName}, fakeStore.specError
}

// Create は、受け取った deploy を覚えて、決めたエラーを返す。
func (fakeStore *fakeDeployStore) Create(_ context.Context, deploy *model.Deploy) error {
	fakeStore.createdDeploy = deploy
	return fakeStore.createError
}

// FindByID は、固定の deploy を返す。
func (fakeStore *fakeDeployStore) FindByID(_ context.Context, userID string, deployID string) (model.Deploy, error) {
	return model.Deploy{ID: deployID, UserID: userID}, nil
}

// DeleteUnstarted は、取り消された ID を覚えて、決めたエラーを返す。
func (fakeStore *fakeDeployStore) DeleteUnstarted(_ context.Context, deployID string) error {
	fakeStore.canceledDeploys = append(fakeStore.canceledDeploys, deployID)
	return fakeStore.cancelError
}

// SavePendingConfig は、何もせず成功する。
func (fakeStore *fakeDeployStore) SavePendingConfig(_ context.Context, _ string, _ string, _ datatypes.JSON, _ *string, _ datatypes.JSON) error {
	return nil
}

// DiscardPendingConfig は、何もせず成功する。
func (fakeStore *fakeDeployStore) DiscardPendingConfig(_ context.Context, _ string, _ string) error {
	return nil
}

// fakeDeployWorkflowStarter は、テスト用の DeployWorkflowStarter。開始を頼まれた ID を覚え、決めたエラーを返す。
type fakeDeployWorkflowStarter struct {
	startedDeployIDs []string
	startError       error
}

// StartDeployCreation は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeDeployWorkflowStarter) StartDeployCreation(_ context.Context, deployID string) error {
	fakeStarter.startedDeployIDs = append(fakeStarter.startedDeployIDs, deployID)
	return fakeStarter.startError
}

// TestDeployServiceCreate は、deploy の作成の、正常・スペックの不正・保存の失敗・Workflow の開始の失敗を確認する。
func TestDeployServiceCreate(testingT *testing.T) {
	// 正常な場合。
	testingT.Run("deploy- で始まる ID で、pending・replicas 1・空の環境変数で保存し、Workflow を開始する", func(subTest *testing.T) {
		fakeStore := &fakeDeployStore{}
		fakeStarter := &fakeDeployWorkflowStarter{}

		deployID, createError := NewDeployService(fakeStore, fakeStarter).Create(context.Background(), "user-a", "project-1", "registry/app:1", "small")

		if createError != nil {
			subTest.Fatalf("エラーにならないはず: %v", createError)
		}
		if !strings.HasPrefix(deployID, "deploy-") || len(deployID) != len("deploy-")+36 {
			subTest.Errorf("ID の形が違う: %q", deployID)
		}
		saved := fakeStore.createdDeploy
		if saved == nil || saved.ID != deployID || saved.ProjectID != "project-1" || saved.UserID != "user-a" || saved.ImageRef != "registry/app:1" ||
			saved.Spec != "small" || saved.Replicas != 1 || saved.Status != model.DeployStatusPending || string(saved.Env) != "{}" {
			subTest.Errorf("保存した内容が違う: %+v", saved)
		}
		if len(fakeStarter.startedDeployIDs) != 1 || fakeStarter.startedDeployIDs[0] != deployID {
			subTest.Errorf("Workflow を1回開始するはず: %v", fakeStarter.startedDeployIDs)
		}
	})
	// スペックが不正なときは、何も保存せず、Workflow も開始しない。
	testingT.Run("スペックが不正なら、そのエラーを返し、保存も開始もしない", func(subTest *testing.T) {
		fakeStore := &fakeDeployStore{specError: apperror.New(apperror.KindInvalidRequest, "指定できないスペックです")}
		fakeStarter := &fakeDeployWorkflowStarter{}

		_, createError := NewDeployService(fakeStore, fakeStarter).Create(context.Background(), "user-a", "project-1", "registry/app:1", "huge")

		if apperror.KindOf(createError) != apperror.KindInvalidRequest || fakeStore.createdDeploy != nil || len(fakeStarter.startedDeployIDs) != 0 {
			subTest.Errorf("invalid_request で、保存も開始もしないはず: %v", createError)
		}
	})
	// 保存に失敗したとき(削除中のプロジェクトなど)は、そのエラーを返し、Workflow を開始しない。
	testingT.Run("保存のエラーはそのまま返し、Workflow を開始しない", func(subTest *testing.T) {
		fakeStore := &fakeDeployStore{createError: apperror.New(apperror.KindConflict, "削除中")}
		fakeStarter := &fakeDeployWorkflowStarter{}

		_, createError := NewDeployService(fakeStore, fakeStarter).Create(context.Background(), "user-a", "project-1", "registry/app:1", "small")

		if apperror.KindOf(createError) != apperror.KindConflict || len(fakeStarter.startedDeployIDs) != 0 {
			subTest.Errorf("conflict で、開始しないはず: %v", createError)
		}
	})
	// Workflow を開始できなかったときは、保存を取り消して internal_error にする。
	testingT.Run("Workflow を開始できなければ、保存を取り消して internal_error", func(subTest *testing.T) {
		fakeStore := &fakeDeployStore{}
		fakeStarter := &fakeDeployWorkflowStarter{startError: errors.New("temporal down")}

		_, createError := NewDeployService(fakeStore, fakeStarter).Create(context.Background(), "user-a", "project-1", "registry/app:1", "small")

		if apperror.KindOf(createError) != apperror.KindInternal {
			subTest.Errorf("internal_error のはず: %v", createError)
		}
		if len(fakeStore.canceledDeploys) != 1 || fakeStore.canceledDeploys[0] != fakeStore.createdDeploy.ID {
			subTest.Errorf("作った deploy を取り消すはず: %v", fakeStore.canceledDeploys)
		}
	})
	// 取り消しにも失敗しても、internal_error で返す。
	testingT.Run("取り消しにも失敗しても、internal_error", func(subTest *testing.T) {
		fakeStore := &fakeDeployStore{cancelError: errors.New("db down")}
		fakeStarter := &fakeDeployWorkflowStarter{startError: errors.New("temporal down")}

		_, createError := NewDeployService(fakeStore, fakeStarter).Create(context.Background(), "user-a", "project-1", "registry/app:1", "small")

		if apperror.KindOf(createError) != apperror.KindInternal {
			subTest.Errorf("internal_error のはず: %v", createError)
		}
	})
}
