# Pult

Pult (`pult`) is a universal remote for your colors: a terminal app that themes your other apps from one palette. It finds which apps you have installed, changes only their colors, and records every change so it can put your configs back exactly as they were.

Status: M1 in progress. `pult` works with a catalog you build yourself; the signed data bundle and packages come later. See the [M1 plan](docs/plan/m1.md).

## Install

Arch Linux, from the `subcult` pacman repository on git.subcult.tv (Gitea signs it with its own key):

```bash
curl -fsS https://git.subcult.tv/api/packages/subculture-collective/arch/repository.key | sudo pacman-key --add -
sudo pacman-key --lsign-key CCE8B0F3BCDD601D909773E4466AA6E8241453D2
printf '\n[subcult]\nSigLevel = Required\nServer = https://git.subcult.tv/api/packages/subculture-collective/arch/$repo/$arch\n' | sudo tee -a /etc/pacman.conf
sudo pacman -Sy pult
```

Other platforms: release tarballs from [Releases](https://git.subcult.tv/subculture-collective/pult/releases) (`pult` with the catalog built in), or build from source with `go build ./cmd/pult`.

## Usage

```bash
make catalog                                   # build .cache/catalog.db.zst from the pinned sources
export PULT_CATALOG=$PWD/.cache/catalog.db.zst
go run ./cmd/pult tui                        # browse, preview, apply and undo
go run ./cmd/pult search -tag pastel rose    # find themes; -similar <id> lists close matches
go run ./cmd/pult apply -dry-run catppuccin-mocha   # plan and diffs, nothing written
go run ./cmd/pult apply catppuccin-mocha
go run ./cmd/pult uninstall                  # put every changed file back
go run ./cmd/pult palette catppuccin-mocha   # print the resolved palette as JSON (or -format toml)
go run ./cmd/pult render catppuccin-mocha my.conf.mustache   # render one template, nothing applied (-aether for an Aether template)
go run ./cmd/pult doctor                     # interrupted runs, app upgrades, Omarchy, frameworks
go run ./cmd/pult update                     # install the newest signed data bundle
go run ./cmd/pult apply -profile desk        # a profile: theme or palette file, apps, terminal font
```

Files that stow, chezmoi, a palette generator or a Hyprland framework manage are only written after you confirm. On Omarchy, Pult writes an Omarchy theme and switches to it instead of editing Omarchy's files.

## Goals

- Pick a theme from a large catalog (iTerm2 Color Schemes, Tinted Theming, Catppuccin, Dracula, Gogh) or bring your own.
- Apply it to every supported app it finds, using the app's include or theme mechanism where possible, so other settings are never touched.
- Undo everything with `pult uninstall`.
- Work alongside Omarchy, stow, chezmoi and palette generators instead of fighting them.
- Linux and macOS.

## Documentation

- [Docs index](docs/README.md)
- [ADR 0002: foundations](docs/adr/0002-theme-tender-foundations.md)
- Contracts: [registry](docs/reference/registry-schema.md), [change record](docs/reference/change-record.md), [catalog](docs/reference/catalog-schema.md)

## Development

```bash
make verify
make catalog   # fetch the pinned theme sources and build .cache/catalog.db.zst (needs git and network)
```

Gitea (`git.subcult.tv/subculture-collective/pult`) is the primary repository.

## License

Code: GPL-3.0-only, see `LICENSE`. Templates (`templates/`) and the app registry (`registry/`): MIT, see `LICENSES/MIT.txt`. Themes in the catalog keep their own licences, recorded per theme.
