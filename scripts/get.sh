#!/usr/bin/env bash
# aidev-clis remote install (macOS/Linux): download a release archive from
# GitHub, verify its sha256, and run the bundled installer in prebuilt mode.
#
#   curl -fsSL https://raw.githubusercontent.com/no-today/aidev-clis/main/scripts/get.sh | bash
#
# AIDEV_VERSION pins a tag (e.g. v2026.7.3-pre); default is the latest release.
# Extra args are forwarded to install.sh (e.g. `bash get.sh dbcli logcli`).
set -euo pipefail

REPO_SLUG="no-today/aidev-clis"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "error: unsupported arch '$arch' (supported: amd64 arm64)" >&2; exit 2 ;;
esac
case "$os" in
  darwin | linux) ;;
  *) echo "error: unsupported OS '$os' (supported: darwin linux; Windows: get.ps1)" >&2; exit 2 ;;
esac

# Resolve the tag: /releases/latest is a redirect to /releases/tag/<tag>, so
# one HEAD request yields the version without touching the GitHub API.
tag="${AIDEV_VERSION:-}"
if [ -z "$tag" ]; then
  tag=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO_SLUG/releases/latest")
  tag=${tag##*/}
  case "$tag" in
    v*) ;;
    *)
      # /releases/latest excludes prereleases; when only prereleases exist it
      # doesn't redirect to a tag. Fall back to the newest release of any kind.
      tag=$(curl -fsSL "https://api.github.com/repos/$REPO_SLUG/releases?per_page=1" |
        sed -n 's/.*"tag_name"[^"]*"\([^"]*\)".*/\1/p' | head -n 1)
      case "$tag" in
        v*) ;;
        *) echo "error: cannot resolve any release; is there a release yet?" >&2; exit 1 ;;
      esac
      ;;
  esac
fi
version=${tag#v}

asset="aidev-clis_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO_SLUG/releases/download/$tag"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo ">> downloading $asset ($tag)"
curl -fsSL -o "$tmp/$asset" "$base/$asset"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

echo ">> verifying sha256"
if command -v sha256sum > /dev/null 2>&1; then
  (cd "$tmp" && grep " $asset\$" checksums.txt | sha256sum -c - > /dev/null)
else
  (cd "$tmp" && grep " $asset\$" checksums.txt | shasum -a 256 -c - > /dev/null)
fi

tar -xzf "$tmp/$asset" -C "$tmp"
installer=$(find "$tmp" -maxdepth 3 -name install.sh -path '*/scripts/*' | head -1)
[ -n "$installer" ] || { echo "error: install.sh not found in archive" >&2; exit 1; }
bash "$installer" "$@"
