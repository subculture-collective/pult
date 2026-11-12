# Pult

Pult (`pult`) is a universal remote for your colours: a terminal app that
themes your other apps from one palette. It finds which supported apps you
have installed, changes only their colours, and records every change so it can
put your configs back exactly as they were.

Pult runs on Linux and macOS. It is early 0.x software, so expect changes
between releases.

Website and user docs: <https://pult.subcult.tv>

## Features

- **Theme catalog** built from iTerm2 Color Schemes, Tinted Theming,
  Catppuccin, Dracula and Gogh. Search by name and tags (`dark`, `pastel`,
  `warm` and so on), or list the themes closest to one you like. Each theme
  keeps its own licence.
- **Apply to every supported app it finds**, through the app's own include or
  theme mechanism where one exists, so other settings stay as they are.
  `-dry-run` prints the plan and diffs without writing anything.
- **Undo**: every file Pult creates, edits or deletes is recorded first, with
  a compressed copy of the original. `pult uninstall` restores them all, and
  `pult doctor -recover` rolls back an interrupted run.
- **Works with other tools**: files owned by stow, chezmoi, matugen, wallust,
  pywal or a Hyprland framework (Noctalia, DankMaterialShell, HyDE, Caelestia,
  end-4) are written only after you confirm. On Omarchy, Pult writes an
  Omarchy theme and switches to it instead of editing Omarchy's files.
- **TUI** (`pult tui`): a full-screen browser with panes, tabs, a search box
  and mouse support, to preview a theme's colour swatches, apply it and roll
  it back.
- **Profiles**: a TOML file that picks a theme, a set of apps and a terminal
  font.
- **Palette files and export**: bring your own palette as JSON or TOML, print
  a theme's resolved palette, or render any Mustache template with it.

Supported apps: Alacritty, Ghostty, kitty, foot, iTerm2, fish, Starship, tmux,
Zellij, bat, btop, cava, delta, eza, fzf, Helix, lazygit, lsd, Yazi, GTK 3,
GTK 4, and the Subarchy shell, lock screen and Hyprland colours. WezTerm and
Terminal.app get instructions instead of edits. `pult list` shows what it
found on your machine. The app definitions are in [registry/apps](registry/apps).

## Install

### Arch Linux

Pult is in the `subcult` pacman repository on git.subcult.tv, signed with that
server's key:

```bash
curl -fsS https://git.subcult.tv/api/packages/subculture-collective/arch/repository.key | sudo pacman-key --add -
sudo pacman-key --lsign-key CCE8B0F3BCDD601D909773E4466AA6E8241453D2
printf '\n[subcult]\nSigLevel = Required\nServer = https://git.subcult.tv/api/packages/subculture-collective/arch/$repo/$arch\n' | sudo tee -a /etc/pacman.conf
sudo pacman -Sy pult
```

### Release tarball

Download the Linux or macOS tarball from the
[releases page](https://git.subcult.tv/subculture-collective/pult/releases),
unpack it and put `pult` on your `PATH`. The catalog is built in.

### From source

You need Go 1.26 or newer, Git and network access:

```bash
git clone https://github.com/subculture-collective/pult.git
cd pult
make catalog
go build -o ~/.local/bin/pult ./cmd/pult
export PULT_CATALOG=$PWD/.cache/catalog.db.zst
```

`make catalog` fetches the pinned theme sources and builds
`.cache/catalog.db.zst`. A source build has no catalog of its own, so keep the
`PULT_CATALOG` export in your shell profile with the full path.

## Usage

```bash
pult list                             # supported apps and their state
pult search -tag pastel rose          # find themes; -similar <id> lists close matches
pult apply -dry-run catppuccin-mocha  # plan and diffs, nothing written
pult apply catppuccin-mocha           # asks, then applies
pult uninstall                        # put every changed file back
pult tui                              # browse, preview, apply and undo
pult apply -profile desk              # a profile from $XDG_CONFIG_HOME/pult/profiles/
pult palette catppuccin-mocha         # resolved palette as JSON (-format toml)
pult render catppuccin-mocha my.conf.mustache   # render one template, apply nothing
pult doctor                           # interrupted runs, app upgrades, Omarchy, frameworks
```

The [user docs](https://pult.subcult.tv/docs/) cover every command, the TUI
keys, profiles and palette files.

## Configuration

| Setting | Default | Meaning |
|---|---|---|
| `-catalog PATH` | `$PULT_CATALOG` | Theme catalog (`catalog.db` or `catalog.db.zst`) |
| `-state DIR` | `$XDG_STATE_HOME/pult` | Change record and copies of original files |
| `-data DIR` | built in | Directory holding `registry/` and `templates/` |
| `-bundle PATH` | `$XDG_DATA_HOME/pult/bundle.tar.zst` | Signed data bundle installed by `pult update` |

Profiles live in `$XDG_CONFIG_HOME/pult/profiles/`. The formats are documented
in [docs/reference](docs/reference/README.md): the
[app registry](docs/reference/registry-schema.md), the
[change record](docs/reference/change-record.md), the
[catalog](docs/reference/catalog-schema.md), [profiles](docs/reference/profiles.md)
and [palette export](docs/reference/palette-export.md).

## Development

```bash
make verify    # gofmt check, go vet, go test and repository checks
make catalog   # build .cache/catalog.db.zst from the pinned sources (needs network)
make dist VERSION=x.y.z   # release tarballs into dist/
```

`make help` lists every target. Packaging templates for the AUR (`pult`,
`pult-bin`) and Homebrew are in [packaging/](packaging/).

The website in [site/](site/README.md) is static HTML with no build step.
[deploy/](deploy/) has a Dockerfile that serves it with unprivileged nginx on
port 8080.

## Project layout

```text
cmd/pult/        the CLI and TUI
cmd/catalog-build/  builds the theme catalog from pinned sources
internal/        apply, change record, config editing, catalog, colour, TUI, framework and Omarchy backends
registry/apps/   one YAML definition per supported app
templates/       Mustache colour templates per app
catalog/         pinned theme source list
site/            pult.subcult.tv landing page and docs
docs/            decisions, reference contracts and plans
```

## Contributing

Report bugs and request features in
[GitHub issues](https://github.com/subculture-collective/pult/issues). For
code, open a pull request on GitHub against `main`. CI runs `make verify` on
Linux and macOS for every pull request, and both must pass before it merges.
See [CONTRIBUTING.md](CONTRIBUTING.md) and, for security reports,
[SECURITY.md](SECURITY.md).

## License

Code: GPL-3.0-only, see [LICENSE](LICENSE). Templates (`templates/`) and the
app registry (`registry/`): MIT, see [LICENSES/MIT.txt](LICENSES/MIT.txt).
Themes in the catalog keep their own licences, recorded per theme.
