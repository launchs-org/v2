package repository

import (
	"context"
	"fmt"
	"reflect"
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

// setSpecQuotaOverride は、ユーザーごとの、スペックごとの deploy の数の上限を設定する。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//   - database: 保存先。
//   - userID: ユーザー ID。
//   - spec: スペックの名前。
//   - value: 上限の値。
func setSpecQuotaOverride(testingT *testing.T, database *gorm.DB, userID string, spec string, value int) {
	// 失敗したとき、このヘルパーではなく呼び出し元の行が表示されるようにする。
	testingT.Helper()
	// 行を作る(あれば更新する)。
	if execError := database.Exec("INSERT INTO account_spec_quotas (user_id, spec, max_deploys) VALUES (?, ?, ?) ON CONFLICT (user_id, spec) DO UPDATE SET max_deploys = EXCLUDED.max_deploys", userID, spec, value).Error; execError != nil {
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

// TestQuotaDeployAndScale は、deploy の合計の数と、スペックごとの deploy の数の上限、スペックの既定とユーザーの上書き、
// replicas(スケール)がクォータの計算に関わらないことを確かめる。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
func TestQuotaDeployAndScale(testingT *testing.T) {
	// small を3個までにする。
	database, deployRepository := newDeployTestEnvironment(testingT)
	ctx := context.Background()
	setSpecQuotaOverride(testingT, database, "user-a", "small", 3)
	// 3つまでは作れ、4つ目は、small の deploy の数で conflict になる。
	for number := 1; number <= 3; number++ {
		if createError := deployRepository.Create(ctx, newTestDeploy(number, "project-00000001", "user-a")); createError != nil {
			testingT.Fatalf("%d つ目は作れるはず: %v", number, createError)
		}
	}
	requireConflictContaining(testingT, deployRepository.Create(ctx, newTestDeploy(4, "project-00000001", "user-a")), "small の deploy の数が上限(3)")
	// replicas は数えない: replicas を最大にしても、クォータでは断られず、使用量も変わらない。
	database.Exec("UPDATE deploys SET status = 'active'")
	if _, scaleError := deployRepository.BeginScale(ctx, "user-a", "deploy-00000001", model.MaxReplicas); scaleError != nil {
		testingT.Fatalf("スケールはクォータの対象ではないはず: %v", scaleError)
	}
	_, usage, _ := NewQuotaRepository(database).Get(ctx, "user-a")
	if usage.DeploysBySpec["small"] != 3 {
		testingT.Fatalf("replicas を増やしても、数は 3 のままのはず: %v", usage.DeploysBySpec)
	}
	// replicas の範囲外は、クォータではなく、400。
	if _, rangeError := deployRepository.BeginScale(ctx, "user-a", "deploy-00000002", model.MaxReplicas+1); apperror.KindOf(rangeError) != apperror.KindInvalidRequest {
		testingT.Fatalf("範囲外は 400 のはず: %v", rangeError)
	}
	// deleting の deploy は数えない: 1つ目を deleting にすると、新しく作れる。
	database.Exec("UPDATE deploys SET status = 'deleting' WHERE id = 'deploy-00000001'")
	if createError := deployRepository.Create(ctx, newTestDeploy(4, "project-00000001", "user-a")); createError != nil {
		testingT.Fatalf("deleting を数えないので、作れるはず: %v", createError)
	}
	// 他のスペックは、別々に数える。large は、スペックの既定(5個)に従い、ユーザーの上書き(1個)で置き換わる。
	large := func(number int) *model.Deploy {
		deploy := newTestDeploy(number, "project-00000001", "user-a")
		deploy.Spec = "large"
		return deploy
	}
	for number := 5; number <= 9; number++ {
		if createError := deployRepository.Create(ctx, large(number)); createError != nil {
			testingT.Fatalf("large は既定で 5 個まで作れるはず(%d つ目): %v", number-4, createError)
		}
	}
	requireConflictContaining(testingT, deployRepository.Create(ctx, large(10)), "large の deploy の数が上限(5)")
	database.Exec("DELETE FROM deploys WHERE spec = 'large'")
	setSpecQuotaOverride(testingT, database, "user-a", "large", 1)
	if createError := deployRepository.Create(ctx, large(5)); createError != nil {
		testingT.Fatalf("1個なら作れるはず: %v", createError)
	}
	requireConflictContaining(testingT, deployRepository.Create(ctx, large(6)), "large の deploy の数が上限(1)")
	// 存在しないスペックは、クォータではなく、リクエストの値の不正(400)。
	unknown := newTestDeploy(7, "project-00000001", "user-a")
	unknown.Spec = "unknown"
	if unknownError := deployRepository.Create(ctx, unknown); apperror.KindOf(unknownError) != apperror.KindInvalidRequest {
		testingT.Fatalf("存在しないスペックは 400 のはず: %v", unknownError)
	}
	// deploy の合計の数の上限も確かめる(いまの deploy を全て消して、上限を1にする)。
	database.Exec("DELETE FROM deploys")
	setQuotaOverride(testingT, database, "user-a", "max_deploys", 1)
	if createError := deployRepository.Create(ctx, newTestDeploy(1, "project-00000001", "user-a")); createError != nil {
		testingT.Fatalf("1つ目は作れるはず: %v", createError)
	}
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
	// プロジェクトと deploy(small、replicas 2)と volume(60 → pending 90)を用意する。
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
	// 取得する(上書きは、small の deploy の数だけ)。
	setSpecQuotaOverride(testingT, database, "user-a", "small", 7)
	limits, usage, getError := NewQuotaRepository(database).Get(ctx, "user-a")
	if getError != nil {
		testingT.Fatal(getError)
	}
	expectedInstanceLimits := map[string]int{"small": 7, "medium": 10, "large": 5}
	if limits.MaxProjects != 10 || limits.MaxDeploys != 20 || limits.MaxVolumes != 10 || limits.MaxVolumeTotalMB != 20480 || !reflect.DeepEqual(limits.MaxDeploysBySpec, expectedInstanceLimits) {
		testingT.Fatalf("上限が違う: %+v", limits)
	}
	if usage.Projects != 1 || usage.Deploys != 1 || usage.Volumes != 1 || usage.VolumeTotalMB != 90 || !reflect.DeepEqual(usage.DeploysBySpec, map[string]int{"small": 1}) {
		testingT.Fatalf("使用量が違う: %+v", usage)
	}
	// 何もしていないユーザーは、全て既定で、使用量は 0。読むだけなので、行は作らない。
	otherLimits, otherUsage, _ := NewQuotaRepository(database).Get(ctx, "user-new")
	var rows int64
	database.Model(&model.AccountQuota{}).Where("user_id = ?", "user-new").Count(&rows)
	if otherLimits.MaxDeploysBySpec["small"] != 20 || otherUsage.Projects != 0 || otherUsage.Deploys != 0 || len(otherUsage.DeploysBySpec) != 0 || rows != 0 {
		testingT.Fatalf("既定で、使用量 0、行は作らないはず: %+v %+v %d", otherLimits, otherUsage, rows)
	}
}
