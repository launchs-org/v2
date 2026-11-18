package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/template"
	"launchs/shared/templateplan"
)

// maxTemplateHosts は、ingress のホスト名の候補を、計画に入れる数(重なったら、次の候補を使う)。
const maxTemplateHosts = 5

// TemplateApplyStore は、テンプレートの実行の、データベースの操作。
//
// 実体は shared/repository の TemplateApplyRepository。テストでは fake に差し替える。
type TemplateApplyStore interface {
	// Create は、実行を保存する。newConfigMaps は、作る ConfigMap の数(プロジェクトの上限の確認に使う)。
	Create(requestContext context.Context, apply *model.TemplateApply, newConfigMaps int) error

	// FindByID は、ユーザーのプロジェクトの実行を 1 件取得する。
	FindByID(requestContext context.Context, userID string, projectID string, applyID string) (model.TemplateApply, error)

	// Finish は、実行を終える(running のときだけ変わる)。
	Finish(requestContext context.Context, applyID string, succeeded bool, failureMessage string) (bool, error)
}

// TemplateProjectReader は、プロジェクトの名前を読む(${project.name} に使う)。
type TemplateProjectReader interface {
	// FindByID は、ユーザーのプロジェクトを 1 件取得する。
	FindByID(requestContext context.Context, userID string, projectID string) (model.Project, error)
}

// TemplateQuotaReader は、ユーザーの上限と使用量を読む(作る前の、まとめての確認に使う)。
type TemplateQuotaReader interface {
	// Get は、上限と使用量を返す。
	Get(requestContext context.Context, userID string) (model.QuotaLimits, model.QuotaUsage, error)
}

// TemplateApplyStarter は、テンプレートの実行の Workflow を開始するもの。
type TemplateApplyStarter interface {
	// StartTemplateApply は、実行の Workflow を開始する。完了は待たない。
	StartTemplateApply(requestContext context.Context, applyID string) error
}

// TemplateApplyService は、テンプレートから部品を作る処理の、受け付け(同期の部分)と、進み具合の取得を担当する。
type TemplateApplyService struct {
	registry        *template.Registry
	applyStore      TemplateApplyStore
	projectReader   TemplateProjectReader
	quotaReader     TemplateQuotaReader
	hostGenerator   IngressHostGenerator
	workflowStarter TemplateApplyStarter
}

// NewTemplateApplyService は、TemplateApplyService を作る。
//
// 引数:
//   - registry: 読み込み済みのテンプレート。
//   - applyStore: 実行の保存先。
//   - projectReader: プロジェクトの読み取り。
//   - quotaReader: 上限と使用量の読み取り。
//   - hostGenerator: ingress のホスト名を作るもの。
//   - workflowStarter: Workflow を開始するもの。
//
// 戻り値:
//   - 作成した TemplateApplyService。
func NewTemplateApplyService(registry *template.Registry, applyStore TemplateApplyStore, projectReader TemplateProjectReader, quotaReader TemplateQuotaReader, hostGenerator IngressHostGenerator, workflowStarter TemplateApplyStarter) *TemplateApplyService {
	// 依存するものを持たせて返す。
	return &TemplateApplyService{registry: registry, applyStore: applyStore, projectReader: projectReader, quotaReader: quotaReader, hostGenerator: hostGenerator, workflowStarter: workflowStarter}
}

// Apply は、テンプレートから部品を作る実行を、受け付ける。
//
// 処理の流れ:
//  1. テンプレートとプロジェクトを確かめる(無ければ not_found)。
//  2. 変数の値を検査して、補う(自動生成のパスワードなど。不正なら invalid_request)。
//  3. 計画を作る(展開。${service.X.dns} は、目印にしておく)。
//  4. クォータを、全体で確かめる(超えるなら conflict。何も作らない)。
//  5. 計画を保存して(プロジェクトが active でなければ conflict)、Workflow を開始する。開始できなければ、実行を失敗にして、エラーを返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: 作る先のプロジェクトの ID。
//   - templateID: テンプレートの id。
//   - input: 利用者が入力した変数の値。
//
// 戻り値:
//   - 実行の ID と、全ての変数の値(自動生成を含む)。
//   - 上記のエラー。
func (templateApplyService *TemplateApplyService) Apply(requestContext context.Context, userID string, projectID string, templateID string, input map[string]string) (string, map[string]string, error) {
	// 1. テンプレートとプロジェクトを確かめる。
	found, exists := templateApplyService.registry.Get(templateID)
	if !exists {
		return "", nil, apperror.New(apperror.KindNotFound, "テンプレートが見つかりません")
	}
	project, projectError := templateApplyService.projectReader.FindByID(requestContext, userID, projectID)
	if projectError != nil {
		return "", nil, projectError
	}
	// 2. 変数の値を、検査して、補う。
	values, valuesError := template.ResolveValues(found, input)
	if valuesError != nil {
		return "", nil, apperror.New(apperror.KindInvalidRequest, valuesError.Error())
	}
	// 3. 計画を作る。ingress があるときだけ、ホスト名の候補を用意する。
	summary := found.Summarize()
	var hosts []string
	if summary.Ingress {
		for len(hosts) < maxTemplateHosts {
			host, hostError := templateApplyService.hostGenerator.Generate()
			if hostError != nil {
				return "", nil, apperror.Wrap(apperror.KindInternal, "ホスト名を払い出せませんでした", hostError)
			}
			hosts = append(hosts, host)
		}
	}
	tokenBytes := make([]byte, 8)
	if _, randomError := rand.Read(tokenBytes); randomError != nil {
		return "", nil, apperror.Wrap(apperror.KindInternal, "乱数を得られませんでした", randomError)
	}
	plan, planError := template.BuildPlan(found, values, template.PlanOptions{
		ProjectName: project.Name, DNSToken: hex.EncodeToString(tokenBytes), Hosts: hosts,
		NewID: func(prefix string) string { return prefix + uuid.NewString() },
	})
	if planError != nil {
		return "", nil, apperror.New(apperror.KindInvalidRequest, "テンプレートを展開できませんでした: "+planError.Error())
	}
	// 4. クォータを、全体で確かめる。
	limits, usage, quotaError := templateApplyService.quotaReader.Get(requestContext, userID)
	if quotaError != nil {
		return "", nil, quotaError
	}
	if batchError := limits.CheckBatch(usage, template.Needs(plan)); batchError != nil {
		return "", nil, batchError
	}
	// 5. 保存する。
	valuesJSON, valuesMarshalError := json.Marshal(values)
	planJSON, planMarshalError := json.Marshal(plan)
	stepsJSON, stepsMarshalError := json.Marshal(plan.InitialStates())
	if valuesMarshalError != nil || planMarshalError != nil || stepsMarshalError != nil {
		return "", nil, apperror.New(apperror.KindInternal, "計画を保存する形にできませんでした")
	}
	apply := model.TemplateApply{
		ID: model.TemplateApplyIDPrefix + uuid.NewString(), ProjectID: projectID, UserID: userID, TemplateID: templateID,
		Values: datatypes.JSON(valuesJSON), Plan: datatypes.JSON(planJSON), Steps: datatypes.JSON(stepsJSON),
	}
	if createError := templateApplyService.applyStore.Create(requestContext, &apply, summary.ConfigMaps); createError != nil {
		return "", nil, createError
	}
	// Workflow を開始する。開始できなければ、実行を失敗にして(止まったままにしない)、エラーを返す。
	if startError := templateApplyService.workflowStarter.StartTemplateApply(requestContext, apply.ID); startError != nil {
		_, _ = templateApplyService.applyStore.Finish(context.WithoutCancel(requestContext), apply.ID, false, "作成の処理を開始できませんでした")
		return "", nil, apperror.Wrap(apperror.KindInternal, "テンプレートの実行を開始できませんでした", startError)
	}
	return apply.ID, values, nil
}

// Get は、テンプレートの実行の、進み具合を返す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - projectID: プロジェクトの ID。
//   - applyID: 実行の ID。
//
// 戻り値:
//   - 実行と、ステップごとの進み具合。無い(他人のものを含む)ときは not_found。
func (templateApplyService *TemplateApplyService) Get(requestContext context.Context, userID string, projectID string, applyID string) (model.TemplateApply, []templateplan.StepState, error) {
	// 保存先から読む。
	apply, findError := templateApplyService.applyStore.FindByID(requestContext, userID, projectID, applyID)
	if findError != nil {
		return model.TemplateApply{}, nil, findError
	}
	// 進み具合を読み込む。
	var states []templateplan.StepState
	if unmarshalError := json.Unmarshal(apply.Steps, &states); unmarshalError != nil {
		return model.TemplateApply{}, nil, apperror.Wrap(apperror.KindInternal, "進み具合を読み込めませんでした", unmarshalError)
	}
	return apply, states, nil
}
