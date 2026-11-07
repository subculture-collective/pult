#!/usr/bin/env bash
# Builds the theme-tender pacman package for VERSION from the release's
# filled PKGBUILD (tests included) and uploads it to the Gitea Arch package
# registry, repository "subcult". Gitea signs the package and the database.
# Runs as root in an archlinux container; needs GITEA_TOKEN with package
# write access, GITHUB_SERVER_URL and GITHUB_REPOSITORY. See
# docs/runbooks/release.md.
set -euo pipefail

version="${1:?usage: scripts/publish-arch.sh VERSION}"
repo=subcult
owner="${GITHUB_REPOSITORY%%/*}"
release="${GITHUB_SERVER_URL}/${GITHUB_REPOSITORY}/releases/download/v${version}"
upload="${GITHUB_SERVER_URL}/api/packages/${owner}/arch/${repo}"

pacman -Syu --noconfirm --needed base-devel go >/dev/null
id builder >/dev/null 2>&1 || useradd -m builder
dir=/home/builder/theme-tender
rm -rf "$dir" && mkdir -p "$dir"
curl -fsSL "$release/PKGBUILD.theme-tender" -o "$dir/PKGBUILD"
chown -R builder "$dir"
su builder -c "cd '$dir' && makepkg --noconfirm"

shopt -s nullglob
pkgs=()
for f in "$dir"/theme-tender-"$version"-*.pkg.tar.zst; do
  [[ $f == *-debug-* ]] || pkgs+=("$f")
done
if ((${#pkgs[@]} == 0)); then
  echo "publish-arch: makepkg made no package for $version" >&2
  exit 1
fi
for f in "${pkgs[@]}"; do
  code=$(curl -sS -o /tmp/upload.out -w '%{http_code}' -X PUT -H "Authorization: token ${GITEA_TOKEN}" --upload-file "$f" "$upload")
  case $code in
    201) echo "published $(basename "$f") to $repo" ;;
    409) echo "$(basename "$f") is already in $repo" ;;
    *) echo "publish-arch: upload of $(basename "$f") returned $code: $(head -c 300 /tmp/upload.out)" >&2; exit 1 ;;
  esac
done
