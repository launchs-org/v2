package exportsend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"

	"launchs/shared/credential"
	"launchs/shared/exportlog"
	"launchs/shared/googledrive"
	"launchs/shared/jst"
	"launchs/shared/model"
	"launchs/shared/repository"
)

const (
	// RootFolderName は、Drive の、書き出し先の大元のフォルダの名前。
	RootFolderName = "launchs-access-logs"

	// DefaultMaxFileBytes は、1 つのファイルの大きさの上限(これを超えるときは、次の分割に書く)。
	DefaultMaxFileBytes = 50 << 20
)

// DriveStore は、Drive への書き込みが、記録を読み書きする先。repository.ExportRepository が実装する。
type DriveStore interface {
	// ProjectName は、プロジェクトの名前を返す。
	ProjectName(requestContext context.Context, projectID string) (string, error)

	// FindProjectFolder と SaveProjectFolder は、プロジェクトのフォルダの ID の記録。
	FindProjectFolder(requestContext context.Context, userID string, projectID string) (string, error)
	SaveProjectFolder(requestContext context.Context, userID string, projectID string, folderID string) error

	// SetConnectionRootFolder は、大元のフォルダの ID を記録する。
	SetConnectionRootFolder(requestContext context.Context, userID string, folderID string) error

	// FindDriveFile と SaveDriveFile は、その日のファイルの記録。
	FindDriveFile(requestContext context.Context, destinationID string, day string) (*repository.DriveFile, error)
	SaveDriveFile(requestContext context.Context, file *repository.DriveFile) error
}

// DriveWriter は、ログを、Google Drive の、日ごとのファイルに書く。
type DriveWriter struct {
	// client は、Google の API のクライアント。
	client *googledrive.Client

	// store は、記録の読み書き先。
	store DriveStore

	// cipher は、合鍵を復号する。
	cipher *credential.Cipher

	// maxFileBytes は、1 つのファイルの大きさの上限。
	maxFileBytes int64
}

// NewDriveWriter は、DriveWriter を作る。
//
// 引数:
//   - client: Google の API のクライアント。
//   - store: 記録の読み書き先。
//   - cipher: 合鍵を復号するもの。
//   - maxFileBytes: 1 つのファイルの大きさの上限(0 以下なら既定)。
//
// 戻り値:
//   - 作った書き手。
func NewDriveWriter(client *googledrive.Client, store DriveStore, cipher *credential.Cipher, maxFileBytes int64) *DriveWriter {
	// 上限が無ければ、既定を使う。
	if maxFileBytes <= 0 {
		maxFileBytes = DefaultMaxFileBytes
	}
	return &DriveWriter{client: client, store: store, cipher: cipher, maxFileBytes: maxFileBytes}
}

// Write は、ログを、日本時間の日ごとに分けて、その日のファイルの末尾に足す。
// Drive は追記ができないので、「今のファイルを取る → 末尾に足す → 置き換える」で行う。
// 同じ行を、二重に書かない(ファイルの記録に、最後に書いた id を持つ)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - connection: アカウントの Google Drive の接続。
//   - destination: 転送の設定(google_drive)。
//   - rows: 書くログ(id の昇順)。
//
// 戻り値:
//   - 失敗したとき。合鍵が効かないときは、googledrive.ErrInvalidGrant を含む。
func (writer *DriveWriter) Write(requestContext context.Context, connection model.GoogleDriveConnection, destination model.ExportDestination, rows []repository.ExportLogRow) error {
	// 書くものが無ければ、何もしない。
	if len(rows) == 0 {
		return nil
	}
	// 合鍵を復号して、Drive の操作の準備をする。
	refreshToken, decryptError := writer.cipher.Decrypt(connection.Credential)
	if decryptError != nil {
		return errors.New("Google の合鍵を復号できませんでした(暗号化の鍵が変わった可能性があります。Google Drive に、もう一度接続してください)")
	}
	session := writer.client.NewSession(refreshToken)
	// プロジェクトのフォルダを用意する。
	projectFolderID, folderError := writer.ensureProjectFolder(requestContext, session, connection, destination.ProjectID)
	if folderError != nil {
		return folderError
	}
	// 日ごとに分けて、書く(rows は id の昇順なので、日も昇順に並ぶ)。
	var days []string
	byDay := map[string][]repository.ExportLogRow{}
	for _, row := range rows {
		day := jst.DayString(row.RequestedAt)
		if _, seen := byDay[day]; !seen {
			days = append(days, day)
		}
		byDay[day] = append(byDay[day], row)
	}
	for _, day := range days {
		if writeError := writer.writeDay(requestContext, session, destination, projectFolderID, day, byDay[day]); writeError != nil {
			return writeError
		}
	}
	return nil
}

// ensureProjectFolder は、プロジェクトのフォルダ(大元のフォルダの下)を用意する。無い、またはゴミ箱に入っていれば、作り直す。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - session: Drive の操作。
//   - connection: アカウントの接続(大元のフォルダの ID を持つ)。
//   - projectID: プロジェクトの ID。
//
// 戻り値:
//   - プロジェクトのフォルダの ID。
func (writer *DriveWriter) ensureProjectFolder(requestContext context.Context, session *googledrive.Session, connection model.GoogleDriveConnection, projectID string) (string, error) {
	// 大元のフォルダが使えなければ、作り直す。
	rootID := connection.RootFolderID
	rootUsable := false
	if rootID != "" {
		usable, usableError := session.FolderUsable(requestContext, rootID)
		if usableError != nil {
			return "", usableError
		}
		rootUsable = usable
	}
	if !rootUsable {
		createdID, createError := session.CreateFolder(requestContext, RootFolderName, "")
		if createError != nil {
			return "", createError
		}
		rootID = createdID
		if saveError := writer.store.SetConnectionRootFolder(requestContext, connection.UserID, rootID); saveError != nil {
			return "", saveError
		}
	}
	// プロジェクトのフォルダが使えなければ(大元を作り直したときも含めて)、作り直す。
	projectFolderID, findError := writer.store.FindProjectFolder(requestContext, connection.UserID, projectID)
	if findError != nil {
		return "", findError
	}
	if projectFolderID != "" && rootUsable {
		usable, usableError := session.FolderUsable(requestContext, projectFolderID)
		if usableError != nil {
			return "", usableError
		}
		if usable {
			return projectFolderID, nil
		}
	}
	projectName, nameError := writer.store.ProjectName(requestContext, projectID)
	if nameError != nil {
		return "", nameError
	}
	createdID, createError := session.CreateFolder(requestContext, projectName, rootID)
	if createError != nil {
		return "", createError
	}
	if saveError := writer.store.SaveProjectFolder(requestContext, connection.UserID, projectID, createdID); saveError != nil {
		return "", saveError
	}
	return createdID, nil
}

// fileName は、日ごとのファイルの名前を作る。
//
// 引数:
//   - host: ingress のホスト名。
//   - day: 日付(2026-11-26)。
//   - part: 分割の番号(1 のときは、付けない)。
//
// 戻り値:
//   - 例: a.example.com_2026-11-26.ndjson、a.example.com_2026-11-26_part2.ndjson。
func fileName(host string, day string, part int) string {
	// 2 つ目以降の分割だけ、番号を付ける。
	if part <= 1 {
		return host + "_" + day + ".ndjson"
	}
	return host + "_" + day + "_part" + strconv.Itoa(part) + ".ndjson"
}

// writeDay は、1 日分のログを、その日のファイルに書く。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - session: Drive の操作。
//   - destination: 転送の設定。
//   - folderID: プロジェクトのフォルダの ID。
//   - day: 日付。
//   - rows: その日のログ(id の昇順)。
//
// 戻り値:
//   - 失敗したとき。
func (writer *DriveWriter) writeDay(requestContext context.Context, session *googledrive.Session, destination model.ExportDestination, folderID string, day string, rows []repository.ExportLogRow) error {
	// すでに書いたファイルの記録を見る。最後に書いた id より後の行だけを、書く。
	existing, findError := writer.store.FindDriveFile(requestContext, destination.ID, day)
	if findError != nil {
		return findError
	}
	var lastWrittenID int64
	if existing != nil {
		lastWrittenID = existing.LastLogID
	}
	var lines []exportlog.Line
	for _, row := range rows {
		if row.ID > lastWrittenID {
			lines = append(lines, row.ToLine())
		}
	}
	if len(lines) == 0 {
		return nil
	}
	added := exportlog.EncodeNDJSON(lines)
	host := rows[0].Host
	// 記録が無ければ、新しいファイル。あっても、大きくなりすぎるなら、次の分割。
	record := repository.DriveFile{DestinationID: destination.ID, Day: day, Part: 1}
	var content []byte
	createNew := existing == nil
	if existing != nil {
		record = *existing
		if existing.Bytes+int64(len(added)) > writer.maxFileBytes {
			record = repository.DriveFile{DestinationID: destination.ID, Day: day, Part: existing.Part + 1}
			createNew = true
		}
	}
	// 既存のファイルの中身を取って、末尾に足す。ファイルが消されていたら、新しく作る。
	if !createNew {
		current, downloadError := session.DownloadFile(requestContext, record.FileID)
		switch {
		case errors.Is(downloadError, googledrive.ErrNotFound):
			createNew = true
			record = repository.DriveFile{DestinationID: destination.ID, Day: day, Part: record.Part}
		case downloadError != nil:
			return downloadError
		default:
			// 末尾に改行が無ければ、足す(行が、つながらないように)。
			if len(current) > 0 && !bytes.HasSuffix(current, []byte("\n")) {
				current = append(current, '\n')
			}
			content = append(current, added...)
		}
	}
	if createNew {
		content = added
		fileID, createError := session.CreateFile(requestContext, fileName(host, day, record.Part), folderID, content)
		if createError != nil {
			return createError
		}
		record.FileID = fileID
		record.LogRows = 0
	} else if updateError := session.UpdateFile(requestContext, record.FileID, content); updateError != nil {
		return updateError
	}
	// 書けたら、記録を更新する(最後に書いた id が、ここで進む)。
	record.Bytes = int64(len(content))
	record.LogRows += int64(len(lines))
	record.LastLogID = lines[len(lines)-1].ID
	if saveError := writer.store.SaveDriveFile(requestContext, &record); saveError != nil {
		return fmt.Errorf("ファイルの記録を保存できませんでした: %w", saveError)
	}
	return nil
}

// WriteTest は、接続を確かめるために、プロジェクトのフォルダに、テスト用のファイルを作る(または、置き換える)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - connection: アカウントの Google Drive の接続。
//   - destination: 転送の設定(google_drive)。
//
// 戻り値:
//   - 失敗したとき。
func (writer *DriveWriter) WriteTest(requestContext context.Context, connection model.GoogleDriveConnection, destination model.ExportDestination) error {
	// 合鍵を復号して、プロジェクトのフォルダを用意する。
	refreshToken, decryptError := writer.cipher.Decrypt(connection.Credential)
	if decryptError != nil {
		return errors.New("Google の合鍵を復号できませんでした。Google Drive に、もう一度接続してください")
	}
	session := writer.client.NewSession(refreshToken)
	folderID, folderError := writer.ensureProjectFolder(requestContext, session, connection, destination.ProjectID)
	if folderError != nil {
		return folderError
	}
	// テスト用のファイルを作る。
	_, createError := session.CreateFile(requestContext, "launchs-connection-test.txt", folderID, []byte("launchs からの、接続のテストです。このファイルは、消してかまいません。\n"))
	return createError
}
