package service

import (
	"context"
	"strings"
	"testing"

	"launchs/shared/apperror"
	"launchs/shared/repository"
)

// fakeTagStore は、タグの保存先の fake。
type fakeTagStore struct {
	resources []repository.TaggedResource
	set       []string
}

// SetTags は、渡されたタグを覚える。
func (store *fakeTagStore) SetTags(_ context.Context, _ string, _ string, _ string, tags []string) error {
	store.set = tags
	return nil
}

// ListTags は、空を返す。
func (store *fakeTagStore) ListTags(context.Context, string, string) ([]repository.DeployTagCount, error) {
	return nil, nil
}

// ListByTag は、決めておいた資源を返す。
func (store *fakeTagStore) ListByTag(context.Context, string, string, string) ([]repository.TaggedResource, error) {
	return store.resources, nil
}

// fakeDeleter は、削除の fake(指定した ID だけ失敗し、呼ばれた順に、種類と ID を覚える)。
type fakeDeleter struct {
	kind   string
	failID string
	calls  *[]string
}

// Delete は、deploy と service の削除(3 つの引数)を、覚える。failID なら conflict を返す。
func (deleter *fakeDeleter) Delete(_ context.Context, _ string, resourceID string) error {
	return deleter.record(resourceID)
}

// record は、呼ばれたことを覚える。failID なら conflict を返す。
func (deleter *fakeDeleter) record(resourceID string) error {
	if resourceID == deleter.failID {
		return apperror.New(apperror.KindConflict, "反映中です")
	}
	*deleter.calls = append(*deleter.calls, deleter.kind+":"+resourceID)
	return nil
}

// fakeProjectDeleter は、volume と ingress の削除(4 つの引数)の fake。
type fakeProjectDeleter struct{ fakeDeleter }

// Delete は、削除を覚える。
func (deleter *fakeProjectDeleter) Delete(_ context.Context, _ string, _ string, resourceID string) error {
	return deleter.record(resourceID)
}

// TestDeleteByTag は、タグの一斉削除が、決めた順(ingress、service、deploy、volume)で進み、失敗した資源を残して他を進めることを確認する。
func TestDeleteByTag(testingT *testing.T) {
	// 呼ばれた順を覚える場所と、deploy の b だけが失敗する削除を用意する。
	calls := []string{}
	deleters := TagDeleters{
		Deploy:  &fakeDeleter{kind: "deploy", failID: "b", calls: &calls},
		Service: &fakeDeleter{kind: "service", calls: &calls},
		Volume:  &fakeProjectDeleter{fakeDeleter{kind: "volume", calls: &calls}},
		Ingress: &fakeProjectDeleter{fakeDeleter{kind: "ingress", calls: &calls}},
	}
	// タグの付いた資源を、わざと、削除の順と違う並びで返す。
	store := &fakeTagStore{resources: []repository.TaggedResource{
		{Kind: "volume", ID: "v"}, {Kind: "deploy", ID: "a"}, {Kind: "deploy", ID: "b"}, {Kind: "service", ID: "s", DeployID: "a"}, {Kind: "ingress", ID: "i"},
	}}
	tagService := NewDeployTagService(store, deleters)
	// 一斉に削除する。
	result, deleteError := tagService.DeleteByTag(context.Background(), "user", "project", "template:redash", false)
	if deleteError != nil {
		testingT.Fatalf("成功するはず: %v", deleteError)
	}
	// ingress、service(deploy の ID で指す)、deploy、volume の順に呼ばれ、deploy の b だけが失敗として返る。
	if strings.Join(calls, ",") != "ingress:i,service:a,deploy:a,volume:v" {
		testingT.Errorf("呼ぶ順が違う: %v", calls)
	}
	if len(result.Accepted) != 4 || len(result.Failed) != 1 || result.Failed[0].ID != "b" || result.Failed[0].Message == "" {
		testingT.Errorf("4 つを受け付け、b だけ失敗のはず: %+v", result)
	}
	// volume を残す指定なら、volume は呼ばれない。
	calls = nil
	if _, keepError := tagService.DeleteByTag(context.Background(), "user", "project", "template:redash", true); keepError != nil || strings.Contains(strings.Join(calls, ","), "volume") {
		testingT.Errorf("volume は呼ばれないはず: %v %v", calls, keepError)
	}
	// 1 つも無いタグは not_found。
	if _, notFoundError := NewDeployTagService(&fakeTagStore{}, deleters).DeleteByTag(context.Background(), "user", "project", "none", false); apperror.KindOf(notFoundError) != apperror.KindNotFound {
		testingT.Errorf("not_found のはず: %v", notFoundError)
	}
	// 不正なタグは invalid_request。
	if _, invalidError := tagService.DeleteByTag(context.Background(), "user", "project", "Bad Tag", false); apperror.KindOf(invalidError) != apperror.KindInvalidRequest {
		testingT.Errorf("invalid_request のはず: %v", invalidError)
	}
	// SetTags は、整えて(昇順にして)保存する。
	saveStore := &fakeTagStore{}
	saved, setError := NewDeployTagService(saveStore, deleters).SetTags(context.Background(), "deploy", "user", "deploy-1", []string{"b", "a"})
	if setError != nil || len(saved) != 2 || saveStore.set[0] != "a" {
		testingT.Errorf("昇順で保存するはず: %v %v", saved, setError)
	}
}
