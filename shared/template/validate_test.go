package template

import (
	"strings"
	"testing"
)

// validTemplateYAML は、検証に通る、最小のテンプレート(各テストが、一部を書き換えて、使う)。
const validTemplateYAML = `
apiVersion: launchs/v1
kind: Template
metadata:
  id: sample
  name: サンプル
  category: web
variables:
  - name: title
    label: 題
    type: string
    default: hello
  - name: storage
    label: 大きさ
    type: integer
    default: "512"
    min: 256
    max: 1024
resources:
  - kind: volume
    name: data
    sizeMB: ${storage}
  - kind: deploy
    name: web
    image: nginx:alpine
    spec: small
    env:
      TITLE: ${title}
    mounts:
      - volume: data
        path: /data
    files:
      - path: /data/index.html
        content: "<h1>${title}</h1>"
    service:
      ports:
        - name: http
          port: 80
          protocol: tcp
  - kind: ingress
    rules:
      - path: /
        deploy: web
        port: 80
`

// mustParse は、YAML をテンプレートとして読み込む(読めなければ、テストを止める)。
//
// 引数:
//   - testingT: テストの状態。
//   - yamlText: YAML のテキスト。
//
// 戻り値:
//   - 読み込んだテンプレート。
func mustParse(testingT *testing.T, yamlText string) Template {
	// 失敗の行を、このヘルパーではなく、呼び出し元として表示させる。
	testingT.Helper()
	parsed, parseError := Parse([]byte(yamlText))
	if parseError != nil {
		testingT.Fatalf("読み込めるはず: %v", parseError)
	}
	return parsed
}

// TestParse は、YAML の読み込みの、正常、未知のフィールド、複数のドキュメント、壊れた YAML を確認する。
func TestParse(testingT *testing.T) {
	testingT.Run("正しい YAML は、読み込め、検証にも通る", func(subTest *testing.T) {
		parsed := mustParse(subTest, validTemplateYAML)
		if validateError := Validate(parsed); validateError != nil {
			subTest.Errorf("通るはず: %v", validateError)
		}
		if parsed.Metadata.ID != "sample" || len(parsed.Resources) != 3 || parsed.Resources[1].Service.Ports[0].Port != 80 {
			subTest.Errorf("内容が読めていない: %+v", parsed)
		}
	})
	testCases := []struct {
		name string
		yaml string
	}{
		{"未知のフィールド(typo)", strings.Replace(validTemplateYAML, "spec: small", "spec: small\n    replica: 2", 1)},
		{"metadata の未知のフィールド", strings.Replace(validTemplateYAML, "name: サンプル", "name: サンプル\n  author: x", 1)},
		{"複数のドキュメント", validTemplateYAML + "\n---\n" + validTemplateYAML},
		{"壊れた YAML", "apiVersion: [launchs/v1"},
		{"型が違う(replicas が文字)", strings.Replace(validTemplateYAML, "spec: small", "spec: small\n    replicas: many", 1)},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name+" は、エラー", func(subTest *testing.T) {
			if _, parseError := Parse([]byte(testCase.yaml)); parseError == nil {
				subTest.Error("エラーになるはず")
			}
		})
	}
}

// TestValidate は、検証の各ルール(正常、異常、境界)を、表形式で確認する。
// 各ケースは、正しいテンプレートの一部を、文字列で書き換えて、期待のエラー(の一部)が出ることを確かめる。
func TestValidate(testingT *testing.T) {
	testCases := []struct {
		name        string
		replace     [2]string
		wantMessage string
	}{
		{"apiVersion が違う", [2]string{"apiVersion: launchs/v1", "apiVersion: launchs/v2"}, "apiVersion"},
		{"kind が違う", [2]string{"kind: Template", "kind: Other"}, "kind"},
		{"id に大文字", [2]string{"id: sample", "id: Sample"}, "metadata.id"},
		{"id が空", [2]string{"id: sample", "id: \"\""}, "metadata.id"},
		{"name が空", [2]string{"name: サンプル", "name: \"\""}, "metadata.name"},
		{"category が未知", [2]string{"category: web", "category: game"}, "category"},
		{"変数名の重複", [2]string{"  - name: storage\n    label: 大きさ", "  - name: title\n    label: 大きさ"}, "重複"},
		{"変数名の形(数字で始まる)", [2]string{"name: title\n    label: 題", "name: 1title\n    label: 題"}, "variables の名前"},
		{"変数の label が空", [2]string{"label: 題", "label: \"\""}, "label"},
		{"変数の type が未知", [2]string{"type: string", "type: text"}, "type"},
		{"generate は password だけ", [2]string{"type: string\n    default: hello", "type: string\n    generate: true\n    default: hello"}, "generate"},
		{"min は integer だけ", [2]string{"type: string\n    default: hello", "type: string\n    min: 1\n    default: hello"}, "min と max"},
		{"min が max より大きい", [2]string{"min: 256\n    max: 1024", "min: 2000\n    max: 1024"}, "min が max"},
		{"choice に options が無い", [2]string{"type: string\n    default: hello", "type: choice\n    default: hello"}, "options"},
		{"pattern が壊れている", [2]string{"type: string\n    default: hello", "type: string\n    pattern: \"[\"\n    default: hello"}, "pattern"},
		{"default が pattern に合わない", [2]string{"type: string\n    default: hello", "type: string\n    pattern: \"^[0-9]+$\"\n    default: hello"}, "default"},
		{"default が min より小さい", [2]string{"default: \"512\"\n    min: 256", "default: \"100\"\n    min: 256"}, "default"},
		{"resources の kind が未知", [2]string{"- kind: ingress", "- kind: cron"}, "kind は"},
		{"deploy の name が重複", [2]string{"  - kind: deploy\n    name: web", "  - kind: deploy\n    name: data"}, "重複"},
		{"spec が未知", [2]string{"spec: small", "spec: huge"}, "spec"},
		{"image に空白", [2]string{"image: nginx:alpine", "image: \"nginx alpine\""}, "image"},
		{"mounts の volume が無い", [2]string{"- volume: data", "- volume: nothing"}, "mounts の volume"},
		{"マウント先が不正", [2]string{"path: /data\n    files", "path: data\n    files"}, "mounts"},
		{"ファイルの path が不正", [2]string{"path: /data/index.html", "path: /data/../x"}, "files"},
		{"ファイルが、マウント先と同じ", [2]string{"path: /data/index.html", "path: /data"}, "マウント先と同じ"},
		{"ポート番号が範囲外", [2]string{"port: 80\n          protocol: tcp\n  - kind: ingress", "port: 70000\n          protocol: tcp\n  - kind: ingress"}, "1〜65535"},
		{"ポートの protocol が未知", [2]string{"protocol: tcp\n  - kind: ingress", "protocol: sctp\n  - kind: ingress"}, "protocol"},
		{"ポート名が大文字", [2]string{"name: http", "name: HTTP"}, "ポート名"},
		{"ポート名が数字だけ", [2]string{"name: http", "name: \"80\""}, "ポート名"},
		{"ポート名が16文字", [2]string{"name: http", "name: abcdefghijklmnop"}, "ポート名"},
		{"ingress の転送先 deploy が無い", [2]string{"deploy: web\n        port: 80", "deploy: nothing\n        port: 80"}, "rules の deploy"},
		{"ingress の転送先のポートが無い", [2]string{"deploy: web\n        port: 80", "deploy: web\n        port: 81"}, "ポート"},
		{"ingress の path が / で始まらない", [2]string{"- path: /\n        deploy", "- path: x\n        deploy"}, "path"},
		{"ingress に name", [2]string{"- kind: ingress\n    rules", "- kind: ingress\n    name: x\n    rules"}, "name を付けられません"},
		{"volume の sizeMB が 0", [2]string{"sizeMB: ${storage}", "sizeMB: \"0\""}, "1 以上"},
		{"volume の sizeMB が文字列の一部", [2]string{"sizeMB: ${storage}", "sizeMB: \"${storage}0\""}, "sizeMB"},
		{"volume の sizeMB の変数が無い", [2]string{"sizeMB: ${storage}", "sizeMB: ${nothing}"}, "variables にありません"},
		{"volume の sizeMB の変数が integer でない", [2]string{"sizeMB: ${storage}", "sizeMB: ${title}"}, "integer"},
		{"volume の sizeMB の変数に min/max が無い", [2]string{"default: \"512\"\n    min: 256\n    max: 1024", "default: \"512\""}, "min と max が必要"},
		{"知らない ${...}", [2]string{"TITLE: ${title}", "TITLE: ${titel}"}, "知らない参照"},
		{"ファイルの内容の、知らない ${...}", [2]string{"<h1>${title}</h1>", "<h1>${nothing}</h1>"}, "知らない参照"},
		{"閉じていない ${", [2]string{"TITLE: ${title}", "TITLE: ${title"}, "知らない参照"},
		{"service.X.dns の X が無い", [2]string{"TITLE: ${title}", "TITLE: ${service.nothing.dns}"}, "resources にありません"},
		{"自分自身の DNS は参照できない", [2]string{"TITLE: ${title}", "TITLE: ${service.web.dns}"}, "自分自身"},
		{"ユニットの無い使われない項目(volume に image)", [2]string{"sizeMB: ${storage}", "sizeMB: ${storage}\n    image: x"}, "deploy だけ"},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			yamlText := strings.Replace(validTemplateYAML, testCase.replace[0], testCase.replace[1], 1)
			if yamlText == validTemplateYAML {
				subTest.Fatalf("置き換えが効いていない(テストの書き間違い): %q", testCase.replace[0])
			}
			parsed, parseError := Parse([]byte(yamlText))
			if parseError != nil {
				subTest.Fatalf("読み込めるはず(検査で、落ちるべき): %v", parseError)
			}
			validateError := Validate(parsed)
			if validateError == nil || !strings.Contains(validateError.Error(), testCase.wantMessage) {
				subTest.Errorf("%q を含むエラーになるはず: %v", testCase.wantMessage, validateError)
			}
		})
	}
}

// TestValidateBoundaries は、境界(ちょうど通る値)を確認する。
func TestValidateBoundaries(testingT *testing.T) {
	testCases := []struct {
		name    string
		replace [2]string
	}{
		{"ポート名が15文字ちょうど", [2]string{"name: http", "name: abcdefghijklmno"}},
		{"ポート番号 65535(ingress の転送先も合わせる)", [2]string{"port: 80\n          protocol: tcp\n  - kind: ingress\n    rules:\n      - path: /\n        deploy: web\n        port: 80", "port: 65535\n          protocol: tcp\n  - kind: ingress\n    rules:\n      - path: /\n        deploy: web\n        port: 65535"}},
		{"volume の sizeMB が 1", [2]string{"sizeMB: ${storage}", "sizeMB: \"1\""}},
		{"replicas が 0", [2]string{"spec: small", "spec: small\n    replicas: 0"}},
		{"replicas が 5", [2]string{"spec: small", "spec: small\n    replicas: 5"}},
		{"ingress を持たない(ingress を削る)", [2]string{"  - kind: ingress\n    rules:\n      - path: /\n        deploy: web\n        port: 80\n", ""}},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name+" は、通る", func(subTest *testing.T) {
			yamlText := strings.Replace(validTemplateYAML, testCase.replace[0], testCase.replace[1], 1)
			if yamlText == validTemplateYAML {
				subTest.Fatalf("置き換えが効いていない: %q", testCase.replace[0])
			}
			if validateError := Validate(mustParse(subTest, yamlText)); validateError != nil {
				subTest.Errorf("通るはず: %v", validateError)
			}
		})
	}
	testingT.Run("replicas が 6 は、拒否", func(subTest *testing.T) {
		yamlText := strings.Replace(validTemplateYAML, "spec: small", "spec: small\n    replicas: 6", 1)
		if validateError := Validate(mustParse(subTest, yamlText)); validateError == nil || !strings.Contains(validateError.Error(), "replicas") {
			subTest.Errorf("拒否されるはず: %v", validateError)
		}
	})
}

// TestValidateCycle は、deploy 同士の ${service.X.dns} の循環(直接、間接)を、検出することを確認する。
func TestValidateCycle(testingT *testing.T) {
	// 3 つの deploy(a、b、c)が、それぞれ service を持つ。env で、参照し合う。
	build := func(aRefersTo string, bRefersTo string, cRefersTo string) string {
		deploy := func(name string, refersTo string) string {
			env := ""
			if refersTo != "" {
				env = "\n    env:\n      PEER: ${service." + refersTo + ".dns}"
			}
			return "  - kind: deploy\n    name: " + name + "\n    image: x\n    spec: small" + env + "\n    service:\n      ports:\n        - name: p\n          port: 80\n          protocol: tcp\n"
		}
		return "apiVersion: launchs/v1\nkind: Template\nmetadata:\n  id: cycle\n  name: c\nresources:\n" + deploy("a", aRefersTo) + deploy("b", bRefersTo) + deploy("c", cRefersTo)
	}
	testCases := []struct {
		name      string
		yaml      string
		wantCycle bool
	}{
		{"参照なし", build("", "", ""), false},
		{"一方向の鎖(a → b → c)は、循環ではない", build("b", "c", ""), false},
		{"直接の循環(a ⇄ b)", build("b", "a", ""), true},
		{"間接の循環(a → b → c → a)", build("b", "c", "a"), true},
		{"枝分かれ(a → b、a → c は、1つの deploy では 1つだけなので、b → c)", build("b", "c", ""), false},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			validateError := Validate(mustParse(subTest, testCase.yaml))
			gotCycle := validateError != nil && strings.Contains(validateError.Error(), "循環")
			if gotCycle != testCase.wantCycle {
				subTest.Errorf("循環の検出が違う(期待 %v): %v", testCase.wantCycle, validateError)
			}
			if !testCase.wantCycle && validateError != nil {
				subTest.Errorf("他のエラーは出ないはず: %v", validateError)
			}
		})
	}
}

// TestValidateNotes は、作成後の案内(notes)の検査(長さ、参照できるのは変数だけ)を確認する。
func TestValidateNotes(testingT *testing.T) {
	build := func(notes string, category string) Template {
		template := mustParse(testingT, validTemplateYAML)
		template.Metadata.Notes = notes
		template.Metadata.Category = category
		return template
	}
	if validateError := Validate(build("タイトルは ${title} です。", "web")); validateError != nil {
		testingT.Errorf("変数の参照は、許されるはず: %v", validateError)
	}
	if validateError := Validate(build("${unknown}", "web")); validateError == nil || !strings.Contains(validateError.Error(), "metadata.notes") {
		testingT.Errorf("知らない参照は、notes のエラーになるはず: %v", validateError)
	}
	if validateError := Validate(build("${service.web.dns}", "web")); validateError == nil {
		testingT.Error("notes では、service の参照は、使えないはず")
	}
	if validateError := Validate(build(strings.Repeat("あ", maxNotesRunes+1), "web")); validateError == nil {
		testingT.Error("長すぎる notes は、拒否するはず")
	}
	for _, category := range []string{"monitoring", "dev"} {
		if validateError := Validate(build("", category)); validateError != nil {
			testingT.Errorf("%s は、使えるカテゴリのはず: %v", category, validateError)
		}
	}
	if validateError := Validate(build("", "unknown")); validateError == nil {
		testingT.Error("知らないカテゴリは、拒否するはず")
	}
}
