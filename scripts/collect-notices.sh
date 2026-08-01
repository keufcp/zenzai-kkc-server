#!/bin/sh
# 成果物に含まれる第三者コードのライセンス表示を 1 ファイルにまとめる．
#
# ビルドステージ内で実行する．ライセンス本文は clone したリポジトリと
# SwiftPM の checkouts にしか存在せず，ランタイムイメージには残らないため．
#
# 期待するファイルが 1 つでも欠けたらビルドを失敗させる．
# 不完全な表示のまま頒布物を作らないようにするため．

set -eu

OUT=${1:?specify the output path}
AKKC=${AKKC_DIR:-/src/akkc}
LLAMA=${LLAMA_DIR:-/src/llama.cpp}
CHECKOUTS="$AKKC/.build/checkouts"
DICT="$AKKC/Sources/KanaKanjiConverterModuleWithDefaultDictionary/azooKey_dictionary_storage"

emit() {
    name=$1
    origin=$2
    path=$3
    if [ ! -f "$path" ]; then
        echo "collect-notices: license file not found ($name: $path)" >&2
        exit 1
    fi
    {
        echo "================================================================================"
        echo "$name"
        echo "Source: $origin"
        echo "================================================================================"
        echo
        cat "$path"
        echo
        echo
    } >> "$OUT"
}

: > "$OUT"

{
    echo "THIRD-PARTY NOTICES"
    echo
    echo "zenzai-kkc-server の成果物には以下の第三者ソフトウェアが含まれる．"
    echo "それぞれのライセンス全文を後段に収録する．"
    echo
    echo "生成日時: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo
} >> "$OUT"

emit "AzooKeyKanaKanjiConverter" \
     "https://github.com/azooKey/AzooKeyKanaKanjiConverter" \
     "$AKKC/LICENSE"

emit "azooKey_dictionary_storage (既定辞書)" \
     "https://github.com/azooKey/azooKey_dictionary_storage" \
     "$DICT/LICENSE"

emit "llama.cpp / ggml" \
     "https://github.com/azooKey/llama.cpp" \
     "$LLAMA/LICENSE"

emit "Swift ランタイム" \
     "https://github.com/apple/swift" \
     "/usr/share/swift/LICENSE.txt"

emit "swift-algorithms" \
     "https://github.com/apple/swift-algorithms" \
     "$CHECKOUTS/swift-algorithms/LICENSE.txt"

emit "swift-collections" \
     "https://github.com/apple/swift-collections" \
     "$CHECKOUTS/swift-collections/LICENSE.txt"

emit "swift-argument-parser" \
     "https://github.com/apple/swift-argument-parser" \
     "$CHECKOUTS/swift-argument-parser/LICENSE.txt"

emit "swift-numerics" \
     "https://github.com/apple/swift-numerics" \
     "$CHECKOUTS/swift-numerics/LICENSE.txt"

emit "swift-tokenizers" \
     "https://github.com/ensan-hcl/swift-tokenizers" \
     "$CHECKOUTS/swift-tokenizers/LICENSE"

emit "Jinja" \
     "https://github.com/huggingface/swift-jinja" \
     "$CHECKOUTS/Jinja/LICENSE"

emit "SwiftyMarisa / marisa-trie" \
     "https://github.com/ensan-hcl/SwiftyMarisa" \
     "$CHECKOUTS/SwiftyMarisa/LICENSE.md"

# zenz のモデルには LICENSE ファイルが同梱されていない．
# Hugging Face のモデルカードのメタデータで Apache-2.0 と宣言されている．
# 全文は上記 azooKey_dictionary_storage の LICENSE（Apache-2.0）と同一．
{
    echo "================================================================================"
    echo "zenz-v3.2-xsmall / zenz-v3.2-small (GGUF モデル)"
    echo "Source: https://huggingface.co/Miwa-Keita/zenz-v3.2-xsmall-gguf"
    echo "        https://huggingface.co/Miwa-Keita/zenz-v3.2-small-gguf"
    echo "================================================================================"
    echo
    echo "Apache License 2.0"
    echo
    echo "これらのリポジトリには LICENSE ファイルが同梱されていない．"
    echo "Hugging Face のモデルカードのメタデータで apache-2.0 と宣言されている．"
    echo "ライセンス全文は本ファイル中の azooKey_dictionary_storage の項を参照すること"
    echo "（同一の Apache License 2.0）．"
    echo
    echo "Developed by: Keita Miwa"
    echo
    echo
} >> "$OUT"

echo "collect-notices: wrote $OUT ($(wc -l < "$OUT") lines)" >&2
