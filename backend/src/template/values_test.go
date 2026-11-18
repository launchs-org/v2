package template

import (
	"strings"
	"testing"
)

// valuesTemplateYAML は、変数の検査と展開のテスト用のテンプレート(型ごとの変数を、一通り持つ)。
const valuesTemplateYAML = `
apiVersion: launchs/v1
kind: Template
metadata:
  id: values
  name: 値
variables:
  - name: name
    label: 名前
    type: string
    pattern: "[a-z][a-z0-9_]*"
    minLength: 2
    maxLength: 10
  - name: count
    label: 数
    type: integer
    default: "3"
    min: 1
    max: 10
  - name: secret
    label: 秘密
    type: password
    generate: true
    length: 16
  - name: mode
    label: 方式
    type: choice
    default: fast
    options:
      - value: fast
        label: 速い
      - value: safe
        label: 安全
  - name: debug
    label: デバッグ
    type: boolean
    default: "false"
resources:
  - kind: deploy
    name: app
    image: app:1
    spec: small
    env:
      NAME: ${name}
      COUNT: ${count}
      SECRET: ${secret}
      MODE: ${mode}
      DEBUG: ${debug}
      PROJECT: ${project.name}
    files:
      - path: /etc/app.conf
        content: "name=${name}\nsecret=${secret}\n"
    service:
      ports:
        - name: http
          port: 80
          protocol: tcp
  - kind: deploy
    name: worker
    image: worker:1
    spec: small
    env:
      APP: ${service.app.dns}
`

// TestResolveValues は、入力の補い(既定値、自動生成)と、型と制約の検査を、表形式で確認する。
func TestResolveValues(testingT *testing.T) {
	parsed := mustParse(testingT, valuesTemplateYAML)
	if validateError := Validate(parsed); validateError != nil {
		testingT.Fatalf("テスト用のテンプレートが、検証に通るはず: %v", validateError)
	}
	testCases := []struct {
		name        string
		input       map[string]string
		wantMessage string
	}{
		{"最小の入力(必須の name だけ)", map[string]string{"name": "app"}, ""},
		{"全ての入力", map[string]string{"name": "app_1", "count": "10", "secret": "mypassword", "mode": "safe", "debug": "true"}, ""},
		{"name が無い(必須)", map[string]string{}, "入力が必要"},
		{"name が空の文字(入力が無い扱い)", map[string]string{"name": ""}, "入力が必要"},
		{"name が pattern に合わない(大文字)", map[string]string{"name": "App"}, "形式"},
		{"name が pattern の一部にしか合わない", map[string]string{"name": "a-b"}, "形式"},
		{"name が短すぎる(1文字)", map[string]string{"name": "a"}, "2 文字以上"},
		{"name が長すぎる(11文字)", map[string]string{"name": "abcdefghijk"}, "10 文字以下"},
		{"name が10文字ちょうど", map[string]string{"name": "abcdefghij"}, ""},
		{"name に改行", map[string]string{"name": "ab\ncd"}, "形式"},
		{"count が整数でない", map[string]string{"name": "app", "count": "many"}, "整数"},
		{"count が min 未満(0)", map[string]string{"name": "app", "count": "0"}, "1 以上"},
		{"count が min ちょうど(1)", map[string]string{"name": "app", "count": "1"}, ""},
		{"count が max 超過(11)", map[string]string{"name": "app", "count": "11"}, "10 以下"},
		{"count が max ちょうど(10)", map[string]string{"name": "app", "count": "10"}, ""},
		{"mode が選択肢にない", map[string]string{"name": "app", "mode": "slow"}, "選択肢"},
		{"debug が true/false でない", map[string]string{"name": "app", "debug": "yes"}, "true か false"},
		{"テンプレートに無い変数", map[string]string{"name": "app", "nope": "x"}, "このテンプレートにありません"},
		{"password に改行", map[string]string{"name": "app", "secret": "a\nb"}, "改行"},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			resolved, resolveError := ResolveValues(parsed, testCase.input)
			if testCase.wantMessage == "" {
				if resolveError != nil {
					subTest.Fatalf("通るはず: %v", resolveError)
				}
				// 補った値が入っている(既定値と、自動生成)。
				if resolved["name"] == "" || resolved["count"] == "" || resolved["mode"] == "" || resolved["debug"] == "" || len(resolved["secret"]) < 8 {
					subTest.Errorf("全ての変数に、値が入るはず: %v", resolved)
				}
				return
			}
			if resolveError == nil || !strings.Contains(resolveError.Error(), testCase.wantMessage) {
				subTest.Errorf("%q を含むエラーになるはず: %v", testCase.wantMessage, resolveError)
			}
		})
	}
	testingT.Run("既定値と、自動生成の長さ(length: 16)", func(subTest *testing.T) {
		resolved, resolveError := ResolveValues(parsed, map[string]string{"name": "app"})
		if resolveError != nil || resolved["count"] != "3" || resolved["mode"] != "fast" || resolved["debug"] != "false" || len(resolved["secret"]) != 16 {
			subTest.Errorf("既定値が入り、secret は 16 文字のはず: %v %v", resolved, resolveError)
		}
	})
	testingT.Run("入力した password は、そのまま使われ、自動生成されない", func(subTest *testing.T) {
		resolved, _ := ResolveValues(parsed, map[string]string{"name": "app", "secret": "my-own-password"})
		if resolved["secret"] != "my-own-password" {
			subTest.Errorf("入力が使われるはず: %q", resolved["secret"])
		}
	})
	testingT.Run("自動生成は、呼ぶたびに違う値", func(subTest *testing.T) {
		first, _ := ResolveValues(parsed, map[string]string{"name": "app"})
		second, _ := ResolveValues(parsed, map[string]string{"name": "app"})
		if first["secret"] == second["secret"] {
			subTest.Error("2 回の自動生成が同じ値(偶然では起きない)")
		}
	})
	testingT.Run("ValidateValues は、同じ検査を行う", func(subTest *testing.T) {
		if validateError := ValidateValues(parsed, map[string]string{"name": "app"}); validateError != nil {
			subTest.Errorf("通るはず: %v", validateError)
		}
		if validateError := ValidateValues(parsed, map[string]string{"name": "X"}); validateError == nil {
			subTest.Error("拒否されるはず")
		}
	})
}

// TestGeneratePassword は、長さ、文字の集合、偏りの無さ(緩い統計)、不正な長さを確認する。
func TestGeneratePassword(testingT *testing.T) {
	testingT.Run("指定した長さで、英数字だけ", func(subTest *testing.T) {
		for _, length := range []int{1, 8, 24, 128} {
			password, generateError := GeneratePassword(length)
			if generateError != nil || len(password) != length {
				subTest.Fatalf("長さ %d のはず: %q %v", length, password, generateError)
			}
			for _, character := range password {
				if !strings.ContainsRune(passwordAlphabet, character) {
					subTest.Fatalf("英数字だけのはず: %q", password)
				}
			}
		}
	})
	testingT.Run("1万回で、全ての文字が現れ、極端な偏りが無い(統計的に緩い検査)", func(subTest *testing.T) {
		counts := map[rune]int{}
		total := 0
		for range 10000 {
			password, _ := GeneratePassword(10)
			for _, character := range password {
				counts[character]++
				total++
			}
		}
		// 10 万文字を 62 文字に配ると、期待は約 1613 回ずつ。全て現れ、期待の 0.7〜1.3 倍に収まるはず(偏りがあれば、外れる)。
		expected := float64(total) / float64(len(passwordAlphabet))
		for _, character := range passwordAlphabet {
			got := float64(counts[character])
			if got < expected*0.7 || got > expected*1.3 {
				subTest.Errorf("文字 %q の出現が偏っている: %v 回(期待 %.0f)", character, got, expected)
			}
		}
	})
	testingT.Run("0 以下の長さは、エラー", func(subTest *testing.T) {
		for _, length := range []int{0, -1} {
			if _, generateError := GeneratePassword(length); generateError == nil {
				subTest.Errorf("長さ %d は拒否されるはず", length)
			}
		}
	})
}

// TestExpand は、展開の置き換え、構造を壊さないこと、再展開しないこと、DNS 名、値が無いときのエラーを確認する。
func TestExpand(testingT *testing.T) {
	parsed := mustParse(testingT, valuesTemplateYAML)
	baseValues := func() map[string]string {
		return map[string]string{"name": "app", "count": "3", "secret": "S3cret", "mode": "fast", "debug": "false"}
	}
	context := ExpandContext{ProjectName: "my-project", ServiceDNS: map[string]string{"app": "service-x.project-y.svc.test-cluster.local"}}

	testingT.Run("env、ファイル、DNS 名、プロジェクト名が、置き換わる", func(subTest *testing.T) {
		expanded, expandError := Expand(parsed, baseValues(), context)
		if expandError != nil {
			subTest.Fatal(expandError)
		}
		app, worker := expanded.Deploys[0], expanded.Deploys[1]
		if app.Env["NAME"] != "app" || app.Env["COUNT"] != "3" || app.Env["SECRET"] != "S3cret" || app.Env["PROJECT"] != "my-project" || app.Files[0].Content != "name=app\nsecret=S3cret\n" {
			subTest.Errorf("置き換わるはず: %+v", app)
		}
		if worker.Env["APP"] != "service-x.project-y.svc.test-cluster.local" {
			subTest.Errorf("DNS 名に置き換わるはず: %q", worker.Env["APP"])
		}
		if app.Replicas != 1 || app.Service == nil || app.Service.Ports[0].Port != 80 {
			subTest.Errorf("台数の既定は 1、service はそのまま: %+v", app)
		}
	})
	testingT.Run("変数の値に、構造を壊す文字があっても、値の中に収まる(YAML を、文字列で組み立てていない)", func(subTest *testing.T) {
		// 改行、コロン、引用符、YAML の記号を含む値(password には、改行を許さないが、展開の関数としては、構造が壊れないことを確かめる)。
		hostile := "x: y\n  - z\n\"quoted\" # comment {a: b}"
		values := baseValues()
		values["secret"] = hostile
		expanded, expandError := Expand(parsed, values, context)
		if expandError != nil {
			subTest.Fatal(expandError)
		}
		app := expanded.Deploys[0]
		if app.Env["SECRET"] != hostile || app.Files[0].Content != "name=app\nsecret="+hostile+"\n" {
			subTest.Errorf("値が、そのまま入るはず: %q", app.Env["SECRET"])
		}
		// 他の項目が、壊れていない(値が、構造に漏れていない)。
		if len(app.Env) != 6 || len(app.Files) != 1 || app.Service == nil || len(expanded.Deploys) != 2 {
			subTest.Errorf("構造が壊れていない: env %d、files %d", len(app.Env), len(app.Files))
		}
	})
	testingT.Run("展開は 1 回だけ(置き換えた値の中の ${...} は、再び展開しない)", func(subTest *testing.T) {
		values := baseValues()
		values["secret"] = "${name}"
		expanded, _ := Expand(parsed, values, context)
		if got := expanded.Deploys[0].Env["SECRET"]; got != "${name}" {
			subTest.Errorf("${name} のままのはず(再展開されている): %q", got)
		}
		values["secret"] = "${service.app.dns}"
		expanded, _ = Expand(parsed, values, context)
		if got := expanded.Deploys[0].Files[0].Content; got != "name=app\nsecret=${service.app.dns}\n" {
			subTest.Errorf("再展開されていない: %q", got)
		}
	})
	testingT.Run("DNS 名が無いと、エラー", func(subTest *testing.T) {
		_, expandError := Expand(parsed, baseValues(), ExpandContext{ProjectName: "p"})
		if expandError == nil || !strings.Contains(expandError.Error(), "service.app.dns") {
			subTest.Errorf("エラーになるはず: %v", expandError)
		}
	})
	testingT.Run("変数の値が無いと、エラー", func(subTest *testing.T) {
		values := baseValues()
		delete(values, "name")
		if _, expandError := Expand(parsed, values, context); expandError == nil || !strings.Contains(expandError.Error(), "name") {
			subTest.Errorf("エラーになるはず: %v", expandError)
		}
	})
	testingT.Run("プロジェクト名が無いと、エラー", func(subTest *testing.T) {
		if _, expandError := Expand(parsed, baseValues(), ExpandContext{ServiceDNS: context.ServiceDNS}); expandError == nil {
			subTest.Error("エラーになるはず")
		}
	})
	testingT.Run("volume の sizeMB が、整数に展開される", func(subTest *testing.T) {
		volumeTemplate := mustParse(subTest, validTemplateYAML)
		expanded, expandError := Expand(volumeTemplate, map[string]string{"title": "t", "storage": "768"}, ExpandContext{ProjectName: "p"})
		if expandError != nil || len(expanded.Volumes) != 1 || expanded.Volumes[0].SizeMB != 768 || len(expanded.Rules) != 1 || expanded.Rules[0].Deploy != "web" {
			subTest.Errorf("768 になり、ingress のルールが残るはず: %+v %v", expanded, expandError)
		}
	})
	testingT.Run("volume の sizeMB が、整数にならない値なら、エラー(検査を通っていない値を渡した場合の、最後の砦)", func(subTest *testing.T) {
		volumeTemplate := mustParse(subTest, validTemplateYAML)
		if _, expandError := Expand(volumeTemplate, map[string]string{"title": "t", "storage": "abc"}, ExpandContext{ProjectName: "p"}); expandError == nil {
			subTest.Error("エラーになるはず")
		}
	})
}
