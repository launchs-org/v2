package model

import (
	"encoding/base64"
	"strings"
	"testing"

	"launchs/shared/apperror"
)

// TestValidateConfigMapFiles は、ConfigMap のファイルの検査(個数、key、encoding、base64、大きさ、重複、並び)を確認する。
func TestValidateConfigMapFiles(testingT *testing.T) {
	// 大きさの上限ちょうどのファイルを作る(key の長さも、保存する大きさに入る)。
	exactContent := strings.Repeat("a", MaxConfigMapStoredBytes-len("big.txt"))
	// 51 個のファイルを作る(個数の上限を超える)。
	tooManyFiles := []ConfigMapFile{}
	for index := 0; index < MaxConfigMapFiles+1; index++ {
		tooManyFiles = append(tooManyFiles, ConfigMapFile{Key: "file" + itoa(index), Encoding: ConfigMapEncodingText, Content: "x"})
	}
	// NUL 文字を含むバイナリを、base64 にする。
	binaryContent := base64.StdEncoding.EncodeToString([]byte{0x00, 0xff, 0x10})

	for _, testCase := range []struct {
		name      string
		files     []ConfigMapFile
		wantError bool
	}{
		{"空の組は許す", []ConfigMapFile{}, false},
		{"text のファイルは許す", []ConfigMapFile{{Key: "app.conf", Encoding: "text", Content: "a=b\n"}}, false},
		{"内容が空でも許す", []ConfigMapFile{{Key: "empty", Encoding: "text", Content: ""}}, false},
		{"NUL を含むバイナリは base64 なら許す", []ConfigMapFile{{Key: "blob.bin", Encoding: "base64", Content: binaryContent}}, false},
		{"text に NUL を含むと不可", []ConfigMapFile{{Key: "a", Encoding: "text", Content: "a\x00b"}}, true},
		{"text が UTF-8 でないと不可", []ConfigMapFile{{Key: "a", Encoding: "text", Content: "\xff\xfe"}}, true},
		{"base64 でない文字列は不可", []ConfigMapFile{{Key: "a", Encoding: "base64", Content: "not base64!"}}, true},
		{"base64 にパディングが無いと不可", []ConfigMapFile{{Key: "a", Encoding: "base64", Content: "AA"}}, true},
		{"base64 に改行があると不可", []ConfigMapFile{{Key: "a", Encoding: "base64", Content: "AAEC\nAw=="}}, true},
		{"知らない encoding は不可", []ConfigMapFile{{Key: "a", Encoding: "hex", Content: "00"}}, true},
		{"encoding が空は不可(controller が text にそろえる)", []ConfigMapFile{{Key: "a", Encoding: "", Content: "x"}}, true},
		{"key に / があると不可", []ConfigMapFile{{Key: "conf.d/a.conf", Encoding: "text", Content: "x"}}, true},
		{"key が . は不可", []ConfigMapFile{{Key: ".", Encoding: "text", Content: "x"}}, true},
		{"key が .. は不可", []ConfigMapFile{{Key: "..", Encoding: "text", Content: "x"}}, true},
		{"key が空は不可", []ConfigMapFile{{Key: "", Encoding: "text", Content: "x"}}, true},
		{"key が 253 文字なら許す", []ConfigMapFile{{Key: strings.Repeat("a", 253), Encoding: "text", Content: "x"}}, false},
		{"key が 254 文字なら不可", []ConfigMapFile{{Key: strings.Repeat("a", 254), Encoding: "text", Content: "x"}}, true},
		{"key の先頭が . でも許す(.env など)", []ConfigMapFile{{Key: ".env", Encoding: "text", Content: "x"}}, false},
		{"key が重複すると不可", []ConfigMapFile{{Key: "a", Encoding: "text", Content: "1"}, {Key: "a", Encoding: "text", Content: "2"}}, true},
		{"50 個なら許す", tooManyFiles[:MaxConfigMapFiles], false},
		{"51 個は不可", tooManyFiles, true},
		{"保存する大きさが上限ちょうどなら許す", []ConfigMapFile{{Key: "big.txt", Encoding: "text", Content: exactContent}}, false},
		{"保存する大きさが上限を 1 バイト超えると不可", []ConfigMapFile{{Key: "big.txt", Encoding: "text", Content: exactContent + "a"}}, true},
		{"合計が上限を超えると不可(ファイル1つは上限以内)", []ConfigMapFile{
			{Key: "a", Encoding: "text", Content: strings.Repeat("a", MaxConfigMapStoredBytes/2)},
			{Key: "b", Encoding: "text", Content: strings.Repeat("b", MaxConfigMapStoredBytes/2)},
		}, true},
		{"base64 は、base64 にした後の大きさで数える", []ConfigMapFile{{Key: "a", Encoding: "base64", Content: base64.StdEncoding.EncodeToString(make([]byte, 800*1024))}}, true},
	} {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			_, validateError := ValidateConfigMapFiles(testCase.files)
			if testCase.wantError {
				if validateError == nil || apperror.KindOf(validateError) != apperror.KindInvalidRequest {
					subTest.Errorf("invalid_request のはず: %v", validateError)
				}
				return
			}
			if validateError != nil {
				subTest.Errorf("許されるはず: %v", validateError)
			}
		})
	}

	testingT.Run("key の昇順に並べて返し、引数の順番は変えない", func(subTest *testing.T) {
		input := []ConfigMapFile{{Key: "b", Encoding: "text", Content: "2"}, {Key: "a", Encoding: "text", Content: "1"}}
		sorted, validateError := ValidateConfigMapFiles(input)
		if validateError != nil || sorted[0].Key != "a" || sorted[1].Key != "b" || input[0].Key != "b" {
			subTest.Errorf("昇順になり、引数は変わらないはず: %v %v %v", sorted, input, validateError)
		}
	})
}

// TestSameConfigMapFiles は、中身が同じかどうかの比較を確認する(版を増やすかどうかの判断に使う)。
func TestSameConfigMapFiles(testingT *testing.T) {
	base := []ConfigMapFile{{Key: "a", Encoding: "text", Content: "1"}}
	for _, testCase := range []struct {
		name  string
		other []ConfigMapFile
		want  bool
	}{
		{"全て同じなら同じ", []ConfigMapFile{{Key: "a", Encoding: "text", Content: "1"}}, true},
		{"内容が違えば違う", []ConfigMapFile{{Key: "a", Encoding: "text", Content: "2"}}, false},
		{"key が違えば違う", []ConfigMapFile{{Key: "b", Encoding: "text", Content: "1"}}, false},
		{"encoding が違えば違う", []ConfigMapFile{{Key: "a", Encoding: "base64", Content: "1"}}, false},
		{"個数が違えば違う", []ConfigMapFile{}, false},
	} {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			if got := SameConfigMapFiles(base, testCase.other); got != testCase.want {
				subTest.Errorf("期待 %v、実際 %v", testCase.want, got)
			}
		})
	}
}

// TestConfigMapFileDecodedBytes は、画面に出す、元のバイト数を確認する。
func TestConfigMapFileDecodedBytes(testingT *testing.T) {
	testingT.Run("text は、そのままのバイト数", func(subTest *testing.T) {
		if got := (ConfigMapFile{Encoding: "text", Content: "あい"}).DecodedBytes(); got != 6 {
			subTest.Errorf("UTF-8 で 6 バイトのはず: %d", got)
		}
	})
	testingT.Run("base64 は、デコードしたバイト数", func(subTest *testing.T) {
		if got := (ConfigMapFile{Encoding: "base64", Content: base64.StdEncoding.EncodeToString(make([]byte, 10))}).DecodedBytes(); got != 10 {
			subTest.Errorf("10 バイトのはず: %d", got)
		}
	})
}
