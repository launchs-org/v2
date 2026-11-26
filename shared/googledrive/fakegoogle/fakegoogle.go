// Package fakegoogle は、試験用の偽の Google(OAuth のトークンと、Drive API の一部)を、メモリの上で動かす。
// googledrive と exporter の試験で使う。本番のコードからは、使わない。
package fakegoogle

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"launchs/shared/googledrive"
)

// File は、偽の Drive にある、ファイルかフォルダ。
type File struct {
	ID      string
	Name    string
	Parent  string
	Folder  bool
	Trashed bool
	Content []byte
}

// Server は、偽の Google。
type Server struct {
	// HTTP は、立ち上げた偽のサーバー。
	HTTP *httptest.Server

	mutex sync.Mutex

	// Files は、ファイルとフォルダ(ID で引ける)。
	Files map[string]*File

	// RevokedTokens は、取り消された合鍵。
	RevokedTokens []string

	// InvalidRefreshTokens は、invalid_grant を返す合鍵(許可が取り消された状態を、再現する)。
	InvalidRefreshTokens map[string]bool

	// FailUploads が true のとき、ファイルの作成・更新は、500 を返す。
	FailUploads bool

	// nextID は、次に振る ID の番号。
	nextID int
}

// New は、偽の Google を立ち上げる。止めるときは、HTTP.Close を呼ぶ。
//
// 戻り値:
//   - 偽の Google。
func New() *Server {
	// 空の状態で作って、ルーティングする。
	server := &Server{Files: map[string]*File{}, InvalidRefreshTokens: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", server.handleToken)
	mux.HandleFunc("/revoke", server.handleRevoke)
	mux.HandleFunc("/drive/v3/files", server.handleCreateFolder)
	mux.HandleFunc("/drive/v3/files/", server.handleFile)
	mux.HandleFunc("/upload/drive/v3/files", server.handleUploadCreate)
	mux.HandleFunc("/upload/drive/v3/files/", server.handleUploadUpdate)
	server.HTTP = httptest.NewServer(mux)
	return server
}

// Config は、この偽の Google に向けた、googledrive の設定を返す。
//
// 戻り値:
//   - 設定(クライアント ID と秘密と戻り先は、試験用の値)。
func (server *Server) Config() googledrive.Config {
	// 各 URL を、偽のサーバーに向ける。
	return googledrive.Config{
		ClientID: "client-id", ClientSecret: "client-secret", RedirectURL: "http://localhost/callback",
		AuthURL: server.HTTP.URL + "/auth", TokenURL: server.HTTP.URL + "/token", RevokeURL: server.HTTP.URL + "/revoke",
		APIBase: server.HTTP.URL + "/drive/v3", UploadBase: server.HTTP.URL + "/upload/drive/v3",
	}
}

// ChildrenNamed は、ある親の下の、名前が一致する(ゴミ箱でない)ファイルを返す。
//
// 引数:
//   - parent: 親のフォルダの ID。
//   - name: 名前。
//
// 戻り値:
//   - 一致するもの。
func (server *Server) ChildrenNamed(parent string, name string) []*File {
	// 順に、見ていく。
	server.mutex.Lock()
	defer server.mutex.Unlock()
	var found []*File
	for _, file := range server.Files {
		if file.Parent == parent && file.Name == name && !file.Trashed {
			found = append(found, file)
		}
	}
	return found
}

// newID は、新しい ID を振る(呼び出し側が、ロックを持っていること)。
func (server *Server) newID(prefix string) string {
	// 番号を進めて、接頭辞を付ける。
	server.nextID++
	return prefix + "-" + string(rune('a'+server.nextID%26)) + strings.Repeat("x", server.nextID/26) + "-" + itoa(server.nextID)
}

// itoa は、整数を文字列にする(strconv を、この小さな関数のために読み込まない)。
func itoa(value int) string {
	// 10 進数に直す。
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// handleToken は、トークンの API(コードの交換と、合鍵での更新)。
func (server *Server) handleToken(writer http.ResponseWriter, request *http.Request) {
	// フォームを読む。
	_ = request.ParseForm()
	writer.Header().Set("Content-Type", "application/json")
	switch request.Form.Get("grant_type") {
	case "authorization_code":
		// コード "bad" は、無効。それ以外は、合鍵と、メールアドレス入りの ID トークンを返す。
		if request.Form.Get("code") == "bad" {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		payload, _ := json.Marshal(map[string]string{"email": "me@example.com"})
		idToken := "x." + base64.RawURLEncoding.EncodeToString(payload) + ".y"
		refreshToken := "refresh-" + request.Form.Get("code")
		if request.Form.Get("code") == "norefresh" {
			refreshToken = ""
		}
		_ = json.NewEncoder(writer).Encode(map[string]string{"access_token": "access-1", "refresh_token": refreshToken, "id_token": idToken})
	case "refresh_token":
		// 無効にした合鍵には、invalid_grant を返す。
		server.mutex.Lock()
		invalid := server.InvalidRefreshTokens[request.Form.Get("refresh_token")]
		server.mutex.Unlock()
		if invalid {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]string{"access_token": "access-2"})
	default:
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":"unsupported_grant_type"}`))
	}
}

// handleRevoke は、合鍵の取り消し。
func (server *Server) handleRevoke(writer http.ResponseWriter, request *http.Request) {
	// 取り消された合鍵を、覚える。
	_ = request.ParseForm()
	server.mutex.Lock()
	server.RevokedTokens = append(server.RevokedTokens, request.Form.Get("token"))
	server.mutex.Unlock()
	writer.WriteHeader(http.StatusOK)
}

// authorized は、短い鍵が付いているかを確かめる(付いていなければ、401 を返す)。
func (server *Server) authorized(writer http.ResponseWriter, request *http.Request) bool {
	// Bearer が付いていること。
	if !strings.HasPrefix(request.Header.Get("Authorization"), "Bearer access-") {
		writer.WriteHeader(http.StatusUnauthorized)
		return false
	}
	return true
}

// handleCreateFolder は、フォルダの作成(POST /files)。
func (server *Server) handleCreateFolder(writer http.ResponseWriter, request *http.Request) {
	// 認証と、メソッドを確かめる。
	if !server.authorized(writer, request) || request.Method != http.MethodPost {
		if request.Method != http.MethodPost {
			writer.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	var metadata struct {
		Name     string   `json:"name"`
		MimeType string   `json:"mimeType"`
		Parents  []string `json:"parents"`
	}
	_ = json.NewDecoder(request.Body).Decode(&metadata)
	server.mutex.Lock()
	file := &File{ID: server.newID("folder"), Name: metadata.Name, Folder: metadata.MimeType == googledrive.FolderMimeType}
	if len(metadata.Parents) > 0 {
		file.Parent = metadata.Parents[0]
	}
	server.Files[file.ID] = file
	server.mutex.Unlock()
	_ = json.NewEncoder(writer).Encode(map[string]string{"id": file.ID})
}

// handleFile は、ファイルの情報(fields=…)と、中身(alt=media)の取得。
func (server *Server) handleFile(writer http.ResponseWriter, request *http.Request) {
	// 認証を確かめて、ファイルを探す。
	if !server.authorized(writer, request) {
		return
	}
	server.mutex.Lock()
	defer server.mutex.Unlock()
	file := server.Files[strings.TrimPrefix(request.URL.Path, "/drive/v3/files/")]
	if file == nil {
		writer.WriteHeader(http.StatusNotFound)
		return
	}
	// 中身を返す。
	if request.URL.Query().Get("alt") == "media" {
		_, _ = writer.Write(file.Content)
		return
	}
	_ = json.NewEncoder(writer).Encode(map[string]any{"id": file.ID, "trashed": file.Trashed})
}

// handleUploadCreate は、ファイルの作成(multipart)。
func (server *Server) handleUploadCreate(writer http.ResponseWriter, request *http.Request) {
	// 認証を確かめる。失敗を再現するときは、500。
	if !server.authorized(writer, request) {
		return
	}
	if server.FailUploads {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	// multipart を、情報と中身に分ける。
	_, parameters, parseError := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if parseError != nil {
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	reader := multipart.NewReader(request.Body, parameters["boundary"])
	var metadata struct {
		Name    string   `json:"name"`
		Parents []string `json:"parents"`
	}
	var content []byte
	for index := 0; ; index++ {
		part, nextError := reader.NextPart()
		if nextError != nil {
			break
		}
		data, _ := io.ReadAll(part)
		if index == 0 {
			_ = json.Unmarshal(data, &metadata)
		} else {
			content = data
		}
	}
	server.mutex.Lock()
	file := &File{ID: server.newID("file"), Name: metadata.Name, Content: content}
	if len(metadata.Parents) > 0 {
		file.Parent = metadata.Parents[0]
	}
	server.Files[file.ID] = file
	server.mutex.Unlock()
	_ = json.NewEncoder(writer).Encode(map[string]string{"id": file.ID})
}

// handleUploadUpdate は、ファイルの中身の置き換え(PATCH)。
func (server *Server) handleUploadUpdate(writer http.ResponseWriter, request *http.Request) {
	// 認証を確かめる。失敗を再現するときは、500。
	if !server.authorized(writer, request) {
		return
	}
	if server.FailUploads {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	content, _ := io.ReadAll(request.Body)
	server.mutex.Lock()
	defer server.mutex.Unlock()
	file := server.Files[strings.TrimPrefix(request.URL.Path, "/upload/drive/v3/files/")]
	if file == nil {
		writer.WriteHeader(http.StatusNotFound)
		return
	}
	file.Content = content
	_ = json.NewEncoder(writer).Encode(map[string]string{"id": file.ID})
}
