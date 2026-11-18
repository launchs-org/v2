package template

import (
	"strings"
	"testing"
	"testing/fstest"

	"launchs/templates"
)

// TestLoadBundled は、同梱のテンプレート(templates/*.yaml)が、全て、読み込みと検証に通ることを確認する。
// 同梱の YAML を壊す変更を、ここで見つける(回帰テスト)。
func TestLoadBundled(testingT *testing.T) {
	registry, loadError := Load(templates.Files)
	if loadError != nil {
		testingT.Fatalf("同梱のテンプレートが、読み込めるはず: %v", loadError)
	}
	// 同梱の 5 つが、id の順に並ぶ。
	listed := registry.List()
	ids := make([]string, 0, len(listed))
	for _, bundled := range listed {
		ids = append(ids, bundled.Metadata.ID)
	}
	if strings.Join(ids, ",") != "code-server,mariadb,nginx,postgres,redis" {
		testingT.Fatalf("5 つが、id の順にあるはず: %v", ids)
	}
	// スペックは、データベースと Redis が medium、code-server が large、nginx が small(決めたとおり)。
	wantSpec := map[string]string{"nginx": "small", "redis": "medium", "postgres": "medium", "mariadb": "medium", "code-server": "large"}
	for _, bundled := range listed {
		for _, resource := range bundled.Resources {
			if resource.Kind == ResourceDeploy && resource.Spec != wantSpec[bundled.Metadata.ID] {
				testingT.Errorf("%s のスペックは %s のはず: %s", bundled.Metadata.ID, wantSpec[bundled.Metadata.ID], resource.Spec)
			}
		}
	}
	// 同梱のテンプレートごとに、既定の入力で、値が決まり、展開できる(service の DNS 名は、仮の値)。
	for _, bundled := range listed {
		testingT.Run(bundled.Metadata.ID+" は、必須の入力を入れれば、展開できる", func(subTest *testing.T) {
			input := map[string]string{}
			for _, variable := range bundled.Variables {
				if variable.RequiredVariable() {
					input[variable.Name] = "abc"
				}
			}
			resolved, resolveError := ResolveValues(bundled, input)
			if resolveError != nil {
				subTest.Fatalf("値が決まるはず: %v", resolveError)
			}
			dns := map[string]string{}
			for _, resource := range bundled.Resources {
				if resource.Kind == ResourceDeploy {
					dns[resource.Name] = resource.Name + ".svc.local"
				}
			}
			if _, expandError := Expand(bundled, resolved, ExpandContext{ProjectName: "my-project", ServiceDNS: dns}); expandError != nil {
				subTest.Errorf("展開できるはず: %v", expandError)
			}
		})
	}
	testingT.Run("postgres: password は自動生成で、既定値を持たず、入力は必須でない", func(subTest *testing.T) {
		postgres, found := registry.Get("postgres")
		if !found {
			subTest.Fatal("postgres があるはず")
		}
		for _, variable := range postgres.Variables {
			if variable.Name == "password" && (!variable.Generate || variable.Default != nil || variable.RequiredVariable()) {
				subTest.Errorf("password は、generate で、default 無し、必須でない: %+v", variable)
			}
		}
		if summary := postgres.Summarize(); summary.Deploys != 1 || summary.Volumes != 1 || summary.Ingress || summary.Files != 1 || summary.ConfigMaps != 1 {
			subTest.Errorf("要約が違う: %+v", summary)
		}
	})
	testingT.Run("nginx の要約: deploy 1、ingress あり、ConfigMap 1(設定、ページ、バイナリの画像)", func(subTest *testing.T) {
		nginx, _ := registry.Get("nginx")
		if summary := nginx.Summarize(); summary.Deploys != 1 || summary.Volumes != 0 || !summary.Ingress || summary.Files != 0 || summary.ConfigMaps != 1 {
			subTest.Errorf("要約が違う: %+v", summary)
		}
	})
	testingT.Run("無い id は、見つからない", func(subTest *testing.T) {
		if _, found := registry.Get("nope"); found {
			subTest.Error("見つからないはず")
		}
	})
}

// minimalYAML は、Load のテスト用の、最小のテンプレート(id を差し込む)。
const minimalYAML = `
apiVersion: launchs/v1
kind: Template
metadata:
  id: %ID%
  name: n
resources:
  - kind: deploy
    name: a
    image: x
    spec: small
`

// TestLoad は、読み込みの、ファイル名との不一致(これが id の重複も防ぐ)、不正な YAML、検証エラー、空のフォルダを確認する。
func TestLoad(testingT *testing.T) {
	file := func(id string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte(strings.Replace(minimalYAML, "%ID%", id, 1))}
	}
	testingT.Run("正しい 2 つは、id の順に読み込める", func(subTest *testing.T) {
		registry, loadError := Load(fstest.MapFS{"b.yaml": file("b"), "a.yaml": file("a")})
		if loadError != nil || len(registry.List()) != 2 || registry.List()[0].Metadata.ID != "a" {
			subTest.Errorf("a、b の順のはず: %v", loadError)
		}
	})
	testingT.Run("空のフォルダは、空の一覧", func(subTest *testing.T) {
		registry, loadError := Load(fstest.MapFS{})
		if loadError != nil || len(registry.List()) != 0 {
			subTest.Errorf("空のはず: %v", loadError)
		}
	})
	testingT.Run("yaml 以外のファイルは、無視する", func(subTest *testing.T) {
		registry, loadError := Load(fstest.MapFS{"a.yaml": file("a"), "README.md": {Data: []byte("# memo")}, "embed.go": {Data: []byte("package x")}})
		if loadError != nil || len(registry.List()) != 1 {
			subTest.Errorf("1 つのはず: %v", loadError)
		}
	})
	testCases := []struct {
		name        string
		files       fstest.MapFS
		wantMessage string
	}{
		{"ファイル名と id が違う", fstest.MapFS{"a.yaml": file("b")}, "ファイル名"},
		{"壊れた YAML", fstest.MapFS{"a.yaml": {Data: []byte("a: [")}}, "a.yaml"},
		{"検証に通らない(id が不正)", fstest.MapFS{"A.yaml": file("A")}, "metadata.id"},
		{"1 つが不正なら、全体がエラー(エラーに、ファイル名が入る)", fstest.MapFS{"a.yaml": file("a"), "bad.yaml": {Data: []byte("kind: x")}}, "bad.yaml"},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name+" は、エラー", func(subTest *testing.T) {
			if _, loadError := Load(testCase.files); loadError == nil || !strings.Contains(loadError.Error(), testCase.wantMessage) {
				subTest.Errorf("%q を含むエラーになるはず: %v", testCase.wantMessage, loadError)
			}
		})
	}
	testingT.Run("サブフォルダのファイルは、読まない(直下の *.yaml だけ。同じ id が、サブフォルダにあっても、重複にならない)", func(subTest *testing.T) {
		// 直下だけを読むので、サブフォルダの同じ id は、無視される(重複エラーにならず、読み込まれない)。
		registry, loadError := Load(fstest.MapFS{"a.yaml": file("a"), "sub/a.yaml": file("a")})
		if loadError != nil || len(registry.List()) != 1 {
			subTest.Errorf("直下の 1 つだけのはず: %v", loadError)
		}
	})
}
