# Releasing Theme Tender

A release is a `v<version>` tag. The `release` workflow builds everything and attaches it to a Gitea release:

- `theme-tender-<version>-{linux,darwin}-{amd64,arm64}.tar.gz`: `tender` with the catalog built in, plus `README.md` and the licences;
- `theme-tender-<version>-src.tar.gz` (`git archive` of the tag) and `theme-tender-<version>-catalog.db.zst`, for source builds;
- `theme-tender-<version>-SHA256SUMS`;
- `PKGBUILD.theme-tender`, `PKGBUILD.theme-tender-bin` and `theme-tender.rb`, already filled with the version, download URLs and checksums.

All tarballs are reproducible: `scripts/dist.sh` sorts entries, fixes times and owners, and uses `gzip -n`. Data bundles are released separately ([bundle-signing.md](bundle-signing.md)).

## Before the first release (owner)

1. **Public downloads.** The AUR and Homebrew fetch the source tarball and catalog from the release URL. That only works once those downloads are public: make the Gitea repository public, or publish releases somewhere public and change the base URL passed to `scripts/render-packaging.sh` in `.gitea/workflows/release.yml`. The M1 plan proposes going public when package 10 lands.
2. **AUR.** An AUR account with an SSH key, to create `theme-tender` and `theme-tender-bin`.
3. **Homebrew tap.** Create `subculture-collective/homebrew-tap` (mirrored to GitHub, ADR 0002) with a `Formula/` directory.

## Release

```bash
git tag v0.1.0
git push origin v0.1.0
```

Then publish the filled files from the release:

```bash
# AUR, once per package (theme-tender, theme-tender-bin)
git clone ssh://aur@aur.archlinux.org/theme-tender.git aur-theme-tender
cp PKGBUILD.theme-tender aur-theme-tender/PKGBUILD
cd aur-theme-tender && makepkg --printsrcinfo > .SRCINFO && git add PKGBUILD .SRCINFO && git commit -m "Update to 0.1.0" && git push

# Homebrew tap
cp theme-tender.rb homebrew-tap/Formula/theme-tender.rb   # commit and push
```

## Check each channel on a clean machine

Arch, in a throwaway container:

```bash
podman run --rm -it docker.io/library/archlinux:latest sh -c '
  pacman -Sy --noconfirm --needed base-devel git go sudo >/dev/null
  useradd -m b && echo "b ALL=(ALL) NOPASSWD: ALL" > /etc/sudoers.d/b
  su b -c "cd && git clone https://aur.archlinux.org/theme-tender.git && cd theme-tender && makepkg -si --noconfirm"
  tender version && tender search -n 3 catppuccin && tender doctor'
```

macOS: `brew install subculture-collective/tap/theme-tender && tender version && tender doctor` on a Mac (Soyuz, with the owner's approval).

Tarball: download and unpack `theme-tender-<version>-linux-amd64.tar.gz` and run `./tender version`.

## Checked so far

2026-11-07, version `0.0.1` built locally on Sputnik:
- two `scripts/dist.sh` runs gave identical checksums;
- both PKGBUILDs built with `makepkg`, and the source package's `check()` passed all test packages;
- both packages installed with `pacman -U` into a fresh `archlinux` container, where `tender version`, `search`, `doctor` and `list` ran with the built-in catalog.

Not yet checked: the release workflow (it runs on the first tag), installs from the AUR and the tap (both need publishing), and Homebrew on macOS.
