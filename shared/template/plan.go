package template

import (
	"fmt"

	"launchs/shared/model"
	"launchs/shared/templateplan"
)

// PlanOptions は、計画を作るときの、テンプレートの外から渡す値。
type PlanOptions struct {
	// ProjectName は、${project.name} になる、プロジェクトの名前。
	ProjectName string

	// DNSToken は、この実行だけの、ランダムな文字列(${service.X.dns} の目印に使う)。
	DNSToken string

	// Hosts は、ingress のホスト名の候補。
	Hosts []string

	// NewID は、部品の ID を作る(引数は、接頭辞。例: "volume-")。
	NewID func(prefix string) string
}

// BuildPlan は、テンプレートと変数の値から、部品を作る「計画」を作る。
//
// 処理の流れ:
//  1. 展開する。${service.X.dns} は、まだ決まらないので、この実行だけの目印に置き換える。
//  2. 部品ごとに、ステップを作り、ID を決める。順番は、volume → configmap → deploy と、その service(参照される側が先)→ ingress。
//  3. 展開したあとの、deploy のファイルと、マウントの組を、改めて検査する(変数の値で、規則に合わなくなることがあるため)。
//
// 引数:
//   - template: テンプレート(Validate を通ったもの)。
//   - values: 全ての変数の値(ResolveValues の結果)。
//   - options: プロジェクトの名前、目印、ホスト名の候補、ID を作る関数。
//
// 戻り値:
//   - 計画。
//   - 展開できないとき、展開したあとの値が規則に合わないときのエラー。
func BuildPlan(template Template, values map[string]string, options PlanOptions) (templateplan.Plan, error) {
	// service を持つ deploy の、DNS 名の目印を用意する。
	dnsTokens := map[string]string{}
	deployResources := map[string]Resource{}
	for _, resource := range template.Resources {
		if resource.Kind == ResourceDeploy {
			deployResources[resource.Name] = resource
			if resource.Service != nil {
				dnsTokens[resource.Name] = templateplan.DNSToken(options.DNSToken, resource.Name)
			}
		}
	}
	// 展開する。
	expanded, expandError := Expand(template, values, ExpandContext{ProjectName: options.ProjectName, ServiceDNS: dnsTokens})
	if expandError != nil {
		return templateplan.Plan{}, expandError
	}
	plan := templateplan.Plan{ProjectName: options.ProjectName, DNSToken: options.DNSToken, Hosts: append([]string(nil), options.Hosts...)}
	// volume、configmap のステップ(元の順)。
	for _, volume := range expanded.Volumes {
		plan.Steps = append(plan.Steps, templateplan.Step{Kind: templateplan.StepVolume, Name: volume.Name, ID: options.NewID(model.VolumeIDPrefix), Volume: &templateplan.VolumePlan{SizeMB: volume.SizeMB}})
	}
	for _, configMap := range expanded.ConfigMaps {
		plan.Steps = append(plan.Steps, templateplan.Step{Kind: templateplan.StepConfigMap, Name: configMap.Name, ID: options.NewID(model.ConfigMapIDPrefix), ConfigMap: &templateplan.ConfigMapPlan{Files: configMap.Files}})
	}
	// deploy は、参照される側が先になるよう、並べる。
	expandedByName := map[string]ExpandedDeploy{}
	for _, deploy := range expanded.Deploys {
		expandedByName[deploy.Name] = deploy
	}
	for _, name := range deployOrder(template.Resources, deployResources) {
		deploy := expandedByName[name]
		deployPlan := &templateplan.DeployPlan{Image: deploy.Image, Spec: deploy.Spec, Replicas: deploy.Replicas, Env: deploy.Env, FSGroup: deploy.FSGroup, Command: deploy.Command, Args: deploy.Args}
		volumeMounts := make([]model.MountSpec, 0, len(deploy.Mounts))
		for _, mount := range deploy.Mounts {
			deployPlan.Mounts = append(deployPlan.Mounts, templateplan.MountPlan{Volume: mount.Volume, Path: mount.Path})
			volumeMounts = append(volumeMounts, model.MountSpec{VolumeID: model.VolumeIDPrefix + mount.Volume, MountPath: mount.Path})
		}
		files := make([]model.FileSpec, 0, len(deploy.Files))
		for _, file := range deploy.Files {
			files = append(files, model.FileSpec{Path: file.Path, Content: file.Content})
		}
		// 展開したあとの、ファイルの大きさ、内容の形を、改めて確かめる。
		if filesError := model.ValidateFiles(files, volumeMounts); filesError != nil {
			return templateplan.Plan{}, fmt.Errorf("deploy %s: files: %v", name, filesError)
		}
		deployPlan.Files = files
		for _, mount := range deploy.ConfigMaps {
			deployPlan.ConfigMaps = append(deployPlan.ConfigMaps, templateplan.ConfigMapMountPlan{ConfigMap: mount.ConfigMap, Key: mount.Key, Path: mount.Path, Mode: mount.Mode})
		}
		plan.Steps = append(plan.Steps, templateplan.Step{Kind: templateplan.StepDeploy, Name: name, ID: options.NewID(model.DeployIDPrefix), Deploy: deployPlan})
		// service は、その deploy の、すぐ後ろに置く。
		if deploy.Service != nil {
			ports := make([]model.ServicePort, 0, len(deploy.Service.Ports))
			for _, port := range deploy.Service.Ports {
				ports = append(ports, model.ServicePort{Name: port.Name, Port: port.Port, Protocol: port.Protocol})
			}
			plan.Steps = append(plan.Steps, templateplan.Step{Kind: templateplan.StepService, Name: name, ID: options.NewID(model.ServiceIDPrefix), Service: &templateplan.ServicePlan{Deploy: name, Ports: ports}})
		}
	}
	// ingress(あれば)。
	if len(expanded.Rules) > 0 {
		ingressPlan := &templateplan.IngressPlan{}
		for _, rule := range expanded.Rules {
			ingressPlan.Rules = append(ingressPlan.Rules, templateplan.IngressRulePlan{Path: rule.Path, Deploy: rule.Deploy, Port: rule.Port})
		}
		plan.Steps = append(plan.Steps, templateplan.Step{Kind: templateplan.StepIngress, Name: "ingress", ID: options.NewID(model.IngressIDPrefix), Ingress: ingressPlan})
	}
	return plan, nil
}

// deployOrder は、deploy の name を、参照される側(${service.X.dns} の X)が先になるように並べる。同じ深さなら、テンプレートの順。
// 循環は、検証(validateNoCycle)で断ってあるので、ここでは起きない。
//
// 引数:
//   - resources: テンプレートの部品(元の順)。
//   - deploys: deploy の name から定義への対応。
//
// 戻り値:
//   - 作る順の、deploy の name。
func deployOrder(resources []Resource, deploys map[string]Resource) []string {
	// 深さ優先で、参照先を先に出す。
	var ordered []string
	visited := map[string]bool{}
	var visit func(name string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		for _, text := range deployTexts(deploys[name]) {
			for _, reference := range referencesIn(text) {
				if dependency, isService := serviceReference(reference); isService {
					visit(dependency)
				}
			}
		}
		ordered = append(ordered, name)
	}
	for _, resource := range resources {
		if resource.Kind == ResourceDeploy {
			visit(resource.Name)
		}
	}
	return ordered
}

// serviceReference は、参照(${...} の中身)が、${service.X.dns} なら、X を返す。
//
// 引数:
//   - reference: ${ と } の間の文字列。
//
// 戻り値:
//   - deploy の name と、service の参照だったか。
func serviceReference(reference string) (string, bool) {
	// 「service.」で始まり「.dns」で終わるものだけ。
	const prefix, suffix = "service.", ".dns"
	if len(reference) > len(prefix)+len(suffix) && reference[:len(prefix)] == prefix && reference[len(reference)-len(suffix):] == suffix {
		return reference[len(prefix) : len(reference)-len(suffix)], true
	}
	return "", false
}

// Needs は、計画が作る量(クォータのまとめての確認に使う)を返す。
//
// 引数:
//   - plan: 計画。
//
// 戻り値:
//   - 作る deploy(スペックごと)、volume の数と大きさの合計。
func Needs(plan templateplan.Plan) model.BatchNeeds {
	// ステップを数える。
	needs := model.BatchNeeds{DeploysBySpec: map[string]int{}}
	for _, step := range plan.Steps {
		switch step.Kind {
		case templateplan.StepDeploy:
			needs.DeploysBySpec[step.Deploy.Spec]++
		case templateplan.StepVolume:
			needs.Volumes++
			needs.VolumeTotalMB += step.Volume.SizeMB
		}
	}
	return needs
}
