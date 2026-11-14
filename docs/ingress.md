# ingress(Traefik の IngressRoute)

プロジェクトの外向きの入り口。`/projects/:projectId/ingresses` の API で作り、ルールを設定すると、Traefik の IngressRoute として k8s に作られる。API の仕様は [endpoints/ingresses.md](endpoints/ingresses.md)、呼び出しの流れは [call-flow.md](call-flow.md) の 5.7。

## ホスト名の払い出し

- 形: `<形容詞>-<色>-<動物>.<ベースドメイン>`(例: `brave-black-horse.launchs.org`)。単語は、暗号論的な乱数で選ぶ。重なったときは、作り直す。
- ベースドメイン: 環境変数 `INGRESS_BASE_DOMAIN`(既定 `launchs.org`)。
- 払い出さない名前: 環境変数 `INGRESS_RESERVED_SUBDOMAINS`(カンマ区切り。指定すると既定の一覧を置き換える。既定は `www,api,app,auth,admin,registry,logs,mail,static,docs,status,traefik,dashboard`)。名前そのもの、または、3つの単語のどれかが含まれると、払い出されない(大文字小文字は区別しない)。
- 設定は backend(払い出す側)だけが読む。ホスト名は、ingress の行に保存される。

## Traefik とクラスターの用意(利用者がすること)

このシステムは、Traefik までのリクエストの到達、ドメイン(DNS)、TLS を管理しない。

- Traefik をクラスターに入れる(IngressRoute の CRD を含む)。`TRAEFIK_API_VERSION`(既定 `traefik.io/v1alpha1`)と `TRAEFIK_ENTRY_POINTS`(既定 `web`)は、worker の環境変数。
- Traefik の namespace を、`INGRESS_NAMESPACE`(worker)に指定する。プロジェクトの NetworkPolicy が、その namespace からの通信を許可する([network-isolation.md](network-isolation.md))。指定しないと、Traefik から Pod へ届かない。
- 本番の RBAC では、worker に、`traefik.io` の `ingressroutes` の get / create / update / delete を許す。
- ベースドメインのワイルドカード(`*.launchs.org`)を、Traefik へ向ける(DNS)。

## 開発用のクラスター(Talos)での確認

Helm で Traefik を NodePort(80 → 30080)で入れて確認した(`traefik/traefik` 42.3.2)。

```
helm install traefik traefik/traefik --version 42.3.2 --namespace traefik --create-namespace \
  --set service.spec.type=NodePort --set ports.web.nodePort=30080 --set ports.websecure.expose.default=false \
  --set-string deployment.podLabels."admission\.datadoghq\.com/enabled"=false --set providers.kubernetesIngress.enabled=false
INGRESS_NAMESPACE=traefik task up:k8s
curl -H "Host: <払い出されたホスト名>" http://192.168.100.12:30080/
```
