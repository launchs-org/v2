package service

import (
	"context"
	"errors"
	"testing"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// routedRules は、反映済みのルールがある ingress の Rules(JSON)。IngressRoute があるものとして扱われる。
var routedRules = []byte(`[{"path":"/","serviceId":"service-1","port":80}]`)

// fakeDomainReader は、テスト用の IngressDomainReader。決めた ingress を返す。
type fakeDomainReader struct {
	// ingress は、返す ingress。
	ingress model.Ingress
}

// FindByID は、決めた ingress を返す。
func (reader *fakeDomainReader) FindByID(_ context.Context, _ string, _ string, _ string) (model.Ingress, error) {
	// 引数に関わらず、決めた ingress を返す。
	return reader.ingress, nil
}

// fakeDomainStore は、テスト用の IngressDomainStore(メモリの上に、1 件だけ持つ)。
type fakeDomainStore struct {
	// domain は、持っているホスト名。
	domain model.IngressDomain

	// deleted は、Delete が呼ばれた回数。
	deleted int
}

// Create は、渡されたホスト名を覚える。
func (store *fakeDomainStore) Create(_ context.Context, _ string, _ string, domain *model.IngressDomain) error {
	// 覚える。
	store.domain = *domain
	return nil
}

// List は、持っているホスト名を返す。
func (store *fakeDomainStore) List(_ context.Context, _ string, _ string, _ string) ([]model.IngressDomain, error) {
	// 1 件だけ返す。
	return []model.IngressDomain{store.domain}, nil
}

// Delete は、持っているホスト名を返して、呼ばれた回数を数える。
func (store *fakeDomainStore) Delete(_ context.Context, _ string, _ string, _ string, _ string) (model.IngressDomain, error) {
	// 回数を数えて、持っているものを返す。
	store.deleted++
	return store.domain, nil
}

// fakeReapplyStarter は、IngressWorkflowStarter のうち、作り直しだけを数える。
type fakeReapplyStarter struct {
	// reapplied は、作り直しを依頼した回数。
	reapplied int

	// failReapply が true のとき、作り直しの開始が失敗する。
	failReapply bool
}

// StartRulesApply は、使わない。
func (starter *fakeReapplyStarter) StartRulesApply(_ context.Context, _ string) error { return nil }

// StartRouteReapply は、依頼の回数を数える(failReapply なら、失敗する)。
func (starter *fakeReapplyStarter) StartRouteReapply(_ context.Context, _ string) error {
	// 失敗を再現するときは、エラーを返す。
	if starter.failReapply {
		return errors.New("開始できません")
	}
	// 回数を増やす。
	starter.reapplied++
	return nil
}

// StartIngressDeletion は、使わない。
func (starter *fakeReapplyStarter) StartIngressDeletion(_ context.Context, _ string) error {
	return nil
}

// fakeSubdomainPolicy は、テスト用の IngressSubdomainPolicy(ベースドメインは shconn.org、予約語は www と api)。
type fakeSubdomainPolicy struct{}

// BaseDomain は、テスト用のベースドメインを返す。
func (fakeSubdomainPolicy) BaseDomain() string { return "shconn.org" }

// IsReserved は、www と api を予約語として扱う。
func (fakeSubdomainPolicy) IsReserved(subdomain string) bool { return subdomain == "www" || subdomain == "api" }

// newTestDomainService は、テスト用の service と、差し替えたものを返す。
func newTestDomainService(ingress model.Ingress) (*IngressDomainService, *fakeDomainStore, *fakeReapplyStarter) {
	store := &fakeDomainStore{}
	starter := &fakeReapplyStarter{}
	return NewIngressDomainService(&fakeDomainReader{ingress: ingress}, store, starter, fakeSubdomainPolicy{}), store, starter
}

// TestIngressDomainServiceAdd は、追加のときの検査(形、予約語、削除中)と、すぐに有効になることを確かめる。
func TestIngressDomainServiceAdd(testingT *testing.T) {
	ctx := context.Background()
	routed := model.Ingress{ID: "ingress-1", Host: "a.shconn.org", Status: model.IngressStatusActive, Rules: routedRules}
	service, store, starter := newTestDomainService(routed)
	// 整えて保存され、すぐに有効になり、ルーティングの作り直しが頼まれる。
	domain, _, addError := service.Add(ctx, "user-a", "project-1", "ingress-1", " MyApp ")
	if addError != nil || domain.Hostname != "myapp.shconn.org" || !store.domain.IsVerified() || starter.reapplied != 1 {
		testingT.Fatalf("整えて追加でき、すぐに有効になるはず: %v %+v 作り直し=%d", addError, domain, starter.reapplied)
	}
	// 末尾に、ベースドメインが付いていても、受け付ける。
	if suffixed, _, suffixError := service.Add(ctx, "user-a", "project-1", "ingress-1", "other.shconn.org"); suffixError != nil || suffixed.Hostname != "other.shconn.org" {
		testingT.Errorf("ベースドメイン付きでも追加できるはず: %v %+v", suffixError, suffixed)
	}
	// 形が不正なもの(ドット入り、外部のドメイン、短い、日本語)と、予約語は、拒否する。
	var appError *apperror.Error
	for _, invalid := range []string{"a.b", "app.example.com", "ab", "あいう", "My_App", "www", "api", ""} {
		if _, _, invalidError := service.Add(ctx, "user-a", "project-1", "ingress-1", invalid); !errors.As(invalidError, &appError) || appError.Kind != apperror.KindInvalidRequest {
			testingT.Errorf("%q は invalid_request のはず: %v", invalid, invalidError)
		}
	}
	// 削除中の ingress には、追加できない。
	deleting, _, _ := newTestDomainService(model.Ingress{ID: "ingress-1", Status: model.IngressStatusDeleting})
	if _, _, deletingError := deleting.Add(ctx, "user-a", "project-1", "ingress-1", "myapp"); !errors.As(deletingError, &appError) || appError.Kind != apperror.KindConflict {
		testingT.Errorf("削除中は conflict のはず: %v", deletingError)
	}
	// ルールが無い(IngressRoute が無い)ingress には、作り直しを頼まない(あとでルールを反映するときに入る)。
	unrouted, _, unroutedStarter := newTestDomainService(model.Ingress{ID: "ingress-1", Status: model.IngressStatusActive, Rules: []byte("[]")})
	if _, _, unroutedError := unrouted.Add(ctx, "user-a", "project-1", "ingress-1", "myapp"); unroutedError != nil || unroutedStarter.reapplied != 0 {
		testingT.Errorf("ルールが無ければ、作り直さないはず: %v %d", unroutedError, unroutedStarter.reapplied)
	}
}

// TestIngressDomainServiceAddRollback は、ルーティングの作り直しを始められないとき、追加を取り消すことを確かめる。
func TestIngressDomainServiceAddRollback(testingT *testing.T) {
	ctx := context.Background()
	service, store, starter := newTestDomainService(model.Ingress{ID: "ingress-1", Host: "a.shconn.org", Status: model.IngressStatusActive, Rules: routedRules})
	// 作り直しを失敗させる。
	starter.failReapply = true
	_, _, addError := service.Add(ctx, "user-a", "project-1", "ingress-1", "myapp")
	var appError *apperror.Error
	if !errors.As(addError, &appError) || appError.Kind != apperror.KindInternal {
		testingT.Fatalf("開始できなければ internal のはず: %v", addError)
	}
	// 保存したホスト名は、取り消される。
	if store.deleted != 1 {
		testingT.Errorf("追加を取り消すはず: %d", store.deleted)
	}
}

// TestIngressDomainServiceNoBaseDomain は、ベースドメインの決まりが無いとき、追加を断ることを確かめる。
func TestIngressDomainServiceNoBaseDomain(testingT *testing.T) {
	service := NewIngressDomainService(&fakeDomainReader{}, &fakeDomainStore{}, &fakeReapplyStarter{}, nil)
	var appError *apperror.Error
	if _, _, addError := service.Add(context.Background(), "user-a", "project-1", "ingress-1", "myapp"); !errors.As(addError, &appError) || appError.Kind != apperror.KindInternal {
		testingT.Errorf("決まりが無ければ internal のはず: %v", addError)
	}
	if service.BaseDomain() != "" {
		testingT.Errorf("ベースドメインは空のはず: %q", service.BaseDomain())
	}
}

// TestIngressDomainServiceRemove は、外したときに、ルーティングの作り直しを頼むことを確かめる。
func TestIngressDomainServiceRemove(testingT *testing.T) {
	ctx := context.Background()
	service, store, starter := newTestDomainService(model.Ingress{ID: "ingress-1", Status: model.IngressStatusActive, Rules: routedRules})
	// 有効だったホスト名を外すと、ルーティングを作り直す。
	verified, _, _ := service.Add(ctx, "user-a", "project-1", "ingress-1", "myapp")
	starter.reapplied = 0
	if removeError := service.Remove(ctx, "user-a", "project-1", "ingress-1", verified.ID); removeError != nil || starter.reapplied != 1 {
		testingT.Errorf("外すと作り直すはず: %v %d", removeError, starter.reapplied)
	}
	// 確認待ち(今は作られないが、古い行が残っていた場合)のものを外しても、作り直さない。
	store.domain.VerifiedAt = nil
	starter.reapplied = 0
	if removeError := service.Remove(ctx, "user-a", "project-1", "ingress-1", verified.ID); removeError != nil || starter.reapplied != 0 {
		testingT.Errorf("確認待ちを外しても、作り直さないはず: %v %d", removeError, starter.reapplied)
	}
}
