package service

import (
	"launchs/backend/src/template"
	"launchs/shared/apperror"
)

// TemplateService は、テンプレート(docs/templates.md)の一覧と取得の、ビジネスルールを担当する。
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

// Get は、id からテンプレートを1件返す。
//
// 引数:
//   - templateID: テンプレートの id。
//
// 戻り値:
//   - テンプレート。
//   - 無ければ not_found。
func (templateService *TemplateService) Get(templateID string) (template.Template, error) {
	// 一覧から引く。
	found, exists := templateService.registry.Get(templateID)
	if !exists {
		return template.Template{}, apperror.New(apperror.KindNotFound, "テンプレートが見つかりません")
	}
	return found, nil
}
