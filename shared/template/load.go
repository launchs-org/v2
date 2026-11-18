package template

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"launchs/shared/model"
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

// TemplateFileName は、テンプレートのフォルダの中の、本体の YAML のファイル名。
const TemplateFileName = "template.yaml"

// Load は、フォルダの中の、テンプレートのフォルダ(<id>/template.yaml)を、全て、読み込み、検証する。
//
// フォルダの形:
//
//	<id>/
//	  template.yaml    本体(必須)
//	  files/...        from: で読み込む、ファイル(任意。置き場所は自由)
//
// フォルダ名は、metadata.id と同じでなければならない(これで、id の重複も、起こらない)。
// フォルダの直下の、フォルダでないもの(README など)は、無視する。
// YAML の from: は、そのテンプレートのフォルダの中のファイルだけを読める(外は、読めない)。
// 1つでも不正なら、全体がエラー(backend は、起動のときに呼び、落ちて気づく)。
//
// 引数:
//   - files: テンプレートのフォルダを含むファイルシステム(通常は、templates.Files)。
//
// 戻り値:
//   - 読み込んだテンプレートの一覧。
//   - 読めない、template.yaml が無い、from のファイルを読めない、検証に通らない、フォルダ名と id が違うときの、テンプレート名つきのエラー。
func Load(files fs.FS) (*Registry, error) {
	// 直下のフォルダを、名前の順に読む。
	entries, readError := fs.ReadDir(files, ".")
	if readError != nil {
		return nil, fmt.Errorf("テンプレートの一覧を読めません: %w", readError)
	}
	registry := &Registry{byID: map[string]Template{}}
	for _, entry := range entries {
		// フォルダでないもの(README など)は、無視する。
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		folder, subError := fs.Sub(files, name)
		if subError != nil {
			return nil, fmt.Errorf("テンプレート %s を開けません: %w", name, subError)
		}
		content, readError := fs.ReadFile(folder, TemplateFileName)
		if readError != nil {
			return nil, fmt.Errorf("テンプレート %s: %s を読めません: %w", name, TemplateFileName, readError)
		}
		template, parseError := Parse(content)
		if parseError != nil {
			return nil, fmt.Errorf("テンプレート %s: %w", name, parseError)
		}
		// from: のファイルを、フォルダから読み込んで、内容にする。
		if resolveError := resolveFiles(&template, folder); resolveError != nil {
			return nil, fmt.Errorf("テンプレート %s: %w", name, resolveError)
		}
		if validateError := Validate(template); validateError != nil {
			return nil, fmt.Errorf("テンプレート %s: %w", name, validateError)
		}
		// フォルダ名と id を、そろえる(探しやすくするため)。同じフォルダの名前は、重複しないので、
		// この確認が通れば、id も重複しない(id の重複を、別に確かめる必要が無い)。
		if template.Metadata.ID != name {
			return nil, fmt.Errorf("テンプレート %s: metadata.id(%q)が、フォルダ名(%q)と違います", name, template.Metadata.ID, name)
		}
		registry.byID[template.Metadata.ID] = template
		registry.ids = append(registry.ids, template.Metadata.ID)
	}
	return registry, nil
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
