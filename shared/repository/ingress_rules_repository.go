package repository

import (
	"context"
	"encoding/json"
	"strconv"

	"launchs/shared/apperror"
	"launchs/shared/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// validateRuleTargets は、ルールの流す先(service とポート)が、実在することを確かめる(トランザクションの中で使う)。
//
// 確かめること: service が同じプロジェクトにあること、そのポートが service の ports にあること、protocol が tcp であること
// (ingress は HTTP だけ)。削除中の service は、流す先にできない。
//
// 引数:
//   - transaction: 使っているトランザクション。
//   - projectID: ingress のプロジェクトの ID。
//   - rules: 確かめるルール。
//   - failureKind: 不正なときのエラーの種類(更新のときは invalid_request、適用のときは conflict)。
//
// 戻り値:
//   - 不正なら、failureKind のエラー。
func validateRuleTargets(transaction *gorm.DB, projectID string, rules []model.IngressRule, failureKind apperror.Kind) error {
	// ルールが無ければ、確かめるものが無い。
	if len(rules) == 0 {
		return nil
	}
	// 流す先の service の ID を集める。
	serviceIDs := []string{}
	for _, rule := range rules {
		serviceIDs = append(serviceIDs, rule.ServiceID)
	}
	// 同じプロジェクトの service を、まとめて読む。
	var services []model.Service
	findError := transaction.Where("project_id = ? AND id IN ?", projectID, serviceIDs).Find(&services).Error
	if findError != nil {
		return apperror.Wrap(apperror.KindInternal, "service を取得できませんでした", findError)
	}
	// ID から引けるようにする。
	servicesByID := map[string]model.Service{}
	for _, service := range services {
		servicesByID[service.ID] = service
	}
	// ルールごとに、流す先を確かめる。
	for _, rule := range rules {
		// 同じプロジェクトの service でなければ、拒否する。
		service, exists := servicesByID[rule.ServiceID]
		if !exists || service.Status == model.ServiceStatusDeleting {
			return apperror.New(failureKind, "このプロジェクトに、流す先の service がありません: "+rule.ServiceID)
		}
		// ポートが、service の ports にあり、tcp であることを確かめる。
		if !service.HasTCPPort(rule.Port) {
			return apperror.New(failureKind, "service "+rule.ServiceID+" に、tcp のポート "+strconv.Itoa(rule.Port)+" がありません")
		}
	}
	return nil
}

// SavePendingRules は、ルールを、まだ適用しない値として保存する。
//
// 現在のルールと同じ内容(JSON の書式ではなく、中身で比べる)なら、変更が無いものとして、pending を保存せず空にする
// (以前の pending も消える)。現在反映されているルールは変えない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - rules: 保存するルール(形は、呼び出し側で確かめてある)。空の配列は、ルールを全て消す変更。
//
// 戻り値:
//   - ingress が無い(他人のものを含む)なら not_found。反映中・削除中なら conflict。
//   - 流す先が不正なら invalid_request。
func (ingressRepository *IngressRepository) SavePendingRules(requestContext context.Context, userID string, projectID string, ingressID string, rules []model.IngressRule) error {
	// 保存する JSON を作る。
	rulesJSON, marshalError := marshalIngressRules(rules)
	if marshalError != nil {
		return marshalError
	}
	// 取得と更新を、1つのトランザクションで行う。
	return ingressRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行をロックして読む。
		ingress, lockError := lockIngress(transaction, userID, projectID, ingressID)
		if lockError != nil {
			return lockError
		}
		// 操作できる状態でなければ、conflict。
		if !ingress.CanChangeRules() {
			return apperror.New(apperror.KindConflict, "ingress は "+ingress.Status+" のため、ルールを操作できません")
		}
		// 流す先が実在することを確かめる。
		if targetError := validateRuleTargets(transaction, ingress.ProjectID, rules, apperror.KindInvalidRequest); targetError != nil {
			return targetError
		}
		// 現在と同じ内容なら、pending は空にする。
		ingress.PendingRules = rulesJSON
		if sameJSON(ingress.Rules, rulesJSON) {
			ingress.PendingRules = nil
		}
		// 変えた ingress を保存する(NULL も書くので、全ての列を書く)。
		if saveError := transaction.Save(&ingress).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "ルールを保存できませんでした", saveError)
		}
		return nil
	})
}

// DiscardPendingRules は、pending のルールを破棄する(ルールには影響しない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//
// 戻り値:
//   - ingress が無いなら not_found。反映中・削除中、または pending が無いなら conflict。
func (ingressRepository *IngressRepository) DiscardPendingRules(requestContext context.Context, userID string, projectID string, ingressID string) error {
	// 取得と更新を、1つのトランザクションで行う。
	return ingressRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行をロックして読む。
		ingress, lockError := lockIngress(transaction, userID, projectID, ingressID)
		if lockError != nil {
			return lockError
		}
		// 操作できる状態でなければ、conflict。
		if !ingress.CanChangeRules() {
			return apperror.New(apperror.KindConflict, "ingress は "+ingress.Status+" のため、ルールを操作できません")
		}
		// 破棄する値が無いときは、conflict。
		if len(ingress.PendingRules) == 0 {
			return apperror.New(apperror.KindConflict, "破棄するルールの変更がありません")
		}
		// pending を空にして保存する。
		ingress.PendingRules = nil
		if saveError := transaction.Save(&ingress).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "ルールを破棄できませんでした", saveError)
		}
		return nil
	})
}

// BeginRulesApply は、ルールの反映を始める(状態を applying にする)。
//
// 流す先の妥当性は、更新のあとにサービスのポートが変わっていることがあるので、ここで改めて確かめる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: 反映する ingress の ID。
//
// 戻り値:
//   - 変える前の状態(開始に失敗したとき、元に戻すために使う)。
//   - 無いなら not_found。反映中・削除中、適用する値が無い、流す先が不正なら conflict。
func (ingressRepository *IngressRepository) BeginRulesApply(requestContext context.Context, userID string, projectID string, ingressID string) (string, error) {
	// 前の状態を、トランザクションの外へ持ち出す入れ物。
	previousStatus := ""
	// 取得と更新を、1つのトランザクションで行う。
	transactionError := ingressRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行をロックして読む。
		ingress, lockError := lockIngress(transaction, userID, projectID, ingressID)
		if lockError != nil {
			return lockError
		}
		// モデルに、反映の開始を任せる(状態の規則は、モデルが持つ)。
		acceptedStatus, beginError := ingress.BeginRulesApply()
		if beginError != nil {
			return beginError
		}
		// pending のルールを読み、流す先を改めて確かめる。
		pendingRules, parseError := model.ParseIngressRules(ingress.PendingRules)
		if parseError != nil {
			return apperror.Wrap(apperror.KindInternal, "pending のルールを読めませんでした", parseError)
		}
		if targetError := validateRuleTargets(transaction, ingress.ProjectID, pendingRules, apperror.KindConflict); targetError != nil {
			return targetError
		}
		// 変えた ingress を保存する。
		if saveError := transaction.Save(&ingress).Error; saveError != nil {
			return apperror.Wrap(apperror.KindInternal, "ingress の状態を更新できませんでした", saveError)
		}
		previousStatus = acceptedStatus
		return nil
	})
	return previousStatus, transactionError
}

// RevertApply は、反映の開始に失敗したとき、ingress の状態を、反映を始める前の状態に戻す。
//
// applying でないとき、または無いときは、何もしない。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - ingressID: 戻す ingress の ID。
//   - previousStatus: 戻す先の状態(BeginRulesApply が返したもの)。
//
// 戻り値:
//   - データベースのエラー。
func (ingressRepository *IngressRepository) RevertApply(requestContext context.Context, ingressID string, previousStatus string) error {
	// 条件の値が空のときは拒否し、applying のときだけ、モデルに前の状態へ戻させる。
	if hasEmpty(previousStatus) {
		return newEmptyConditionError()
	}
	_, revertError := transitionIngress(ingressRepository.database, requestContext, ingressID, func(ingress *model.Ingress) bool {
		return ingress.RevertApply(previousStatus)
	})
	return revertError
}

// CommitRulesApply は、ルールの反映の結果を確定する。
//
// 成功ならルールを pending の内容にして pending を空に戻し、active にする。失敗ならルールと pending は変えず、failed にする。
// applying でなければ、何も変えず false を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - ingressID: 結果を確定する ingress の ID。
//   - succeeded: 反映に成功したか。
//
// 戻り値:
//   - 確定したら true。
//   - データベースのエラー。
func (ingressRepository *IngressRepository) CommitRulesApply(requestContext context.Context, ingressID string, succeeded bool) (bool, error) {
	// モデルに、結果の確定を任せる。
	return transitionIngress(ingressRepository.database, requestContext, ingressID, func(ingress *model.Ingress) bool {
		return ingress.CommitRules(succeeded)
	})
}

// marshalIngressRules は、ルールを、保存する JSON にする。nil は、空の配列にする。
//
// 引数:
//   - rules: ルール。
//
// 戻り値:
//   - JSON。作れなければ internal_error。
func marshalIngressRules(rules []model.IngressRule) (datatypes.JSON, error) {
	// nil だと JSON の null になるので、空の配列にそろえる。
	if rules == nil {
		rules = []model.IngressRule{}
	}
	// JSON にする。
	encoded, marshalError := json.Marshal(rules)
	if marshalError != nil {
		return nil, apperror.Wrap(apperror.KindInternal, "ルールを保存できませんでした", marshalError)
	}
	return datatypes.JSON(encoded), nil
}

// SetAccessStats は、アクセスの集計の有効・無効を保存し、保存後の ingress を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - ingressID: ingress の ID。
//   - enabled: 有効にするなら true。
//
// 戻り値:
//   - 保存後の ingress。無ければ not_found、反映中・削除中なら conflict。
func (ingressRepository *IngressRepository) SetAccessStats(requestContext context.Context, userID string, projectID string, ingressID string, enabled bool) (model.Ingress, error) {
	// 取得と更新を、1つのトランザクションで行う。
	var result model.Ingress
	transactionError := ingressRepository.database.WithContext(requestContext).Transaction(func(transaction *gorm.DB) error {
		// 行をロックして読む。
		ingress, lockError := lockIngress(transaction, userID, projectID, ingressID)
		if lockError != nil {
			return lockError
		}
		// 操作できる状態でなければ、conflict。
		if !ingress.CanChangeAccessStats() {
			return apperror.New(apperror.KindConflict, "ingress は "+ingress.Status+" のため、アクセスの集計の設定を変えられません")
		}
		// 値を設定して、変わったときだけ保存する(bool の false も保存するよう、列を指定する)。
		if ingress.SetAccessStats(enabled) {
			if saveError := transaction.Model(&model.Ingress{}).Where("id = ?", ingress.ID).Update("access_stats", ingress.AccessStats).Error; saveError != nil {
				return apperror.Wrap(apperror.KindInternal, "アクセスの集計の設定を保存できませんでした", saveError)
			}
		}
		result = ingress
		return nil
	})
	return result, transactionError
}
