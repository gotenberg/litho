#!/bin/sh
# Installs the litho CLI from a GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/gotenberg/litho/main/install.sh | sh
#
# Environment:
#   LITHO_VERSION      release tag to install, for example v1.2.3 (default: latest)
#   LITHO_INSTALL_DIR  directory for the binary (default: /usr/local/bin)
#
# Supports Linux on amd64, arm64, 386, armv7, ppc64le, riscv64, s390x and
# loong64, and macOS and FreeBSD on amd64 and arm64. Windows users download the zip from the releases
# page.

set -eu

REPO="gotenberg/litho"
RELEASES="https://github.com/${REPO}/releases"
INSTALL_DIR="${LITHO_INSTALL_DIR:-/usr/local/bin}"

fail() {
  echo "litho install: $*" >&2
  exit 1
}

need() {
  command -v "$1" > /dev/null 2>&1 || fail "$1 is required"
}

need curl
need tar
need uname

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  FreeBSD) os=freebsd ;;
  *) fail "unsupported OS $(uname -s); download a binary from ${RELEASES}" ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  i386 | i686) arch=386 ;;
  armv7l | armv8l) arch=armv7 ;;
  ppc64le) arch=ppc64le ;;
  riscv64) arch=riscv64 ;;
  s390x) arch=s390x ;;
  loongarch64) arch=loong64 ;;
  *) fail "unsupported architecture $(uname -m); download a binary from ${RELEASES}" ;;
esac

if command -v sha256sum > /dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum > /dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
elif command -v sha256 > /dev/null 2>&1; then
  sha256() { command sha256 -q "$1"; }
else
  fail "sha256sum, shasum or sha256 is required"
fi

# The latest-release page redirects to the tag, which avoids the rate-limited
# GitHub API.
tag="${LITHO_VERSION:-}"
if [ -z "$tag" ]; then
  tag="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "${RELEASES}/latest")" \
    || fail "no published release found at ${RELEASES}"
  tag="${tag##*/}"
  [ -n "$tag" ] && [ "$tag" != "latest" ] || fail "no published release found"
fi

archive="litho_${tag#v}_${os}_${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading litho ${tag} for ${os}/${arch}"
curl -fsSL -o "${tmp}/${archive}" "${RELEASES}/download/${tag}/${archive}" \
  || fail "cannot download ${archive} from release ${tag}"
curl -fsSL -o "${tmp}/checksums.txt" "${RELEASES}/download/${tag}/checksums.txt" \
  || fail "cannot download checksums.txt from release ${tag}"

want="$(awk -v f="$archive" '$2 == f { print $1 }' "${tmp}/checksums.txt")"
[ -n "$want" ] || fail "${archive} is missing from checksums.txt"
[ "$(sha256 "${tmp}/${archive}")" = "$want" ] || fail "checksum mismatch for ${archive}"

tar -xzf "${tmp}/${archive}" -C "$tmp" litho

if [ -w "$INSTALL_DIR" ] || { [ ! -e "$INSTALL_DIR" ] && mkdir -p "$INSTALL_DIR" 2> /dev/null; }; then
  sudo=""
else
  need sudo
  sudo="sudo"
  echo "${INSTALL_DIR} is not writable, using sudo"
fi
$sudo mkdir -p "$INSTALL_DIR"
$sudo install -m 0755 "${tmp}/litho" "${INSTALL_DIR}/litho"

echo "Installed $("${INSTALL_DIR}/litho" --version) to ${INSTALL_DIR}/litho"
