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
	// 番号から ID と名前を作って、active のプロジェクトを返す。
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
	// 失敗したとき、このヘルパーではなく呼び出し元の行が表示されるようにする。
	testingT.Helper()
	// リポジトリで保存し、失敗したらテストを止める。
	if createError := NewProjectRepository(database).Create(context.Background(), project); createError != nil {
		testingT.Fatalf("保存できませんでした: %v", createError)
	}
}

// TestProjectRepositoryCreateAndFind は、保存したプロジェクトを取得でき、
// 他のユーザーからは not_found になることを確認する。
func TestProjectRepositoryCreateAndFind(testingT *testing.T) {
	// 使い捨てのデータベースを用意する(無い環境では、ここで飛ばされる)。
	database := testdatabase.New(testingT)
	// テスト対象のリポジトリを作る。
	projectRepository := NewProjectRepository(database)
	// user-a のプロジェクトを1件保存しておく。
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))

	// 持ち主・他のユーザー・存在しない ID の3つの場合を並べる。
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
	// 場合ごとに、サブテストとして実行する。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// 取得を実行する。
			project, findError := projectRepository.FindByID(context.Background(), testCase.userID, testCase.projectID)
			// エラーの種類が期待と違わないか確認する(期待が空なら、成功していること)。
			if apperror.KindOf(findError) != testCase.expectedKind && !(testCase.expectedKind == "" && findError == nil) {
				subTest.Fatalf("エラーの種類が違う: 期待 %q、実際 %v", testCase.expectedKind, findError)
			}
			// 取得できたときは、保存した内容が返っているか確認する。
			if findError == nil && project.Name != "app-1" {
				subTest.Errorf("名前が違う: %q", project.Name)
			}
		})
	}
}

// TestProjectRepositoryCreateRejectsDuplicateName は、
// 同じユーザーの中で名前が重複すると conflict になり、別のユーザーなら通ることを確認する。
func TestProjectRepositoryCreateRejectsDuplicateName(testingT *testing.T) {
	// 使い捨てのデータベースを用意する。
	database := testdatabase.New(testingT)
	// テスト対象のリポジトリを作る。
	projectRepository := NewProjectRepository(database)
	// 名前が app-1 のプロジェクトを保存しておく。
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))

	// 同じユーザーで、同じ名前のプロジェクトを作る。
	sameUserProject := newTestProject(2, "user-a", time.Now())
	// 既存と同じ名前にする。
	sameUserProject.Name = "app-1"
	// 保存すると、conflict になるはず。
	if kind := apperror.KindOf(projectRepository.Create(context.Background(), sameUserProject)); kind != apperror.KindConflict {
		testingT.Errorf("同じユーザーの重複は conflict のはず: %q", kind)
	}

	// 別のユーザーで、同じ名前のプロジェクトを作る。
	otherUserProject := newTestProject(3, "user-b", time.Now())
	// 名前の重複は、ユーザーごとなので、同じ名前にしても通るはず。
	otherUserProject.Name = "app-1"
	// 保存して、エラーにならないことを確認する。
	if createError := projectRepository.Create(context.Background(), otherUserProject); createError != nil {
		testingT.Errorf("別のユーザーなら同じ名前でも通るはず: %v", createError)
	}
}

// TestProjectRepositoryCreateRejectsInvalidName は、名前の制約(英数字とハイフンのみ、1〜64文字)を、
// データベースが守ることを確認する。
func TestProjectRepositoryCreateRejectsInvalidName(testingT *testing.T) {
	// 使い捨てのデータベースを用意する。
	database := testdatabase.New(testingT)
	// テスト対象のリポジトリを作る。
	projectRepository := NewProjectRepository(database)

	// 境界値の確認のために、65文字の名前を作る。
	longName := ""
	// a を65回つなげる。
	for range 65 {
		// 1文字ずつ足す。
		longName += "a"
	}
	// 文字数の境界(64と65)、空、記号の場合を並べる。
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
	// 場合ごとに、サブテストとして実行する。
	for index, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// ID が重ならないよう、添字から番号を作る。
			project := newTestProject(index+1, "user-a", time.Now())
			// 確認したい名前に差し替える。
			project.Name = testCase.projectName
			// 保存を実行する。
			createError := projectRepository.Create(context.Background(), project)
			// エラーの有無が期待どおりか確認する。
			if (createError != nil) != testCase.expectError {
				subTest.Errorf("エラーの有無が違う: 期待 %v、実際 %v", testCase.expectError, createError)
			}
		})
	}
}

// TestProjectRepositoryListByUser は、一覧が、作成日時の新しい順で、
// 自分のものだけを、limit とカーソルでページングして返すことを確認する。
func TestProjectRepositoryListByUser(testingT *testing.T) {
	// 使い捨てのデータベースを用意する。
	database := testdatabase.New(testingT)
	// テスト対象のリポジトリを作る。
	projectRepository := NewProjectRepository(database)
	// 作成日時の基準にする時刻。
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 古い順に 1〜5 を保存する。3 と 4 は作成日時が同じ(ID で順序を決める)。user-b の 6 は、一覧に出ないことを確認するためのもの。
	mustCreate(testingT, database, newTestProject(1, "user-a", baseTime))
	mustCreate(testingT, database, newTestProject(2, "user-a", baseTime.Add(time.Minute)))
	mustCreate(testingT, database, newTestProject(3, "user-a", baseTime.Add(2*time.Minute)))
	mustCreate(testingT, database, newTestProject(4, "user-a", baseTime.Add(2*time.Minute)))
	mustCreate(testingT, database, newTestProject(5, "user-a", baseTime.Add(3*time.Minute)))
	mustCreate(testingT, database, newTestProject(6, "user-b", baseTime.Add(4*time.Minute)))

	// 全ページから集めた ID。
	var collectedIDs []string
	// 次に読むページの開始位置。最初は nil(先頭から)。
	var cursorPosition *pagination.Position
	// 読んだページの数。
	pageCount := 0
	// 最後のページまで、2件ずつ読み続ける。
	for {
		// 1ページ分を取得する。
		projects, nextPosition, listError := projectRepository.ListByUser(context.Background(), "user-a", 2, cursorPosition)
		// 取得できなければ、テストを止める。
		if listError != nil {
			testingT.Fatalf("一覧を取得できませんでした: %v", listError)
		}
		// ページ数を数える。
		pageCount++
		// このページの ID を、順番に集める。
		for _, project := range projects {
			collectedIDs = append(collectedIDs, project.ID)
		}
		// 次のページが無ければ、読み終わり。
		if nextPosition == nil {
			break
		}
		// 次のページは、ここから読む。
		cursorPosition = nextPosition
	}

	// 新しい順で、同じ日時なら ID の大きい順。user-b のものは含まない。
	expectedIDs := []string{"project-00000005", "project-00000004", "project-00000003", "project-00000002", "project-00000001"}
	// 順序と内容が期待どおりか確認する。
	if fmt.Sprint(collectedIDs) != fmt.Sprint(expectedIDs) {
		testingT.Errorf("順序か内容が違う: 期待 %v、実際 %v", expectedIDs, collectedIDs)
	}
	// 2件ずつ5件なので、3ページで終わるか確認する。
	if pageCount != 3 {
		testingT.Errorf("2件ずつ5件なら3ページのはず: %d", pageCount)
	}
}

// TestProjectRepositoryListByUserLastPageHasNoNext は、ちょうど limit 件のとき、
// 次のページが無い(nil)ことを確認する。
func TestProjectRepositoryListByUserLastPageHasNoNext(testingT *testing.T) {
	// 使い捨てのデータベースを用意する。
	database := testdatabase.New(testingT)
	// テスト対象のリポジトリを作る。
	projectRepository := NewProjectRepository(database)
	// ちょうど2件を保存する。
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))
	mustCreate(testingT, database, newTestProject(2, "user-a", time.Now()))

	// limit を2件にして、先頭のページを取得する。
	projects, nextPosition, listError := projectRepository.ListByUser(context.Background(), "user-a", 2, nil)

	// 2件が返り、次のページの位置が nil であることを確認する。
	if listError != nil || len(projects) != 2 || nextPosition != nil {
		testingT.Errorf("2件で、次のページ無しのはず: 件数 %d、次 %v、エラー %v", len(projects), nextPosition, listError)
	}
}

// TestProjectRepositoryRename は、名前の変更の、正常・not_found・重複・削除中の場合を確認する。
func TestProjectRepositoryRename(testingT *testing.T) {
	// 使い捨てのデータベースを用意する。
	database := testdatabase.New(testingT)
	// テスト対象のリポジトリを作る。
	projectRepository := NewProjectRepository(database)
	// 名前を変える対象(1)と、重複の確認に使う名前を持つもの(2)を保存する。
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))
	mustCreate(testingT, database, newTestProject(2, "user-a", time.Now()))
	// 削除中のプロジェクトを用意する。
	deletingProject := newTestProject(3, "user-a", time.Now())
	// status を削除中にする。
	deletingProject.Status = model.ProjectStatusDeleting
	// 削除中のプロジェクトを保存する。
	mustCreate(testingT, database, deletingProject)

	// 正常・他のユーザー・存在しない・重複・削除中の場合を並べる。
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

	// 場合ごとに、サブテストとして実行する。
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// 名前の変更を実行する。
			renameError := projectRepository.Rename(context.Background(), testCase.userID, testCase.projectID, testCase.newName)
			// 成功を期待する場合は、結果の中身まで確認する。
			if testCase.expectedKind == "" {
				// 成功するはずなのにエラーなら、止める。
				if renameError != nil {
					subTest.Fatalf("成功するはずが、エラー: %v", renameError)
				}
				// 変更後のプロジェクトを取得し直す。
				renamedProject, _ := projectRepository.FindByID(context.Background(), "user-a", "project-00000001")
				// 名前が変わっているか確認する。
				if renamedProject.Name != "renamed" {
					subTest.Errorf("名前が変わっていない: %q", renamedProject.Name)
				}
				// 更新日時が、作成日時より後になっているか確認する。
				if !renamedProject.UpdatedAt.After(renamedProject.CreatedAt) {
					subTest.Errorf("更新日時が自動で更新されていない: 作成 %v、更新 %v", renamedProject.CreatedAt, renamedProject.UpdatedAt)
				}
				// 成功の場合の確認は、ここまで。
				return
			}
			// 失敗を期待する場合は、エラーの種類が期待どおりか確認する。
			if apperror.KindOf(renameError) != testCase.expectedKind {
				subTest.Errorf("エラーの種類が違う: 期待 %q、実際 %v", testCase.expectedKind, renameError)
			}
		})
	}
}

// TestProjectRepositoryMarkDeleting は、削除中への変更の、正常・not_found・二重の場合を確認する。
func TestProjectRepositoryMarkDeleting(testingT *testing.T) {
	// 使い捨てのデータベースを用意する。
	database := testdatabase.New(testingT)
	// テスト対象のリポジトリを作る。
	projectRepository := NewProjectRepository(database)
	// active のプロジェクトを1件保存する。
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))

	// 1回目は、削除中にできるはず。
	if markError := projectRepository.MarkDeleting(context.Background(), "user-a", "project-00000001"); markError != nil {
		testingT.Fatalf("削除中にできるはずが、エラー: %v", markError)
	}
	// 変更後のプロジェクトを取得し直す。
	project, _ := projectRepository.FindByID(context.Background(), "user-a", "project-00000001")
	// status が deleting になっているか確認する。
	if project.Status != model.ProjectStatusDeleting {
		testingT.Errorf("status が deleting になっていない: %q", project.Status)
	}

	// 同じものをもう一度削除中にしようとして、conflict になることを確認する。
	testingT.Run("すでに削除中なら conflict", func(subTest *testing.T) {
		// 2回目の削除中への変更を実行する。
		markError := projectRepository.MarkDeleting(context.Background(), "user-a", "project-00000001")
		// conflict になっているか確認する。
		if apperror.KindOf(markError) != apperror.KindConflict {
			subTest.Errorf("conflict のはずが、違う: %v", markError)
		}
	})
	// 他のユーザーが指定すると、存在しないものとして not_found になることを確認する。
	testingT.Run("他のユーザーは not_found", func(subTest *testing.T) {
		// 他のユーザーとして、削除中への変更を実行する。
		markError := projectRepository.MarkDeleting(context.Background(), "user-b", "project-00000001")
		// not_found になっているか確認する。
		if apperror.KindOf(markError) != apperror.KindNotFound {
			subTest.Errorf("not_found のはずが、違う: %v", markError)
		}
	})
}

// TestProjectRepositoryRevertDeleting は、削除中のプロジェクトを active に戻せ、
// active のものや他のユーザーのものには何も起きないことを確認する。
func TestProjectRepositoryRevertDeleting(testingT *testing.T) {
	// 使い捨てのデータベースに、削除中のプロジェクトを1件作る。
	database := testdatabase.New(testingT)
	projectRepository := NewProjectRepository(database)
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))
	if markError := projectRepository.MarkDeleting(context.Background(), "user-a", "project-00000001"); markError != nil {
		testingT.Fatalf("削除中にできませんでした: %v", markError)
	}

	// 他のユーザーが戻そうとしても、変わらない。
	testingT.Run("他のユーザーでは戻らない", func(subTest *testing.T) {
		if revertError := projectRepository.RevertDeleting(context.Background(), "user-b", "project-00000001"); revertError != nil {
			subTest.Fatalf("エラーにならないはず: %v", revertError)
		}
		project, _ := projectRepository.FindByID(context.Background(), "user-a", "project-00000001")
		if project.Status != model.ProjectStatusDeleting {
			subTest.Errorf("deleting のままのはず: %q", project.Status)
		}
	})
	// 持ち主なら、active に戻る。
	testingT.Run("持ち主なら active に戻る", func(subTest *testing.T) {
		if revertError := projectRepository.RevertDeleting(context.Background(), "user-a", "project-00000001"); revertError != nil {
			subTest.Fatalf("エラーにならないはず: %v", revertError)
		}
		project, _ := projectRepository.FindByID(context.Background(), "user-a", "project-00000001")
		if project.Status != model.ProjectStatusActive {
			subTest.Errorf("active に戻るはず: %q", project.Status)
		}
	})
	// すでに active でも、エラーにならない。
	testingT.Run("active のものに実行してもエラーにならない", func(subTest *testing.T) {
		if revertError := projectRepository.RevertDeleting(context.Background(), "user-a", "project-00000001"); revertError != nil {
			subTest.Errorf("エラーにならないはず: %v", revertError)
		}
	})
}

// TestProjectRepositoryDeleteRecord は、削除中のプロジェクトだけが物理削除され、
// active のものは消えず、繰り返し実行してもエラーにならないことを確認する。
func TestProjectRepositoryDeleteRecord(testingT *testing.T) {
	// active のプロジェクトと、削除中のプロジェクトを1件ずつ作る。
	database := testdatabase.New(testingT)
	projectRepository := NewProjectRepository(database)
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))
	mustCreate(testingT, database, newTestProject(2, "user-a", time.Now()))
	if markError := projectRepository.MarkDeleting(context.Background(), "user-a", "project-00000002"); markError != nil {
		testingT.Fatalf("削除中にできませんでした: %v", markError)
	}

	// active のものは、消えない(安全装置)。
	testingT.Run("active のプロジェクトは消えない", func(subTest *testing.T) {
		if deleteError := projectRepository.DeleteRecord(context.Background(), "project-00000001"); deleteError != nil {
			subTest.Fatalf("エラーにならないはず: %v", deleteError)
		}
		if _, findError := projectRepository.FindByID(context.Background(), "user-a", "project-00000001"); findError != nil {
			subTest.Errorf("消えていないはず: %v", findError)
		}
	})
	// 削除中のものは、消える。2回目はすでに無いが、エラーにならない。
	testingT.Run("削除中のプロジェクトは消え、2回目もエラーにならない", func(subTest *testing.T) {
		for range 2 {
			if deleteError := projectRepository.DeleteRecord(context.Background(), "project-00000002"); deleteError != nil {
				subTest.Fatalf("エラーにならないはず: %v", deleteError)
			}
		}
		_, findError := projectRepository.FindByID(context.Background(), "user-a", "project-00000002")
		if apperror.KindOf(findError) != apperror.KindNotFound {
			subTest.Errorf("not_found になるはず: %v", findError)
		}
	})
}
