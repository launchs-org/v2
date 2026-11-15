package password

import (
	_ "embed"
	"strings"
	"unicode/utf8"

	"launchs/shared/apperror"
)

const (
	// MinLength は、パスワードの最小の長さ(文字数)。
	MinLength = 12

	// MaxLength は、パスワードの最大の長さ(バイト数)。argon2id に、巨大な入力を渡させないための上限。
	MaxLength = 256
)

// commonPasswordsText は、よくあるパスワードの一覧(1行に1つ、小文字)。バイナリに埋め込む。
//
//go:embed common_passwords.txt
var commonPasswordsText string

// commonPasswords は、よくあるパスワードの集合。
var commonPasswords = loadCommonPasswords()

// loadCommonPasswords は、埋め込んだ一覧を、集合にする。
//
// 戻り値:
//   - よくあるパスワードの集合。
func loadCommonPasswords() map[string]bool {
	// 行ごとに、空でないものを集める。
	passwords := map[string]bool{}
	for _, line := range strings.Split(commonPasswordsText, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			passwords[trimmed] = true
		}
	}
	return passwords
}

// Check は、パスワードが、サインアップに使える強さかを確かめる。
//
// 組み合わせの規則(大文字・数字など)は課さない。次を拒否する。
//   - 12文字未満、または 256 バイト超
//   - よくあるパスワード(末尾の数字や記号を除いた形も、照合する)
//   - 同じ文字の繰り返し、短い並びの繰り返し(例: abababababab)、連続した数字や文字だけ(例: 123456789012)
//   - ユーザー名と同じ、またはユーザー名(4文字以上)を含むもの
//
// 引数:
//   - userName: サインアップするユーザー名。
//   - plainPassword: 確かめるパスワード。
//
// 戻り値:
//   - 使えないときは invalid_request(理由を、メッセージに入れる)。
func Check(userName string, plainPassword string) error {
	// 長さを確かめる(文字数で数える)。
	if utf8.RuneCountInString(plainPassword) < MinLength {
		return apperror.New(apperror.KindInvalidRequest, "password は12文字以上でなければなりません")
	}
	if len(plainPassword) > MaxLength {
		return apperror.New(apperror.KindInvalidRequest, "password が長すぎます")
	}
	// よくあるパスワードを拒否する(末尾の数字や記号を除いた形も調べる)。
	lowered := strings.ToLower(plainPassword)
	if commonPasswords[lowered] || commonPasswords[strings.TrimRight(lowered, "0123456789!@#$%^&*._-")] {
		return apperror.New(apperror.KindInvalidRequest, "password がよくあるパスワードです。別のものにしてください")
	}
	// 単純な並びを拒否する。
	if isSimplePattern(lowered) {
		return apperror.New(apperror.KindInvalidRequest, "password が単純な並びです。別のものにしてください")
	}
	// ユーザー名を含むものを拒否する(短いユーザー名は、偶然の一致が多いので、4文字以上のときだけ)。
	loweredName := strings.ToLower(userName)
	if loweredName == lowered || (len(loweredName) >= 4 && strings.Contains(lowered, loweredName)) {
		return apperror.New(apperror.KindInvalidRequest, "password にユーザー名を含めないでください")
	}
	return nil
}

// isSimplePattern は、同じ文字の繰り返し、短い並びの繰り返し、連続した文字だけの並び、のどれかかを返す。
//
// 引数:
//   - text: 小文字にしたパスワード。
//
// 戻り値:
//   - 単純な並びなら true。
func isSimplePattern(text string) bool {
	// 文字(ルーン)の並びにして調べる。
	runes := []rune(text)
	// 周期が1〜4の繰り返し(全体が、その並びの繰り返し)を調べる。
	for period := 1; period <= 4 && period < len(runes); period++ {
		repeating := true
		for index := period; index < len(runes); index++ {
			if runes[index] != runes[index-period] {
				repeating = false
				break
			}
		}
		if repeating {
			return true
		}
	}
	// 1つずつ増える、または減る並び(123456789012、abcdefghijkl など)を調べる。
	for _, step := range []rune{1, -1} {
		sequential := true
		for index := 1; index < len(runes); index++ {
			if runes[index]-runes[index-1] != step {
				sequential = false
				break
			}
		}
		if sequential {
			return true
		}
	}
	return false
}
