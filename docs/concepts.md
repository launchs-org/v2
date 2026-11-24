---
title: "しくみと言葉"
nav: "しくみと言葉"
lead: "画面に出てくる言葉を、たとえ話つきで説明します。この 1 ページで、全体像がつかめます。"
crumb: "はじめに"
---

## 全体の絵

<figure>
<svg viewBox="0 0 880 250" role="img" aria-label="インターネットから、公開の入口、アプリ、データの置き場所へのつながり">
<defs><marker id="arr" markerWidth="10" markerHeight="10" refX="8" refY="5" orient="auto"><path d="M0,0 L10,5 L0,10 z" fill="#8a86a6"/></marker></defs>
<rect class="svg-box" x="10" y="85" width="150" height="80" rx="14"/><text class="svg-text" x="85" y="122" text-anchor="middle">🌐 インターネット</text><text class="svg-sub" x="85" y="145" text-anchor="middle">あなたのお客さん</text>
<rect class="svg-box" x="215" y="85" width="170" height="80" rx="14"/><text class="svg-text" x="300" y="122" text-anchor="middle">🚪 Ingress</text><text class="svg-sub" x="300" y="145" text-anchor="middle">公開の入口(URL)</text>
<rect class="svg-main" x="440" y="70" width="190" height="110" rx="14"/><text class="svg-text" x="535" y="112" text-anchor="middle">📦 デプロイ</text><text class="svg-sub" x="535" y="136" text-anchor="middle">アプリ本体</text><text class="svg-sub" x="535" y="156" text-anchor="middle">(Service で受付)</text>
<rect class="svg-box" x="700" y="15" width="170" height="80" rx="14"/><text class="svg-text" x="785" y="52" text-anchor="middle">💾 ボリューム</text><text class="svg-sub" x="785" y="75" text-anchor="middle">消えないデータ</text>
<rect class="svg-box" x="700" y="150" width="170" height="80" rx="14"/><text class="svg-text" x="785" y="187" text-anchor="middle">📄 ConfigMap</text><text class="svg-sub" x="785" y="210" text-anchor="middle">設定ファイル</text>
<path class="svg-line" d="M160,125 L212,125"/><path class="svg-line" d="M385,125 L437,125"/><path class="svg-line" d="M630,100 L697,60"/><path class="svg-line" d="M630,150 L697,190"/>
</svg>
<figcaption>お客さんが URL を開くと、入口(Ingress)を通って、アプリ(デプロイ)に届きます。アプリは、データと設定ファイルを、必要なら使います。</figcaption>
</figure>

## 言葉の説明

:::words
🗂 プロジェクト :: アプリ一式を入れる**箱**。1 つのサービスや、1 つのお店を、1 つのプロジェクトにする、というイメージです。プロジェクトごとに、ほかのプロジェクトから隔てられて(のぞけない・つながらない)います。
📦 デプロイ :: アプリを**実際に動かすこと**、または、動いているアプリ 1 つ。「コンテナイメージ」(アプリの入った、箱詰めのファイル)を選ぶと、launchs が動かします。
🔌 Service(サービス) :: アプリが**受け付ける、ポート(窓口の番号)**を決めるもの。「このアプリは、80 番で、お客さんを待っています」という宣言です。
🚪 Ingress(イングレス) :: インターネットに公開する**入口**。URL(例: `https://brave-blue-fox.example.org`)が自動で付き、「どのパスに来たお客さんを、どのアプリに届けるか」の**ルール**を決めます。
💾 ボリューム :: アプリを作り直しても**消えない、データの置き場所**。データベースのデータや、アップロードされた画像など、残したいものを置きます。
📄 ConfigMap(コンフィグマップ) :: アプリに渡す**設定ファイル**のセット。画面から、書いたり、ファイルをアップロードしたりできます。
🧰 テンプレート :: **よく使う構成のひな形**。「WordPress を動かしたい」のように、必要な部品(アプリ、データ、公開の入口)を、まとめて作ります。
🔢 レプリカ(台数) :: 同じアプリを**何台**動かすか。台数を増やすと、たくさんのお客さんを、さばけます(0〜5 台)。
🧱 Pod(ポッド) :: 動いているアプリ 1 台の、実体。「台数 3」なら、Pod が 3 つ動きます。
:::

## 変更の「下書き → 反映」のしくみ

launchs では、動いているアプリに、**いきなり変更が入ることはありません**。変更は、いったん「**未反映(下書き)**」として保存されて、[[反映]]を押したときに、はじめて動き出します。

:::steps
1. **保存する**

   画面で値を変えて、[[保存]]を押します。この時点では、**動いているアプリは、何も変わりません**。

2. **確認する**

   「未反映の変更があります」という、帯が出ます。何が変わるのか、内容を確認できます。

3. **反映する、または、破棄する**

   [[反映]]を押すと、実際に変更が入ります。やめたいときは、[[破棄]]を押すと、下書きが消えます。
:::

:::note tip 安心ポイント
反映に失敗しても、**元の動いていた状態に戻す**ようになっています。まず保存して、よく見てから、反映しましょう。
:::

:::note warn 反映すると、アプリが再起動することがあります
環境変数などを変えて反映すると、アプリが作り直されます。そのあいだ、短く、つながらなくなることがあります。
:::

## 状態の見方

カードや一覧に出る、状態の表示です。

| 表示 | 意味 |
|---|---|
| [[ok:active]] | 動いています(問題なし)。 |
| [[run:pending / applying]] | 作っている途中、または、反映している途中です。**終わるまで、ほかの操作はできません**。 |
| [[bad:failed]] | 失敗しました。理由は、「タスク」「イベント」「ログ」で、探せます([困ったとき](faq.md))。 |
| [[warn:deleting]] | 削除している途中です。 |
