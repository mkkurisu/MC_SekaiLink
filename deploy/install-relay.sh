#!/usr/bin/env bash
set -euo pipefail

NODE_ID="${SEKAILINK_NODE_ID:-SV-Oracle}"
PUBLIC_HOST="${SEKAILINK_PUBLIC_HOST:-}"
SECRET="${SEKAILINK_SECRET:-}"
PORT="${SEKAILINK_PORT:-25590}"
PORT_MIN="${SEKAILINK_PORT_MIN:-25600}"
PORT_MAX="${SEKAILINK_PORT_MAX:-25700}"
BINARY_URL="${SEKAILINK_BINARY_URL:-}"
NO_TLS="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --node-id) NODE_ID="$2"; shift 2 ;;
    --public-host) PUBLIC_HOST="$2"; shift 2 ;;
    --secret) SECRET="$2"; shift 2 ;;
    --port) PORT="$2"; shift 2 ;;
    --port-min) PORT_MIN="$2"; shift 2 ;;
    --port-max) PORT_MAX="$2"; shift 2 ;;
    --binary-url) BINARY_URL="$2"; shift 2 ;;
    --no-tls) NO_TLS="true"; shift 1 ;;
    *)
      echo "Unknown option: $1"
      exit 1
      ;;
  esac
done

if [[ $EUID -ne 0 ]]; then
  echo "Please run as root (sudo bash install-relay.sh ...)"
  exit 1
fi

ARCH_RAW="$(uname -m)"
case "$ARCH_RAW" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported CPU architecture: $ARCH_RAW"
    exit 1
    ;;
esac

if [[ -z "$PUBLIC_HOST" ]]; then
  PUBLIC_HOST="$(curl -4 -fsSL --max-time 5 https://api.ipify.org 2>/dev/null || curl -4 -fsSL --max-time 5 https://ifconfig.me 2>/dev/null || true)"
fi

if [[ -z "$SECRET" ]]; then
  if command -v openssl >/dev/null 2>&1; then
    SECRET="$(openssl rand -hex 12)"
  else
    SECRET="$(od -An -N12 -tx1 /dev/urandom | tr -d ' \n')"
  fi
fi

BIN_PATH="/usr/local/bin/sekailink-relay"
CONF_DIR="/etc/sekailink-relay"
mkdir -p "$CONF_DIR"
chmod 700 "$CONF_DIR"

LOCAL_CANDIDATE="./sekailink-relay-linux-${ARCH}"
if [[ -f "$LOCAL_CANDIDATE" ]]; then
  echo "Installing local binary $LOCAL_CANDIDATE -> $BIN_PATH"
  install -m 0755 "$LOCAL_CANDIDATE" "$BIN_PATH"
elif [[ -n "$BINARY_URL" ]]; then
  echo "Downloading relay binary from $BINARY_URL ..."
  curl -fSL -o "${BIN_PATH}.tmp" "$BINARY_URL"
  install -m 0755 "${BIN_PATH}.tmp" "$BIN_PATH"
  rm -f "${BIN_PATH}.tmp"
else
  DEFAULT_URL="https://github.com/mkkurisu/MC_SekaiLink/releases/latest/download/sekailink-relay-linux-${ARCH}"
  echo "Downloading $DEFAULT_URL ..."
  if curl -fSL -o "${BIN_PATH}.tmp" "$DEFAULT_URL"; then
    install -m 0755 "${BIN_PATH}.tmp" "$BIN_PATH"
    rm -f "${BIN_PATH}.tmp"
  elif command -v go >/dev/null 2>&1; then
    echo "Release asset not found; building from source with Go..."
    TMP_SRC="$(mktemp -d)"
    git clone --depth 1 https://github.com/mkkurisu/MC_SekaiLink.git "$TMP_SRC"
    (cd "$TMP_SRC" && CGO_ENABLED=0 go build -ldflags="-s -w" -o "$BIN_PATH" ./cmd/sekailink-relay)
    rm -rf "$TMP_SRC"
    chmod 0755 "$BIN_PATH"
  else
    echo "Failed to download release binary and Go is not installed. Pass --binary-url or install golang."
    exit 1
  fi
fi

TLS_FLAGS="--cert-dir ${CONF_DIR}"
if [[ "$NO_TLS" == "true" ]]; then
  TLS_FLAGS="--no-tls"
fi

cat > /etc/systemd/system/sekailink-relay.service <<EOF
[Unit]
Description=MC SekaiLink VDS Bridge (${NODE_ID})
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${BIN_PATH} --node-id "${NODE_ID}" --listen ":${PORT}" --public-host "${PUBLIC_HOST}" --secret "${SECRET}" --port-min ${PORT_MIN} --port-max ${PORT_MAX} ${TLS_FLAGS}
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

# Open firewall ports on ufw / firewalld / iptables when present
if command -v ufw >/dev/null 2>&1 && ufw status | grep -q "Status: active"; then
  ufw allow "${PORT}/tcp" || true
  ufw allow "${PORT_MIN}:${PORT_MAX}/tcp" || true
  ufw allow "${PORT_MIN}:${PORT_MAX}/udp" || true
elif command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld; then
  firewall-cmd --permanent --add-port="${PORT}/tcp" || true
  firewall-cmd --permanent --add-port="${PORT_MIN}-${PORT_MAX}/tcp" || true
  firewall-cmd --permanent --add-port="${PORT_MIN}-${PORT_MAX}/udp" || true
  firewall-cmd --reload || true
elif command -v iptables >/dev/null 2>&1; then
  iptables -C INPUT -p tcp --dport "${PORT}" -j ACCEPT 2>/dev/null || iptables -I INPUT 1 -p tcp --dport "${PORT}" -j ACCEPT || true
  iptables -C INPUT -p tcp --dport "${PORT_MIN}:${PORT_MAX}" -j ACCEPT 2>/dev/null || iptables -I INPUT 1 -p tcp --dport "${PORT_MIN}:${PORT_MAX}" -j ACCEPT || true
  iptables -C INPUT -p udp --dport "${PORT_MIN}:${PORT_MAX}" -j ACCEPT 2>/dev/null || iptables -I INPUT 1 -p udp --dport "${PORT_MIN}:${PORT_MAX}" -j ACCEPT || true
fi

systemctl daemon-reload
systemctl enable sekailink-relay
systemctl restart sekailink-relay
sleep 1

CERT_PIN=""
if [[ -f "${CONF_DIR}/relay.pin" ]]; then
  CERT_PIN="$(tr -d '[:space:]' < "${CONF_DIR}/relay.pin")"
fi

echo ""
echo "=========================================================="
echo "  MC SekaiLink VDS Bridge Ready! (${NODE_ID})"
echo "=========================================================="
echo "  Node ID   : ${NODE_ID}"
echo "  Address   : ${PUBLIC_HOST:-YOUR_VDS_IP}:${PORT}"
echo "  Secret    : ${SECRET}"
echo "  PortPool  : ${PORT_MIN}-${PORT_MAX} (TCP + UDP)"
if [[ "$NO_TLS" == "true" ]]; then
  echo "  TLS 1.3   : Disabled"
else
  echo "  TLS 1.3   : Enabled"
  echo "  TLS Pin   : ${CERT_PIN:-pending (check: cat ${CONF_DIR}/relay.pin)}"
fi
echo ""
echo "  Remember: If using Oracle Cloud / AWS / Azure, open"
echo "  Ingress rules for TCP ${PORT} & TCP/UDP ${PORT_MIN}-${PORT_MAX}."
echo "=========================================================="
