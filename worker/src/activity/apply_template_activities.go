package activity

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"go.temporal.io/sdk/temporal"
	"gorm.io/datatypes"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/repository"
	"launchs/shared/templateplan"
	"launchs/shared/workflow"
)

// TemplateApplyWorkerStore は、テンプレートの実行の、記録の読み書き。
type TemplateApplyWorkerStore interface {
	// FindForWorker は、実行を ID で 1 件取得する。
	FindForWorker(requestContext context.Context, applyID string) (model.TemplateApply, error)

	// UpdateStep は、ステップ 1 つの進み具合を書き換える。
	UpdateStep(requestContext context.Context, applyID string, index int, state templateplan.StepState) error

	// Finish は、実行を終える(running のときだけ変わる)。
	Finish(requestContext context.Context, applyID string, succeeded bool, failureMessage string) (bool, error)
}

// TemplateVolumeStore は、volume の作成と、存在の確認。
type TemplateVolumeStore interface {
	// Create は、volume を保存する(クォータを確かめる)。
	Create(requestContext context.Context, volume *model.Volume) error

	// FindForWorker は、volume を ID で 1 件取得する。
	FindForWorker(requestContext context.Context, volumeID string) (model.Volume, error)
}

// TemplateConfigMapStore は、ConfigMap の作成と、存在の確認。
type TemplateConfigMapStore interface {
	// Create は、ConfigMap を、版 1 のファイルと一緒に保存する。
	Create(requestContext context.Context, configMap *model.ConfigMap, files []model.ConfigMapFile) error

	// FindRevisionForWorker は、ConfigMap の版を 1 件取得する(あるかの確認に使う)。
	FindRevisionForWorker(requestContext context.Context, configMapID string, revision int) (model.ConfigMapRevision, error)
}

// TemplateDeployStore は、deploy の作成と、存在の確認。
type TemplateDeployStore interface {
	// Create は、deploy を保存する(クォータを確かめる)。
	Create(requestContext context.Context, deploy *model.Deploy) error

	// FindForWorker は、deploy を ID で 1 件取得する。
	FindForWorker(requestContext context.Context, deployID string) (model.Deploy, error)
}

// TemplateServiceStore は、service の作成と、読み取り。
type TemplateServiceStore interface {
	// Create は、service を保存する。
	Create(requestContext context.Context, service *model.Service) error

	// FindForWorker は、service を ID で 1 件取得する。
	FindForWorker(requestContext context.Context, serviceID string) (model.Service, error)
}

// TemplateIngressStore は、ingress の作成と、ルールの保存・反映の開始。
type TemplateIngressStore interface {
	// Create は、ingress を保存する。ホスト名が重なったときは repository.ErrIngressHostTaken。
	Create(requestContext context.Context, ingress *model.Ingress) error

	// FindForWorker は、ingress を ID で 1 件取得する。
	FindForWorker(requestContext context.Context, ingressID string) (model.Ingress, error)

	// SavePendingRules は、ルールを、まだ適用しない値として保存する。
	SavePendingRules(requestContext context.Context, userID string, projectID string, ingressID string, rules []model.IngressRule) error

	// BeginRulesApply は、ルールの反映を始める(状態を applying にする)。
	BeginRulesApply(requestContext context.Context, userID string, projectID string, ingressID string) (string, error)
}

// ApplyTemplateActivities は、テンプレートの実行の Workflow が使う Activity をまとめたもの。
// 計画のステップを実行して、データベースの行を作る(k8s への反映は、既存の子 Workflow が行う)。
type ApplyTemplateActivities struct {
	applyStore     TemplateApplyWorkerStore
	volumeStore    TemplateVolumeStore
	configMapStore TemplateConfigMapStore
	deployStore    TemplateDeployStore
	serviceStore   TemplateServiceStore
	ingressStore   TemplateIngressStore
	appLogger      *slog.Logger
}

// NewApplyTemplateActivities は、ApplyTemplateActivities を作る。
//
// 引数:
//   - applyStore: 実行の記録。
//   - volumeStore、configMapStore、deployStore、serviceStore、ingressStore: 各部品の保存先。
//   - appLogger: ログの出力先。
//
// 戻り値:
//   - 作成した ApplyTemplateActivities。
func NewApplyTemplateActivities(applyStore TemplateApplyWorkerStore, volumeStore TemplateVolumeStore, configMapStore TemplateConfigMapStore, deployStore TemplateDeployStore, serviceStore TemplateServiceStore, ingressStore TemplateIngressStore, appLogger *slog.Logger) *ApplyTemplateActivities {
	// 受け取ったものを持たせる。
	return &ApplyTemplateActivities{applyStore: applyStore, volumeStore: volumeStore, configMapStore: configMapStore, deployStore: deployStore, serviceStore: serviceStore, ingressStore: ingressStore, appLogger: appLogger}
}

// loadPlan は、実行と計画を読み込む。計画が壊れているのは、何度やっても同じなので、リトライしない。
//
// 引数:
//   - requestContext: Temporal が渡す context。
//   - applyID: 実行の ID。
//
// 戻り値:
//   - 実行と、計画。
//   - 読めないとき、計画が壊れているときのエラー。
func (applyTemplateActivities *ApplyTemplateActivities) loadPlan(requestContext context.Context, applyID string) (model.TemplateApply, templateplan.Plan, error) {
	// 実行を読む。
	apply, findError := applyTemplateActivities.applyStore.FindForWorker(requestContext, applyID)
	if findError != nil {
		return model.TemplateApply{}, templateplan.Plan{}, findError
	}
	// 計画を読み込む。
	var plan templateplan.Plan
	if unmarshalError := json.Unmarshal(apply.Plan, &plan); unmarshalError != nil {
		return model.TemplateApply{}, templateplan.Plan{}, temporal.NewNonRetryableApplicationError("計画が壊れています", "InvalidPlan", unmarshalError)
	}
	return apply, plan, nil
}

// loadStep は、実行と計画を読み、ステップを 1 つ取り出す。種類が違えば、リトライしないエラー。
//
// 引数:
//   - requestContext: Temporal が渡す context。
//   - input: 実行の ID とステップの位置。
//   - kind: 期待するステップの種類。
//
// 戻り値:
//   - 実行、計画、ステップ。
//   - 読めない、位置が範囲外、種類が違うときのエラー。
func (applyTemplateActivities *ApplyTemplateActivities) loadStep(requestContext context.Context, input workflow.TemplateStepInput, kind string) (model.TemplateApply, templateplan.Plan, templateplan.Step, error) {
	// 実行と計画を読む。
	apply, plan, loadError := applyTemplateActivities.loadPlan(requestContext, input.ApplyID)
	if loadError != nil {
		return model.TemplateApply{}, templateplan.Plan{}, templateplan.Step{}, loadError
	}
	if input.Index < 0 || input.Index >= len(plan.Steps) || plan.Steps[input.Index].Kind != kind {
		return model.TemplateApply{}, templateplan.Plan{}, templateplan.Step{}, temporal.NewNonRetryableApplicationError("ステップが、期待した種類ではありません: "+kind, "InvalidStep", nil)
	}
	return apply, plan, plan.Steps[input.Index], nil
}

// resourceName は、作る部品の名前(プロジェクトの中で、一意)を決める。「<テンプレート>-<name>-<実行の ID の先頭 6 文字>」。
// 同じ実行は、同じ名前になる(再実行しても、二重に作らない)。別の実行は、別の名前になる(同じテンプレートを、何度でも作れる)。
//
// 引数:
//   - apply: 実行。
//   - name: テンプレートの中の部品の name。
//
// 戻り値:
//   - 部品の名前(英数字とハイフン、64 文字以内)。
func resourceName(apply model.TemplateApply, name string) string {
	// 実行の ID から、接頭辞を除いた先頭 6 文字を使う。
	suffix := strings.TrimPrefix(apply.ID, model.TemplateApplyIDPrefix)
	if len(suffix) > 6 {
		suffix = suffix[:6]
	}
	full := apply.TemplateID + "-" + name + "-" + suffix
	if len(full) > model.MaxVolumeNameLength {
		// 長すぎるときは、テンプレートの id を縮める。
		keep := model.MaxVolumeNameLength - len(name) - len(suffix) - 2
		if keep < 1 {
			keep = 1
		}
		full = apply.TemplateID[:min(keep, len(apply.TemplateID))] + "-" + name + "-" + suffix
	}
	return full
}

// LoadTemplateSteps は、計画のステップの、種類と name を、順に返す(Workflow が、順に実行するため)。
//
// 引数:
//   - requestContext: Temporal が渡す context。
//   - input: 実行の ID。
//
// 戻り値:
//   - ステップの一覧。読めないときのエラー。
func (applyTemplateActivities *ApplyTemplateActivities) LoadTemplateSteps(requestContext context.Context, input workflow.ApplyTemplateInput) ([]workflow.TemplateStepRef, error) {
	// 計画を読んで、種類と name だけを返す。
	_, plan, loadError := applyTemplateActivities.loadPlan(requestContext, input.ApplyID)
	if loadError != nil {
		return nil, loadError
	}
	refs := make([]workflow.TemplateStepRef, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		refs = append(refs, workflow.TemplateStepRef{Kind: step.Kind, Name: step.Name})
	}
	return refs, nil
}

// CreateTemplateVolume は、計画の volume のステップを実行する(行を作る。PVC は、deploy の起動の前に、用意される)。
//
// すでに、同じ ID の volume があれば、何もしない(再実行しても、二重に作らない)。
//
// 引数:
//   - requestContext: Temporal が渡す context。
//   - input: 実行の ID とステップの位置。
//
// 戻り値:
//   - 作った(すでにあった)volume の ID。
//   - 作れなかったとき(クォータ、名前の重複など)のエラー。リトライしても同じなので、データベースの規則で断られたものは、リトライしない。
func (applyTemplateActivities *ApplyTemplateActivities) CreateTemplateVolume(requestContext context.Context, input workflow.TemplateStepInput) (workflow.TemplateStepResult, error) {
	// 実行、計画、ステップを読む。
	apply, _, step, loadError := applyTemplateActivities.loadStep(requestContext, input, templateplan.StepVolume)
	if loadError != nil {
		return workflow.TemplateStepResult{}, loadError
	}
	// すでにあれば、何もしない。
	if _, findError := applyTemplateActivities.volumeStore.FindForWorker(requestContext, step.ID); findError == nil {
		return workflow.TemplateStepResult{ResourceID: step.ID}, nil
	}
	// 行を作る。
	volume := model.Volume{ID: step.ID, ProjectID: apply.ProjectID, UserID: apply.UserID, Name: resourceName(apply, step.Name), SizeMB: step.Volume.SizeMB, Tags: mustJSON(model.TemplateDeployTags(apply.TemplateID, apply.ID))}
	if createError := applyTemplateActivities.volumeStore.Create(requestContext, &volume); createError != nil {
		return workflow.TemplateStepResult{}, nonRetryableIfRule(createError)
	}
	return workflow.TemplateStepResult{ResourceID: step.ID}, nil
}

// CreateTemplateConfigMap は、計画の configmap のステップを実行する(行と版 1 を作る)。すでにあれば、何もしない。
//
// 引数:
//   - requestContext: Temporal が渡す context。
//   - input: 実行の ID とステップの位置。
//
// 戻り値:
//   - 作った(すでにあった)ConfigMap の ID。作れなかったときのエラー。
func (applyTemplateActivities *ApplyTemplateActivities) CreateTemplateConfigMap(requestContext context.Context, input workflow.TemplateStepInput) (workflow.TemplateStepResult, error) {
	// 実行、計画、ステップを読む。
	apply, _, step, loadError := applyTemplateActivities.loadStep(requestContext, input, templateplan.StepConfigMap)
	if loadError != nil {
		return workflow.TemplateStepResult{}, loadError
	}
	// すでにあれば(版 1 がある)、何もしない。
	if _, findError := applyTemplateActivities.configMapStore.FindRevisionForWorker(requestContext, step.ID, 1); findError == nil {
		return workflow.TemplateStepResult{ResourceID: step.ID}, nil
	}
	// 行と版 1 を作る。
	configMap := model.ConfigMap{ID: step.ID, ProjectID: apply.ProjectID, UserID: apply.UserID, Name: resourceName(apply, step.Name)}
	if createError := applyTemplateActivities.configMapStore.Create(requestContext, &configMap, step.ConfigMap.Files); createError != nil {
		return workflow.TemplateStepResult{}, nonRetryableIfRule(createError)
	}
	return workflow.TemplateStepResult{ResourceID: step.ID}, nil
}

// dnsNames は、これまでに作った service の、DNS 名を集める(deploy の name → DNS 名)。
//
// 引数:
//   - requestContext: Temporal が渡す context。
//   - apply: 実行。
//   - plan: 計画。
//
// 戻り値:
//   - 完了した service の、deploy の name から DNS 名への対応。読めないときのエラー。
func (applyTemplateActivities *ApplyTemplateActivities) dnsNames(requestContext context.Context, apply model.TemplateApply, plan templateplan.Plan) (map[string]string, error) {
	// 進み具合を読んで、完了した service のステップを探す。
	var states []templateplan.StepState
	if unmarshalError := json.Unmarshal(apply.Steps, &states); unmarshalError != nil {
		return nil, temporal.NewNonRetryableApplicationError("進み具合が壊れています", "InvalidPlan", unmarshalError)
	}
	names := map[string]string{}
	for index, step := range plan.Steps {
		if step.Kind != templateplan.StepService || index >= len(states) || states[index].Status != templateplan.StatusDone {
			continue
		}
		service, findError := applyTemplateActivities.serviceStore.FindForWorker(requestContext, step.ID)
		if findError != nil {
			return nil, findError
		}
		if service.DNSName != nil && *service.DNSName != "" {
			names[step.Service.Deploy] = *service.DNSName
		}
	}
	return names, nil
}

// CreateTemplateDeploy は、計画の deploy のステップを実行する。
//
// deploy の行を、**最初から、マウント(volume、ConfigMap)とファイルを持った形**で作る(起動の前に、PVC と ConfigMap が用意される)。
// 文字列の値の中の、${service.X.dns} の目印は、ここで、作った service の DNS 名に置き換える(この実行の目印だけ)。
// すでに、同じ ID の deploy があれば、何もしない。
//
// 引数:
//   - requestContext: Temporal が渡す context。
//   - input: 実行の ID とステップの位置。
//
// 戻り値:
//   - 作った(すでにあった)deploy の ID。
//   - 目印が残る、クォータを超えるなど、リトライしても同じものは、リトライしないエラー。
func (applyTemplateActivities *ApplyTemplateActivities) CreateTemplateDeploy(requestContext context.Context, input workflow.TemplateStepInput) (workflow.TemplateStepResult, error) {
	// 実行、計画、ステップを読む。
	apply, plan, step, loadError := applyTemplateActivities.loadStep(requestContext, input, templateplan.StepDeploy)
	if loadError != nil {
		return workflow.TemplateStepResult{}, loadError
	}
	// すでにあれば、何もしない。
	if _, findError := applyTemplateActivities.deployStore.FindForWorker(requestContext, step.ID); findError == nil {
		return workflow.TemplateStepResult{ResourceID: step.ID}, nil
	}
	// 作った service の DNS 名で、目印を置き換える。
	dns, dnsError := applyTemplateActivities.dnsNames(requestContext, apply, plan)
	if dnsError != nil {
		return workflow.TemplateStepResult{}, dnsError
	}
	replace := func(text string) string { return templateplan.ReplaceDNS(text, plan.DNSToken, dns) }
	deployPlan := step.Deploy
	env := map[string]string{}
	for name, value := range deployPlan.Env {
		env[name] = replace(value)
	}
	args := make([]string, 0, len(deployPlan.Args))
	for _, argument := range deployPlan.Args {
		args = append(args, replace(argument))
	}
	files := make([]model.FileSpec, 0, len(deployPlan.Files))
	for _, file := range deployPlan.Files {
		files = append(files, model.FileSpec{Path: file.Path, Content: replace(file.Content)})
	}
	image, command := replace(deployPlan.Image), replace(deployPlan.Command)
	// 目印が残っていたら(参照する service が、まだ無い)、作らない。
	remaining := append([]string{image, command}, args...)
	for _, value := range env {
		remaining = append(remaining, value)
	}
	for _, file := range files {
		remaining = append(remaining, file.Content)
	}
	for _, value := range remaining {
		if templateplan.HasUnresolvedDNS(value, plan.DNSToken) {
			return workflow.TemplateStepResult{}, temporal.NewNonRetryableApplicationError("service の DNS 名が、まだ決まっていません: "+step.Name, "UnresolvedDNS", nil)
		}
	}
	// マウントを、作った volume の ID にする。
	mounts := make([]model.MountSpec, 0, len(deployPlan.Mounts))
	for _, mount := range deployPlan.Mounts {
		mounts = append(mounts, model.MountSpec{VolumeID: plan.IDOf(templateplan.StepVolume, mount.Volume), MountPath: mount.Path})
	}
	// ConfigMap のマウントを、作った ConfigMap の ID と、版 1 にする。
	configMapMounts := make([]model.ConfigMapMountSpec, 0, len(deployPlan.ConfigMaps))
	for _, mount := range deployPlan.ConfigMaps {
		configMapMounts = append(configMapMounts, model.ConfigMapMountSpec{ConfigMapID: plan.IDOf(templateplan.StepConfigMap, mount.ConfigMap), Revision: 1, Key: mount.Key, Path: mount.Path, Mode: mount.Mode})
	}
	deploy := model.Deploy{ID: step.ID, ProjectID: apply.ProjectID, UserID: apply.UserID, ImageRef: image, Spec: deployPlan.Spec, Replicas: deployPlan.Replicas, Status: model.DeployStatusPending}
	deploy.Env = mustJSON(env)
	// テンプレートから作った deploy には、タグ(template:<id> と apply:<適用の識別>)を、自動で付ける。タグ単位の一斉削除に使う。
	deploy.Tags = mustJSON(model.TemplateDeployTags(apply.TemplateID, apply.ID))
	if command != "" {
		deploy.Command = &command
	}
	if deployPlan.FSGroup > 0 {
		fsGroup := deployPlan.FSGroup
		deploy.FSGroup = &fsGroup
	}
	if len(args) > 0 {
		deploy.Args = mustJSON(args)
	}
	if len(mounts) > 0 {
		deploy.Mounts = mustJSON(mounts)
	}
	if len(files) > 0 {
		deploy.Files = mustJSON(files)
	}
	if len(configMapMounts) > 0 {
		deploy.ConfigMapMounts = mustJSON(configMapMounts)
	}
	if createError := applyTemplateActivities.deployStore.Create(requestContext, &deploy); createError != nil {
		return workflow.TemplateStepResult{}, nonRetryableIfRule(createError)
	}
	return workflow.TemplateStepResult{ResourceID: step.ID}, nil
}

// CreateTemplateService は、計画の service のステップを実行する(行を作る。k8s の Service は、子 Workflow が作る)。すでにあれば、何もしない。
//
// 引数:
//   - requestContext: Temporal が渡す context。
//   - input: 実行の ID とステップの位置。
//
// 戻り値:
//   - 作った(すでにあった)service の ID。作れなかったときのエラー。
func (applyTemplateActivities *ApplyTemplateActivities) CreateTemplateService(requestContext context.Context, input workflow.TemplateStepInput) (workflow.TemplateStepResult, error) {
	// 実行、計画、ステップを読む。
	apply, plan, step, loadError := applyTemplateActivities.loadStep(requestContext, input, templateplan.StepService)
	if loadError != nil {
		return workflow.TemplateStepResult{}, loadError
	}
	// すでにあれば、何もしない。
	if _, findError := applyTemplateActivities.serviceStore.FindForWorker(requestContext, step.ID); findError == nil {
		return workflow.TemplateStepResult{ResourceID: step.ID}, nil
	}
	// 公開する deploy の ID は、計画から決まる。
	service := model.Service{ID: step.ID, DeployID: plan.IDOf(templateplan.StepDeploy, step.Service.Deploy), UserID: apply.UserID, Ports: mustJSON(step.Service.Ports), Tags: mustJSON(model.TemplateDeployTags(apply.TemplateID, apply.ID))}
	if createError := applyTemplateActivities.serviceStore.Create(requestContext, &service); createError != nil {
		return workflow.TemplateStepResult{}, nonRetryableIfRule(createError)
	}
	return workflow.TemplateStepResult{ResourceID: step.ID}, nil
}

// CreateTemplateIngress は、計画の ingress のステップを実行する。
//
// ingress の行を作り(ホスト名は、計画の候補を、順に試す。重なったら、次)、ルールを pending に保存して、反映を始める(applying にする)。
// 反映そのものは、子 Workflow(ApplyIngressRulesWorkflow)が行う。途中まで進んでいても、再実行して、続きから進む。
//
// 引数:
//   - requestContext: Temporal が渡す context。
//   - input: 実行の ID とステップの位置。
//
// 戻り値:
//   - ingress の ID。ホスト名の候補が尽きたとき、ルールを保存できなかったときのエラー。
func (applyTemplateActivities *ApplyTemplateActivities) CreateTemplateIngress(requestContext context.Context, input workflow.TemplateStepInput) (workflow.TemplateStepResult, error) {
	// 実行、計画、ステップを読む。
	apply, plan, step, loadError := applyTemplateActivities.loadStep(requestContext, input, templateplan.StepIngress)
	if loadError != nil {
		return workflow.TemplateStepResult{}, loadError
	}
	// 無ければ、ホスト名の候補を順に試して、作る。
	if _, findError := applyTemplateActivities.ingressStore.FindForWorker(requestContext, step.ID); findError != nil {
		created := false
		for _, host := range plan.Hosts {
			ingress := model.Ingress{ID: step.ID, ProjectID: apply.ProjectID, UserID: apply.UserID, Host: host, Tags: mustJSON(model.TemplateDeployTags(apply.TemplateID, apply.ID))}
			createError := applyTemplateActivities.ingressStore.Create(requestContext, &ingress)
			if errors.Is(createError, repository.ErrIngressHostTaken) {
				continue
			}
			if createError != nil {
				return workflow.TemplateStepResult{}, nonRetryableIfRule(createError)
			}
			created = true
			break
		}
		if !created {
			return workflow.TemplateStepResult{}, temporal.NewNonRetryableApplicationError("重ならないホスト名を払い出せませんでした", "HostsExhausted", nil)
		}
	}
	// すでに反映を始めていれば(再実行)、続きは、子 Workflow に任せる。
	ingress, findError := applyTemplateActivities.ingressStore.FindForWorker(requestContext, step.ID)
	if findError != nil {
		return workflow.TemplateStepResult{}, findError
	}
	if ingress.IsApplying() {
		return workflow.TemplateStepResult{ResourceID: step.ID}, nil
	}
	// ルール(転送先は、作った service の ID)を、pending に保存して、反映を始める。
	rules := make([]model.IngressRule, 0, len(step.Ingress.Rules))
	for _, rule := range step.Ingress.Rules {
		rules = append(rules, model.IngressRule{Path: rule.Path, ServiceID: plan.IDOf(templateplan.StepService, rule.Deploy), Port: rule.Port})
	}
	if saveError := applyTemplateActivities.ingressStore.SavePendingRules(requestContext, apply.UserID, apply.ProjectID, step.ID, rules); saveError != nil {
		return workflow.TemplateStepResult{}, nonRetryableIfRule(saveError)
	}
	if _, beginError := applyTemplateActivities.ingressStore.BeginRulesApply(requestContext, apply.UserID, apply.ProjectID, step.ID); beginError != nil {
		return workflow.TemplateStepResult{}, nonRetryableIfRule(beginError)
	}
	return workflow.TemplateStepResult{ResourceID: step.ID}, nil
}

// UpdateTemplateStep は、ステップの進み具合を記録する。
//
// 引数:
//   - requestContext: Temporal が渡す context。
//   - input: 実行の ID、ステップの位置、新しい進み具合。
//
// 戻り値:
//   - 記録できなかったときのエラー。
func (applyTemplateActivities *ApplyTemplateActivities) UpdateTemplateStep(requestContext context.Context, input workflow.UpdateTemplateStepInput) error {
	// 実行の記録から、種類と name を引き継ぐために、計画を読む。
	_, plan, loadError := applyTemplateActivities.loadPlan(requestContext, input.ApplyID)
	if loadError != nil {
		return loadError
	}
	if input.Index < 0 || input.Index >= len(plan.Steps) {
		return temporal.NewNonRetryableApplicationError("ステップの位置が範囲外です", "InvalidStep", nil)
	}
	step := plan.Steps[input.Index]
	return applyTemplateActivities.applyStore.UpdateStep(requestContext, input.ApplyID, input.Index, templateplan.StepState{Kind: step.Kind, Name: step.Name, Status: input.Status, ResourceID: input.ResourceID, Error: input.Error})
}

// FinishTemplateApply は、実行の結果を確定する(成功か、失敗か)。
//
// 引数:
//   - requestContext: Temporal が渡す context。
//   - input: 実行の ID、成功したか、失敗の理由。
//
// 戻り値:
//   - 確定できなかったときのエラー。
func (applyTemplateActivities *ApplyTemplateActivities) FinishTemplateApply(requestContext context.Context, input workflow.FinishTemplateApplyInput) error {
	// 結果をログに残す(失敗は、運用で気づけるよう警告にする)。
	if input.Succeeded {
		applyTemplateActivities.appLogger.Info("テンプレートの実行に成功した", "apply_id", input.ApplyID, "activity", workflow.FinishTemplateApplyActivityName)
	} else {
		applyTemplateActivities.appLogger.Warn("テンプレートの実行に失敗した", "apply_id", input.ApplyID, "activity", workflow.FinishTemplateApplyActivityName)
	}
	// 確定を、保存先に任せる(確定済みなら何もしない)。
	_, finishError := applyTemplateActivities.applyStore.Finish(requestContext, input.ApplyID, input.Succeeded, input.Error)
	return finishError
}

// mustJSON は、値を JSON にする(失敗しない値だけを渡す)。
//
// 引数:
//   - value: JSON にする値。
//
// 戻り値:
//   - JSON。
func mustJSON(value any) datatypes.JSON {
	// 文字列、数、配列、マップだけを渡すので、失敗しない。
	encoded, _ := json.Marshal(value)
	return datatypes.JSON(encoded)
}

// nonRetryableIfRule は、データベースの規則で断られたエラー(クォータ、名前の重複、入力の不正、状態の競合)を、リトライしないエラーにする。
// 一時的なエラー(データベースの障害など)は、そのまま返す(リトライされる)。
//
// 引数:
//   - err: 作成のエラー。
//
// 戻り値:
//   - リトライしない印を付けたエラー、またはそのままのエラー。
func nonRetryableIfRule(err error) error {
	// 規則で断られた種類だけに、印を付ける。
	switch apperror.KindOf(err) {
	case apperror.KindConflict, apperror.KindInvalidRequest, apperror.KindNotFound:
		return temporal.NewNonRetryableApplicationError(err.Error(), "TemplateStepRejected", err)
	}
	return err
}
