// Package repository は、データベースの読み書きを担当する。
//
// バックエンドの API と、ワーカーの Activity の両方が同じテーブルを扱うため、shared に置く。
// ビジネスルールは書かず、SQL の実行と、データベースのエラーの変換だけを行う。
package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/pagination"
)

// ProjectRepository は、projects テーブルの読み書きを担当する。
type ProjectRepository struct {
	database *gorm.DB
}

// NewProjectRepository は、ProjectRepository を作る。
//
// 引数:
//   - database: 使うデータベースの接続。
//
// 戻り値:
//   - 作成した ProjectRepository。
func NewProjectRepository(database *gorm.DB) *ProjectRepository {
	// 受け取ったデータベースの接続を持たせた、リポジトリを返す。
	return &ProjectRepository{database: database}
}

// Create は、プロジェクトを1件保存する。
//
// 保存の前に、ユーザーのクォータ(プロジェクトの数)を、行をロックして確かめる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - project: 保存するプロジェクト。ID は、呼び出し側で作っておく。
//
// 戻り値:
//   - 上限に達しているときの、conflict のエラー。
//   - 同じユーザーの中で名前が重複したときの、conflict のエラー。
//   - それ以外のデータベースのエラーは、internal_error。
func (projectRepository *ProjectRepository) Create(requestContext context.Context, project *model.Project) error {
	// 上限の確認と保存を、1つのトランザクションで行う。
	createError := projectRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// ユーザーのクォータをロックして、上限に収まるかを確かめる。
		limits, usage, quotaError := loadQuota(transaction, project.UserID, true)
		if quotaError != nil {
			return quotaError
		}
		if limitError := limits.CheckProjectCreate(usage); limitError != nil {
			return limitError
		}
		// context を渡して、プロジェクトを INSERT する。
		return transaction.Create(project).Error
	})
	// 上限を超えたときなど、利用者に伝えるエラーは、そのまま返す。
	var knownError *apperror.Error
	if errors.As(createError, &knownError) {
		return createError
	}
	// 一意制約の違反は、名前の重複なので、利用者に伝えられる conflict にする。
	if errors.Is(createError, gorm.ErrDuplicatedKey) {
		return apperror.Wrap(apperror.KindConflict, "同じ名前のプロジェクトがすでにあります", createError)
	}
	// それ以外のエラーは、利用者には詳細を見せない internal にする。
	if createError != nil {
		return apperror.Wrap(apperror.KindInternal, "プロジェクトを保存できませんでした", createError)
	}
	// 保存できたので、エラー無しで返す。
	return nil
}

// FindByID は、ユーザーのプロジェクトを1件取得する。
//
// 他のユーザーのプロジェクトは、存在しないものとして扱う(共通ルール)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 取得するプロジェクトの ID。
//
// 戻り値:
//   - 見つかったプロジェクト。
//   - 無い、または他のユーザーのものだったときの、not_found のエラー。
func (projectRepository *ProjectRepository) FindByID(requestContext context.Context, userID string, projectID string) (model.Project, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID) {
		return model.Project{}, apperror.New(apperror.KindNotFound, "プロジェクトが見つかりません")
	}
	// 取得した行を入れる変数。
	var project model.Project
	// ID と持ち主の両方で絞って1件取る。持ち主も条件に入れて、他人のものを見せない。
	findError := projectRepository.database.WithContext(requestContext).
		Where(&model.Project{ID: projectID, UserID: userID}).
		Take(&project).Error
	// 行が無いときは、他人のものも含めて、not_found にする。
	if errors.Is(findError, gorm.ErrRecordNotFound) {
		return model.Project{}, apperror.Wrap(apperror.KindNotFound, "プロジェクトが見つかりません", findError)
	}
	// それ以外のエラーは、internal にする。
	if findError != nil {
		return model.Project{}, apperror.Wrap(apperror.KindInternal, "プロジェクトを取得できませんでした", findError)
	}
	// 見つかったプロジェクトを返す。
	return project, nil
}

// ListByUser は、ユーザーのプロジェクトを、作成日時の新しい順に1ページ分取得する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - limit: 1ページの最大件数。
//   - after: 前のページの最後の位置。最初のページなら nil。
//
// 戻り値:
//   - プロジェクトの一覧(最大 limit 件)。
//   - 次のページがあるときは、その開始の位置の元になる、最後の行の位置。最後のページなら nil。
//   - データベースのエラー。
func (projectRepository *ProjectRepository) ListByUser(requestContext context.Context, userID string, limit int, after *pagination.Position) ([]model.Project, *pagination.Position, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID) {
		return nil, nil, newEmptyConditionError()
	}
	// 自分のプロジェクトを、新しい順に並べる検索を組み立てる。
	query := projectRepository.database.WithContext(requestContext).
		Where(&model.Project{UserID: userID}).
		Order("created_at DESC, id DESC").
		// 次のページがあるかを知るため、1件多く読む。
		Limit(limit + 1)
	// 2ページ目以降なら、前のページの最後より後ろ(古い側)だけに絞る。
	if after != nil {
		// 2列の組の比較は、構造体では書けないので、ここだけ SQL の文字列で書く。
		query = query.Where("(created_at, id) < (?, ?)", after.CreatedAt, after.ID)
	}

	// 取得した行を入れる変数。
	var projects []model.Project
	// 検索を実行する。失敗したときは、internal にして返す。
	if findError := query.Find(&projects).Error; findError != nil {
		return nil, nil, apperror.Wrap(apperror.KindInternal, "プロジェクトの一覧を取得できませんでした", findError)
	}

	// limit 件以下なら、次のページは無いので、位置は nil で返す。
	if len(projects) <= limit {
		return projects, nil, nil
	}
	// 確認のために多く読んだ1件を、返す一覧から外す。
	projects = projects[:limit]
	// 次のページの開始位置の元になる、このページの最後の行を取る。
	lastProject := projects[limit-1]
	// 一覧と、次のページの位置を返す。
	return projects, &pagination.Position{CreatedAt: lastProject.CreatedAt, ID: lastProject.ID}, nil
}

// Rename は、プロジェクトの名前を変える。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 名前を変えるプロジェクトの ID。
//   - newName: 新しい名前。
//
// 戻り値:
//   - 無い、または他のユーザーのものなら not_found。
//   - 削除中(deleting)なら conflict。
//   - 同じユーザーの中で名前が重複したら conflict。
func (projectRepository *ProjectRepository) Rename(requestContext context.Context, userID string, projectID string, newName string) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID, newName) {
		return newEmptyConditionError()
	}
	// active のものだけを対象に、名前を UPDATE する。削除中のものは変えない。
	updateResult := projectRepository.database.WithContext(requestContext).
		Model(&model.Project{}).
		Where(&model.Project{ID: projectID, UserID: userID, Status: model.ProjectStatusActive}).
		// updated_at は、Gorm が更新日時に自動で入れる。
		Updates(&model.Project{Name: newName})
	// 一意制約の違反は、名前の重複なので、conflict にする。
	if errors.Is(updateResult.Error, gorm.ErrDuplicatedKey) {
		return apperror.Wrap(apperror.KindConflict, "同じ名前のプロジェクトがすでにあります", updateResult.Error)
	}
	// それ以外のエラーは、internal にする。
	if updateResult.Error != nil {
		return apperror.Wrap(apperror.KindInternal, "プロジェクトの名前を変更できませんでした", updateResult.Error)
	}
	// 更新された行が無いときは、無いのか、削除中なのかを調べて、理由のエラーを返す。
	if updateResult.RowsAffected == 0 {
		return projectRepository.explainNoUpdate(requestContext, userID, projectID)
	}
	// 名前を変えられたので、エラー無しで返す。
	return nil
}

// MarkDeleting は、プロジェクトの状態を、削除中(deleting)にする。
//
// 通常(active)のものと、前回の削除に失敗して止まっている(delete_failed)ものが、対象。
// 後者は、削除のやり直しになる。実体の削除は、ここでは行わない(Workflow が非同期に行う)。
//
// 状態の確認と更新は、行をロックした1つのトランザクションで行う。同時に2つの削除が来ても、
// 1つだけが受け付けられる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 削除するプロジェクトの ID。
//
// 戻り値:
//   - 更新する前の状態(active または delete_failed)。Workflow を開始できなかったときに、戻すために使う。
//   - 無い、または他のユーザーのものなら not_found。すでに削除中なら conflict。
func (projectRepository *ProjectRepository) MarkDeleting(requestContext context.Context, userID string, projectID string) (string, error) {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID) {
		return "", apperror.New(apperror.KindNotFound, "プロジェクトが見つかりません")
	}
	// 更新前の状態を、トランザクションの外へ持ち出すための変数。
	previousStatus := ""

	// 行をロックして読み、状態を確かめて、更新する。
	transactionError := projectRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 持ち主のプロジェクトを、更新が終わるまで他から変更されないよう、ロックして読む。
		var project model.Project
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.Project{ID: projectID, UserID: userID}).
			Take(&project).Error
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return apperror.Wrap(apperror.KindNotFound, "プロジェクトが見つかりません", findError)
		}
		if findError != nil {
			return apperror.Wrap(apperror.KindInternal, "プロジェクトを取得できませんでした", findError)
		}

		// モデルに、削除の受け付けを任せる(active、delete_failed 以外は、conflict)。
		acceptedStatus, beginError := project.BeginDelete()
		if beginError != nil {
			return beginError
		}
		// 変えた project を保存する。
		if saveError := transaction.Save(&project).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "プロジェクトを削除中にできませんでした", saveError)
		}
		previousStatus = acceptedStatus
		return nil
	})
	// トランザクションが失敗したら、そのエラー(not_found、conflict、internal)を返す。
	if transactionError != nil {
		return "", transactionError
	}
	return previousStatus, nil
}

// explainNoUpdate は、更新の対象が0件だったとき、その理由をエラーにする。
//
// 存在しない(他のユーザーのものを含む)なら not_found、存在するが active でないなら conflict。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 更新しようとしたプロジェクトの ID。
//
// 戻り値:
//   - 理由に対応したエラー。
func (projectRepository *ProjectRepository) explainNoUpdate(requestContext context.Context, userID string, projectID string) error {
	// 持ち主のプロジェクトとして取得し、存在するかを調べる。
	project, findError := projectRepository.FindByID(requestContext, userID, projectID)
	// 存在しない(または他人の)ときは、not_found などのエラーをそのまま返す。
	if findError != nil {
		return findError
	}
	// 存在するのに更新されなかったのは、status のためなので、その status を伝える conflict を返す。
	return apperror.New(apperror.KindConflict, "プロジェクトは "+project.Status+" のため、変更できません")
}

// RevertDeleting は、削除中(deleting)にしたプロジェクトを、削除を受け付ける前の状態に戻す。
//
// 削除の Workflow を開始できなかったときに、プロジェクトが削除中のまま残らないよう、
// 状態を戻すために使う。削除中でないとき、または無いときは、何もしない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 状態を戻すプロジェクトの ID。
//   - previousStatus: 戻す先の状態(MarkDeleting が返した、active または delete_failed)。
//
// 戻り値:
//   - データベースのエラー。
func (projectRepository *ProjectRepository) RevertDeleting(requestContext context.Context, userID string, projectID string, previousStatus string) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(userID, projectID, previousStatus) {
		return newEmptyConditionError()
	}
	// 持ち主の、削除中のプロジェクトだけを対象にして、前の状態に戻す(更新日時は Gorm が更新する)。
	// 削除中のときだけ、モデルに前の状態へ戻させる。
	_, updateError := transitionProject(projectRepository.database, requestContext, projectID, func(project *model.Project) bool {
		// 他のユーザーのプロジェクトは、戻さない。
		if project.UserID != userID {
			return false
		}
		return project.RevertDeleting(previousStatus)
	})
	if updateError != nil {
		return updateError
	}
	return nil
}

// MarkDeleteFailed は、削除中(deleting)のプロジェクトを、削除に失敗した状態(delete_failed)にする。
//
// 削除の Workflow が、リトライの期限までに削除を終えられなかったときに、補償として使う。
// 削除中でないとき、または無いときは、何もしない(冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - projectID: 削除に失敗したプロジェクトの ID。
//
// 戻り値:
//   - データベースのエラー。
func (projectRepository *ProjectRepository) MarkDeleteFailed(requestContext context.Context, projectID string) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(projectID) {
		return newEmptyConditionError()
	}
	// 削除中のプロジェクトだけを、delete_failed にする(更新日時は Gorm が更新する)。
	// 削除中のときだけ、モデルに削除失敗の状態へ変えさせる。
	_, updateError := transitionProject(projectRepository.database, requestContext, projectID, func(project *model.Project) bool {
		return project.MarkDeleteFailed()
	})
	if updateError != nil {
		return updateError
	}
	return nil
}

// DeleteRecord は、プロジェクトと、配下のレコードを物理削除する。削除の Workflow が、最後に使う。
//
// 削除するもの(1つのトランザクションで、全て成功するか、全て取り消される):
//   - プロジェクトの配下の deploy
//   - プロジェクトの行
//
// 繰り返し実行されても壊れないよう、行がすでに無くてもエラーにしない。
// 削除中(deleting)でないプロジェクトは、配下も含めて、消さない(誤って消さないための安全装置)。
// build、service、ingress、volume のテーブルができたら、配下の削除に、ここへ足す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - projectID: 削除するプロジェクトの ID。
//
// 戻り値:
//   - データベースのエラー。
func (projectRepository *ProjectRepository) DeleteRecord(requestContext context.Context, projectID string) error {
	// 条件の値が空だと、Gorm が条件を無視して他の行が対象になるので、先に拒否する。
	if hasEmpty(projectID) {
		return newEmptyConditionError()
	}
	// 配下と本体の削除を、1つのトランザクションで行う。
	transactionError := projectRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 削除中のプロジェクトかを、行をロックして確かめる。そうでなければ(無い、または削除中でない)、何も消さない。
		var project model.Project
		findError := transaction.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&model.Project{ID: projectID, Status: model.ProjectStatusDeleting}).
			Take(&project).Error
		if errors.Is(findError, gorm.ErrRecordNotFound) {
			return nil
		}
		if findError != nil {
			return findError
		}

		// 配下の deploy を削除する。
		if deployError := transaction.Where(&model.Deploy{ProjectID: projectID}).Delete(&model.Deploy{}).Error; deployError != nil {
			return deployError
		}
		// 配下の ingress を削除する(IngressRoute は、namespace の削除で消えている)。
		if ingressError := transaction.Where(&model.Ingress{ProjectID: projectID}).Delete(&model.Ingress{}).Error; ingressError != nil {
			return ingressError
		}
		// 配下の volume を削除する(PVC は、namespace の削除で消えている)。
		if volumeError := transaction.Where(&model.Volume{ProjectID: projectID}).Delete(&model.Volume{}).Error; volumeError != nil {
			return volumeError
		}
		// 配下の ConfigMap を削除する(版は、外部キーの CASCADE で消える。k8s の ConfigMap は、namespace の削除で消えている)。
		if configMapError := transaction.Where(&model.ConfigMap{ProjectID: projectID}).Delete(&model.ConfigMap{}).Error; configMapError != nil {
			return configMapError
		}
		// プロジェクトの行を削除する。
		return transaction.Delete(&project).Error
	})
	// データベースのエラーは、内部エラーにして返す。
	if transactionError != nil {
		return apperror.Wrap(apperror.KindInternal, "プロジェクトを削除できませんでした", transactionError)
	}
	return nil
}
