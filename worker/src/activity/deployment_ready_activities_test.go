package activity

import (
	"context"
	"errors"
	"launchs/shared/model"
	"launchs/shared/workflow"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"
)

// readerStep は、fakeObservationReader の、1回分の確認(1回の待ちの周回)で返す内容。
type readerStep struct {
	// hasHeartbeat が true なら、心拍があり、その経過時間が age。false なら、心拍が一度も無い。
	hasHeartbeat bool
	age          time.Duration
	// observation は、観測値(hasObservation が false なら、まだ観測されていない)。
	observation    model.DeployObservation
	hasObservation bool
	// err は、問い合わせの失敗。
	err error
}

// fakeObservationReader は、テスト用の ObservationReader。1回の確認ごとに、決めた内容を順に返す(最後のものは繰り返す)。
// 何も決めなければ、心拍が一度も無い(watcher が動いていない)ものとして答える。
type fakeObservationReader struct {
	steps          []readerStep
	heartbeatCalls int
	findCalls      int
}

// current は、いまの周回で返す内容を返す。
func (fakeReader *fakeObservationReader) current() readerStep {
	// 何も決めていないときは、心拍が無い。
	if len(fakeReader.steps) == 0 {
		return readerStep{}
	}
	// 周回の数に対応する内容を返す(足りなければ、最後のもの)。
	index := fakeReader.heartbeatCalls - 1
	if index >= len(fakeReader.steps) {
		index = len(fakeReader.steps) - 1
	}
	return fakeReader.steps[index]
}

// HeartbeatAge は、周回を1つ進めて、心拍の有無と経過時間を返す。
func (fakeReader *fakeObservationReader) HeartbeatAge(_ context.Context) (time.Duration, bool, error) {
	fakeReader.heartbeatCalls++
	step := fakeReader.current()
	return step.age, step.hasHeartbeat, step.err
}

// FindDeployment は、いまの周回の観測値を返す。
func (fakeReader *fakeObservationReader) FindDeployment(_ context.Context, _ string) (model.DeployObservation, bool, error) {
	fakeReader.findCalls++
	step := fakeReader.current()
	return step.observation, step.hasObservation, step.err
}

// TestWaitDeploymentReady は、watcher の心拍が無いとき、k8s に直接問い合わせて、Ready になるまで確かめ続け、
// 確認の失敗はエラーにすることを確認する。
func TestWaitDeploymentReady(testingT *testing.T) {
	// すぐ Ready・待つ・確認の失敗の場合。
	testCases := []struct {
		name           string
		fakeOperator   *fakeDeploymentOperator
		expectedChecks int
		expectError    bool
	}{
		{name: "心拍が無ければ、k8s に問い合わせ、すぐ Ready なら、1回の確認で終わる", fakeOperator: &fakeDeploymentOperator{}, expectedChecks: 1},
		{name: "Ready になるまで、確認を繰り返して待つ", fakeOperator: &fakeDeploymentOperator{notReadyChecks: 3}, expectedChecks: 4},
		{name: "確認のエラーはそのまま返す", fakeOperator: &fakeDeploymentOperator{readyError: errors.New("k8s down")}, expectedChecks: 1, expectError: true},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// Activity をテスト環境で実行する。
			activities := newTestActivities(&fakeDeployStore{deploy: newTestDeploy()}, testCase.fakeOperator)
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.WaitDeploymentReady)
			_, executeError := testEnvironment.ExecuteActivity(activities.WaitDeploymentReady, workflow.WaitDeploymentReadyInput{DeployID: "deploy-1"})

			// 確認の回数と、エラーの有無を確認する。
			if testCase.fakeOperator.readyChecks != testCase.expectedChecks {
				subTest.Errorf("確認の回数が違う: 期待 %d、実際 %d", testCase.expectedChecks, testCase.fakeOperator.readyChecks)
			}
			if (executeError != nil) != testCase.expectError {
				subTest.Errorf("エラーの有無が違う: %v", executeError)
			}
		})
	}
}

// TestWaitDeploymentReadyFromObservation は、watcher の心拍が新しいとき、DB の観測値で判断する(k8s には問い合わせない)こと、
// 更新直後の古い観測値を信用しないこと、心拍が古くなったら k8s に切り替えることを確認する。
func TestWaitDeploymentReadyFromObservation(testingT *testing.T) {
	// 希望するレプリカ数が 2 の deploy で、世代 3 以上の Ready を待つ。観測値を作る道具。
	fresh := 5 * time.Second
	observation := func(generation int64, observedGeneration int64, replicas int, updated int, ready int, available int) model.DeployObservation {
		return model.DeployObservation{DeployID: "deploy-1", Generation: generation, ObservedGeneration: observedGeneration,
			Replicas: replicas, UpdatedReplicas: updated, ReadyReplicas: ready, AvailableReplicas: available}
	}
	readyObservation := readerStep{hasHeartbeat: true, age: fresh, observation: observation(3, 3, 2, 2, 2, 2), hasObservation: true}

	testCases := []struct {
		name                string
		steps               []readerStep
		fakeOperator        *fakeDeploymentOperator
		expectError         bool
		expectedHeartbeats  int
		expectedK8sChecks   int
		expectedFindCallsAt int
	}{
		{name: "心拍が新しく、観測値が Ready なら、k8s に問い合わせず、1回で終わる",
			steps: []readerStep{readyObservation}, fakeOperator: &fakeDeploymentOperator{}, expectedHeartbeats: 1, expectedK8sChecks: 0},
		{name: "更新直後の古い設定の観測値(世代が下限より小さい)は信用せず、新しい世代になるまで待つ",
			steps: []readerStep{
				{hasHeartbeat: true, age: fresh, observation: observation(2, 2, 2, 2, 2, 2), hasObservation: true},
				{hasHeartbeat: true, age: fresh, observation: observation(2, 2, 2, 2, 2, 2), hasObservation: true},
				readyObservation},
			fakeOperator: &fakeDeploymentOperator{}, expectedHeartbeats: 3, expectedK8sChecks: 0},
		{name: "k8s が新しい設定をまだ処理していない(observedGeneration が小さい)間は待つ",
			steps: []readerStep{
				{hasHeartbeat: true, age: fresh, observation: observation(3, 2, 2, 2, 2, 2), hasObservation: true},
				readyObservation},
			fakeOperator: &fakeDeploymentOperator{}, expectedHeartbeats: 2, expectedK8sChecks: 0},
		{name: "観測値がまだ無いときは、現れるまで待つ",
			steps:        []readerStep{{hasHeartbeat: true, age: fresh}, readyObservation},
			fakeOperator: &fakeDeploymentOperator{}, expectedHeartbeats: 2, expectedK8sChecks: 0},
		{name: "レプリカ数が希望に届かない間は待つ(準備完了が1つ足りない)",
			steps: []readerStep{
				{hasHeartbeat: true, age: fresh, observation: observation(3, 3, 2, 2, 1, 1), hasObservation: true},
				readyObservation},
			fakeOperator: &fakeDeploymentOperator{}, expectedHeartbeats: 2, expectedK8sChecks: 0},
		{name: "古い Pod が残っている(総数が希望より多い)間は待つ",
			steps: []readerStep{
				{hasHeartbeat: true, age: fresh, observation: observation(3, 3, 3, 2, 2, 2), hasObservation: true},
				readyObservation},
			fakeOperator: &fakeDeploymentOperator{}, expectedHeartbeats: 2, expectedK8sChecks: 0},
		{name: "心拍が古ければ、観測値は信用せず、k8s に問い合わせる",
			steps:        []readerStep{{hasHeartbeat: true, age: time.Hour, observation: observation(9, 9, 0, 0, 0, 0), hasObservation: true}},
			fakeOperator: &fakeDeploymentOperator{}, expectedHeartbeats: 1, expectedK8sChecks: 1},
		{name: "心拍が、止まる直前(しきい値ちょうど)なら、k8s に問い合わせる",
			steps:        []readerStep{{hasHeartbeat: true, age: model.WatcherHeartbeatStaleAfter, observation: readyObservation.observation, hasObservation: true}},
			fakeOperator: &fakeDeploymentOperator{}, expectedHeartbeats: 1, expectedK8sChecks: 1},
		{name: "待っている途中で watcher が止まったら、k8s に切り替えて、Ready なら終わる",
			steps: []readerStep{
				{hasHeartbeat: true, age: fresh, observation: observation(3, 3, 2, 2, 1, 1), hasObservation: true},
				{hasHeartbeat: true, age: time.Hour}},
			fakeOperator: &fakeDeploymentOperator{}, expectedHeartbeats: 2, expectedK8sChecks: 1},
		{name: "心拍の問い合わせの失敗は、エラーとして返す",
			steps: []readerStep{{err: errors.New("database down")}}, fakeOperator: &fakeDeploymentOperator{}, expectError: true, expectedHeartbeats: 1},
		{name: "観測値の問い合わせの失敗は、エラーとして返す",
			steps: []readerStep{{hasHeartbeat: true, age: fresh, err: errors.New("database down")}}, fakeOperator: &fakeDeploymentOperator{}, expectError: true, expectedHeartbeats: 1},
		{name: "心拍が古く、k8s への問い合わせも失敗したら、エラーとして返す",
			steps: []readerStep{{hasHeartbeat: false}}, fakeOperator: &fakeDeploymentOperator{readyError: errors.New("k8s down")}, expectError: true, expectedHeartbeats: 1, expectedK8sChecks: 1},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			// replicas が 2 の deploy で、Activity を作り、読み取り先を差し替える。
			fakeReader := &fakeObservationReader{steps: testCase.steps}
			activities := newTestActivities(&fakeDeployStore{deploy: newTestDeploy()}, testCase.fakeOperator)
			activities.observationReader = fakeReader
			testEnvironment := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			testEnvironment.RegisterActivity(activities.WaitDeploymentReady)

			_, executeError := testEnvironment.ExecuteActivity(activities.WaitDeploymentReady, workflow.WaitDeploymentReadyInput{DeployID: "deploy-1", MinGeneration: 3})

			if (executeError != nil) != testCase.expectError {
				subTest.Errorf("エラーの有無が違う: %v", executeError)
			}
			if fakeReader.heartbeatCalls != testCase.expectedHeartbeats {
				subTest.Errorf("確認の周回の数が違う: 期待 %d、実際 %d", testCase.expectedHeartbeats, fakeReader.heartbeatCalls)
			}
			if testCase.fakeOperator.readyChecks != testCase.expectedK8sChecks {
				subTest.Errorf("k8s への問い合わせの回数が違う: 期待 %d、実際 %d", testCase.expectedK8sChecks, testCase.fakeOperator.readyChecks)
			}
		})
	}
}

// TestObservationReady は、観測値が Ready とみなされる条件(世代と、全てのレプリカ数)を、境界を含めて確認する。
func TestObservationReady(testingT *testing.T) {
	// 希望するレプリカ数、世代の下限、観測値ごとの期待。
	testCases := []struct {
		name        string
		observation model.DeployObservation
		desired     int
		minimum     int64
		expected    bool
	}{
		{"全て揃っていれば Ready", model.DeployObservation{Generation: 3, ObservedGeneration: 3, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2}, 2, 3, true},
		{"世代が下限より新しくても、処理済みなら Ready", model.DeployObservation{Generation: 4, ObservedGeneration: 4, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2}, 2, 3, true},
		{"世代が下限より小さければ Ready ではない", model.DeployObservation{Generation: 2, ObservedGeneration: 2, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2}, 2, 3, false},
		{"処理済みの世代が追いついていなければ Ready ではない", model.DeployObservation{Generation: 3, ObservedGeneration: 2, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2}, 2, 3, false},
		{"最新の設定の Pod が足りなければ Ready ではない", model.DeployObservation{Generation: 3, ObservedGeneration: 3, Replicas: 2, UpdatedReplicas: 1, ReadyReplicas: 2, AvailableReplicas: 2}, 2, 3, false},
		{"使える Pod が足りなければ Ready ではない", model.DeployObservation{Generation: 3, ObservedGeneration: 3, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 1}, 2, 3, false},
		{"0 に減らすときは、Pod が全て無くなれば Ready", model.DeployObservation{Generation: 3, ObservedGeneration: 3}, 0, 3, true},
		{"0 に減らすとき、Pod が残っていれば Ready ではない", model.DeployObservation{Generation: 3, ObservedGeneration: 3, Replicas: 1, UpdatedReplicas: 1}, 0, 3, false},
		{"下限が 0(世代を使わない)なら、世代の条件は、処理済みだけ", model.DeployObservation{Generation: 1, ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1}, 1, 0, true},
	}
	for _, testCase := range testCases {
		testingT.Run(testCase.name, func(subTest *testing.T) {
			if got := observationReady(testCase.observation, testCase.desired, testCase.minimum); got != testCase.expected {
				subTest.Errorf("期待 %v、実際 %v", testCase.expected, got)
			}
		})
	}
}
