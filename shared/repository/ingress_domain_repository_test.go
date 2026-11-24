package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"launchs/shared/apperror"
	"launchs/shared/model"
)

// newTestDomain は、テスト用のホスト名を(有効な状態で)作る。
//
// 引数:
//   - ingressID: 付け先の ingress の ID。
//   - hostname: ホスト名。
//
// 戻り値:
//   - 保存前のホスト名(ID は ingress と hostname から作る)。
func newTestDomain(ingressID string, hostname string) *model.IngressDomain {
	verifiedAt := time.Now()
	return &model.IngressDomain{ID: model.IngressDomainIDPrefix + ingressID + hostname, IngressID: ingressID, Hostname: hostname, Token: "launchs-verify=token-" + hostname, VerifiedAt: &verifiedAt}
}

// TestIngressDomainRepository は、追加・一覧・重複・払い出しとの衝突・上限・削除・他人のものを確かめる。
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

	// 追加できて、一覧に出る(有効)。
	domain := newTestDomain(first.ID, "app.launchs.org")
	if createError := domainRepository.Create(ctx, "user-a", "project-00000001", domain); createError != nil {
		testingT.Fatalf("追加できるはず: %v", createError)
	}
	domains, _ := domainRepository.List(ctx, "user-a", "project-00000001", first.ID)
	if len(domains) != 1 || !domains[0].IsVerified() {
		testingT.Fatalf("有効なものが 1 つのはず: %+v", domains)
	}
	// 同じ名前は、同じ ingress にも、別の ingress にも、付けられない。
	var appError *apperror.Error
	for _, target := range []string{first.ID, second.ID} {
		if duplicateError := domainRepository.Create(ctx, "user-a", "project-00000001", newTestDomain(target, "app.launchs.org")); !errors.As(duplicateError, &appError) || appError.Kind != apperror.KindConflict {
			testingT.Errorf("重複は conflict のはず(%s): %v", target, duplicateError)
		}
	}
	// 払い出されたホスト名(ingresses.host)と同じ名前は、付けられない。
	if assignedError := domainRepository.Create(ctx, "user-a", "project-00000001", newTestDomain(second.ID, "a.launchs.org")); !errors.As(assignedError, &appError) || appError.Kind != apperror.KindConflict {
		testingT.Errorf("払い出された名前は conflict のはず: %v", assignedError)
	}
	// 他人からは、見えない。
	if _, listError := domainRepository.List(ctx, "user-b", "project-00000001", first.ID); !errors.As(listError, &appError) || appError.Kind != apperror.KindNotFound {
		testingT.Errorf("他人のものは not_found のはず: %v", listError)
	}
	// 有効なホスト名だけが、ワーカー向けに返る。
	hosts, _ := ingressRepository.VerifiedDomainHosts(ctx, first.ID)
	if len(hosts) != 1 || hosts[0] != "app.launchs.org" {
		testingT.Errorf("有効な 1 つだけのはず: %v", hosts)
	}
	otherHosts, _ := ingressRepository.VerifiedDomainHosts(ctx, second.ID)
	if len(otherHosts) != 0 {
		testingT.Errorf("付けていない ingress には返らないはず: %v", otherHosts)
	}

	// 上限(5 つ)を超えると conflict。
	for _, name := range []string{"n1.launchs.org", "n2.launchs.org", "n3.launchs.org", "n4.launchs.org"} {
		if createError := domainRepository.Create(ctx, "user-a", "project-00000001", newTestDomain(first.ID, name)); createError != nil {
			testingT.Fatalf("上限までは追加できるはず: %v", createError)
		}
	}
	if overError := domainRepository.Create(ctx, "user-a", "project-00000001", newTestDomain(first.ID, "f.launchs.org")); !errors.As(overError, &appError) || appError.Kind != apperror.KindConflict {
		testingT.Errorf("上限を超えたら conflict のはず: %v", overError)
	}

	// 削除すると、外したホスト名が返り、ワーカー向けからも消える。
	removed, deleteError := domainRepository.Delete(ctx, "user-a", "project-00000001", first.ID, domain.ID)
	if deleteError != nil || !removed.IsVerified() {
		testingT.Errorf("外せるはず: %v %+v", deleteError, removed)
	}
	hosts, _ = ingressRepository.VerifiedDomainHosts(ctx, first.ID)
	for _, host := range hosts {
		if host == "app.launchs.org" {
			testingT.Errorf("外したホスト名は返らないはず: %v", hosts)
		}
	}
	// 無いものは not_found。
	if _, missingError := domainRepository.Delete(ctx, "user-a", "project-00000001", first.ID, domain.ID); !errors.As(missingError, &appError) || appError.Kind != apperror.KindNotFound {
		testingT.Errorf("無いものは not_found のはず: %v", missingError)
	}
}
