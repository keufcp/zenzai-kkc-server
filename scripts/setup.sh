#!/bin/sh

set -eu

REPO=${ZKKC_REPO:-keufcp/zenzai-kkc-server}
PREFIX=${ZKKC_PREFIX:-/opt/zenzai-kkc}
SERVICE=zenzai-kkc
UNIT=/etc/systemd/system/$SERVICE.service
ENVFILE=/etc/default/$SERVICE
USER_NAME=zenzai-kkc
BUNDLE_NAME=zenzai-kkc-bundle.tar.gz
VERSION=${1:-}
HOST=${ZKKC_BIND:-127.0.0.1}

die() {
    echo "setup: $*" >&2
    exit 1
}

[ "$(id -u)" -eq 0 ] || die "run as root"
[ "$(uname -s)" = Linux ] || die "Linux is required"
[ "$(uname -m)" = x86_64 ] || die "x86-64 is required (got $(uname -m))"
grep -q '\bavx2\b' /proc/cpuinfo || die "the CPU must support AVX2"
command -v systemctl >/dev/null 2>&1 || die "systemd is required"

[ -n "${ZKKC_PORT:-}" ] || die "ZKKC_PORT is required and has no default
  choose a port outside the ephemeral range, e.g. ZKKC_PORT=61234"

set --
command -v curl >/dev/null 2>&1 || set -- "$@" curl
command -v stdbuf >/dev/null 2>&1 || set -- "$@" coreutils

if [ "$#" -gt 0 ]; then
    echo "installing: $*"
    apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq ca-certificates "$@"
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

if [ -n "${ZKKC_BUNDLE:-}" ]; then
    [ -f "$ZKKC_BUNDLE" ] || die "no such file: $ZKKC_BUNDLE"
    echo "using local bundle: $ZKKC_BUNDLE"
    cp "$ZKKC_BUNDLE" "$work/$BUNDLE_NAME"
else
    if [ -z "$VERSION" ]; then
        base="https://github.com/$REPO/releases/latest/download"
    else
        base="https://github.com/$REPO/releases/download/$VERSION"
    fi

    echo "downloading: $base/$BUNDLE_NAME"
    curl -fsSL -o "$work/$BUNDLE_NAME" "$base/$BUNDLE_NAME" \
        || die "failed to download the bundle"
    curl -fsSL -o "$work/$BUNDLE_NAME.sha256" "$base/$BUNDLE_NAME.sha256" \
        || die "failed to download the checksum"

    ( cd "$work" && sha256sum -c "$BUNDLE_NAME.sha256" >/dev/null ) \
        || die "checksum mismatch"
    echo "checksum verified"
fi

tar xzf "$work/$BUNDLE_NAME" -C "$work"
[ -x "$work/zenzai-kkc/zenzai-kkc-server" ] || die "the bundle does not contain the server"

id -u "$USER_NAME" >/dev/null 2>&1 \
    || useradd --system --no-create-home --shell /usr/sbin/nologin "$USER_NAME"

systemctl stop "$SERVICE" 2>/dev/null || true

rm -rf "$PREFIX"
mkdir -p "$(dirname "$PREFIX")"
mv "$work/zenzai-kkc" "$PREFIX"
chown -R root:root "$PREFIX"

cat > "$ENVFILE" <<EOF
ZKKC_BIND=$HOST
ZKKC_PORT=$ZKKC_PORT
ZKKC_ANCO=$PREFIX/anco
ZKKC_MODEL=${ZKKC_MODEL:-$PREFIX/models/zenz-v3.2-xsmall.gguf}
ZKKC_INFERENCE_LIMIT=${ZKKC_INFERENCE_LIMIT:-1}
EOF

cat > "$UNIT" <<EOF
[Unit]
Description=Japanese kana-kanji conversion HTTP API
After=network-online.target
Wants=network-online.target

[Service]
Type=exec
User=$USER_NAME
Group=$USER_NAME
EnvironmentFile=$ENVFILE
ExecStart=$PREFIX/zenzai-kkc-server
Restart=on-failure
RestartSec=5
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ProtectKernelTunables=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
RestrictNamespaces=yes
LockPersonality=yes

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now "$SERVICE" >/dev/null

if [ "$HOST" = 0.0.0.0 ] || [ "$HOST" = :: ]; then
    HOST=127.0.0.1
fi

ready=
i=0
while [ "$i" -lt 120 ]; do
    if ! systemctl is-active --quiet "$SERVICE"; then
        break
    fi
    if curl -fsS -o /dev/null "http://$HOST:$ZKKC_PORT/v1/health"; then
        ready=1
        break
    fi
    i=$((i + 1))
    sleep 1
done

if [ -z "$ready" ]; then
    echo "setup: the service did not become ready" >&2
    journalctl -u "$SERVICE" -n 20 --no-pager >&2
    exit 1
fi

echo
echo "installed: $PREFIX"
echo "service  : $SERVICE (systemctl status $SERVICE)"
echo "config   : $ENVFILE"
echo "listening: $HOST:$ZKKC_PORT"
