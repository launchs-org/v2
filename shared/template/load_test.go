package template

import (
	"encoding/base64"
	"strings"
	"testing"
	"testing/fstest"

	"launchs/templates"
)

// TestLoadBundled は、同梱のテンプレート(templates/<id>/)が、全て、読み込みと検証に通ることを確認する。
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
	testingT.Run("nginx: フォルダの files/ から、text 2 つと、バイナリ(favicon.gif は GIF のまま、base64)を読み込む", func(subTest *testing.T) {
		nginx, _ := registry.Get("nginx")
		files := nginx.Resources[0].Files
		decoded, _ := base64.StdEncoding.DecodeString(files[2].Content)
		if len(files) != 3 || files[0].Encoding != "text" || !strings.Contains(files[0].Content, "gzip ${gzip};") || files[2].Key != "favicon.gif" || files[2].Encoding != "base64" || !strings.HasPrefix(string(decoded), "GIF89a") {
			subTest.Errorf("text 2 つと GIF のはず: %+v", files)
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

// TestLoad は、読み込みの、フォルダ名との不一致(これが id の重複も防ぐ)、不正な YAML、検証エラー、template.yaml が無いフォルダ、空のフォルダを確認する。
func TestLoad(testingT *testing.T) {
	// id のテンプレートのフォルダ(<id>/template.yaml)を作る。
	folder := func(id string) fstest.MapFS {
		return fstest.MapFS{id + "/" + TemplateFileName: {Data: []byte(strings.Replace(minimalYAML, "%ID%", id, 1))}}
	}
	merge := func(maps ...fstest.MapFS) fstest.MapFS {
		merged := fstest.MapFS{}
		for _, one := range maps {
			for name, file := range one {
				merged[name] = file
			}
		}
		return merged
	}
	testingT.Run("正しい 2 つは、id の順に読み込める", func(subTest *testing.T) {
		registry, loadError := Load(merge(folder("b"), folder("a")))
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
	testingT.Run("直下の、フォルダでないもの(README、embed.go)は、無視する", func(subTest *testing.T) {
		registry, loadError := Load(merge(folder("a"), fstest.MapFS{"README.md": {Data: []byte("# memo")}, "embed.go": {Data: []byte("package x")}}))
		if loadError != nil || len(registry.List()) != 1 {
			subTest.Errorf("1 つのはず: %v", loadError)
		}
	})
	testCases := []struct {
		name        string
		files       fstest.MapFS
		wantMessage string
	}{
		{"フォルダ名と id が違う", fstest.MapFS{"a/template.yaml": {Data: []byte(strings.Replace(minimalYAML, "%ID%", "b", 1))}}, "フォルダ名"},
		{"壊れた YAML", fstest.MapFS{"a/template.yaml": {Data: []byte("a: [")}}, "テンプレート a"},
		{"検証に通らない(id が不正)", fstest.MapFS{"A/template.yaml": {Data: []byte(strings.Replace(minimalYAML, "%ID%", "A", 1))}}, "metadata.id"},
		{"template.yaml が無いフォルダ", fstest.MapFS{"a/readme.md": {Data: []byte("x")}}, "template.yaml"},
		{"1 つが不正なら、全体がエラー(エラーに、テンプレート名が入る)", merge(folder("a"), fstest.MapFS{"bad/template.yaml": {Data: []byte("kind: x")}}), "テンプレート bad"},
		{"旧形式(直下の *.yaml)は、読まない(フォルダでないので、無視され、template.yaml が無いフォルダも無い)", fstest.MapFS{}, ""},
	}
	for _, testCase := range testCases {
		if testCase.wantMessage == "" {
			continue
		}
		testingT.Run(testCase.name+" は、エラー", func(subTest *testing.T) {
			if _, loadError := Load(testCase.files); loadError == nil || !strings.Contains(loadError.Error(), testCase.wantMessage) {
				subTest.Errorf("%q を含むエラーになるはず: %v", testCase.wantMessage, loadError)
			}
		})
	}
	testingT.Run("旧形式の、直下の a.yaml は、読み込まれない", func(subTest *testing.T) {
		registry, loadError := Load(fstest.MapFS{"a.yaml": {Data: []byte(strings.Replace(minimalYAML, "%ID%", "a", 1))}})
		if loadError != nil || len(registry.List()) != 0 {
			subTest.Errorf("空のはず: %v", loadError)
		}
	})
}

// fromYAML は、from: のテストに使う、configmap と deploy を持つテンプレート(%FILES% と %DEPLOYFILES% を差し込む)。
const fromYAML = `
apiVersion: launchs/v1
kind: Template
metadata:
  id: t
  name: t
variables:
  - name: title
    label: 題
    type: string
    default: hi
resources:
  - kind: configmap
    name: conf
    files:
%FILES%
  - kind: deploy
    name: web
    image: nginx
    spec: small
%DEPLOYFILES%
`

// TestLoadFrom は、from: が、フォルダのファイルに置き換わること(text、バイナリの自動判定、強制)と、規則に反するときのエラーを確認する。
func TestLoadFrom(testingT *testing.T) {
	// 与えたファイルと、files / deploy の files の書き方で、フォルダを作って読み込む。
	load := func(files string, deployFiles string, extra map[string][]byte) (*Registry, error) {
		text := strings.Replace(strings.Replace(fromYAML, "%FILES%", files, 1), "%DEPLOYFILES%", deployFiles, 1)
		folder := fstest.MapFS{"t/" + TemplateFileName: {Data: []byte(text)}}
		for name, data := range extra {
			folder["t/"+name] = &fstest.MapFile{Data: data}
		}
		return Load(folder)
	}
	configMapOf := func(registry *Registry) Resource {
		found, _ := registry.Get("t")
		return found.Resources[0]
	}
	testingT.Run("text は、内容になり、${変数} が使える(from は、空になる)", func(subTest *testing.T) {
		registry, loadError := load("      - key: a.conf\n        from: files/a.conf", "", map[string][]byte{"files/a.conf": []byte("title=${title}\n")})
		if loadError != nil {
			subTest.Fatal(loadError)
		}
		file := configMapOf(registry).Files[0]
		if file.Content != "title=${title}\n" || file.Encoding != "text" || file.From != "" {
			subTest.Errorf("text として読み込まれるはず: %+v", file)
		}
	})
	testingT.Run("NUL を含むファイルは、自動で base64 になる", func(subTest *testing.T) {
		registry, loadError := load("      - key: b.bin\n        from: files/b.bin", "", map[string][]byte{"files/b.bin": {0, 1, 2, 255}})
		if loadError != nil {
			subTest.Fatal(loadError)
		}
		file := configMapOf(registry).Files[0]
		if file.Encoding != "base64" || file.Content != base64.StdEncoding.EncodeToString([]byte{0, 1, 2, 255}) {
			subTest.Errorf("base64 のはず: %+v", file)
		}
	})
	testingT.Run("UTF-8 でないファイルも、base64 になる", func(subTest *testing.T) {
		registry, loadError := load("      - key: b.bin\n        from: files/b.bin", "", map[string][]byte{"files/b.bin": {0xff, 0xfe}})
		if loadError != nil || configMapOf(registry).Files[0].Encoding != "base64" {
			subTest.Errorf("base64 のはず: %v", loadError)
		}
	})
	testingT.Run("encoding: base64 を指定すると、テキストも、バイナリとして扱う(変数は展開されない)", func(subTest *testing.T) {
		registry, loadError := load("      - key: a.conf\n        encoding: base64\n        from: files/a.conf", "", map[string][]byte{"files/a.conf": []byte("x=${title}")})
		if loadError != nil || configMapOf(registry).Files[0].Encoding != "base64" {
			subTest.Errorf("base64 のはず: %v", loadError)
		}
	})
	testingT.Run("deploy の files にも、from が使える(text)", func(subTest *testing.T) {
		registry, loadError := load("      - key: a\n        content: x", "    files:\n      - path: /etc/a\n        from: files/a", map[string][]byte{"files/a": []byte("hello")})
		if loadError != nil {
			subTest.Fatal(loadError)
		}
		found, _ := registry.Get("t")
		if found.Resources[1].Files[0].Content != "hello" {
			subTest.Errorf("読み込まれるはず: %+v", found.Resources[1].Files)
		}
	})
	for _, testCase := range []struct {
		name        string
		files       string
		deployFiles string
		extra       map[string][]byte
		wantMessage string
	}{
		{"ファイルが無い", "      - key: a\n        from: files/none", "", nil, "読めません"},
		{"content と同時", "      - key: a\n        content: x\n        from: files/a", "", map[string][]byte{"files/a": []byte("x")}, "同時には"},
		{"フォルダの外(..)", "      - key: a\n        from: ../a", "", map[string][]byte{"files/a": []byte("x")}, "相対パス"},
		{"絶対パス", "      - key: a\n        from: /etc/passwd", "", nil, "相対パス"},
		{"text を指定したのに、バイナリ", "      - key: a\n        encoding: text\n        from: files/a", "", map[string][]byte{"files/a": {0, 1}}, "テキストではありません"},
		{"encoding が不正", "      - key: a\n        encoding: hex\n        from: files/a", "", map[string][]byte{"files/a": []byte("x")}, "encoding"},
		{"1MiB を超える", "      - key: a\n        from: files/a", "", map[string][]byte{"files/a": make([]byte, 1024*1024+1)}, "大きすぎます"},
		{"deploy の files に、バイナリ", "      - key: a\n        content: x", "    files:\n      - path: /etc/a\n        from: files/a", map[string][]byte{"files/a": {0, 1}}, "テキストだけ"},
	} {
		testingT.Run(testCase.name+" は、エラー", func(subTest *testing.T) {
			if _, loadError := load(testCase.files, testCase.deployFiles, testCase.extra); loadError == nil || !strings.Contains(loadError.Error(), testCase.wantMessage) {
				subTest.Errorf("%q を含むエラーのはず: %v", testCase.wantMessage, loadError)
			}
		})
	}
	testingT.Run("from が残ったテンプレートは、Validate が断る(Load を通していない)", func(subTest *testing.T) {
		parsed, parseError := Parse([]byte(strings.Replace(strings.Replace(fromYAML, "%FILES%", "      - key: a\n        from: files/a", 1), "%DEPLOYFILES%", "", 1)))
		if parseError != nil {
			subTest.Fatal(parseError)
		}
		if validateError := Validate(parsed); validateError == nil || !strings.Contains(validateError.Error(), "読み込まれていません") {
			subTest.Errorf("エラーのはず: %v", validateError)
		}
	})
}
