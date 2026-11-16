package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// TestDeployRepositoryListByProject は、プロジェクトの deploy が、作成日時の新しい順に、ページごとに、重複も欠落も無く返ること、
// 他のプロジェクトの deploy が混ざらないこと、削除中のものも含まれること、プロジェクトが無い・他人のものは not_found になることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestDeployRepositoryListByProject(testingT *testing.T) {
	// プロジェクトを2つ(user-a の2つ目と、user-b のもの)用意する。
	database, deployRepository := newDeployTestEnvironment(testingT)
	ctx := context.Background()
	mustCreate(testingT, database, newTestProject(2, "user-a", time.Now()))
	mustCreate(testingT, database, newTestProject(3, "user-b", time.Now()))
	// project-1 に、5 つの deploy を、1 分ずつずらして作る(deploy-5 が最も新しい)。1つは削除中。
	base := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	for number := 1; number <= 5; number++ {
		deploy := newTestDeploy(number, "project-00000001", "user-a")
		deploy.CreatedAt = base.Add(time.Duration(number) * time.Minute)
		deploy.UpdatedAt = deploy.CreatedAt
		if createError := database.Create(deploy).Error; createError != nil {
			testingT.Fatal(createError)
		}
	}
	database.Exec("UPDATE deploys SET status = 'deleting' WHERE id = 'deploy-00000003'")
	// 別のプロジェクトの deploy は、混ざらない。
	other := newTestDeploy(9, "project-00000002", "user-a")
	if createError := database.Create(other).Error; createError != nil {
		testingT.Fatal(createError)
	}
	// 1ページ目(2件): 新しい順で、続きがある。
	first, next, listError := deployRepository.ListByProject(ctx, "user-a", "project-00000001", 2, nil)
	if listError != nil || len(first) != 2 || first[0].ID != "deploy-00000005" || first[1].ID != "deploy-00000004" || next == nil {
		testingT.Fatalf("新しい2件と、次の位置のはず: %+v %v %v", first, next, listError)
	}
	// 一覧には、一覧に出す項目が入り、大きな列(環境変数など)は、読まない。
	if first[0].Spec != "small" || first[0].Replicas != 1 || first[0].ImageRef != "registry.example/app:1" || first[0].Status != model.DeployStatusPending || first[0].Env != nil {
		testingT.Fatalf("一覧の項目だけが入るはず: %+v", first[0])
	}
	// 2ページ目(続き): 削除中のものも含まれる。重複しない。
	second, secondNext, _ := deployRepository.ListByProject(ctx, "user-a", "project-00000001", 2, next)
	if len(second) != 2 || second[0].ID != "deploy-00000003" || second[0].Status != "deleting" || second[1].ID != "deploy-00000002" || secondNext == nil {
		testingT.Fatalf("次の2件(削除中を含む)のはず: %+v", second)
	}
	// 最後のページ: 残りの1件で、次の位置は無い。
	last, lastNext, _ := deployRepository.ListByProject(ctx, "user-a", "project-00000001", 2, secondNext)
	if len(last) != 1 || last[0].ID != "deploy-00000001" || lastNext != nil {
		testingT.Fatalf("最後の1件のはず: %+v %v", last, lastNext)
	}
	// ちょうど limit 件のときは、次のページは無い。
	exact, exactNext, _ := deployRepository.ListByProject(ctx, "user-a", "project-00000001", 5, nil)
	if len(exact) != 5 || exactNext != nil {
		testingT.Fatalf("ちょうど5件で、次の位置は無いはず: %d %v", len(exact), exactNext)
	}
	// 別のプロジェクトの deploy は、そのプロジェクトにだけ出る。
	if mine, _, _ := deployRepository.ListByProject(ctx, "user-a", "project-00000002", 20, nil); len(mine) != 1 || mine[0].ID != other.ID {
		testingT.Fatalf("別のプロジェクトのものだけのはず: %+v", mine)
	}
	// deploy が無いプロジェクトは、空(エラーにしない)。
	if empty, emptyNext, emptyError := deployRepository.ListByProject(ctx, "user-b", "project-00000003", 20, nil); emptyError != nil || len(empty) != 0 || emptyNext != nil {
		testingT.Fatalf("空のはず: %v %v %v", empty, emptyNext, emptyError)
	}
	// 他人のプロジェクト、存在しないプロジェクトは、同じ not_found。
	for name, arguments := range map[string][2]string{"他人のプロジェクト": {"user-b", "project-00000001"}, "存在しないプロジェクト": {"user-a", "project-nothing"}} {
		if _, _, notFoundError := deployRepository.ListByProject(ctx, arguments[0], arguments[1], 20, nil); apperror.KindOf(notFoundError) != apperror.KindNotFound {
			testingT.Errorf("%s は not_found のはず: %v", name, notFoundError)
		}
	}
}

// TestDeployRepositoryListByProjectKeepsOrderForSameTimestamp は、作成日時が同じ deploy が複数あっても、ID で順序が決まり、ページをまたいで、重複も欠落も無いことを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestDeployRepositoryListByProjectKeepsOrderForSameTimestamp(testingT *testing.T) {
	// 同じ作成日時の deploy を 7 つ作る。
	database, deployRepository := newDeployTestEnvironment(testingT)
	sameTime := time.Now().Add(-time.Minute).Truncate(time.Microsecond)
	for number := 1; number <= 7; number++ {
		deploy := newTestDeploy(number, "project-00000001", "user-a")
		deploy.CreatedAt, deploy.UpdatedAt = sameTime, sameTime
		if createError := database.Create(deploy).Error; createError != nil {
			testingT.Fatal(createError)
		}
	}
	// 3 件ずつ辿って、全て集める。
	seen := map[string]bool{}
	var after *pagination.Position
	for page := 0; page < 10; page++ {
		deploys, next, listError := deployRepository.ListByProject(context.Background(), "user-a", "project-00000001", 3, after)
		if listError != nil {
			testingT.Fatal(listError)
		}
		for _, deploy := range deploys {
			if seen[deploy.ID] {
				testingT.Fatalf("重複した: %s", deploy.ID)
			}
			seen[deploy.ID] = true
		}
		if next == nil {
			break
		}
		after = next
	}
	if len(seen) != 7 {
		testingT.Fatalf("7件全てを、1回ずつ集めるはず: %d %s", len(seen), fmt.Sprint(seen))
	}
}
