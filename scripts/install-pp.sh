#!/usr/bin/env sh
# Printing Press 2.0 zero-Go installer for `pp`.
# Downloads a prebuilt, checksum-verified `pp` binary for macOS or Linux.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/mvanhorn/cli-printing-press/main/scripts/install-pp.sh | sh
set -eu

REPO="${PP_REPO:-mvanhorn/cli-printing-press}"
INSTALL_DIR="${PP_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${PP_VERSION:-latest}"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
  darwin|linux) ;;
  *)
    echo "Unsupported OS: $OS (build from source with Go or download a Windows zip from GitHub Releases)" >&2
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

ASSET="pp_${OS}_${ARCH}.tar.gz"
if [ "$VERSION" = "latest" ]; then
  BASE_URL="https://github.com/${REPO}/releases/latest/download"
else
  BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

echo "Downloading ${ASSET} from ${REPO} (${VERSION})..."
curl -fsSL "${BASE_URL}/${ASSET}" -o "${TMP_DIR}/${ASSET}"
curl -fsSL "${BASE_URL}/checksums.txt" -o "${TMP_DIR}/checksums.txt"

EXPECTED="$(awk -v target="$ASSET" '$2 == target || $2 == "*" target { print $1; exit }' "${TMP_DIR}/checksums.txt")"
if [ -z "$EXPECTED" ]; then
  echo "ERROR: ${ASSET} not found in checksums.txt" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "${TMP_DIR}/${ASSET}" | awk '{ print $1 }')"
else
  ACTUAL="$(shasum -a 256 "${TMP_DIR}/${ASSET}" | awk '{ print $1 }')"
fi

if [ "$EXPECTED" != "$ACTUAL" ]; then
  echo "ERROR: checksum mismatch for ${ASSET} (expected ${EXPECTED}, got ${ACTUAL})" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
tar -xzf "${TMP_DIR}/${ASSET}" -C "$TMP_DIR"
install -m 0755 "${TMP_DIR}/pp" "${INSTALL_DIR}/pp"

echo "Installed pp to ${INSTALL_DIR}/pp (SHA-256 verified, zero Go required)."
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo "Add ${INSTALL_DIR} to your PATH:"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    ;;
esac
echo "Next: pp route https://www.espn.com/nba/scoreboard && pp run espn scoreboard --compact"
