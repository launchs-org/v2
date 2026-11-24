package repository

import (
	"context"
	"errors"
	"testing"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// newTestDomain は、テスト用のカスタムドメインを作る。
//
// 引数:
//   - ingressID: 付け先の ingress の ID。
//   - hostname: ドメイン名。
//
// 戻り値:
//   - 保存前のドメイン(ID は ingress と hostname から作る)。
func newTestDomain(ingressID string, hostname string) *model.IngressDomain {
	return &model.IngressDomain{ID: model.IngressDomainIDPrefix + ingressID + hostname, IngressID: ingressID, Hostname: hostname, Token: "launchs-verify=token-" + hostname}
}

// TestIngressDomainRepository は、追加・一覧・確認済み・重複・上限・削除・他人のものを確かめる。
func TestIngressDomainRepository(testingT *testing.T) {
	ctx := context.Background()
	ingressRepository, database, _ := newIngressTestEnvironment(testingT)
	domainRepository := NewIngressDomainRepository(database)
	first := newTestIngress("a.launchs.org")
	second := newTestIngress("b.launchs.org")
	for _, ingress := range []*model.Ingress{first, second} {
		if createError := ingressRepository.Create(ctx, ingress); createError != nil {
			testingT.Fatalf("ingress を作れるはず: %v", createError)
		}
	}

	// 追加できて、一覧に出る(確認待ち)。
	domain := newTestDomain(first.ID, "app.example.com")
	if createError := domainRepository.Create(ctx, "user-a", "project-00000001", domain); createError != nil {
		testingT.Fatalf("追加できるはず: %v", createError)
	}
	domains, _ := domainRepository.List(ctx, "user-a", "project-00000001", first.ID)
	if len(domains) != 1 || domains[0].IsVerified() {
		testingT.Fatalf("確認待ちが 1 つのはず: %+v", domains)
	}
	// 同じ ingress に、同じドメインは、2 つ付けられない。
	var appError *apperror.Error
	if duplicateError := domainRepository.Create(ctx, "user-a", "project-00000001", newTestDomain(first.ID, "app.example.com")); !errors.As(duplicateError, &appError) || appError.Kind != apperror.KindConflict {
		testingT.Errorf("重複は conflict のはず: %v", duplicateError)
	}
	// 他人からは、見えない。
	if _, listError := domainRepository.List(ctx, "user-b", "project-00000001", first.ID); !errors.As(listError, &appError) || appError.Kind != apperror.KindNotFound {
		testingT.Errorf("他人のものは not_found のはず: %v", listError)
	}
	if _, findError := domainRepository.Find(ctx, "user-b", "project-00000001", first.ID, domain.ID); !errors.As(findError, &appError) || appError.Kind != apperror.KindNotFound {
		testingT.Errorf("他人のものは not_found のはず: %v", findError)
	}

	// 別の ingress にも、確認待ちのまま、同じドメインを付けられる(先に押さえられないため)。
	other := newTestDomain(second.ID, "app.example.com")
	if createError := domainRepository.Create(ctx, "user-a", "project-00000001", other); createError != nil {
		testingT.Fatalf("確認待ちの重なりは許すはず: %v", createError)
	}
	// 先に確認した方だけが、確認済みになれる。
	if markError := domainRepository.MarkVerified(ctx, domain.ID); markError != nil {
		testingT.Fatalf("確認済みにできるはず: %v", markError)
	}
	if markError := domainRepository.MarkVerified(ctx, other.ID); !errors.Is(markError, ErrIngressDomainTaken) {
		testingT.Errorf("後からは ErrIngressDomainTaken のはず: %v", markError)
	}
	// 確認済みのドメイン名だけが、ワーカー向けに返る。
	hosts, _ := ingressRepository.VerifiedDomainHosts(ctx, first.ID)
	if len(hosts) != 1 || hosts[0] != "app.example.com" {
		testingT.Errorf("確認済みの 1 つだけのはず: %v", hosts)
	}
	otherHosts, _ := ingressRepository.VerifiedDomainHosts(ctx, second.ID)
	if len(otherHosts) != 0 {
		testingT.Errorf("確認待ちは返らないはず: %v", otherHosts)
	}

	// 上限(5 つ)を超えると conflict。
	for _, name := range []string{"b.example.com", "c.example.com", "d.example.com", "e.example.com"} {
		if createError := domainRepository.Create(ctx, "user-a", "project-00000001", newTestDomain(first.ID, name)); createError != nil {
			testingT.Fatalf("上限までは追加できるはず: %v", createError)
		}
	}
	if overError := domainRepository.Create(ctx, "user-a", "project-00000001", newTestDomain(first.ID, "f.example.com")); !errors.As(overError, &appError) || appError.Kind != apperror.KindConflict {
		testingT.Errorf("上限を超えたら conflict のはず: %v", overError)
	}

	// 削除すると、外したドメイン(確認済みだったか)が返り、ワーカー向けからも消える。
	removed, deleteError := domainRepository.Delete(ctx, "user-a", "project-00000001", first.ID, domain.ID)
	if deleteError != nil || !removed.IsVerified() {
		testingT.Errorf("確認済みのドメインを外せるはず: %v %+v", deleteError, removed)
	}
	hosts, _ = ingressRepository.VerifiedDomainHosts(ctx, first.ID)
	if len(hosts) != 0 {
		testingT.Errorf("外したドメインは返らないはず: %v", hosts)
	}
	// 無いものは not_found。
	if _, missingError := domainRepository.Delete(ctx, "user-a", "project-00000001", first.ID, domain.ID); !errors.As(missingError, &appError) || appError.Kind != apperror.KindNotFound {
		testingT.Errorf("無いものは not_found のはず: %v", missingError)
	}
}
