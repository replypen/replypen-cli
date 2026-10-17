#!/bin/sh
#
# install.sh — install the `rp` CLI on Linux/WSL, or explicitly to RC_INSTALL_DIR.
#
# Detects your OS/arch, downloads the matching prebuilt binary from the latest GitHub Release,
# and drops `rp` somewhere on your PATH, plus an `rc` → `rp` symlink so the pre-rename command keeps
# working. Idempotent: re-run it any time to upgrade; a pre-rename `rc` install is upgraded in place.
#
#   curl -fsSL https://raw.githubusercontent.com/replypen/replypen-cli/main/scripts/install.sh | sh
#
# Knobs (env vars):
#   RC_VERSION       install a specific version instead of latest, e.g. RC_VERSION=v0.5.1
#   RC_ASSET_SHA256  expected archive digest for a reviewed pinned release (requires RC_VERSION)
#   RC_INSTALL_DIR   install into this dir instead of auto-picking (/usr/local/bin or ~/.local/bin)

set -eu

REPO="replypen/replypen-cli"

err() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
info() { printf '\033[36m==>\033[0m %s\n' "$*"; }

command -v curl >/dev/null 2>&1 || err "curl is required but not found"
command -v tar  >/dev/null 2>&1 || err "tar is required but not found"

# --- detect os/arch ----------------------------------------------------------
os="$(uname -s)"
case "$os" in
  Linux)  os=linux ;;   # WSL reports Linux too — same binary
  Darwin) os=darwin ;;
  *) err "unsupported OS '$os' — on native Windows use scripts/install.ps1, or 'go install $REPO/cmd/rp@latest'" ;;
esac

# Homebrew is the one canonical macOS installation. An explicit RC_INSTALL_DIR remains available for
# managed/portable environments, but the default path must never create a second copy beside a cask.
if [ "$os" = darwin ] && [ "${RC_INSTALL_DIR:-}" = "" ]; then
  [ "${RC_VERSION:-}" = "" ] || err "RC_VERSION pinning requires an explicit RC_INSTALL_DIR on macOS; canonical Homebrew tracks latest"
  command -v brew >/dev/null 2>&1 || err "Homebrew is required for the canonical macOS install (or set RC_INSTALL_DIR explicitly)"
  info "updating the canonical Homebrew installation"
  brew update
  if brew list --cask rp >/dev/null 2>&1; then
    brew upgrade --cask rp
  elif brew list --cask rc >/dev/null 2>&1; then
    # the tap's cask_renames.json {"rc":"rp"} turns this into a migration onto the rp cask
    brew upgrade --cask rc || err "could not migrate the rc cask; run: brew uninstall --cask rc && brew install --cask replypen/tap/rp"
  else
    brew install --cask replypen/tap/rp
  fi
  canonical="$(brew --prefix)/bin/rp"
  [ -x "$canonical" ] || err "Homebrew completed but $canonical is missing"
  "$canonical" self update --migrate
  "$canonical" --version
  exit 0
fi

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64)  arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) err "unsupported arch '$arch' (need x86_64 or arm64)" ;;
esac

# --- resolve version ---------------------------------------------------------
if [ "${RC_VERSION:-}" != "" ]; then
  tag="$RC_VERSION"
  case "$tag" in v*) ;; *) tag="v$tag" ;; esac
else
  info "resolving latest release"
  tag="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
  [ -n "$tag" ] || err "could not resolve the latest release tag from the GitHub API"
fi
version="${tag#v}"

if [ "${RC_ASSET_SHA256:-}" != "" ]; then
  [ "${RC_VERSION:-}" != "" ] || err "RC_ASSET_SHA256 requires RC_VERSION"
  case "$RC_ASSET_SHA256" in
    *[!0-9a-fA-F]*|'') err "RC_ASSET_SHA256 must be a hexadecimal SHA-256 digest" ;;
  esac
  [ "${#RC_ASSET_SHA256}" -eq 64 ] || err "RC_ASSET_SHA256 must contain exactly 64 hex characters"
fi

asset="rp_${version}_${os}_${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/$tag/$asset"

# --- pick an install dir -----------------------------------------------------
# An existing rp/rc on PATH (including a pre-rename rc) is upgraded where it lives, never duplicated.
# is_ours <file>: a binary of this CLI (current or pre-rename module path in its Go build info).
is_ours() { [ -f "$1" ] && grep -qaE 'replypen/replypen-cli|rootcause-org/rootcause-cli' "$1"; }
existing="$(command -v rp 2>/dev/null || true)"
if [ -z "$existing" ]; then
  existing="$(command -v rc 2>/dev/null || true)"
  is_ours "$existing" || existing=""
fi
case "$existing" in /*) ;; *) existing="" ;; esac
if [ "${RC_INSTALL_DIR:-}" != "" ]; then
  bindir="$RC_INSTALL_DIR"
elif [ -n "$existing" ] && [ -w "$(dirname "$existing")" ]; then
  bindir="$(dirname "$existing")"
elif [ -w /usr/local/bin ] 2>/dev/null; then
  bindir=/usr/local/bin
elif command -v sudo >/dev/null 2>&1 && [ -d /usr/local/bin ]; then
  bindir=/usr/local/bin
  sudo=sudo
else
  bindir="$HOME/.local/bin"
fi
: "${sudo:=}"

# Never create a second, shadowed copy. Re-running against the selected target is idempotent; choosing
# a different directory requires the operator to resolve the existing installation first. A PATH `rc`
# is fine when it is the one in $bindir (an old copy there becomes the alias symlink below).
for cmd in rp rc; do
  selected_before="$(command -v "$cmd" 2>/dev/null || true)"
  case "$selected_before" in /*) ;; *) continue ;; esac
  [ "$cmd" = rp ] || is_ours "$selected_before" || continue # a foreign rc (e.g. Plan 9 shell) is not ours
  if [ "$(dirname "$selected_before")" != "$bindir" ] && ! [ "$selected_before" -ef "$bindir/rp" ]; then
    err "PATH already selects $selected_before; refusing to create a second install at $bindir (run '$selected_before self doctor', or set RC_INSTALL_DIR=$(dirname "$selected_before"))"
  fi
done

# --- download + extract + install --------------------------------------------
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

info "downloading $asset ($tag)"
curl -fsSL "$url" -o "$tmp/rp.tar.gz" || err "download failed: $url"
if [ "${RC_ASSET_SHA256:-}" != "" ]; then
  want="$RC_ASSET_SHA256"
else
  curl -fsSL "https://github.com/$REPO/releases/download/$tag/checksums.txt" -o "$tmp/checksums.txt" \
    || err "checksums download failed"
  want="$(awk -v asset="$asset" '$2 == asset {print $1; exit}' "$tmp/checksums.txt")"
  [ -n "$want" ] || err "checksums.txt has no entry for $asset"
fi
if command -v sha256sum >/dev/null 2>&1; then
  got="$(sha256sum "$tmp/rp.tar.gz" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  got="$(shasum -a 256 "$tmp/rp.tar.gz" | awk '{print $1}')"
else
  err "sha256sum or shasum is required to verify the download"
fi
[ "$got" = "$want" ] || err "checksum mismatch for $asset — refusing to install"
tar -xzf "$tmp/rp.tar.gz" -C "$tmp" rp || err "archive did not contain an 'rp' binary"
chmod +x "$tmp/rp"

$sudo mkdir -p "$bindir"
$sudo mv "$tmp/rp" "$bindir/rp"
if [ ! -e "$bindir/rc" ] || [ -L "$bindir/rc" ] || is_ours "$bindir/rc"; then
  $sudo ln -sfn rp "$bindir/rc"
  info "installed rp $version → $bindir/rp (rc → rp alias)"
else
  info "installed rp $version → $bindir/rp (left the unrelated $bindir/rc alone; no rc alias)"
fi
"$bindir/rp" --version >/dev/null 2>&1 && info "$("$bindir/rp" --version)"

for cmd in rp rc; do
  selected="$(command -v "$cmd" 2>/dev/null || true)"
  [ "$cmd" = rp ] || is_ours "$selected" || continue
  if [ -n "$selected" ] && ! [ "$selected" -ef "$bindir/rp" ]; then
    err "installed $bindir/rp, but PATH still selects $selected for '$cmd'; run '$bindir/rp self doctor' and remove the shadowing install"
  fi
done

# --- PATH hint ---------------------------------------------------------------
case ":$PATH:" in
  *":$bindir:"*) ;;
  *) printf '\033[33m! %s is not on your PATH — add this to your shell rc:\033[0m\n    export PATH="%s:$PATH"\n' "$bindir" "$bindir" ;;
esac
