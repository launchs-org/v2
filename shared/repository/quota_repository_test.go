package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"launchs/shared/apperror"
	"launchs/shared/database/testdatabase"
	"launchs/shared/model"
)

// setQuotaOverride は、ユーザーごとの上書きを、1項目だけ設定する(行が無ければ作る)。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - database: 保存先。
//   - userID: ユーザー ID。
//   - column: 列名(max_projects など)。
//   - value: 上限の値。
func setQuotaOverride(testingT *testing.T, database *gorm.DB, userID string, column string, value int) {
	// 失敗したとき、このヘルパーではなく呼び出し元の行が表示されるようにする。
	testingT.Helper()
	// 行を作ってから、列を更新する。
	if execError := database.Exec("INSERT INTO account_quotas (user_id) VALUES (?) ON CONFLICT (user_id) DO NOTHING", userID).Error; execError != nil {
		testingT.Fatal(execError)
	}
	if execError := database.Exec(fmt.Sprintf("UPDATE account_quotas SET %s = ? WHERE user_id = ?", column), value, userID).Error; execError != nil {
		testingT.Fatal(execError)
	}
}

// requireConflictContaining は、conflict のエラーで、メッセージに指定の文字列を含むことを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - checkedError: 確かめるエラー。
//   - piece: メッセージに含まれるはずの文字列。
func requireConflictContaining(testingT *testing.T, checkedError error, piece string) {
	// 失敗したとき、このヘルパーではなく呼び出し元の行が表示されるようにする。
	testingT.Helper()
	// 種類とメッセージを確かめる。
	if apperror.KindOf(checkedError) != apperror.KindConflict || !strings.Contains(checkedError.Error(), piece) {
		testingT.Fatalf("%q を含む conflict のはず: %v", piece, checkedError)
	}
}

// TestQuotaProjectLimit は、プロジェクトの数の上限で作成が conflict になること、deleting は数えないこと、
// 他のユーザーには影響しないこと、上書きが既定より優先されることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaProjectLimit(testingT *testing.T) {
	// データベースを用意し、user-a の上限を2にする。
	database := testdatabase.New(testingT)
	projectRepository := NewProjectRepository(database)
	setQuotaOverride(testingT, database, "user-a", "max_projects", 2)
	ctx := context.Background()
	// 2つまでは作れ、3つ目は conflict になる。
	for number := 1; number <= 2; number++ {
		if createError := projectRepository.Create(ctx, newTestProject(number, "user-a", time.Now())); createError != nil {
			testingT.Fatalf("%d つ目は作れるはず: %v", number, createError)
		}
	}
	requireConflictContaining(testingT, projectRepository.Create(ctx, newTestProject(3, "user-a", time.Now())), "プロジェクトの数が上限(2)")
	// deleting のプロジェクトは数えないので、1つ消す途中にすれば、また作れる。
	database.Exec("UPDATE projects SET status = 'deleting' WHERE id = ?", "project-00000001")
	if createError := projectRepository.Create(ctx, newTestProject(3, "user-a", time.Now())); createError != nil {
		testingT.Fatalf("deleting を数えないので、作れるはず: %v", createError)
	}
	// 他のユーザーは、既定(10)なので、影響されない。
	if createError := projectRepository.Create(ctx, newTestProject(4, "user-b", time.Now())); createError != nil {
		testingT.Fatalf("他のユーザーは作れるはず: %v", createError)
	}
	// 名前の重複は、これまでどおり conflict(クォータのメッセージではない)。
	duplicate := newTestProject(5, "user-b", time.Now())
	duplicate.Name = "app-4"
	if duplicateError := projectRepository.Create(ctx, duplicate); apperror.KindOf(duplicateError) != apperror.KindConflict || !strings.Contains(duplicateError.Error(), "同じ名前") {
		testingT.Fatalf("名前の重複の conflict のはず: %v", duplicateError)
	}
}

// TestQuotaProjectLimitIsRaceFree は、同じユーザーが同時にプロジェクトを作っても、上限を超えないことを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaProjectLimitIsRaceFree(testingT *testing.T) {
	// 上限を3にして、12個を同時に作る。
	database := testdatabase.New(testingT)
	projectRepository := NewProjectRepository(database)
	setQuotaOverride(testingT, database, "user-a", "max_projects", 3)
	var succeeded atomic.Int64
	var group sync.WaitGroup
	for number := 1; number <= 12; number++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if createError := projectRepository.Create(context.Background(), newTestProject(number, "user-a", time.Now())); createError == nil {
				succeeded.Add(1)
			}
		}()
	}
	group.Wait()
	// 作れるのは、ちょうど3つ。
	var stored int64
	database.Model(&model.Project{}).Where("user_id = ?", "user-a").Count(&stored)
	if succeeded.Load() != 3 || stored != 3 {
		testingT.Fatalf("ちょうど3つのはず: 成功 %d、保存 %d", succeeded.Load(), stored)
	}
}

// TestQuotaDeployAndScale は、deploy の数、CPU の合計、メモリの合計の上限と、スケールの増加の判定(減らすのは確かめない)を確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaDeployAndScale(testingT *testing.T) {
	// スペック small は、250 ミリコア、256 MiB。CPU の上限を 600 にする。
	database, deployRepository := newDeployTestEnvironment(testingT)
	ctx := context.Background()
	setQuotaOverride(testingT, database, "user-a", "max_cpu_millicores", 600)
	// 2つ(合計 500)までは作れ、3つ目(750)は、CPU で conflict になる。
	for number := 1; number <= 2; number++ {
		if createError := deployRepository.Create(ctx, newTestDeploy(number, "project-00000001", "user-a")); createError != nil {
			testingT.Fatalf("%d つ目は作れるはず: %v", number, createError)
		}
	}
	requireConflictContaining(testingT, deployRepository.Create(ctx, newTestDeploy(3, "project-00000001", "user-a")), "CPU の合計が上限(600ミリコア)")
	// 状態を active にして、スケールを試す。増やす(1 → 2: 合計が 750)は conflict、変わらない。
	database.Exec("UPDATE deploys SET status = 'active'")
	_, scaleError := deployRepository.BeginScale(ctx, "user-a", "deploy-00000001", 2)
	requireConflictContaining(testingT, scaleError, "CPU の合計")
	unchanged, _ := deployRepository.FindByID(ctx, "user-a", "deploy-00000001")
	if unchanged.Replicas != 1 || unchanged.Status != model.DeployStatusActive {
		testingT.Fatalf("超えたときは、変わらないはず: %d %s", unchanged.Replicas, unchanged.Status)
	}
	// 減らす(0 にする)ときは確かめず、空いた分で、増やせる(2つ目を 0 にしたあと、1つ目を 2 にする: 合計 500)。
	if _, downError := deployRepository.BeginScale(ctx, "user-a", "deploy-00000002", 0); downError != nil {
		testingT.Fatalf("減らせるはず: %v", downError)
	}
	if _, upError := deployRepository.BeginScale(ctx, "user-a", "deploy-00000001", 2); upError != nil {
		testingT.Fatalf("空いた分で、増やせるはず: %v", upError)
	}
	// deleting の deploy は数えない: 1つ目を deleting にすると、新しく作れる。
	database.Exec("UPDATE deploys SET status = 'deleting' WHERE id = 'deploy-00000001'")
	if createError := deployRepository.Create(ctx, newTestDeploy(3, "project-00000001", "user-a")); createError != nil {
		testingT.Fatalf("deleting を数えないので、作れるはず: %v", createError)
	}
	// メモリの上限(256 MiB の small は、300 までに1つ)と、deploy の数の上限も確かめる。
	setQuotaOverride(testingT, database, "user-a", "max_memory_mib", 300)
	database.Exec("DELETE FROM deploys")
	if createError := deployRepository.Create(ctx, newTestDeploy(1, "project-00000001", "user-a")); createError != nil {
		testingT.Fatalf("1つ目は作れるはず: %v", createError)
	}
	requireConflictContaining(testingT, deployRepository.Create(ctx, newTestDeploy(2, "project-00000001", "user-a")), "メモリの合計が上限(300MiB)")
	setQuotaOverride(testingT, database, "user-a", "max_memory_mib", 100000)
	setQuotaOverride(testingT, database, "user-a", "max_deploys", 1)
	requireConflictContaining(testingT, deployRepository.Create(ctx, newTestDeploy(2, "project-00000001", "user-a")), "deploy の数が上限(1)")
}

// TestQuotaVolume は、volume の数と大きさの合計の上限、拡張の pending を予約として数えること、
// 縮小や1つの上限の 400 が、クォータの 409 より先に返ることを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaVolume(testingT *testing.T) {
	// プロジェクトを用意し、合計の上限を 100MB にする。
	database, _ := newDeployTestEnvironment(testingT)
	volumeRepository := NewVolumeRepository(database)
	ctx := context.Background()
	setQuotaOverride(testingT, database, "user-a", "max_volume_total_mb", 100)
	create := func(number int, sizeMB int) error {
		return volumeRepository.Create(ctx, &model.Volume{ID: fmt.Sprintf("volume-%08d", number), ProjectID: "project-00000001", UserID: "user-a", Name: fmt.Sprintf("data-%d", number), SizeMB: sizeMB})
	}
	// 60 は作れ、50(合計 110)は conflict、40(合計 100)は作れる。
	if createError := create(1, 60); createError != nil {
		testingT.Fatalf("作れるはず: %v", createError)
	}
	requireConflictContaining(testingT, create(2, 50), "volume の大きさの合計が上限(100MB)")
	if createError := create(2, 40); createError != nil {
		testingT.Fatalf("ちょうど上限なら、作れるはず: %v", createError)
	}
	// 1つ目を、100 に拡張する保存(合計が 140)は conflict。80 に拡張する保存(合計 120)も conflict。
	requireConflictContaining(testingT, volumeRepository.SavePendingSize(ctx, "user-a", "project-00000001", "volume-00000001", 100, 5120), "volume の大きさの合計")
	requireConflictContaining(testingT, volumeRepository.SavePendingSize(ctx, "user-a", "project-00000001", "volume-00000001", 80, 5120), "volume の大きさの合計")
	// 縮小(400)と、1つの上限(400)は、クォータより先に返る。
	if shrinkError := volumeRepository.SavePendingSize(ctx, "user-a", "project-00000001", "volume-00000001", 10, 5120); apperror.KindOf(shrinkError) != apperror.KindInvalidRequest {
		testingT.Fatalf("縮小は 400 のはず: %v", shrinkError)
	}
	if overError := volumeRepository.SavePendingSize(ctx, "user-a", "project-00000001", "volume-00000001", 6000, 5120); apperror.KindOf(overError) != apperror.KindInvalidRequest {
		testingT.Fatalf("1つの上限は 400 のはず: %v", overError)
	}
	// 2つ目を消して(deleting でも数える)、空いた分の拡張を保存すると、予約になり、他の volume の作成を妨げる。
	database.Exec("DELETE FROM volumes WHERE id = 'volume-00000002'")
	if saveError := volumeRepository.SavePendingSize(ctx, "user-a", "project-00000001", "volume-00000001", 90, 5120); saveError != nil {
		testingT.Fatalf("合計 90 なので、保存できるはず: %v", saveError)
	}
	requireConflictContaining(testingT, create(3, 20), "volume の大きさの合計")
	// 同じ pending を保存し直しても(増えない)、確かめで落ちない。破棄すれば、予約が外れて、作れる。
	if saveError := volumeRepository.SavePendingSize(ctx, "user-a", "project-00000001", "volume-00000001", 90, 5120); saveError != nil {
		testingT.Fatalf("同じ値の保存は通るはず: %v", saveError)
	}
	if discardError := volumeRepository.DiscardPendingSize(ctx, "user-a", "project-00000001", "volume-00000001"); discardError != nil {
		testingT.Fatal(discardError)
	}
	if createError := create(3, 20); createError != nil {
		testingT.Fatalf("予約が外れたので、作れるはず: %v", createError)
	}
	// 数の上限: 3つ目で、volume の数が 2 に達する。
	setQuotaOverride(testingT, database, "user-a", "max_volumes", 2)
	requireConflictContaining(testingT, create(4, 1), "volume の数が上限(2)")
}

// TestQuotaRepositoryGet は、既定の上限と、上書き、使用量が取得できることを確かめる(ロックの行は作らない)。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaRepositoryGet(testingT *testing.T) {
	// プロジェクトと deploy(small × 2 レプリカ)と volume(60 → pending 90)を用意する。
	database, deployRepository := newDeployTestEnvironment(testingT)
	ctx := context.Background()
	deploy := newTestDeploy(1, "project-00000001", "user-a")
	deploy.Replicas = 2
	if createError := deployRepository.Create(ctx, deploy); createError != nil {
		testingT.Fatal(createError)
	}
	volumeRepository := NewVolumeRepository(database)
	_ = volumeRepository.Create(ctx, &model.Volume{ID: "volume-00000001", ProjectID: "project-00000001", UserID: "user-a", Name: "data", SizeMB: 60})
	_ = volumeRepository.SavePendingSize(ctx, "user-a", "project-00000001", "volume-00000001", 90, 5120)
	// 取得する(上書きは、メモリだけ)。
	setQuotaOverride(testingT, database, "user-a", "max_memory_mib", 1000)
	limits, usage, getError := NewQuotaRepository(database).Get(ctx, "user-a")
	if getError != nil {
		testingT.Fatal(getError)
	}
	expectedLimits := model.QuotaLimits{MaxProjects: 10, MaxDeploys: 20, MaxCPUMillicores: 4000, MaxMemoryMiB: 1000, MaxVolumes: 10, MaxVolumeTotalMB: 20480}
	expectedUsage := model.QuotaUsage{Projects: 1, Deploys: 1, CPUMillicores: 500, MemoryMiB: 512, Volumes: 1, VolumeTotalMB: 90}
	if limits != expectedLimits || usage != expectedUsage {
		testingT.Fatalf("上限と使用量が違う:\n上限 %+v\n使用量 %+v", limits, usage)
	}
	// 何もしていないユーザーは、全て既定で、使用量は 0。読むだけなので、行は作らない。
	otherLimits, otherUsage, _ := NewQuotaRepository(database).Get(ctx, "user-new")
	var rows int64
	database.Model(&model.AccountQuota{}).Where("user_id = ?", "user-new").Count(&rows)
	if otherLimits.MaxMemoryMiB != 8192 || otherUsage != (model.QuotaUsage{}) || rows != 0 {
		testingT.Fatalf("既定で、使用量 0、行は作らないはず: %+v %+v %d", otherLimits, otherUsage, rows)
	}
}
