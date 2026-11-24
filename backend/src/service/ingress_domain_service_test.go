package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/repository"
)

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
	// domain は、持っているドメイン。
	domain model.IngressDomain

	// verifyConflict が true のとき、MarkVerified が ErrIngressDomainTaken を返す。
	verifyConflict bool
}

// Create は、渡されたドメインを覚える。
func (store *fakeDomainStore) Create(_ context.Context, _ string, _ string, domain *model.IngressDomain) error {
	// 覚える。
	store.domain = *domain
	return nil
}

// List は、持っているドメインを返す。
func (store *fakeDomainStore) List(_ context.Context, _ string, _ string, _ string) ([]model.IngressDomain, error) {
	// 1 件だけ返す。
	return []model.IngressDomain{store.domain}, nil
}

// Find は、持っているドメインを返す。
func (store *fakeDomainStore) Find(_ context.Context, _ string, _ string, _ string, _ string) (model.IngressDomain, error) {
	// そのまま返す。
	return store.domain, nil
}

// MarkVerified は、確認済みにする(verifyConflict なら、重なりのエラー)。
func (store *fakeDomainStore) MarkVerified(_ context.Context, _ string) error {
	// 重なりを再現するときは、専用のエラーを返す。
	if store.verifyConflict {
		return repository.ErrIngressDomainTaken
	}
	now := time.Now()
	store.domain.VerifiedAt = &now
	return nil
}

// Delete は、持っているドメインを返す。
func (store *fakeDomainStore) Delete(_ context.Context, _ string, _ string, _ string, _ string) (model.IngressDomain, error) {
	// 外したドメインとして返す。
	return store.domain, nil
}

// fakeDomainVerifier は、テスト用の IngressDomainVerifier。決めた結果を返す。
type fakeDomainVerifier struct {
	// ok は、確認できたことにするか。
	ok bool
}

// Verify は、決めた結果を返す。
func (verifier *fakeDomainVerifier) Verify(_ context.Context, _ string, _ string, _ string) (bool, string) {
	// 決めた結果と、理由を返す。
	return verifier.ok, "DNS が合いません"
}

// fakeReapplyStarter は、IngressWorkflowStarter のうち、作り直しだけを数える。
type fakeReapplyStarter struct {
	// reapplied は、作り直しを依頼した回数。
	reapplied int
}

// StartRulesApply は、使わない。
func (starter *fakeReapplyStarter) StartRulesApply(_ context.Context, _ string) error { return nil }

// StartRouteReapply は、依頼の回数を数える。
func (starter *fakeReapplyStarter) StartRouteReapply(_ context.Context, _ string) error {
	// 回数を増やす。
	starter.reapplied++
	return nil
}

// StartIngressDeletion は、使わない。
func (starter *fakeReapplyStarter) StartIngressDeletion(_ context.Context, _ string) error {
	return nil
}

// newTestDomainService は、テスト用の service と、差し替えたものを返す。
func newTestDomainService(ingress model.Ingress, verifierOK bool) (*IngressDomainService, *fakeDomainStore, *fakeReapplyStarter) {
	store := &fakeDomainStore{}
	starter := &fakeReapplyStarter{}
	return NewIngressDomainService(&fakeDomainReader{ingress: ingress}, store, &fakeDomainVerifier{ok: verifierOK}, starter, "shconn.org"), store, starter
}

// TestIngressDomainServiceAdd は、追加のときの検査(形、ベースドメイン、削除中)を確かめる。
func TestIngressDomainServiceAdd(testingT *testing.T) {
	ctx := context.Background()
	service, store, _ := newTestDomainService(model.Ingress{ID: "ingress-1", Host: "a.shconn.org", Status: model.IngressStatusActive}, true)
	// 整えて保存され、確認用の値が付く。
	domain, _, addError := service.Add(ctx, "user-a", "project-1", "ingress-1", " App.Example.com. ")
	if addError != nil || domain.Hostname != "app.example.com" || store.domain.Token == "" {
		testingT.Errorf("整えて追加できるはず: %v %+v", addError, domain)
	}
	// ベースドメインの下は、拒否する。
	var appError *apperror.Error
	if _, _, baseError := service.Add(ctx, "user-a", "project-1", "ingress-1", "x.shconn.org"); !errors.As(baseError, &appError) || appError.Kind != apperror.KindInvalidRequest {
		testingT.Errorf("ベースドメインの下は invalid_request のはず: %v", baseError)
	}
	// 削除中の ingress には、追加できない。
	deleting, _, _ := newTestDomainService(model.Ingress{ID: "ingress-1", Status: model.IngressStatusDeleting}, true)
	if _, _, deletingError := deleting.Add(ctx, "user-a", "project-1", "ingress-1", "app.example.com"); !errors.As(deletingError, &appError) || appError.Kind != apperror.KindConflict {
		testingT.Errorf("削除中は conflict のはず: %v", deletingError)
	}
}

// TestIngressDomainServiceVerify は、確認の結果と、IngressRoute の作り直しの依頼を確かめる。
func TestIngressDomainServiceVerify(testingT *testing.T) {
	ctx := context.Background()
	routed := model.Ingress{ID: "ingress-1", Host: "a.shconn.org", Status: model.IngressStatusActive, Rules: []byte(`[{"path":"/","serviceId":"service-1","port":80}]`)}

	// DNS が合わなければ、確認済みにせず、理由を返し、作り直さない。
	service, store, starter := newTestDomainService(routed, false)
	store.domain = model.IngressDomain{ID: "domain-1", IngressID: "ingress-1", Hostname: "app.example.com", Token: "t"}
	result, _, verifyError := service.Verify(ctx, "user-a", "project-1", "ingress-1", "domain-1")
	if verifyError != nil || result.Verified || result.Reason == "" || store.domain.IsVerified() || starter.reapplied != 0 {
		testingT.Errorf("合わないときは、確認済みにならないはず: %v %+v", verifyError, result)
	}

	// DNS が合えば、確認済みにして、IngressRoute を作り直す。
	service, store, starter = newTestDomainService(routed, true)
	store.domain = model.IngressDomain{ID: "domain-1", IngressID: "ingress-1", Hostname: "app.example.com", Token: "t"}
	result, _, verifyError = service.Verify(ctx, "user-a", "project-1", "ingress-1", "domain-1")
	if verifyError != nil || !result.Verified || !result.Domain.IsVerified() || starter.reapplied != 1 {
		testingT.Errorf("合えば確認済みになり、作り直すはず: %v %+v 作り直し=%d", verifyError, result, starter.reapplied)
	}

	// ルールが無い ingress では、IngressRoute が無いので、作り直さない。
	service, store, starter = newTestDomainService(model.Ingress{ID: "ingress-1", Host: "a.shconn.org", Status: model.IngressStatusActive, Rules: []byte("[]")}, true)
	store.domain = model.IngressDomain{ID: "domain-1", IngressID: "ingress-1", Hostname: "app.example.com", Token: "t"}
	if _, _, verifyError = service.Verify(ctx, "user-a", "project-1", "ingress-1", "domain-1"); verifyError != nil || starter.reapplied != 0 {
		testingT.Errorf("ルールが無ければ、作り直さないはず: %v 作り直し=%d", verifyError, starter.reapplied)
	}

	// 別の ingress がすでに確認済みなら、conflict。
	service, store, _ = newTestDomainService(routed, true)
	store.domain = model.IngressDomain{ID: "domain-1", IngressID: "ingress-1", Hostname: "app.example.com", Token: "t"}
	store.verifyConflict = true
	var appError *apperror.Error
	if _, _, conflictError := service.Verify(ctx, "user-a", "project-1", "ingress-1", "domain-1"); !errors.As(conflictError, &appError) || appError.Kind != apperror.KindConflict {
		testingT.Errorf("重なったら conflict のはず: %v", conflictError)
	}
}

// TestIngressDomainServiceRemove は、確認済みのドメインを外すと、作り直されることを確かめる。
func TestIngressDomainServiceRemove(testingT *testing.T) {
	ctx := context.Background()
	routed := model.Ingress{ID: "ingress-1", Status: model.IngressStatusActive, Rules: []byte(`[{"path":"/","serviceId":"service-1","port":80}]`)}
	now := time.Now()
	// 確認済みを外すと、作り直す。
	service, store, starter := newTestDomainService(routed, true)
	store.domain = model.IngressDomain{ID: "domain-1", IngressID: "ingress-1", Hostname: "app.example.com", VerifiedAt: &now}
	if removeError := service.Remove(ctx, "user-a", "project-1", "ingress-1", "domain-1"); removeError != nil || starter.reapplied != 1 {
		testingT.Errorf("確認済みを外したら、作り直すはず: %v 作り直し=%d", removeError, starter.reapplied)
	}
	// 確認待ちを外しても、作り直さない。
	service, store, starter = newTestDomainService(routed, true)
	store.domain = model.IngressDomain{ID: "domain-1", IngressID: "ingress-1", Hostname: "app.example.com"}
	if removeError := service.Remove(ctx, "user-a", "project-1", "ingress-1", "domain-1"); removeError != nil || starter.reapplied != 0 {
		testingT.Errorf("確認待ちを外しても、作り直さないはず: %v 作り直し=%d", removeError, starter.reapplied)
	}
}
