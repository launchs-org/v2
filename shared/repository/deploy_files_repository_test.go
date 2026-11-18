package repository

import (
	"context"
	"testing"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// newFilesTestEnvironment は、active な deploy(deploy-00000001)が 1 件ある、使い捨てのデータベースと、リポジトリを作る。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - DeployRepository(データベースは、その中)。
func newFilesTestEnvironment(testingT *testing.T) *DeployRepository {
	// プロジェクトと、deploy を作り、deploy を active にする(起動中のままだと、反映を始められない)。
	database, deployRepository := newDeployTestEnvironment(testingT)
	deploy := newTestDeploy(1, "project-00000001", "user-a")
	deploy.Status = model.DeployStatusActive
	if createError := database.Create(deploy).Error; createError != nil {
		testingT.Fatal(createError)
	}
	return deployRepository
}

// TestDeployFilesRepository は、ファイルの保存(pending)、同じ内容なら pending を消す、破棄、反映の開始と確定を確認する。
func TestDeployFilesRepository(testingT *testing.T) {
	deployRepository := newFilesTestEnvironment(testingT)
	ctx := context.Background()
	load := func() model.Deploy {
		deploy, findError := deployRepository.FindByID(ctx, "user-a", "deploy-00000001")
		if findError != nil {
			testingT.Fatal(findError)
		}
		return deploy
	}
	oneFile := []model.FileSpec{{Path: "/etc/app.conf", Content: "key=value\n"}}

	testingT.Run("保存すると pending に入り、files は変わらない", func(subTest *testing.T) {
		if saveError := deployRepository.SavePendingFiles(ctx, "user-a", "deploy-00000001", oneFile); saveError != nil {
			subTest.Fatal(saveError)
		}
		deploy := load()
		if !deploy.HasPendingFiles() || len(deploy.Files) != 0 {
			subTest.Errorf("pending だけに入るはず: files=%s pending=%s", deploy.Files, deploy.PendingFiles)
		}
	})
	testingT.Run("他のユーザーの deploy には、保存も、破棄も、できない(not_found)", func(subTest *testing.T) {
		if apperror.KindOf(deployRepository.SavePendingFiles(ctx, "user-b", "deploy-00000001", oneFile)) != apperror.KindNotFound {
			subTest.Error("保存は not_found のはず")
		}
		if apperror.KindOf(deployRepository.DiscardPendingFiles(ctx, "user-b", "deploy-00000001")) != apperror.KindNotFound {
			subTest.Error("破棄は not_found のはず")
		}
	})
	testingT.Run("破棄すると pending が消え、もう一度破棄すると conflict", func(subTest *testing.T) {
		if discardError := deployRepository.DiscardPendingFiles(ctx, "user-a", "deploy-00000001"); discardError != nil {
			subTest.Fatal(discardError)
		}
		if load().HasPendingFiles() {
			subTest.Error("pending が消えているはず")
		}
		if apperror.KindOf(deployRepository.DiscardPendingFiles(ctx, "user-a", "deploy-00000001")) != apperror.KindConflict {
			subTest.Error("2 回目は conflict のはず")
		}
	})
	testingT.Run("現在と同じ内容(空)を保存すると、pending は付かない", func(subTest *testing.T) {
		if saveError := deployRepository.SavePendingFiles(ctx, "user-a", "deploy-00000001", []model.FileSpec{}); saveError != nil {
			subTest.Fatal(saveError)
		}
		if load().HasPendingFiles() {
			subTest.Error("現在(ファイル無し)と同じなので、pending は付かないはず")
		}
	})
	testingT.Run("反映: 開始で applying、成功で files が pending の内容になり pending が消え、active に戻る", func(subTest *testing.T) {
		if saveError := deployRepository.SavePendingFiles(ctx, "user-a", "deploy-00000001", oneFile); saveError != nil {
			subTest.Fatal(saveError)
		}
		previous, beginError := deployRepository.BeginFilesApply(ctx, "user-a", "deploy-00000001")
		if beginError != nil || previous != model.DeployStatusActive || load().Status != model.DeployStatusApplying {
			subTest.Fatalf("applying になるはず: %q %v %s", previous, beginError, load().Status)
		}
		// 反映中は、保存も、もう一度の開始も、できない(conflict)。
		if apperror.KindOf(deployRepository.SavePendingFiles(ctx, "user-a", "deploy-00000001", oneFile)) != apperror.KindConflict {
			subTest.Error("反映中の保存は conflict のはず")
		}
		if _, secondError := deployRepository.BeginFilesApply(ctx, "user-a", "deploy-00000001"); apperror.KindOf(secondError) != apperror.KindConflict {
			subTest.Errorf("2 回目の開始は conflict のはず: %v", secondError)
		}
		committed, commitError := deployRepository.CommitFilesResult(ctx, "deploy-00000001", true)
		deploy := load()
		parsed, _ := model.ParseFiles(deploy.Files)
		if commitError != nil || !committed || deploy.HasPendingFiles() || deploy.Status != model.DeployStatusActive || len(parsed) != 1 || parsed[0].Path != "/etc/app.conf" {
			subTest.Errorf("確定されるはず: %v %v %s files=%s", commitError, committed, deploy.Status, deploy.Files)
		}
	})
	testingT.Run("失敗の確定では、files と pending は変わらず、failed になる", func(subTest *testing.T) {
		if saveError := deployRepository.SavePendingFiles(ctx, "user-a", "deploy-00000001", []model.FileSpec{}); saveError != nil {
			subTest.Fatal(saveError)
		}
		if _, beginError := deployRepository.BeginFilesApply(ctx, "user-a", "deploy-00000001"); beginError != nil {
			subTest.Fatal(beginError)
		}
		if _, commitError := deployRepository.CommitFilesResult(ctx, "deploy-00000001", false); commitError != nil {
			subTest.Fatal(commitError)
		}
		deploy := load()
		parsed, _ := model.ParseFiles(deploy.Files)
		if deploy.Status != model.DeployStatusFailed || !deploy.HasPendingFiles() || len(parsed) != 1 {
			subTest.Errorf("failed で、files と pending が残るはず: %s files=%s pending=%s", deploy.Status, deploy.Files, deploy.PendingFiles)
		}
	})
}

// TestDeployFilesAndMountsOverlap は、ファイルの path と、マウント先が、まったく同じになる保存を、どちらの側からも拒否し、
// マウント先の下にファイルを置くことは、許すことを確認する。
func TestDeployFilesAndMountsOverlap(testingT *testing.T) {
	deployRepository := newFilesTestEnvironment(testingT)
	ctx := context.Background()
	// マウントの保存は、volume が要るので、ここでは、DB に、pending のマウントを、直接入れる代わりに、ファイル側から確かめる。
	// ファイルの保存が、現在のマウントに照らされることを確かめるため、deploy に、マウントを直接設定する。
	database := deployRepository.database
	if updateError := database.Model(&model.Deploy{}).Where("id = ?", "deploy-00000001").Update("mounts", `[{"volumeId":"volume-a","mountPath":"/data"}]`).Error; updateError != nil {
		testingT.Fatal(updateError)
	}

	testingT.Run("マウント先と、まったく同じ path のファイルは、invalid_request", func(subTest *testing.T) {
		saveError := deployRepository.SavePendingFiles(ctx, "user-a", "deploy-00000001", []model.FileSpec{{Path: "/data", Content: "x"}})
		if apperror.KindOf(saveError) != apperror.KindInvalidRequest {
			subTest.Errorf("invalid_request のはず: %v", saveError)
		}
	})
	testingT.Run("マウント先の下のファイルは、保存できる", func(subTest *testing.T) {
		if saveError := deployRepository.SavePendingFiles(ctx, "user-a", "deploy-00000001", []model.FileSpec{{Path: "/data/init.sql", Content: "SELECT 1;"}}); saveError != nil {
			subTest.Errorf("保存できるはず: %v", saveError)
		}
	})
	testingT.Run("pending のファイルがあるとき、同じ path のマウントは、保存できない(invalid_request)", func(subTest *testing.T) {
		// pending のファイルに、/etc/conf を足してから、マウントを、/etc/conf にする保存を、試みる。
		if saveError := deployRepository.SavePendingFiles(ctx, "user-a", "deploy-00000001", []model.FileSpec{{Path: "/etc/conf", Content: "x"}}); saveError != nil {
			subTest.Fatal(saveError)
		}
		// マウント先の volume が、実在しなくても、重なりの検査は、volume の確認より前に行う必要がある。
		// volume を実在させるため、プロジェクトに volume を作る。
		if createError := database.Create(&model.Volume{ID: "volume-b", ProjectID: "project-00000001", Name: "b", SizeMB: 10, Status: model.VolumeStatusActive}).Error; createError != nil {
			subTest.Fatal(createError)
		}
		saveError := deployRepository.SavePendingMounts(ctx, "user-a", "deploy-00000001", []model.MountSpec{{VolumeID: "volume-b", MountPath: "/etc/conf"}})
		if apperror.KindOf(saveError) != apperror.KindInvalidRequest {
			subTest.Errorf("invalid_request のはず: %v", saveError)
		}
	})
}
