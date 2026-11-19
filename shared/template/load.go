package template

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"launchs/shared/model"
)

// Registry は、読み込んだテンプレートの一覧(id から引ける。版ごとに持つ)。
type Registry struct {
	// entries は、id → そのテンプレートの全ての版。
	entries map[string]*registryEntry

	// ids は、id の昇順。
	ids []string
}

// registryEntry は、1 つのテンプレートの、全ての版。
type registryEntry struct {
	// versions は、版の一覧(新しい順)。
	versions []string
	// latest は、既定の版。
	latest string
	// byVersion は、版 → テンプレート。
	byVersion map[string]Template
}

// Parse は、YAML のテキスト1つを、テンプレートとして読み込む(検証は、しない)。
// 知らないフィールド(typo など)と、複数のドキュメントは、エラー。
//
// 引数:
//   - content: YAML のテキスト。
//
// 戻り値:
//   - テンプレート。
//   - YAML として読めない、知らないフィールドがある、複数のドキュメントがあるときのエラー。
func Parse(content []byte) (Template, error) {
	// 知らないフィールドを、エラーにして読み込む。
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	var template Template
	if decodeError := decoder.Decode(&template); decodeError != nil {
		return Template{}, fmt.Errorf("YAML を読み込めません: %w", decodeError)
	}
	// 2つ目のドキュメントがあれば、意図しない記述なので、拒否する。
	var second yaml.Node
	if secondError := decoder.Decode(&second); secondError != io.EOF {
		return Template{}, fmt.Errorf("YAML に、複数のドキュメントがあります(1つだけです)")
	}
	return template, nil
}

// 版のフォルダの中の、ファイル名(どれも、無くてよい。config.yaml 以外は、無ければ、その種類は無い)。
const (
	// ConfigFileName は、変数(variables)と、作成後の案内(notes)。
	ConfigFileName = "config.yaml"
	// VolumesFileName は、ボリューム(volumes)。
	VolumesFileName = "volumes.yaml"
	// ConfigMapsFileName は、共有の設定ファイル(configmaps)。
	ConfigMapsFileName = "configmaps.yaml"
	// DeployFileName は、コンテナ(deploys)。
	DeployFileName = "deploy.yaml"
	// ServiceFileName は、deploy に付ける Service(services)。
	ServiceFileName = "service.yaml"
	// IngressFileName は、公開(rules)。
	IngressFileName = "ingress.yaml"
	// ManifestFileName は、テンプレートのフォルダの直下の、名前・説明・アイコンなど(版によらない情報)。
	ManifestFileName = "manifest.yaml"
	// ReadmeFileName は、テンプレートのフォルダの直下の、説明の文書(任意)。
	ReadmeFileName = "README.md"
)

// Manifest は、テンプレートのフォルダの直下の manifest.yaml。版によらない、テンプレートの情報。
type Manifest struct {
	// ID は、テンプレートの id(フォルダ名と同じ)。
	ID string `yaml:"id"`
	// Name は、画面に出す名前。
	Name string `yaml:"name"`
	// Description は、1〜2 文の説明。
	Description string `yaml:"description"`
	// Icon は、アイコンの名前。
	Icon string `yaml:"icon"`
	// Category は、分類。
	Category string `yaml:"category"`
	// Latest は、既定で使う版(省略すると、いちばん新しい版)。
	Latest string `yaml:"latest"`
}

// versionPattern は、版のフォルダの名前(semver の、数字 3 つ)。
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// compareVersions は、2 つの版(x.y.z)を比べる。
//
// 引数:
//   - left: 1 つ目の版。
//   - right: 2 つ目の版。
//
// 戻り値:
//   - left が小さければ負、同じなら 0、大きければ正。
func compareVersions(left string, right string) int {
	// 数字の組に直して、先頭から比べる。
	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	for index := 0; index < 3; index++ {
		leftNumber, _ := strconv.Atoi(leftParts[index])
		rightNumber, _ := strconv.Atoi(rightParts[index])
		if leftNumber != rightNumber {
			return leftNumber - rightNumber
		}
	}
	return 0
}

// decodeStrict は、YAML を、知らない項目をエラーにして、読み込む(ファイルが無いときは、何もしない)。
//
// 引数:
//   - folder: ファイルのあるフォルダ。
//   - fileName: ファイル名。
//   - target: 読み込み先(構造体へのポインタ)。
//   - required: true なら、ファイルが無いのは、エラー。
//
// 戻り値:
//   - 読み込めたか(ファイルがあったか)。
//   - 読めない、書式が違う、複数のドキュメントがあるときのエラー。
func decodeStrict(folder fs.FS, fileName string, target any, required bool) (bool, error) {
	// ファイルを読む。
	content, readError := fs.ReadFile(folder, fileName)
	if errors.Is(readError, fs.ErrNotExist) && !required {
		return false, nil
	}
	if readError != nil {
		return false, fmt.Errorf("%s を読めません: %w", fileName, readError)
	}
	// 知らない項目を許さずに、1 つのドキュメントとして読む。
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if decodeError := decoder.Decode(target); decodeError != nil && decodeError != io.EOF {
		return false, fmt.Errorf("%s: %w", fileName, decodeError)
	}
	var second yaml.Node
	if secondError := decoder.Decode(&second); secondError != io.EOF {
		return false, fmt.Errorf("%s: YAML に、複数のドキュメントがあります(1つだけです)", fileName)
	}
	return true, nil
}

// Load は、フォルダの中の、テンプレートのフォルダ(<id>/manifest.yaml と、<id>/<版>/…)を、全て、読み込み、検証する。
//
// フォルダの形:
//
//	<id>/manifest.yaml        名前、説明、アイコン、分類、既定の版(latest)
//	<id>/README.md            説明の文書(任意)
//	<id>/<x.y.z>/config.yaml  変数(variables)と、作成後の案内(notes)
//	<id>/<x.y.z>/volumes.yaml, configmaps.yaml, deploy.yaml, service.yaml, ingress.yaml
//	<id>/<x.y.z>/files/…      from: で読み込むファイル
//
// 処理の流れ(テンプレートごと):
//  1. manifest.yaml を読み、id がフォルダ名と同じことを確かめる。
//  2. 版のフォルダ(x.y.z の名前)を、全て読み、種類ごとのファイルを 1 つのテンプレートに組み立てて、from: を読み込み、検証する。
//  3. 既定の版を決める(manifest の latest。無ければ、いちばん新しい版)。
//
// 引数:
//   - files: テンプレートのフォルダが並ぶ、ファイルシステム。
//
// 戻り値:
//   - 読み込んだ登録簿。1 つでも不正なら、nil とエラー(エラーに、テンプレートと版の名前が入る)。
func Load(files fs.FS) (*Registry, error) {
	// テンプレートのフォルダを探す。
	entries, readError := fs.ReadDir(files, ".")
	if readError != nil {
		return nil, fmt.Errorf("テンプレートの一覧を読めません: %w", readError)
	}
	registry := &Registry{entries: map[string]*registryEntry{}}
	for _, entry := range entries {
		// フォルダだけが、テンプレート。
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		loaded, loadError := loadTemplate(files, name)
		if loadError != nil {
			return nil, fmt.Errorf("テンプレート %s: %w", name, loadError)
		}
		registry.entries[name] = loaded
		registry.ids = append(registry.ids, name)
	}
	return registry, nil
}

// loadTemplate は、テンプレートのフォルダ 1 つを、全ての版を含めて、読み込む。
//
// 引数:
//   - files: テンプレートのフォルダが並ぶ、ファイルシステム。
//   - name: テンプレートのフォルダの名前。
//
// 戻り値:
//   - 登録簿の 1 件分。
//   - 読み込み・検証のエラー。
func loadTemplate(files fs.FS, name string) (*registryEntry, error) {
	// フォルダを開く。
	folder, subError := fs.Sub(files, name)
	if subError != nil {
		return nil, fmt.Errorf("開けません: %w", subError)
	}
	// 1. manifest.yaml を読む。
	var manifest Manifest
	if _, manifestError := decodeStrict(folder, ManifestFileName, &manifest, true); manifestError != nil {
		return nil, manifestError
	}
	if manifest.ID != name {
		return nil, fmt.Errorf("manifest.yaml の id(%q)が、フォルダ名(%q)と違います", manifest.ID, name)
	}
	// README(任意)を読む。
	readme, _ := fs.ReadFile(folder, ReadmeFileName)
	// 2. 版のフォルダを読む。
	children, childrenError := fs.ReadDir(folder, ".")
	if childrenError != nil {
		return nil, childrenError
	}
	versions := []string{}
	for _, child := range children {
		if child.IsDir() {
			versions = append(versions, child.Name())
		}
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("版のフォルダ(1.0.0 のような名前)が、ありません")
	}
	for _, version := range versions {
		if !versionPattern.MatchString(version) {
			return nil, fmt.Errorf("版のフォルダの名前 %q が、x.y.z の形ではありません", version)
		}
	}
	// 新しい版が先になるよう、並べる。
	sort.Slice(versions, func(first int, second int) bool { return compareVersions(versions[first], versions[second]) > 0 })
	loaded := &registryEntry{versions: versions, byVersion: map[string]Template{}}
	for _, version := range versions {
		versionFolder, versionError := fs.Sub(folder, version)
		if versionError != nil {
			return nil, versionError
		}
		built, buildError := loadVersion(versionFolder, manifest, string(readme), version, versions)
		if buildError != nil {
			return nil, fmt.Errorf("版 %s: %w", version, buildError)
		}
		loaded.byVersion[version] = built
	}
	// 3. 既定の版を決める。
	loaded.latest = versions[0]
	if manifest.Latest != "" {
		if _, exists := loaded.byVersion[manifest.Latest]; !exists {
			return nil, fmt.Errorf("manifest.yaml の latest(%q)の版が、ありません", manifest.Latest)
		}
		loaded.latest = manifest.Latest
	}
	return loaded, nil
}

// loadVersion は、版のフォルダ 1 つを、1 つのテンプレートに組み立てて、検証する。
//
// 引数:
//   - folder: 版のフォルダ。
//   - manifest: manifest.yaml の内容。
//   - readme: README.md の内容。
//   - version: この版の名前。
//   - versions: このテンプレートの全ての版(新しい順)。
//
// 戻り値:
//   - 検証済みのテンプレート。
//   - 読み込み・検証のエラー。
func loadVersion(folder fs.FS, manifest Manifest, readme string, version string, versions []string) (Template, error) {
	// config.yaml(必須)。
	var config struct {
		Notes     string     `yaml:"notes"`
		Variables []Variable `yaml:"variables"`
	}
	if _, configError := decodeStrict(folder, ConfigFileName, &config, true); configError != nil {
		return Template{}, configError
	}
	built := Template{
		APIVersion: "launchs/v1", Kind: "Template", Variables: config.Variables,
		Metadata: Metadata{ID: manifest.ID, Name: manifest.Name, Description: manifest.Description, Icon: manifest.Icon, Category: manifest.Category, Notes: config.Notes, Version: version, Versions: versions, Readme: readme},
	}
	// 種類ごとのファイルを読む。
	var volumes struct {
		Volumes []Resource `yaml:"volumes"`
	}
	var configMaps struct {
		ConfigMaps []Resource `yaml:"configmaps"`
	}
	var deploys struct {
		Deploys []Resource `yaml:"deploys"`
	}
	var services struct {
		Services []struct {
			Deploy string `yaml:"deploy"`
			Ports  []Port `yaml:"ports"`
		} `yaml:"services"`
	}
	var ingress struct {
		Rules []Rule `yaml:"rules"`
	}
	for fileName, target := range map[string]any{VolumesFileName: &volumes, ConfigMapsFileName: &configMaps, DeployFileName: &deploys, ServiceFileName: &services, IngressFileName: &ingress} {
		if _, decodeError := decodeStrict(folder, fileName, target, false); decodeError != nil {
			return Template{}, decodeError
		}
	}
	// 部品の種類(kind)は、ファイルで決まる。書いてあれば、間違い。
	kinds := []struct {
		kind      string
		fileName  string
		resources []Resource
	}{{ResourceVolume, VolumesFileName, volumes.Volumes}, {ResourceConfigMap, ConfigMapsFileName, configMaps.ConfigMaps}, {ResourceDeploy, DeployFileName, deploys.Deploys}}
	for _, group := range kinds {
		for _, resource := range group.resources {
			if resource.Kind != "" {
				return Template{}, fmt.Errorf("%s: %s の kind は、書きません(ファイルで決まります)", group.fileName, resource.Name)
			}
			if resource.Service != nil {
				return Template{}, fmt.Errorf("%s: %s の service は、%s に書きます", group.fileName, resource.Name, ServiceFileName)
			}
			resource.Kind = group.kind
			built.Resources = append(built.Resources, resource)
		}
	}
	// service を、deploy に付ける。
	for _, service := range services.Services {
		attached := false
		for index := range built.Resources {
			if built.Resources[index].Kind == ResourceDeploy && built.Resources[index].Name == service.Deploy {
				if built.Resources[index].Service != nil {
					return Template{}, fmt.Errorf("%s: deploy %s の service が、重複しています", ServiceFileName, service.Deploy)
				}
				built.Resources[index].Service = &Service{Ports: service.Ports}
				attached = true
			}
		}
		if !attached {
			return Template{}, fmt.Errorf("%s: deploy %q が、%s にありません", ServiceFileName, service.Deploy, DeployFileName)
		}
	}
	// 公開。
	if len(ingress.Rules) > 0 {
		built.Resources = append(built.Resources, Resource{Kind: ResourceIngress, Rules: ingress.Rules})
	}
	// from: を読み込んで、検証する。
	if resolveError := resolveFiles(&built, folder); resolveError != nil {
		return Template{}, resolveError
	}
	if validateError := Validate(built); validateError != nil {
		return Template{}, validateError
	}
	return built, nil
}

// resolveFiles は、部品の files の from: を、フォルダのファイルの内容に置き換える。
//
// 規則:
//   - from と content は、同時に書けない。
//   - from は、フォルダの中の、相対パス(「/」で始まらない、「..」を含まない)。
//   - configmap の files: encoding の省略は、自動(UTF-8 で NUL を含まなければ text、それ以外は base64)。
//     text を指定したのにバイナリなら、エラー。base64 を指定すれば、テキストでも、バイナリとして扱う。
//   - deploy の files: テキストだけ(バイナリは、エラー)。
//   - 1 つのファイルは、1 MiB まで(ConfigMap の上限)。
//
// 引数:
//   - template: 読み込んだテンプレート(中身を書き換える)。
//   - folder: テンプレートのフォルダ。
//
// 戻り値:
//   - 規則に合わないとき、ファイルを読めないときのエラー(どの部品の、どのファイルかが分かる)。
func resolveFiles(template *Template, folder fs.FS) error {
	// 部品ごとに、ファイルを順に見る。
	for resourceIndex := range template.Resources {
		resource := &template.Resources[resourceIndex]
		for fileIndex := range resource.Files {
			file := &resource.Files[fileIndex]
			if file.From == "" {
				continue
			}
			where := fmt.Sprintf("%s %s の files の from(%s)", resource.Kind, resource.Name, file.From)
			if file.Content != "" {
				return fmt.Errorf("%s: content と同時には、書けません", where)
			}
			if !fs.ValidPath(file.From) || file.From == "." {
				return fmt.Errorf("%s: フォルダの中の相対パスにしてください(「/」で始まる、「..」を含むパスは、使えません)", where)
			}
			data, readError := fs.ReadFile(folder, file.From)
			if readError != nil {
				return fmt.Errorf("%s: 読めません: %w", where, readError)
			}
			if len(data) > model.MaxConfigMapStoredBytes {
				return fmt.Errorf("%s: 大きすぎます(1MiB まで)", where)
			}
			isText := utf8.Valid(data) && !bytes.Contains(data, []byte{0})
			switch {
			case resource.Kind == ResourceDeploy:
				// deploy の files は、テキストだけ。
				if !isText {
					return fmt.Errorf("%s: deploy の files は、テキストだけです(バイナリは、configmap に置いてください)", where)
				}
				file.Content = string(data)
			case file.Encoding == model.ConfigMapEncodingBase64 || (file.Encoding == "" && !isText):
				// バイナリ(または、指定されたとき)は、base64 にする。
				file.Encoding = model.ConfigMapEncodingBase64
				file.Content = base64.StdEncoding.EncodeToString(data)
			case file.Encoding == "" || file.Encoding == model.ConfigMapEncodingText:
				// テキスト。text と指定されたのに、バイナリのときは、エラー。
				if !isText {
					return fmt.Errorf("%s: encoding が text ですが、UTF-8 のテキストではありません(base64 にしてください)", where)
				}
				file.Encoding = model.ConfigMapEncodingText
				file.Content = string(data)
			default:
				return fmt.Errorf("%s: encoding は、text か base64 です(%q)", where, file.Encoding)
			}
			// 読み込めたので、from は、空にする(あとの検証と展開は、content だけを見る)。
			file.From = ""
		}
	}
	return nil
}

// List は、全てのテンプレートを、既定の版で、id の順に返す。
//
// 戻り値:
//   - テンプレートの一覧(それぞれ、Metadata.Version が既定の版、Metadata.Versions が全ての版)。
func (registry *Registry) List() []Template {
	// 既定の版を集める。
	templates := make([]Template, 0, len(registry.ids))
	for _, id := range registry.ids {
		entry := registry.entries[id]
		templates = append(templates, entry.byVersion[entry.latest])
	}
	return templates
}

// Get は、id から、既定の版のテンプレートを引く。
//
// 引数:
//   - id: テンプレートの id。
//
// 戻り値:
//   - テンプレートと、あったか。
func (registry *Registry) Get(id string) (Template, bool) {
	// 既定の版に任せる。
	return registry.GetVersion(id, "")
}

// GetVersion は、id と版から、テンプレートを引く。
//
// 引数:
//   - id: テンプレートの id。
//   - version: 版(x.y.z)。空なら、既定の版。
//
// 戻り値:
//   - テンプレートと、あったか。
func (registry *Registry) GetVersion(id string, version string) (Template, bool) {
	// テンプレートを探す。
	entry, found := registry.entries[id]
	if !found {
		return Template{}, false
	}
	// 版が空なら、既定の版。
	if version == "" {
		version = entry.latest
	}
	template, exists := entry.byVersion[version]
	return template, exists
}
