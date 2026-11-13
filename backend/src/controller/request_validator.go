package controller

import (
	"reflect"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v5"

	"launchs/shared/apperror"
)

// resourceNamePattern は、リソースの名前に使える文字(英数字とハイフンのみ)。
var resourceNamePattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// requestValidator は、リクエストのボディの検証に使う validator。
// エラーのメッセージに JSON のキー名(name など)が出るよう、タグ名の取り方を差し替えてある。
var requestValidator = newRequestValidator()

// newRequestValidator は、独自の規則(resourcename)を登録した validator を作る。
//
// 登録する規則:
//   - resourcename: 英数字とハイフンのみ(長さは、min と max で別に指定する)。
//
// 戻り値:
//   - 設定済みの validator。
func newRequestValidator() *validator.Validate {
	// 構造体のフィールドが必須かどうかも、タグで検証できるようにして作る。
	validate := validator.New(validator.WithRequiredStructEnabled())
	// エラーに出すフィールド名を、Go の名前ではなく JSON のキー名にする。
	validate.RegisterTagNameFunc(func(structField reflect.StructField) string {
		// json タグから、カンマより前のキー名だけを取り出す。
		jsonName, _, _ := strings.Cut(structField.Tag.Get("json"), ",")
		// キー名が無い、または出力しない(-)なら、Go のフィールド名を使う。
		if jsonName == "" || jsonName == "-" {
			// キー名が使えないので、フィールド名で代用する。
			return structField.Name
		}
		// JSON のキー名を、エラーに出す名前として返す。
		return jsonName
	})
	// resourcename の規則を登録する。登録の失敗は、規則名が固定なので起きない前提で捨てる。
	_ = validate.RegisterValidation("resourcename", func(fieldLevel validator.FieldLevel) bool {
		// 値が、英数字とハイフンだけでできているかを返す。
		return resourceNamePattern.MatchString(fieldLevel.Field().String())
	})
	// 設定済みの validator を返す。
	return validate
}

// bindAndValidate は、リクエストのボディを構造体に読み込み、validate タグで検証する。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//   - request: 読み込み先の構造体へのポインタ。
//
// 戻り値:
//   - ボディが JSON として読めない、または検証に失敗したときの、invalid_request のエラー。
//     検証の失敗では、メッセージに、問題のあったキー名を含める。
func bindAndValidate(echoContext *echo.Context, request any) error {
	// ボディを構造体に読み込む。JSON として読めなければ失敗する。
	if bindError := echoContext.Bind(request); bindError != nil {
		// 読み込みの失敗を、invalid_request のエラーとして返す。
		return apperror.Wrap(apperror.KindInvalidRequest, "リクエストのボディを読み取れませんでした", bindError)
	}

	// validate タグに従って、値を検証する。
	validationError := requestValidator.Struct(request)
	// 検証に通ったら、エラー無しで終わる。
	if validationError == nil {
		// 問題が無いので、エラー無しを返す。
		return nil
	}
	// 項目ごとのエラーを入れる変数を用意する。
	var fieldErrors validator.ValidationErrors
	// 検証のエラーが項目ごとのエラーなら、その一覧を取り出す。
	if asFieldErrors, isFieldErrors := validationError.(validator.ValidationErrors); isFieldErrors {
		// 取り出した一覧を使う。
		fieldErrors = asFieldErrors
	}
	// 問題のあった項目名を集める入れ物を用意する。
	invalidFieldNames := make([]string, 0, len(fieldErrors))
	// 問題のあった項目を、1つずつ見る。
	for _, fieldError := range fieldErrors {
		// JSON のキー名を、メッセージ用に追加する。
		invalidFieldNames = append(invalidFieldNames, fieldError.Field())
	}
	// 問題のあった項目名を含めた、invalid_request のエラーを返す。
	return apperror.Wrap(apperror.KindInvalidRequest, "次の項目の値が正しくありません: "+strings.Join(invalidFieldNames, ", "), validationError)
}
