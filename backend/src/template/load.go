package template

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Registry は、読み込んだテンプレートの一覧(id から引ける)。
type Registry struct {
	// byID は、id → テンプレート。
	byID map[string]Template

	// ids は、id の辞書順(一覧の並びを、毎回、同じにするため)。
	ids []string
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

// Load は、フォルダの中の *.yaml を、全て、読み込み、検証する。
//
// ファイル名(拡張子を除く)は、metadata.id と同じでなければならない(これで、id の重複も、起こらない)。
// 1つでも不正なら、全体がエラー(backend は、起動のときに呼び、落ちて気づく)。
//
// 引数:
//   - files: テンプレートの YAML を含むファイルシステム(通常は、templates.Files)。
//
// 戻り値:
//   - 読み込んだテンプレートの一覧。
//   - 読めない、検証に通らない、id が重複する、ファイル名と id が違うときの、ファイル名つきのエラー。
func Load(files fs.FS) (*Registry, error) {
	// フォルダの直下の *.yaml を、名前の順に読む。
	names, globError := fs.Glob(files, "*.yaml")
	if globError != nil {
		return nil, fmt.Errorf("テンプレートの一覧を読めません: %w", globError)
	}
	sort.Strings(names)
	registry := &Registry{byID: map[string]Template{}}
	for _, name := range names {
		content, readError := fs.ReadFile(files, name)
		if readError != nil {
			return nil, fmt.Errorf("テンプレート %s を読めません: %w", name, readError)
		}
		template, parseError := Parse(content)
		if parseError != nil {
			return nil, fmt.Errorf("テンプレート %s: %w", name, parseError)
		}
		if validateError := Validate(template); validateError != nil {
			return nil, fmt.Errorf("テンプレート %s: %w", name, validateError)
		}
		// ファイル名と id を、そろえる(探しやすくするため)。同じフォルダのファイル名は、重複しないので、
		// この確認が通れば、id も重複しない(id の重複を、別に確かめる必要が無い)。
		if expected := strings.TrimSuffix(path.Base(name), ".yaml"); template.Metadata.ID != expected {
			return nil, fmt.Errorf("テンプレート %s: metadata.id(%q)が、ファイル名(%q)と違います", name, template.Metadata.ID, expected)
		}
		registry.byID[template.Metadata.ID] = template
		registry.ids = append(registry.ids, template.Metadata.ID)
	}
	return registry, nil
}

// List は、全てのテンプレートを、id の順に返す。
//
// 戻り値:
//   - テンプレートの一覧(コピー)。
func (registry *Registry) List() []Template {
	// id の順に並べて返す。
	templates := make([]Template, 0, len(registry.ids))
	for _, id := range registry.ids {
		templates = append(templates, registry.byID[id])
	}
	return templates
}

// Get は、id からテンプレートを引く。
//
// 引数:
//   - id: テンプレートの id。
//
// 戻り値:
//   - テンプレートと、見つかったか。
func (registry *Registry) Get(id string) (Template, bool) {
	// 表から引く。
	template, found := registry.byID[id]
	return template, found
}
