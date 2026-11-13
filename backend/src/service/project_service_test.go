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
	// 渡されたプロジェクトを、後で確認できるように覚える。
	fakeStore.createdProject = project
	// テストが決めたエラー(無ければ nil)を返す。
	return fakeStore.createError
}

// FindByID は、固定のプロジェクトを返す。
func (fakeStore *fakeProjectStore) FindByID(_ context.Context, userID string, projectID string) (model.Project, error) {
	// 渡された ID とユーザーだけを持つプロジェクトを返す。
	return model.Project{ID: projectID, UserID: userID}, nil
}

// ListByUser は、空の一覧を返す。
func (fakeStore *fakeProjectStore) ListByUser(_ context.Context, _ string, _ int, _ *pagination.Position) ([]model.Project, *pagination.Position, error) {
	// 一覧は空で、次のページも無いことにする。
	return nil, nil, nil
}

// Rename は、受け取った名前を覚えて、決めたエラーを返す。
func (fakeStore *fakeProjectStore) Rename(_ context.Context, _ string, _ string, newName string) error {
	// 渡された新しい名前を、後で確認できるように覚える。
	fakeStore.renamedName = newName
	// テストが決めたエラー(無ければ nil)を返す。
	return fakeStore.renameError
}

// TestProjectServiceCreate は、作成すると、project- で始まる ID と active の状態で保存され、
// 保存のエラーはそのまま返ることを確認する。
func TestProjectServiceCreate(testingT *testing.T) {
	testingT.Run("ID を作り、active で保存する", func(subTest *testing.T) {
		// 何も覚えていない fake の保存先を用意する。
		fakeStore := &fakeProjectStore{}
		// fake の保存先を使うサービスを作る。
		projectService := NewProjectService(fakeStore)

		// 検証対象の作成を呼ぶ。
		projectID, createError := projectService.Create(context.Background(), "user-a", "my-app")

		// 作成はエラーにならないはず。
		if createError != nil {
			subTest.Fatalf("エラーにならないはず: %v", createError)
		}
		// ID が project- で始まり、後ろが UUID の36文字であることを確認する。
		if !strings.HasPrefix(projectID, "project-") || len(projectID) != len("project-")+36 {
			subTest.Errorf("ID の形が違う: %q", projectID)
		}
		// 保存先に渡された内容を取り出す。
		saved := fakeStore.createdProject
		// 保存された内容が、渡した値と active の状態になっていることを確認する。
		if saved == nil || saved.ID != projectID || saved.UserID != "user-a" || saved.Name != "my-app" || saved.Status != model.ProjectStatusActive {
			subTest.Errorf("保存した内容が違う: %+v", saved)
		}
	})

	testingT.Run("2回作ると、違う ID になる", func(subTest *testing.T) {
		// 保存先に何も仕込まないサービスを作る。
		projectService := NewProjectService(&fakeProjectStore{})
		// 1つ目のプロジェクトを作る。
		firstID, _ := projectService.Create(context.Background(), "user-a", "first")
		// 2つ目のプロジェクトを作る。
		secondID, _ := projectService.Create(context.Background(), "user-a", "second")
		// 2つの ID が同じなら、重複しているので失敗にする。
		if firstID == secondID {
			subTest.Errorf("ID が重複した: %q", firstID)
		}
	})

	testingT.Run("保存のエラーはそのまま返す", func(subTest *testing.T) {
		// 保存先が返すエラーとして、conflict を用意する。
		conflictError := apperror.New(apperror.KindConflict, "重複")
		// 保存が conflict で失敗する保存先を使うサービスを作る。
		projectService := NewProjectService(&fakeProjectStore{createError: conflictError})

		// 作成を呼ぶ。
		projectID, createError := projectService.Create(context.Background(), "user-a", "my-app")

		// 同じエラーがそのまま返り、ID は空であることを確認する。
		if !errors.Is(createError, conflictError) || projectID != "" {
			subTest.Errorf("エラーをそのまま返すはず: ID %q、エラー %v", projectID, createError)
		}
	})
}

// TestProjectServiceRename は、名前の変更を保存先に渡し、そのエラーを返すことを確認する。
func TestProjectServiceRename(testingT *testing.T) {
	// 名前の変更が conflict で失敗する保存先を用意する。
	fakeStore := &fakeProjectStore{renameError: apperror.New(apperror.KindConflict, "削除中")}
	// fake の保存先を使うサービスを作る。
	projectService := NewProjectService(fakeStore)

	// 検証対象の名前の変更を呼ぶ。
	renameError := projectService.Rename(context.Background(), "user-a", "project-1", "renamed")

	// 新しい名前が、保存先にそのまま渡されたことを確認する。
	if fakeStore.renamedName != "renamed" {
		testingT.Errorf("新しい名前を渡していない: %q", fakeStore.renamedName)
	}
	// 保存先のエラーの種類(conflict)が、そのまま返ることを確認する。
	if apperror.KindOf(renameError) != apperror.KindConflict {
		testingT.Errorf("conflict を返すはず: %v", renameError)
	}
}
