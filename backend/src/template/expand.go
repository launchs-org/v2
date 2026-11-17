package template

import (
	"fmt"
	"strconv"
	"strings"
)

// ExpandContext は、展開のときに、変数の他に、参照できる値。
type ExpandContext struct {
	// ProjectName は、${project.name} になる、プロジェクトの名前。
	ProjectName string

	// ServiceDNS は、${service.<deploy の name>.dns} になる、service の DNS 名(deploy の name → DNS 名)。
	// service が作られてから(段階 C が)決める値。
	ServiceDNS map[string]string
}

// ExpandedDeploy は、展開したあとの deploy 1つ(全ての ${...} が、置き換わっている)。
type ExpandedDeploy struct {
	// Name は、テンプレートの中の name。
	Name string

	// Image、Spec、Replicas は、deploy の基本の設定。
	Image    string
	Spec     string
	Replicas int

	// Env、Command、Args は、起動の設定。Command は、空なら、イメージの既定。
	Env     map[string]string
	Command string
	Args    []string

	// Mounts は、volume のマウント(volume は、テンプレートの中の name)。
	Mounts []Mount

	// Files は、設定ファイル(内容は、展開済み)。
	Files []File

	// Service は、ポートの公開(無ければ nil)。
	Service *Service
}

// ExpandedVolume は、展開したあとの volume 1つ。
type ExpandedVolume struct {
	// Name は、テンプレートの中の name。
	Name string

	// SizeMB は、大きさ(MB)。
	SizeMB int
}

// ExpandedResources は、展開したあとの、全ての部品(元の順)。
type ExpandedResources struct {
	// Volumes、Deploys は、元の順。
	Volumes []ExpandedVolume
	Deploys []ExpandedDeploy

	// Rules は、ingress のルール(無ければ、nil。ingress を作らない)。
	Rules []Rule
}

// Expand は、テンプレートの ${...} を、値で置き換えた、部品の一覧を作る。
//
// 展開は、YAML を構造として読み込んだあとの、**文字列の値**に対して行う(YAML のテキストに、直接、置換しない)。
// そのため、変数の値に、改行、「:」、「"」、「${x}」が含まれていても、構造は壊れない。
// 展開は **1 回だけ**(置き換えた値の中の ${...} は、再び展開しない)。
//
// 引数:
//   - template: テンプレート(Validate を通ったもの)。
//   - values: 全ての変数の値(ResolveValues の結果)。
//   - context: プロジェクトの名前と、service の DNS 名。
//
// 戻り値:
//   - 展開した部品。
//   - 値が無い変数、DNS 名が無い参照、volume の大きさが整数でないときのエラー。
func Expand(template Template, values map[string]string, context ExpandContext) (ExpandedResources, error) {
	// 置き換えの道具(1 回の走査で、置き換える)。
	replace := func(text string) (string, error) { return expandText(text, values, context) }
	var expanded ExpandedResources
	for _, resource := range template.Resources {
		switch resource.Kind {
		case ResourceVolume:
			sizeText, replaceError := replace(resource.SizeMB)
			if replaceError != nil {
				return ExpandedResources{}, fmt.Errorf("volume %s: %w", resource.Name, replaceError)
			}
			size, parseError := strconv.Atoi(sizeText)
			if parseError != nil || size < 1 {
				return ExpandedResources{}, fmt.Errorf("volume %s: sizeMB は、1 以上の整数でなければなりません(%q)", resource.Name, sizeText)
			}
			expanded.Volumes = append(expanded.Volumes, ExpandedVolume{Name: resource.Name, SizeMB: size})
		case ResourceDeploy:
			deploy, deployError := expandDeploy(resource, replace)
			if deployError != nil {
				return ExpandedResources{}, fmt.Errorf("deploy %s: %w", resource.Name, deployError)
			}
			expanded.Deploys = append(expanded.Deploys, deploy)
		case ResourceIngress:
			expanded.Rules = append([]Rule(nil), resource.Rules...)
		}
	}
	return expanded, nil
}

// expandDeploy は、deploy 1つの、文字列の値を展開する。
//
// 引数:
//   - resource: deploy の定義。
//   - replace: 文字列の ${...} を置き換える関数。
//
// 戻り値:
//   - 展開した deploy。
//   - 置き換えに失敗したときのエラー。
func expandDeploy(resource Resource, replace func(string) (string, error)) (ExpandedDeploy, error) {
	// 台数の既定は 1。
	replicas := 1
	if resource.Replicas != nil {
		replicas = *resource.Replicas
	}
	deploy := ExpandedDeploy{Name: resource.Name, Spec: resource.Spec, Replicas: replicas, Mounts: append([]Mount(nil), resource.Mounts...), Service: resource.Service}
	var replaceError error
	if deploy.Image, replaceError = replace(resource.Image); replaceError != nil {
		return ExpandedDeploy{}, replaceError
	}
	if deploy.Command, replaceError = replace(resource.Command); replaceError != nil {
		return ExpandedDeploy{}, replaceError
	}
	for _, argument := range resource.Args {
		replaced, argumentError := replace(argument)
		if argumentError != nil {
			return ExpandedDeploy{}, argumentError
		}
		deploy.Args = append(deploy.Args, replaced)
	}
	if len(resource.Env) > 0 {
		deploy.Env = map[string]string{}
	}
	for name, value := range resource.Env {
		replaced, envError := replace(value)
		if envError != nil {
			return ExpandedDeploy{}, fmt.Errorf("env %s: %w", name, envError)
		}
		deploy.Env[name] = replaced
	}
	for _, file := range resource.Files {
		content, contentError := replace(file.Content)
		if contentError != nil {
			return ExpandedDeploy{}, fmt.Errorf("files %s: %w", file.Path, contentError)
		}
		deploy.Files = append(deploy.Files, File{Path: file.Path, Content: content})
	}
	return deploy, nil
}

// expandText は、文字列の ${...} を、1 回の走査で置き換える。置き換えた値は、再び走査しない。
//
// 引数:
//   - text: 元の文字列。
//   - values: 変数の値。
//   - context: プロジェクトの名前と、service の DNS 名。
//
// 戻り値:
//   - 置き換えた文字列。
//   - 値が無い参照があるときのエラー。
func expandText(text string, values map[string]string, context ExpandContext) (string, error) {
	// 先頭から、${ を探し、対応する } までを、値に置き換える。残りの文字列だけを、続けて走査する。
	var builder strings.Builder
	rest := text
	for {
		start := strings.Index(rest, "${")
		if start < 0 {
			builder.WriteString(rest)
			return builder.String(), nil
		}
		end := strings.Index(rest[start:], "}")
		if end < 0 {
			return "", fmt.Errorf("${ が、閉じていません")
		}
		reference := rest[start+2 : start+end]
		value, found := lookup(reference, values, context)
		if !found {
			return "", fmt.Errorf("${%s} の値がありません", reference)
		}
		builder.WriteString(rest[:start])
		builder.WriteString(value)
		rest = rest[start+end+1:]
	}
}

// lookup は、参照(「title」「project.name」「service.db.dns」)の値を引く。
//
// 引数:
//   - reference: ${ と } の間の文字列。
//   - values: 変数の値。
//   - context: プロジェクトの名前と、service の DNS 名。
//
// 戻り値:
//   - 値と、見つかったか。
func lookup(reference string, values map[string]string, context ExpandContext) (string, bool) {
	// 種類ごとに、引く先を変える。
	switch {
	case reference == "project.name":
		return context.ProjectName, context.ProjectName != ""
	case strings.HasPrefix(reference, "service.") && strings.HasSuffix(reference, ".dns"):
		dns, found := context.ServiceDNS[strings.TrimSuffix(strings.TrimPrefix(reference, "service."), ".dns")]
		return dns, found && dns != ""
	default:
		value, found := values[reference]
		return value, found
	}
}
