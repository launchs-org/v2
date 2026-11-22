package controller

import "testing"

// TestImageRefFromWebhookBody は、webhook のボディから、イメージを決める規則を確認する。
func TestImageRefFromWebhookBody(testingT *testing.T) {
	// 受け付ける形と、その結果を表にする。
	cases := []struct {
		name    string
		body    string
		current string
		want    string
		failed  bool
	}{
		{"完全な参照", `{"imageRef":"ghcr.io/org/app:v2"}`, "ghcr.io/org/app:v1", "ghcr.io/org/app:v2", false},
		{"タグだけ", `{"tag":"v2"}`, "ghcr.io/org/app:v1", "ghcr.io/org/app:v2", false},
		{"ホストのポートを、タグと取り違えない", `{"tag":"v2"}`, "localhost:5000/p/app:v1", "localhost:5000/p/app:v2", false},
		{"タグが無い参照にも付ける", `{"tag":"v2"}`, "nginx", "nginx:v2", false},
		{"digest を外す", `{"tag":"v2"}`, "org/app@sha256:abc", "org/app:v2", false},
		{"Docker Hub の形式", `{"push_data":{"tag":"v3"}}`, "org/app:v1", "org/app:v3", false},
		{"空のボディは拒否", ``, "org/app:v1", "", true},
		{"使えない文字は拒否", `{"imageRef":"a b"}`, "org/app:v1", "", true},
		{"JSON でないものは拒否", `x`, "org/app:v1", "", true},
	}
	// 表の全てを確かめる。
	for _, testCase := range cases {
		got, resolveError := imageRefFromWebhookBody([]byte(testCase.body), testCase.current)
		if (resolveError != nil) != testCase.failed || got != testCase.want {
			testingT.Errorf("%s: got=%q err=%v", testCase.name, got, resolveError)
		}
	}
}
