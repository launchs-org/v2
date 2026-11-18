// Package templates は、このシステムに同梱する、テンプレートの定義(YAML)を、バイナリに埋め込んで公開する。
//
// 定義を足すときは、このフォルダに YAML を置く(ファイル名は、metadata.id と同じ)。読み込みと検証は、shared/template が行う。
// 埋め込みにしているのは、イメージに含め忘れない(ファイルが、実行時に、無いことが起きない)ためと、利用者が定義を変えられないため。
package templates

import "embed"

// Files は、同梱のテンプレートの YAML(templates/*.yaml)。
//
//go:embed *.yaml
var Files embed.FS
