# zenzai-kkc-server

かな漢字変換の HTTP API サーバ．変換エンジンに
[AzooKeyKanaKanjiConverter](https://github.com/azooKey/AzooKeyKanaKanjiConverter) の
ニューラルかな漢字変換システム Zenzai を使う．

Linux / CPU のみで動作する．GPU は不要．

## 概要

このリポジトリはビルド定義とサーバ実装だけを持つ．第三者のコード・辞書・モデルは
**コミットしていない**．ビルド時にコミットハッシュで固定して取得する．

成果物は 2 つ．

| 成果物 | 用途 |
|---|---|
| コンテナイメージ | Docker / Podman が使える環境 |
| tar バンドル | コンテナが使えない環境（LXC 等）．同じイメージから書き出す |

## 動作要件

- x86-64，**AVX2 必須**（2013 年以降の CPU）．llama.cpp を AVX2 でビルドしている
- tar バンドルを使う場合は glibc 2.36 以上（Debian 12 以降）．
  Swift ランタイムを同梱しているのでツールチェーンのインストールは不要

## 導入

### コンテナ

```sh
podman compose up -d
```

既定では `127.0.0.1:61234` に公開する．公開先を変えるときは `ZKKC_PUBLISH` を渡す．

### ホストに直接（LXC 等）

root で実行する．

```sh
ZKKC_PORT=61234 sh -c "$(curl -fsSL https://raw.githubusercontent.com/keufcp/zenzai-kkc-server/main/scripts/setup.sh)"
```

GitHub Releases から tar バンドルを取得して `/opt/zenzai-kkc` に展開し，
systemd サービス `zenzai-kkc` として登録・起動する．
`/v1/health` が応答するまで待ってから終了するので，スクリプトが成功したなら
サーバは実際に応答できる状態にある．

版を固定する場合は，スクリプトも同じタグから取る．

```sh
ZKKC_PORT=61234 sh -c "$(curl -fsSL https://raw.githubusercontent.com/keufcp/zenzai-kkc-server/v0.1.0/scripts/setup.sh)" setup v0.1.0
```

手元でビルドしたバンドルを使うときは `ZKKC_BUNDLE` にパスを渡す．

### 自分でビルドする

```sh
podman build -t zenzai-kkc:latest .
podman run --rm --entrypoint /opt/zenzai-kkc/verify-bundle.sh zenzai-kkc:latest
scripts/export-bundle.sh
```

`dist/zenzai-kkc-bundle.tar.gz` が書き出される．

### リリース

`v` で始まるタグを push すると `.github/workflows/release.yml` が
イメージをビルドして検証し，tar バンドルと sha256 を Release に添付する．

## API

`docs/openapi.yaml` が唯一の規定．エンドポイントは `GET /v1/convert` と
`GET /v1/health` の 2 つ．

```sh
curl "http://127.0.0.1:61234/v1/convert?text=きょうはいいてんきですね"
```

## 設定

すべて環境変数で行う．

| 変数 | 既定 | 説明 |
|---|---|---|
| `ZKKC_BIND` | `127.0.0.1` | 待ち受けアドレス．既定では外部に晒さない |
| `ZKKC_PORT` | **なし（必須）** | 待ち受けポート．未設定なら起動しない |
| `ZKKC_MODEL` | 同梱の zenz-v3.2-xsmall | GGUF モデルのパス |
| `ZKKC_INFERENCE_LIMIT` | `1` | Zenzai の推論回数上限 |

### ポート番号に既定値が無い理由

既定値があると他のサービスと衝突しうる．運用者が明示的に選ぶ．

- **32768–60999 は避ける．** Linux の ephemeral port range の既定値であり，
  OS が外向き接続に使う．ここに固定リッスンを置くと，その番号がたまたま使用中のときに
  bind が失敗する．起動タイミングに依存する不定な障害になる
- **61000–65535 が安全．** ephemeral range の上，かつ IANA 未割り当ての動的ポート範囲

サーバは起動時に `/proc/sys/net/ipv4/ip_local_port_range` を読み，
指定ポートがその範囲内なら警告を出す．

## モデルの選択

zenz-v3.2 の xsmall と small を同梱する．既定は **xsmall**．

Intel N95（2 コア割り当て）での実測値．チャット文 207 文，平均読み長 10.7 かな．

| 構成 | 文正解率 | 中央値 | p95 | 最大 RSS |
|---|---:|---:|---:|---:|
| Zenzai なし | 80.7% | 5.3 ms | 14.4 ms | 43 MB |
| **zenz-v3.2-xsmall** | **87.0%** | **31.1 ms** | **48.7 ms** | 88 MB |
| zenz-v3.2-small | 88.4% | 116.7 ms | 159.3 ms | 145 MB |

small は 1.4 ポイントの精度と引き換えに中央値が 3.7 倍になる．
低消費電力 CPU では xsmall が妥当．余力のある環境では `ZKKC_MODEL` で切り替える．

## upstream への改変

`ZenzaiCPU` trait は GPU バックエンドを含まない llama.cpp ではモデルをロードできない．
`patches/` に回避パッチを置き，ビルド時に `git apply` で適用する．

詳細は `patches/` 内の各パッチのヘッダを参照．

## 第三者コードのライセンス

ビルド成果物には以下が含まれる．頒布する場合は成果物に同梱される
`THIRD-PARTY-NOTICES` を参照すること．

このファイルはビルド時に `scripts/collect-notices.sh` が生成する．
ライセンス本文は clone したリポジトリと SwiftPM の checkouts にしか存在せず，
ランタイムイメージには残らないため，ビルドステージで収集する必要がある．
期待するライセンスファイルが 1 つでも欠けていればビルドが失敗する．

| 構成要素 | 出所 | ライセンス |
|---|---|---|
| AzooKeyKanaKanjiConverter | azooKey/AzooKeyKanaKanjiConverter | MIT |
| 既定辞書 | azooKey/azooKey_dictionary_storage | Apache-2.0 |
| llama.cpp / ggml | azooKey/llama.cpp | MIT |
| zenz-v3.2 モデル | Miwa-Keita/zenz-v3.2-{xsmall,small}-gguf | Apache-2.0 |
| Swift ランタイム | apple/swift | Apache-2.0 with Runtime Library Exception |
| SwiftyMarisa / marisa-trie | ensan-hcl/SwiftyMarisa | BSD-2-Clause または LGPL のデュアル |
| swift-algorithms / collections / argument-parser / numerics | apple | Apache-2.0 |
| swift-tokenizers | ensan-hcl/swift-tokenizers | Apache-2.0 |
| swift-jinja | huggingface/swift-jinja | Apache-2.0 |

### 絵文字辞書を同梱しない

AzooKeyKanaKanjiConverter は絵文字辞書を submodule として持つが，
[azooKey_emoji_dictionary_storage](https://github.com/azooKey/azooKey_emoji_dictionary_storage)
には LICENSE ファイルが無く，README にも記載が無い．明示の許諾が無いため頒布物に含めない．

かな漢字変換には不要であり，削除しても変換が正常に動作することを確認済み．

## ライセンス

このリポジトリのコードは MIT（`LICENSE`）．
ビルド成果物に含まれる第三者のコードはそれぞれのライセンスに従う．
