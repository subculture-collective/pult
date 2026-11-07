#!/usr/bin/env bash
# Builds the release files for VERSION into dist/:
#   theme-tender-VERSION-<os>-<arch>.tar.gz   tender with the catalog built in
#   theme-tender-VERSION-src.tar.gz           source, from git archive
#   theme-tender-VERSION-catalog.db.zst       catalog, for source builds
#   theme-tender-VERSION-SHA256SUMS
# Needs builtin/catalog.db.zst (make catalog, then copy it there). Tarballs
# are reproducible: sorted entries, fixed times and owners, gzip -n.
set -euo pipefail

version="${1:?usage: scripts/dist.sh VERSION [GIT-REV]}"
rev="${2:-HEAD}"
catalog=builtin/catalog.db.zst
if [[ ! -f $catalog ]]; then
  echo "dist: $catalog is missing; run make catalog and copy .cache/catalog.db.zst there" >&2
  exit 1
fi

rm -rf dist/theme-tender-"$version"-*
mkdir -p dist
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os=${target%/*} arch=${target#*/}
  name="theme-tender-$version-$os-$arch"
  stage="dist/$name"
  mkdir -p "$stage"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=$version" -o "$stage/tender" ./cmd/tender
  cp README.md LICENSE "$stage/"
  cp -r LICENSES "$stage/"
  tar --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner -C dist -cf - "$name" | gzip -n9 > "dist/$name.tar.gz"
  rm -rf "$stage"
done
git archive --format=tar.gz --prefix="theme-tender-$version/" -o "dist/theme-tender-$version-src.tar.gz" "$rev"
cp "$catalog" "dist/theme-tender-$version-catalog.db.zst"
(cd dist && sha256sum theme-tender-"$version"-*.tar.gz theme-tender-"$version"-catalog.db.zst > "theme-tender-$version-SHA256SUMS")
cat "dist/theme-tender-$version-SHA256SUMS"
