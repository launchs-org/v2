package template

import (
	"encoding/base64"
	"strings"
	"testing"
)

// configMapTemplateYAML は、検証に通る、configmap を使うテンプレート(各テストが、一部を書き換えて使う)。
const configMapTemplateYAML = `
apiVersion: launchs/v1
kind: Template
metadata:
  id: with-configmap
  name: ConfigMap つき
  category: web
variables:
  - name: title
    label: 題
    type: string
    default: hello
resources:
  - kind: configmap
    name: conf
    files:
      - key: app.conf
        content: "title=${title}\n"
      - key: logo.bin
        encoding: base64
        content: AAEC/w==
  - kind: deploy
    name: web
    image: nginx:alpine
    spec: small
    files:
      - path: /etc/own.conf
        content: own
    configmaps:
      - configmap: conf
        key: app.conf
        path: /etc/app/app.conf
        mode: "0600"
      - configmap: conf
        key: logo.bin
        path: /usr/share/logo.bin
`

// TestConfigMapValidate は、configmap の定義と、deploy からのマウントの検証(正常、異常)を確認する。
func TestConfigMapValidate(testingT *testing.T) {
	testingT.Run("正しいテンプレートは、読み込めて、検証に通り、要約に configmap が入る", func(subTest *testing.T) {
		parsed := mustParse(subTest, configMapTemplateYAML)
		if validateError := Validate(parsed); validateError != nil {
			subTest.Fatalf("通るはず: %v", validateError)
		}
		summary := parsed.Summarize()
		if summary.ConfigMaps != 1 || summary.Deploys != 1 || summary.Files != 1 {
			subTest.Errorf("configmap 1、deploy 1、files 1 のはず: %+v", summary)
		}
	})
	for _, testCase := range []struct {
		name        string
		replace     [2]string
		wantMessage string
	}{
		{"configmap の name が、deploy と重複", [2]string{"name: conf", "name: web"}, "重複"},
		{"configmap の files が空", [2]string{"    files:\n      - key: app.conf\n        content: \"title=${title}\\n\"\n      - key: logo.bin\n        encoding: base64\n        content: AAEC/w==\n", "    files: []\n"}, "files が空"},
		{"configmap の files に path がある", [2]string{"      - key: app.conf", "      - path: /x\n        key: app.conf"}, "path ではなく、key"},
		{"key に / がある", [2]string{"key: app.conf\n        content: \"title", "key: conf.d/app.conf\n        content: \"title"}, "key"},
		{"key が重複", [2]string{"key: logo.bin", "key: app.conf"}, "重複"},
		{"encoding が未知", [2]string{"encoding: base64", "encoding: hex"}, "encoding"},
		{"base64 が正しくない", [2]string{"content: AAEC/w==", "content: \"not base64!\""}, "base64"},
		{"base64 に変数がある", [2]string{"content: AAEC/w==", "content: \"${title}\""}, "base64"},
		{"configmap の内容が、知らない変数を指す", [2]string{"title=${title}", "title=${nothing}"}, "知らない参照"},
		{"configmap の内容が、service の DNS を参照", [2]string{"title=${title}", "title=${service.web.dns}"}, "参照できません"},
		{"deploy が、無い configmap を指す", [2]string{"- configmap: conf\n        key: app.conf", "- configmap: nothing\n        key: app.conf"}, "resources にありません"},
		{"deploy が、無い key を指す", [2]string{"key: app.conf\n        path: /etc/app/app.conf", "key: nothing.conf\n        path: /etc/app/app.conf"}, "files にありません"},
		{"mode が不正", [2]string{`mode: "0600"`, `mode: "0999"`}, "mode"},
		{"マウント先が、deploy のファイルと同じ", [2]string{"path: /etc/app/app.conf", "path: /etc/own.conf"}, "同じ"},
		{"マウント先が、configmap のマウント同士で重複", [2]string{"path: /usr/share/logo.bin", "path: /etc/app/app.conf"}, "重複"},
		{"path が不正(..)", [2]string{"path: /etc/app/app.conf", "path: /etc/../app.conf"}, "path"},
		{"configmaps は deploy だけ", [2]string{"  - kind: configmap\n    name: conf\n", "  - kind: configmap\n    name: conf\n    image: x\n"}, "deploy だけ"},
	} {
		testingT.Run(testCase.name+" は、エラー", func(subTest *testing.T) {
			parsed, parseError := Parse([]byte(strings.Replace(configMapTemplateYAML, testCase.replace[0], testCase.replace[1], 1)))
			if parseError != nil {
				subTest.Skipf("読み込みの時点で断られた(検証の前): %v", parseError)
			}
			validateError := Validate(parsed)
			if validateError == nil || !strings.Contains(validateError.Error(), testCase.wantMessage) {
				subTest.Errorf("%q を含むエラーのはず: %v", testCase.wantMessage, validateError)
			}
		})
	}
	testingT.Run("deploy が、configmap を使わなくてもよい(configmap だけの部品も、許す)", func(subTest *testing.T) {
		withoutMounts := strings.Replace(configMapTemplateYAML, `    configmaps:
      - configmap: conf
        key: app.conf
        path: /etc/app/app.conf
        mode: "0600"
      - configmap: conf
        key: logo.bin
        path: /usr/share/logo.bin
`, "", 1)
		if validateError := Validate(mustParse(subTest, withoutMounts)); validateError != nil {
			subTest.Errorf("通るはず: %v", validateError)
		}
	})
}

// TestConfigMapExpand は、configmap の展開(text だけ展開、base64 はそのまま、mode の既定、展開後の検査)を確認する。
func TestConfigMapExpand(testingT *testing.T) {
	parsed := mustParse(testingT, configMapTemplateYAML)
	testingT.Run("text の内容だけ展開し、base64 はそのまま。key の昇順で、mode の省略は 0644", func(subTest *testing.T) {
		expanded, expandError := Expand(parsed, map[string]string{"title": "Hi"}, ExpandContext{ProjectName: "p"})
		if expandError != nil {
			subTest.Fatal(expandError)
		}
		files := expanded.ConfigMaps[0].Files
		if len(files) != 2 || files[0].Key != "app.conf" || files[0].Content != "title=Hi\n" || files[1].Key != "logo.bin" || files[1].Encoding != "base64" || files[1].Content != "AAEC/w==" {
			subTest.Errorf("text だけ展開されるはず: %+v", files)
		}
		mounts := expanded.Deploys[0].ConfigMaps
		if len(mounts) != 2 || mounts[0].Mode != "0600" || mounts[1].Mode != "0644" || mounts[1].ConfigMap != "conf" {
			subTest.Errorf("mode の省略は 0644 のはず: %+v", mounts)
		}
	})
	testingT.Run("変数の値に、NUL が入ると、展開後の検査で断る", func(subTest *testing.T) {
		if _, expandError := Expand(parsed, map[string]string{"title": "a\x00b"}, ExpandContext{ProjectName: "p"}); expandError == nil {
			subTest.Error("エラーになるはず")
		}
	})
	testingT.Run("変数の値で、1MiB を超えると、展開後の検査で断る", func(subTest *testing.T) {
		if _, expandError := Expand(parsed, map[string]string{"title": strings.Repeat("a", 1024*1024)}, ExpandContext{ProjectName: "p"}); expandError == nil {
			subTest.Error("エラーになるはず")
		}
	})
	testingT.Run("変数の値に ${...} が入っていても、再展開しない", func(subTest *testing.T) {
		expanded, expandError := Expand(parsed, map[string]string{"title": "${title}"}, ExpandContext{ProjectName: "p"})
		if expandError != nil || expanded.ConfigMaps[0].Files[0].Content != "title=${title}\n" {
			subTest.Errorf("そのまま入るはず: %v %+v", expandError, expanded.ConfigMaps)
		}
	})
	testingT.Run("base64 は、デコードできる形のまま渡る", func(subTest *testing.T) {
		expanded, _ := Expand(parsed, map[string]string{"title": "x"}, ExpandContext{ProjectName: "p"})
		decoded, decodeError := base64.StdEncoding.DecodeString(expanded.ConfigMaps[0].Files[1].Content)
		if decodeError != nil || string(decoded) != "\x00\x01\x02\xff" {
			subTest.Errorf("元のバイト列のはず: %v %v", decoded, decodeError)
		}
	})
}
