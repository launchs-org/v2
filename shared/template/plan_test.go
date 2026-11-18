package template

import (
	"strconv"
	"strings"
	"testing"

	"launchs/shared/templateplan"
)

// multiDeployYAML は、2 つの deploy が、service の DNS 名で、つながるテンプレート(web が db を参照する。web を先に書く)。
const multiDeployYAML = `
apiVersion: launchs/v1
kind: Template
metadata:
  id: wp
  name: web と DB
variables:
  - name: password
    label: パスワード
    type: password
    generate: true
resources:
  - kind: deploy
    name: web
    image: wordpress:6
    spec: small
    env:
      DB_HOST: ${service.db.dns}
      DB_PASSWORD: ${password}
    service:
      ports: [{ name: http, port: 80, protocol: tcp }]
  - kind: volume
    name: dbdata
    sizeMB: 1024
  - kind: configmap
    name: conf
    files:
      - key: a.cnf
        content: "host=${project.name}\n"
  - kind: deploy
    name: db
    image: mariadb:11
    spec: medium
    env:
      MARIADB_PASSWORD: ${password}
    mounts: [{ volume: dbdata, path: /var/lib/mysql }]
    configmaps: [{ configmap: conf, key: a.cnf, path: /etc/mysql/conf.d/a.cnf }]
    service:
      ports: [{ name: mysql, port: 3306, protocol: tcp }]
  - kind: ingress
    rules: [{ path: /, deploy: web, port: 80 }]
`

// planOptions は、テスト用の、計画を作るときの値を作る(ID は、接頭辞と連番)。
func planOptions() PlanOptions {
	counter := 0
	return PlanOptions{ProjectName: "demo", DNSToken: "abc123", Hosts: []string{"a.example", "b.example"}, NewID: func(prefix string) string {
		counter++
		return prefix + strconv.Itoa(counter)
	}}
}

// TestBuildPlan は、計画の、ステップの順序、ID、DNS の目印、クォータの量を確認する。
func TestBuildPlan(testingT *testing.T) {
	parsed := mustParse(testingT, multiDeployYAML)
	if validateError := Validate(parsed); validateError != nil {
		testingT.Fatalf("検証に通るはず: %v", validateError)
	}
	values := map[string]string{"password": "secret"}
	plan, planError := BuildPlan(parsed, values, planOptions())
	if planError != nil {
		testingT.Fatal(planError)
	}
	testingT.Run("順序は、volume → configmap → db(参照される側が先)→ db の service → web → web の service → ingress", func(subTest *testing.T) {
		var order []string
		for _, step := range plan.Steps {
			order = append(order, step.Kind+":"+step.Name)
		}
		want := "volume:dbdata configmap:conf deploy:db service:db deploy:web service:web ingress:ingress"
		if got := strings.Join(order, " "); got != want {
			subTest.Errorf("順序が違う:\n got %s\nwant %s", got, want)
		}
	})
	testingT.Run("ID は、部品ごとに、接頭辞つきで、重複せず決まる", func(subTest *testing.T) {
		seen := map[string]bool{}
		for _, step := range plan.Steps {
			if step.ID == "" || seen[step.ID] {
				subTest.Errorf("ID が空、または重複: %+v", step)
			}
			seen[step.ID] = true
		}
		if !strings.HasPrefix(plan.IDOf(templateplan.StepDeploy, "web"), "deploy-") || !strings.HasPrefix(plan.IDOf(templateplan.StepIngress, "ingress"), "ingress-") {
			subTest.Errorf("接頭辞が違う: %v", plan.Steps)
		}
	})
	testingT.Run("${service.db.dns} は、この実行の目印になり、置き換えられる。パスワードは、そのまま入る", func(subTest *testing.T) {
		web := plan.Steps[plan.StepIndex(templateplan.StepDeploy, "web")].Deploy
		token := templateplan.DNSToken("abc123", "db")
		if web.Env["DB_HOST"] != token || web.Env["DB_PASSWORD"] != "secret" {
			subTest.Fatalf("目印とパスワードのはず: %v", web.Env)
		}
		replaced := templateplan.ReplaceDNS(web.Env["DB_HOST"], "abc123", map[string]string{"db": "db.p.svc.cluster.local"})
		if replaced != "db.p.svc.cluster.local" || templateplan.HasUnresolvedDNS(replaced, "abc123") {
			subTest.Errorf("置き換わるはず: %q", replaced)
		}
		// 別の実行の目印は、置き換えない。
		other := templateplan.DNSToken("zzz999", "db")
		if templateplan.ReplaceDNS(other, "abc123", map[string]string{"db": "X"}) != other {
			subTest.Error("別の実行の目印は、置き換えないはず")
		}
	})
	testingT.Run("db は、volume と configmap をマウントし、configmap は展開済み", func(subTest *testing.T) {
		db := plan.Steps[plan.StepIndex(templateplan.StepDeploy, "db")].Deploy
		configMap := plan.Steps[plan.StepIndex(templateplan.StepConfigMap, "conf")].ConfigMap
		if len(db.Mounts) != 1 || db.Mounts[0].Volume != "dbdata" || len(db.ConfigMaps) != 1 || db.ConfigMaps[0].Mode != "0644" || configMap.Files[0].Content != "host=demo\n" {
			subTest.Errorf("マウントと内容が違う: %+v %+v", db, configMap)
		}
	})
	testingT.Run("クォータの量: deploy は small 1 と medium 1、volume は 1 つで 1024MB", func(subTest *testing.T) {
		needs := Needs(plan)
		if needs.DeploysBySpec["small"] != 1 || needs.DeploysBySpec["medium"] != 1 || needs.Volumes != 1 || needs.VolumeTotalMB != 1024 {
			subTest.Errorf("量が違う: %+v", needs)
		}
	})
	testingT.Run("ステップの進み具合は、全て待機中から始まる", func(subTest *testing.T) {
		for _, state := range plan.InitialStates() {
			if state.Status != templateplan.StatusWaiting {
				subTest.Errorf("待機中のはず: %+v", state)
			}
		}
	})
}

// TestBuildPlanErrors は、展開のあとの値が規則に合わないときに、計画を作れないことを確認する。
func TestBuildPlanErrors(testingT *testing.T) {
	testingT.Run("ConfigMap の内容が、変数の値で 1MiB を超えると、断る", func(subTest *testing.T) {
		parsed := mustParse(subTest, multiDeployYAML)
		bigYAML := strings.Replace(multiDeployYAML, `content: "host=${project.name}\n"`, `content: "host=${password}"`, 1)
		parsed = mustParse(subTest, bigYAML)
		if _, planError := BuildPlan(parsed, map[string]string{"password": strings.Repeat("a", 1024*1024)}, planOptions()); planError == nil {
			subTest.Error("エラーになるはず")
		}
	})
}
