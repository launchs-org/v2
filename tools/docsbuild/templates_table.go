package main

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// templateManifest は、テンプレートの manifest.yaml のうち、一覧に使う項目。
type templateManifest struct {
	// ID はテンプレートの識別子(フォルダ名)。
	ID string `yaml:"id"`
	// Name は画面に出す名前。
	Name string `yaml:"name"`
	// Description は説明。
	Description string `yaml:"description"`
	// Category は分類。
	Category string `yaml:"category"`
	// DefaultVersion は既定の版。
	DefaultVersion string `yaml:"defaultVersion"`
}

// templateVariables は、版の config.yaml のうち、利用者が入れる値の一覧。
type templateVariables struct {
	// Variables は入力の項目。
	Variables []struct {
		// Label は画面に出す名前。
		Label string `yaml:"label"`
	} `yaml:"variables"`
}

// categoryTitles は、分類の名前と、表の順番。
var categoryTitles = []struct{ key, title string }{
	{"web", "ウェブ"}, {"database", "データベース"}, {"cache", "キャッシュ"}, {"dev", "開発"}, {"monitoring", "監視・画面"}, {"tool", "個人ツール"},
}

// buildTemplateHTML は、テンプレートのフォルダを読んで、分類ごとの表(名前、説明、入れる項目)の HTML を作る。
// フォルダが無いときは、空の HTML を返す(原稿だけで試すときのため)。
func buildTemplateHTML(directory string) (string, error) {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return "<p>(テンプレートの一覧は、テンプレートのフォルダがあるときに作られます)</p>", nil
	}
	if err != nil {
		return "", err
	}
	byCategory := map[string][]string{}
	rows := map[string]string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestBytes, err := os.ReadFile(filepath.Join(directory, entry.Name(), "manifest.yaml"))
		if err != nil {
			continue
		}
		var manifest templateManifest
		if err := yaml.Unmarshal(manifestBytes, &manifest); err != nil {
			return "", fmt.Errorf("%s/manifest.yaml: %w", entry.Name(), err)
		}
		labels := variableLabels(filepath.Join(directory, entry.Name()), manifest.DefaultVersion)
		inputs := "なし(すぐ使えます)"
		if len(labels) > 0 {
			inputs = html.EscapeString(strings.Join(labels, "、"))
		}
		rows[manifest.ID] = "<tr><td><b>" + html.EscapeString(manifest.Name) + "</b></td><td>" + html.EscapeString(manifest.Description) + "</td><td>" + inputs + "</td></tr>\n"
		byCategory[manifest.Category] = append(byCategory[manifest.Category], manifest.ID)
	}
	var buffer strings.Builder
	for _, category := range categoryTitles {
		ids := byCategory[category.key]
		if len(ids) == 0 {
			continue
		}
		sort.Strings(ids)
		buffer.WriteString("<h3>" + category.title + "</h3>\n<div class=\"table-wrap\"><table>\n<tr><th>テンプレート</th><th>何ができる?</th><th>入れる項目</th></tr>\n")
		for _, id := range ids {
			buffer.WriteString(rows[id])
		}
		buffer.WriteString("</table></div>\n")
	}
	return buffer.String(), nil
}

// variableLabels は、テンプレートの既定の版の、入力の項目の名前を返す(版が分からなければ、最初の版)。
func variableLabels(directory, version string) []string {
	if version == "" {
		entries, _ := os.ReadDir(directory)
		for _, entry := range entries {
			if entry.IsDir() {
				version = entry.Name()
			}
		}
	}
	content, err := os.ReadFile(filepath.Join(directory, version, "config.yaml"))
	if err != nil {
		return nil
	}
	var config templateVariables
	if err := yaml.Unmarshal(content, &config); err != nil {
		return nil
	}
	var labels []string
	for _, variable := range config.Variables {
		labels = append(labels, variable.Label)
	}
	return labels
}
