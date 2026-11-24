package service

import (
	"launchs/shared/apperror"
	"launchs/shared/template"
)

// TemplateService は、テンプレート(dev-docs/templates.md)の一覧と取得の、ビジネスルールを担当する。
//
// テンプレートは、リポジトリの YAML を起動時に読み込んだもので、データベースには無い。利用者によらず、全員に同じものを見せる。
type TemplateService struct {
	registry *template.Registry
}

// NewTemplateService は、TemplateService を作る。
//
// 引数:
//   - registry: 読み込み済みのテンプレートの一覧。
//
// 戻り値:
//   - 作成した TemplateService。
func NewTemplateService(registry *template.Registry) *TemplateService {
	// 一覧を持たせて返す。
	return &TemplateService{registry: registry}
}

// List は、全てのテンプレートを、id の順に返す。
//
// 戻り値:
//   - テンプレートの一覧。
func (templateService *TemplateService) List() []template.Template {
	// 一覧に任せる。
	return templateService.registry.List()
}

// Get は、id と版からテンプレートを1件返す。
//
// 引数:
//   - templateID: テンプレートの id。
//   - version: 版(x.y.z)。空なら、既定の版。
//
// 戻り値:
//   - テンプレート。
//   - テンプレートか版が無ければ not_found。
func (templateService *TemplateService) Get(templateID string, version string) (template.Template, error) {
	// 登録簿から引く。
	found, exists := templateService.registry.GetVersion(templateID, version)
	if !exists {
		return template.Template{}, apperror.New(apperror.KindNotFound, "テンプレートが見つかりません")
	}
	return found, nil
}
