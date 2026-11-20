package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"

	"launchs/backend/src/middlewares"
	"launchs/shared/apperror"
	"launchs/shared/model"
)

// maxServicePorts は、1つの service に公開できるポートの最大数。
const maxServicePorts = 20

// ServiceUseCase は、ServiceController が呼ぶ、service のビジネスルール。
//
// 実体は service の ServiceService。テストでは fake に差し替える。
type ServiceUseCase interface {
	Create(requestContext context.Context, userID string, deployID string, ports []model.ServicePort) (model.Service, error)
	Get(requestContext context.Context, userID string, deployID string) (model.Service, error)
	Delete(requestContext context.Context, userID string, deployID string) error
	UpdatePorts(requestContext context.Context, userID string, deployID string, ports []model.ServicePort) error
	DiscardPorts(requestContext context.Context, userID string, deployID string) error
	ApplyPorts(requestContext context.Context, userID string, deployID string) error
}

// ServicePortRequest は、公開するポート1つ(リクエストのボディ)。
type ServicePortRequest struct {
	// Name は、ポートの名前。小文字の英数字とハイフン、15文字まで(k8s のポート名の規則)。
	Name string `json:"name" validate:"required,max=15,portname"`

	// Port は、ポート番号。1〜65535。
	Port int `json:"port" validate:"required,min=1,max=65535"`

	// Protocol は、"tcp" または "udp"。
	Protocol string `json:"protocol" validate:"required,oneof=tcp udp"`
}

// CreateServiceRequest は、service 作成のリクエストのボディ。
type CreateServiceRequest struct {
	// Ports は、公開するポート。1〜20個。
	Ports []ServicePortRequest `json:"ports" validate:"required,min=1,max=20,dive"`
}

// ServicePortResponse は、公開しているポート1つ(レスポンスのボディ)。
type ServicePortResponse struct {
	// Name は、ポートの名前。
	Name string `json:"name"`

	// Port は、ポート番号。
	Port int `json:"port"`

	// Protocol は、"tcp" または "udp"。
	Protocol string `json:"protocol"`
}

// ServiceResponse は、service のレスポンスのボディ。
type ServiceResponse struct {
	// Tags は、タグ(昇順。無ければ空の配列)。
	Tags []string `json:"tags"`

	// ServiceID は、service の ID。
	ServiceID string `json:"serviceId"`

	// Ports は、現在反映されているポート。
	Ports []ServicePortResponse `json:"ports"`

	// PendingPorts は、まだ適用していないポート。変更が無ければ null。
	PendingPorts []ServicePortResponse `json:"pending_ports"`

	// Status は、service の状態(pending、active、failed、deleting)。
	Status string `json:"status"`

	// DNSName は、DNS 名。active になるまで null。
	DNSName *string `json:"dnsName"`

	// ClusterIP は、ClusterIP。active になるまで null(払い出されない種類のときも null)。
	ClusterIP *string `json:"clusterIP"`
}

// ServiceController は、/deploy/:deployId/service の HTTP の入出力を担当する。
type ServiceController struct {
	serviceUseCase ServiceUseCase
}

// NewServiceController は、ServiceController を作る。
//
// 引数:
//   - serviceUseCase: service のビジネスルール。
//
// 戻り値:
//   - 作成した ServiceController。
func NewServiceController(serviceUseCase ServiceUseCase) *ServiceController {
	// ビジネスルールを持たせた ServiceController を返す。
	return &ServiceController{serviceUseCase: serviceUseCase}
}

// Create は、deploy に service を作る(POST /deploy/:deployId/service)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - ボディが不正なら invalid_request、deploy が無い・他人のものなら not_found、
//     削除の途中・すでに service があるなら conflict、未認証なら unauthorized。
func (serviceController *ServiceController) Create(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// ボディを読み込み、validate タグで検証する。
	var request CreateServiceRequest
	if bindError := bindAndValidate(echoContext, &request); bindError != nil {
		return bindError
	}
	// 配列の中の重複(名前、番号と protocol の組)を検査して、モデルにする。
	ports, portsError := newServicePorts(request.Ports)
	if portsError != nil {
		return portsError
	}

	// 作成を、ビジネスルールに任せる。
	service, createError := serviceController.serviceUseCase.Create(echoContext.Request().Context(), userID, echoContext.Param("deployId"), ports)
	if createError != nil {
		return createError
	}
	// 作成した service を返す(この時点では pending で、DNS 名と ClusterIP は null)。
	return echoContext.JSON(http.StatusOK, newServiceResponse(service))
}

// Get は、deploy の service を返す(GET /deploy/:deployId/service)。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - deploy が無い・他人のもの、または service が無いなら not_found、未認証なら unauthorized。
func (serviceController *ServiceController) Get(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// ビジネスルールに、取得を任せる。
	service, getError := serviceController.serviceUseCase.Get(echoContext.Request().Context(), userID, echoContext.Param("deployId"))
	if getError != nil {
		return getError
	}
	return echoContext.JSON(http.StatusOK, newServiceResponse(service))
}

// Delete は、service の削除を受け付ける(DELETE /deploy/:deployId/service)。
//
// 削除の完了は待たず、すぐに応答する。status はすぐに deleting になり、完了するとレコードが消えて GET が 404 になる。
//
// 引数:
//   - echoContext: Echo のリクエストのコンテキスト。
//
// 戻り値:
//   - 無い・他人のものなら not_found、作成中・削除中・deploy が削除の途中なら conflict、未認証なら unauthorized。
func (serviceController *ServiceController) Delete(echoContext *echo.Context) error {
	// 認証したユーザーの ID を取り出す。
	userID, userIDError := middlewares.UserIDFrom(echoContext)
	if userIDError != nil {
		return userIDError
	}
	// 削除の受け付けを、ビジネスルールに任せる。
	if deleteError := serviceController.serviceUseCase.Delete(echoContext.Request().Context(), userID, echoContext.Param("deployId")); deleteError != nil {
		return deleteError
	}
	// 空のオブジェクトを返す。
	return echoContext.JSON(http.StatusOK, struct{}{})
}

// newServicePorts は、リクエストのポートの配列を、重複を検査して、モデルの配列にする。
//
// 検査すること:
//   - 名前が、同じ配列の中で重複しない
//   - 同じ番号と protocol の組が、重複しない(同じ番号でも、protocol が違えば、許可する。例: DNS の 53 番の tcp と udp)
//
// 引数:
//   - requests: リクエストのポート(1つずつの値は、検査済み)。
//
// 戻り値:
//   - モデルのポートの配列。
//   - 重複があるときの、invalid_request のエラー。
func newServicePorts(requests []ServicePortRequest) ([]model.ServicePort, error) {
	// すでに使われた名前と、番号と protocol の組を覚えながら、モデルにしていく。
	usedNames := map[string]bool{}
	usedPorts := map[string]bool{}
	ports := make([]model.ServicePort, 0, len(requests))
	for _, request := range requests {
		if usedNames[request.Name] {
			return nil, apperror.New(apperror.KindInvalidRequest, "ports の name が重複しています: "+request.Name)
		}
		portKey := fmt.Sprintf("%d/%s", request.Port, request.Protocol)
		if usedPorts[portKey] {
			return nil, apperror.New(apperror.KindInvalidRequest, "ports の port と protocol の組が重複しています: "+portKey)
		}
		usedNames[request.Name], usedPorts[portKey] = true, true
		ports = append(ports, model.ServicePort{Name: request.Name, Port: request.Port, Protocol: request.Protocol})
	}
	return ports, nil
}

// newServiceResponse は、モデルを、レスポンスの形にする。
//
// ポートの JSON の列は、読み取って、配列にする。pending のポートが無ければ null にする。
//
// 引数:
//   - service: service のモデル。
//
// 戻り値:
//   - レスポンスのボディ。
func newServiceResponse(service model.Service) ServiceResponse {
	return ServiceResponse{
		ServiceID:    service.ID,
		Tags:         service.TagList(),
		Ports:        servicePortResponses(service.Ports),
		PendingPorts: pendingServicePortResponses(service.PendingPorts),
		Status:       service.Status,
		DNSName:      service.DNSName,
		ClusterIP:    service.ClusterIP,
	}
}

// servicePortResponses は、ポートの JSON の列を、レスポンスのポートの配列にする。読み取れないときは、空の配列にする。
//
// 引数:
//   - column: ポートの JSON の列。
//
// 戻り値:
//   - レスポンスのポートの配列(null にしない)。
func servicePortResponses(column []byte) []ServicePortResponse {
	// JSON の配列を読む。読めなければ、空にする。
	ports := []ServicePortResponse{}
	if len(column) > 0 {
		if unmarshalError := json.Unmarshal(column, &ports); unmarshalError != nil {
			return []ServicePortResponse{}
		}
	}
	return ports
}

// pendingServicePortResponses は、pending のポートの JSON の列を、レスポンスのポートの配列にする。値が無ければ nil(null)にする。
//
// 引数:
//   - column: pending のポートの JSON の列(NULL のときは長さ 0)。
//
// 戻り値:
//   - レスポンスのポートの配列。無ければ nil。
func pendingServicePortResponses(column []byte) []ServicePortResponse {
	// 値が無いとき(DB の NULL)は、null にする。
	if len(column) == 0 {
		return nil
	}
	return servicePortResponses(column)
}
