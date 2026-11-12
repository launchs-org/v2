package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// fakeProjectStore は、テスト用の ProjectStore。受け取った引数を覚え、決めたエラーを返す。
type fakeProjectStore struct {
	createdProject *model.Project
	createError    error
	renameError    error
	renamedName    string
}

// Create は、受け取ったプロジェクトを覚えて、決めたエラーを返す。
func (fakeStore *fakeProjectStore) Create(_ context.Context, project *model.Project) error {
	fakeStore.createdProject = project
	return fakeStore.createError
}

// FindByID は、固定のプロジェクトを返す。
func (fakeStore *fakeProjectStore) FindByID(_ context.Context, userID string, projectID string) (model.Project, error) {
	return model.Project{ID: projectID, UserID: userID}, nil
}

// ListByUser は、空の一覧を返す。
func (fakeStore *fakeProjectStore) ListByUser(_ context.Context, _ string, _ int, _ *pagination.Position) ([]model.Project, *pagination.Position, error) {
	return nil, nil, nil
}

// Rename は、受け取った名前を覚えて、決めたエラーを返す。
func (fakeStore *fakeProjectStore) Rename(_ context.Context, _ string, _ string, newName string) error {
	fakeStore.renamedName = newName
	return fakeStore.renameError
}

// TestProjectServiceCreate は、作成すると、project- で始まる ID と active の状態で保存され、
// 保存のエラーはそのまま返ることを確認する。
func TestProjectServiceCreate(testingT *testing.T) {
	testingT.Run("ID を作り、active で保存する", func(subTest *testing.T) {
		fakeStore := &fakeProjectStore{}
		projectService := NewProjectService(fakeStore)

		projectID, createError := projectService.Create(context.Background(), "user-a", "my-app")

		if createError != nil {
			subTest.Fatalf("エラーにならないはず: %v", createError)
		}
		if !strings.HasPrefix(projectID, "project-") || len(projectID) != len("project-")+36 {
			subTest.Errorf("ID の形が違う: %q", projectID)
		}
		saved := fakeStore.createdProject
		if saved == nil || saved.ID != projectID || saved.UserID != "user-a" || saved.Name != "my-app" || saved.Status != model.ProjectStatusActive {
			subTest.Errorf("保存した内容が違う: %+v", saved)
		}
	})

	testingT.Run("2回作ると、違う ID になる", func(subTest *testing.T) {
		projectService := NewProjectService(&fakeProjectStore{})
		firstID, _ := projectService.Create(context.Background(), "user-a", "first")
		secondID, _ := projectService.Create(context.Background(), "user-a", "second")
		if firstID == secondID {
			subTest.Errorf("ID が重複した: %q", firstID)
		}
	})

	testingT.Run("保存のエラーはそのまま返す", func(subTest *testing.T) {
		conflictError := apperror.New(apperror.KindConflict, "重複")
		projectService := NewProjectService(&fakeProjectStore{createError: conflictError})

		projectID, createError := projectService.Create(context.Background(), "user-a", "my-app")

		if !errors.Is(createError, conflictError) || projectID != "" {
			subTest.Errorf("エラーをそのまま返すはず: ID %q、エラー %v", projectID, createError)
		}
	})
}

// TestProjectServiceRename は、名前の変更を保存先に渡し、そのエラーを返すことを確認する。
func TestProjectServiceRename(testingT *testing.T) {
	fakeStore := &fakeProjectStore{renameError: apperror.New(apperror.KindConflict, "削除中")}
	projectService := NewProjectService(fakeStore)

	renameError := projectService.Rename(context.Background(), "user-a", "project-1", "renamed")

	if fakeStore.renamedName != "renamed" {
		testingT.Errorf("新しい名前を渡していない: %q", fakeStore.renamedName)
	}
	if apperror.KindOf(renameError) != apperror.KindConflict {
		testingT.Errorf("conflict を返すはず: %v", renameError)
	}
}
