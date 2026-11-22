package controller

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/apitoken"
	"launchs/shared/apperror"
	"launchs/shared/model"
)

// webhookBodyLimit は、webhook のボディの大きさの上限(バイト)。
const webhookBodyLimit = 1 << 20

// WebhookDeployUseCase は、WebhookController が使う、deploy のユースケース。
type WebhookDeployUseCase interface {
	Get(requestContext context.Context, userID string, deployID string) (model.Deploy, error)
	UpdateImage(requestContext context.Context, userID string, deployID string, imageRef string) (bool, error)
	ApplyConfig(requestContext context.Context, userID string, deployID string) error
}

// WebhookController は、CI やレジストリから呼ばれる、自動デプロイの webhook を受ける。
type WebhookController struct {
	deployUseCase WebhookDeployUseCase
	signer        *apitoken.Signer
	checker       middlewares.APITokenActiveChecker
}

// NewWebhookController は、WebhookController を作る。
//
// 引数:
//   - deployUseCase: deploy のユースケース。
//   - signer: API トークンの検証(nil なら webhook は全て拒否する)。
//   - checker: 失効の確認。
//
// 戻り値:
//   - 作成した WebhookController。
func NewWebhookController(deployUseCase WebhookDeployUseCase, signer *apitoken.Signer, checker middlewares.APITokenActiveChecker) *WebhookController {
	// 依存するものを持たせて返す。
	return &WebhookController{deployUseCase: deployUseCase, signer: signer, checker: checker}
}

// DeployWebhook は、deploy のイメージを更新して、反映を始める(POST /hooks/deploy)。
//
// トークンは、ヘッダーで渡す(X-Launchs-Token、または Authorization: Bearer)。
// トークンは、scope が deploy:image(または write)で、対象の deploy がちょうど 1 つに決まっているもの。
// ボディからイメージを決める(上から順に見る):
//   - {"imageRef": "ghcr.io/org/app:v2"} 完全な参照。
//   - {"tag": "v2"} いまのイメージの、タグだけを差し替える。
//   - Docker Hub の形式 {"push_data": {"tag": "v2"}, "repository": {"repo_name": "org/app"}}
//
// 引数:
//   - echoContext: Echo のリクエストの context。
//
// 戻り値:
//   - {"changed", "applied"}。トークンが無効なら unauthorized、権限が無ければ forbidden、
//     イメージを決められなければ invalid_request、反映中などは conflict。
func (webhookController *WebhookController) DeployWebhook(echoContext *echo.Context) error {
	// ヘッダーから、トークンを取り出す。
	tokenText := strings.TrimSpace(echoContext.Request().Header.Get("X-Launchs-Token"))
	if tokenText == "" {
		_, bearer, _ := strings.Cut(echoContext.Request().Header.Get("Authorization"), " ")
		tokenText = strings.TrimSpace(bearer)
	}
	// 署名と失効を確かめる。
	claims, authenticateError := middlewares.AuthenticateAPIToken(echoContext, webhookController.signer, webhookController.checker, tokenText)
	if authenticateError != nil {
		return authenticateError
	}
	// scope と、対象の deploy を確かめる(deploy は、トークンの 1 つだけ)。
	if !claims.HasScope(apitoken.ScopeDeployImage) && !claims.HasScope(apitoken.ScopeWrite) {
		return apperror.New(apperror.KindForbidden, "このトークンでは、webhook は使えません(deploy:image の scope が必要です)")
	}
	if len(claims.Deploys) != 1 {
		return apperror.New(apperror.KindForbidden, "webhook のトークンは、deploy を 1 つだけ指定してください")
	}
	deployID := claims.Deploys[0]
	userID := claims.Subject
	// ボディを読む(大きさを制限する)。
	bodyBytes, readError := io.ReadAll(io.LimitReader(echoContext.Request().Body, webhookBodyLimit))
	if readError != nil {
		return apperror.New(apperror.KindInvalidRequest, "ボディを読めませんでした")
	}
	// いまのイメージを取り、ボディから新しいイメージを決める。
	deploy, getError := webhookController.deployUseCase.Get(echoContext.Request().Context(), userID, deployID)
	if getError != nil {
		return getError
	}
	imageRef, imageError := imageRefFromWebhookBody(bodyBytes, deploy.ImageRef)
	if imageError != nil {
		return imageError
	}
	// イメージを保存して、変更があったときだけ、反映を始める。
	changed, updateError := webhookController.deployUseCase.UpdateImage(echoContext.Request().Context(), userID, deployID, imageRef)
	if updateError != nil {
		return updateError
	}
	applied := false
	if changed {
		if applyError := webhookController.deployUseCase.ApplyConfig(echoContext.Request().Context(), userID, deployID); applyError != nil {
			return applyError
		}
		applied = true
	}
	return echoContext.JSON(http.StatusOK, map[string]any{"deployId": deployID, "imageRef": imageRef, "changed": changed, "applied": applied})
}

// imageRefFromWebhookBody は、webhook のボディから、反映するイメージの参照を決める。
//
// 引数:
//   - body: webhook のボディ(JSON)。
//   - currentImageRef: いまのイメージ(タグだけの指定のとき、これを元にする)。
//
// 戻り値:
//   - 新しいイメージの参照。決められない、または使えない文字を含むときは invalid_request。
func imageRefFromWebhookBody(body []byte, currentImageRef string) (string, error) {
	// 受け付ける形(どれか)を読む。
	var payload struct {
		ImageRef string `json:"imageRef"`
		Tag      string `json:"tag"`
		PushData struct {
			Tag string `json:"tag"`
		} `json:"push_data"`
		Repository struct {
			RepoName string `json:"repo_name"`
		} `json:"repository"`
	}
	if len(body) > 0 {
		if unmarshalError := json.Unmarshal(body, &payload); unmarshalError != nil {
			return "", apperror.New(apperror.KindInvalidRequest, "ボディが JSON ではありません")
		}
	}
	// 完全な参照が最優先。
	imageRef := payload.ImageRef
	if imageRef == "" {
		// タグだけの指定(自前の形、または Docker Hub の形)。
		tag := payload.Tag
		if tag == "" {
			tag = payload.PushData.Tag
		}
		if tag == "" {
			return "", apperror.New(apperror.KindInvalidRequest, "imageRef か tag を指定してください")
		}
		imageRef = replaceImageTag(currentImageRef, tag)
	}
	// 作成のときと同じ規則で、使える文字と長さを確かめる。
	if len(imageRef) > 512 || !imageReferencePattern.MatchString(imageRef) {
		return "", apperror.New(apperror.KindInvalidRequest, "イメージの参照が正しくありません")
	}
	return imageRef, nil
}

// replaceImageTag は、イメージの参照の、タグ(または digest)を、別のタグに差し替える。
//
// 引数:
//   - imageRef: いまの参照(例: ghcr.io/org/app:v1)。
//   - tag: 新しいタグ。
//
// 戻り値:
//   - 差し替えた参照(例: ghcr.io/org/app:v2)。
func replaceImageTag(imageRef string, tag string) string {
	// digest があれば、先に外す。
	if before, _, found := strings.Cut(imageRef, "@"); found {
		imageRef = before
	}
	// 最後の "/" より後ろに ":" があれば、それがタグ(ホストのポートと取り違えない)。
	lastSlash := strings.LastIndex(imageRef, "/")
	if colon := strings.LastIndex(imageRef, ":"); colon > lastSlash {
		imageRef = imageRef[:colon]
	}
	return imageRef + ":" + tag
}
