package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	sharedworkflow "launchs/shared/workflow"
)

// TestApplyVolumeSizeWorkflow は、PVC の拡張のあと、成功・失敗のどちらも確定し、失敗なら失敗で終わることを確認する。
func TestApplyVolumeSizeWorkflow(testingT *testing.T) {
	newEnvironment := func() *testsuite.TestWorkflowEnvironment {
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
		testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.ExpandVolumeClaimInput) error { return nil },
			activity.RegisterOptions{Name: sharedworkflow.ExpandVolumeClaimActivityName})
		testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.CommitVolumeSizeResultInput) error { return nil },
			activity.RegisterOptions{Name: sharedworkflow.CommitVolumeSizeResultActivityName})
		return testEnvironment
	}
	expandInput := sharedworkflow.ExpandVolumeClaimInput{VolumeID: "volume-1"}
	succeeded := sharedworkflow.CommitVolumeSizeResultInput{VolumeID: "volume-1", Succeeded: true}
	failed := sharedworkflow.CommitVolumeSizeResultInput{VolumeID: "volume-1", Succeeded: false}
	workflowInput := sharedworkflow.ApplyVolumeSizeInput{VolumeID: "volume-1"}

	testingT.Run("成功すると、成功を確定して、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newEnvironment()
		testEnvironment.OnActivity(sharedworkflow.ExpandVolumeClaimActivityName, mock.Anything, expandInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitVolumeSizeResultActivityName, mock.Anything, succeeded).Return(nil)
		testEnvironment.ExecuteWorkflow(ApplyVolumeSizeWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.CommitVolumeSizeResultActivityName, mock.Anything, failed)
	})
	testingT.Run("一時的に失敗しても、リトライで回復すれば、成功で終わる", func(subTest *testing.T) {
		testEnvironment := newEnvironment()
		testEnvironment.OnActivity(sharedworkflow.ExpandVolumeClaimActivityName, mock.Anything, expandInput).Return(errors.New("k8s down")).Twice()
		testEnvironment.OnActivity(sharedworkflow.ExpandVolumeClaimActivityName, mock.Anything, expandInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitVolumeSizeResultActivityName, mock.Anything, succeeded).Return(nil)
		testEnvironment.ExecuteWorkflow(ApplyVolumeSizeWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功するはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNumberOfCalls(subTest, sharedworkflow.ExpandVolumeClaimActivityName, 3)
	})
	testingT.Run("失敗し続けると、失敗を確定して、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newEnvironment()
		testEnvironment.OnActivity(sharedworkflow.ExpandVolumeClaimActivityName, mock.Anything, expandInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.CommitVolumeSizeResultActivityName, mock.Anything, failed).Return(nil)
		testEnvironment.ExecuteWorkflow(ApplyVolumeSizeWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.CommitVolumeSizeResultActivityName, mock.Anything, failed)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.CommitVolumeSizeResultActivityName, mock.Anything, succeeded)
	})
	testingT.Run("確定に失敗し続けたら、成功した拡張でも、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newEnvironment()
		testEnvironment.OnActivity(sharedworkflow.ExpandVolumeClaimActivityName, mock.Anything, expandInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitVolumeSizeResultActivityName, mock.Anything, succeeded).Return(errors.New("database down"))
		testEnvironment.ExecuteWorkflow(ApplyVolumeSizeWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
	})
}

// TestDeleteVolumeWorkflow は、PVC、レコードの順に削除し、期限まで失敗し続けたら、failed にして失敗で終わることを確認する。
func TestDeleteVolumeWorkflow(testingT *testing.T) {
	newEnvironment := func() *testsuite.TestWorkflowEnvironment {
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
		testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.DeleteVolumeClaimInput) error { return nil },
			activity.RegisterOptions{Name: sharedworkflow.DeleteVolumeClaimActivityName})
		testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.DeleteVolumeRecordInput) error { return nil },
			activity.RegisterOptions{Name: sharedworkflow.DeleteVolumeRecordActivityName})
		testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.MarkVolumeDeleteFailedInput) error { return nil },
			activity.RegisterOptions{Name: sharedworkflow.MarkVolumeDeleteFailedActivityName})
		return testEnvironment
	}
	claimInput := sharedworkflow.DeleteVolumeClaimInput{VolumeID: "volume-1"}
	recordInput := sharedworkflow.DeleteVolumeRecordInput{VolumeID: "volume-1"}
	failedInput := sharedworkflow.MarkVolumeDeleteFailedInput{VolumeID: "volume-1"}
	workflowInput := sharedworkflow.DeleteVolumeInput{VolumeID: "volume-1"}

	testingT.Run("PVC とレコードを削除して、成功で終わる(failed にはしない)", func(subTest *testing.T) {
		testEnvironment := newEnvironment()
		testEnvironment.OnActivity(sharedworkflow.DeleteVolumeClaimActivityName, mock.Anything, claimInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteVolumeRecordActivityName, mock.Anything, recordInput).Return(nil)
		testEnvironment.ExecuteWorkflow(DeleteVolumeWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.MarkVolumeDeleteFailedActivityName, mock.Anything, failedInput)
	})
	testingT.Run("PVC の削除が失敗し続けたら、レコードは消さずに failed にして、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newEnvironment()
		testEnvironment.OnActivity(sharedworkflow.DeleteVolumeClaimActivityName, mock.Anything, claimInput).Return(errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.MarkVolumeDeleteFailedActivityName, mock.Anything, failedInput).Return(nil)
		testEnvironment.ExecuteWorkflow(DeleteVolumeWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.DeleteVolumeRecordActivityName, mock.Anything, recordInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkVolumeDeleteFailedActivityName, mock.Anything, failedInput)
	})
	testingT.Run("レコードの削除が失敗し続けたら、failed にして、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newEnvironment()
		testEnvironment.OnActivity(sharedworkflow.DeleteVolumeClaimActivityName, mock.Anything, claimInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.DeleteVolumeRecordActivityName, mock.Anything, recordInput).Return(errors.New("database down"))
		testEnvironment.OnActivity(sharedworkflow.MarkVolumeDeleteFailedActivityName, mock.Anything, failedInput).Return(nil)
		testEnvironment.ExecuteWorkflow(DeleteVolumeWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.MarkVolumeDeleteFailedActivityName, mock.Anything, failedInput)
	})
}

// TestApplyDeployMountsWorkflow は、更新・起動の待機の順に進み、失敗したら、元のマウントに戻してから失敗を確定して失敗で終わることを確認する。
func TestApplyDeployMountsWorkflow(testingT *testing.T) {
	newEnvironment := func() *testsuite.TestWorkflowEnvironment {
		testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
		testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.ApplyPendingMountsInput) (int64, error) { return 0, nil },
			activity.RegisterOptions{Name: sharedworkflow.ApplyPendingMountsActivityName})
		testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.WaitDeploymentReadyInput) error { return nil },
			activity.RegisterOptions{Name: sharedworkflow.WaitDeploymentReadyActivityName})
		testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.RollbackDeploymentMountsInput) error { return nil },
			activity.RegisterOptions{Name: sharedworkflow.RollbackDeploymentMountsActivityName})
		testEnvironment.RegisterActivityWithOptions(func(context.Context, sharedworkflow.CommitMountsResultInput) error { return nil },
			activity.RegisterOptions{Name: sharedworkflow.CommitMountsResultActivityName})
		return testEnvironment
	}
	applyInput := sharedworkflow.ApplyPendingMountsInput{DeployID: "deploy-1"}
	waitInput := sharedworkflow.WaitDeploymentReadyInput{DeployID: "deploy-1", MinGeneration: 7}
	rollbackInput := sharedworkflow.RollbackDeploymentMountsInput{DeployID: "deploy-1"}
	succeeded := sharedworkflow.CommitMountsResultInput{DeployID: "deploy-1", Succeeded: true}
	failed := sharedworkflow.CommitMountsResultInput{DeployID: "deploy-1", Succeeded: false}
	workflowInput := sharedworkflow.ApplyDeployMountsInput{DeployID: "deploy-1"}

	testingT.Run("成功すると、更新後の世代で起動を待ち、成功を確定して、成功で終わる(巻き戻しはしない)", func(subTest *testing.T) {
		testEnvironment := newEnvironment()
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingMountsActivityName, mock.Anything, applyInput).Return(int64(7), nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitMountsResultActivityName, mock.Anything, succeeded).Return(nil)
		testEnvironment.ExecuteWorkflow(ApplyDeployMountsWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() != nil {
			subTest.Fatalf("成功で終わるはず: %v", testEnvironment.GetWorkflowError())
		}
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.RollbackDeploymentMountsActivityName, mock.Anything, rollbackInput)
	})
	testingT.Run("更新が失敗し続けると、元のマウントに戻し、失敗を確定して、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newEnvironment()
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingMountsActivityName, mock.Anything, applyInput).Return(int64(0), errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.RollbackDeploymentMountsActivityName, mock.Anything, rollbackInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitMountsResultActivityName, mock.Anything, failed).Return(nil)
		testEnvironment.ExecuteWorkflow(ApplyDeployMountsWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.RollbackDeploymentMountsActivityName, mock.Anything, rollbackInput)
		testEnvironment.AssertCalled(subTest, sharedworkflow.CommitMountsResultActivityName, mock.Anything, failed)
		testEnvironment.AssertNotCalled(subTest, sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, mock.Anything)
	})
	testingT.Run("起動し終えなかったら、元のマウントに戻し、失敗を確定して、失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newEnvironment()
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingMountsActivityName, mock.Anything, applyInput).Return(int64(7), nil)
		testEnvironment.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(errors.New("not ready"))
		testEnvironment.OnActivity(sharedworkflow.RollbackDeploymentMountsActivityName, mock.Anything, rollbackInput).Return(nil)
		testEnvironment.OnActivity(sharedworkflow.CommitMountsResultActivityName, mock.Anything, failed).Return(nil)
		testEnvironment.ExecuteWorkflow(ApplyDeployMountsWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.RollbackDeploymentMountsActivityName, mock.Anything, rollbackInput)
	})
	testingT.Run("元に戻せなくても、失敗を確定して、失敗で終わる。確定に失敗し続けたら、成功でも失敗で終わる", func(subTest *testing.T) {
		testEnvironment := newEnvironment()
		testEnvironment.OnActivity(sharedworkflow.ApplyPendingMountsActivityName, mock.Anything, applyInput).Return(int64(0), errors.New("k8s down"))
		testEnvironment.OnActivity(sharedworkflow.RollbackDeploymentMountsActivityName, mock.Anything, rollbackInput).Return(errors.New("still down"))
		testEnvironment.OnActivity(sharedworkflow.CommitMountsResultActivityName, mock.Anything, failed).Return(nil)
		testEnvironment.ExecuteWorkflow(ApplyDeployMountsWorkflow, workflowInput)
		if testEnvironment.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
		testEnvironment.AssertCalled(subTest, sharedworkflow.CommitMountsResultActivityName, mock.Anything, failed)
		commitFailing := newEnvironment()
		commitFailing.OnActivity(sharedworkflow.ApplyPendingMountsActivityName, mock.Anything, applyInput).Return(int64(7), nil)
		commitFailing.OnActivity(sharedworkflow.WaitDeploymentReadyActivityName, mock.Anything, waitInput).Return(nil)
		commitFailing.OnActivity(sharedworkflow.CommitMountsResultActivityName, mock.Anything, succeeded).Return(errors.New("database down"))
		commitFailing.ExecuteWorkflow(ApplyDeployMountsWorkflow, workflowInput)
		if commitFailing.GetWorkflowError() == nil {
			subTest.Fatalf("失敗で終わるはず")
		}
	})
}
