// Package password は、パスワードのハッシュ(argon2id)と、パスワードの強さの検査を行う。
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	// saltLength は、ハッシュごとの、ランダムなソルトの長さ(バイト)。
	saltLength = 16

	// keyLength は、ハッシュの長さ(バイト)。
	keyLength = 32
)

// Parameters は、argon2id のパラメータ。ハッシュの文字列に入るので、あとから強くしても、古いハッシュを検証できる。
type Parameters struct {
	// MemoryKiB は、使うメモリ(KiB)。
	MemoryKiB uint32

	// Iterations は、反復の回数。
	Iterations uint32

	// Parallelism は、並列度。
	Parallelism uint8
}

// DefaultParameters は、本番で使うパラメータ(メモリ 64MiB、反復 3、並列 2。OWASP の推奨の範囲)。
//
// 戻り値:
//   - 既定のパラメータ。
func DefaultParameters() Parameters {
	// OWASP の推奨(m=64MiB、t=3、p=2)。
	return Parameters{MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 2}
}

// Hasher は、パスワードのハッシュを作り、検証する。
type Hasher struct {
	// parameters は、新しく作るハッシュのパラメータ。
	parameters Parameters
}

// NewHasher は、Hasher を作る。
//
// 引数:
//   - parameters: 新しく作るハッシュのパラメータ。
//
// 戻り値:
//   - 作成した Hasher。
func NewHasher(parameters Parameters) *Hasher {
	// パラメータを持たせて返す。
	return &Hasher{parameters: parameters}
}

// Hash は、パスワードのハッシュを作る(ソルトは、毎回ランダム)。
//
// 引数:
//   - plainPassword: パスワード。
//
// 戻り値:
//   - "$argon2id$v=19$m=...,t=...,p=...$<ソルト>$<ハッシュ>" の形の文字列。乱数を得られなければエラー。
func (hasher *Hasher) Hash(plainPassword string) (string, error) {
	// ランダムなソルトを作る。
	salt := make([]byte, saltLength)
	if _, readError := rand.Read(salt); readError != nil {
		return "", fmt.Errorf("ソルトを作れませんでした: %w", readError)
	}
	// ハッシュを計算して、パラメータごと文字列にする。
	key := argon2.IDKey([]byte(plainPassword), salt, hasher.parameters.Iterations, hasher.parameters.MemoryKiB, hasher.parameters.Parallelism, keyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		hasher.parameters.MemoryKiB, hasher.parameters.Iterations, hasher.parameters.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify は、パスワードが、ハッシュと一致するかを確かめる(時間が、一致の有無で変わらないよう、定数時間で比べる)。
//
// 引数:
//   - plainPassword: 確かめるパスワード。
//   - encodedHash: Hash が作った文字列。
//
// 戻り値:
//   - 一致するなら true。
//   - 一致するとき、ハッシュのパラメータが、今の設定と違うなら true(ハッシュを作り直すべき)。
//   - ハッシュの文字列を読めないときは、エラー。
func (hasher *Hasher) Verify(plainPassword string, encodedHash string) (bool, bool, error) {
	// ハッシュの文字列を、パラメータ・ソルト・ハッシュに分ける。
	parameters, salt, expectedKey, parseError := decode(encodedHash)
	if parseError != nil {
		return false, false, parseError
	}
	// 同じパラメータで計算して、定数時間で比べる。
	actualKey := argon2.IDKey([]byte(plainPassword), salt, parameters.Iterations, parameters.MemoryKiB, parameters.Parallelism, uint32(len(expectedKey)))
	if subtle.ConstantTimeCompare(actualKey, expectedKey) != 1 {
		return false, false, nil
	}
	// 一致した。パラメータが、今の設定と違うときは、作り直すべきと伝える。
	return true, parameters != hasher.parameters, nil
}

// decode は、ハッシュの文字列を、パラメータ・ソルト・ハッシュに分ける。
//
// 引数:
//   - encodedHash: Hash が作った文字列。
//
// 戻り値:
//   - パラメータ、ソルト、ハッシュ。形が違うときはエラー。
func decode(encodedHash string) (Parameters, []byte, []byte, error) {
	// "$" で分ける(先頭が空なので、6つになる)。
	fields := strings.Split(encodedHash, "$")
	if len(fields) != 6 || fields[1] != "argon2id" {
		return Parameters{}, nil, nil, errors.New("ハッシュの形が正しくありません")
	}
	// バージョンを確かめる。
	var version int
	if _, scanError := fmt.Sscanf(fields[2], "v=%d", &version); scanError != nil || version != argon2.Version {
		return Parameters{}, nil, nil, errors.New("ハッシュのバージョンが正しくありません")
	}
	// パラメータを読む。
	var parameters Parameters
	if _, scanError := fmt.Sscanf(fields[3], "m=%d,t=%d,p=%d", &parameters.MemoryKiB, &parameters.Iterations, &parameters.Parallelism); scanError != nil {
		return Parameters{}, nil, nil, errors.New("ハッシュのパラメータが正しくありません")
	}
	// 0 のパラメータは、計算できない(攻撃や破損)ので、拒否する。
	if parameters.MemoryKiB == 0 || parameters.Iterations == 0 || parameters.Parallelism == 0 {
		return Parameters{}, nil, nil, errors.New("ハッシュのパラメータが正しくありません")
	}
	// ソルトとハッシュを読む。
	salt, saltError := base64.RawStdEncoding.DecodeString(fields[4])
	key, keyError := base64.RawStdEncoding.DecodeString(fields[5])
	if saltError != nil || keyError != nil || len(key) == 0 {
		return Parameters{}, nil, nil, errors.New("ハッシュの内容が正しくありません")
	}
	return parameters, salt, key, nil
}
