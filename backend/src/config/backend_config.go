// Package config は、バックエンドの設定を環境変数から読む。
//
// 環境変数を読む部品は shared/config にあり、このパッケージは、
// バックエンドが必要とする環境変数の名前と、既定値を決める。
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	sharedconfig "launchs/shared/config"
	"launchs/shared/logger"
)

const (
	// defaultHTTPPort は、HTTP_PORT が設定されていないときに使うポート番号。
	defaultHTTPPort = 8080

	// defaultTemporalAddress は、TEMPORAL_ADDRESS が設定されていないときに使う接続先。
	defaultTemporalAddress = "temporal:7233"

	// defaultTemporalNamespace は、TEMPORAL_NAMESPACE が設定されていないときに使う namespace。
	defaultTemporalNamespace = "default"

	// defaultLogLevelText は、LOG_LEVEL が設定されていないときに使うログレベル。
	defaultLogLevelText = "info"

	// defaultIngressBaseDomain は、INGRESS_BASE_DOMAIN が設定されていないときに使う、ホスト名のベースドメイン。
	defaultIngressBaseDomain = "launchs.org"

	// defaultIngressReservedSubdomains は、INGRESS_RESERVED_SUBDOMAINS が設定されていないときに使う、払い出さないサブドメイン。
	defaultIngressReservedSubdomains = "www,api,app,auth,admin,registry,logs,mail,static,docs,status,traefik,dashboard"

	// defaultVolumeMaxSizeMB は、VOLUME_MAX_SIZE_MB が設定されていないときに使う、volume の大きさの上限(MB)。
	defaultVolumeMaxSizeMB = 5120

	// minimumPort と maximumPort は、ポート番号として使える範囲。
	minimumPort = 1
	maximumPort = 65535
)

// ingressDomainPattern は、ベースドメインとして受け付ける形。
var ingressDomainPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)+$`)

// BackendConfig は、バックエンドの設定。
type BackendConfig struct {
	// HTTPPort は、API サーバーが待ち受けるポート番号(環境変数 HTTP_PORT)。
	HTTPPort int

	// LogLevel は、出力するログの最低のレベル(環境変数 LOG_LEVEL)。
	LogLevel slog.Level

	// DatabaseURL は、app データベースの接続先(環境変数 DATABASE_URL)。パスワードを含むので、ログに出さない。
	DatabaseURL string

	// AuthJWKSURL は、認証のコンテナ(auth)の JWKS のエンドポイント(環境変数 AUTH_JWKS_URL。例: http://auth:8080/auth/.well-known/jwks.json)。
	// 指定すると、access token を、この公開鍵で検証する。
	AuthJWKSURL string

	// AuthRequireJWKS は、本番の指定(環境変数 AUTH_REQUIRE_JWKS=true)。true のときは、AUTH_JWKS_URL が必須で、DEV_USER_ID は指定できない。
	AuthRequireJWKS bool

	// DevUserID は、開発用の仮の認証で、全てのリクエストのユーザーとして扱う ID(環境変数 DEV_USER_ID)。
	// AUTH_JWKS_URL を使わないときだけ指定できる(auth を起動せずに、backend だけを試すとき用)。
	DevUserID string

	// TemporalAddress は、Temporal サーバーの接続先(環境変数 TEMPORAL_ADDRESS)。
	TemporalAddress string

	// TemporalNamespace は、使う Temporal の namespace(環境変数 TEMPORAL_NAMESPACE)。
	TemporalNamespace string

	// VolumeMaxSizeMB は、volume 1つの大きさの上限(MB。環境変数 VOLUME_MAX_SIZE_MB)。アカウントごとの上限は、認証のコンテナを作るときに設ける。
	VolumeMaxSizeMB int

	// IngressBaseDomain は、ingress に払い出すホスト名の、後ろに付けるドメイン(環境変数 INGRESS_BASE_DOMAIN)。
	IngressBaseDomain string

	// IngressReservedSubdomains は、ingress に払い出さないサブドメイン(環境変数 INGRESS_RESERVED_SUBDOMAINS。カンマ区切り)。
	// 指定すると、既定の一覧を置き換える。ホスト名のどれか1つの単語が、これに含まれると、その名前は払い出されない。
	IngressReservedSubdomains []string
}

// LoadBackendConfig は、環境変数からバックエンドの設定を読む。
//
// 読む環境変数:
//   - DATABASE_URL: app データベースの接続先(必須)。
//   - AUTH_JWKS_URL: 認証のコンテナの JWKS の URL(本番)。DEV_USER_ID のどちらか一方が必須。
//   - AUTH_REQUIRE_JWKS: true なら、AUTH_JWKS_URL が必須で、DEV_USER_ID を拒否する(本番用。既定は false)。
//   - DEV_USER_ID: 開発用の仮の認証で使うユーザー ID(AUTH_JWKS_URL と同時には指定できない)。
//   - TEMPORAL_ADDRESS: Temporal サーバーの接続先。省略すると temporal:7233。
//   - TEMPORAL_NAMESPACE: Temporal の namespace。省略すると default。
//   - HTTP_PORT: 待ち受けるポート番号(1〜65535)。省略すると 8080。
//   - LOG_LEVEL: ログレベル(debug、info、warn、error)。省略すると info。
//
// 設定に問題があっても止まらず、見つかった全ての問題をまとめて報告する。
//
// 引数:
//   - reader: 環境変数を読む EnvironmentReader。
//
// 戻り値:
//   - 読み込んだ設定。問題があったときは、ゼロ値。
//   - エラー。問題があったとき、全ての問題の説明を含む。
func LoadBackendConfig(reader *sharedconfig.EnvironmentReader) (BackendConfig, error) {
	// 必須の環境変数を読む。無いときは、reader が問題として覚えておく(ここでは止まらない)。
	databaseURL := reader.RequiredString("DATABASE_URL")
	// 開発用のユーザー ID を読む。省略は許すので、既定値は空にする。
	devUserID := reader.OptionalString("DEV_USER_ID", "")
	// 認証の設定を読む(JWKS の URL と、本番の指定)。
	authJWKSURL := reader.OptionalString("AUTH_JWKS_URL", "")
	authRequireJWKSText := reader.OptionalString("AUTH_REQUIRE_JWKS", "false")
	// ポート番号を整数で読む。整数でないときは、reader が問題として覚えておく。
	temporalAddress := reader.OptionalString("TEMPORAL_ADDRESS", defaultTemporalAddress)
	temporalNamespace := reader.OptionalString("TEMPORAL_NAMESPACE", defaultTemporalNamespace)
	httpPort := reader.OptionalInt("HTTP_PORT", defaultHTTPPort)
	// ログレベルを文字列のまま読む。意味の確認は、あとで ParseLevel で行う。
	logLevelText := reader.OptionalString("LOG_LEVEL", defaultLogLevelText)
	// volume の大きさの上限を読む。
	volumeMaxSizeMB := reader.OptionalInt("VOLUME_MAX_SIZE_MB", defaultVolumeMaxSizeMB)
	// ingress のホスト名に関する設定を読む。
	ingressBaseDomain := reader.OptionalString("INGRESS_BASE_DOMAIN", defaultIngressBaseDomain)
	reservedSubdomainsText := reader.OptionalString("INGRESS_RESERVED_SUBDOMAINS", defaultIngressReservedSubdomains)

	// ここまでの読み込みで見つかった問題を取り出し、以降の確認の問題と、まとめて報告できるようにする。
	loadError := reader.Err()

	// ポート番号が使える範囲の外のときは、問題として加える。
	if httpPort < minimumPort || httpPort > maximumPort {
		// 範囲と、実際の値を含むエラーを作る。
		portError := fmt.Errorf("環境変数 HTTP_PORT は %d〜%d でなければならない: %d", minimumPort, maximumPort, httpPort)
		// 見つかった問題を、これまでの問題にまとめる。
		loadError = errors.Join(loadError, portError)
	}

	// ログレベルの文字列を、slog のレベルに変換する。
	logLevel, levelError := logger.ParseLevel(logLevelText)
	// 未知のレベルのときは、どの環境変数の問題か分かるようにして、問題に加える。
	if levelError != nil {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 LOG_LEVEL: %w", levelError))
	}

	// 上限は、1以上でなければならない。
	if volumeMaxSizeMB < 1 {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 VOLUME_MAX_SIZE_MB は 1 以上でなければならない: %d", volumeMaxSizeMB))
	}

	// ベースドメインは、DNS 名の形(小文字の英数字とハイフンを、ドットで区切ったもの)でなければならない。
	if !ingressDomainPattern.MatchString(ingressBaseDomain) {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 INGRESS_BASE_DOMAIN は、小文字の英数字とハイフンを、ドットで区切った DNS 名でなければなりません: %q", ingressBaseDomain))
	}

	// 本番の指定は、true か false。
	if authRequireJWKSText != "true" && authRequireJWKSText != "false" {
		loadError = errors.Join(loadError, fmt.Errorf("環境変数 AUTH_REQUIRE_JWKS は true か false でなければならない: %q", authRequireJWKSText))
	}
	authRequireJWKS := authRequireJWKSText == "true"
	// 認証の方法は、ちょうど1つ(JWKS か、開発用の仮の認証)。どちらも無いと、誰でも通れるか、誰も通れないので、起動させない。
	if authJWKSURL == "" && devUserID == "" {
		loadError = errors.Join(loadError, errors.New("環境変数 AUTH_JWKS_URL(本番)か DEV_USER_ID(開発用)のどちらかが必要です"))
	}
	if authJWKSURL != "" && devUserID != "" {
		loadError = errors.Join(loadError, errors.New("環境変数 AUTH_JWKS_URL と DEV_USER_ID は、同時に指定できません"))
	}
	// 本番の指定のときは、全員が同じユーザーになる、開発用の仮の認証を、使わせない。
	if authRequireJWKS && (authJWKSURL == "" || devUserID != "") {
		loadError = errors.Join(loadError, errors.New("環境変数 AUTH_REQUIRE_JWKS=true のときは、AUTH_JWKS_URL が必須で、DEV_USER_ID は指定できません"))
	}

	// 1つでも問題があれば、中途半端な設定を使わせないため、ゼロ値とエラーを返す。
	if loadError != nil {
		return BackendConfig{}, loadError
	}
	// 問題が無いので、読み込んだ値で設定を作って返す。
	return BackendConfig{HTTPPort: httpPort, LogLevel: logLevel, DatabaseURL: databaseURL, DevUserID: devUserID, AuthJWKSURL: authJWKSURL, AuthRequireJWKS: authRequireJWKS, TemporalAddress: temporalAddress, TemporalNamespace: temporalNamespace,
		VolumeMaxSizeMB: volumeMaxSizeMB, IngressBaseDomain: ingressBaseDomain, IngressReservedSubdomains: splitCommaSeparated(reservedSubdomainsText)}, nil
}

// splitCommaSeparated は、カンマ区切りの文字列を、前後の空白を除いて分け、空の要素を捨てる。
//
// 引数:
//   - text: カンマ区切りの文字列。
//
// 戻り値:
//   - 要素の一覧。
func splitCommaSeparated(text string) []string {
	// カンマで分けて、前後の空白を除き、空でないものだけを集める。
	items := []string{}
	for _, item := range strings.Split(text, ",") {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}
