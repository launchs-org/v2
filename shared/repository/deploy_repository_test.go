package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"launchs/shared/apperror"
	"launchs/shared/database/testdatabase"
	"launchs/shared/model"
)

// newTestDeploy は、テスト用の deploy を作る。
//
// 引数:
//   - number: ID を区別するための番号。
//   - projectID: 属するプロジェクトの ID。
//   - userID: 持ち主のユーザー ID。
//
// 戻り値:
//   - pending 状態の deploy。
func newTestDeploy(number int, projectID string, userID string) *model.Deploy {
	return &model.Deploy{
		ID:        fmt.Sprintf("deploy-%08d", number),
		ProjectID: projectID,
		UserID:    userID,
		ImageRef:  "registry.example/app:1",
		Spec:      "small",
		Replicas:  1,
		Status:    model.DeployStatusPending,
		Env:       datatypes.JSON(`{}`),
	}
}

// newDeployTestEnvironment は、使い捨てのデータベースと、持ち主 user-a の active なプロジェクトを用意する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - データベース。
//   - その上の DeployRepository。
func newDeployTestEnvironment(testingT *testing.T) (*gorm.DB, *DeployRepository) {
	// 使い捨てのデータベースに、プロジェクトを1件作る。
	database := testdatabase.New(testingT)
	mustCreate(testingT, database, newTestProject(1, "user-a", time.Now()))
	return database, NewDeployRepository(database)
}

// TestDeployRepositoryFindSpec は、マスターデータのスペックを取得でき、無い名前は invalid_request になることを確認する。
func TestDeployRepositoryFindSpec(testingT *testing.T) {
	// マイグレーションで入るマスターデータを使う。
	_, deployRepository := newDeployTestEnvironment(testingT)

	// 3つのスペックの大きさ(仮の初期値)と、無い名前の場合。
	testCases := []struct {
		name           string
		specName       string
		expectedCPU    int
		expectedMemory int
		expectedKind   apperror.Kind
	}{
		{name: "small は 0.25 CPU / 256MiB", specName: "small", expectedCPU: 250, expectedMemory: 256},
		{name: "medium は 0.5 CPU / 512MiB", specName: "medium", expectedCPU: 500, expectedMemory: 512},
		{name: "large は 1 CPU / 1GiB", specName: "large", expectedCPU: 1000, expectedMemory: 1024},
		{name: "無い名前は invalid_request", specName: "huge", expectedKind: apperror.KindInvalidRequest},
		{name: "空の名前は invalid_request", specName: "", expectedKind: apperror.KindInvalidRequest},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			spec, findError := deployRepository.FindSpec(context.Background(), testCase.specName)
			if apperror.KindOf(findError) != testCase.expectedKind {
				subTest.Fatalf("エラーの種類が違う: 期待 %q、実際 %v", testCase.expectedKind, findError)
			}
			if findError == nil && (spec.CPUMillicores != testCase.expectedCPU || spec.MemoryMiB != testCase.expectedMemory) {
				subTest.Errorf("大きさが違う: %+v", spec)
			}
		})
	}
}

// TestDeployRepositoryCreate は、プロジェクトが active のときだけ作れ、無い・他人のものは not_found、
// 削除中などは conflict になることを確認する。
func TestDeployRepositoryCreate(testingT *testing.T) {
	// user-a の active なプロジェクトに加えて、削除中と、delete_failed のプロジェクトも作る。
	database, deployRepository := newDeployTestEnvironment(testingT)
	deletingProject := newTestProject(2, "user-a", time.Now())
	deletingProject.Status = model.ProjectStatusDeleting
	mustCreate(testingT, database, deletingProject)
	failedProject := newTestProject(3, "user-a", time.Now())
	failedProject.Status = model.ProjectStatusDeleteFailed
	mustCreate(testingT, database, failedProject)

	// プロジェクトと持ち主の組み合わせごとの期待。
	testCases := []struct {
		name         string
		projectID    string
		userID       string
		expectedKind apperror.Kind
	}{
		{name: "active なプロジェクトには作れる", projectID: "project-00000001", userID: "user-a"},
		{name: "他のユーザーのプロジェクトは not_found", projectID: "project-00000001", userID: "user-b", expectedKind: apperror.KindNotFound},
		{name: "存在しないプロジェクトは not_found", projectID: "project-99999999", userID: "user-a", expectedKind: apperror.KindNotFound},
		{name: "削除中のプロジェクトは conflict", projectID: "project-00000002", userID: "user-a", expectedKind: apperror.KindConflict},
		{name: "delete_failed のプロジェクトも conflict", projectID: "project-00000003", userID: "user-a", expectedKind: apperror.KindConflict},
	}
	for index, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			createError := deployRepository.Create(context.Background(), newTestDeploy(index+1, testCase.projectID, testCase.userID))
			if apperror.KindOf(createError) != testCase.expectedKind {
				subTest.Errorf("エラーの種類が違う: 期待 %q、実際 %v", testCase.expectedKind, createError)
			}
		})
	}
}

// TestDeployRepositoryFindByID は、持ち主は取得でき、他のユーザーと存在しない ID は not_found になることを確認する。
func TestDeployRepositoryFindByID(testingT *testing.T) {
	// deploy を1件作る。
	database, deployRepository := newDeployTestEnvironment(testingT)
	if createError := deployRepository.Create(context.Background(), newTestDeploy(1, "project-00000001", "user-a")); createError != nil {
		testingT.Fatalf("作成できませんでした: %v", createError)
	}
	_ = database

	// 持ち主・他のユーザー・存在しない ID の場合。
	testCases := []struct {
		name         string
		userID       string
		deployID     string
		expectedKind apperror.Kind
	}{
		{name: "持ち主は取得できる", userID: "user-a", deployID: "deploy-00000001"},
		{name: "他のユーザーは not_found", userID: "user-b", deployID: "deploy-00000001", expectedKind: apperror.KindNotFound},
		{name: "存在しない ID は not_found", userID: "user-a", deployID: "deploy-99999999", expectedKind: apperror.KindNotFound},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			deploy, findError := deployRepository.FindByID(context.Background(), testCase.userID, testCase.deployID)
			if apperror.KindOf(findError) != testCase.expectedKind {
				subTest.Fatalf("エラーの種類が違う: 期待 %q、実際 %v", testCase.expectedKind, findError)
			}
			if findError == nil && (deploy.ImageRef != "registry.example/app:1" || deploy.Status != model.DeployStatusPending || deploy.Replicas != 1) {
				subTest.Errorf("内容が違う: %+v", deploy)
			}
		})
	}
}

// TestDeployRepositoryUpdateStatus は、期待する状態のときだけ状態が変わり、
// 別の処理が先に変えていたときは、何も変えずに false を返すことを確認する。
func TestDeployRepositoryUpdateStatus(testingT *testing.T) {
	// pending の deploy を1件作る。
	_, deployRepository := newDeployTestEnvironment(testingT)
	if createError := deployRepository.Create(context.Background(), newTestDeploy(1, "project-00000001", "user-a")); createError != nil {
		testingT.Fatalf("作成できませんでした: %v", createError)
	}

	// 期待が合えば変わる。
	testingT.Run("期待する状態なら変わる", func(subTest *testing.T) {
		changed, updateError := deployRepository.UpdateStatus(context.Background(), "deploy-00000001", model.DeployStatusPending, model.DeployStatusActive)
		if updateError != nil || !changed {
			subTest.Fatalf("変わるはず: %v %v", changed, updateError)
		}
		deploy, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
		if deploy.Status != model.DeployStatusActive {
			subTest.Errorf("active になるはず: %q", deploy.Status)
		}
	})
	// 期待が合わなければ変わらない(古い結果で上書きしない)。
	testingT.Run("期待する状態でなければ、変えずに false", func(subTest *testing.T) {
		changed, updateError := deployRepository.UpdateStatus(context.Background(), "deploy-00000001", model.DeployStatusPending, model.DeployStatusFailed)
		if updateError != nil || changed {
			subTest.Fatalf("変わらないはず: %v %v", changed, updateError)
		}
		deploy, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
		if deploy.Status != model.DeployStatusActive {
			subTest.Errorf("active のままのはず: %q", deploy.Status)
		}
	})
	// 無い deploy は、false になる。
	testingT.Run("無い deploy は false", func(subTest *testing.T) {
		changed, updateError := deployRepository.UpdateStatus(context.Background(), "deploy-99999999", model.DeployStatusPending, model.DeployStatusActive)
		if updateError != nil || changed {
			subTest.Errorf("false のはず: %v %v", changed, updateError)
		}
	})
}

// TestDeployRepositoryDeleteUnstarted は、pending の deploy だけが物理削除され、
// 起動済み(active)のものは消えず、繰り返しても壊れないことを確認する。
func TestDeployRepositoryDeleteUnstarted(testingT *testing.T) {
	// pending と active の deploy を1件ずつ作る。
	_, deployRepository := newDeployTestEnvironment(testingT)
	for number := 1; number <= 2; number++ {
		if createError := deployRepository.Create(context.Background(), newTestDeploy(number, "project-00000001", "user-a")); createError != nil {
			testingT.Fatalf("作成できませんでした: %v", createError)
		}
	}
	if _, updateError := deployRepository.UpdateStatus(context.Background(), "deploy-00000002", model.DeployStatusPending, model.DeployStatusActive); updateError != nil {
		testingT.Fatalf("状態を変えられませんでした: %v", updateError)
	}

	// active のものは消えない。
	testingT.Run("active の deploy は消えない", func(subTest *testing.T) {
		if deleteError := deployRepository.DeleteUnstarted(context.Background(), "deploy-00000002"); deleteError != nil {
			subTest.Fatalf("エラーにならないはず: %v", deleteError)
		}
		if _, findError := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000002"); findError != nil {
			subTest.Errorf("消えていないはず: %v", findError)
		}
	})
	// pending のものは消え、2回目もエラーにならない。
	testingT.Run("pending の deploy は消え、2回目もエラーにならない", func(subTest *testing.T) {
		for range 2 {
			if deleteError := deployRepository.DeleteUnstarted(context.Background(), "deploy-00000001"); deleteError != nil {
				subTest.Fatalf("エラーにならないはず: %v", deleteError)
			}
		}
		_, findError := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
		if apperror.KindOf(findError) != apperror.KindNotFound {
			subTest.Errorf("not_found のはず: %v", findError)
		}
	})
}

// TestDeployRepositoryCommitConfigResult は、反映の結果の確定で、成功なら正式な値が pending の内容になって
// pending が空に戻り、失敗なら正式な値も pending も変わらず failed になることを確認する。
func TestDeployRepositoryCommitConfigResult(testingT *testing.T) {
	// 結果ごとの期待。
	testCases := []struct {
		name           string
		succeeded      bool
		replicas       int
		expectedStatus string
	}{
		{name: "成功なら active", succeeded: true, replicas: 1, expectedStatus: model.DeployStatusActive},
		{name: "成功で replicas が 0 なら scaled_zero", succeeded: true, replicas: 0, expectedStatus: model.DeployStatusScaledZero},
		{name: "失敗なら、正式な値も pending も変えず、failed", succeeded: false, replicas: 1, expectedStatus: model.DeployStatusFailed},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// active の deploy に、pending を入れて、反映を始める。
			database, deployRepository := newDeployTestEnvironment(subTest)
			database.Create(newTestDeploy(1, "project-00000001", "user-a"))
			database.Exec("UPDATE deploys SET status = 'active', replicas = ? WHERE id = 'deploy-00000001'", testCase.replicas)
			command := "/app/server"
			if saveError := deployRepository.SavePendingConfig(context.Background(), "user-a", "deploy-00000001", datatypes.JSON(`{"A":"1"}`), &command, datatypes.JSON(`["x"]`)); saveError != nil {
				subTest.Fatalf("保存できませんでした: %v", saveError)
			}
			if _, beginError := deployRepository.BeginConfigApply(context.Background(), "user-a", "deploy-00000001"); beginError != nil {
				subTest.Fatalf("開始できませんでした: %v", beginError)
			}

			// 確定する。
			committed, commitError := deployRepository.CommitConfigResult(context.Background(), "deploy-00000001", testCase.succeeded)
			if commitError != nil || !committed {
				subTest.Fatalf("確定できるはず: %v %v", committed, commitError)
			}
			deploy, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
			if deploy.Status != testCase.expectedStatus {
				subTest.Errorf("status が違う: 期待 %q、実際 %q", testCase.expectedStatus, deploy.Status)
			}

			if testCase.succeeded {
				// 成功: 正式な値が pending の内容になり、pending は空になる。
				if !sameJSON(deploy.Env, []byte(`{"A":"1"}`)) || deploy.Command == nil || *deploy.Command != "/app/server" || string(deploy.Args) != `["x"]` {
					subTest.Errorf("正式な値が pending の内容になっていない: %s %v %s", deploy.Env, deploy.Command, deploy.Args)
				}
				if len(deploy.PendingEnv) != 0 || deploy.PendingCommand != nil || len(deploy.PendingArgs) != 0 {
					subTest.Errorf("pending が空に戻っていない: %s %v %s", deploy.PendingEnv, deploy.PendingCommand, deploy.PendingArgs)
				}
			} else {
				// 失敗: 正式な値は元のまま({}、command と args は NULL)で、pending は残る。
				if !sameJSON(deploy.Env, []byte(`{}`)) || deploy.Command != nil || len(deploy.Args) != 0 {
					subTest.Errorf("正式な値は変わらないはず: %s %v %s", deploy.Env, deploy.Command, deploy.Args)
				}
				if !sameJSON(deploy.PendingEnv, []byte(`{"A":"1"}`)) || deploy.PendingCommand == nil || string(deploy.PendingArgs) != `["x"]` {
					subTest.Errorf("pending は残るはず: %s %v %s", deploy.PendingEnv, deploy.PendingCommand, deploy.PendingArgs)
				}
			}
			// もう一度確定しても、何も変わらない(冪等)。
			if again, _ := deployRepository.CommitConfigResult(context.Background(), "deploy-00000001", !testCase.succeeded); again {
				subTest.Errorf("applying でないので、確定されないはず")
			}
		})
	}
}

// TestDeployRepositorySavePendingConfigSameAsCurrent は、現在の値と同じ内容を保存すると、
// pending が保存されず(以前の pending も消えて)、変更なしとして扱われることを確認する。
func TestDeployRepositorySavePendingConfigSameAsCurrent(testingT *testing.T) {
	// 現在の値が、env が {"A":"1"}、command が /x、args が ["y"] の deploy を用意する。
	database, deployRepository := newDeployTestEnvironment(testingT)
	database.Create(newTestDeploy(1, "project-00000001", "user-a"))
	database.Exec(`UPDATE deploys SET status = 'active', env = '{"A":"1"}', command = '/x', args = '["y"]' WHERE id = 'deploy-00000001'`)
	command := "/x"
	otherCommand := "/z"

	// 比べ方ごとの期待(pending が保存されるか)。
	testCases := []struct {
		name            string
		env             string
		command         *string
		args            string
		expectedPending bool
	}{
		{name: "完全に同じなら保存しない", env: `{"A":"1"}`, command: &command, args: `["y"]`, expectedPending: false},
		{name: "JSON の書式(空白)が違うだけなら同じとみなす", env: `{ "A" : "1" }`, command: &command, args: `[ "y" ]`, expectedPending: false},
		{name: "環境変数が違えば保存する", env: `{"A":"2"}`, command: &command, args: `["y"]`, expectedPending: true},
		{name: "コマンドが違えば保存する", env: `{"A":"1"}`, command: &otherCommand, args: `["y"]`, expectedPending: true},
		{name: "コマンドを既定(null)に戻すなら保存する", env: `{"A":"1"}`, command: nil, args: `["y"]`, expectedPending: true},
		{name: "引数が違えば保存する", env: `{"A":"1"}`, command: &command, args: `["y","w"]`, expectedPending: true},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// 先に、別の内容の pending を入れておく(同じ内容の保存で、消えることを見るため)。
			database.Exec(`UPDATE deploys SET pending_env = '{"OLD":"1"}' WHERE id = 'deploy-00000001'`)
			var args datatypes.JSON
			if testCase.args != "" {
				args = datatypes.JSON(testCase.args)
			}

			saveError := deployRepository.SavePendingConfig(context.Background(), "user-a", "deploy-00000001", datatypes.JSON(testCase.env), testCase.command, args)

			deploy, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
			if saveError != nil || (len(deploy.PendingEnv) != 0) != testCase.expectedPending {
				subTest.Errorf("pending の有無が違う(期待 %v): %v %s", testCase.expectedPending, saveError, deploy.PendingEnv)
			}
		})
	}
}

// TestDeployRepositoryBeginConfigApply は、反映の開始で、状態が applying になり、元の状態が返ることと、
// 受け付けない状態を確認する。
func TestDeployRepositoryBeginConfigApply(testingT *testing.T) {
	// 開始できる状態と、できない状態。
	testCases := []struct {
		name          string
		status        string
		withPending   bool
		expectedKind  apperror.Kind
		expectedError bool
	}{
		{name: "active で pending があれば開始できる", status: "active", withPending: true},
		{name: "failed で pending があれば開始できる", status: "failed", withPending: true},
		{name: "pending が無ければ conflict", status: "active", withPending: false, expectedError: true, expectedKind: apperror.KindConflict},
		{name: "applying なら conflict", status: "applying", withPending: true, expectedError: true, expectedKind: apperror.KindConflict},
		{name: "起動中(pending)なら conflict", status: "pending", withPending: true, expectedError: true, expectedKind: apperror.KindConflict},
		{name: "deleting なら conflict", status: "deleting", withPending: true, expectedError: true, expectedKind: apperror.KindConflict},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			database, deployRepository := newDeployTestEnvironment(subTest)
			database.Create(newTestDeploy(1, "project-00000001", "user-a"))
			database.Exec("UPDATE deploys SET status = ? WHERE id = 'deploy-00000001'", testCase.status)
			if testCase.withPending {
				database.Exec(`UPDATE deploys SET pending_env = '{}' WHERE id = 'deploy-00000001'`)
			}

			previousStatus, beginError := deployRepository.BeginConfigApply(context.Background(), "user-a", "deploy-00000001")

			deploy, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
			if testCase.expectedError {
				// 拒否されたときは、状態が変わらない。
				if apperror.KindOf(beginError) != testCase.expectedKind || deploy.Status != testCase.status {
					subTest.Errorf("拒否され、状態は変わらないはず: %v %q", beginError, deploy.Status)
				}
				return
			}
			// 開始できたときは、applying になり、元の状態が返る。
			if beginError != nil || previousStatus != testCase.status || deploy.Status != model.DeployStatusApplying {
				subTest.Errorf("applying になり、元の状態が返るはず: %v %q %q", beginError, previousStatus, deploy.Status)
			}
		})
	}
}

// TestDeployRepositoryBeginScaleAndRevert は、スケーリングの開始で replicas と状態が変わり(0 にもできる)、
// 同じ数なら変わらず、受け付けない状態は拒否され、元に戻せることを確認する。
func TestDeployRepositoryBeginScaleAndRevert(testingT *testing.T) {
	// 開始の入力ごとの期待。
	testCases := []struct {
		name             string
		status           string
		replicas         int
		newReplicas      int
		expectedKind     apperror.Kind
		expectedChanged  bool
		expectedReplicas int
		expectedStatus   string
	}{
		{name: "active で数を変えれば、applying になる", status: "active", replicas: 1, newReplicas: 3, expectedChanged: true, expectedReplicas: 3, expectedStatus: "applying"},
		{name: "0 にできる", status: "active", replicas: 2, newReplicas: 0, expectedChanged: true, expectedReplicas: 0, expectedStatus: "applying"},
		{name: "scaled_zero から増やせる", status: "scaled_zero", replicas: 0, newReplicas: 1, expectedChanged: true, expectedReplicas: 1, expectedStatus: "applying"},
		{name: "同じ数なら変えない", status: "active", replicas: 2, newReplicas: 2, expectedChanged: false, expectedReplicas: 2, expectedStatus: "active"},
		{name: "failed なら、同じ数でもやり直す", status: "failed", replicas: 2, newReplicas: 2, expectedChanged: true, expectedReplicas: 2, expectedStatus: "applying"},
		{name: "applying なら conflict", status: "applying", replicas: 1, newReplicas: 2, expectedKind: apperror.KindConflict, expectedReplicas: 1, expectedStatus: "applying"},
		{name: "起動中(pending)なら conflict", status: "pending", replicas: 1, newReplicas: 2, expectedKind: apperror.KindConflict, expectedReplicas: 1, expectedStatus: "pending"},
		{name: "deleting なら conflict", status: "deleting", replicas: 1, newReplicas: 2, expectedKind: apperror.KindConflict, expectedReplicas: 1, expectedStatus: "deleting"},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			database, deployRepository := newDeployTestEnvironment(subTest)
			database.Create(newTestDeploy(1, "project-00000001", "user-a"))
			database.Exec("UPDATE deploys SET status = ?, replicas = ? WHERE id = 'deploy-00000001'", testCase.status, testCase.replicas)

			start, beginError := deployRepository.BeginScale(context.Background(), "user-a", "deploy-00000001", testCase.newReplicas)

			deploy, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
			if apperror.KindOf(beginError) != testCase.expectedKind || deploy.Replicas != testCase.expectedReplicas || deploy.Status != testCase.expectedStatus {
				subTest.Fatalf("結果が違う: %v replicas=%d status=%q", beginError, deploy.Replicas, deploy.Status)
			}
			if beginError != nil {
				return
			}
			if start.Changed != testCase.expectedChanged || start.PreviousReplicas != testCase.replicas || start.PreviousStatus != testCase.status {
				subTest.Errorf("変える前の値が違う: %+v", start)
			}
			// 変えたときは、元に戻せる。
			if testCase.expectedChanged {
				if revertError := deployRepository.RevertScale(context.Background(), "deploy-00000001", start); revertError != nil {
					subTest.Fatalf("戻せるはず: %v", revertError)
				}
				reverted, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
				if reverted.Replicas != testCase.replicas || reverted.Status != testCase.status {
					subTest.Errorf("元に戻るはず: replicas=%d status=%q", reverted.Replicas, reverted.Status)
				}
			}
		})
	}
}

// TestDeployRepositoryDeletionFlow は、削除の受け付け(状態ごとの可否)、巻き戻し、失敗の記録、
// レコードの物理削除(削除中のものだけ)を確認する。
func TestDeployRepositoryDeletionFlow(testingT *testing.T) {
	// 受け付けの可否。
	acceptCases := []struct {
		status       string
		expectedKind apperror.Kind
	}{
		{status: "active"}, {status: "scaled_zero"}, {status: "failed"}, {status: "delete_failed"},
		{status: "pending", expectedKind: apperror.KindConflict},
		{status: "applying", expectedKind: apperror.KindConflict},
		{status: "deleting", expectedKind: apperror.KindConflict},
	}
	for _, testCase := range acceptCases {
		testingT.Run("受け付け: "+testCase.status, func(subTest *testing.T) {
			database, deployRepository := newDeployTestEnvironment(subTest)
			database.Create(newTestDeploy(1, "project-00000001", "user-a"))
			database.Exec("UPDATE deploys SET status = ? WHERE id = 'deploy-00000001'", testCase.status)

			previousStatus, markError := deployRepository.MarkDeleting(context.Background(), "user-a", "deploy-00000001")

			deploy, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
			if apperror.KindOf(markError) != testCase.expectedKind {
				subTest.Fatalf("エラーの種類が違う: %v", markError)
			}
			if testCase.expectedKind != "" {
				// 拒否されたときは、状態が変わらない。
				if deploy.Status != testCase.status {
					subTest.Errorf("状態は変わらないはず: %q", deploy.Status)
				}
				return
			}
			// 受け付けられたときは、deleting になり、元の状態が返る。
			if deploy.Status != "deleting" || previousStatus != testCase.status {
				subTest.Errorf("deleting になり、元の状態が返るはず: %q %q", deploy.Status, previousStatus)
			}
			// 巻き戻すと、元の状態になる。
			if revertError := deployRepository.RevertDeleting(context.Background(), "deploy-00000001", previousStatus); revertError != nil {
				subTest.Fatalf("戻せるはず: %v", revertError)
			}
			reverted, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
			if reverted.Status != testCase.status {
				subTest.Errorf("元の状態に戻るはず: %q", reverted.Status)
			}
		})
	}
	// 他人の deploy は not_found。
	testingT.Run("他人の deploy は not_found", func(subTest *testing.T) {
		database, deployRepository := newDeployTestEnvironment(subTest)
		database.Create(newTestDeploy(1, "project-00000001", "user-a"))
		if _, markError := deployRepository.MarkDeleting(context.Background(), "user-b", "deploy-00000001"); apperror.KindOf(markError) != apperror.KindNotFound {
			subTest.Errorf("not_found のはず: %v", markError)
		}
	})
	// 失敗の記録とレコードの削除は、削除中のものだけが対象。
	testingT.Run("失敗の記録とレコードの削除は、削除中の deploy だけが対象で、繰り返しても壊れない", func(subTest *testing.T) {
		database, deployRepository := newDeployTestEnvironment(subTest)
		database.Create(newTestDeploy(1, "project-00000001", "user-a"))
		database.Create(newTestDeploy(2, "project-00000001", "user-a"))
		database.Exec("UPDATE deploys SET status = 'deleting' WHERE id = 'deploy-00000001'")
		database.Exec("UPDATE deploys SET status = 'active' WHERE id = 'deploy-00000002'")

		// active のものには、効かない。
		if markError := deployRepository.MarkDeleteFailed(context.Background(), "deploy-00000002"); markError != nil {
			subTest.Fatalf("エラーにならないはず: %v", markError)
		}
		if deleteError := deployRepository.DeleteRecord(context.Background(), "deploy-00000002"); deleteError != nil {
			subTest.Fatalf("エラーにならないはず: %v", deleteError)
		}
		if untouched, findError := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000002"); findError != nil || untouched.Status != "active" {
			subTest.Fatalf("active のものは変わらず残るはず: %v %q", findError, untouched.Status)
		}
		// 削除中のものは、delete_failed になり、その状態では消えない。
		if markError := deployRepository.MarkDeleteFailed(context.Background(), "deploy-00000001"); markError != nil {
			subTest.Fatalf("記録できるはず: %v", markError)
		}
		failed, _ := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001")
		if failed.Status != "delete_failed" {
			subTest.Fatalf("delete_failed になるはず: %q", failed.Status)
		}
		_ = deployRepository.DeleteRecord(context.Background(), "deploy-00000001")
		if _, findError := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001"); findError != nil {
			subTest.Fatalf("delete_failed のものは消えないはず: %v", findError)
		}
		// 削除中に戻して消すと、消える。繰り返しても壊れない。
		database.Exec("UPDATE deploys SET status = 'deleting' WHERE id = 'deploy-00000001'")
		for attempt := 0; attempt < 2; attempt++ {
			if deleteError := deployRepository.DeleteRecord(context.Background(), "deploy-00000001"); deleteError != nil {
				subTest.Fatalf("消せるはず(%d回目): %v", attempt+1, deleteError)
			}
		}
		if _, findError := deployRepository.FindByID(context.Background(), "user-a", "deploy-00000001"); apperror.KindOf(findError) != apperror.KindNotFound {
			subTest.Errorf("消えているはず: %v", findError)
		}
	})
}
