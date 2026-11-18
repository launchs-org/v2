package controller

import (
	"testing"

	"launchs/shared/template"
)

// TestNewTemplateResponse は、レスポンスの組み立て(秘密の既定値を返さない、空の配列、必須かどうか)を、表形式で確認する。
func TestNewTemplateResponse(testingT *testing.T) {
	// 既定値を持つ変数を作るための、小さな補助。
	text := func(value string) *string { return &value }
	found := template.Template{
		Metadata: template.Metadata{ID: "t", Name: "テスト", Description: "説明", Icon: "database", Category: "database"},
		Variables: []template.Variable{
			{Name: "plain", Label: "普通", Type: template.VariableString, Default: text("x")},
			{Name: "generated", Label: "生成", Type: template.VariablePassword, Generate: true, Default: text("must-not-leak")},
			{Name: "required", Label: "必須", Type: template.VariableString},
			{Name: "pattern", Label: "形式", Type: template.VariableString, Pattern: "^a+$"},
			{Name: "choice", Label: "選択", Type: template.VariableChoice, Default: text("a"), Options: []template.Option{{Value: "a", Label: "A"}}},
		},
		Resources: []template.Resource{{Kind: template.ResourceVolume, Name: "v"}, {Kind: template.ResourceDeploy, Name: "d", Files: []template.File{{Path: "/a"}, {Path: "/b"}}}, {Kind: template.ResourceIngress}},
	}
	response := newTemplateResponse(found)
	byName := map[string]TemplateVariableResponse{}
	for _, variable := range response.Variables {
		byName[variable.Name] = variable
	}
	testCases := []struct {
		name  string
		check func() bool
	}{
		{"普通の変数は、既定値を返し、必須でない", func() bool {
			return byName["plain"].Default != nil && *byName["plain"].Default == "x" && !byName["plain"].Required
		}},
		{"自動生成の password は、定義に既定値があっても、返さない", func() bool {
			return byName["generated"].Default == nil && byName["generated"].Generate && !byName["generated"].Required
		}},
		{"既定値の無い変数は、必須", func() bool { return byName["required"].Required && byName["required"].Default == nil }},
		{"pattern が入る。無ければ null", func() bool {
			return byName["pattern"].Pattern != nil && *byName["pattern"].Pattern == "^a+$" && byName["plain"].Pattern == nil
		}},
		{"choice の options が入る。他は、null でなく空の配列", func() bool {
			return len(byName["choice"].Options) == 1 && byName["plain"].Options != nil && len(byName["plain"].Options) == 0
		}},
		{"要約は、deploy 1、volume 1、ingress あり、ファイル 2", func() bool {
			return response.Summary.Deploys == 1 && response.Summary.Volumes == 1 && response.Summary.Ingress && response.Summary.Files == 2
		}},
		{"metadata が入る", func() bool {
			return response.ID == "t" && response.Name == "テスト" && response.Category == "database" && response.Icon == "database"
		}},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			if !testCase.check() {
				subTest.Errorf("期待と違う: %+v", response)
			}
		})
	}
}
