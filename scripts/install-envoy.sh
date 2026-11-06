#!/bin/sh
# Install the Envoy binary Ano starts. Envoy is not its own service.
set -eu
version="${1:-1.40.2}"
dest="${2:-./bin/envoy}"
machine=$(uname -m)
case "$machine" in
  x86_64) arch=x86_64 ;;
  aarch64 | arm64) arch=aarch_64 ;;
  *)
    echo "Envoy has no build for $machine" >&2
    exit 1
    ;;
esac
url="https://github.com/envoyproxy/envoy/releases/download/v${version}/envoy-${version}-linux-${arch}"
tmp="$(mktemp)"
mkdir -p "$(dirname "$dest")"
curl -fsSL -o "$tmp" "$url"
chmod +x "$tmp"
install -m 755 "$tmp" "$dest"
rm -f "$tmp"
"$dest" --version
