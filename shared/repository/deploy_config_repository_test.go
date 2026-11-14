package repository

import (
	"context"
	"launchs/shared/apperror"
	"launchs/shared/model"
	"testing"

	"gorm.io/datatypes"
)

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
