package template

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"launchs/shared/model"
)

var (
	// templateIDPattern は、metadata.id の形(小文字の英数字とハイフン。先頭は英数字)。
	templateIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

	// variableNamePattern は、変数名の形(英字で始まる、英数字と _)。
	variableNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

	// resourceNamePattern は、部品の name の形(小文字の英字で始まる、小文字の英数字とハイフン)。
	resourceNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

	// portNamePattern は、service のポート名の形(小文字の英数字とハイフン。先頭と末尾のハイフン、連続するハイフンは不可)。
	// docs/endpoints/services.md と同じ規則(15文字までと、英字を1つ以上含むことは、別に確かめる)。
	portNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

	// letterPattern は、英字を含むかの確認用。
	letterPattern = regexp.MustCompile(`[a-z]`)
)

// 使える値の一覧。
var (
	validSpecs     = map[string]bool{"small": true, "medium": true, "large": true}
	validProtocols = map[string]bool{"tcp": true, "udp": true}
	validCategory  = map[string]bool{"database": true, "web": true, "cache": true, "tool": true}
)

// maxPasswordLength は、自動で作る password の長さの上限。
const maxPasswordLength = 128

// Validate は、テンプレート1つの形と参照を確かめる。エラーは、日本語で、どの部分かが分かるように書く。
//
// 確かめること:
//   - 必須の項目、metadata の形、変数の定義(名前、型ごとの整合、既定値)、部品の定義(名前、参照、値の範囲)。
//   - ${...} の参照(変数、service.<deploy>.dns、project.name)が、全て存在すること。知らない ${...} は、エラー。
//   - deploy 同士の参照(${service.X.dns})に、循環が無いこと。
//
// 引数:
//   - template: 確かめるテンプレート。
//
// 戻り値:
//   - 最初に見つけた問題(複数あっても、1つだけ返す)。問題が無ければ nil。
func Validate(template Template) error {
	// ===== 1. 全体と metadata =====
	if template.APIVersion != APIVersion {
		return fmt.Errorf("apiVersion は %q でなければなりません(%q)", APIVersion, template.APIVersion)
	}
	if template.Kind != Kind {
		return fmt.Errorf("kind は %q でなければなりません(%q)", Kind, template.Kind)
	}
	if !templateIDPattern.MatchString(template.Metadata.ID) {
		return fmt.Errorf("metadata.id は、小文字の英数字とハイフンの、63文字までです(%q)", template.Metadata.ID)
	}
	if strings.TrimSpace(template.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name が空です")
	}
	if template.Metadata.Category != "" && !validCategory[template.Metadata.Category] {
		return fmt.Errorf("metadata.category は、database、web、cache、tool のどれかです(%q)", template.Metadata.Category)
	}
	if len(template.Resources) == 0 {
		return fmt.Errorf("resources が空です(作る部品が、1つもありません)")
	}

	// ===== 2. 変数 =====
	variables, variableError := validateVariables(template.Variables)
	if variableError != nil {
		return variableError
	}

	// ===== 3. 部品 =====
	return validateResources(template.Resources, variables)
}

// validateVariables は、変数の定義を確かめ、名前から引ける表を返す。
//
// 引数:
//   - variables: 変数の定義。
//
// 戻り値:
//   - 名前から変数を引く表。
//   - 最初に見つけた問題。
func validateVariables(variables []Variable) (map[string]Variable, error) {
	// 名前の重複を見つけるため、表に入れながら確かめる。
	byName := map[string]Variable{}
	for _, variable := range variables {
		where := fmt.Sprintf("variables[%s]", variable.Name)
		if !variableNamePattern.MatchString(variable.Name) {
			return nil, fmt.Errorf("variables の名前は、英字で始まる、英数字と _ です(%q)", variable.Name)
		}
		if _, exists := byName[variable.Name]; exists {
			return nil, fmt.Errorf("%s: 名前が重複しています", where)
		}
		if strings.TrimSpace(variable.Label) == "" {
			return nil, fmt.Errorf("%s: label が空です", where)
		}
		if validateError := validateVariable(where, variable); validateError != nil {
			return nil, validateError
		}
		byName[variable.Name] = variable
	}
	return byName, nil
}

// validateVariable は、変数1つの、型ごとの設定の整合と、既定値を確かめる。
//
// 引数:
//   - where: エラーに添える、場所の表記。
//   - variable: 確かめる変数。
//
// 戻り値:
//   - 最初に見つけた問題。
func validateVariable(where string, variable Variable) error {
	// 型に関係の無い設定は、書けない(typo や、勘違いを、見逃さない)。
	if variable.Generate && variable.Type != VariablePassword {
		return fmt.Errorf("%s: generate は、password だけに使えます", where)
	}
	if variable.Length != nil && variable.Type != VariablePassword {
		return fmt.Errorf("%s: length は、password だけに使えます", where)
	}
	if (variable.Min != nil || variable.Max != nil) && variable.Type != VariableInteger {
		return fmt.Errorf("%s: min と max は、integer だけに使えます", where)
	}
	if variable.Pattern != "" && variable.Type != VariableString {
		return fmt.Errorf("%s: pattern は、string だけに使えます", where)
	}
	if (variable.MinLength != nil || variable.MaxLength != nil) && variable.Type != VariableString && variable.Type != VariablePassword {
		return fmt.Errorf("%s: minLength と maxLength は、string と password だけに使えます", where)
	}
	if len(variable.Options) > 0 && variable.Type != VariableChoice {
		return fmt.Errorf("%s: options は、choice だけに使えます", where)
	}
	// 型ごとに、必要なものを確かめる。
	switch variable.Type {
	case VariableString, VariablePassword, VariableBoolean:
	case VariableInteger:
		if variable.Min != nil && variable.Max != nil && *variable.Min > *variable.Max {
			return fmt.Errorf("%s: min が max より大きいです", where)
		}
	case VariableChoice:
		if len(variable.Options) == 0 {
			return fmt.Errorf("%s: choice には、options が要ります", where)
		}
		seen := map[string]bool{}
		for _, option := range variable.Options {
			if seen[option.Value] {
				return fmt.Errorf("%s: options の value が重複しています(%q)", where, option.Value)
			}
			seen[option.Value] = true
		}
	default:
		return fmt.Errorf("%s: type は、string、integer、password、choice、boolean のどれかです(%q)", where, variable.Type)
	}
	if variable.Pattern != "" {
		if _, compileError := regexp.Compile(variable.Pattern); compileError != nil {
			return fmt.Errorf("%s: pattern が、正規表現として読めません: %v", where, compileError)
		}
	}
	if variable.MinLength != nil && variable.MaxLength != nil && *variable.MinLength > *variable.MaxLength {
		return fmt.Errorf("%s: minLength が maxLength より大きいです", where)
	}
	if variable.Length != nil && (*variable.Length < 8 || *variable.Length > maxPasswordLength) {
		return fmt.Errorf("%s: length は、8〜%d です", where, maxPasswordLength)
	}
	// 既定値は、同じ制約に合っていなければならない(既定のままで、検査に落ちないこと)。
	if variable.Default != nil {
		if valueError := validateValue(variable, *variable.Default); valueError != nil {
			return fmt.Errorf("%s: default が、制約に合いません: %v", where, valueError)
		}
	}
	return nil
}

// validateResources は、部品の定義と、参照を確かめる。
//
// 引数:
//   - resources: 部品の一覧。
//   - variables: 変数の表。
//
// 戻り値:
//   - 最初に見つけた問題。
func validateResources(resources []Resource, variables map[string]Variable) error {
	// 名前の重複と、種類ごとの表を作る(参照の確認に使う)。
	volumes := map[string]bool{}
	deploys := map[string]*Resource{}
	configMaps := map[string]*Resource{}
	ingressCount := 0
	for index := range resources {
		resource := &resources[index]
		switch resource.Kind {
		case ResourceVolume, ResourceDeploy, ResourceConfigMap:
			if !resourceNamePattern.MatchString(resource.Name) {
				return fmt.Errorf("resources[%d]: name は、小文字の英字で始まる、小文字の英数字とハイフンの、32文字までです(%q)", index, resource.Name)
			}
			if volumes[resource.Name] || deploys[resource.Name] != nil || configMaps[resource.Name] != nil {
				return fmt.Errorf("resources[%d]: name %q が重複しています(volume、deploy、configmap の間でも、重複できません)", index, resource.Name)
			}
			switch resource.Kind {
			case ResourceVolume:
				volumes[resource.Name] = true
			case ResourceConfigMap:
				configMaps[resource.Name] = resource
			default:
				deploys[resource.Name] = resource
			}
		case ResourceIngress:
			ingressCount++
			if ingressCount > 1 {
				return fmt.Errorf("resources[%d]: ingress は、1つだけです", index)
			}
			if resource.Name != "" {
				return fmt.Errorf("resources[%d]: ingress には、name を付けられません", index)
			}
		default:
			return fmt.Errorf("resources[%d]: kind は、volume、deploy、configmap、ingress のどれかです(%q)", index, resource.Kind)
		}
	}
	// 部品ごとに、中身を確かめる。
	for index := range resources {
		resource := resources[index]
		where := fmt.Sprintf("resources[%d](%s %s)", index, resource.Kind, resource.Name)
		var resourceError error
		switch resource.Kind {
		case ResourceVolume:
			resourceError = validateVolume(where, resource, variables)
		case ResourceDeploy:
			resourceError = validateDeploy(where, resource, volumes, configMaps, variables, deploys)
		case ResourceConfigMap:
			resourceError = validateConfigMap(where, resource, variables)
		case ResourceIngress:
			resourceError = validateIngress(where, resource, deploys)
		}
		if resourceError != nil {
			return resourceError
		}
	}
	// deploy 同士の参照(${service.X.dns})に、循環が無いことを確かめる。
	return validateNoCycle(deploys)
}

// checkUnusedFields は、その種類の部品が使わない項目が、書かれていないことを確かめる(typo を、見逃さない)。
//
// 引数:
//   - where: エラーに添える場所。
//   - resource: 部品。
//
// 戻り値:
//   - 使わない項目が書かれていれば、そのエラー。
func checkUnusedFields(where string, resource Resource) error {
	// 種類ごとに、使わない項目を調べる。
	isDeploy := resource.Kind == ResourceDeploy
	if resource.Kind != ResourceVolume && resource.SizeMB != "" {
		return fmt.Errorf("%s: sizeMB は、volume だけに使えます", where)
	}
	if !isDeploy && (resource.Image != "" || resource.Spec != "" || resource.Replicas != nil || len(resource.Env) > 0 || resource.Command != "" || len(resource.Args) > 0 || len(resource.Mounts) > 0 || len(resource.ConfigMaps) > 0 || resource.Service != nil) {
		return fmt.Errorf("%s: image、spec、replicas、env、command、args、mounts、configmaps、service は、deploy だけに使えます", where)
	}
	if !isDeploy && resource.Kind != ResourceConfigMap && len(resource.Files) > 0 {
		return fmt.Errorf("%s: files は、deploy と configmap だけに使えます", where)
	}
	if resource.Kind != ResourceIngress && len(resource.Rules) > 0 {
		return fmt.Errorf("%s: rules は、ingress だけに使えます", where)
	}
	return nil
}

// validateConfigMap は、configmap(共有の設定ファイルの組)の定義を確かめる。
//
// 確かめること: ファイルが 1 つ以上あること、key があり path が無いこと、encoding(text か base64)、
// base64 が正しい形で変数を含まないこと、text の ${...} が、変数と project.name だけを指すこと(service の DNS 名は、作る順序が循環するので不可)、
// ファイルの個数・key の規則・大きさ(model.ValidateConfigMapFiles。展開の前の大きさ)。
//
// 引数:
//   - where: エラーに添える場所。
//   - resource: configmap。
//   - variables: 変数の表。
//
// 戻り値:
//   - 最初に見つけた問題。
func validateConfigMap(where string, resource Resource, variables map[string]Variable) error {
	if unusedError := checkUnusedFields(where, resource); unusedError != nil {
		return unusedError
	}
	if len(resource.Files) == 0 {
		return fmt.Errorf("%s: files が空です(ファイルの無い configmap は、作れません)", where)
	}
	files := make([]model.ConfigMapFile, 0, len(resource.Files))
	for _, file := range resource.Files {
		if file.From != "" {
			return fmt.Errorf("%s: files の from(%s)が、読み込まれていません(フォルダから Load してください)", where, file.From)
		}
		if file.Path != "" {
			return fmt.Errorf("%s: configmap の files には、path ではなく、key を書きます(%q)", where, file.Path)
		}
		encoding := file.Encoding
		if encoding == "" {
			encoding = model.ConfigMapEncodingText
		}
		switch encoding {
		case model.ConfigMapEncodingBase64:
			// バイナリは、変数を含められない(展開で、壊れるため)。
			if strings.Contains(file.Content, "${") {
				return fmt.Errorf("%s: base64 の内容(%s)に、${...} は使えません", where, file.Key)
			}
		case model.ConfigMapEncodingText:
			// 参照できるのは、変数と project.name だけ。
			for _, reference := range referencesIn(file.Content) {
				if strings.HasPrefix(reference, "service.") {
					return fmt.Errorf("%s: configmap の内容(%s)で、${%s} は参照できません(configmap は、deploy より先に作るため)", where, file.Key, reference)
				}
			}
			if referenceError := validateReferences(where, file.Content, variables, nil, ""); referenceError != nil {
				return referenceError
			}
		default:
			return fmt.Errorf("%s: files の encoding は、text か base64 です(%s: %q)", where, file.Key, file.Encoding)
		}
		files = append(files, model.ConfigMapFile{Key: file.Key, Encoding: encoding, Content: file.Content})
	}
	if _, filesError := model.ValidateConfigMapFiles(files); filesError != nil {
		return fmt.Errorf("%s: files: %v", where, filesError)
	}
	return nil
}

// validateVolume は、volume の定義を確かめる。sizeMB は、整数か、integer の変数(min と max が必須)。
//
// 引数:
//   - where: エラーに添える場所。
//   - resource: volume。
//   - variables: 変数の表。
//
// 戻り値:
//   - 最初に見つけた問題。
func validateVolume(where string, resource Resource, variables map[string]Variable) error {
	if unusedError := checkUnusedFields(where, resource); unusedError != nil {
		return unusedError
	}
	if strings.TrimSpace(resource.SizeMB) == "" {
		return fmt.Errorf("%s: sizeMB が必要です", where)
	}
	// 整数で書かれていれば、1 以上であること。
	if number, parseError := strconv.Atoi(resource.SizeMB); parseError == nil {
		if number < 1 {
			return fmt.Errorf("%s: sizeMB は、1 以上です", where)
		}
		return nil
	}
	// それ以外は、${変数} の1つだけ(文字列の一部としての使用は、不可)。変数は integer で、min と max が必須(展開前に、範囲を保証するため)。
	references := referencesIn(resource.SizeMB)
	if len(references) != 1 || resource.SizeMB != "${"+references[0]+"}" {
		return fmt.Errorf("%s: sizeMB は、整数か、${変数} の1つだけです(%q)", where, resource.SizeMB)
	}
	variable, exists := variables[references[0]]
	if !exists {
		return fmt.Errorf("%s: 変数 %q が、variables にありません", where, references[0])
	}
	if variable.Type != VariableInteger {
		return fmt.Errorf("%s: sizeMB に使う変数 %q は、integer でなければなりません", where, variable.Name)
	}
	if variable.Min == nil || variable.Max == nil {
		return fmt.Errorf("%s: sizeMB に使う変数 %q には、min と max が必要です(展開の前に、範囲を保証するため)", where, variable.Name)
	}
	if *variable.Min < 1 {
		return fmt.Errorf("%s: sizeMB に使う変数 %q の min は、1 以上です", where, variable.Name)
	}
	return nil
}

// validateDeploy は、deploy の定義を確かめる。
//
// 引数:
//   - where: エラーに添える場所。
//   - resource: deploy。
//   - volumes: テンプレートの中の volume の name の表。
//   - configMaps: テンプレートの中の configmap の表。
//   - variables: 変数の表。
//   - deploys: テンプレートの中の deploy の表。
//
// 戻り値:
//   - 最初に見つけた問題。
func validateDeploy(where string, resource Resource, volumes map[string]bool, configMaps map[string]*Resource, variables map[string]Variable, deploys map[string]*Resource) error {
	if unusedError := checkUnusedFields(where, resource); unusedError != nil {
		return unusedError
	}
	if strings.TrimSpace(resource.Image) == "" || strings.ContainsAny(resource.Image, " \t\n") {
		return fmt.Errorf("%s: image は、空白を含まない、空でない文字列です", where)
	}
	if !validSpecs[resource.Spec] {
		return fmt.Errorf("%s: spec は、small、medium、large のどれかです(%q)", where, resource.Spec)
	}
	if resource.Replicas != nil && (*resource.Replicas < 0 || *resource.Replicas > model.MaxReplicas) {
		return fmt.Errorf("%s: replicas は、0〜%d です", where, model.MaxReplicas)
	}
	// 文字列の値(env、command、args、files の内容、image)の ${...} が、全て、存在することを確かめる。
	for _, text := range deployTexts(resource) {
		if referenceError := validateReferences(where, text, variables, deploys, resource.Name); referenceError != nil {
			return referenceError
		}
	}
	// マウント: volume が、テンプレートの中に存在し、path が正しく、重複しない。
	mountSpecs := make([]model.MountSpec, 0, len(resource.Mounts))
	seenVolumes := map[string]bool{}
	for _, mount := range resource.Mounts {
		if !volumes[mount.Volume] {
			return fmt.Errorf("%s: mounts の volume %q が、resources にありません", where, mount.Volume)
		}
		if seenVolumes[mount.Volume] {
			return fmt.Errorf("%s: 同じ volume %q を、2回マウントできません", where, mount.Volume)
		}
		seenVolumes[mount.Volume] = true
		mountSpecs = append(mountSpecs, model.MountSpec{VolumeID: "volume-" + mount.Volume, MountPath: mount.Path})
	}
	if mountError := model.ValidateMounts(mountSpecs); mountError != nil {
		return fmt.Errorf("%s: mounts: %v", where, mountError)
	}
	// ファイル: path と内容の規則(マウント先と、まったく同じ path は不可)。
	fileSpecs := make([]model.FileSpec, 0, len(resource.Files))
	for _, file := range resource.Files {
		if file.From != "" {
			return fmt.Errorf("%s: files の from(%s)が、読み込まれていません(フォルダから Load してください)", where, file.From)
		}
		fileSpecs = append(fileSpecs, model.FileSpec{Path: file.Path, Content: file.Content})
	}
	if fileError := model.ValidateFiles(fileSpecs, mountSpecs); fileError != nil {
		return fmt.Errorf("%s: files: %v", where, fileError)
	}
	// configmaps: configmap と key が、テンプレートの中に存在し、path と mode が正しく、他のファイルやマウント先と、まったく同じ path にならない。
	configMapSpecs := make([]model.ConfigMapMountSpec, 0, len(resource.ConfigMaps))
	for _, mount := range resource.ConfigMaps {
		target := configMaps[mount.ConfigMap]
		if target == nil {
			return fmt.Errorf("%s: configmaps の configmap %q が、resources にありません", where, mount.ConfigMap)
		}
		hasKey := false
		for _, file := range target.Files {
			if file.Key == mount.Key {
				hasKey = true
			}
		}
		if !hasKey {
			return fmt.Errorf("%s: configmaps の key %q が、configmap %q の files にありません", where, mount.Key, mount.ConfigMap)
		}
		configMapSpecs = append(configMapSpecs, model.ConfigMapMountSpec{ConfigMapID: model.ConfigMapIDPrefix + mount.ConfigMap, Key: mount.Key, Path: mount.Path, Mode: mount.Mode})
	}
	if _, configMapError := model.ValidateConfigMapMounts(configMapSpecs); configMapError != nil {
		return fmt.Errorf("%s: configmaps: %v", where, configMapError)
	}
	if overlapError := model.ValidateConfigMapMountPaths(configMapSpecs, fileSpecs, mountSpecs); overlapError != nil {
		return fmt.Errorf("%s: configmaps: %v", where, overlapError)
	}
	// service: ポートの規則。
	if resource.Service != nil {
		return validateServicePorts(where, resource.Service.Ports)
	}
	return nil
}

// validateServicePorts は、service のポートの規則(1〜20個、名前、番号、プロトコル、重複)を確かめる(docs/endpoints/services.md と同じ)。
//
// 引数:
//   - where: エラーに添える場所。
//   - ports: ポートの一覧。
//
// 戻り値:
//   - 最初に見つけた問題。
func validateServicePorts(where string, ports []Port) error {
	if len(ports) < 1 || len(ports) > 20 {
		return fmt.Errorf("%s: service の ports は、1〜20個です", where)
	}
	names := map[string]bool{}
	pairs := map[string]bool{}
	for _, port := range ports {
		if len(port.Name) < 1 || len(port.Name) > 15 || !portNamePattern.MatchString(port.Name) || !letterPattern.MatchString(port.Name) {
			return fmt.Errorf("%s: ポート名 %q は、小文字の英数字とハイフンで、15文字まで、英字を1つ以上含めてください(先頭と末尾のハイフン、連続するハイフンは不可)", where, port.Name)
		}
		if port.Port < 1 || port.Port > 65535 {
			return fmt.Errorf("%s: ポート番号は、1〜65535 です(%s: %d)", where, port.Name, port.Port)
		}
		if !validProtocols[port.Protocol] {
			return fmt.Errorf("%s: protocol は、tcp か udp です(%s: %q)", where, port.Name, port.Protocol)
		}
		pair := port.Protocol + "/" + strconv.Itoa(port.Port)
		if names[port.Name] || pairs[pair] {
			return fmt.Errorf("%s: ポート名、または、同じ番号とプロトコルの組が、重複しています(%s)", where, port.Name)
		}
		names[port.Name] = true
		pairs[pair] = true
	}
	return nil
}

// validateIngress は、ingress の定義を確かめる。転送先の deploy は、service を持ち、そのポートは tcp(ingress は HTTP だけ)。
//
// 引数:
//   - where: エラーに添える場所。
//   - resource: ingress。
//   - deploys: テンプレートの中の deploy の表。
//
// 戻り値:
//   - 最初に見つけた問題。
func validateIngress(where string, resource Resource, deploys map[string]*Resource) error {
	if unusedError := checkUnusedFields(where, resource); unusedError != nil {
		return unusedError
	}
	if len(resource.Rules) == 0 {
		return fmt.Errorf("%s: rules が空です(ルールの無い ingress は、外から届きません)", where)
	}
	seenPaths := map[string]bool{}
	for _, rule := range resource.Rules {
		if !strings.HasPrefix(rule.Path, "/") || strings.ContainsAny(rule.Path, " \t\n") || len(rule.Path) > 200 {
			return fmt.Errorf("%s: rules の path は、「/」で始まる、空白を含まない、200文字までです(%q)", where, rule.Path)
		}
		if seenPaths[rule.Path] {
			return fmt.Errorf("%s: rules の path が重複しています(%q)", where, rule.Path)
		}
		seenPaths[rule.Path] = true
		target := deploys[rule.Deploy]
		if target == nil {
			return fmt.Errorf("%s: rules の deploy %q が、resources にありません", where, rule.Deploy)
		}
		if target.Service == nil {
			return fmt.Errorf("%s: rules の deploy %q は、service を持ちません(ingress の転送先には、service が要ります)", where, rule.Deploy)
		}
		found := false
		for _, port := range target.Service.Ports {
			if port.Port == rule.Port && port.Protocol == "tcp" {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("%s: rules の port %d は、deploy %q の service の、tcp のポートにありません(ingress は HTTP だけです)", where, rule.Port, rule.Deploy)
		}
	}
	return nil
}

// deployTexts は、deploy の、${...} を含められる文字列(image、env の値、command、args、files の内容)を、全て返す。
//
// 引数:
//   - resource: deploy。
//
// 戻り値:
//   - 文字列の一覧。
func deployTexts(resource Resource) []string {
	// 順に集める(env は、キーの順が不定なので、値だけを集める)。
	texts := []string{resource.Image, resource.Command}
	texts = append(texts, resource.Args...)
	for _, value := range resource.Env {
		texts = append(texts, value)
	}
	for _, file := range resource.Files {
		texts = append(texts, file.Content)
	}
	return texts
}

// validateReferences は、文字列の ${...} が、全て、定義済みのものを指していることを確かめる。
//
// 引数:
//   - where: エラーに添える場所。
//   - text: 調べる文字列。
//   - variables: 変数の表。
//   - deploys: テンプレートの中の deploy の表。
//   - self: この文字列を持つ deploy の name(自分の service の DNS は、自分では使えない。循環になる)。
//
// 戻り値:
//   - 知らない参照があれば、そのエラー。
func validateReferences(where string, text string, variables map[string]Variable, deploys map[string]*Resource, self string) error {
	for _, reference := range referencesIn(text) {
		switch {
		case reference == "project.name":
		case strings.HasPrefix(reference, "service.") && strings.HasSuffix(reference, ".dns"):
			name := strings.TrimSuffix(strings.TrimPrefix(reference, "service."), ".dns")
			target := deploys[name]
			if target == nil {
				return fmt.Errorf("%s: ${%s} の deploy %q が、resources にありません", where, reference, name)
			}
			if target.Service == nil {
				return fmt.Errorf("%s: ${%s} の deploy %q は、service を持ちません", where, reference, name)
			}
			if name == self {
				return fmt.Errorf("%s: ${%s}: 自分自身の service の DNS 名は、参照できません", where, reference)
			}
		default:
			if _, exists := variables[reference]; !exists {
				return fmt.Errorf("%s: ${%s} は、知らない参照です(variables にも、project.name、service.<deploy>.dns にも、ありません)", where, reference)
			}
		}
	}
	return nil
}

// validateNoCycle は、deploy 同士の参照(${service.X.dns})に、循環が無いことを、深さ優先で確かめる。
//
// 引数:
//   - deploys: テンプレートの中の deploy の表。
//
// 戻り値:
//   - 循環があれば、その経路を示すエラー。
func validateNoCycle(deploys map[string]*Resource) error {
	// deploy ごとの、参照先の deploy の一覧(間接の循環を見つけるため)。
	edges := map[string][]string{}
	for name, deploy := range deploys {
		for _, text := range deployTexts(*deploy) {
			for _, reference := range referencesIn(text) {
				if strings.HasPrefix(reference, "service.") && strings.HasSuffix(reference, ".dns") {
					edges[name] = append(edges[name], strings.TrimSuffix(strings.TrimPrefix(reference, "service."), ".dns"))
				}
			}
		}
	}
	// 深さ優先で、いま辿っている経路(visiting)に、再び入ったら、循環。
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := map[string]int{}
	var path []string
	var visit func(name string) error
	visit = func(name string) error {
		state[name] = visiting
		path = append(path, name)
		for _, next := range edges[name] {
			if state[next] == visiting {
				return fmt.Errorf("deploy の参照(${service.X.dns})が、循環しています: %s → %s", strings.Join(path, " → "), next)
			}
			if state[next] == unvisited {
				if cycleError := visit(next); cycleError != nil {
					return cycleError
				}
			}
		}
		path = path[:len(path)-1]
		state[name] = done
		return nil
	}
	// 名前の順に調べる(エラーの経路を、毎回、同じにするため)。
	names := make([]string, 0, len(deploys))
	for name := range deploys {
		names = append(names, name)
	}
	sortStrings(names)
	for _, name := range names {
		if state[name] == unvisited {
			if cycleError := visit(name); cycleError != nil {
				return cycleError
			}
		}
	}
	return nil
}
