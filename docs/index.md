---
title: "launchs へようこそ"
nav: "ホーム"
home: true
---

<section class="hero">
<h1>launchs へようこそ</h1>
<p>自分で作った、または、公開されているアプリを、<b>画面を数回クリックするだけ</b>で動かして、インターネットに公開できるサービスです。サーバーの難しい知識は、要りません。</p>
</section>

<div class="cards">
<a class="card" href="start.html"><div class="ico">🚀</div><b>はじめてのガイド</b><span>アカウントを作って、最初のアプリを動かすまで。いちばん最初は、ここから。</span></a>
<a class="card" href="concepts.html"><div class="ico">🧩</div><b>しくみと言葉</b><span>「デプロイ」「Ingress」など、画面に出てくる言葉を、やさしく説明します。</span></a>
<a class="card" href="deploy.html"><div class="ico">📦</div><b>機能ごとの使い方</b><span>アプリの動かし方、公開のしかた、データの置き方、様子の見方。</span></a>
<a class="card" href="faq.html"><div class="ico">🛟</div><b>困ったとき(FAQ)</b><span>動かない、見られない、押せない。よくある困りごとと、直し方。</span></a>
</div>

## launchs でできること {#can}

<ul class="can">
<li><b>アプリを動かす</b>公開されているアプリ(コンテナイメージ)を選ぶだけで、動かせます。</li>
<li><b>インターネットに公開する</b>誰でも開ける URL(ホームページのアドレス)を、自動で用意します。</li>
<li><b>データを残す</b>アプリを作り直しても消えない、データの置き場所(ボリューム)を使えます。</li>
<li><b>設定ファイルを渡す</b>設定ファイルを、画面から書いて、アプリに渡せます。</li>
<li><b>ひな形から一括で作る</b>WordPress、データベース、nginx などを、ひな形(テンプレート)から、まとめて作れます。</li>
<li><b>様子を見る</b>ログ、できごと、裏の処理、アクセスの記録を、画面で確認できます。</li>
</ul>

## 知りたいことから探す

- **とにかく動かしたい** → [最初にデプロイする方法](first-deploy.md)
- **言葉を知りたい** → [コンテナ](what-is-container.md)、[ボリューム](what-is-volume.md)、[Service](what-is-service.md)、[Ingress](what-is-ingress.md)、[ConfigMap](what-is-configmap.md)、[タグ](what-is-tag.md)、[タスク](what-is-task.md)、[メトリクス](metrics.md)
- **データベースを使いたい** → [DBを使う場合](database.md)
- **push で自動デプロイしたい** → [GitHub との連携](github.md)、[CI/CD をする方法](cicd.md)
- **運営する人** → [管理者向けページ](admin.md)

## どう読めばいい?

:::steps
1. まず [はじめてのガイド](start.md) を、上から順にやってみます(15 分くらい)。
2. 画面に出てくる言葉が分からなくなったら、[しくみと言葉](concepts.md) を見ます。
3. やりたいことが決まったら、「機能ごとの使い方」から、該当のページを開きます。
4. うまくいかないときは、[困ったとき](faq.md) を見ます。
:::

:::note info このガイドの前提
launchs を運営している人(管理者)から、サイトの URL と、(必要なら)招待コードを受け取っている、という前提で書いています。
:::
