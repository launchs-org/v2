package repository

import "launchs/shared/apperror"

// hasEmpty は、渡された値のうち、空文字のものが1つでもあるかを返す。
//
// Gorm の構造体による条件(Where(&model.Project{ID: id}))は、空の値のフィールドを、条件に含めない。
// そのため、空の ID を渡すと、条件が消えて、他の行(最悪の場合、全ての行)が対象になってしまう。
// これを防ぐため、条件に使う値は、使う前に、この関数で空でないことを確かめる。
//
// 引数:
//   - values: 条件に使う値。
//
// 戻り値:
//   - 空文字が1つでもあれば true。
func hasEmpty(values ...string) bool {
	// 1つでも空文字があれば、true にする。
	for _, value := range values {
		if value == "" {
			return true
		}
	}
	return false
}

// newEmptyConditionError は、条件の値が空だったときの、internal_error を作る。
//
// 空の値は、呼び出し側の実装の誤りなので、隠さずに、エラーとして返す。
//
// 戻り値:
//   - internal_error のエラー。
func newEmptyConditionError() error {
	// 利用者向けのメッセージには、内部の事情を出さない(共通のメッセージになる)。
	return apperror.New(apperror.KindInternal, "検索条件に空の値が渡されました")
}
