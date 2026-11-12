#!/usr/bin/env bash
# Publishes the npm packages for VERSION. Needs the four platform tarballs
# for VERSION in dist/: with FETCH=1 it downloads them from the Gitea
# release and checks them against its SHA256SUMS first. NODE_AUTH_TOKEN
# is an npm token that may publish to SCOPE (default @subcult). A package
# version already on the registry is skipped, so a rerun is safe.
set -euo pipefail

version="${1:?usage: scripts/publish-npm.sh VERSION}"
scope="${SCOPE:-@subcult}"
: "${NODE_AUTH_TOKEN:?NODE_AUTH_TOKEN is not set}"

if [[ ${FETCH:-} == 1 ]]; then
  base="https://git.subcult.tv/subculture-collective/pult/releases/download/v$version"
  mkdir -p dist
  curl -fsSL -o "dist/pult-$version-SHA256SUMS" "$base/pult-$version-SHA256SUMS"
  for t in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do
    curl -fsSL -o "dist/pult-$version-$t.tar.gz" "$base/pult-$version-$t.tar.gz"
  done
  (cd dist && grep -E "pult-$version-(linux|darwin)-" "pult-$version-SHA256SUMS" | sha256sum -c -)
fi

SCOPE="$scope" scripts/npm-packages.sh "$version"
npmrc=$(mktemp)
trap 'rm -f "$npmrc"' EXIT
# npm expands ${NODE_AUTH_TOKEN} itself, so the token is never written out.
printf '//registry.npmjs.org/:_authToken=${NODE_AUTH_TOKEN}\n' > "$npmrc"
# Platform packages first, so the launcher never names a missing version.
for dir in dist/npm/pult-*/ dist/npm/pult/; do
  name=$(node -p "require('./$dir/package.json').name")
  if npm view --userconfig "$npmrc" "$name@$version" version >/dev/null 2>&1; then
    echo "$name@$version is already published; skipping"
    continue
  fi
  npm publish --userconfig "$npmrc" --access public "$dir"
done
