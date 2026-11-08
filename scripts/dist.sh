#!/usr/bin/env bash
# Builds the release files for VERSION into dist/:
#   pult-VERSION-<os>-<arch>.tar.gz   pult with the catalog built in
#   pult-VERSION-src.tar.gz           source, from git archive
#   pult-VERSION-catalog.db.zst       catalog, for source builds
#   pult-VERSION-SHA256SUMS
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

# The built-in data's bundle version: the tagged commit's date, so a signed
# bundle older than this binary does not replace its data.
dataversion=$(TZ=UTC git log -1 --format=%cd --date=format-local:%Y.%m.%d.0 "$rev")

rm -rf dist/pult-"$version"-*
mkdir -p dist
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os=${target%/*} arch=${target#*/}
  name="pult-$version-$os-$arch"
  stage="dist/$name"
  mkdir -p "$stage"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=$version -X git.subcult.tv/subculture-collective/pult.BuiltinVersion=$dataversion" -o "$stage/pult" ./cmd/pult
  cp README.md LICENSE "$stage/"
  cp -r LICENSES "$stage/"
  tar --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner -C dist -cf - "$name" | gzip -n9 > "dist/$name.tar.gz"
  rm -rf "$stage"
done
git archive --format=tar.gz --prefix="pult-$version/" -o "dist/pult-$version-src.tar.gz" "$rev"
cp "$catalog" "dist/pult-$version-catalog.db.zst"
(cd dist && sha256sum pult-"$version"-*.tar.gz pult-"$version"-catalog.db.zst > "pult-$version-SHA256SUMS")
cat "dist/pult-$version-SHA256SUMS"
