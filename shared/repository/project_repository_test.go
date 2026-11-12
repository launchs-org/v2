package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"launchs/shared/apperror"
	"launchs/shared/database/testdatabase"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// newTestProject は、テスト用のプロジェクトを作る。
//
// 引数:
//   - number: ID と名前を区別するための番号。
//   - userID: 持ち主のユーザー ID。
//   - createdAt: 作成日時。
//
// 戻り値:
//   - active 状態のプロジェクト。
func newTestProject(number int, userID string, createdAt time.Time) *model.Project {
	return &model.Project{
		ID:        fmt.Sprintf("project-%08d", number),
		UserID:    userID,
		Name:      fmt.Sprintf("app-%d", number),
		Status:    model.ProjectStatusActive,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
}

// mustCreate は、プロジェクトを保存し、失敗したらテストを止める。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - database: 保存先。
//   - project: 保存するプロジェクト。
func mustCreate(testingT *testing.T, database *gorm.DB, project *model.Project) {
	testingT.Helper()
	if createError := NewProjectRepository(database).Create(context.Background(), project); createError != nil {
		testingT.Fatalf("保存できませんでした: %v", createError)
	}
}

// TestProjectRepositoryCreateAndFind は、保存したプロジェクトを取得でき、
// 他のユーザーからは not_found になることを確認する。
func TestProjectRepositoryCreateAndFind(testingT *testing.T) {
	database := testdatabase.New(testingT)
	projectRepository := NewProjectRepository(database)
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))

	testCases := []struct {
		name         string
		userID       string
		projectID    string
		expectedKind apperror.Kind
	}{
		{name: "持ち主は取得できる", userID: "user-a", projectID: "project-00000001"},
		{name: "他のユーザーは not_found", userID: "user-b", projectID: "project-00000001", expectedKind: apperror.KindNotFound},
		{name: "存在しない ID は not_found", userID: "user-a", projectID: "project-99999999", expectedKind: apperror.KindNotFound},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			project, findError := projectRepository.FindByID(context.Background(), testCase.userID, testCase.projectID)
			if apperror.KindOf(findError) != testCase.expectedKind && !(testCase.expectedKind == "" && findError == nil) {
				subTest.Fatalf("エラーの種類が違う: 期待 %q、実際 %v", testCase.expectedKind, findError)
			}
			if findError == nil && project.Name != "app-1" {
				subTest.Errorf("名前が違う: %q", project.Name)
			}
		})
	}
}

// TestProjectRepositoryCreateRejectsDuplicateName は、
// 同じユーザーの中で名前が重複すると conflict になり、別のユーザーなら通ることを確認する。
func TestProjectRepositoryCreateRejectsDuplicateName(testingT *testing.T) {
	database := testdatabase.New(testingT)
	projectRepository := NewProjectRepository(database)
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))

	sameUserProject := newTestProject(2, "user-a", time.Now())
	sameUserProject.Name = "app-1"
	if kind := apperror.KindOf(projectRepository.Create(context.Background(), sameUserProject)); kind != apperror.KindConflict {
		testingT.Errorf("同じユーザーの重複は conflict のはず: %q", kind)
	}

	otherUserProject := newTestProject(3, "user-b", time.Now())
	otherUserProject.Name = "app-1"
	if createError := projectRepository.Create(context.Background(), otherUserProject); createError != nil {
		testingT.Errorf("別のユーザーなら同じ名前でも通るはず: %v", createError)
	}
}

// TestProjectRepositoryCreateRejectsInvalidName は、名前の制約(英数字とハイフンのみ、1〜64文字)を、
// データベースが守ることを確認する。
func TestProjectRepositoryCreateRejectsInvalidName(testingT *testing.T) {
	database := testdatabase.New(testingT)
	projectRepository := NewProjectRepository(database)

	longName := ""
	for range 65 {
		longName += "a"
	}
	testCases := []struct {
		name        string
		projectName string
		expectError bool
	}{
		{name: "64文字は通る", projectName: longName[:64]},
		{name: "65文字はエラー", projectName: longName, expectError: true},
		{name: "空文字はエラー", projectName: "", expectError: true},
		{name: "記号はエラー", projectName: "my_app", expectError: true},
	}
	for index, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			project := newTestProject(index+1, "user-a", time.Now())
			project.Name = testCase.projectName
			createError := projectRepository.Create(context.Background(), project)
			if (createError != nil) != testCase.expectError {
				subTest.Errorf("エラーの有無が違う: 期待 %v、実際 %v", testCase.expectError, createError)
			}
		})
	}
}

// TestProjectRepositoryListByUser は、一覧が、作成日時の新しい順で、
// 自分のものだけを、limit とカーソルでページングして返すことを確認する。
func TestProjectRepositoryListByUser(testingT *testing.T) {
	database := testdatabase.New(testingT)
	projectRepository := NewProjectRepository(database)
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 古い順に 1〜5 を保存する。3 と 4 は作成日時が同じ(ID で順序を決める)。
	mustCreate(testingT, database, newTestProject(1, "user-a", baseTime))
	mustCreate(testingT, database, newTestProject(2, "user-a", baseTime.Add(time.Minute)))
	mustCreate(testingT, database, newTestProject(3, "user-a", baseTime.Add(2*time.Minute)))
	mustCreate(testingT, database, newTestProject(4, "user-a", baseTime.Add(2*time.Minute)))
	mustCreate(testingT, database, newTestProject(5, "user-a", baseTime.Add(3*time.Minute)))
	mustCreate(testingT, database, newTestProject(6, "user-b", baseTime.Add(4*time.Minute)))

	var collectedIDs []string
	var cursorPosition *pagination.Position
	pageCount := 0
	for {
		projects, nextPosition, listError := projectRepository.ListByUser(context.Background(), "user-a", 2, cursorPosition)
		if listError != nil {
			testingT.Fatalf("一覧を取得できませんでした: %v", listError)
		}
		pageCount++
		for _, project := range projects {
			collectedIDs = append(collectedIDs, project.ID)
		}
		if nextPosition == nil {
			break
		}
		cursorPosition = nextPosition
	}

	expectedIDs := []string{"project-00000005", "project-00000004", "project-00000003", "project-00000002", "project-00000001"}
	if fmt.Sprint(collectedIDs) != fmt.Sprint(expectedIDs) {
		testingT.Errorf("順序か内容が違う: 期待 %v、実際 %v", expectedIDs, collectedIDs)
	}
	if pageCount != 3 {
		testingT.Errorf("2件ずつ5件なら3ページのはず: %d", pageCount)
	}
}

// TestProjectRepositoryListByUserLastPageHasNoNext は、ちょうど limit 件のとき、
// 次のページが無い(nil)ことを確認する。
func TestProjectRepositoryListByUserLastPageHasNoNext(testingT *testing.T) {
	database := testdatabase.New(testingT)
	projectRepository := NewProjectRepository(database)
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))
	mustCreate(testingT, database, newTestProject(2, "user-a", time.Now()))

	projects, nextPosition, listError := projectRepository.ListByUser(context.Background(), "user-a", 2, nil)

	if listError != nil || len(projects) != 2 || nextPosition != nil {
		testingT.Errorf("2件で、次のページ無しのはず: 件数 %d、次 %v、エラー %v", len(projects), nextPosition, listError)
	}
}

// TestProjectRepositoryRename は、名前の変更の、正常・not_found・重複・削除中の場合を確認する。
func TestProjectRepositoryRename(testingT *testing.T) {
	database := testdatabase.New(testingT)
	projectRepository := NewProjectRepository(database)
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))
	mustCreate(testingT, database, newTestProject(2, "user-a", time.Now()))
	deletingProject := newTestProject(3, "user-a", time.Now())
	deletingProject.Status = model.ProjectStatusDeleting
	mustCreate(testingT, database, deletingProject)

	testCases := []struct {
		name         string
		userID       string
		projectID    string
		newName      string
		expectedKind apperror.Kind
	}{
		{name: "名前を変えられる", userID: "user-a", projectID: "project-00000001", newName: "renamed"},
		{name: "他のユーザーは not_found", userID: "user-b", projectID: "project-00000001", newName: "x", expectedKind: apperror.KindNotFound},
		{name: "存在しないものは not_found", userID: "user-a", projectID: "project-99999999", newName: "x", expectedKind: apperror.KindNotFound},
		{name: "重複する名前は conflict", userID: "user-a", projectID: "project-00000001", newName: "app-2", expectedKind: apperror.KindConflict},
		{name: "削除中は conflict", userID: "user-a", projectID: "project-00000003", newName: "x", expectedKind: apperror.KindConflict},
	}

	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			renameError := projectRepository.Rename(context.Background(), testCase.userID, testCase.projectID, testCase.newName)
			if testCase.expectedKind == "" {
				if renameError != nil {
					subTest.Fatalf("成功するはずが、エラー: %v", renameError)
				}
				renamedProject, _ := projectRepository.FindByID(context.Background(), "user-a", "project-00000001")
				if renamedProject.Name != "renamed" {
					subTest.Errorf("名前が変わっていない: %q", renamedProject.Name)
				}
				if !renamedProject.UpdatedAt.After(renamedProject.CreatedAt) {
					subTest.Errorf("更新日時が自動で更新されていない: 作成 %v、更新 %v", renamedProject.CreatedAt, renamedProject.UpdatedAt)
				}
				return
			}
			if apperror.KindOf(renameError) != testCase.expectedKind {
				subTest.Errorf("エラーの種類が違う: 期待 %q、実際 %v", testCase.expectedKind, renameError)
			}
		})
	}
}

// TestProjectRepositoryMarkDeleting は、削除中への変更の、正常・not_found・二重の場合を確認する。
func TestProjectRepositoryMarkDeleting(testingT *testing.T) {
	database := testdatabase.New(testingT)
	projectRepository := NewProjectRepository(database)
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))

	if markError := projectRepository.MarkDeleting(context.Background(), "user-a", "project-00000001"); markError != nil {
		testingT.Fatalf("削除中にできるはずが、エラー: %v", markError)
	}
	project, _ := projectRepository.FindByID(context.Background(), "user-a", "project-00000001")
	if project.Status != model.ProjectStatusDeleting {
		testingT.Errorf("status が deleting になっていない: %q", project.Status)
	}

	testingT.Run("すでに削除中なら conflict", func(subTest *testing.T) {
		markError := projectRepository.MarkDeleting(context.Background(), "user-a", "project-00000001")
		if apperror.KindOf(markError) != apperror.KindConflict {
			subTest.Errorf("conflict のはずが、違う: %v", markError)
		}
	})
	testingT.Run("他のユーザーは not_found", func(subTest *testing.T) {
		markError := projectRepository.MarkDeleting(context.Background(), "user-b", "project-00000001")
		if apperror.KindOf(markError) != apperror.KindNotFound {
			subTest.Errorf("not_found のはずが、違う: %v", markError)
		}
	})
}
