---
title: "インターネットに公開する"
nav: "インターネットに公開する"
lead: "アプリに URL を付けて、誰でも開けるようにする方法です。"
crumb: "機能ごとの使い方"
---

## 公開のながれ

<figure>
<svg viewBox="0 0 880 130" role="img" aria-label="Service でポートを決め、Ingress でルールを作ると、URL で開けるようになる">
<defs><marker id="arr" markerWidth="10" markerHeight="10" refX="8" refY="5" orient="auto"><path d="M0,0 L10,5 L0,10 z" fill="#8a86a6"/></marker></defs>
<rect class="svg-main" x="10" y="25" width="250" height="80" rx="14"/><text class="svg-text" x="135" y="62" text-anchor="middle">① Service を作る</text><text class="svg-sub" x="135" y="86" text-anchor="middle">アプリのポートを決める</text>
<rect class="svg-main" x="315" y="25" width="250" height="80" rx="14"/><text class="svg-text" x="440" y="62" text-anchor="middle">② Ingress を作る</text><text class="svg-sub" x="440" y="86" text-anchor="middle">URL が自動で付く</text>
<rect class="svg-main" x="620" y="25" width="250" height="80" rx="14"/><text class="svg-text" x="745" y="62" text-anchor="middle">③ ルールを反映</text><text class="svg-sub" x="745" y="86" text-anchor="middle">「このパス → このアプリ」</text>
<path class="svg-line" d="M262,65 L312,65"/><path class="svg-line" d="M567,65 L617,65"/>
</svg>
</figure>

:::note tip はじめてのデプロイで「外からアクセスできるようにする」を選んだ人は
①と②は、すでに済んでいます。③のルールを、確認するだけです。
:::

## ① Service で、ポートを決める

アプリが、お客さんを待っている**窓口の番号(ポート)**を、教えます。デプロイのパネルの [[Service]] タブで、ポートを追加します。

| 項目 | 入れるもの |
|---|---|
| 名前 | 自由に付ける名前(例: `web`)。 |
| ポート | アプリが使う番号。`nginx` や、ふつうのホームページなら `80`、アプリによっては `3000` や `8080` など。**アプリの説明書で確認**しましょう。 |
| プロトコル | **tcp** か **udp**。ホームページ(Web)は **tcp**。 |

[[保存]] → [[反映]] で、有効になります。

:::note warn インターネットに公開できるのは、tcp の、Web(HTTP)だけ
udp や、Web 以外の通信の公開は、まだ対応していません。
:::

## ② Ingress を作る(公開の入口)

:::steps
1. **追加する**

   構成図の右上 [[+ 追加]] → [[Ingress]]。

2. **URL を確認する**

   「**形容詞-色-動物**」のような、覚えやすい名前の URL が、**自動**で付きます(例: `https://brave-blue-fox.example.org`)。自分で、名前を選ぶことは、できません。
:::

:::note info この時点では、まだ公開されていません
Ingress を作っただけでは、ルールが空なので、URL を開いても、つながりません。次の③が、必要です。
:::

## ③ ルールを作って、反映する

「**どのパスに来たお客さんを、どのアプリのどのポートに、届けるか**」を決めます。Ingress のカードを押して、右のパネルの「ルール」で、[[+ ルール追加]]。

| 項目 | 意味 | 例 |
|---|---|---|
| パス | URL の、ドメインより後ろの部分。`/` は「全部」。 | `/`、`/api` |
| Service | 届け先のアプリ。**動いている(active)で、tcp のポートがある**ものが、選べます。 | nginx ・ service-… |
| ポート | 届け先のポート番号。 | `80` |

- パスは **/ で始めます**。英数字と `. _ ~ - % /` が使えます(200 文字まで)。
- 同じパスを、2 つ作ることは、できません。ルールは、**50 件**まで。
- パスは「**その文字で始まるもの、全部**」に、一致します(`/api` は、`/api/users` にも一致)。**長い(くわしい)パスが、先に**使われます。`/` と `/api` の両方を作ると、`/api…` は後者に、それ以外は前者に、行きます。

:::steps
1. **保存する**

   [[保存]]。まだ、公開は変わりません。「反映待ち」の帯と、変更内容が出ます。

2. **反映する**

   [[反映]]。これで、公開されます。反映中は、状態が [[run:applying]] になります。

3. **URL を開く**

   パネルの「公開 URL」を押します。**数分**かかることがあります。
:::

:::note tip URL は、https(暗号化された通信)で開きます
入口の手前で、暗号化の処理をしています。アプリ側で、特別な設定は、要りません。
:::

## 公開をやめる

- **ルールを全部消して、反映** → URL を開いても、つながらなくなります(Ingress は残ります)。
- **Ingress を削除** → URL ごと、なくなります。あとで作り直しても、**同じ URL にはなりません**(新しい名前が付きます)。

## どんなお客さんが来たかを見る

Ingress のパネルに、**アクセスログ**(1 回ずつのアクセスの記録)が出ます。くわしくは [様子を見る](monitoring.md#access)。
