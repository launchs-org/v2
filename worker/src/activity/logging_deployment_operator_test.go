package activity

import (
	"bytes"
	"context"
	"launchs/shared/logger"
	"log/slog"
	"testing"
)

// TestLoggingDeploymentOperator は、仮の実装が、何も作らず、常に Ready を返し、警告のログを出すことを確認する。
func TestLoggingDeploymentOperator(testingT *testing.T) {
	// ログの出力先を、テストで読める入れ物にする。
	var logOutput bytes.Buffer
	loggingOperator := NewLoggingDeploymentOperator(logger.New(&logOutput, "worker", slog.LevelInfo))

	// 3つの操作を呼ぶ。
	ensureError := loggingOperator.EnsureNamespace(context.Background(), "project-1")
	generation, applyError := loggingOperator.ApplyDeployment(context.Background(), DeploymentDefinition{Namespace: "project-1", Name: "deploy-1"})
	ready, readyError := loggingOperator.DeploymentReady(context.Background(), "project-1", "deploy-1")

	// どれもエラーにならず、Ready で、警告のログが出ていること。
	if ensureError != nil || applyError != nil || readyError != nil || !ready || generation != 0 {
		testingT.Errorf("エラーにならず、Ready のはず: %v %v %v %v", ensureError, applyError, readyError, ready)
	}
	if bytes.Count(logOutput.Bytes(), []byte(`"level":"WARN"`)) != 2 {
		testingT.Errorf("警告のログが2回出るはず: %s", logOutput.String())
	}
}
