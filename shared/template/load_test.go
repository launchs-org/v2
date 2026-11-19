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
	// 同梱の 16 個が、id の順に並ぶ。
	listed := registry.List()
	ids := make([]string, 0, len(listed))
	for _, bundled := range listed {
		ids = append(ids, bundled.Metadata.ID)
	}
	if strings.Join(ids, ",") != "cloudbeaver,code-server,excalidraw,gitea,grafana,mailpit,mariadb,memos,n8n,nginx,postgres,redash,redis,uptime-kuma,vaultwarden,wordpress" {
		testingT.Fatalf("16 個が、id の順にあるはず: %v", ids)
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

// versionedFolder は、Load のテスト用の、最小のテンプレート(<id>/manifest.yaml と、<id>/<版>/ の config.yaml、deploy.yaml)を作る。
//
// 引数:
//   - id: テンプレートの id(フォルダ名と、manifest の id)。
//   - versions: 作る版の名前(複数なら、全て同じ中身)。
//
// 戻り値:
//   - メモリ上のファイルシステム。
func versionedFolder(id string, versions ...string) fstest.MapFS {
	// manifest と、README を作る。
	folder := fstest.MapFS{
		id + "/manifest.yaml": {Data: []byte("id: " + id + "\nname: n\n")},
		id + "/README.md":     {Data: []byte("# " + id + "\n")},
	}
	// 版ごとに、config.yaml と deploy.yaml を作る。
	for _, version := range versions {
		folder[id+"/"+version+"/config.yaml"] = &fstest.MapFile{Data: []byte("notes: hi\n")}
		folder[id+"/"+version+"/deploy.yaml"] = &fstest.MapFile{Data: []byte("deploys:\n  - name: a\n    image: x\n    spec: small\n")}
	}
	return folder
}

// mergeFolders は、複数のメモリ上のファイルシステムを、1 つにまとめる。
//
// 引数:
//   - maps: まとめるもの。
//
// 戻り値:
//   - まとめたもの。
func mergeFolders(maps ...fstest.MapFS) fstest.MapFS {
	// 全てのファイルを写す。
	merged := fstest.MapFS{}
	for _, one := range maps {
		for name, file := range one {
			merged[name] = file
		}
	}
	return merged
}

// TestLoad は、読み込みの、版、既定の版、manifest との不一致、不正な YAML、検証エラー、ファイルの分け方の規則を確認する。
func TestLoad(testingT *testing.T) {
	testingT.Run("正しい 2 つは、id の順に読み込める", func(subTest *testing.T) {
		registry, loadError := Load(mergeFolders(versionedFolder("b", "1.0.0"), versionedFolder("a", "1.0.0")))
		if loadError != nil || len(registry.List()) != 2 || registry.List()[0].Metadata.ID != "a" {
			subTest.Errorf("a、b の順のはず: %v", loadError)
		}
	})
	testingT.Run("manifest の情報と、config.yaml の notes と、README と、版が、テンプレートに入る", func(subTest *testing.T) {
		registry, loadError := Load(versionedFolder("a", "1.0.0"))
		if loadError != nil {
			subTest.Fatal(loadError)
		}
		found, _ := registry.Get("a")
		if found.Metadata.Name != "n" || found.Metadata.Notes != "hi" || found.Metadata.Readme != "# a\n" || found.Metadata.Version != "1.0.0" || len(found.Metadata.Versions) != 1 {
			subTest.Errorf("入っているはず: %+v", found.Metadata)
		}
	})
	testingT.Run("版は、新しい順に並び(数として比べる)、既定は、いちばん新しい版", func(subTest *testing.T) {
		registry, loadError := Load(versionedFolder("a", "1.0.0", "1.10.0", "1.2.0", "0.9.9"))
		if loadError != nil {
			subTest.Fatal(loadError)
		}
		found, _ := registry.Get("a")
		if strings.Join(found.Metadata.Versions, ",") != "1.10.0,1.2.0,1.0.0,0.9.9" || found.Metadata.Version != "1.10.0" {
			subTest.Errorf("1.10.0 が最新のはず: %v %s", found.Metadata.Versions, found.Metadata.Version)
		}
	})
	testingT.Run("manifest の latest で、既定の版を選べる。GetVersion で、版を指定できる", func(subTest *testing.T) {
		folder := versionedFolder("a", "1.0.0", "2.0.0")
		folder["a/manifest.yaml"] = &fstest.MapFile{Data: []byte("id: a\nname: n\nlatest: 1.0.0\n")}
		registry, loadError := Load(folder)
		if loadError != nil {
			subTest.Fatal(loadError)
		}
		found, _ := registry.Get("a")
		old, oldFound := registry.GetVersion("a", "2.0.0")
		_, missing := registry.GetVersion("a", "3.0.0")
		if found.Metadata.Version != "1.0.0" || !oldFound || old.Metadata.Version != "2.0.0" || missing {
			subTest.Errorf("既定は 1.0.0、2.0.0 も引け、3.0.0 は無いはず: %s", found.Metadata.Version)
		}
	})
	testingT.Run("空のフォルダは、空の一覧", func(subTest *testing.T) {
		registry, loadError := Load(fstest.MapFS{})
		if loadError != nil || len(registry.List()) != 0 {
			subTest.Errorf("空のはず: %v", loadError)
		}
	})
	testingT.Run("直下の、フォルダでないもの(README、embed.go)は、無視する", func(subTest *testing.T) {
		registry, loadError := Load(mergeFolders(versionedFolder("a", "1.0.0"), fstest.MapFS{"README.md": {Data: []byte("# memo")}, "embed.go": {Data: []byte("package x")}}))
		if loadError != nil || len(registry.List()) != 1 {
			subTest.Errorf("1 つのはず: %v", loadError)
		}
	})
	testingT.Run("service.yaml の service は、deploy に付く。ingress.yaml は、公開になる", func(subTest *testing.T) {
		folder := versionedFolder("a", "1.0.0")
		folder["a/1.0.0/service.yaml"] = &fstest.MapFile{Data: []byte("services:\n  - deploy: a\n    ports:\n      - name: http\n        port: 80\n        protocol: tcp\n")}
		folder["a/1.0.0/ingress.yaml"] = &fstest.MapFile{Data: []byte("rules:\n  - path: /\n    deploy: a\n    port: 80\n")}
		registry, loadError := Load(folder)
		if loadError != nil {
			subTest.Fatal(loadError)
		}
		found, _ := registry.Get("a")
		if found.Resources[0].Service == nil || found.Resources[0].Service.Ports[0].Port != 80 || !found.Summarize().Ingress {
			subTest.Errorf("service と ingress があるはず: %+v", found.Resources)
		}
	})
	withFile := func(id string, name string, content string) fstest.MapFS {
		folder := versionedFolder(id, "1.0.0")
		folder[id+"/"+name] = &fstest.MapFile{Data: []byte(content)}
		return folder
	}
	testCases := []struct {
		name        string
		files       fstest.MapFS
		wantMessage string
	}{
		{"フォルダ名と manifest の id が違う", withFile("a", "manifest.yaml", "id: b\nname: n\n"), "フォルダ名"},
		{"壊れた manifest", withFile("a", "manifest.yaml", "a: ["), "テンプレート a"},
		{"manifest に、知らない項目", withFile("a", "manifest.yaml", "id: a\nname: n\nfoo: 1\n"), "foo"},
		{"検証に通らない(id が不正)", versionedFolder("A", "1.0.0"), "metadata.id"},
		{"manifest.yaml が無い", fstest.MapFS{"a/1.0.0/config.yaml": {Data: []byte("")}}, "manifest.yaml"},
		{"版のフォルダが無い", fstest.MapFS{"a/manifest.yaml": {Data: []byte("id: a\nname: n\n")}}, "版のフォルダ"},
		{"版の名前が、x.y.z でない", mergeFolders(versionedFolder("a", "1.0.0"), fstest.MapFS{"a/v2/config.yaml": {Data: []byte("")}}), "x.y.z"},
		{"版の名前に、先頭の 0 がある", versionedFolder("a", "01.0.0"), "x.y.z"},
		{"latest の版が無い", withFile("a", "manifest.yaml", "id: a\nname: n\nlatest: 9.9.9\n"), "latest"},
		{"config.yaml が無い版", fstest.MapFS{"a/manifest.yaml": {Data: []byte("id: a\nname: n\n")}, "a/1.0.0/deploy.yaml": {Data: []byte("")}}, "config.yaml"},
		{"deploy.yaml に kind を書いた", withFile("a", "1.0.0/deploy.yaml", "deploys:\n  - kind: deploy\n    name: a\n    image: x\n    spec: small\n"), "kind"},
		{"deploy の中に service を書いた", withFile("a", "1.0.0/deploy.yaml", "deploys:\n  - name: a\n    image: x\n    spec: small\n    service:\n      ports: []\n"), "service.yaml"},
		{"service.yaml が、無い deploy を指す", withFile("a", "1.0.0/service.yaml", "services:\n  - deploy: zz\n    ports: []\n"), "zz"},
		{"service.yaml で、同じ deploy が重複", withFile("a", "1.0.0/service.yaml", "services:\n  - deploy: a\n    ports: []\n  - deploy: a\n    ports: []\n"), "重複"},
		{"1 つが不正なら、全体がエラー(エラーに、テンプレート名が入る)", mergeFolders(versionedFolder("a", "1.0.0"), fstest.MapFS{"bad/manifest.yaml": {Data: []byte("a: [")}}), "テンプレート bad"},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name+" は、エラー", func(subTest *testing.T) {
			if _, loadError := Load(testCase.files); loadError == nil || !strings.Contains(loadError.Error(), testCase.wantMessage) {
				subTest.Errorf("%q を含むエラーになるはず: %v", testCase.wantMessage, loadError)
			}
		})
	}
	testingT.Run("旧形式の、直下の a.yaml は、読み込まれない", func(subTest *testing.T) {
		registry, loadError := Load(fstest.MapFS{"a.yaml": {Data: []byte("kind: x")}})
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
	// 与えたファイルと、files / deploy の files の書き方で、新しい構成のフォルダを作って読み込む。
	load := func(files string, deployFiles string, extra map[string][]byte) (*Registry, error) {
		folder := fstest.MapFS{
			"t/manifest.yaml":         {Data: []byte("id: t\nname: t\n")},
			"t/1.0.0/config.yaml":     {Data: []byte("variables:\n  - name: title\n    label: 題\n    type: string\n    default: hi\n")},
			"t/1.0.0/configmaps.yaml": {Data: []byte("configmaps:\n  - name: conf\n    files:\n" + files + "\n")},
			"t/1.0.0/deploy.yaml":     {Data: []byte("deploys:\n  - name: web\n    image: nginx\n    spec: small\n" + deployFiles + "\n")},
		}
		for name, data := range extra {
			folder["t/1.0.0/"+name] = &fstest.MapFile{Data: data}
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
