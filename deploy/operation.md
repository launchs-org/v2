# 本番の運用手順(リリース・マイグレーション・バックアップ)

namespace `launchs` で動かしている launchs を、更新したり、戻したり、バックアップしたりする手順です。最初の導入は [deploy.md](deploy.md) を見てください。
コマンドは、`deploy/` で、実行します。**環境ごとの設定(`settings/`。ConfigMap と Secret)は、本体と別に適用します**(`kubectl apply -k settings/`。`settings/secret.yaml` を置いた状態で)。本体の `kubectl apply -k .` は、設定を変えません。

## 1. 全体の流れ
```
main に入れる → GitHub Actions が、テストを通して、イメージを GHCR に出す(sha-… と latest)
   → CI が、deploy/kustomization.yaml の newTag を、新しい sha-… に書き換えて、main にコミットする(自動)
   → 運用者が、main を取り込んで、kubectl apply -k .(手動。ここで、人が確認する)
   → backend と auth の Pod が起動するとき、initContainer が、マイグレーションを流す
   → 新しい Pod が、Ready になってから、古い Pod が止まる(ローリングアップデート)
```

## 2. マイグレーションのしくみ
- **自動で流れる**: `backend` の initContainer が app データベース(`/migrations/app`)を、`auth` の initContainer が auth データベース(`/migrations/auth`)を、**Pod が起動するたびに**、`up` で流します。**すでに流した分は、何もしません**(何度起動しても、安全です)。
- **イメージ**: `ghcr.io/launchs-org/v2/migrate:<タグ>`(golang-migrate に、SQL を入れたもの。`migrations/dockerfile`)。**backend、auth と、同じ sha のタグ**にそろえます(`kustomization.yaml` の `images`)。
- **同時に流れても安全**: backend は 3 つ起動しますが、golang-migrate は、PostgreSQL の advisory lock を取るので、1 つが流して、残りは待ちます。
- **down はありません**(`up` だけを書く決まりです)。戻すときは、§6 を見てください。
- **現在の版を見る**:
  ```sh
  kubectl -n launchs exec postgres-0 -- psql -U postgres -d app  -c "select version, dirty from schema_migrations"
  kubectl -n launchs exec postgres-0 -- psql -U postgres -d auth -c "select version, dirty from schema_migrations"
  ```
  `version` が、`migrations/app/` と `migrations/auth/` の、最後の番号と同じで、`dirty` が `f` なら、最新です。
- **マイグレーションのログ**:
  ```sh
  kubectl -n launchs logs deploy/backend -c migrate
  kubectl -n launchs logs deploy/auth -c migrate
  ```

### 2.1 マイグレーションを足すときの決まり(後方互換)
ローリングアップデートでは、**新しい Pod が、マイグレーションを流したあとも、古い Pod が、しばらく動き続けます**(新しい Pod が Ready になるまで)。そのため、マイグレーションは、**1 つ前の版のアプリが、そのまま動けるもの**にします。

| やってよい(後方互換) | やってはいけない(2 回のリリースに分ける) |
|---|---|
| 表・列・インデックスの**追加**(列は、NULL を許すか、`DEFAULT` を付ける) | 列・表の**削除**、列の名前の変更 |
| 制約を**ゆるめる** | 制約を**きつくする**(古い版が、書き込めなくなる) |

破壊的な変更は、「①新しい版で、新しい列を使い始める → ②その版が、全部に行き渡ってから、次のリリースで、古い列を消す」の、2 段階にします。

## 3. リリースの手順(通常の更新)
1. **更新する内容を確認する**: マージされた PR に、**マイグレーションの追加**(`migrations/…`)や、**環境変数・RBAC の変更**(`deploy/` の差分)が、あるかを見ます。ある場合は、§3.2 も読みます。
2. **イメージが出ていることを確認する**: GitHub Actions の `release` が、成功していて、`ghcr.io/launchs-org/v2/<名前>:sha-<短い SHA>` が、10 個(backend、worker、watcher、auth、logcollector、accessstats、exporter、admin、frontend、migrate)ともあること。
3. **バックアップを取る**(§5)。**マイグレーションがある更新は、必ず**取ります。
4. **タグを取り込む**: `release` が成功すると、CI が、`deploy/kustomization.yaml` の `images` の、**8 つ全部**の `newTag` を、今回の `sha-…` に書き換えて、`main` に、`deploy: イメージのタグを sha-… に更新する` というコミットを、自動で入れます(§3.3)。**`git pull` で、このコミットを取り込みます**。手元のコピーで運用しているときは、`kustomization.yaml` の `newTag` を、同じ `sha-…` に、8 つ全部書き換えます(1 つでも古いと、アプリとマイグレーションの版が、食い違います)。
5. **差分を見る**:
   ```sh
   kubectl diff -k .
   ```
   変わるのが、イメージのタグ(と、意図した変更)だけであることを、確認します。
6. **適用する**:
   ```sh
   kubectl apply -k .
   ```
7. **進み具合を見る**:
   ```sh
   kubectl -n launchs get pods -w
   kubectl -n launchs rollout status deploy/backend deploy/auth deploy/worker deploy/frontend
   ```
   backend と auth は、先に、initContainer(`migrate`)が走ります。
8. **確認する**:
   - `curl https://<ホスト名>/healthz` が 200。
   - 画面を開いて、ログインできる。
   - §2 の、マイグレーションの版が、最新。
   - 異常な Pod がない: `kubectl -n launchs get pods | grep -v Running`
9. **記録する**: どの `sha-…` を、いつ、適用したかを、控えます(戻すときに、使います)。

### 3.3 タグの自動更新(CI)
- **しくみ**: `.github/workflows/release.yml` の `update-tags` ジョブが、10 個のイメージが、全部出たあとで、`.github/scripts/update-image-tags.sh` を使って、`newTag` を書き換え、`main` にコミットします(`github-actions[bot]`)。**クラスターには、適用しません**(オンプレで、GitHub のランナーから届かないため)。
- **無限ループにならない**: `GITHUB_TOKEN` での push は、新しいワークフローを起こさないので、このコミットで、`release` は、もう一度動きません。
- **`main` への直接 push**: ブランチ保護で、直接 push が禁止されていると、このジョブは、失敗します(その場合は、ブランチ保護の例外に、`github-actions[bot]` を足すか、PR を作る方式に、替えます)。
- **失敗したとき**: ジョブが失敗しても、イメージは、すでに出ています。`gh workflow run release.yml` で、やり直すか、手で、`newTag` を書き換えます。
- **手元で試す**: `bash .github/scripts/update-image-tags.sh sha-abc1234 deploy/kustomization.yaml`(`sha-<16進の7〜40文字>` 以外は、拒否します。行が 8 つでないときも、止まります)。
- **`develop` とのずれ**: このコミットは、`main` だけに入ります。`develop` には入らないので、次に `develop` → `main` の PR を作る前に、`main` を `develop` に取り込んでおくと、`kustomization.yaml` で、競合しません。

### 3.1 `latest` タグで運用しているとき
`newTag: latest` のままだと、マニフェストが変わらないので、`kubectl apply -k .` では、Pod が、入れ替わりません。新しいイメージを使うには、次のようにします。
```sh
kubectl -n launchs rollout restart deploy/backend deploy/auth deploy/worker deploy/watcher deploy/logcollector deploy/accessstats deploy/exporter deploy/frontend
```
ただし、**どの版が動いているかが、分からなくなる**ので、本番では、`sha-…` で、固定してください。

### 3.2 マイグレーションがある更新の注意
- **順番**: backend と auth の initContainer が、マイグレーションを流します。worker、watcher、logcollector、accessstats は、**マイグレーションを流さず**、同じデータベースを使います。新しい列を使う版の worker が、先に起動して、まだ列がない、という状況を避けるため、**backend と auth が、先に更新される**ようにします(`kubectl apply -k .` は、全部を一度に更新するので、worker などが、先に起動して、一時的にエラーを出して、再起動することがあります。数分で、落ち着きます)。
- **時間がかかるマイグレーション**(大きな表の、インデックス作成など)は、その間、Pod が起動しません。事前に、手動で流してから、更新します(§4)。

## 4. マイグレーションを、手動で流す・確かめる
Pod の更新と、切り離して、先に流したいとき、または、状態を確かめたいときに使います。
```sh
PW=$(kubectl -n launchs get secret launchs-secret -o jsonpath='{.data.POSTGRES_PASSWORD}' | base64 -d)
TAG=sha-xxxxxxx     # 流したい版のタグ

# 現在の版を見る
kubectl -n launchs run migrate-manual --rm -i --restart=Never --labels=admission.datadoghq.com/enabled=false \
  --image=ghcr.io/launchs-org/v2/migrate:$TAG -- \
  -path=/migrations/app -database="postgres://postgres:${PW}@postgres:5432/app?sslmode=disable" version

# 最新まで流す(auth は、-path=/migrations/auth、データベース名を auth にする)
kubectl -n launchs run migrate-manual --rm -i --restart=Never --labels=admission.datadoghq.com/enabled=false \
  --image=ghcr.io/launchs-org/v2/migrate:$TAG -- \
  -path=/migrations/app -database="postgres://postgres:${PW}@postgres:5432/app?sslmode=disable" up
```

### 4.1 マイグレーションが失敗して、止まった(dirty)
`schema_migrations` の `dirty` が `t` のとき、途中で失敗したマイグレーションが、あります。**そのままでは、次の `up` が、動きません**(`Dirty database version N. Fix and force version.`)。
1. **ログで原因を見る**(§2)。SQL のエラーなら、どこまで反映されたかを、確かめます。
2. **中途半端な変更を、手で元に戻す**(SQL で、該当の表・列を、整える)。トランザクションで流れるマイグレーションは、失敗すると、全部ロールバックされるので、何も変わっていないことが多いです。
3. **版を、直前に戻して、dirty を直す**:
   ```sh
   ... force <直前の版の番号>      # 例: 22 で成功、23 で失敗したなら、force 22
   ```
4. **原因を直した版(新しいイメージ)で、もう一度 `up`**。
**`force` は、データを直しません**(印を付け替えるだけです)。何が起きたかを、理解してから、使ってください。

## 5. バックアップと復元
### 5.1 バックアップ(更新の前に)
PostgreSQL は、Pod が 1 つで、データは、Longhorn の PVC(3 複製)に、あります。複製は、**ディスクの故障には強い**ですが、**誤った削除や、壊れたマイグレーションには、守ってくれません**。論理バックアップ(`pg_dump`)を取ります。
```sh
DATE=$(date +%Y%m%d-%H%M)
for DB in app auth; do
  kubectl -n launchs exec postgres-0 -- pg_dump -U postgres -Fc $DB > backup-$DB-$DATE.dump
done
ls -la backup-*-$DATE.dump     # 大きさが 0 でないことを確かめる
```
- **Temporal のデータ**(`temporal`、`temporal_visibility`)は、動いている処理の状態です。通常は、バックアップしなくても、launchs のデータは、失われません(動いている処理は、やり直しになりえます)。取るなら、同じ要領で、DB 名を変えます。
- **ダンプは、安全な場所に、保管**してください(中身に、ユーザーのデータが、含まれます)。リポジトリには、置きません。

### 5.2 Longhorn のスナップショット(早く戻したいとき)
Longhorn の画面(または `kubectl`)で、PostgreSQL の PVC(`data-postgres-0`)の、**スナップショット**を作ると、更新の直前の状態に、短い時間で、戻せます。`pg_dump` の代わりではなく、**併用**します。

### 5.3 復元
**データを上書きします。** 復元の前に、アプリを止めます。
```sh
# 1. アプリを止める(データベースに、つながないようにする)
kubectl -n launchs scale deploy/backend deploy/auth deploy/worker deploy/watcher deploy/logcollector deploy/accessstats deploy/exporter --replicas=0

# 2. 復元する(--clean で、既存のものを消してから)
kubectl -n launchs exec -i postgres-0 -- pg_restore -U postgres -d app --clean --if-exists --no-owner < backup-app-<日時>.dump
kubectl -n launchs exec -i postgres-0 -- pg_restore -U postgres -d auth --clean --if-exists --no-owner < backup-auth-<日時>.dump

# 3. 版を戻して、アプリを起動する(§6)
kubectl apply -k .      # replicas は、kustomization.yaml の値に戻る
```

## 6. 更新を戻す(ロールバック)
**アプリ**は、`newTag` を、前の `sha-…` に戻して、`kubectl apply -k .` です(§9 の記録を使います)。

**データベース**は、`down` がないので、自動では戻りません。
- **後方互換のマイグレーション**(§2.1)だけなら、アプリを、前の版に戻すだけで、動きます(増えた列や表は、使われないだけです)。**データベースは、戻しません**。
- **破壊的な変更を、入れてしまった**、または、データが壊れたときは、§5.3 で、更新の前のバックアップから、復元します(更新のあとに書き込まれたデータは、失われます)。

## 7. 運用のコマンド集
```sh
# 管理者を、昇格させる(サインアップ済みのユーザー名)
kubectl -n launchs exec deploy/auth -- /auth admin promote <userName>

# 管理者を、作る(パスワードは、標準入力)
printf '%s' "<パスワード>" | kubectl -n launchs exec -i deploy/auth -- /auth admin create <userName>

# ログ(Pod が複数あるときは、Pod の名前を指定する)
kubectl -n launchs logs deploy/backend --tail=100
kubectl -n launchs logs deploy/worker  --tail=100

# 台数を変える(恒久的には、kustomization.yaml の replicas を変える)
kubectl -n launchs scale deploy/frontend --replicas=2

# 1 つだけ、再起動する
kubectl -n launchs rollout restart deploy/backend
```
- **招待コードの発行**: 管理者でログインして、`POST /auth/admin/invitations` を呼びます([dev-docs/endpoints/auth_admin.md](../dev-docs/endpoints/auth_admin.md))。
- **サインアップの公開範囲**: `settings/config.yaml` の `AUTH_SIGNUP_MODE`(`open` か `invite`)を変えて、`kubectl apply -k settings/` し、`rollout restart deploy/auth` します(設定は、本体とは別に適用します)。

## 8. よくある問題
| 症状 | 原因と対処 |
|---|---|
| backend、auth が `Init:CrashLoopBackOff` | `kubectl logs … -c migrate` を見る。`lookup postgres … no such host` なら、Service `postgres` が、ない。`database "app" does not exist` なら、DB が作られていない(§8.1)。`Dirty database version` なら、§4.1。 |
| `too many clients already` | PostgreSQL の `max_connections`(`postgres.yaml`)が、足りない。レプリカを増やしたときは、見直す([description.md](description.md) の「高可用性」)。 |
| 新しいイメージにしたのに、変わらない | `latest` で、マニフェストが同じ(§3.1)。または、`newTag` が、8 つのうち、一部しか変わっていない。 |
| `ImagePullBackOff` | タグが、存在しない(`release` が、失敗している)、または、GHCR のパッケージが、非公開で、`imagePullSecrets` がない。 |
| worker、watcher が、`database … does not exist` | マイグレーションが、まだ流れていない(backend の更新を、待つ)。 |

### 8.1 データベース(`app`、`auth`、`temporal`)が、ない
初期化 SQL(`postgres-init`)は、PostgreSQL の**データを最初に作るときだけ**動きます。すでに PVC があって、DB がないときは、手で作ります。
```sh
for DB in app auth temporal temporal_visibility; do
  kubectl -n launchs exec postgres-0 -- psql -U postgres -c "CREATE DATABASE $DB"
done
```

## アクセスログの転送(exporter)を有効にする

アクセスログを、webhook や Google Drive に書き出す機能(`dev-docs/log-export.md`)は、**Secret `launchs-secret` に、次の値を足して、backend と exporter を再起動する**と、有効になります(**無くても、Pod は起動します**。そのときは、機能が無効です)。

| キー | 中身 | 無いと |
|---|---|---|
| `EXPORT_CREDENTIAL_KEY` | 秘密(Google の合鍵、webhook の署名の秘密)を DB に暗号化して保存する鍵。`openssl rand -base64 32` | **転送の機能が、全部無効**(作る・テストは 503) |
| `GOOGLE_OAUTH_CLIENT_ID`、`GOOGLE_OAUTH_CLIENT_SECRET` | Google Cloud の OAuth クライアント(種類は Web アプリ) | **Google Drive だけ**が無効(webhook は使える) |

- **`EXPORT_CREDENTIAL_KEY` は、変えない**。変えると、保存済みの秘密を復号できなくなり、Google の再接続と、webhook の作り直しが、全員に要ります。バックアップして、保管してください。
- Google Cloud 側: OAuth クライアントのリダイレクト URI に `https://<LAUNCHS_HOST>/api/v1/export/google/callback` を登録します。同意の画面は**「本番」に公開**します(「テスト」のままだと、合鍵が **7 日で切れます**。`drive.file` は審査の軽い権限です)。戻り先を変えるときは、`GOOGLE_OAUTH_REDIRECT_URL`(ConfigMap か Secret)。
- exporter は、外向きの HTTPS(webhook の宛先と、Google の API)に接続します。`launchs` の namespace から、外へ出られることを確認してください。
- 確認: `kubectl -n launchs logs deploy/exporter` に「exporter を起動する」と出て、リーダーになったものが 1 つあること。`EXPORT_CREDENTIAL_KEY` が空だと、「エクスポートは無効です」と出て、待機します。
