#!/usr/bin/env bash

set -euo pipefail

NAME="subcommit"
REPO="reemus-dev/subcommit"
DOWNLOAD_BASE_URL="https://github.com/$REPO/releases/latest/download"
INSTALL_DIR="$HOME/.local/bin"
CUSTOM_INSTALL_DIR=false

usage() {
  cat <<EOF
Install the latest $NAME release.

Usage: install.sh [-d <directory>]

  -d <directory>  Install in an existing directory instead of \$HOME/.local/bin
  -h              Show this help
EOF
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

while getopts ":d:h" option; do
  case "$option" in
    d)
      INSTALL_DIR="$OPTARG"
      CUSTOM_INSTALL_DIR=true
      ;;
    h)
      usage
      exit 0
      ;;
    :)
      fail "option -$OPTARG requires an argument"
      ;;
    \?)
      fail "unknown option: -$OPTARG"
      ;;
  esac
done

[ "$OPTIND" -gt "$#" ] || fail "unexpected argument: ${!OPTIND}"
if [ "$CUSTOM_INSTALL_DIR" = false ]; then
  mkdir -p "$INSTALL_DIR"
fi
[ -d "$INSTALL_DIR" ] || fail "installation directory does not exist: $INSTALL_DIR"
[ -w "$INSTALL_DIR" ] || fail "installation directory is not writable: $INSTALL_DIR"

for command in curl tar install; do
  command -v "$command" >/dev/null 2>&1 || fail "required command not found: $command"
done

case "$(uname -s)" in
  Darwin | Linux) os="$(uname -s)" ;;
  *) fail "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch="x86_64" ;;
  arm64 | aarch64) arch="arm64" ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/subcommit.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

printf 'Downloading checksums\n'
curl -fL --proto '=https' --tlsv1.2 -sS \
  "$DOWNLOAD_BASE_URL/checksums.txt" \
  -o "$tmp_dir/checksums.txt"

archive="$(awk -v suffix="_${os}_${arch}.tar.gz" \
  'substr($2, length($2) - length(suffix) + 1) == suffix { print $2 }' \
  "$tmp_dir/checksums.txt")"
[ -n "$archive" ] || fail "no release archive found for ${os}/${arch}"
[ "$(printf '%s\n' "$archive" | wc -l | tr -d ' ')" = "1" ] || \
  fail "multiple release archives found for ${os}/${arch}"

printf 'Downloading %s\n' "$archive"
curl -fL --proto '=https' --tlsv1.2 -sS \
  "$DOWNLOAD_BASE_URL/$archive" \
  -o "$tmp_dir/$archive"

expected_checksum="$(awk -v archive="$archive" '$2 == archive { print $1 }' "$tmp_dir/checksums.txt")"
if command -v sha256sum >/dev/null 2>&1; then
  actual_checksum="$(sha256sum "$tmp_dir/$archive" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
  actual_checksum="$(shasum -a 256 "$tmp_dir/$archive" | awk '{ print $1 }')"
else
  fail "required command not found: sha256sum or shasum"
fi
[ "$actual_checksum" = "$expected_checksum" ] || fail "checksum verification failed for $archive"

printf 'Installing subcommit and git-subcommit in %s\n' "$INSTALL_DIR"
tar -xzf "$tmp_dir/$archive" -C "$tmp_dir" subcommit git-subcommit
install -m 0755 "$tmp_dir/subcommit" "$INSTALL_DIR/subcommit"
install -m 0755 "$tmp_dir/git-subcommit" "$INSTALL_DIR/git-subcommit"

printf 'Installed successfully\n'
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) printf 'Add %s to PATH to run subcommit.\n' "$INSTALL_DIR" ;;
esac
