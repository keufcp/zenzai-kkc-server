#!/bin/sh
# ビルドしたバンドルが壊れていないことを確認する．
#
# 引数にバンドルのディレクトリを取る．省略時は自動で探す．
# コンテナ内でも，展開した tar バンドルに対しても同じように動く．

set -eu

if [ $# -ge 1 ]; then
    B=$1
else
    # バンドルに同梱された場合はルート直下に，リポジトリでは scripts/ の下に置かれる．
    d=$(cd "$(dirname "$0")" && pwd)
    if [ -x "$d/anco" ]; then
        B=$d
    elif [ -x "$d/../anco" ]; then
        B=$(cd "$d/.." && pwd)
    else
        B=/opt/zenzai-kkc
    fi
fi

if [ ! -x "$B/anco" ]; then
    echo "verify-bundle: bundle not found ($B)" >&2
    echo "  pass the bundle directory as an argument" >&2
    exit 1
fi

MODEL=${ZKKC_MODEL:-$B/models/zenz-v3.2-xsmall.gguf}
fail=0

# anco は出力に ANSI の装飾を付ける．装飾を落として先頭行だけを取る．
esc=$(printf '\033')

check() {
    if [ "$2" = "$3" ]; then
        printf '  ok   %s\n' "$1"
    else
        printf '  FAIL %s\n         want: %s\n         got:  %s\n' "$1" "$3" "$2"
        fail=1
    fi
}

echo "bundle: $B"
echo "model : $MODEL"
echo

# --- 1. conversion without Zenzai -----------------------------------------------------
out=$("$B/anco" run "わたしのともだちはりょうりがじょうずです" --only_whole_conversion -n 1 \
        2>/dev/null | sed "s/${esc}\[[0-9;]*m//g" | head -1)
check "conversion without Zenzai" "$out" "私の友達は料理が上手です"

# --- 2. conversion with Zenzai -----------------------------------------------------
err=$(mktemp)
out=$("$B/anco" run "わたしのともだちはりょうりがじょうずです" --only_whole_conversion -n 1 \
        --zenz "$MODEL" --zenz_v3 --config_zenzai_inference_limit 1 \
        2>"$err" | sed "s/${esc}\[[0-9;]*m//g" | head -1)
check "conversion with Zenzai" "$out" "私の友達は料理が上手です"

# --- 3. モデルが実際にロードされていること -------------------------------------
# ZenzaiCPU のパッチが当たっていないと，ここで main_gpu のエラーが出たまま
# 変換自体は継続してしまう．終了コードでは検出できないので stderr を見る．
if grep -q "invalid value for main_gpu" "$err"; then
    printf '  FAIL model load\n         stderr contains a main_gpu error; patches/ has not been applied\n'
    fail=1
else
    printf '  ok   model load\n'
fi
rm -f "$err"

# --- 4. 長文が途中で切れないこと ----------------------------------------------
# 自由生成ではなく制約付きデコードなので出力長はドラフト候補が決める．
# 出力が極端に短ければ変換経路が壊れている．
long="じんこうちのうはきんねんめざましいはってんをとげており、さまざまなぶんやでかつようされています。とくにしぜんげんごしょりのぎじゅつは、にんげんのことばをりかいし、しぜんなぶんしょうをせいせいすることがかのうになりました。"
out=$("$B/anco" run "$long" --only_whole_conversion -n 1 \
        --zenz "$MODEL" --zenz_v3 --config_zenzai_inference_limit 1 \
        2>/dev/null | sed "s/${esc}\[[0-9;]*m//g" | head -1)
# ロケールが UTF-8 でないと wc -m はバイト数を返す．文字数で数えるため明示する．
n=$(printf '%s' "$out" | LC_ALL=C.UTF-8 wc -m)
if [ "$n" -ge 60 ]; then
    printf '  ok   long input (%s runes)\n' "$n"
else
    printf '  FAIL long input\n         output is too short (%s runes): %s\n' "$n" "$out"
    fail=1
fi

# --- 5. 絵文字辞書が含まれていないこと -----------------------------------------
if ls "$B"/*.resources/EmojiDictionary >/dev/null 2>&1; then
    printf '  FAIL emoji dictionary\n         the bundle contains content that may not be redistributed\n'
    fail=1
else
    printf '  ok   emoji dictionary is excluded\n'
fi

# --- 6. ライセンス表示が同梱されていること -------------------------------------
if [ -s "$B/THIRD-PARTY-NOTICES" ]; then
    printf '  ok   THIRD-PARTY-NOTICES is present (%s lines)\n' "$(wc -l < "$B/THIRD-PARTY-NOTICES")"
else
    printf '  FAIL THIRD-PARTY-NOTICES is missing or empty\n'
    fail=1
fi

echo
if [ "$fail" -eq 0 ]; then
    echo "all checks passed"
else
    echo "some checks failed"
fi
exit "$fail"
