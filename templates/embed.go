// Package templates は、このシステムに同梱する、テンプレート(フォルダ形式)を、バイナリに埋め込んで公開する。
//
// テンプレートは、1 つにつき 1 つのフォルダ(<id>/template.yaml と、from: で読み込むファイル)。
// 追加するときは、このフォルダの下に、metadata.id と同じ名前のフォルダを作る。読み込みと検証は、shared/template が行う。
// 埋め込みにしているのは、イメージに含め忘れない(ファイルが、実行時に、無いことが起きない)ためと、利用者が定義を変えられないため。
package templates

import "embed"

// Files は、同梱のテンプレート(templates/<id>/...)。フォルダの中を、全て埋め込む。
//
//go:embed */*
var Files embed.FS
