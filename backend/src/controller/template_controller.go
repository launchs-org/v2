package controller

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/shared/template"
)

// TemplateUseCase は、TemplateController が呼ぶ、テンプレートのビジネスルール。
//
// 実体は service の TemplateService。テストでは fake に差し替える。
type TemplateUseCase interface {
	// List は、全てのテンプレートを、id の順に返す。
	List() []template.Template

	// Get は、id と版からテンプレートを1件返す。版が空なら、既定の版。無ければ not_found。
	Get(templateID string, version string) (template.Template, error)
}

// TemplateOptionResponse は、choice の選択肢1つ。
type TemplateOptionResponse struct {
	// Value は、変数に入る値。
	Value string `json:"value"`

	// Label は、画面に出す名前。
	Label string `json:"label"`
}

// TemplateVariableResponse は、テンプレートの変数(入力欄)1つ。
type TemplateVariableResponse struct {
	// Name は、変数名。
	Name string `json:"name"`

	// Label は、入力欄の見出し。
	Label string `json:"label"`

	// Type は、string、integer、password、choice、boolean のどれか。
	Type string `json:"type"`

	// Default は、既定の値。無ければ null。自動で作る password は、常に null(値を、返さない)。
	Default *string `json:"default"`

	// Required は、入力が必須か(既定値が無く、自動生成でもない)。
	Required bool `json:"required"`

	// Generate は、password を、入力が無いとき、自動で作るか。
	Generate bool `json:"generate"`

	// Options は、choice の選択肢。無ければ空の配列。
	Options []TemplateOptionResponse `json:"options"`

	// Min と Max は、integer の範囲。無ければ null。
	Min *int `json:"min"`
	Max *int `json:"max"`

	// MinLength と MaxLength は、string と password の長さの範囲。無ければ null。
	MinLength *int `json:"minLength"`
	MaxLength *int `json:"maxLength"`

	// Pattern は、string の値が、全体で一致すべき正規表現。無ければ null。
	Pattern *string `json:"pattern"`
}

// TemplateResponse は、テンプレート1件のレスポンスのボディ。作る部品の中身(resources)は、返さず、要約だけを返す。
type TemplateResponse struct {
	// ID は、テンプレートの識別子。
	ID string `json:"id"`

	// Name は、画面に出す名前。
	Name string `json:"name"`

	// Description は、画面に出す説明。
	Description string `json:"description"`

	// Icon は、画面の絵の名前。
	Icon string `json:"icon"`

	// Category は、分類(database、web、cache、tool、monitoring、dev)。無ければ空。
	Category string `json:"category"`

	// Notes は、作成が終わったあとに出す案内(${変数名} は、画面が、入力した値に置き換える)。無ければ空。
	Notes string `json:"notes"`

	// Version は、この応答の版(x.y.z)。一覧では、既定の版。
	Version string `json:"version"`

	// Versions は、選べる全ての版(新しい順)。
	Versions []string `json:"versions"`

	// Readme は、テンプレートの README.md(詳細だけ。一覧では空)。
	Readme string `json:"readme,omitempty"`

	// Variables は、入力欄の定義。
	Variables []TemplateVariableResponse `json:"variables"`

	// Summary は、何が作られるかの要約。
	Summary template.Summary `json:"summary"`
}

// ListTemplatesResponse は、テンプレートの一覧のレスポンスのボディ。
type ListTemplatesResponse struct {
	// Templates は、テンプレートの一覧(id の順)。
	Templates []TemplateResponse `json:"templates"`
}

// TemplateController は、/templates の HTTP の入出力を担当する。
type TemplateController struct {
	templateUseCase TemplateUseCase
}

// NewTemplateController は、TemplateController を作る。
//
// 引数:
//   - templateUseCase: テンプレートのビジネスルール。
//
// 戻り値:
//   - 作成した TemplateController。
func NewTemplateController(templateUseCase TemplateUseCase) *TemplateController {
	// ビジネスルールを持たせて返す。
	return &TemplateController{templateUseCase: templateUseCase}
}

// List は、GET /templates のハンドラー。テンプレートの一覧を返す。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - レスポンスを書き込めなかったときのエラー。認証は、ミドルウェアが確かめる(未認証は 401)。
func (templateController *TemplateController) List(echoContext *echo.Context) error {
	// テンプレートを、レスポンスの形にして返す(空でも、null ではなく、空の配列)。
	templates := templateController.templateUseCase.List()
	response := ListTemplatesResponse{Templates: make([]TemplateResponse, 0, len(templates))}
	for _, found := range templates {
		response.Templates = append(response.Templates, newTemplateResponse(found))
	}
	return echoContext.JSON(http.StatusOK, response)
}

// Get は、GET /templates/:templateId のハンドラー。テンプレートを1件返す(クエリ ?version=x.y.z で版を指定。省略は、既定の版)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 無ければ not_found。
func (templateController *TemplateController) Get(echoContext *echo.Context) error {
	// ビジネスルールに、取得を任せる。
	found, getError := templateController.templateUseCase.Get(echoContext.Param("templateId"), echoContext.QueryParam("version"))
	if getError != nil {
		return getError
	}
	// 詳細では、README も返す。
	response := newTemplateResponse(found)
	response.Readme = found.Metadata.Readme
	return echoContext.JSON(http.StatusOK, response)
}

// newTemplateResponse は、テンプレートを、レスポンスの形にする。
// 自動で作る password の既定値は、返さない(定義に、あっても、決して、見せない)。
//
// 引数:
//   - found: テンプレート。
//
// 戻り値:
//   - レスポンスのボディ。
func newTemplateResponse(found template.Template) TemplateResponse {
	// 変数を、1つずつ、レスポンスの形にする。
	variables := make([]TemplateVariableResponse, 0, len(found.Variables))
	for _, variable := range found.Variables {
		response := TemplateVariableResponse{
			Name: variable.Name, Label: variable.Label, Type: variable.Type, Default: variable.Default, Required: variable.RequiredVariable(), Generate: variable.Generate,
			Options: make([]TemplateOptionResponse, 0, len(variable.Options)), Min: variable.Min, Max: variable.Max, MinLength: variable.MinLength, MaxLength: variable.MaxLength,
		}
		// 自動で作る password は、既定値を返さない。
		if variable.Generate {
			response.Default = nil
		}
		if variable.Pattern != "" {
			pattern := variable.Pattern
			response.Pattern = &pattern
		}
		for _, option := range variable.Options {
			response.Options = append(response.Options, TemplateOptionResponse{Value: option.Value, Label: option.Label})
		}
		variables = append(variables, response)
	}
	return TemplateResponse{ID: found.Metadata.ID, Name: found.Metadata.Name, Description: found.Metadata.Description, Icon: found.Metadata.Icon, Category: found.Metadata.Category, Notes: found.Metadata.Notes, Version: found.Metadata.Version, Versions: append([]string{}, found.Metadata.Versions...), Variables: variables, Summary: found.Summarize()}
}
