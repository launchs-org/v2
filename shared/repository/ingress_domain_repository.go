package repository

import (
	"context"
	"errors"

	"launchs/shared/apperror"
	"launchs/shared/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IngressDomainRepository は、ingress の追加のホスト名(ingress_domains)を読み書きする。
type IngressDomainRepository struct {
	// database は、Gorm のデータベース接続。
	database *gorm.DB
}

// NewIngressDomainRepository は、IngressDomainRepository を作る。
//
// 引数:
//   - database: Gorm のデータベース接続。
//
// 戻り値:
//   - 作った repository。
func NewIngressDomainRepository(database *gorm.DB) *IngressDomainRepository {
	// 接続を保持した repository を返す。
	return &IngressDomainRepository{database: database}
}

// lockOwnedIngress は、ユーザーのプロジェクトの ingress が存在することを確かめ、行をロックする(トランザクションの中で使う)。
//
// 引数:
//   - transaction: 使っているトランザクション。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//
// 戻り値:
//   - 無い、または他のユーザーのものは not_found。
func lockOwnedIngress(transaction *gorm.DB, userID string, projectID string, ingressID string) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID, ingressID) {
		return apperror.New(apperror.KindNotFound, "ingress が見つかりません")
	}
	// 持ち主の ingress を、更新用にロックして読む。
	var ingress model.Ingress
	findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(&model.Ingress{ID: ingressID, ProjectID: projectID, UserID: userID}).
		Take(&ingress).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return apperror.Wrap(apperror.KindNotFound, "ingress が見つかりません", findError)
	}
	if findError != nil {
		return apperror.Wrap(apperror.KindInternal, "ingress を取得できませんでした", findError)
	}
	return nil
}

// Create は、ingress に追加のホスト名を付ける。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - domain: 付けるドメイン(ID、IngressID、Hostname、Token を入れておく)。
//
// 戻り値:
//   - ingress が無い(他人のものを含む)ときは not_found。上限を超えるとき、同じドメインがすでに付いているときは conflict。
func (ingressDomainRepository *IngressDomainRepository) Create(requestContext context.Context, userID string, projectID string, domain *model.IngressDomain) error {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(domain.ID, domain.IngressID, domain.Hostname, domain.Token) {
		return newEmptyConditionError()
	}
	return ingressDomainRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 持ち主の ingress をロックする(上限の数え直しが、同時に起きないように)。
		if lockError := lockOwnedIngress(transaction, userID, projectID, domain.IngressID); lockError != nil {
			return lockError
		}
		// 付いている数を数えて、上限を確かめる。
		var count int64
		if countError := transaction.Model(&model.IngressDomain{}).Where(&model.IngressDomain{IngressID: domain.IngressID}).Count(&count).Error; countError != nil {
			return apperror.Wrap(apperror.KindInternal, "ドメインを数えられませんでした", countError)
		}
		if count >= model.MaxIngressDomains {
			return apperror.New(apperror.KindConflict, "1 つの ingress に付けられるホスト名は、5 つまでです")
		}
		// 払い出されたホスト名(ingresses.host)と同じ名前は、使えない(同じ Host で、2 つのルートができるため)。
		var assignedCount int64
		if assignedError := transaction.Model(&model.Ingress{}).Where("host = ?", domain.Hostname).Count(&assignedCount).Error; assignedError != nil {
			return apperror.Wrap(apperror.KindInternal, "ホスト名を確かめられませんでした", assignedError)
		}
		if assignedCount > 0 {
			return apperror.New(apperror.KindConflict, "この名前は、すでに使われています")
		}
		// 保存する。確認済みのホスト名は全体で 1 つ(部分一意インデックス)なので、重なったら、すでに使われている。
		createError := transaction.Create(domain).Error
		if errors.Is(createError, gorm.ErrDuplicatedKey) {
			return apperror.New(apperror.KindConflict, "この名前は、すでに使われています")
		}
		if createError != nil {
			return apperror.Wrap(apperror.KindInternal, "ドメインを保存できませんでした", createError)
		}
		return nil
	})
}

// List は、ingress に付いているカスタムドメインを、追加した順に取得する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//
// 戻り値:
//   - ドメインの一覧(無ければ空)。ingress が無い(他人のものを含む)ときは not_found。
func (ingressDomainRepository *IngressDomainRepository) List(requestContext context.Context, userID string, projectID string, ingressID string) ([]model.IngressDomain, error) {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID, ingressID) {
		return nil, apperror.New(apperror.KindNotFound, "ingress が見つかりません")
	}
	// ingress が、持ち主のものかを確かめる。
	var ingressCount int64
	if countError := ingressDomainRepository.database.WithContext(requestContext).Model(&model.Ingress{}).
		Where(&model.Ingress{ID: ingressID, ProjectID: projectID, UserID: userID}).Count(&ingressCount).Error; countError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ingress を取得できませんでした", countError)
	}
	if ingressCount == 0 {
		return nil, apperror.New(apperror.KindNotFound, "ingress が見つかりません")
	}
	// 追加した順に、取得する。
	var domains []model.IngressDomain
	if findError := ingressDomainRepository.database.WithContext(requestContext).
		Where(&model.IngressDomain{IngressID: ingressID}).Order("created_at ASC, id ASC").Find(&domains).Error; findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ドメインを取得できませんでした", findError)
	}
	return domains, nil
}

// Delete は、ingress からカスタムドメインを外す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - domainID: ドメインの ID。
//
// 戻り値:
//   - 外したドメイン(確認済みだったかを、呼び出し側が見るため)。無い(他人のものを含む)ときは not_found。
func (ingressDomainRepository *IngressDomainRepository) Delete(requestContext context.Context, userID string, projectID string, ingressID string, domainID string) (model.IngressDomain, error) {
	var removed model.IngressDomain
	transactionError := ingressDomainRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 持ち主の ingress をロックしてから、ドメインを探す。
		if lockError := lockOwnedIngress(transaction, userID, projectID, ingressID); lockError != nil {
			return lockError
		}
		if hasEmpty(domainID) {
			return apperror.New(apperror.KindNotFound, "ドメインが見つかりません")
		}
		findError := transaction.Where(&model.IngressDomain{ID: domainID, IngressID: ingressID}).Take(&removed).Error
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "ドメインが見つかりません", findError)
		}
		if findError != nil {
			return apperror.Wrap(apperror.KindInternal, "ドメインを取得できませんでした", findError)
		}
		// 行を削除する。
		if deleteError := transaction.Delete(&model.IngressDomain{}, "id = ?", domainID).Error; deleteError != nil {
			return apperror.Wrap(apperror.KindInternal, "ドメインを削除できませんでした", deleteError)
		}
		return nil
	})
	if transactionError != nil {
		return model.IngressDomain{}, transactionError
	}
	return removed, nil
}

// VerifiedDomainHosts は、ingress に付いている、確認済みのカスタムドメイン名を返す(ワーカーが、IngressRoute を作るときに使う)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - ingressID: ingress の ID。
//
// 戻り値:
//   - 確認済みのドメイン名(追加した順。無ければ空)。
func (ingressRepository *IngressRepository) VerifiedDomainHosts(requestContext context.Context, ingressID string) ([]string, error) {
	// 値が空だと、意図しない行が対象になるので、先に拒否する。
	if hasEmpty(ingressID) {
		return nil, newEmptyConditionError()
	}
	// 確認済みのドメイン名だけを、追加した順に取得する。
	var hostnames []string
	if findError := ingressRepository.database.WithContext(requestContext).Model(&model.IngressDomain{}).
		Where("ingress_id = ? AND verified_at IS NOT NULL", ingressID).Order("created_at ASC, id ASC").Pluck("hostname", &hostnames).Error; findError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ドメインを取得できませんでした", findError)
	}
	return hostnames, nil
}
