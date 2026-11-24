package server

import (
	"net/http"
	"testing"
)

// TestIngressDomainAPI は、カスタムドメインの、追加・一覧・検査・他人からの見え方・削除を、HTTP の経路で確かめる。
// 所有の確認(DNS を引く)は、service の試験で行うので、ここでは行わない。
func TestIngressDomainAPI(testingT *testing.T) {
	environment := newIngressTestEnvironment(testingT)
	ingressID := createIngress(environment)
	domainsPath := environment.basePath + "/" + ingressID + "/domains"

	// 追加できる(201)。確認待ちで、DNS に設定する内容が返る。
	statusCode, body := environment.ownerClient.call(http.MethodPost, domainsPath, `{"hostname":" App.Example.com. "}`)
	if statusCode != http.StatusCreated || body["hostname"] != "app.example.com" || body["status"] != "pending" {
		testingT.Fatalf("追加できるはず: %d %v", statusCode, body)
	}
	challenge, _ := body["challenge"].(map[string]any)
	if challenge["txtName"] != "_launchs-challenge.app.example.com" || challenge["cnameValue"] != "brave-blue-fox.launchs.org" || challenge["txtValue"] == "" {
		testingT.Errorf("確認の内容が違う: %v", challenge)
	}
	domainID := body["domainId"].(string)

	// 同じドメインの追加は 409、不正な形は 400。
	if duplicateStatus, _ := environment.ownerClient.call(http.MethodPost, domainsPath, `{"hostname":"app.example.com"}`); duplicateStatus != http.StatusConflict {
		testingT.Errorf("重複は 409 のはず: %d", duplicateStatus)
	}
	for _, invalid := range []string{`{"hostname":"localhost"}`, `{"hostname":"*.example.com"}`, `{"hostname":"192.0.2.1"}`, `{}`} {
		if invalidStatus, _ := environment.ownerClient.call(http.MethodPost, domainsPath, invalid); invalidStatus != http.StatusBadRequest {
			testingT.Errorf("%s は 400 のはず: %d", invalid, invalidStatus)
		}
	}

	// 一覧に出る。他人からは 404。
	statusCode, body = environment.ownerClient.call(http.MethodGet, domainsPath, "")
	domains, _ := body["domains"].([]any)
	if statusCode != http.StatusOK || len(domains) != 1 {
		testingT.Fatalf("一覧に 1 つあるはず: %d %v", statusCode, body)
	}
	if otherStatus, _ := environment.otherClient.call(http.MethodGet, domainsPath, ""); otherStatus != http.StatusNotFound {
		testingT.Errorf("他人からは 404 のはず: %d", otherStatus)
	}
	if otherStatus, _ := environment.otherClient.call(http.MethodDelete, domainsPath+"/"+domainID, ""); otherStatus != http.StatusNotFound {
		testingT.Errorf("他人の削除は 404 のはず: %d", otherStatus)
	}

	// 削除できる。確認待ちを外しても、IngressRoute は作り直さない。
	if deleteStatus, _ := environment.ownerClient.call(http.MethodDelete, domainsPath+"/"+domainID, ""); deleteStatus != http.StatusOK {
		testingT.Errorf("削除できるはず: %d", deleteStatus)
	}
	if len(environment.starter.reappliedIngressIDs) != 0 {
		testingT.Errorf("確認待ちを外しても、作り直さないはず: %v", environment.starter.reappliedIngressIDs)
	}
	if missingStatus, _ := environment.ownerClient.call(http.MethodDelete, domainsPath+"/"+domainID, ""); missingStatus != http.StatusNotFound {
		testingT.Errorf("無いものは 404 のはず: %d", missingStatus)
	}
}
