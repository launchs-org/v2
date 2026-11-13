package workflow

import (
	"strings"
	"testing"
)

// TestNewCreateDeployWorkflowID は、deploy 作成の Workflow ID が「操作-リソースID-uuid」の形で、
// 呼ぶたびに違う値になることを確認する。
func TestNewCreateDeployWorkflowID(testingT *testing.T) {
	// 同じ deploy で2回作る。
	firstID := NewCreateDeployWorkflowID("deploy-abc")
	secondID := NewCreateDeployWorkflowID("deploy-abc")

	// 操作名とリソース ID が先頭に入り、末尾に UUID(36文字)が付くこと。
	if !strings.HasPrefix(firstID, "create-deploy-abc-") || len(firstID) != len("create-deploy-abc-")+36 {
		testingT.Errorf("形が違う: %q", firstID)
	}
	// 操作ごとに一意であること。
	if firstID == secondID {
		testingT.Errorf("2回とも同じ ID になった: %q", firstID)
	}
}
