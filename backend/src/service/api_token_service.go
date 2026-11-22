package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"launchs/shared/apitoken"
	"launchs/shared/apperror"
	"launchs/shared/model"
)

// APITokenStore は、APITokenService が必要とする、API トークンの保存先の操作。
//
// 実体は shared/repository の APITokenRepository。
type APITokenStore interface {
	Create(requestContext context.Context, token *model.APIToken) error
	ListByUser(requestContext context.Context, userID string) ([]model.APIToken, error)
	Revoke(requestContext context.Context, userID string, tokenID string) error
}

// APITokenService は、API トークンの発行・一覧・失効を行う。
type APITokenService struct {
	store  APITokenStore
	signer *apitoken.Signer
}

// NewAPITokenService は、APITokenService を作る。
//
// 引数:
//   - store: 保存先。
//   - signer: トークンの署名(nil のときは、API トークンの機能は無効で、発行は unavailable になる)。
//
// 戻り値:
//   - 作成した APITokenService。
func NewAPITokenService(store APITokenStore, signer *apitoken.Signer) *APITokenService {
	// 依存するものを持たせて返す。
	return &APITokenService{store: store, signer: signer}
}

// Create は、API トークンを発行して、有効なものとして保存する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - name: 名前(用途のメモ)。
//   - scope: 許す操作。
//   - deploys: 対象の deploy の ID(空なら絞らない)。
//   - projects: 対象のプロジェクトの ID(空なら絞らない)。
//   - expiresAt: 期限。nil なら無期限。
//
// 戻り値:
//   - 保存した行と、トークンの文字列(このときだけ得られる)。
//   - scope が空・不正なら invalid_request。deploy:image だけのときは、deploy をちょうど 1 つ指定する。機能が無効なら service_unavailable。
func (service *APITokenService) Create(requestContext context.Context, userID string, name string, scope []string, deploys []string, projects []string, expiresAt *time.Time) (model.APIToken, string, error) {
	// 署名の秘密が無いときは、機能が無効。
	if service.signer == nil {
		return model.APIToken{}, "", apperror.New(apperror.KindUnavailable, "API トークンは、この環境では使えません(API_TOKEN_SECRET が未設定です)")
	}
	// scope を確かめる。
	if len(scope) == 0 {
		return model.APIToken{}, "", apperror.New(apperror.KindInvalidRequest, "scope を 1 つ以上指定してください")
	}
	for _, item := range scope {
		if !apitoken.ValidScope(item) {
			return model.APIToken{}, "", apperror.New(apperror.KindInvalidRequest, "scope は read、write、deploy:image のどれかです")
		}
	}
	// deploy:image だけのトークン(deploy 用)は、対象の deploy を 1 つに決める(webhook が、どの deploy かを分かるように)。
	if len(scope) == 1 && scope[0] == apitoken.ScopeDeployImage && (len(deploys) != 1 || len(projects) != 0) {
		return model.APIToken{}, "", apperror.New(apperror.KindInvalidRequest, "deploy:image のトークンは、deploy を 1 つだけ指定してください")
	}
	// 期限が過去なら、拒否する。
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		return model.APIToken{}, "", apperror.New(apperror.KindInvalidRequest, "期限は未来の日時にしてください")
	}
	// 署名して、保存する行を作る。
	tokenID := model.APITokenIDPrefix + uuid.NewString()
	tokenText, issueError := service.signer.Issue(tokenID, userID, scope, deploys, projects, expiresAt)
	if issueError != nil {
		return model.APIToken{}, "", apperror.Wrap(apperror.KindInternal, "API トークンを発行できませんでした", issueError)
	}
	token := model.APIToken{ID: tokenID, UserID: userID, Name: name, Scope: mustJSON(scope), Deploys: mustJSON(deploys), Projects: mustJSON(projects), ExpiresAt: expiresAt}
	if createError := service.store.Create(requestContext, &token); createError != nil {
		return model.APIToken{}, "", createError
	}
	return token, tokenText, nil
}

// List は、利用者の API トークンの一覧を返す(失効したものも含む)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//
// 戻り値:
//   - トークンの一覧。
func (service *APITokenService) List(requestContext context.Context, userID string) ([]model.APIToken, error) {
	// 保存先に任せる。
	return service.store.ListByUser(requestContext, userID)
}

// Revoke は、API トークンを失効させる。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - userID: 持ち主のユーザー ID。
//   - tokenID: 失効させるトークンの ID。
//
// 戻り値:
//   - 無い、または他のユーザーのものなら not_found。
func (service *APITokenService) Revoke(requestContext context.Context, userID string, tokenID string) error {
	// 保存先に任せる。
	return service.store.Revoke(requestContext, userID, tokenID)
}

// mustJSON は、文字列の配列を JSON にする(nil は空の配列にする)。文字列の配列は必ず JSON にできる。
//
// 引数:
//   - values: 文字列の配列。
//
// 戻り値:
//   - JSON のバイト列。
func mustJSON(values []string) []byte {
	// nil のままだと null になるので、空の配列にする。
	if values == nil {
		values = []string{}
	}
	encoded, _ := json.Marshal(values)
	return encoded
}
