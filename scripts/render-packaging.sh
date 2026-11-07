#!/usr/bin/env bash
# Fills the AUR and Homebrew templates in packaging/ with VERSION, the
# download base URL and the checksums from dist/, into dist/packaging/.
# BASE is where the release files can be downloaded; with BASE empty the
# sources are plain file names, for a local makepkg test.
set -euo pipefail

version="${1:?usage: scripts/render-packaging.sh VERSION BASE}"
base="${2-}"
sums="dist/theme-tender-$version-SHA256SUMS"
sha() { awk -v f="$1" '$2 == f { print $1 }' "$sums"; }

src=$(sha "theme-tender-$version-src.tar.gz")
cat=$(sha "theme-tender-$version-catalog.db.zst")
amd=$(sha "theme-tender-$version-linux-amd64.tar.gz")
arm=$(sha "theme-tender-$version-linux-arm64.tar.gz")
for v in src cat amd arm; do
  if [[ -z ${!v} ]]; then echo "render-packaging: no checksum for $v in $sums" >&2; exit 1; fi
done
prefix=""
[[ -n $base ]] && prefix="${base%/}/"

out=dist/packaging
rm -rf "$out"
for tmpl in packaging/aur/theme-tender/PKGBUILD packaging/aur/theme-tender-bin/PKGBUILD packaging/homebrew/theme-tender.rb; do
  mkdir -p "$out/$(dirname "${tmpl#packaging/}")"
  sed -e "s|@VERSION@|$version|g" -e "s|@BASE@|$prefix|g" \
    -e "s|@SHA_SRC@|$src|g" -e "s|@SHA_CATALOG@|$cat|g" \
    -e "s|@SHA_LINUX_AMD64@|$amd|g" -e "s|@SHA_LINUX_ARM64@|$arm|g" \
    "$tmpl" > "$out/${tmpl#packaging/}"
  if grep -n '@[A-Z_]*@' "$out/${tmpl#packaging/}"; then
    echo "render-packaging: unfilled placeholder in $tmpl" >&2
    exit 1
  fi
done
find "$out" -type f | sort
