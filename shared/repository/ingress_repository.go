package repository

import (
	"context"
	"errors"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrIngressHostTaken は、払い出したホスト名が、すでに別の ingress に使われているときのエラー。
// 呼び出し側は、ホスト名を作り直して、もう一度保存する。
var ErrIngressHostTaken = errors.New("ホスト名はすでに使われています")

// IngressRepository は、ingresses テーブルの読み書きを担当する。
// 状態の規則は model.Ingress が持ち、ここは「ロックして読む → モデルを呼ぶ → 保存する」を行う。
type IngressRepository struct {
	database *gorm.DB
}

// NewIngressRepository は、IngressRepository を作る。
//
// 引数:
//   - database: 使うデータベース。
//
// 戻り値:
//   - 作成した IngressRepository。
func NewIngressRepository(database *gorm.DB) *IngressRepository {
	return &IngressRepository{database: database}
}

// Create は、プロジェクトに ingress を1つ保存する(status は active、ルールは空)。
//
// プロジェクトが、持ち主のものであること、active であることを、プロジェクトの行をロックして確かめる。
// ロックは、確認のあとに、プロジェクトの削除の受け付けが入り込んで、削除中のプロジェクトに ingress が作られるのを防ぐ。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - ingress: 保存する ingress。ID、ProjectID、UserID、Host は、呼び出し側で設定しておく。
//
// 戻り値:
//   - プロジェクトが無い、または他のユーザーのものなら not_found。active でなければ conflict。
//   - ホスト名がすでに使われているときは ErrIngressHostTaken。
//   - それ以外のデータベースのエラーは internal_error。
func (ingressRepository *IngressRepository) Create(requestContext context.Context, ingress *model.Ingress) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(ingress.ID, ingress.ProjectID, ingress.UserID, ingress.Host) {
		return newEmptyConditionError()
	}
	// プロジェクトの確認と ingress の保存を、1つのトランザクションで行う。
	return ingressRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 持ち主のプロジェクトを、他から状態を変えられないよう、ロックして読む。
		var project model.Project
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.Project{ID: ingress.ProjectID, UserID: ingress.UserID}).
			Take(&project).Error
		// 無い、または他のユーザーのものは、存在しないものとして not_found にする。
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "プロジェクトが見つかりません", findError)
		}
		if findError != nil {
			return apperror.Wrap(apperror.KindInternal, "プロジェクトを取得できませんでした", findError)
		}
		// 稼働中でないプロジェクトには、作れない。
		if project.Status != model.ProjectStatusActive {
			return apperror.New(apperror.KindConflict, "プロジェクトは "+project.Status+" のため、ingress を作れません")
		}
		// 作成直後は、ルールが空で、反映済み(active)。
		ingress.Rules = []byte("[]")
		ingress.PendingRules = nil
		ingress.Status = model.IngressStatusActive
		createError := transaction.Create(ingress).Error
		// ホスト名が重なったときは、呼び出し側が作り直せるよう、専用のエラーにする。
		if errors.Is(createError, gorm.ErrDuplicatedKey) {
			return ErrIngressHostTaken
		}
		if createError != nil {
			return apperror.Wrap(apperror.KindInternal, "ingress を保存できませんでした", createError)
		}
		return nil
	})
}

// ListByProject は、プロジェクトの ingress を、新しい順に取得する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - limit: 1ページの件数。
//   - after: 2ページ目以降の、前のページの最後の位置。最初は nil。
//
// 戻り値:
//   - ingress の一覧と、次のページの位置(最後のページなら nil)。
//   - プロジェクトが無い(他人のものを含む)ときは not_found。
func (ingressRepository *IngressRepository) ListByProject(requestContext context.Context, userID string, projectID string, limit int, after *pagination.Position) ([]model.Ingress, *pagination.Position, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID) {
		return nil, nil, apperror.New(apperror.KindNotFound, "プロジェクトが見つかりません")
	}
	// プロジェクトが、持ち主のものかを確かめる。
	var projectCount int64
	if countError := ingressRepository.database.WithContext(requestContext).Model(&model.Project{}).
		Where(&model.Project{ID: projectID, UserID: userID}).Count(&projectCount).Error; countError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "プロジェクトを取得できませんでした", countError)
	}
	if projectCount == 0 {
		return nil, nil, apperror.New(apperror.KindNotFound, "プロジェクトが見つかりません")
	}
	// 新しい順に並べる検索を組み立てる(次のページがあるかを知るため、1件多く読む)。
	query := ingressRepository.database.WithContext(requestContext).
		Where(&model.Ingress{ProjectID: projectID}).
		Order("created_at DESC, id DESC").
		Limit(limit + 1)
	// 2ページ目以降なら、前のページの最後より後ろだけに絞る。
	if after != nil {
		query = query.Where("(created_at, id) < (?, ?)", after.CreatedAt, after.ID)
	}
	// 検索を実行する。
	var ingresses []model.Ingress
	if findError := query.Find(&ingresses).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "ingress の一覧を取得できませんでした", findError)
	}
	// limit 件以下なら、次のページは無い。
	if len(ingresses) <= limit {
		return ingresses, nil, nil
	}
	// 確認のために多く読んだ1件を外し、次のページの位置を作る。
	ingresses = ingresses[:limit]
	lastIngress := ingresses[limit-1]
	return ingresses, &pagination.Position{CreatedAt: lastIngress.CreatedAt, ID: lastIngress.ID}, nil
}

// FindByID は、ユーザーのプロジェクトの ingress を1件取得する。他のユーザーのものは、存在しないものとして扱う。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//
// 戻り値:
//   - ingress。無い(他人のものを含む)ときは not_found。
func (ingressRepository *IngressRepository) FindByID(requestContext context.Context, userID string, projectID string, ingressID string) (model.Ingress, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID, ingressID) {
		return model.Ingress{}, apperror.New(apperror.KindNotFound, "ingress が見つかりません")
	}
	// 持ち主とプロジェクトの条件つきで探す。
	var ingress model.Ingress
	findError := ingressRepository.database.WithContext(requestContext).
		Where(&model.Ingress{ID: ingressID, ProjectID: projectID, UserID: userID}).
		Take(&ingress).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.Ingress{}, apperror.Wrap(apperror.KindNotFound, "ingress が見つかりません", findError)
	}
	if findError != nil {
		return model.Ingress{}, apperror.Wrap(apperror.KindInternal, "ingress を取得できませんでした", findError)
	}
	return ingress, nil
}

// FindForWorker は、持ち主を問わずに、ingress を ID で1件取得する(ワーカー用)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - ingressID: ingress の ID。
//
// 戻り値:
//   - ingress。無いときは not_found。
func (ingressRepository *IngressRepository) FindForWorker(requestContext context.Context, ingressID string) (model.Ingress, error) {
	// 条件の値が空だと、Gorm が条件を無視して先頭の1件を返すので、先に拒否する。
	if hasEmpty(ingressID) {
		return model.Ingress{}, apperror.New(apperror.KindNotFound, "ingress が見つかりません")
	}
	// ID で探す。
	var ingress model.Ingress
	findError := ingressRepository.database.WithContext(requestContext).Where(&model.Ingress{ID: ingressID}).Take(&ingress).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.Ingress{}, apperror.Wrap(apperror.KindNotFound, "ingress が見つかりません", findError)
	}
	if findError != nil {
		return model.Ingress{}, apperror.Wrap(apperror.KindInternal, "ingress を取得できませんでした", findError)
	}
	return ingress, nil
}

// lockIngress は、ユーザーのプロジェクトの ingress を、更新用にロックして取得する(トランザクションの中で使う)。状態は確かめない。
//
// 引数:
//   - transaction: 使っているトランザクション。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//
// 戻り値:
//   - ロックした ingress。無い、または他のユーザーのものなら not_found。
func lockIngress(transaction *gorm.DB, userID string, projectID string, ingressID string) (model.Ingress, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID, ingressID) {
		return model.Ingress{}, apperror.New(apperror.KindNotFound, "ingress が見つかりません")
	}
	// 持ち主とプロジェクトの条件つきで、ロックして読む。
	var ingress model.Ingress
	findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(&model.Ingress{ID: ingressID, ProjectID: projectID, UserID: userID}).
		Take(&ingress).Error
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.Ingress{}, apperror.Wrap(apperror.KindNotFound, "ingress が見つかりません", findError)
	}
	if findError != nil {
		return model.Ingress{}, apperror.Wrap(apperror.KindInternal, "ingress を取得できませんでした", findError)
	}
	return ingress, nil
}

// transitionIngress は、ingress 1行に対して transitionRow を行う。
//
// 引数:
//   - database: 使うデータベース。
//   - requestContext: キャンセルや期限を伝える context。
//   - ingressID: 操作する ingress の ID。
//   - operation: モデルの操作。変えたら true を返す。
//
// 戻り値:
//   - 変えて保存したら true。
//   - データベースのエラー。
func transitionIngress(database *gorm.DB, requestContext context.Context, ingressID string, operation func(ingress *model.Ingress) bool) (bool, error) {
	// 型を決めて、共通の処理に任せる。
	return transitionRow[model.Ingress](database, requestContext, ingressID, operation)
}

// MarkDeleting は、ingress の削除を受け付ける(状態を deleting にする)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: 削除する ingress の ID。
//
// 戻り値:
//   - 受け付ける前の状態(開始に失敗したとき、元に戻すために使う)。
//   - 無い(他人のものを含む)なら not_found。反映中・削除中なら conflict。
func (ingressRepository *IngressRepository) MarkDeleting(requestContext context.Context, userID string, projectID string, ingressID string) (string, error) {
	// 前の状態を、トランザクションの外へ持ち出す入れ物。
	previousStatus := ""
	// 取得と更新を、1つのトランザクションで行う。
	transactionError := ingressRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行をロックして読む。
		ingress, lockError := lockIngress(transaction, userID, projectID, ingressID)
		if lockError != nil {
			return lockError
		}
		// モデルに、削除の受け付けを任せる。
		acceptedStatus, beginError := ingress.BeginDelete()
		if beginError != nil {
			return beginError
		}
		// 変えた ingress を保存する。
		if saveError := transaction.Save(&ingress).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "ingress を削除中にできませんでした", saveError)
		}
		previousStatus = acceptedStatus
		return nil
	})
	return previousStatus, transactionError
}

// RevertDeleting は、削除中にした ingress を、削除を受け付ける前の状態に戻す(Workflow を開始できなかったとき)。
//
// 削除中でないとき、または無いときは、何もしない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - ingressID: 戻す ingress の ID。
//   - previousStatus: 戻す先の状態(MarkDeleting が返したもの)。
//
// 戻り値:
//   - データベースのエラー。
func (ingressRepository *IngressRepository) RevertDeleting(requestContext context.Context, ingressID string, previousStatus string) error {
	// 条件の値が空のときは拒否し、削除中のときだけ、モデルに前の状態へ戻させる。
	if hasEmpty(previousStatus) {
		return newEmptyConditionError()
	}
	_, revertError := transitionIngress(ingressRepository.database, requestContext, ingressID, func(ingress *model.Ingress) bool {
		return ingress.RevertDeleting(previousStatus)
	})
	return revertError
}

// MarkDeleteFailed は、削除中の ingress を、failed にする(削除の Workflow が、リトライの期限までに終えられなかったとき)。
//
// 削除中でないとき、または無いときは、何もしない(冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - ingressID: 削除に失敗した ingress の ID。
//
// 戻り値:
//   - データベースのエラー。
func (ingressRepository *IngressRepository) MarkDeleteFailed(requestContext context.Context, ingressID string) error {
	// 削除中のときだけ、モデルに failed へ変えさせる。
	_, markError := transitionIngress(ingressRepository.database, requestContext, ingressID, func(ingress *model.Ingress) bool {
		return ingress.MarkDeleteFailed()
	})
	return markError
}

// DeleteRecord は、ingress を物理削除する。削除の Workflow が、最後に使う。
//
// 削除中(deleting)の ingress だけを消す(誤って消さないための安全装置)。繰り返し実行されても壊れない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - ingressID: 削除する ingress の ID。
//
// 戻り値:
//   - データベースのエラー。
func (ingressRepository *IngressRepository) DeleteRecord(requestContext context.Context, ingressID string) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(ingressID) {
		return newEmptyConditionError()
	}
	// 削除中の ingress だけを物理削除する。
	deleteError := ingressRepository.database.WithContext(requestContext).
		Where(&model.Ingress{ID: ingressID, Status: model.IngressStatusDeleting}).
		Delete(&model.Ingress{}).Error
	if deleteError != nil {
		return apperror.Wrap(apperror.KindInternal, "ingress を削除できませんでした", deleteError)
	}
	return nil
}
