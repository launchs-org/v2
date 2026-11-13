package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"

	sharedworkflow "launchs/shared/workflow"
)

// TestTemporalProjectDeletionStarter は、正しい名前・Task Queue・入力で Workflow を開始し、
// 開始のエラーを返すことを確認する。
func TestTemporalProjectDeletionStarter(testingT *testing.T) {
	// 成功する場合と、失敗する場合。
	testCases := []struct {
		name         string
		executeError error
	}{
		{name: "Workflow を開始する"},
		{name: "開始のエラーは返す", executeError: errors.New("temporal down")},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// Temporal のクライアントを、モックにする。渡されたオプションを覚える。
			var startedOptions client.StartWorkflowOptions
			mockClient := &temporalmocks.Client{}
			mockClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, sharedworkflow.DeleteProjectWorkflowName, sharedworkflow.DeleteProjectInput{ProjectID: "project-1"}).
				Run(func(arguments mock.Arguments) { startedOptions = arguments.Get(1).(client.StartWorkflowOptions) }).
				Return(nil, testCase.executeError)

			// 開始を頼む。
			startError := NewTemporalProjectDeletionStarter(mockClient).StartProjectDeletion(context.Background(), "project-1")

			// エラーの有無と、渡されたオプションを確認する。
			if (startError != nil) != (testCase.executeError != nil) {
				subTest.Errorf("エラーの有無が違う: %v", startError)
			}
			if !strings.HasPrefix(startedOptions.ID, "delete-project-1-") || startedOptions.TaskQueue != sharedworkflow.TaskQueueGeneral {
				subTest.Errorf("オプションが違う: %+v", startedOptions)
			}
		})
	}
}
