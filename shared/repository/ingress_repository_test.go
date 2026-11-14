package repository

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"launchs/shared/apperror"
	"launchs/shared/database/testdatabase"
	"launchs/shared/model"
)

// newIngressTestEnvironment は、active のプロジェクトと、service(web の 8080 の tcp)を用意し、IngressRepository を返す。
//
// 引数:
//   - testingT: 呼び出し元のテスト。
//
// 戻り値:
//   - IngressRepository、データベース、service の ID。
func newIngressTestEnvironment(testingT *testing.T) (*IngressRepository, *gorm.DB, string) {
	// データベースを用意する。
	database := testdatabase.New(testingT)
	// プロジェクトを作る。
	project := model.Project{ID: "project-00000001", UserID: "user-a", Name: "my-app", Status: model.ProjectStatusActive}
	if createError := database.Create(&project).Error; createError != nil {
		testingT.Fatalf("プロジェクトを作れませんでした: %v", createError)
	}
	// deploy と service を、直接保存する。
	deploy := newTestDeploy(1, project.ID, "user-a")
	if createError := database.Create(deploy).Error; createError != nil {
		testingT.Fatalf("deploy を作れませんでした: %v", createError)
	}
	service := model.Service{ID: model.ServiceIDPrefix + "00000001", DeployID: deploy.ID, ProjectID: project.ID, UserID: "user-a",
		Ports: []byte(`[{"name":"web","port":8080,"protocol":"tcp"}]`), Status: model.ServiceStatusActive}
	if createError := database.Create(&service).Error; createError != nil {
		testingT.Fatalf("service を作れませんでした: %v", createError)
	}
	return NewIngressRepository(database), database, service.ID
}

// newTestIngress は、テスト用の ingress を作る。
//
// 引数:
//   - host: ホスト名。
//
// 戻り値:
//   - 保存前の ingress(ID は host から作る)。
func newTestIngress(host string) *model.Ingress {
	return &model.Ingress{ID: model.IngressIDPrefix + host, ProjectID: "project-00000001", UserID: "user-a", Host: host}
}

// TestIngressRepositoryCreate は、作成の保存・ホスト名の重複・プロジェクトの条件を確認する。
func TestIngressRepositoryCreate(testingT *testing.T) {
	ctx := context.Background()
	ingressRepository, database, _ := newIngressTestEnvironment(testingT)

	// 作成すると、active で、ルールは空の配列になる。
	ingress := newTestIngress("a.launchs.org")
	if createError := ingressRepository.Create(ctx, ingress); createError != nil {
		testingT.Fatalf("作れるはず: %v", createError)
	}
	found, _ := ingressRepository.FindByID(ctx, "user-a", "project-00000001", ingress.ID)
	if found.Status != model.IngressStatusActive || string(found.Rules) != "[]" || found.PendingRules != nil {
		testingT.Errorf("active で、ルールは空のはず: %+v", found)
	}
	// 同じホスト名は、ErrIngressHostTaken。
	if duplicateError := ingressRepository.Create(ctx, newTestIngress("a.launchs.org")); !errors.Is(duplicateError, ErrIngressHostTaken) {
		testingT.Errorf("ErrIngressHostTaken のはず: %v", duplicateError)
	}
	// 他のユーザーのプロジェクトは not_found、削除中は conflict。
	other := newTestIngress("b.launchs.org")
	other.UserID = "user-b"
	var appError *apperror.Error
	if otherError := ingressRepository.Create(ctx, other); !errors.As(otherError, &appError) || appError.Kind != apperror.KindNotFound {
		testingT.Errorf("not_found のはず: %v", otherError)
	}
	database.Exec("UPDATE projects SET status = 'deleting'")
	if deletingError := ingressRepository.Create(ctx, newTestIngress("c.launchs.org")); !errors.As(deletingError, &appError) || appError.Kind != apperror.KindConflict {
		testingT.Errorf("conflict のはず: %v", deletingError)
	}
}

// TestIngressRepositoryApplyLifecycle は、反映の開始・確定(成功と失敗)・戻し、削除の受け付け・戻し・失敗・レコード削除を確認する。
func TestIngressRepositoryApplyLifecycle(testingT *testing.T) {
	ctx := context.Background()
	ingressRepository, _, serviceID := newIngressTestEnvironment(testingT)
	ingress := newTestIngress("a.launchs.org")
	_ = ingressRepository.Create(ctx, ingress)
	rules := []model.IngressRule{{Path: "/", ServiceID: serviceID, Port: 8080}}

	// ルールを保存して、反映を始め、成功を確定すると、ルールが昇格する。
	if saveError := ingressRepository.SavePendingRules(ctx, "user-a", ingress.ProjectID, ingress.ID, rules); saveError != nil {
		testingT.Fatalf("保存できるはず: %v", saveError)
	}
	previous, beginError := ingressRepository.BeginRulesApply(ctx, "user-a", ingress.ProjectID, ingress.ID)
	if beginError != nil || previous != model.IngressStatusActive {
		testingT.Fatalf("反映を始められるはず: %v %q", beginError, previous)
	}
	committed, commitError := ingressRepository.CommitRulesApply(ctx, ingress.ID, true)
	found, _ := ingressRepository.FindForWorker(ctx, ingress.ID)
	if commitError != nil || !committed || found.Status != model.IngressStatusActive || found.PendingRules != nil || len(found.Rules) < 10 {
		testingT.Fatalf("昇格するはず: %v %+v", commitError, found)
	}
	// 確定済みなら、もう一度確定しても、何も変わらない。
	if again, _ := ingressRepository.CommitRulesApply(ctx, ingress.ID, false); again {
		testingT.Error("applying でなければ確定しないはず")
	}

	// 失敗の確定では、ルールと pending は変わらず、failed になる。
	_ = ingressRepository.SavePendingRules(ctx, "user-a", ingress.ProjectID, ingress.ID, []model.IngressRule{})
	_, _ = ingressRepository.BeginRulesApply(ctx, "user-a", ingress.ProjectID, ingress.ID)
	_, _ = ingressRepository.CommitRulesApply(ctx, ingress.ID, false)
	failed, _ := ingressRepository.FindForWorker(ctx, ingress.ID)
	if failed.Status != model.IngressStatusFailed || string(failed.PendingRules) != "[]" || string(failed.Rules) != string(found.Rules) {
		testingT.Errorf("failed で、ルールと pending は変わらないはず: %+v", failed)
	}
	// 開始の戻し(applying のときだけ)。
	_ = ingressRepository.SavePendingRules(ctx, "user-a", ingress.ProjectID, ingress.ID, rules[:0])
	if _, beginError = ingressRepository.BeginRulesApply(ctx, "user-a", ingress.ProjectID, ingress.ID); beginError != nil {
		testingT.Fatalf("failed からも始められるはず: %v", beginError)
	}
	_ = ingressRepository.RevertApply(ctx, ingress.ID, model.IngressStatusFailed)
	reverted, _ := ingressRepository.FindForWorker(ctx, ingress.ID)
	if reverted.Status != model.IngressStatusFailed {
		testingT.Errorf("戻るはず: %s", reverted.Status)
	}

	// 削除: 受け付け → 戻す → 受け付け → 失敗 → もう一度受け付け → レコード削除。
	if _, deleteError := ingressRepository.MarkDeleting(ctx, "user-a", ingress.ProjectID, ingress.ID); deleteError != nil {
		testingT.Fatalf("受け付けられるはず: %v", deleteError)
	}
	_ = ingressRepository.RevertDeleting(ctx, ingress.ID, model.IngressStatusFailed)
	_, _ = ingressRepository.MarkDeleting(ctx, "user-a", ingress.ProjectID, ingress.ID)
	_ = ingressRepository.MarkDeleteFailed(ctx, ingress.ID)
	afterFail, _ := ingressRepository.FindForWorker(ctx, ingress.ID)
	if afterFail.Status != model.IngressStatusFailed {
		testingT.Fatalf("failed のはず: %s", afterFail.Status)
	}
	// 削除中でないときは、レコードを消さない。
	_ = ingressRepository.DeleteRecord(ctx, ingress.ID)
	if _, findError := ingressRepository.FindForWorker(ctx, ingress.ID); findError != nil {
		testingT.Fatalf("削除中でなければ消えないはず: %v", findError)
	}
	_, _ = ingressRepository.MarkDeleting(ctx, "user-a", ingress.ProjectID, ingress.ID)
	_ = ingressRepository.DeleteRecord(ctx, ingress.ID)
	var appError *apperror.Error
	if _, findError := ingressRepository.FindForWorker(ctx, ingress.ID); !errors.As(findError, &appError) || appError.Kind != apperror.KindNotFound {
		testingT.Errorf("消えているはず: %v", findError)
	}
}
