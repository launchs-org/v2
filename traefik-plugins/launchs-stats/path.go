package launchs_stats

import "strings"

// maxPathLength は、集計するパスの長さの上限。
const maxPathLength = 200

// normalizePath は、集計用にパスをそろえる。クエリは落とし、数字・UUID・長い 16 進の区間は「:id」にする。
// 長すぎるパスは「(other)」にする(キーの種類が増え続けないようにするため)。
func normalizePath(rawPath string) string {
	// 空なら「/」。
	if rawPath == "" {
		return "/"
	}
	// クエリを落とす。
	if index := strings.IndexByte(rawPath, '?'); index >= 0 {
		rawPath = rawPath[:index]
	}
	// 長すぎれば、まとめる。
	if len(rawPath) > maxPathLength {
		return "(other)"
	}
	// 区間ごとに見て、ID らしいものを置き換える。
	segments := strings.Split(rawPath, "/")
	for index, segment := range segments {
		if looksLikeID(segment) {
			segments[index] = ":id"
		}
	}
	return strings.Join(segments, "/")
}

// looksLikeID は、区間が、ID のように見えるか(数字だけ、UUID、16 文字以上の 16 進)を返す。
func looksLikeID(segment string) bool {
	// 空は違う。
	if segment == "" {
		return false
	}
	// 数字だけ。
	allDigits := true
	for _, character := range segment {
		if character < '0' || character > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return true
	}
	// UUID(8-4-4-4-12 の 16 進)。
	if len(segment) == 36 && segment[8] == '-' && segment[13] == '-' && segment[18] == '-' && segment[23] == '-' {
		return isHex(strings.ReplaceAll(segment, "-", ""))
	}
	// 16 文字以上の 16 進。
	return len(segment) >= 16 && isHex(segment)
}

// isHex は、全て 16 進の文字かを返す。
func isHex(text string) bool {
	// 1 文字ずつ確かめる。
	for _, character := range text {
		isDigit := character >= '0' && character <= '9'
		isLower := character >= 'a' && character <= 'f'
		isUpper := character >= 'A' && character <= 'F'
		if !isDigit && !isLower && !isUpper {
			return false
		}
	}
	return text != ""
}
