package model

import (
	"strings"
	"testing"

	"gorm.io/datatypes"
)

// TestValidateIngressRules は、ルールの形(path の書式、重複、ポート番号の範囲、個数)の検査を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestValidateIngressRules(testing *testing.T) {
	// 正しい service の ID。
	serviceID := ServiceIDPrefix + "00000000-0000-4000-8000-000000000000"
	// 空のルールと、正しいルールは通る。
	if ValidateIngressRules(nil) != nil || ValidateIngressRules([]IngressRule{{Path: "/", ServiceID: serviceID, Port: 8080}, {Path: "/api/v1-x_y.z~%20", ServiceID: serviceID, Port: 1}}) != nil {
		testing.Error("正しいルールは通るはず")
	}
	// 不正なルールは、全て invalid_request になる。
	invalid := map[string][]IngressRule{
		"/ で始まらない":         {{Path: "api", ServiceID: serviceID, Port: 80}},
		"空の path":          {{Path: "", ServiceID: serviceID, Port: 80}},
		"空白":               {{Path: "/a b", ServiceID: serviceID, Port: 80}},
		"バッククォート":          {{Path: "/a`b", ServiceID: serviceID, Port: 80}},
		"引用符":              {{Path: "/a\"b", ServiceID: serviceID, Port: 80}},
		"長すぎる path":        {{Path: "/" + strings.Repeat("a", MaxIngressPathLength), ServiceID: serviceID, Port: 80}},
		"重複":               {{Path: "/", ServiceID: serviceID, Port: 80}, {Path: "/", ServiceID: serviceID, Port: 81}},
		"serviceId の形":     {{Path: "/", ServiceID: "nope", Port: 80}},
		"serviceId が接頭辞だけ": {{Path: "/", ServiceID: ServiceIDPrefix, Port: 80}},
		"port が 0":         {{Path: "/", ServiceID: serviceID, Port: 0}},
		"port が 65536":     {{Path: "/", ServiceID: serviceID, Port: 65536}},
	}
	for name, rules := range invalid {
		if ValidateIngressRules(rules) == nil {
			testing.Errorf("%s: エラーになるはず", name)
		}
	}
	// 個数は、50件まで。
	many := []IngressRule{}
	for index := 0; index <= MaxIngressRules; index++ {
		many = append(many, IngressRule{Path: "/p" + strings.Repeat("a", index), ServiceID: serviceID, Port: 80})
	}
	if ValidateIngressRules(many[:MaxIngressRules]) != nil || ValidateIngressRules(many) == nil {
		testing.Error("50件までのはず")
	}
}

// TestIngressTransitions は、ingress の状態の規則と遷移(反映の開始・確定・戻し、削除)を確かめる。
//
// 引数:
//   - testing: テストの状態。
func TestIngressTransitions(testing *testing.T) {
	// 状態ごとの、操作の可否。
	for _, testCase := range []struct {
		status         string
		canChangeRules bool
		canDelete      bool
	}{
		{IngressStatusActive, true, true},
		{IngressStatusFailed, true, true},
		{IngressStatusApplying, false, false},
		{IngressStatusDeleting, false, false},
	} {
		ingress := Ingress{Status: testCase.status}
		if ingress.CanChangeRules() != testCase.canChangeRules || ingress.CanDelete() != testCase.canDelete {
			testing.Errorf("%s: 可否が違う", testCase.status)
		}
	}
	// pending が無いと、反映は始められない。
	idle := Ingress{Status: IngressStatusActive}
	if _, beginError := idle.BeginRulesApply(); beginError == nil {
		testing.Error("pending が無ければ conflict のはず")
	}
	// 反映の成功: pending がルールになり、pending が空になり、active になる。
	ingress := Ingress{Status: IngressStatusFailed, Rules: datatypes.JSON(`[]`), PendingRules: datatypes.JSON(`[{"path":"/"}]`)}
	previous, beginError := ingress.BeginRulesApply()
	if beginError != nil || previous != IngressStatusFailed || ingress.Status != IngressStatusApplying {
		testing.Fatalf("反映の開始が違う: %v %+v", beginError, ingress)
	}
	if !ingress.CommitRules(true) || ingress.Status != IngressStatusActive || string(ingress.Rules) != `[{"path":"/"}]` || ingress.PendingRules != nil {
		testing.Errorf("成功の確定が違う: %+v", ingress)
	}
	if ingress.CommitRules(true) {
		testing.Error("applying でなければ何もしないはず")
	}
	// 反映の失敗: ルールと pending は、そのままで、failed になる。
	failing := Ingress{Status: IngressStatusApplying, Rules: datatypes.JSON(`[]`), PendingRules: datatypes.JSON(`[{"path":"/"}]`)}
	if !failing.CommitRules(false) || failing.Status != IngressStatusFailed || string(failing.Rules) != `[]` || failing.PendingRules == nil {
		testing.Errorf("失敗の確定が違う: %+v", failing)
	}
	// 開始に失敗したときの戻し。
	reverting := Ingress{Status: IngressStatusApplying}
	if !reverting.RevertApply(IngressStatusActive) || reverting.Status != IngressStatusActive || reverting.RevertApply(IngressStatusFailed) {
		testing.Error("戻しが違う")
	}
	// 削除: 受け付け(前の状態が返る)→ 戻す / failed にする。
	deleting := Ingress{Status: IngressStatusFailed}
	previous, deleteError := deleting.BeginDelete()
	if deleteError != nil || previous != IngressStatusFailed || deleting.Status != IngressStatusDeleting {
		testing.Error("削除の受け付けが違う")
	}
	if _, secondError := deleting.BeginDelete(); secondError == nil {
		testing.Error("削除中は conflict のはず")
	}
	if !deleting.MarkDeleteFailed() || deleting.Status != IngressStatusFailed || deleting.MarkDeleteFailed() || deleting.RevertDeleting(IngressStatusActive) {
		testing.Error("削除失敗の遷移が違う")
	}
}

// TestServiceHasTCPPort は、tcp のポートの有無の判定を確かめる(udp は含めない)。
//
// 引数:
//   - testing: テストの状態。
func TestServiceHasTCPPort(testing *testing.T) {
	// tcp の 8080 と、udp の 27015 を持つ service。
	service := Service{Ports: datatypes.JSON(`[{"name":"web","port":8080,"protocol":"tcp"},{"name":"game","port":27015,"protocol":"udp"}]`)}
	if !service.HasTCPPort(8080) || service.HasTCPPort(27015) || service.HasTCPPort(1) {
		testing.Error("tcp の 8080 だけがあるはず")
	}
	// 壊れたポートの JSON は、無いものとして扱う。
	if (Service{Ports: datatypes.JSON(`{`)}).HasTCPPort(8080) {
		testing.Error("壊れた JSON は false のはず")
	}
}
