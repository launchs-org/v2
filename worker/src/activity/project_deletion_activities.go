// Package activity は、Temporal の Activity(外部への入出力を行う処理)を実装する。
//
// Activity は、繰り返し実行されても壊れないよう、冪等に書く。
package activity

import (
	"context"
	"log/slog"
	"time"

	"go.temporal.io/sdk/activity"

	"launchs/shared/workflow"
)

// defaultNamespacePollInterval は、namespace が消えたかを確かめる、既定の間隔。
const defaultNamespacePollInterval = 2 * time.Second

// NamespaceDeleter は、プロジェクトの k8s の namespace を削除する。
//
// 実体は、client-go で namespace を削除するもの(別のPRで作る)。
// 今は、何も削除せずログを出すだけの LoggingNamespaceDeleter を使う。
type NamespaceDeleter interface {
	// DeleteNamespace は、namespace を削除する。すでに無いときは、成功として扱う(冪等)。
	DeleteNamespace(requestContext context.Context, namespaceName string) error

	// NamespaceExists は、namespace が(削除中のものを含めて)まだ残っているかを返す。
	NamespaceExists(requestContext context.Context, namespaceName string) (bool, error)
}

// ProjectRecordDeleter は、プロジェクトのレコードを DB から削除する。
//
// 実体は shared/repository の ProjectRepository。
type ProjectRecordDeleter interface {
	// DeleteRecord は、削除中のプロジェクトの行を物理削除する。すでに無いときは、成功として扱う(冪等)。
	DeleteRecord(requestContext context.Context, projectID string) error
}

// ProjectDeletionActivities は、プロジェクト削除の Workflow が呼ぶ Activity をまとめる。
type ProjectDeletionActivities struct {
	namespaceDeleter     NamespaceDeleter
	projectRecordDeleter ProjectRecordDeleter
	appLogger            *slog.Logger
	// namespacePollInterval は、namespace が消えたかを確かめる間隔。
	namespacePollInterval time.Duration
}

// NewProjectDeletionActivities は、ProjectDeletionActivities を作る。
//
// 引数:
//   - namespaceDeleter: k8s の namespace を削除するもの。
//   - projectRecordDeleter: プロジェクトのレコードを削除するもの。
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した ProjectDeletionActivities。
func NewProjectDeletionActivities(namespaceDeleter NamespaceDeleter, projectRecordDeleter ProjectRecordDeleter, appLogger *slog.Logger) *ProjectDeletionActivities {
	return &ProjectDeletionActivities{
		namespaceDeleter:      namespaceDeleter,
		projectRecordDeleter:  projectRecordDeleter,
		appLogger:             appLogger,
		namespacePollInterval: defaultNamespacePollInterval,
	}
}

// DeleteProjectNamespace は、プロジェクトの k8s の namespace を削除する。
//
// namespace の名前は projectId と同じ。namespace を消すと、配下の deploy、service、
// ingress、volume(PVC のデータを含む)も、まとめて消える。すでに無ければ成功とする。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 削除するプロジェクトの ID。
//
// 戻り値:
//   - namespace の削除に失敗したときのエラー(Temporal がリトライする)。
func (projectDeletionActivities *ProjectDeletionActivities) DeleteProjectNamespace(requestContext context.Context, input workflow.DeleteProjectNamespaceInput) error {
	// ログに、どの Activity が何のために動いているかを残す。
	projectDeletionActivities.appLogger.Info("プロジェクトの namespace を削除する",
		"project_id", input.ProjectID, "activity", workflow.DeleteProjectNamespaceActivityName)
	// 削除を頼む(namespace 名は projectId)。すでに無ければ、成功として扱われる。
	if deleteError := projectDeletionActivities.namespaceDeleter.DeleteNamespace(requestContext, input.ProjectID); deleteError != nil {
		return deleteError
	}

	// k8s の namespace の削除は非同期なので、配下のリソース(PVC のデータを含む)を含めて、
	// 完全に消えるまで待つ。待つ間は、Temporal に、生きていることを伝え続ける。
	for {
		// namespace がまだ残っているかを確かめる。
		exists, existsError := projectDeletionActivities.namespaceDeleter.NamespaceExists(requestContext, input.ProjectID)
		if existsError != nil {
			return existsError
		}
		// 消えていれば、完了。
		if !exists {
			return nil
		}
		// まだ残っているので、生きていることを伝えて、少し待つ(キャンセルや期限なら、そこで終わる)。
		activity.RecordHeartbeat(requestContext)
		select {
		case <-requestContext.Done():
			return requestContext.Err()
		case <-time.After(projectDeletionActivities.namespacePollInterval):
		}
	}
}

// DeleteProjectRecords は、プロジェクトの行を DB から削除する。
//
// 今は projects テーブルだけ。build や deploy などのテーブルができたら、配下の行も、
// ここで同じトランザクションの中で削除する。
//
// 引数:
//   - requestContext: Temporal が渡す context(キャンセルと期限が伝わる)。
//   - input: 削除するプロジェクトの ID。
//
// 戻り値:
//   - 削除に失敗したときのエラー(Temporal がリトライする)。
func (projectDeletionActivities *ProjectDeletionActivities) DeleteProjectRecords(requestContext context.Context, input workflow.DeleteProjectRecordsInput) error {
	// ログに、どの Activity が何のために動いているかを残す。
	projectDeletionActivities.appLogger.Info("プロジェクトのレコードを削除する",
		"project_id", input.ProjectID, "activity", workflow.DeleteProjectRecordsActivityName)
	// Temporal に、この Activity が生きていることを伝える(短い処理だが、決まりとして送る)。
	activity.RecordHeartbeat(requestContext)
	// 行の削除は、レコードを削除するものに任せる。
	return projectDeletionActivities.projectRecordDeleter.DeleteRecord(requestContext, input.ProjectID)
}

// LoggingNamespaceDeleter は、namespace を削除せず、ログに出すだけの NamespaceDeleter。
//
// k8s への接続(client-go)ができるまでの、開発用の仮の実装。本番では使わない。
type LoggingNamespaceDeleter struct {
	appLogger *slog.Logger
}

// NewLoggingNamespaceDeleter は、LoggingNamespaceDeleter を作る。
//
// 引数:
//   - appLogger: ログを出すロガー。
//
// 戻り値:
//   - 作成した LoggingNamespaceDeleter。
func NewLoggingNamespaceDeleter(appLogger *slog.Logger) *LoggingNamespaceDeleter {
	return &LoggingNamespaceDeleter{appLogger: appLogger}
}

// DeleteNamespace は、削除する予定の namespace の名前を、警告のログに出すだけで、何も削除しない。
//
// 引数:
//   - requestContext: 使わない。
//   - namespaceName: 削除する予定の namespace の名前。
//
// 戻り値:
//   - 常に nil。
func (loggingNamespaceDeleter *LoggingNamespaceDeleter) DeleteNamespace(_ context.Context, namespaceName string) error {
	// 実際には削除していないことが分かるよう、警告で出す。
	loggingNamespaceDeleter.appLogger.Warn("k8s に未接続のため、namespace は削除しない(仮の実装)", "namespace", namespaceName)
	return nil
}

// NamespaceExists は、仮の実装なので、何も作っていない・消していないため、常に「無い」を返す。
//
// 引数:
//   - requestContext: 使わない。
//   - namespaceName: 使わない。
//
// 戻り値:
//   - 常に false と nil。
func (loggingNamespaceDeleter *LoggingNamespaceDeleter) NamespaceExists(_ context.Context, _ string) (bool, error) {
	// 何も削除していないので、待つ必要は無く、常に無いものとして扱う。
	return false, nil
}
