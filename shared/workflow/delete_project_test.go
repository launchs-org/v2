package workflow

import (
	"strings"
	"testing"
)

// TestNewDeleteProjectWorkflowID は、Workflow ID が「操作-リソースID-uuid」の形で、
// 呼ぶたびに違う値になることを確認する。
func TestNewDeleteProjectWorkflowID(testingT *testing.T) {
	// 同じプロジェクトで2回作る。
	firstID := NewDeleteProjectWorkflowID("project-abc")
	secondID := NewDeleteProjectWorkflowID("project-abc")

	// 操作名とリソース ID が先頭に入っていること。
	if !strings.HasPrefix(firstID, "delete-project-abc-") {
		testingT.Errorf("形が違う: %q", firstID)
	}
	// 末尾の UUID(36文字)が付いていること。
	if len(firstID) != len("delete-project-abc-")+36 {
		testingT.Errorf("UUID が付いていない: %q", firstID)
	}
	// 操作ごとに一意であること。
	if firstID == secondID {
		testingT.Errorf("2回とも同じ ID になった: %q", firstID)
	}
}
