// Package harness は、Traefik のプラグインを、本物の Yaegi(Traefik が使う Go の解釈器)で読み込んで、試験するための道具。
// 通常の go test で通っても、Yaegi で動くとは限らない(標準ライブラリの一部しか使えない、など)ため、同じ読み込みの方法で確かめる。
// 実行時には使わない(テストだけ)。
package harness

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// Plugin は、Yaegi で読み込んだ、プラグイン 1 つ。
type Plugin struct {
	// interpreter は、Yaegi の解釈器。
	interpreter *interp.Interpreter
	// basePackage は、インポートしたパッケージの名前(Traefik と同じく、パスの最後の要素の「-」を「_」にしたもの)。
	basePackage string
}

// Load は、プラグインのフォルダを、Traefik のローカルプラグインと同じ置き方で、Yaegi に読み込む。
//
// 引数:
//   - pluginDirectory: プラグインのフォルダ(go.mod と .go ファイルがある)。
//   - modulePath: go.mod のモジュール名(例: github.com/launchs-org/launchs-edge)。
//
// 戻り値:
//   - 読み込んだプラグイン。読み込めないときのエラー。
func Load(pluginDirectory string, modulePath string) (*Plugin, error) {
	// Traefik の /plugins-local/src/<モジュール名>/ と同じ形の、GOPATH を、一時フォルダに作って、ソースを写す(テストファイルは除く)。
	goPath, mkdirError := os.MkdirTemp("", "yaegi-gopath-")
	if mkdirError != nil {
		return nil, mkdirError
	}
	target := filepath.Join(goPath, "src", filepath.FromSlash(modulePath))
	if copyError := os.MkdirAll(target, 0o755); copyError != nil {
		return nil, copyError
	}
	entries, readError := os.ReadDir(pluginDirectory)
	if readError != nil {
		return nil, readError
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		content, fileError := os.ReadFile(filepath.Join(pluginDirectory, entry.Name()))
		if fileError != nil {
			return nil, fileError
		}
		if writeError := os.WriteFile(filepath.Join(target, entry.Name()), content, 0o644); writeError != nil {
			return nil, writeError
		}
	}
	interpreter := interp.New(interp.Options{GoPath: goPath, Env: os.Environ()})
	if useError := interpreter.Use(stdlib.Symbols); useError != nil {
		return nil, useError
	}
	if _, evalError := interpreter.Eval(fmt.Sprintf("import %q", modulePath)); evalError != nil {
		return nil, fmt.Errorf("プラグインを読み込めません: %w", evalError)
	}
	base := strings.ReplaceAll(filepath.Base(modulePath), "-", "_")
	return &Plugin{interpreter: interpreter, basePackage: base}, nil
}

// New は、プラグインのミドルウェアを作る(Traefik と同じく、CreateConfig と New を、reflect で呼ぶ)。
//
// 引数:
//   - next: 次のハンドラー。
//   - settings: 設定の値(フィールド名 → 値。文字列、文字列の配列、整数など)。
//
// 戻り値:
//   - ミドルウェア。作れないときのエラー。
func (plugin *Plugin) New(next http.Handler, settings map[string]any) (http.Handler, error) {
	// CreateConfig で、既定の設定を作って、設定の値を、同じ名前のフィールドに入れる。
	createValue, createError := plugin.interpreter.Eval(plugin.basePackage + ".CreateConfig")
	if createError != nil {
		return nil, createError
	}
	config := reflect.ValueOf(createValue.Interface()).Call(nil)[0]
	for name, value := range settings {
		field := config.Elem().FieldByName(name)
		if !field.IsValid() {
			return nil, fmt.Errorf("設定にフィールドがありません: %s", name)
		}
		field.Set(reflect.ValueOf(value))
	}
	newValue, newError := plugin.interpreter.Eval(plugin.basePackage + ".New")
	if newError != nil {
		return nil, newError
	}
	results := reflect.ValueOf(newValue.Interface()).Call([]reflect.Value{reflect.ValueOf(context.Background()), reflect.ValueOf(next), config, reflect.ValueOf("test")})
	if !results[1].IsNil() {
		return nil, results[1].Interface().(error)
	}
	handler, isHandler := results[0].Interface().(http.Handler)
	if !isHandler {
		return nil, fmt.Errorf("New の戻り値が http.Handler ではありません")
	}
	return handler, nil
}
