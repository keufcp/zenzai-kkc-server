#!/bin/sh
# イメージから成果物を tar バンドルとして書き出す．コンテナを使えない環境向け．
#
# 使い方:
#   scripts/export-bundle.sh [イメージ] [出力先ディレクトリ]
#   既定は zenzai-kkc:latest と ./dist．

set -eu

IMAGE=${1:-zenzai-kkc:latest}
OUTDIR=${2:-dist}

if command -v podman >/dev/null 2>&1; then
    CE=podman
elif command -v docker >/dev/null 2>&1; then
    CE=docker
else
    echo "export-bundle: podman or docker is required" >&2
    exit 1
fi

mkdir -p "$OUTDIR"
TAR="$OUTDIR/zenzai-kkc-bundle.tar.gz"

echo "container engine: $CE" >&2
echo "image           : $IMAGE" >&2

# tar はコンテナ内で作り標準出力へ流す．ホストに展開すると実行ビットが失われる環境がある．
# 情報表示は stderr へ出す．stdout は tar 本体が流れるため．
# イメージには ENTRYPOINT があるので，シェルを動かすには上書きする．
$CE run --rm --entrypoint sh "$IMAGE" -c '
set -eu
S=/tmp/stage
rm -rf "$S"; mkdir -p "$S"
cp -a /opt/zenzai-kkc "$S/zenzai-kkc"

# 中身の一覧と sha256．配置先の物が何から作られたかを後から追えるようにする．
cd "$S/zenzai-kkc"
find . -type f ! -name MANIFEST -print0 | sort -z | xargs -0 sha256sum > MANIFEST

{
    echo
    echo "=== BUILD-INFO ==="
    cat "$S/zenzai-kkc/BUILD-INFO"
    echo
    echo "  extracted : $(du -sh "$S/zenzai-kkc" | cut -f1)"
} >&2

cd "$S"
tar czf - zenzai-kkc
' > "$TAR"

echo "  tar.gz    : $(du -h "$TAR" | cut -f1)" >&2
echo "  sha256    : $(sha256sum "$TAR" | cut -d" " -f1)" >&2
echo "  written to: $TAR" >&2
echo >&2
echo "to verify on the target host:" >&2
echo "  tar xzf zenzai-kkc-bundle.tar.gz && ./zenzai-kkc/verify-bundle.sh" >&2
