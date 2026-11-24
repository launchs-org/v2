// docsbuild は、利用者向けのドキュメント(docs/ の Markdown)から、GitHub Pages 用の静的なサイトを作る。
//
// 使い方: docsbuild -src docs -templates templates/templates -out _site
// 検査(リンク切れ、ナビに無いページ、閉じていないブロック)に失敗したら、終了コード 1 で終わる。
package main

import (
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// siteName は、ブラウザのタブに出す、サイトの名前。
const siteName = "launchs 利用ガイド"

// navFile は、ナビ(サイドバー)の定義(docs/nav.yaml)。
type navFile struct {
	// Groups は、サイドバーの見出しと、その下のページ。
	Groups []navGroup `yaml:"groups"`
}

// navGroup は、サイドバーの 1 つのまとまり。
type navGroup struct {
	// Title は見出し。
	Title string `yaml:"title"`
	// Pages は、ページの名前(拡張子なしのファイル名)の並び。
	Pages []string `yaml:"pages"`
}

// pageMeta は、各ページの先頭(front matter)に書く情報。
type pageMeta struct {
	// Title はページの題(h1 とタブ)。
	Title string `yaml:"title"`
	// Nav はサイドバーでの名前(省略で Title)。
	Nav string `yaml:"nav"`
	// Lead は題の下の、ひとこと。
	Lead string `yaml:"lead"`
	// Crumb はパンくずに出す、分類の名前。
	Crumb string `yaml:"crumb"`
	// Home は、ホームのページかどうか(題を「ようこそ」の見た目にしない場合は不要)。
	Home bool `yaml:"home"`
}

// page は、読み込んだページ 1 つ。
type page struct {
	// Name は拡張子なしのファイル名(例: start)。
	Name string
	// Meta は front matter。
	Meta pageMeta
	// Body は Markdown の本文。
	Body string
}

func main() {
	src := flag.String("src", "docs", "Markdown の原稿のフォルダ")
	templates := flag.String("templates", "templates/templates", "テンプレートのフォルダ(一覧を作るために読む。無ければ空)")
	out := flag.String("out", "_site", "出力するフォルダ")
	flag.Parse()
	if err := build(*src, *templates, *out); err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
}

// build は、原稿を読み、ページを作り、検査して、出力する。
func build(src, templatesDir, out string) error {
	// ナビを読む。
	var nav navFile
	navBytes, err := os.ReadFile(filepath.Join(src, "nav.yaml"))
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(navBytes, &nav); err != nil {
		return fmt.Errorf("nav.yaml: %w", err)
	}
	// ページを読む。
	pages, err := loadPages(src)
	if err != nil {
		return err
	}
	// ナビと、ページの食い違いを検査する。
	order, err := checkNav(nav, pages)
	if err != nil {
		return err
	}
	// テンプレートの一覧(HTML)を作る。
	templateHTML, err := buildTemplateHTML(templatesDir)
	if err != nil {
		return err
	}
	// 出力のフォルダを作り直す。
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	rendered := map[string]string{}
	for _, name := range order {
		current := pages[name]
		body, err := renderBody(current.Body, templateHTML)
		if err != nil {
			return fmt.Errorf("%s.md: %w", name, err)
		}
		body = rewriteLinks(body)
		rendered[name] = body
	}
	// リンクの検査(出力の前に、全部のページを見る)。
	if err := checkLinks(rendered); err != nil {
		return err
	}
	if err := checkImages(src, rendered); err != nil {
		return err
	}
	for _, name := range order {
		document := renderPage(nav, pages, order, name, rendered[name])
		if err := os.WriteFile(filepath.Join(out, name+".html"), []byte(document), 0o644); err != nil {
			return err
		}
	}
	// 見た目のファイルと、Jekyll を使わない印を置く。
	if err := copyDirectory(filepath.Join(src, "assets"), filepath.Join(out, "assets")); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, ".nojekyll"), nil, 0o644); err != nil {
		return err
	}
	fmt.Printf("%d ページを %s に作りました\n", len(order), out)
	return nil
}

// loadPages は、src の直下の *.md を、全部読む。
func loadPages(src string) (map[string]page, error) {
	files, err := filepath.Glob(filepath.Join(src, "*.md"))
	if err != nil {
		return nil, err
	}
	pages := map[string]page{}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(filepath.Base(file), ".md")
		// README.md は、GitHub で見たときの入口(目次)であり、サイトでは、ホーム(index.html)になる。
		if name == "README" {
			name = "index"
		}
		text := string(content)
		// front matter(先頭の --- と、次の --- の間)を取り出す。
		if !strings.HasPrefix(text, "---\n") {
			return nil, fmt.Errorf("%s: 先頭に front matter(---)がありません", file)
		}
		end := strings.Index(text[4:], "\n---\n")
		if end < 0 {
			return nil, fmt.Errorf("%s: front matter が閉じていません", file)
		}
		var meta pageMeta
		if err := yaml.Unmarshal([]byte(text[4:4+end]), &meta); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		if meta.Title == "" {
			return nil, fmt.Errorf("%s: title がありません", file)
		}
		pages[name] = page{Name: name, Meta: meta, Body: text[4+end+5:]}
	}
	return pages, nil
}

// checkNav は、ナビの中のページが全部あり、ページが全部ナビにあることを確かめる。
// 戻り値は、ナビの順のページ名(重複なし)。
func checkNav(nav navFile, pages map[string]page) ([]string, error) {
	var order []string
	seen := map[string]bool{}
	for _, group := range nav.Groups {
		for _, name := range group.Pages {
			if _, ok := pages[name]; !ok {
				return nil, fmt.Errorf("nav.yaml のページ %q の原稿(%s.md)がありません", name, name)
			}
			if seen[name] {
				return nil, fmt.Errorf("nav.yaml に %q が 2 回あります", name)
			}
			seen[name] = true
			order = append(order, name)
		}
	}
	var missing []string
	for name := range pages {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return nil, fmt.Errorf("nav.yaml に無いページがあります: %s", strings.Join(missing, ", "))
	}
	return order, nil
}

// markdownLinkPattern は、HTML の中の、`.md` へのリンク。
var markdownLinkPattern = regexp.MustCompile(`href="([A-Za-z0-9_-]+)\.md(#[^"]*)?"`)

// rewriteLinks は、ページ同士のリンクを、`.md` から `.html` に直す。
func rewriteLinks(body string) string {
	return markdownLinkPattern.ReplaceAllString(body, `href="$1.html$2"`)
}

// internalLinkPattern は、サイトの中のリンク(`xxx.html` と `xxx.html#id`)。
var internalLinkPattern = regexp.MustCompile(`href="([A-Za-z0-9_-]+)\.html(?:#([^"]*))?"`)

// idPattern は、HTML の中の id の指定。
var idPattern = regexp.MustCompile(`id="([^"]+)"`)

// checkLinks は、ページ同士のリンクと、ページの中の見出しへのリンクが、切れていないかを確かめる。
func checkLinks(rendered map[string]string) error {
	ids := map[string]map[string]bool{}
	for name, body := range rendered {
		ids[name] = map[string]bool{}
		for _, match := range idPattern.FindAllStringSubmatch(body, -1) {
			ids[name][match[1]] = true
		}
	}
	var problems []string
	for name, body := range rendered {
		for _, match := range internalLinkPattern.FindAllStringSubmatch(body, -1) {
			target := match[1]
			if _, ok := rendered[target]; !ok {
				problems = append(problems, fmt.Sprintf("%s.md: リンク先 %s.html がありません", name, target))
				continue
			}
			if match[2] != "" && !ids[target][match[2]] {
				problems = append(problems, fmt.Sprintf("%s.md: リンク先 %s.html に #%s がありません", name, target, match[2]))
			}
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return fmt.Errorf("リンク切れ:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

// imageSourcePattern は、本文の中の、assets 以下の画像への参照。
var imageSourcePattern = regexp.MustCompile(`src="(assets/[^"]+)"`)

// checkImages は、本文が参照している画像のファイルが、src の下にあることを確かめる。
func checkImages(src string, rendered map[string]string) error {
	var problems []string
	for name, body := range rendered {
		for _, match := range imageSourcePattern.FindAllStringSubmatch(body, -1) {
			if _, err := os.Stat(filepath.Join(src, match[1])); err != nil {
				problems = append(problems, fmt.Sprintf("%s.md: 画像 %s がありません", name, match[1]))
			}
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return fmt.Errorf("画像が見つかりません:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

// renderPage は、ページ 1 つの HTML 全体(サイドバー、パンくず、題、本文、前後の案内)を作る。
func renderPage(nav navFile, pages map[string]page, order []string, name string, body string) string {
	current := pages[name]
	var buffer strings.Builder
	title := current.Meta.Title + " | " + siteName
	if current.Meta.Home {
		title = siteName
	}
	buffer.WriteString("<!doctype html>\n<html lang=\"ja\">\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	buffer.WriteString("<title>" + html.EscapeString(title) + "</title>\n")
	if current.Meta.Lead != "" {
		buffer.WriteString("<meta name=\"description\" content=\"" + html.EscapeString(current.Meta.Lead) + "\">\n")
	}
	buffer.WriteString("<link rel=\"stylesheet\" href=\"assets/style.css\">\n</head>\n<body>\n")
	// 狭い画面で、ナビを開け閉めするための、チェックボックス(CSS だけで動く)。
	buffer.WriteString("<input type=\"checkbox\" id=\"menu-toggle\" aria-label=\"メニュー\">\n")
	buffer.WriteString("<header class=\"topbar\"><label class=\"menu-button\" for=\"menu-toggle\" aria-label=\"メニューを開く\"><svg width=\"24\" height=\"24\" viewBox=\"0 0 24 24\" fill=\"currentColor\"><path d=\"M3 18h18v-2H3v2zm0-5h18v-2H3v2zm0-7v2h18V6H3z\"/></svg></label><a class=\"brand\" href=\"index.html\">launchs</a><span class=\"section\">ドキュメント</span></header>\n<div class=\"layout\">\n")
	buffer.WriteString(renderSidebar(nav, pages, name))
	buffer.WriteString("\n<main>\n")
	if !current.Meta.Home {
		crumb := current.Meta.Crumb
		buffer.WriteString("<div class=\"crumb\"><a href=\"index.html\">ホーム</a> / " + html.EscapeString(crumb) + "</div>\n")
		buffer.WriteString("<h1>" + html.EscapeString(current.Meta.Title) + "</h1>")
		if current.Meta.Lead != "" {
			buffer.WriteString("<p class=\"lead\">" + html.EscapeString(current.Meta.Lead) + "</p>")
		}
		buffer.WriteString("\n")
	}
	buffer.WriteString(body)
	if !current.Meta.Home {
		buffer.WriteString(renderPager(pages, order, name))
	}
	buffer.WriteString("\n<footer>このドキュメントは、launchs の画面の言葉に合わせて書いています。画面が変わったときは、ここも更新します。</footer>\n</main>\n")
	buffer.WriteString(renderTOC(body))
	buffer.WriteString("\n</div>\n</body>\n</html>\n")
	return buffer.String()
}

// headingLinkPattern は、本文の中の、h2 と h3(id つき)。右の目次に使う。
var headingLinkPattern = regexp.MustCompile(`(?s)<h([23])[^>]*id="([^"]+)"[^>]*>(.*?)</h[23]>`)

// tagPattern は、HTML のタグ(目次の見出しの文字から、タグを除くため)。
var tagPattern = regexp.MustCompile(`<[^>]+>`)

// renderTOC は、ページの右に出す「このページの内容」(h2 と h3 へのリンク)を作る。見出しが 2 つ未満なら、出さない。
func renderTOC(body string) string {
	matches := headingLinkPattern.FindAllStringSubmatch(body, -1)
	if len(matches) < 2 {
		return "<nav class=\"toc\"></nav>"
	}
	var buffer strings.Builder
	buffer.WriteString("<nav class=\"toc\" aria-label=\"このページの内容\"><strong>このページの内容</strong>")
	for _, match := range matches {
		class := ""
		if match[1] == "3" {
			class = " class=\"h3\""
		}
		buffer.WriteString("<a" + class + " href=\"#" + match[2] + "\">" + tagPattern.ReplaceAllString(match[3], "") + "</a>")
	}
	buffer.WriteString("</nav>")
	return buffer.String()
}

// navName は、サイドバーに出すページの名前。
func navName(current page) string {
	if current.Meta.Nav != "" {
		return current.Meta.Nav
	}
	return current.Meta.Title
}

// renderSidebar は、サイドバー(ナビ)の HTML を作る。現在のページには aria-current を付ける。
func renderSidebar(nav navFile, pages map[string]page, name string) string {
	var buffer strings.Builder
	buffer.WriteString("<aside class=\"side\">\n")
	for _, group := range nav.Groups {
		buffer.WriteString("<h2>" + html.EscapeString(group.Title) + "</h2>\n<div class=\"group\">\n")
		for _, pageName := range group.Pages {
			current := ""
			if pageName == name {
				current = " aria-current=\"page\""
			}
			buffer.WriteString("<a class=\"nav\" href=\"" + pageName + ".html\"" + current + ">" + html.EscapeString(navName(pages[pageName])) + "</a>\n")
		}
		buffer.WriteString("</div>\n")
	}
	buffer.WriteString("</aside>")
	return buffer.String()
}

// renderPager は、ページの下の「前へ / 次へ」を作る(ホームは順番に入れない)。
func renderPager(pages map[string]page, order []string, name string) string {
	var flat []string
	for _, pageName := range order {
		if !pages[pageName].Meta.Home {
			flat = append(flat, pageName)
		}
	}
	index := -1
	for position, pageName := range flat {
		if pageName == name {
			index = position
		}
	}
	if index < 0 {
		return ""
	}
	var buffer strings.Builder
	buffer.WriteString("\n<div class=\"pager\">")
	if index > 0 {
		previous := flat[index-1]
		buffer.WriteString("<a href=\"" + previous + ".html\"><small>← 前へ</small>" + html.EscapeString(navName(pages[previous])) + "</a>")
	} else {
		buffer.WriteString("<span></span>")
	}
	if index+1 < len(flat) {
		next := flat[index+1]
		buffer.WriteString("<a class=\"next\" href=\"" + next + ".html\"><small>次へ →</small>" + html.EscapeString(navName(pages[next])) + "</a>")
	}
	buffer.WriteString("</div>")
	return buffer.String()
}

// copyDirectory は、フォルダの中身(下のフォルダも)を、出力のフォルダへ写す(フォルダが無ければ何もしない)。
func copyDirectory(from, to string) error {
	entries, err := os.ReadDir(from)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if err := copyDirectory(filepath.Join(from, entry.Name()), filepath.Join(to, entry.Name())); err != nil {
				return err
			}
			continue
		}
		content, err := os.ReadFile(filepath.Join(from, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(to, entry.Name()), content, 0o644); err != nil {
			return err
		}
	}
	return nil
}
