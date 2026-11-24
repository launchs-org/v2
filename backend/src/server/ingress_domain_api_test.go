package server

import (
	"net/http"
	"testing"
)

// TestIngressDomainAPI は、ingress の追加のホスト名(利用者が選ぶサブドメイン)の、追加・一覧・検査・他人からの見え方・削除を、HTTP の経路で確かめる。
func TestIngressDomainAPI(testingT *testing.T) {
	environment := newIngressTestEnvironment(testingT)
	ingressID := createIngress(environment)
	domainsPath := environment.basePath + "/" + ingressID + "/domains"

	// 追加できる(201)。ベースドメインが付き、すぐに有効になる。
	statusCode, body := environment.ownerClient.call(http.MethodPost, domainsPath, `{"subdomain":" MyApp "}`)
	if statusCode != http.StatusCreated || body["hostname"] != "myapp.launchs.org" || body["status"] != "verified" || body["verifiedAt"] == nil {
		testingT.Fatalf("追加できるはず: %d %v", statusCode, body)
	}
	domainID := body["domainId"].(string)

	// 同じ名前の追加は 409。払い出されたホスト名と同じ名前も 409(自分の ingress でも)。
	for _, taken := range []string{`{"subdomain":"myapp"}`, `{"subdomain":"brave-blue-fox"}`} {
		if takenStatus, _ := environment.ownerClient.call(http.MethodPost, domainsPath, taken); takenStatus != http.StatusConflict {
			testingT.Errorf("%s は 409 のはず: %d", taken, takenStatus)
		}
	}
	// 不正な形、外部のドメイン、予約語、空は 400。
	for _, invalid := range []string{`{"subdomain":"a.b"}`, `{"subdomain":"app.example.com"}`, `{"subdomain":"ab"}`, `{"subdomain":"www"}`, `{"subdomain":"My_App"}`, `{}`} {
		if invalidStatus, _ := environment.ownerClient.call(http.MethodPost, domainsPath, invalid); invalidStatus != http.StatusBadRequest {
			testingT.Errorf("%s は 400 のはず: %d", invalid, invalidStatus)
		}
	}

	// 一覧に出る(ベースドメインも返る)。他人からは 404。
	statusCode, body = environment.ownerClient.call(http.MethodGet, domainsPath, "")
	domains, _ := body["domains"].([]any)
	if statusCode != http.StatusOK || len(domains) != 1 || body["baseDomain"] != "launchs.org" {
		testingT.Fatalf("一覧に 1 つあるはず: %d %v", statusCode, body)
	}
	if otherStatus, _ := environment.otherClient.call(http.MethodGet, domainsPath, ""); otherStatus != http.StatusNotFound {
		testingT.Errorf("他人からは 404 のはず: %d", otherStatus)
	}
	if otherStatus, _ := environment.otherClient.call(http.MethodDelete, domainsPath+"/"+domainID, ""); otherStatus != http.StatusNotFound {
		testingT.Errorf("他人の削除は 404 のはず: %d", otherStatus)
	}

	// 削除できる。ルールが無い ingress なので、IngressRoute は作り直さない。
	if deleteStatus, _ := environment.ownerClient.call(http.MethodDelete, domainsPath+"/"+domainID, ""); deleteStatus != http.StatusOK {
		testingT.Errorf("削除できるはず: %d", deleteStatus)
	}
	if len(environment.starter.reappliedIngressIDs) != 0 {
		testingT.Errorf("ルールが無ければ、作り直さないはず: %v", environment.starter.reappliedIngressIDs)
	}
	if missingStatus, _ := environment.ownerClient.call(http.MethodDelete, domainsPath+"/"+domainID, ""); missingStatus != http.StatusNotFound {
		testingT.Errorf("無いものは 404 のはず: %d", missingStatus)
	}
}
