package service

import (
	"testing"
	"testing/fstest"

	"launchs/backend/src/template"
	"launchs/shared/apperror"
)

// TestTemplateService は、一覧と取得(無い id は not_found)を確認する。
func TestTemplateService(testingT *testing.T) {
	// 最小のテンプレートを 2 つ持つ、一覧を作る。
	yamlText := func(id string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte("apiVersion: launchs/v1\nkind: Template\nmetadata:\n  id: " + id + "\n  name: n\nresources:\n  - kind: deploy\n    name: a\n    image: x\n    spec: small\n")}
	}
	registry, loadError := template.Load(fstest.MapFS{"b.yaml": yamlText("b"), "a.yaml": yamlText("a")})
	if loadError != nil {
		testingT.Fatal(loadError)
	}
	templateService := NewTemplateService(registry)

	testingT.Run("一覧は、id の順", func(subTest *testing.T) {
		listed := templateService.List()
		if len(listed) != 2 || listed[0].Metadata.ID != "a" || listed[1].Metadata.ID != "b" {
			subTest.Errorf("a、b の順のはず: %+v", listed)
		}
	})
	testingT.Run("1 件を取得できる", func(subTest *testing.T) {
		found, getError := templateService.Get("b")
		if getError != nil || found.Metadata.ID != "b" {
			subTest.Errorf("b のはず: %v", getError)
		}
	})
	testingT.Run("無い id は、not_found", func(subTest *testing.T) {
		if _, getError := templateService.Get("nope"); apperror.KindOf(getError) != apperror.KindNotFound {
			subTest.Errorf("not_found のはず: %v", getError)
		}
	})
}
