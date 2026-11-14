# プロジェクトの隔離(NetworkPolicy)

任意のイメージを動かせるので、**プロジェクトの namespace は、他から隔離する**。namespace を用意するとき(`EnsureNamespace`)に、`launchs-isolation` という NetworkPolicy を一緒に作る。

## なぜ必要か
Cilium の既定(`enable-policy = default`)は「拒否」ではなく、**ポリシーが選んだ Pod だけが隔離される**。ポリシーが無いと、全ての通信が許可される。実機で、隔離が無いと、プロジェクト A の Pod から、別のプロジェクトの Pod、システムの Pod(Datadog のエージェント)、ノードの kubelet(10250)に届くことを確認した。悪意のあるイメージが、クラスターの中を調べ回れる。

## 規則(`worker/src/cluster/network_policy.go`)
namespace の全ての Pod に、次を適用する。

| 向き | 許可するもの |
|---|---|
| 入ってくる通信 | 同じ namespace の Pod。`INGRESS_NAMESPACE` を指定すれば、その namespace(Traefik など)からも |
| 出ていく通信 | ① 同じ namespace の Pod ② クラスターの DNS(`kube-system` の `k8s-app=kube-dns`。UDP と TCP の 53) ③ インターネット(`0.0.0.0/0` から、内部のネットワークを除く) |

③で除く内部のネットワーク: `10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`(ノード、Pod、Service の CIDR を含む)、`169.254.0.0/16`(リンクローカル。クラウドのメタデータなど)、`100.64.0.0/10`(CGNAT。Tailscale など)。

→ 他のプロジェクト、システムの Pod、ノードへの通信は、止まる。別のプロジェクトの deploy を、Service 名で呼ぶことも、できない(意図した仕様)。

## 適用のタイミング
- **namespace を作ったとき**(プロジェクトの最初の deploy の作成)。
- **deploy を作るたびに、作り直しではなく、内容を合わせる**(冪等)。手で変えられた場合や、`INGRESS_NAMESPACE` を変えたあとも、次の deploy の作成で、期待する内容に戻る。
- ポリシーが無かった**既存のプロジェクトは、次の deploy の作成**で、隔離される。スケールや設定の反映では、適用しない(今のところ、全プロジェクトへの一括の適用は、ない)。

## 設定
| 環境変数(ワーカー) | 意味 | 既定 |
|---|---|---|
| `INGRESS_NAMESPACE` | 外からの通信を中継する namespace(Traefik など)。ここからの通信を許可する | 空(許可しない) |

## 実機での確認(Talos、Cilium)
- プロジェクト A の Pod から: 同じプロジェクトの別の deploy は**届く**。別のプロジェクト、Datadog の Pod、ノードの IP(`:10250`)は**届かない**(タイムアウト)。インターネットは**届く**。クラスター内の名前(`kubernetes.default.svc.test-cluster.local`)も**解決できる**。
- プロジェクト B から A も、**届かない**。
- NetworkPolicy を手で削除しても、同じプロジェクトに deploy を作ると、**元に戻る**。
- `INGRESS_NAMESPACE=default` にすると、`default` の Pod からは**届き**、ほかの namespace の Pod からは**届かない**。

## 既知の制限
- **インターネットへの出口は、全て許可**している(SMTP、任意のポートなど)。必要なら、ポートを絞る。
- **IPv6 は、扱っていない**(このクラスターは IPv4)。
- 隔離は、Cilium が NetworkPolicy を強制している前提。強制しない CNI では、効かない。
- ノードの kubelet などからの、ヘルスチェックは、Cilium が許可する(Pod のプローブは、このシステムでは、まだ使っていない)。
