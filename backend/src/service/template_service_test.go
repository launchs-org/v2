package service

import (
	"testing"
	"testing/fstest"

	"launchs/shared/apperror"
	"launchs/shared/template"
)

// TestTemplateService は、一覧と取得(版の指定、無い id と無い版は not_found)を確認する。
func TestTemplateService(testingT *testing.T) {
	// 最小のテンプレート(新しい構成: manifest.yaml と、版のフォルダ)を作る。b は、2 つの版を持つ。
	template1 := func(id string, versions ...string) fstest.MapFS {
		folder := fstest.MapFS{id + "/manifest.yaml": {Data: []byte("id: " + id + "\nname: n\n")}}
		for _, version := range versions {
			folder[id+"/"+version+"/config.yaml"] = &fstest.MapFile{Data: []byte("")}
			folder[id+"/"+version+"/deploy.yaml"] = &fstest.MapFile{Data: []byte("deploys:\n  - name: a\n    image: x\n    spec: small\n")}
		}
		return folder
	}
	files := fstest.MapFS{}
	for _, folder := range []fstest.MapFS{template1("b", "1.0.0", "1.1.0"), template1("a", "1.0.0")} {
		for name, file := range folder {
			files[name] = file
		}
	}
	registry, loadError := template.Load(files)
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
		found, getError := templateService.Get("b", "")
		if getError != nil || found.Metadata.ID != "b" {
			subTest.Errorf("b のはず: %v", getError)
		}
	})
	testingT.Run("版を指定できる。版を指定しないと、最新。無い版は、not_found", func(subTest *testing.T) {
		latest, latestError := templateService.Get("b", "")
		old, oldError := templateService.Get("b", "1.0.0")
		_, missingError := templateService.Get("b", "9.9.9")
		if latestError != nil || latest.Metadata.Version != "1.1.0" || oldError != nil || old.Metadata.Version != "1.0.0" || apperror.KindOf(missingError) != apperror.KindNotFound {
			subTest.Errorf("最新は 1.1.0、指定すれば 1.0.0、無い版は not_found のはず: %v %v %v", latestError, oldError, missingError)
		}
	})
	testingT.Run("無い id は、not_found", func(subTest *testing.T) {
		if _, getError := templateService.Get("nope", ""); apperror.KindOf(getError) != apperror.KindNotFound {
			subTest.Errorf("not_found のはず: %v", getError)
		}
	})
}
