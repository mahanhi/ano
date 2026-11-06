#!/bin/sh
# Install Ano on the board with two Ethernet jacks.
# Use that board's official Debian or Ubuntu image so both ports come up.
# Do not install Raspberry Pi OS. Do not hardcode eth0.
# After the first boot, run `ip -4 addr` and label the jack that faces
# the existing router and the jack that faces the access point.
#
# On the build machine, cross-compile first:
#   GOOS=linux GOARCH=arm64 go build -o ano ./cmd/ano
# Copy this repository to the board, then run this script there as root.
#
# Ano, the screen, and the model are enabled for boot. Each one comes
# back if it stops. This script does not start the gateway.
# The screen listens on port 80. This Pi's /etc/hosts maps
# ano.home.security.ai to its address. Development machines are unchanged.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
if [ "$(uname -s)" != "Linux" ] || [ "$(uname -m)" != "aarch64" ]; then
  echo "Run this on Ano after copying the arm64 ano binary." >&2
  exit 1
fi
if [ ! -x "$root/ano" ]; then
  echo "Build first: GOOS=linux GOARCH=arm64 go build -o ano ./cmd/ano" >&2
  exit 1
fi
systemctl stop ano-web ano 2>/dev/null || true
apt-get update
apt-get install -y nftables conntrack dnsmasq curl ca-certificates
systemctl disable --now dnsmasq || true
if ! command -v ollama >/dev/null 2>&1; then
  curl -fsSL https://ollama.com/install.sh | sh
fi
install -d /etc/systemd/system/ollama.service.d
install -m 644 "$root/deploy/orangepi/ollama.conf" /etc/systemd/system/ollama.service.d/ano.conf
systemctl daemon-reload
systemctl enable --now ollama
i=0
while [ "$i" -lt 30 ]; do
  if curl -fsS http://127.0.0.1:11434/api/tags >/dev/null; then
    break
  fi
  i=$((i + 1))
  sleep 1
done
if ! curl -fsS http://127.0.0.1:11434/api/tags >/dev/null; then
  echo "The model did not start on 127.0.0.1:11434." >&2
  exit 1
fi
ollama pull llama3.2:1b
install -d /opt/ano /var/lib/ano
install -m 755 "$root/ano" /opt/ano/ano
sh "$root/scripts/install-envoy.sh" 1.40.2 /usr/local/bin/envoy
if ! command -v node >/dev/null 2>&1 || ! node -e 'process.exit(Number(process.versions.node.split(".")[0]) >= 20 ? 0 : 1)'; then
  curl -fsSL -o /tmp/node.tar.xz https://nodejs.org/dist/v20.19.5/node-v20.19.5-linux-arm64.tar.xz
  tar -xJf /tmp/node.tar.xz -C /usr/local --strip-components=1
  rm -f /tmp/node.tar.xz
fi
rm -rf /opt/ano/web
install -d /opt/ano/web
tar -C "$root/web" --exclude node_modules --exclude .next -cf - . | tar -C /opt/ano/web -xf -
cd /opt/ano/web
npm ci
npm run build
# next start uses the build. The TypeScript source does not stay on the Pi.
rm -rf src scripts
rm -f components.json eslint.config.mjs tsconfig.json next-env.d.ts README.md go.mod go.sum postcss.config.mjs
install -m 644 "$root/deploy/orangepi/ano.service" /etc/systemd/system/ano.service
install -m 644 "$root/deploy/orangepi/ano-web.service" /etc/systemd/system/ano-web.service
# Screen name for this Pi only. The address is whatever this board has now.
ui_name=ano.home.security.ai
ui_ip=$(ip -4 -o addr show scope global | awk '{print $4}' | cut -d/ -f1 | head -n 1)
if [ -n "$ui_ip" ]; then
  tmp=$(mktemp)
  if [ -f /etc/hosts ]; then
    grep -v -E "[[:space:]]${ui_name}([[:space:]]|\$)" /etc/hosts > "$tmp" || true
  fi
  printf '%s %s\n' "$ui_ip" "$ui_name" >> "$tmp"
  cat "$tmp" > /etc/hosts
  rm -f "$tmp"
  install -d /etc/dnsmasq.d
  cat > /etc/dnsmasq.d/ano-ui.conf << EOF
bind-interfaces
listen-address=${ui_ip}
except-interface=lo
no-dhcp-interface=*
no-resolv
server=1.1.1.1
address=/${ui_name}/${ui_ip}
EOF
  systemctl enable dnsmasq
  systemctl restart dnsmasq
fi
systemctl daemon-reload
systemctl enable --now ano ano-web ollama
echo "Ano is installed. The screen is http://ano.home.security.ai/ on this Pi, port 80. The model is llama3.2:1b. Ano, the screen, and the model start on boot and come back if they stop. The gateway stays off."
# The Pi keeps /opt/ano/ano and the built screen. The copied repository is removed.
case "$root" in
  /opt/ano|/opt/ano/*) ;;
  *)
    if [ -f "$root/go.mod" ] || [ -d "$root/web/src" ] || [ -d "$root/src" ]; then
      rm -rf "$root"
    fi
    ;;
esac
