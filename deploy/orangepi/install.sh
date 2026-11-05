#!/bin/sh
# Orange Pi 6 Plus boot note.
# Use the official Debian or Ubuntu image for this board so both RTL8126
# ports come up. Do not install Raspberry Pi OS. Do not hardcode eth0.
# After the first boot, run `ip -4 addr` and label the jack that faces
# the existing router and the jack that faces the access point.
#
# On a Mac, cross-compile first:
#   GOOS=linux GOARCH=arm64 go build -o hbar ./cmd/hbar
# Copy hbar to the Orange Pi, then run this script there as root.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
if [ "$(uname -s)" != "Linux" ] || [ "$(uname -m)" != "aarch64" ]; then
  echo "Run this on the Orange Pi after copying the arm64 hbar binary." >&2
  exit 1
fi
if [ ! -x "$root/hbar" ]; then
  echo "Build first: GOOS=linux GOARCH=arm64 go build -o hbar ./cmd/hbar" >&2
  exit 1
fi
apt-get update
apt-get install -y nftables conntrack dnsmasq
install -d /opt/ano /var/lib/ano
install -m 755 "$root/hbar" /opt/ano/hbar
install -m 644 "$root/deploy/orangepi/ano.service" /etc/systemd/system/ano.service
systemctl daemon-reload
systemctl enable --now ano
echo "Ano is installed. Open the System page, then start the gateway."
