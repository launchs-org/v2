package password

import (
	"strings"
	"testing"

	"launchs/shared/apperror"
)

// lowCostParameters は、テストを速くするための、弱いパラメータ。
var lowCostParameters = Parameters{MemoryKiB: 8, Iterations: 1, Parallelism: 1}

// TestHasher は、ハッシュを作って検証できること(ソルトは毎回違う)、パラメータが古いと作り直しを伝えること、
// 壊れたハッシュを、エラーにすることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestHasher(testing *testing.T) {
	// 弱いパラメータで、ハッシュを作る。
	hasher := NewHasher(lowCostParameters)
	hash, hashError := hasher.Hash("correct horse battery")
	if hashError != nil || !strings.HasPrefix(hash, "$argon2id$v=19$m=8,t=1,p=1$") {
		testing.Fatalf("argon2id の形のはず: %q %v", hash, hashError)
	}
	// 同じパスワードでも、ソルトが違うので、ハッシュは毎回違う。
	other, _ := hasher.Hash("correct horse battery")
	if hash == other {
		testing.Error("ソルトは毎回違うはず")
	}
	// 一致するパスワードは通り、違うパスワードは通らない。
	if matched, needsRehash, verifyError := hasher.Verify("correct horse battery", hash); !matched || needsRehash || verifyError != nil {
		testing.Errorf("一致するはず: %v %v %v", matched, needsRehash, verifyError)
	}
	if matched, _, verifyError := hasher.Verify("wrong password!!", hash); matched || verifyError != nil {
		testing.Errorf("一致しないはず: %v %v", matched, verifyError)
	}
	// パラメータが違う Hasher で検証すると、一致したうえで、作り直しを伝える。
	stronger := NewHasher(Parameters{MemoryKiB: 16, Iterations: 2, Parallelism: 1})
	if matched, needsRehash, _ := stronger.Verify("correct horse battery", hash); !matched || !needsRehash {
		testing.Errorf("一致して、作り直しを伝えるはず: %v %v", matched, needsRehash)
	}
	// 壊れた・危険なハッシュは、エラー(0 のパラメータは、計算できない)。
	for _, broken := range []string{"", "plain", "$argon2id$v=19$m=0,t=1,p=1$AAAA$AAAA", "$argon2i$v=19$m=8,t=1,p=1$AAAA$AAAA", "$argon2id$v=18$m=8,t=1,p=1$AAAA$AAAA", "$argon2id$v=19$m=8,t=1,p=1$!!!$AAAA"} {
		if _, _, verifyError := hasher.Verify("x", broken); verifyError == nil {
			testing.Errorf("%q はエラーのはず", broken)
		}
	}
}

// TestDefaultParameters は、本番のパラメータが、OWASP の推奨(メモリ 64MiB、反復 3、並列 2)であることを確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestDefaultParameters(testing *testing.T) {
	// 推奨の値。
	if DefaultParameters() != (Parameters{MemoryKiB: 65536, Iterations: 3, Parallelism: 2}) {
		testing.Errorf("推奨の値のはず: %+v", DefaultParameters())
	}
}

// TestCheck は、パスワードの強さの検査(長さ、よくあるパスワード、単純な並び、ユーザー名を含む)を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestCheck(testing *testing.T) {
	// 通るパスワード(組み合わせの規則は、課さない。全て小文字でも、スペースを含んでも通る)。
	for _, valid := range []string{"correct horse battery", "tqzvbnmwkelp", "Zx9!Zx9!Zx9!Qw", "あいうえおかきくけこさし"} {
		if checkError := Check("alice", valid); checkError != nil {
			testing.Errorf("%q は通るはず: %v", valid, checkError)
		}
	}
	// 通らないパスワードと、その理由(全て invalid_request)。
	for name, testCase := range map[string]struct{ userName, password string }{
		"11文字":   {"alice", "abcdefghijk"},
		"空":      {"alice", ""},
		"257バイト": {"alice", strings.Repeat("ab", 129)},
		"よくあるパスワード(12文字以上)": {"alice", "administrator"},
		"よくあるパスワード + 末尾の数字": {"alice", "password123456"},
		"よくあるパスワード(大文字)":    {"alice", "PASSWORD1234"},
		"同じ文字の繰り返し":         {"alice", "aaaaaaaaaaaa"},
		"短い並びの繰り返し":         {"alice", "abababababab"},
		"4文字の並びの繰り返し":       {"alice", "xyzwxyzwxyzw"},
		"連続した数字":            {"alice", "123456789012"},
		"連続した文字(逆順)":        {"alice", "zyxwvutsrqpo"},
		"ユーザー名と同じ":          {"correct-horse-battery", "correct-horse-battery"},
		"ユーザー名を含む(4文字以上)":   {"alice", "xx-ALICE-xx-yy"},
	} {
		checkError := Check(testCase.userName, testCase.password)
		var appError *apperror.Error
		if checkError == nil || !asAppError(checkError, &appError) || appError.Kind != apperror.KindInvalidRequest {
			testing.Errorf("%s: invalid_request のはず: %v", name, checkError)
		}
	}
	// 短いユーザー名(3文字以下)は、偶然の一致が多いので、含んでいても通る。
	if checkError := Check("bob", "correct bob battery"); checkError != nil {
		testing.Errorf("短いユーザー名は、含んでいても通るはず: %v", checkError)
	}
}

// asAppError は、エラーを *apperror.Error に変換する。
//
// 引数:
//   - checkedError: 変換するエラー。
//   - target: 結果の入れ物。
//
// 戻り値:
//   - 変換できたら true。
func asAppError(checkedError error, target **apperror.Error) bool {
	// *apperror.Error なら、取り出す。
	appError, isAppError := checkedError.(*apperror.Error)
	*target = appError
	return isAppError
}
