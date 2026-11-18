package repository

import (
	"context"
	"testing"
	"time"

	"launchs/shared/apperror"
	"launchs/shared/database/testdatabase"
	"launchs/shared/model"
)

// newTestTask は、試験用のタスクを作る。
func newTestTask(projectID string, index int, workflowID string, parentWorkflowID string, startedAt time.Time) model.ProjectTask {
	// 区別できる ID を付ける。
	return model.ProjectTask{ID: model.ProjectTaskIDPrefix + string(rune('a'+index)), ProjectID: projectID, UserID: "user-a", ProjectName: "my-app", Kind: "deploy_create", WorkflowType: "CreateDeployWorkflow",
		ResourceID: "deploy-1", WorkflowID: workflowID, RunID: "run-1", ParentWorkflowID: parentWorkflowID, StartedAt: startedAt}
}

// TestProjectTaskRepository は、記録(二重にならない)、結果の更新、一覧(子を除く・絞り込み・ページング)、詳細(子つき)、数、削除、持ち主の確認を確認する。
func TestProjectTaskRepository(testingT *testing.T) {
	ctx := context.Background()
	database := testdatabase.New(testingT)
	repo := NewProjectTaskRepository(database)
	project := model.Project{ID: "project-00000001", UserID: "user-a", Name: "my-app", Status: model.ProjectStatusActive}
	database.Create(&project)
	deploy := newTestDeploy(1, project.ID, "user-a")
	database.Create(deploy)
	now := time.Now().UTC().Truncate(time.Millisecond)

	testingT.Run("入力から、プロジェクトを決める", func(subTest *testing.T) {
		target, found, err := repo.ResolveProject(ctx, map[string]any{"DeployID": deploy.ID})
		if err != nil || !found || target.ProjectID != project.ID || target.ResourceID != deploy.ID || target.UserID != "user-a" || target.ProjectName != "my-app" {
			subTest.Errorf("deploy から、持ち主と名前まで決まるはず: %+v %v %v", target, found, err)
		}
		if target, found, _ := repo.ResolveProject(ctx, map[string]any{"ProjectID": project.ID}); !found || target.ResourceID != project.ID {
			subTest.Error("ProjectID は、そのプロジェクト自身が対象のはず")
		}
		if _, found, _ := repo.ResolveProject(ctx, map[string]any{"ProjectID": "project-none"}); found {
			subTest.Error("プロジェクトが無ければ、見つからないはず")
		}
		if _, found, _ := repo.ResolveProject(ctx, map[string]any{"DeployID": "deploy-none"}); found {
			subTest.Error("リソースが無ければ、見つからないはず")
		}
		if _, found, _ := repo.ResolveProject(ctx, map[string]any{"Other": "x"}); found {
			subTest.Error("知らない項目だけなら、見つからないはず")
		}
	})

	// 親と子のタスクと、実行中、失敗、成功のタスクを用意する。
	parent := newTestTask(project.ID, 0, "wf-parent", "", now.Add(-10*time.Minute))
	child := newTestTask(project.ID, 1, "wf-child", "wf-parent", now.Add(-9*time.Minute))
	failed := newTestTask(project.ID, 2, "wf-failed", "", now.Add(-5*time.Minute))
	running := newTestTask(project.ID, 3, "wf-running", "", now.Add(-time.Minute))
	stale := newTestTask(project.ID, 4, "wf-stale", "", now.Add(-3*time.Hour))
	for _, task := range []model.ProjectTask{parent, child, failed, running, stale} {
		if err := repo.Start(ctx, task); err != nil {
			testingT.Fatal(err)
		}
	}
	// 同じ Workflow の実行を、もう一度記録しても、二重にならない。
	if err := repo.Start(ctx, parent); err != nil {
		testingT.Fatal(err)
	}
	_ = repo.Finish(ctx, "wf-parent", "run-1", model.TaskStatusSucceeded, "", now)
	_ = repo.Finish(ctx, "wf-child", "run-1", model.TaskStatusSucceeded, "", now)
	_ = repo.Finish(ctx, "wf-failed", "run-1", model.TaskStatusFailed, "イメージを取得できません", now)
	// 終わったタスクを、もう一度終わらせても、変わらない。
	_ = repo.Finish(ctx, "wf-failed", "run-1", model.TaskStatusSucceeded, "", now)

	testingT.Run("一覧は、トップのタスクだけを、新しい順に返す", func(subTest *testing.T) {
		tasks, next, err := repo.List(ctx, "user-a", project.ID, TaskFilter{}, 10, nil, now)
		if err != nil || next != nil || len(tasks) != 4 || tasks[0].WorkflowID != "wf-running" || tasks[3].WorkflowID != "wf-stale" {
			subTest.Fatalf("子を除く 4 件が、新しい順のはず: %v %+v", err, tasks)
		}
		if tasks[1].Status != model.TaskStatusFailed || tasks[1].Error != "イメージを取得できません" {
			subTest.Errorf("失敗の理由が入るはず: %+v", tasks[1])
		}
	})
	testingT.Run("絞り込み(実行中は lost を含まない)", func(subTest *testing.T) {
		for status, want := range map[string]int{"running": 1, "lost": 1, "failed": 1, "succeeded": 1, "canceled": 0} {
			tasks, _, err := repo.List(ctx, "user-a", project.ID, TaskFilter{Status: status}, 10, nil, now)
			if err != nil || len(tasks) != want {
				subTest.Errorf("%s: %d 件のはず: %v %d", status, want, err, len(tasks))
			}
		}
		if _, _, err := repo.List(ctx, "user-a", project.ID, TaskFilter{Status: "bad"}, 10, nil, now); apperror.KindOf(err) != apperror.KindInvalidRequest {
			subTest.Errorf("不正な status は invalid_request: %v", err)
		}
		if tasks, _, _ := repo.List(ctx, "user-a", project.ID, TaskFilter{Kind: "service_create"}, 10, nil, now); len(tasks) != 0 {
			subTest.Error("種類で絞れるはず")
		}
	})
	testingT.Run("ページング", func(subTest *testing.T) {
		first, cursor, _ := repo.List(ctx, "user-a", project.ID, TaskFilter{}, 3, nil, now)
		rest, last, _ := repo.List(ctx, "user-a", project.ID, TaskFilter{}, 3, cursor, now)
		if len(first) != 3 || cursor == nil || len(rest) != 1 || last != nil || rest[0].WorkflowID != "wf-stale" {
			subTest.Errorf("3 件と 1 件に分かれるはず: %d %d", len(first), len(rest))
		}
	})
	testingT.Run("詳細は、子のタスクを含む", func(subTest *testing.T) {
		task, children, err := repo.Get(ctx, "user-a", project.ID, parent.ID)
		if err != nil || task.WorkflowID != "wf-parent" || len(children) != 1 || children[0].WorkflowID != "wf-child" {
			subTest.Errorf("子が 1 つのはず: %v %+v", err, children)
		}
	})
	testingT.Run("数", func(subTest *testing.T) {
		counts, err := repo.Counts(ctx, "user-a", project.ID, now)
		if err != nil || counts.Running != 1 || counts.FailedRecent != 1 {
			subTest.Errorf("実行中 1、失敗 1 のはず: %v %+v", err, counts)
		}
	})
	testingT.Run("他のユーザーは not_found", func(subTest *testing.T) {
		if _, _, err := repo.List(ctx, "user-b", project.ID, TaskFilter{}, 10, nil, now); apperror.KindOf(err) != apperror.KindNotFound {
			subTest.Errorf("not_found のはず: %v", err)
		}
		if _, _, err := repo.Get(ctx, "user-b", project.ID, parent.ID); apperror.KindOf(err) != apperror.KindNotFound {
			subTest.Errorf("not_found のはず: %v", err)
		}
		if _, _, err := repo.Get(ctx, "user-a", project.ID, "task-none"); apperror.KindOf(err) != apperror.KindNotFound {
			subTest.Errorf("存在しないタスクは not_found のはず: %v", err)
		}
	})
	testingT.Run("ユーザーの全てのプロジェクトのタスク(削除されたプロジェクトも含む)", func(subTest *testing.T) {
		database.Create(&model.Project{ID: "project-00000003", UserID: "user-a", Name: "second", Status: model.ProjectStatusActive})
		_ = repo.Start(ctx, model.ProjectTask{ID: "task-z1", ProjectID: "project-00000003", UserID: "user-a", ProjectName: "second", Kind: "deploy_create", WorkflowType: "CreateDeployWorkflow", ResourceID: "deploy-9", WorkflowID: "wf-second", RunID: "run-1", StartedAt: now})
		// 別のユーザーのタスクは、混ざらない。
		_ = repo.Start(ctx, model.ProjectTask{ID: "task-z2", ProjectID: "project-x", UserID: "user-b", ProjectName: "x", Kind: "deploy_create", WorkflowType: "CreateDeployWorkflow", ResourceID: "deploy-8", WorkflowID: "wf-b", RunID: "run-1", StartedAt: now})
		// プロジェクトが削除されても、削除のタスクと名前は残る。
		database.Exec("DELETE FROM projects WHERE id = 'project-00000003'")
		tasks, _, err := repo.ListByUser(ctx, "user-a", TaskFilter{}, 20, nil, now)
		if err != nil || len(tasks) != 5 || tasks[0].ID != "task-z1" || tasks[0].ProjectName != "second" {
			subTest.Fatalf("user-a のタスク 5 件で、削除済みのプロジェクトのものも先頭に出るはず: %v %d", err, len(tasks))
		}
		counts, _ := repo.CountsByUser(ctx, "user-a", now)
		if counts.Running != 2 || counts.FailedRecent != 1 {
			subTest.Errorf("実行中 2(wf-running と wf-second)、失敗 1 のはず: %+v", counts)
		}
		if _, _, err := repo.ListByUser(ctx, "", TaskFilter{}, 20, nil, now); apperror.KindOf(err) != apperror.KindNotFound {
			subTest.Errorf("ユーザーが空なら not_found のはず: %v", err)
		}
		if err := repo.RecordFinished(ctx, model.ProjectTask{ID: "task-z3", ProjectID: "project-00000001", UserID: "user-a", ProjectName: "my-app", Kind: "project_create", WorkflowType: "", ResourceID: project.ID, WorkflowID: "project-create-1", RunID: "-", Status: model.TaskStatusSucceeded, StartedAt: now, FinishedAt: &now}); err != nil {
			subTest.Fatal(err)
		}
		if again := repo.RecordFinished(ctx, model.ProjectTask{ID: "task-z4", ProjectID: "project-00000001", UserID: "user-a", Kind: "project_create", WorkflowID: "project-create-1", RunID: "-", Status: model.TaskStatusSucceeded, StartedAt: now, FinishedAt: &now}); again != nil {
			subTest.Errorf("同じ記録を、もう一度保存しても、エラーにならないはず: %v", again)
		}
	})
	testingT.Run("削除: 期間と、件数の上限(プロジェクトが無いタスクは残す)", func(subTest *testing.T) {
		if err := repo.Prune(ctx, now.Add(-2*time.Hour), 3); err != nil {
			subTest.Fatal(err)
		}
		var total int64
		database.Model(&model.ProjectTask{}).Where("project_id = ?", project.ID).Count(&total)
		if total != 3 {
			subTest.Errorf("プロジェクトごとに 3 件に絞られるはず: %d", total)
		}
		var deletedProject int64
		database.Model(&model.ProjectTask{}).Where("project_id = ?", "project-00000003").Count(&deletedProject)
		if deletedProject != 1 {
			subTest.Errorf("削除されたプロジェクトのタスクは、残るはず: %d", deletedProject)
		}
	})
}
