package main

import (
	"strings"
	"testing"
)

// TestRenderBody は、独自のブロックと印が、HTML になることを確かめる。
func TestRenderBody(testingT *testing.T) {
	// 手順(入れ子の枠つき)、枠、よくある質問、言葉の説明、ボタン、状態の札、表を、1 つの本文に書く。
	source := "## 見出し {#anchor}\n\n" +
		":::steps\n1. **手順(1)**を行う\n\n   本文\n\n   ::::note warn 注意\n   気をつけて\n   ::::\n:::\n\n" +
		":::faq 質問ですか\n答えです。\n:::\n\n" +
		":::words\n📦 言葉 :: **説明**です\n:::\n\n" +
		"[[保存]](確認)と [[ok:active]]\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n```\n[[そのまま]] **そのまま**\n```\n"
	output, err := renderBody(source, "")
	if err != nil {
		testingT.Fatal(err)
	}
	// 期待する断片が、全て含まれているか。
	for _, want := range []string{
		`<h2 class="sec" id="anchor">`,
		`<ol class="steps">`,
		`<strong>手順(1)</strong>を行う`,
		`<div class="note warn"><b>注意</b>`,
		`<details><summary>質問ですか</summary>`,
		`<dt>📦 言葉</dt><dd><strong>説明</strong>です</dd>`,
		`<span class="ui">保存</span>`,
		`<span class="badge b-ok">active</span>`,
		`<div class="table-wrap"><table>`,
		`[[そのまま]] **そのまま**`,
	} {
		if !strings.Contains(output, want) {
			testingT.Errorf("出力に %q がありません:\n%s", want, output)
		}
	}
}

// TestRenderBodyUnclosed は、閉じていないブロックがエラーになることを確かめる。
func TestRenderBodyUnclosed(testingT *testing.T) {
	if _, err := renderBody(":::note tip 題\n本文\n", ""); err == nil {
		testingT.Error("閉じていないブロックが、エラーになりません")
	}
}

// TestCheckLinks は、リンク切れと、見出しの無いリンクが見つかることを確かめる。
func TestCheckLinks(testingT *testing.T) {
	rendered := map[string]string{
		"a": `<a href="b.html#x">ok</a><a href="c.html">なし</a><a href="b.html#y">見出しなし</a>`,
		"b": `<h2 id="x">x</h2>`,
	}
	err := checkLinks(rendered)
	if err == nil || !strings.Contains(err.Error(), "c.html") || !strings.Contains(err.Error(), "#y") {
		testingT.Errorf("リンク切れを見つけられません: %v", err)
	}
}

// TestCheckNav は、ナビに無いページと、原稿の無いナビがエラーになることを確かめる。
func TestCheckNav(testingT *testing.T) {
	pages := map[string]page{"a": {}, "b": {}}
	if _, err := checkNav(navFile{Groups: []navGroup{{Pages: []string{"a"}}}}, pages); err == nil {
		testingT.Error("ナビに無いページが、エラーになりません")
	}
	if _, err := checkNav(navFile{Groups: []navGroup{{Pages: []string{"a", "b", "z"}}}}, pages); err == nil {
		testingT.Error("原稿の無いナビが、エラーになりません")
	}
	if order, err := checkNav(navFile{Groups: []navGroup{{Pages: []string{"b", "a"}}}}, pages); err != nil || order[0] != "b" {
		testingT.Errorf("正しいナビが通りません: %v %v", order, err)
	}
}
