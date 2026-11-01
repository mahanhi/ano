#!/bin/sh
set -eu
version="${1:-1.40.0}"
dest="${2:-./bin/envoy}"
url="https://github.com/envoyproxy/envoy/releases/download/v${version}/envoy-${version}-linux-x86_64"
tmp="$(mktemp)"
mkdir -p "$(dirname "$dest")"
curl -fsSL -o "$tmp" "$url"
chmod +x "$tmp"
install -m 755 "$tmp" "$dest"
rm -f "$tmp"
"$dest" --version
