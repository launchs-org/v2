package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/datatypes"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"
	"launchs/shared/repository"
)

// fakeDeployStore は、テスト用の DeployStore。受け取った引数を覚え、決めたエラーを返す。
type fakeDeployStore struct {
	specError       error
	createError     error
	createdDeploy   *model.Deploy
	cancelError     error
	canceledDeploys []string
	findError       error
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
	return model.Deploy{ID: deployID, UserID: userID}, fakeStore.findError
}

// ListByProject は、何も返さない(一覧の振る舞いは、リポジトリと API のテストで確かめる)。
func (fakeStore *fakeDeployStore) ListByProject(_ context.Context, _ string, _ string, _ int, _ *pagination.Position) ([]model.Deploy, *pagination.Position, error) {
	return nil, nil, nil
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

// BeginConfigApply は、何もせず成功する。
func (fakeStore *fakeDeployStore) BeginConfigApply(_ context.Context, _ string, _ string) (string, error) {
	return model.DeployStatusActive, nil
}

// SavePendingMounts は、何もせず成功する。
func (fakeStore *fakeDeployStore) SavePendingMounts(_ context.Context, _ string, _ string, _ []model.MountSpec) error {
	return nil
}

// DiscardPendingMounts は、何もせず成功する。
func (fakeStore *fakeDeployStore) DiscardPendingMounts(_ context.Context, _ string, _ string) error {
	return nil
}

// BeginMountsApply は、元の状態として active を返す。
func (fakeStore *fakeDeployStore) BeginMountsApply(_ context.Context, _ string, _ string) (string, error) {
	return model.DeployStatusActive, nil
}

// RevertApply は、何もせず成功する。
func (fakeStore *fakeDeployStore) RevertApply(_ context.Context, _ string, _ string) error {
	return nil
}

// BeginScale は、何もせず、変更ありとして成功する。
func (fakeStore *fakeDeployStore) BeginScale(_ context.Context, _ string, _ string, _ int) (repository.ScaleStart, error) {
	return repository.ScaleStart{Changed: true, PreviousReplicas: 1, PreviousStatus: model.DeployStatusActive}, nil
}

// RevertScale は、何もせず成功する。
func (fakeStore *fakeDeployStore) RevertScale(_ context.Context, _ string, _ repository.ScaleStart) error {
	return nil
}

// MarkDeleting は、何もせず、元の状態を active として成功する。
func (fakeStore *fakeDeployStore) MarkDeleting(_ context.Context, _ string, _ string) (string, error) {
	return model.DeployStatusActive, nil
}

// RevertDeleting は、何もせず成功する。
func (fakeStore *fakeDeployStore) RevertDeleting(_ context.Context, _ string, _ string) error {
	return nil
}

// fakePodStore は、テスト用の PodStore。決めた Pod を返し、問い合わせた deploy の ID を覚える。
type fakePodStore struct {
	pods       []model.DeployPod
	queriedIDs []string
	err        error
}

// ListPodsByDeploy は、問い合わせた ID を覚えて、決めた Pod とエラーを返す。
func (fakeStore *fakePodStore) ListPodsByDeploy(_ context.Context, deployID string) ([]model.DeployPod, error) {
	fakeStore.queriedIDs = append(fakeStore.queriedIDs, deployID)
	return fakeStore.pods, fakeStore.err
}

// fakeDeployWorkflowStarter は、テスト用の DeployWorkflowStarter。開始を頼まれた ID を覚え、決めたエラーを返す。
type fakeDeployWorkflowStarter struct {
	startedDeployIDs []string
	startError       error
}

// StartMountsApply は、何もせず成功する。
func (fakeStarter *fakeDeployWorkflowStarter) StartMountsApply(_ context.Context, _ string) error {
	return nil
}

// StartDeployCreation は、ID を覚えて、決めたエラーを返す。
func (fakeStarter *fakeDeployWorkflowStarter) StartDeployCreation(_ context.Context, deployID string) error {
	fakeStarter.startedDeployIDs = append(fakeStarter.startedDeployIDs, deployID)
	return fakeStarter.startError
}

// StartConfigApply は、何もせず、決めたエラーを返す。
func (fakeStarter *fakeDeployWorkflowStarter) StartConfigApply(_ context.Context, _ string) error {
	return fakeStarter.startError
}

// StartScale は、何もせず、決めたエラーを返す。
func (fakeStarter *fakeDeployWorkflowStarter) StartScale(_ context.Context, _ string) error {
	return fakeStarter.startError
}

// StartDeletion は、何もせず、決めたエラーを返す。
func (fakeStarter *fakeDeployWorkflowStarter) StartDeletion(_ context.Context, _ string) error {
	return fakeStarter.startError
}

// TestDeployServiceCreate は、deploy の作成の、正常・スペックの不正・保存の失敗・Workflow の開始の失敗を確認する。
func TestDeployServiceCreate(testingT *testing.T) {
	// 正常な場合。
	testingT.Run("deploy- で始まる ID で、pending・replicas 1・空の環境変数で保存し、Workflow を開始する", func(subTest *testing.T) {
		fakeStore := &fakeDeployStore{}
		fakeStarter := &fakeDeployWorkflowStarter{}

		deployID, createError := NewDeployService(fakeStore, &fakePodStore{}, fakeStarter).Create(context.Background(), "user-a", "project-1", "registry/app:1", "small")

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

		_, createError := NewDeployService(fakeStore, &fakePodStore{}, fakeStarter).Create(context.Background(), "user-a", "project-1", "registry/app:1", "huge")

		if apperror.KindOf(createError) != apperror.KindInvalidRequest || fakeStore.createdDeploy != nil || len(fakeStarter.startedDeployIDs) != 0 {
			subTest.Errorf("invalid_request で、保存も開始もしないはず: %v", createError)
		}
	})
	// 保存に失敗したとき(削除中のプロジェクトなど)は、そのエラーを返し、Workflow を開始しない。
	testingT.Run("保存のエラーはそのまま返し、Workflow を開始しない", func(subTest *testing.T) {
		fakeStore := &fakeDeployStore{createError: apperror.New(apperror.KindConflict, "削除中")}
		fakeStarter := &fakeDeployWorkflowStarter{}

		_, createError := NewDeployService(fakeStore, &fakePodStore{}, fakeStarter).Create(context.Background(), "user-a", "project-1", "registry/app:1", "small")

		if apperror.KindOf(createError) != apperror.KindConflict || len(fakeStarter.startedDeployIDs) != 0 {
			subTest.Errorf("conflict で、開始しないはず: %v", createError)
		}
	})
	// Workflow を開始できなかったときは、保存を取り消して internal_error にする。
	testingT.Run("Workflow を開始できなければ、保存を取り消して internal_error", func(subTest *testing.T) {
		fakeStore := &fakeDeployStore{}
		fakeStarter := &fakeDeployWorkflowStarter{startError: errors.New("temporal down")}

		_, createError := NewDeployService(fakeStore, &fakePodStore{}, fakeStarter).Create(context.Background(), "user-a", "project-1", "registry/app:1", "small")

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

		_, createError := NewDeployService(fakeStore, &fakePodStore{}, fakeStarter).Create(context.Background(), "user-a", "project-1", "registry/app:1", "small")

		if apperror.KindOf(createError) != apperror.KindInternal {
			subTest.Errorf("internal_error のはず: %v", createError)
		}
	})
}

// TestDeployServiceListPods は、持ち主の確認のあとで Pod を読み、他人や無い deploy の Pod は読まないことを確認する。
func TestDeployServiceListPods(testingT *testing.T) {
	testingT.Run("持ち主の deploy なら、Pod の一覧を返す", func(subTest *testing.T) {
		fakePods := &fakePodStore{pods: []model.DeployPod{{PodUID: "uid-1"}, {PodUID: "uid-2"}}}
		pods, listError := NewDeployService(&fakeDeployStore{}, fakePods, &fakeDeployWorkflowStarter{}).ListPods(context.Background(), "user-a", "deploy-1")
		if listError != nil || len(pods) != 2 || len(fakePods.queriedIDs) != 1 || fakePods.queriedIDs[0] != "deploy-1" {
			subTest.Errorf("2件を、deploy-1 で読むはず: %v %v %v", listError, pods, fakePods.queriedIDs)
		}
	})
	testingT.Run("deploy が無い、または他人のものなら not_found で、Pod は読まない", func(subTest *testing.T) {
		fakePods := &fakePodStore{pods: []model.DeployPod{{PodUID: "uid-1"}}}
		fakeStore := &fakeDeployStore{findError: apperror.New(apperror.KindNotFound, "deploy が見つかりません")}
		pods, listError := NewDeployService(fakeStore, fakePods, &fakeDeployWorkflowStarter{}).ListPods(context.Background(), "user-b", "deploy-1")
		if apperror.KindOf(listError) != apperror.KindNotFound || pods != nil || len(fakePods.queriedIDs) != 0 {
			subTest.Errorf("not_found で、Pod は読まないはず: %v %v %v", listError, pods, fakePods.queriedIDs)
		}
	})
	testingT.Run("Pod の読み取りに失敗したら、そのエラーを返す", func(subTest *testing.T) {
		fakePods := &fakePodStore{err: errors.New("database down")}
		if _, listError := NewDeployService(&fakeDeployStore{}, fakePods, &fakeDeployWorkflowStarter{}).ListPods(context.Background(), "user-a", "deploy-1"); listError == nil {
			subTest.Errorf("エラーのはず")
		}
	})
}
