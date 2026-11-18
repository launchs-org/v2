package activity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"go.temporal.io/sdk/temporal"
	"gorm.io/datatypes"

	"launchs/shared/apperror"
	"launchs/shared/model"
	"launchs/shared/repository"
	"launchs/shared/templateplan"
	"launchs/shared/workflow"
)

// fakeTemplateStores は、テンプレートの実行の Activity が使う、全ての保存先の、テスト用の代わり(1 つの型で、全てを満たす)。
type fakeTemplateStores struct {
	apply         model.TemplateApply
	updates       []templateplan.StepState
	finished      []bool
	volumes       map[string]model.Volume
	configMaps    map[string]bool
	deploys       map[string]model.Deploy
	services      map[string]model.Service
	ingresses     map[string]model.Ingress
	createError   error
	takenHosts    map[string]bool
	savedRules    []model.IngressRule
	beginCalls    int
	triedHosts    []string
	savedFilesFor map[string][]model.ConfigMapFile
}

// templateApplyAdapter など、メソッド名が重なる保存先は、小さな型に分けて満たす。
type templateApplyAdapter struct{ stores *fakeTemplateStores }

func (adapter templateApplyAdapter) FindForWorker(_ context.Context, _ string) (model.TemplateApply, error) {
	return adapter.stores.apply, nil
}
func (adapter templateApplyAdapter) UpdateStep(_ context.Context, _ string, index int, state templateplan.StepState) error {
	adapter.stores.updates = append(adapter.stores.updates, state)
	return nil
}
func (adapter templateApplyAdapter) Finish(_ context.Context, _ string, succeeded bool, _ string) (bool, error) {
	adapter.stores.finished = append(adapter.stores.finished, succeeded)
	return true, nil
}

type templateVolumeAdapter struct{ stores *fakeTemplateStores }

func (adapter templateVolumeAdapter) Create(_ context.Context, volume *model.Volume) error {
	if adapter.stores.createError != nil {
		return adapter.stores.createError
	}
	adapter.stores.volumes[volume.ID] = *volume
	return nil
}
func (adapter templateVolumeAdapter) FindForWorker(_ context.Context, volumeID string) (model.Volume, error) {
	volume, exists := adapter.stores.volumes[volumeID]
	if !exists {
		return model.Volume{}, apperror.New(apperror.KindNotFound, "無い")
	}
	return volume, nil
}

type templateConfigMapAdapter struct{ stores *fakeTemplateStores }

func (adapter templateConfigMapAdapter) Create(_ context.Context, configMap *model.ConfigMap, files []model.ConfigMapFile) error {
	if adapter.stores.createError != nil {
		return adapter.stores.createError
	}
	adapter.stores.configMaps[configMap.ID] = true
	adapter.stores.savedFilesFor[configMap.Name] = files
	return nil
}
func (adapter templateConfigMapAdapter) FindRevisionForWorker(_ context.Context, configMapID string, _ int) (model.ConfigMapRevision, error) {
	if !adapter.stores.configMaps[configMapID] {
		return model.ConfigMapRevision{}, apperror.New(apperror.KindNotFound, "無い")
	}
	return model.ConfigMapRevision{}, nil
}

type templateDeployAdapter struct{ stores *fakeTemplateStores }

func (adapter templateDeployAdapter) Create(_ context.Context, deploy *model.Deploy) error {
	if adapter.stores.createError != nil {
		return adapter.stores.createError
	}
	adapter.stores.deploys[deploy.ID] = *deploy
	return nil
}
func (adapter templateDeployAdapter) FindForWorker(_ context.Context, deployID string) (model.Deploy, error) {
	deploy, exists := adapter.stores.deploys[deployID]
	if !exists {
		return model.Deploy{}, apperror.New(apperror.KindNotFound, "無い")
	}
	return deploy, nil
}

type templateServiceAdapter struct{ stores *fakeTemplateStores }

func (adapter templateServiceAdapter) Create(_ context.Context, service *model.Service) error {
	adapter.stores.services[service.ID] = *service
	return nil
}
func (adapter templateServiceAdapter) FindForWorker(_ context.Context, serviceID string) (model.Service, error) {
	service, exists := adapter.stores.services[serviceID]
	if !exists {
		return model.Service{}, apperror.New(apperror.KindNotFound, "無い")
	}
	return service, nil
}

type templateIngressAdapter struct{ stores *fakeTemplateStores }

func (adapter templateIngressAdapter) Create(_ context.Context, ingress *model.Ingress) error {
	adapter.stores.triedHosts = append(adapter.stores.triedHosts, ingress.Host)
	if adapter.stores.takenHosts[ingress.Host] {
		return repository.ErrIngressHostTaken
	}
	ingress.Status = model.IngressStatusActive
	adapter.stores.ingresses[ingress.ID] = *ingress
	return nil
}
func (adapter templateIngressAdapter) FindForWorker(_ context.Context, ingressID string) (model.Ingress, error) {
	ingress, exists := adapter.stores.ingresses[ingressID]
	if !exists {
		return model.Ingress{}, apperror.New(apperror.KindNotFound, "無い")
	}
	return ingress, nil
}
func (adapter templateIngressAdapter) SavePendingRules(_ context.Context, _ string, _ string, _ string, rules []model.IngressRule) error {
	adapter.stores.savedRules = rules
	return nil
}
func (adapter templateIngressAdapter) BeginRulesApply(_ context.Context, _ string, _ string, ingressID string) (string, error) {
	adapter.stores.beginCalls++
	ingress := adapter.stores.ingresses[ingressID]
	ingress.Status = model.IngressStatusApplying
	adapter.stores.ingresses[ingressID] = ingress
	return model.IngressStatusActive, nil
}

// testTemplatePlan は、volume、configmap、db(+service)、web(+service。db の DNS 名を参照)、ingress の計画を作る。
func testTemplatePlan() templateplan.Plan {
	token := "tok"
	return templateplan.Plan{ProjectName: "demo", DNSToken: token, Hosts: []string{"a.example", "b.example"}, Steps: []templateplan.Step{
		{Kind: templateplan.StepVolume, Name: "data", ID: "volume-1", Volume: &templateplan.VolumePlan{SizeMB: 512}},
		{Kind: templateplan.StepConfigMap, Name: "conf", ID: "configmap-1", ConfigMap: &templateplan.ConfigMapPlan{Files: []model.ConfigMapFile{{Key: "a.conf", Encoding: "text", Content: "x"}}}},
		{Kind: templateplan.StepDeploy, Name: "db", ID: "deploy-db", Deploy: &templateplan.DeployPlan{Image: "mariadb:11", Spec: "medium", Replicas: 1, Env: map[string]string{"P": "pw"},
			Mounts: []templateplan.MountPlan{{Volume: "data", Path: "/data"}}, ConfigMaps: []templateplan.ConfigMapMountPlan{{ConfigMap: "conf", Key: "a.conf", Path: "/etc/a.conf", Mode: "0600"}},
			Files: []model.FileSpec{{Path: "/etc/f", Content: "f"}}}},
		{Kind: templateplan.StepService, Name: "db", ID: "service-db", Service: &templateplan.ServicePlan{Deploy: "db", Ports: []model.ServicePort{{Name: "mysql", Port: 3306, Protocol: "tcp"}}}},
		{Kind: templateplan.StepDeploy, Name: "web", ID: "deploy-web", Deploy: &templateplan.DeployPlan{Image: "wp", Spec: "small", Replicas: 1, Env: map[string]string{"DB_HOST": templateplan.DNSToken(token, "db")}, Args: []string{"--db=" + templateplan.DNSToken(token, "db")}}},
		{Kind: templateplan.StepService, Name: "web", ID: "service-web", Service: &templateplan.ServicePlan{Deploy: "web", Ports: []model.ServicePort{{Name: "http", Port: 80, Protocol: "tcp"}}}},
		{Kind: templateplan.StepIngress, Name: "ingress", ID: "ingress-1", Ingress: &templateplan.IngressPlan{Rules: []templateplan.IngressRulePlan{{Path: "/", Deploy: "web", Port: 80}}}},
	}}
}

// newTemplateActivities は、計画と、進み具合(完了した service)から、テスト用の ApplyTemplateActivities と保存先を作る。
//
// 引数:
//   - doneServices: 完了しているものとして扱う service のステップの位置から、DNS 名への対応。
//
// 戻り値:
//   - Activity と、保存先の代わり。
func newTemplateActivities(doneServices map[int]string) (*ApplyTemplateActivities, *fakeTemplateStores) {
	plan := testTemplatePlan()
	planJSON, _ := json.Marshal(plan)
	states := plan.InitialStates()
	stores := &fakeTemplateStores{volumes: map[string]model.Volume{}, configMaps: map[string]bool{}, deploys: map[string]model.Deploy{}, services: map[string]model.Service{}, ingresses: map[string]model.Ingress{}, savedFilesFor: map[string][]model.ConfigMapFile{}}
	for index, dns := range doneServices {
		states[index].Status = templateplan.StatusDone
		dnsName := dns
		stores.services[plan.Steps[index].ID] = model.Service{ID: plan.Steps[index].ID, DNSName: &dnsName}
	}
	statesJSON, _ := json.Marshal(states)
	stores.apply = model.TemplateApply{ID: "apply-abcdef0123456789", ProjectID: "project-1", UserID: "user-a", TemplateID: "wp", Status: model.TemplateApplyStatusRunning, Plan: datatypes.JSON(planJSON), Steps: datatypes.JSON(statesJSON)}
	activities := NewApplyTemplateActivities(templateApplyAdapter{stores}, templateVolumeAdapter{stores}, templateConfigMapAdapter{stores}, templateDeployAdapter{stores}, templateServiceAdapter{stores}, templateIngressAdapter{stores}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return activities, stores
}

// TestApplyTemplateActivities は、計画のステップから、行を作ること(名前、ID、マウント、DNS 名の置き換え)と、冪等性、規則違反のリトライ可否を確認する。
func TestApplyTemplateActivities(testingT *testing.T) {
	ctx := context.Background()
	step := func(index int) workflow.TemplateStepInput {
		return workflow.TemplateStepInput{ApplyID: "apply-abcdef0123456789", Index: index}
	}

	testingT.Run("LoadTemplateSteps は、種類と name を、順に返す", func(subTest *testing.T) {
		activities, _ := newTemplateActivities(nil)
		refs, loadError := activities.LoadTemplateSteps(ctx, workflow.ApplyTemplateInput{ApplyID: "apply-abcdef0123456789"})
		if loadError != nil || len(refs) != 7 || refs[0].Kind != "volume" || refs[2].Name != "db" || refs[6].Kind != "ingress" {
			subTest.Errorf("7 つのはず: %v %+v", loadError, refs)
		}
	})
	testingT.Run("volume と configmap は、実行ごとに一意の名前(<テンプレート>-<name>-<実行の ID の先頭 6 文字>)で作り、2 回目は何もしない", func(subTest *testing.T) {
		activities, stores := newTemplateActivities(nil)
		for repeat := 0; repeat < 2; repeat++ {
			volumeResult, volumeError := activities.CreateTemplateVolume(ctx, step(0))
			configMapResult, configMapError := activities.CreateTemplateConfigMap(ctx, step(1))
			if volumeError != nil || configMapError != nil || volumeResult.ResourceID != "volume-1" || configMapResult.ResourceID != "configmap-1" {
				subTest.Fatalf("作れるはず: %v %v", volumeError, configMapError)
			}
		}
		if stores.volumes["volume-1"].Name != "wp-data-abcdef" || stores.volumes["volume-1"].SizeMB != 512 || stores.volumes["volume-1"].UserID != "user-a" || len(stores.savedFilesFor) != 1 {
			subTest.Errorf("名前と大きさのはず: %+v %v", stores.volumes["volume-1"], stores.savedFilesFor)
		}
		if _, exists := stores.savedFilesFor["wp-conf-abcdef"]; !exists {
			subTest.Errorf("configmap の名前のはず: %v", stores.savedFilesFor)
		}
	})
	testingT.Run("deploy は、最初から、マウント、ファイル、ConfigMap のマウント(版 1)を持った形で作る", func(subTest *testing.T) {
		activities, stores := newTemplateActivities(nil)
		if _, createError := activities.CreateTemplateDeploy(ctx, step(2)); createError != nil {
			subTest.Fatal(createError)
		}
		deploy := stores.deploys["deploy-db"]
		var mounts []model.MountSpec
		var configMapMounts []model.ConfigMapMountSpec
		_ = json.Unmarshal(deploy.Mounts, &mounts)
		_ = json.Unmarshal(deploy.ConfigMapMounts, &configMapMounts)
		if deploy.Status != model.DeployStatusPending || deploy.ImageRef != "mariadb:11" || deploy.Spec != "medium" || deploy.ProjectID != "project-1" ||
			len(mounts) != 1 || mounts[0] != (model.MountSpec{VolumeID: "volume-1", MountPath: "/data"}) ||
			len(configMapMounts) != 1 || configMapMounts[0] != (model.ConfigMapMountSpec{ConfigMapID: "configmap-1", Revision: 1, Key: "a.conf", Path: "/etc/a.conf", Mode: "0600"}) ||
			!strings.Contains(string(deploy.Files), "/etc/f") || !strings.Contains(string(deploy.Env), `"P":"pw"`) || deploy.Command != nil || len(deploy.Args) != 0 {
			subTest.Errorf("計画どおりの deploy のはず: %+v", deploy)
		}
	})
	testingT.Run("deploy は、完了した service の DNS 名で、env と args の目印を置き換える", func(subTest *testing.T) {
		activities, stores := newTemplateActivities(map[int]string{3: "db.project-1.svc.cluster.local"})
		if _, createError := activities.CreateTemplateDeploy(ctx, step(4)); createError != nil {
			subTest.Fatal(createError)
		}
		deploy := stores.deploys["deploy-web"]
		if !strings.Contains(string(deploy.Env), `"DB_HOST":"db.project-1.svc.cluster.local"`) || !strings.Contains(string(deploy.Args), "--db=db.project-1.svc.cluster.local") || strings.Contains(string(deploy.Env), "@@dns-") {
			subTest.Errorf("DNS 名に置き換わるはず: %s %s", deploy.Env, deploy.Args)
		}
	})
	testingT.Run("service がまだ完了していなければ、目印が残るので、作らず、リトライしないエラー", func(subTest *testing.T) {
		activities, stores := newTemplateActivities(nil)
		_, createError := activities.CreateTemplateDeploy(ctx, step(4))
		var applicationError *temporal.ApplicationError
		if !errors.As(createError, &applicationError) || !applicationError.NonRetryable() || len(stores.deploys) != 0 {
			subTest.Errorf("作らず、リトライしないエラーのはず: %v %d", createError, len(stores.deploys))
		}
	})
	testingT.Run("deploy を 2 回実行しても、二重に作らない", func(subTest *testing.T) {
		activities, stores := newTemplateActivities(nil)
		activities.CreateTemplateDeploy(ctx, step(2))
		stores.createError = errors.New("2 回目は、作ろうとしないはず")
		if result, createError := activities.CreateTemplateDeploy(ctx, step(2)); createError != nil || result.ResourceID != "deploy-db" {
			subTest.Errorf("すでにあるので、何もしないはず: %v", createError)
		}
	})
	testingT.Run("service は、計画の deploy の ID に、ポートつきで作る", func(subTest *testing.T) {
		activities, stores := newTemplateActivities(nil)
		if _, createError := activities.CreateTemplateService(ctx, step(3)); createError != nil {
			subTest.Fatal(createError)
		}
		service := stores.services["service-db"]
		if service.DeployID != "deploy-db" || service.UserID != "user-a" || !strings.Contains(string(service.Ports), `"port":3306`) {
			subTest.Errorf("deploy-db の service のはず: %+v", service)
		}
	})
	testingT.Run("ingress は、ホスト名が重なったら次の候補を使い、ルールの転送先は service の ID。反映を始める(applying)", func(subTest *testing.T) {
		activities, stores := newTemplateActivities(nil)
		stores.takenHosts = map[string]bool{"a.example": true}
		if _, createError := activities.CreateTemplateIngress(ctx, step(6)); createError != nil {
			subTest.Fatal(createError)
		}
		ingress := stores.ingresses["ingress-1"]
		if ingress.Host != "b.example" || strings.Join(stores.triedHosts, ",") != "a.example,b.example" || ingress.Status != model.IngressStatusApplying {
			subTest.Errorf("2 番目の候補で作り、applying のはず: %+v %v", ingress, stores.triedHosts)
		}
		if len(stores.savedRules) != 1 || stores.savedRules[0] != (model.IngressRule{Path: "/", ServiceID: "service-web", Port: 80}) || stores.beginCalls != 1 {
			subTest.Errorf("ルールは service-web に向くはず: %+v", stores.savedRules)
		}
		// 再実行(すでに applying)は、何もしない。
		if _, repeatError := activities.CreateTemplateIngress(ctx, step(6)); repeatError != nil || stores.beginCalls != 1 {
			subTest.Errorf("再実行では、反映を、二重に始めないはず: %v %d", repeatError, stores.beginCalls)
		}
	})
	testingT.Run("ホスト名の候補が尽きたら、リトライしないエラー", func(subTest *testing.T) {
		activities, stores := newTemplateActivities(nil)
		stores.takenHosts = map[string]bool{"a.example": true, "b.example": true}
		_, createError := activities.CreateTemplateIngress(ctx, step(6))
		var applicationError *temporal.ApplicationError
		if !errors.As(createError, &applicationError) || !applicationError.NonRetryable() {
			subTest.Errorf("リトライしないエラーのはず: %v", createError)
		}
	})
	testingT.Run("クォータなどの規則で断られたら、リトライしない。一時的なエラーは、そのまま返す(リトライされる)", func(subTest *testing.T) {
		activities, stores := newTemplateActivities(nil)
		stores.createError = apperror.New(apperror.KindConflict, "上限です")
		_, rejected := activities.CreateTemplateVolume(ctx, step(0))
		var applicationError *temporal.ApplicationError
		if !errors.As(rejected, &applicationError) || !applicationError.NonRetryable() {
			subTest.Errorf("リトライしないはず: %v", rejected)
		}
		stores.createError = errors.New("database down")
		_, transient := activities.CreateTemplateVolume(ctx, step(0))
		if errors.As(transient, &applicationError) && applicationError.NonRetryable() {
			subTest.Errorf("リトライされるはず: %v", transient)
		}
	})
	testingT.Run("種類が違うステップは、リトライしないエラー", func(subTest *testing.T) {
		activities, _ := newTemplateActivities(nil)
		_, createError := activities.CreateTemplateVolume(ctx, step(2))
		var applicationError *temporal.ApplicationError
		if !errors.As(createError, &applicationError) || !applicationError.NonRetryable() {
			subTest.Errorf("リトライしないエラーのはず: %v", createError)
		}
	})
	testingT.Run("進み具合の記録と、結果の確定は、保存先に渡す。種類と name は、計画から引き継ぐ", func(subTest *testing.T) {
		activities, stores := newTemplateActivities(nil)
		if updateError := activities.UpdateTemplateStep(ctx, workflow.UpdateTemplateStepInput{ApplyID: "apply-abcdef0123456789", Index: 2, Status: templateplan.StatusFailed, ResourceID: "deploy-db", Error: "boom"}); updateError != nil {
			subTest.Fatal(updateError)
		}
		activities.FinishTemplateApply(ctx, workflow.FinishTemplateApplyInput{ApplyID: "apply-abcdef0123456789", Succeeded: false, Error: "boom"})
		if len(stores.updates) != 1 || stores.updates[0] != (templateplan.StepState{Kind: "deploy", Name: "db", Status: "failed", ResourceID: "deploy-db", Error: "boom"}) || len(stores.finished) != 1 || stores.finished[0] {
			subTest.Errorf("記録されるはず: %+v %v", stores.updates, stores.finished)
		}
	})
}

// TestResourceName は、部品の名前の、形と長さの上限を確認する。
func TestResourceName(testingT *testing.T) {
	apply := model.TemplateApply{ID: "apply-0123456789abcdef", TemplateID: "code-server"}
	if name := resourceName(apply, "home"); name != "code-server-home-012345" {
		testingT.Errorf("形が違う: %s", name)
	}
	long := model.TemplateApply{ID: "apply-0123456789abcdef", TemplateID: strings.Repeat("a", 63)}
	name := resourceName(long, "data")
	if len(name) > model.MaxVolumeNameLength || model.ValidateVolumeName(name) != nil || !strings.HasSuffix(name, "-data-012345") {
		testingT.Errorf("64 文字以内で、名前の規則に合うはず: %s(%d)", name, len(name))
	}
}
