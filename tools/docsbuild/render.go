package main

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

// markdownEngine は、Markdown を HTML にする goldmark。表、見出しの ID と {#id}、生の HTML を使える。
var markdownEngine = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.Strikethrough),
	goldmark.WithParserOptions(parser.WithAutoHeadingID(), parser.WithAttribute()),
	goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
)

// blockOpenPattern は、独自のブロックの開始の行(`:::note tip 題名` など。字下げと、コロンの数は自由)。
var blockOpenPattern = regexp.MustCompile(`^(\s*)(:{3,})\s*([a-z]+)\s*(.*)$`)

// blockClosePattern は、独自のブロックの終了の行(コロンだけの行)。
var blockClosePattern = regexp.MustCompile(`^\s*(:{3,})\s*$`)

// renderBody は、1 ページの本文(Markdown)を HTML にする。
//
// 独自のブロック(`:::note`、`:::steps`、`:::words`、`:::templates`)は、先に HTML にして、目印の行に置き換えてから、
// Markdown 全体を変換し、最後に目印を HTML に戻す(リストの中のブロックも、字下げのまま扱える)。
// 引数 source は本文、templateHTML は `:::templates` の場所に入れる HTML。戻り値は HTML。
func renderBody(source string, templateHTML string) (string, error) {
	// 独自のブロックを、目印に置き換える。
	replaced, blocks, err := extractBlocks(source, templateHTML)
	if err != nil {
		return "", err
	}
	// 全体を HTML にする。
	var buffer bytes.Buffer
	if err := markdownEngine.Convert([]byte(replaced), &buffer); err != nil {
		return "", err
	}
	output := buffer.String()
	// 目印を、ブロックの HTML に戻す(中にさらに目印があるので、繰り返す)。
	for round := 0; round < 5; round++ {
		changed := false
		for index, block := range blocks {
			marker := fmt.Sprintf("<!--BLOCK:%d-->", index)
			if strings.Contains(output, marker) {
				output = strings.ReplaceAll(output, marker, block)
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return decorate(output), nil
}

// extractBlocks は、本文から独自のブロックを探し、HTML にして、目印の行に置き換える。
// 戻り値は、置き換えた本文、ブロックの HTML の並び(目印の番号が添字)。
func extractBlocks(source string, templateHTML string) (string, []string, error) {
	lines := strings.Split(source, "\n")
	var output []string
	var blocks []string
	inFence := false
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		// コードの囲み(```)の中は、そのまま。
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		match := blockOpenPattern.FindStringSubmatch(line)
		if inFence || match == nil {
			// コードの外では、**強調** と [[ボタン]] を HTML にする(日本語の隣では、Markdown の強調が効かないため)。
			if !inFence && !strings.HasPrefix(strings.TrimSpace(line), "```") {
				line = convertInline(line)
			}
			output = append(output, line)
			continue
		}
		// 対応する終了の行(同じコロンの数)まで集める。入れ子は、コロンの数の違いで、見分ける。
		indent, colons, kind, rest := match[1], match[2], match[3], match[4]
		end := -1
		for next := index + 1; next < len(lines); next++ {
			closing := blockClosePattern.FindStringSubmatch(lines[next])
			if closing != nil && closing[1] == colons {
				end = next
				break
			}
		}
		if end < 0 {
			return "", nil, fmt.Errorf("ブロックが閉じていません(%d 行目: %s)", index+1, line)
		}
		// 中身の字下げを取り除く。
		inner := make([]string, 0, end-index-1)
		for _, innerLine := range lines[index+1 : end] {
			inner = append(inner, strings.TrimPrefix(innerLine, indent))
		}
		rendered, err := renderBlock(kind, rest, strings.Join(inner, "\n"), templateHTML)
		if err != nil {
			return "", nil, fmt.Errorf("%d 行目のブロック(%s): %w", index+1, kind, err)
		}
		blocks = append(blocks, rendered)
		output = append(output, fmt.Sprintf("%s<!--BLOCK:%d-->", indent, len(blocks)-1))
		index = end
	}
	return strings.Join(output, "\n"), blocks, nil
}

// renderBlock は、独自のブロック 1 つを HTML にする。
// 引数 kind は種類(note、faq、steps、words、templates)、rest は開始の行の残り、inner は中身。
func renderBlock(kind, rest, inner, templateHTML string) (string, error) {
	switch kind {
	case "note":
		// `note tip 題名` の形。先頭の語が種類(info、tip、warn)、残りが題名。
		fields := strings.SplitN(strings.TrimSpace(rest), " ", 2)
		style := fields[0]
		if style != "info" && style != "tip" && style != "warn" {
			return "", fmt.Errorf("note の種類は info、tip、warn のどれか: %q", style)
		}
		title := ""
		if len(fields) == 2 {
			title = "<b>" + html.EscapeString(strings.TrimSpace(fields[1])) + "</b>"
		}
		body, err := renderBody(inner, templateHTML)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("<div class=\"note %s\">%s\n%s</div>", style, title, body), nil
	case "faq":
		// `faq 質問の文` の形。押すと開く、質問と答え。
		body, err := renderBody(inner, templateHTML)
		if err != nil {
			return "", err
		}
		return "<details><summary>" + html.EscapeString(strings.TrimSpace(rest)) + "</summary><div class=\"a\">" + body + "</div></details>", nil
	case "steps":
		body, err := renderBody(inner, templateHTML)
		if err != nil {
			return "", err
		}
		// 最初の番号つきリストを、手順の見た目にする。
		return strings.Replace(body, "<ol>", "<ol class=\"steps\">", 1), nil
	case "words":
		return renderWords(inner)
	case "templates":
		return templateHTML, nil
	}
	return "", fmt.Errorf("知らないブロックです: %s", kind)
}

// renderWords は、`言葉 :: 説明` の行の並びを、言葉の説明(dl.words)にする。
func renderWords(inner string) (string, error) {
	var buffer strings.Builder
	buffer.WriteString("<dl class=\"words\">\n")
	for _, line := range strings.Split(inner, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " :: ", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("words の行は「言葉 :: 説明」の形: %q", line)
		}
		description, err := renderInline(convertInline(parts[1]))
		if err != nil {
			return "", err
		}
		buffer.WriteString("<dt>" + html.EscapeString(parts[0]) + "</dt><dd>" + description + "</dd>\n")
	}
	buffer.WriteString("</dl>")
	return buffer.String(), nil
}

// renderInline は、1 段落ぶんの Markdown を、段落の囲み(p)なしの HTML にする。
func renderInline(source string) (string, error) {
	var buffer bytes.Buffer
	if err := markdownEngine.Convert([]byte(source), &buffer); err != nil {
		return "", err
	}
	output := strings.TrimSpace(buffer.String())
	output = strings.TrimPrefix(output, "<p>")
	output = strings.TrimSuffix(output, "</p>")
	return output, nil
}

// uiPattern は、`[[保存]]`(操作のボタンの名前)と `[[ok:active]]`(状態の札)。
var uiPattern = regexp.MustCompile(`\[\[(?:(ok|run|bad|warn|mut):)?([^\]]+)\]\]`)

// headingPattern は、h2 の開始の印。
var headingPattern = regexp.MustCompile(`<h2( |>)`)

// codeSpanPattern は、行の中のコードの部分(`…` と “…“)。
var codeSpanPattern = regexp.MustCompile("``.+?``|`[^`]+`")

// strongPattern は、**強調**。
var strongPattern = regexp.MustCompile(`\*\*([^*]+?)\*\*`)

// convertInline は、1 行の中の **強調** と [[ボタン・状態]] を、HTML の印にする。コードの部分は、そのまま。
// CommonMark の強調は、「)」の直後に日本語が続くと効かない。HTML にしておけば、確実に効く。
func convertInline(line string) string {
	positions := codeSpanPattern.FindAllStringIndex(line, -1)
	var result strings.Builder
	last := 0
	convert := func(text string) string {
		text = strongPattern.ReplaceAllString(text, "<strong>$1</strong>")
		return replaceUI(text)
	}
	for _, position := range positions {
		result.WriteString(convert(line[last:position[0]]))
		result.WriteString(line[position[0]:position[1]])
		last = position[1]
	}
	result.WriteString(convert(line[last:]))
	return result.String()
}

// imagePattern は、1 つの画像だけの段落(`![説明](assets/images/x.png)` を、Markdown が変換したもの)。
var imagePattern = regexp.MustCompile(`<p><img src="([^"]+)" alt="([^"]*)"\s*/?></p>`)

// decorate は、HTML に、このサイトの見た目のための印を足す: 表を囲み、h2 に sec を付ける。
func decorate(output string) string {
	output = strings.ReplaceAll(output, "<table>", "<div class=\"table-wrap\"><table>")
	output = strings.ReplaceAll(output, "</table>", "</table></div>")
	output = headingPattern.ReplaceAllString(output, "<h2 class=\"sec\"$1")
	// 画像だけの段落は、説明(alt)を図の下の説明にした、figure にする。
	output = imagePattern.ReplaceAllString(output, "<figure><img src=\"$1\" alt=\"$2\" loading=\"lazy\"><figcaption>$2</figcaption></figure>")
	return output
}

// replaceUI は、[[…]] を、ボタンの名前(span.ui)か、状態の札(span.badge)にする。
func replaceUI(text string) string {
	return uiPattern.ReplaceAllStringFunc(text, func(found string) string {
		match := uiPattern.FindStringSubmatch(found)
		if match[1] == "" {
			return "<span class=\"ui\">" + match[2] + "</span>"
		}
		names := map[string]string{"ok": "b-ok", "run": "b-run", "bad": "b-bad", "warn": "b-warn", "mut": "b-mut"}
		return "<span class=\"badge " + names[match[1]] + "\">" + match[2] + "</span>"
	})
}
