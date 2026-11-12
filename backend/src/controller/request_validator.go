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
	validate := validator.New(validator.WithRequiredStructEnabled())
	validate.RegisterTagNameFunc(func(structField reflect.StructField) string {
		jsonName, _, _ := strings.Cut(structField.Tag.Get("json"), ",")
		if jsonName == "" || jsonName == "-" {
			return structField.Name
		}
		return jsonName
	})
	_ = validate.RegisterValidation("resourcename", func(fieldLevel validator.FieldLevel) bool {
		return resourceNamePattern.MatchString(fieldLevel.Field().String())
	})
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
	if bindError := echoContext.Bind(request); bindError != nil {
		return apperror.Wrap(apperror.KindInvalidRequest, "リクエストのボディを読み取れませんでした", bindError)
	}

	validationError := requestValidator.Struct(request)
	if validationError == nil {
		return nil
	}
	var fieldErrors validator.ValidationErrors
	if asFieldErrors, isFieldErrors := validationError.(validator.ValidationErrors); isFieldErrors {
		fieldErrors = asFieldErrors
	}
	invalidFieldNames := make([]string, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		invalidFieldNames = append(invalidFieldNames, fieldError.Field())
	}
	return apperror.Wrap(apperror.KindInvalidRequest, "次の項目の値が正しくありません: "+strings.Join(invalidFieldNames, ", "), validationError)
}
