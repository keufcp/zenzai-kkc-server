# zenzai-kkc-server
#
# ビルドステージで AzooKeyKanaKanjiConverter の CLI (anco) と HTTP サーバを構築し，
# ランタイムステージには成果物だけを置く．
#
# 第三者コードはすべてコミットハッシュで固定して取得する．タグやブランチは動きうるため使わない．
# モデルは Hugging Face のリビジョンで固定し，sha256 で検証する．

ARG SWIFT_IMAGE=docker.io/library/swift@sha256:085d84812050452237ff2a04470fb278400b0e799a7b969c5c6063eab6ea38e0
ARG GO_IMAGE=docker.io/library/golang@sha256:1a6d4452c65dea36aac2e2d606b01b4a029ec90cc1ae53890540ce6173ea77ac
ARG RUNTIME_IMAGE=docker.io/library/debian@sha256:63a496b5d3b99214b39f5ed70eb71a61e590a77979c79cbee4faf991f8c0783e

# =============================================================================
FROM ${GO_IMAGE} AS goserver

WORKDIR /src
COPY server/ ./

# 静的リンクにしてランタイムイメージへ依存を持ち込まない．
# 整形・検査・テストを通らなければイメージを作らせない．
RUN test -z "$(gofmt -l .)" \
    && go vet ./... \
    && go test ./... \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/zenzai-kkc-server .

# =============================================================================
FROM ${SWIFT_IMAGE} AS build

ARG LLAMA_CPP_REPO=https://github.com/azooKey/llama.cpp.git
ARG LLAMA_CPP_COMMIT=10131b23ee0dbd9d03dfc618cec9fe3402919277

ARG AKKC_REPO=https://github.com/azooKey/AzooKeyKanaKanjiConverter.git
ARG AKKC_COMMIT=bbef9d2d99a2e9e69ac3f7e2e07b08474de59a81

ARG ZENZ_XSMALL_REPO=Miwa-Keita/zenz-v3.2-xsmall-gguf
ARG ZENZ_XSMALL_REV=4f5423f0fad41a73b1242eb96fe5c12ae4fdca83
ARG ZENZ_XSMALL_SHA256=00c64b3d318045a708d0cad5434faccab10f5481a49e6362864551fd0995fa58

ARG ZENZ_SMALL_REPO=Miwa-Keita/zenz-v3.2-small-gguf
ARG ZENZ_SMALL_REV=c67e03e07d215c869f591b274c1631170d3e11fe
ARG ZENZ_SMALL_SHA256=29c223d4c23327b80fd13ebb5ab2555057a46317997d5da391584ffbef0db673

# CPU 命令セット．既定は AVX2 まで（AVX-512 を持たない低消費電力 CPU でも動くように）．
# 別のベースラインで作りたい場合はここを上書きする．
ARG GGML_ISA_FLAGS="-DGGML_AVX=ON -DGGML_AVX2=ON -DGGML_FMA=ON -DGGML_F16C=ON -DGGML_AVX512=OFF"

RUN apt-get update && apt-get install -y --no-install-recommends \
        cmake ninja-build git ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# --- llama.cpp (azooKey fork) ------------------------------------------------
# 独自 pre-tokenizer 'gpt2-small-japanese-char' が登録されたフォーク．
# 本家では zenz の GGUF を読めない．
RUN git init -q llama.cpp \
    && git -C llama.cpp remote add origin "${LLAMA_CPP_REPO}" \
    && git -C llama.cpp fetch -q --depth 1 origin "${LLAMA_CPP_COMMIT}" \
    && git -C llama.cpp checkout -q FETCH_HEAD

# swift イメージは cc=GCC / c++=Clang の混在．ggml の CMake は CXX(Clang) を見て
# Clang 専用の警告フラグを C 側にも渡すため，GCC だとビルドが落ちる．両方 clang に揃える．
RUN cmake -S llama.cpp -B llama-build -G Ninja \
        -DCMAKE_C_COMPILER=/usr/bin/clang \
        -DCMAKE_CXX_COMPILER=/usr/bin/clang++ \
        -DCMAKE_BUILD_TYPE=Release \
        -DBUILD_SHARED_LIBS=ON \
        -DGGML_NATIVE=OFF \
        ${GGML_ISA_FLAGS} \
        -DLLAMA_CURL=OFF \
        -DLLAMA_BUILD_TESTS=OFF -DLLAMA_BUILD_EXAMPLES=OFF -DLLAMA_BUILD_SERVER=OFF \
    && cmake --build llama-build -j "$(nproc)"

# --- AzooKeyKanaKanjiConverter ----------------------------------------------
RUN git init -q akkc \
    && git -C akkc remote add origin "${AKKC_REPO}" \
    && git -C akkc fetch -q --depth 1 origin "${AKKC_COMMIT}" \
    && git -C akkc checkout -q FETCH_HEAD \
    && git -C akkc submodule update -q --init --depth 1

# git apply なので，対象が変わっていれば黙って通らずビルドが失敗する．
# 各パッチの内容と理由は patches/ 内のヘッダを参照．
COPY patches/ /patches/
RUN for p in /patches/*.patch; do echo "--- $p"; git -C akkc apply --verbose "$p"; done

WORKDIR /src/akkc

# Sources/llama.cpp は systemLibrary（ヘッダは vendoring 済み）．共有ライブラリは
# リンカに見つけさせる必要があるのでリポジトリ直下に置く．
RUN cp /src/llama-build/bin/lib*.so ./

RUN swift build -c release --traits ZenzaiCPU \
        -Xswiftc -strict-concurrency=complete \
        -Xlinker -L/src/akkc \
        -j "$(nproc)"

# --- モデル ------------------------------------------------------------------
RUN mkdir -p /models \
    && curl -fsSL -o /models/zenz-v3.2-xsmall.gguf \
        "https://huggingface.co/${ZENZ_XSMALL_REPO}/resolve/${ZENZ_XSMALL_REV}/ggml-model-Q5_K_M.gguf" \
    && curl -fsSL -o /models/zenz-v3.2-small.gguf \
        "https://huggingface.co/${ZENZ_SMALL_REPO}/resolve/${ZENZ_SMALL_REV}/ggml-model-Q5_K_M.gguf" \
    && echo "${ZENZ_XSMALL_SHA256}  /models/zenz-v3.2-xsmall.gguf" | sha256sum -c - \
    && echo "${ZENZ_SMALL_SHA256}  /models/zenz-v3.2-small.gguf"   | sha256sum -c -

# --- バンドルの組み立て -------------------------------------------------------
# CliTool は SwiftPM のリソースバンドルを実行ファイルと同じ階層から探すため，隣に置く．
#
# 絵文字辞書 (*.resources/EmojiDictionary) は azooKey_emoji_dictionary_storage 由来だが，
# 当該リポジトリに LICENSE が無く再配布の許諾が無い．かな漢字変換には不要なので除外する．
RUN set -eu; \
    B=/out/zenzai-kkc; \
    mkdir -p "$B/lib" "$B/models"; \
    R=/src/akkc/.build/release; \
    cp "$R/CliTool" "$B/"; \
    cp -r "$R"/*.resources "$B/"; \
    cp /src/akkc/lib*.so "$B/lib/"; \
    ldd "$R/CliTool" | awk '/\/usr\/lib\/swift\/linux\//{print $3}' | xargs -r -I{} cp {} "$B/lib/"; \
    cp /models/*.gguf "$B/models/"; \
    rm -rf "$B"/*.resources/EmojiDictionary; \
    printf '%s\n' \
        '#!/bin/sh' \
        'd=$(cd "$(dirname "$0")" && pwd)' \
        'exec env LD_LIBRARY_PATH="$d/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}" "$d/CliTool" "$@"' \
        > "$B/anco"; \
    chmod +x "$B/anco"

COPY --from=goserver /out/zenzai-kkc-server /out/zenzai-kkc/zenzai-kkc-server

# 検証スクリプトを成果物に含める．イメージ単体でも，書き出した tar バンドルに対しても
# 同じものが使えるようにするため．
COPY scripts/verify-bundle.sh /out/zenzai-kkc/verify-bundle.sh
RUN chmod +x /out/zenzai-kkc/verify-bundle.sh

# --- 第三者ライセンス表示 ------------------------------------------------------
# ライセンス本文は clone したリポジトリと SwiftPM の checkouts にしかなく，
# ランタイムイメージには残らない．ここで収集して成果物に同梱する．
COPY scripts/collect-notices.sh /usr/local/bin/collect-notices.sh
RUN sh /usr/local/bin/collect-notices.sh /out/zenzai-kkc/THIRD-PARTY-NOTICES

# ビルドに使った実際のコミットを記録する．成果物の出所を後から追えるようにするため．
RUN set -eu; \
    { echo "akkc=$(git -C /src/akkc rev-parse HEAD)"; \
      echo "llama.cpp=$(git -C /src/llama.cpp rev-parse HEAD)"; \
      echo "zenz-v3.2-xsmall=${ZENZ_XSMALL_REPO}@${ZENZ_XSMALL_REV}"; \
      echo "zenz-v3.2-small=${ZENZ_SMALL_REPO}@${ZENZ_SMALL_REV}"; \
      echo "ggml_isa_flags=${GGML_ISA_FLAGS}"; \
      echo "swift=$(swift --version 2>&1 | head -1)"; \
    } > /out/zenzai-kkc/BUILD-INFO

# =============================================================================
FROM ${RUNTIME_IMAGE} AS runtime

LABEL org.opencontainers.image.title="zenzai-kkc-server" \
      org.opencontainers.image.description="Japanese kana-kanji conversion HTTP API backed by AzooKeyKanaKanjiConverter and Zenzai" \
      org.opencontainers.image.licenses="MIT"

COPY --from=build /out/zenzai-kkc /opt/zenzai-kkc

ENV ZKKC_ANCO=/opt/zenzai-kkc/anco \
    ZKKC_MODEL=/opt/zenzai-kkc/models/zenz-v3.2-xsmall.gguf \
    ZKKC_INFERENCE_LIMIT=1 \
    ZKKC_BIND=0.0.0.0

# ZKKC_PORT に既定値は置かない．理由は README を参照．

ENV PATH=/opt/zenzai-kkc:${PATH}

ENTRYPOINT ["/opt/zenzai-kkc/zenzai-kkc-server"]
