#!/usr/bin/env sh
# Install the latest argus release binary for this OS/arch into ./bin (or $BINDIR).
set -eu

REPO="gnanam1990/argus"
BINDIR="${BINDIR:-./bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64 | amd64) arch="amd64" ;;
  aarch64 | arm64) arch="arm64" ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

ext="tar.gz"
[ "$os" = "windows" ] && ext="zip"

tag="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep -m1 '"tag_name"' | cut -d'"' -f4)"
[ -n "$tag" ] || { echo "could not resolve latest release" >&2; exit 1; }

ver="${tag#v}"
asset="argus_${ver}_${os}_${arch}.${ext}"
url="https://github.com/${REPO}/releases/download/${tag}/${asset}"

echo "downloading ${url}"
tmp="$(mktemp -d)"
curl -fsSL "$url" -o "${tmp}/${asset}"

mkdir -p "$BINDIR"
if [ "$ext" = "zip" ]; then
  unzip -o "${tmp}/${asset}" -d "$BINDIR" >/dev/null
else
  tar -xzf "${tmp}/${asset}" -C "$BINDIR"
fi
rm -rf "$tmp"

echo "installed argus ${ver} to ${BINDIR}"
