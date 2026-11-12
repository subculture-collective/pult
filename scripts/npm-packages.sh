#!/usr/bin/env bash
# Builds the npm packages for VERSION from the release tarballs in dist/,
# into dist/npm/:
#   pult-<os>-<cpu>/   one per platform, holding that platform's binary
#   pult/              the launcher, with the four as optional dependencies
# npm installs only the platform package whose "os" and "cpu" match.
# SCOPE is the npm scope that owns the packages (default @subcult).
set -euo pipefail

version="${1:?usage: scripts/npm-packages.sh VERSION}"
scope="${SCOPE:-@subcult}"
out=dist/npm
rm -rf "$out"
mkdir -p "$out"

common() {
  cat <<JSON
  "version": "$version",
  "license": "GPL-3.0-only AND MIT",
  "homepage": "https://pult.subcult.tv",
  "repository": { "type": "git", "url": "git+https://github.com/subculture-collective/pult.git" },
  "bugs": "https://github.com/subculture-collective/pult/issues"
JSON
}

deps=""
for target in linux/amd64/x64 linux/arm64/arm64 darwin/amd64/x64 darwin/arm64/arm64; do
  IFS=/ read -r os goarch cpu <<<"$target"
  tarball="dist/pult-$version-$os-$goarch.tar.gz"
  [[ -f $tarball ]] || { echo "npm-packages: $tarball is missing; run scripts/dist.sh first" >&2; exit 1; }
  name="pult-$os-$cpu"
  dir="$out/$name"
  mkdir -p "$dir/bin"
  tar -xzf "$tarball" -C "$dir" --strip-components=1
  mv "$dir/pult" "$dir/bin/pult"
  chmod 0755 "$dir/bin/pult"
  cat > "$dir/package.json" <<JSON
{
  "name": "$scope/$name",
  "description": "The pult binary for $os on $cpu. Install $scope/pult instead.",
$(common),
  "os": ["$os"],
  "cpu": ["$cpu"],
  "files": ["bin/pult", "README.md", "LICENSE", "LICENSES"]
}
JSON
  deps+="${deps:+,
}    \"$scope/$name\": \"$version\""
done

dir="$out/pult"
mkdir -p "$dir/bin"
sed "s|@SCOPE@|$scope|" packaging/npm/pult.js > "$dir/bin/pult.js"
chmod 0755 "$dir/bin/pult.js"
cp README.md LICENSE "$dir/"
cp -r LICENSES "$dir/"
cat > "$dir/package.json" <<JSON
{
  "name": "$scope/pult",
  "description": "Apply one palette across your terminal apps, and undo every change",
$(common),
  "keywords": ["theme", "terminal", "colors", "palette", "tui", "dotfiles"],
  "bin": { "pult": "bin/pult.js" },
  "files": ["bin/pult.js", "README.md", "LICENSE", "LICENSES"],
  "engines": { "node": ">=18" },
  "optionalDependencies": {
$deps
  }
}
JSON
find "$out" -name package.json | sort
