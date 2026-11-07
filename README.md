# Theme Tender

Theme Tender (`tender`) is a terminal app that themes your other apps from one palette. It finds which apps you have installed, changes only their colors, and records every change so it can put your configs back exactly as they were.

Status: planning. Nothing is usable yet. See the [M1 plan](docs/plan/m1.md).

## Goals

- Pick a theme from a large catalog (iTerm2 Color Schemes, Tinted Theming, Catppuccin, Dracula, Gogh) or bring your own.
- Apply it to every supported app it finds, using the app's include or theme mechanism where possible, so other settings are never touched.
- Undo everything with `tender uninstall`.
- Work alongside Omarchy, stow, chezmoi and palette generators instead of fighting them.
- Linux and macOS.

## Documentation

- [Docs index](docs/README.md)
- [ADR 0002: foundations](docs/adr/0002-theme-tender-foundations.md)
- Contracts: [registry](docs/reference/registry-schema.md), [change record](docs/reference/change-record.md), [catalog](docs/reference/catalog-schema.md)

## Development

```bash
make verify
```

Gitea (`git.subcult.tv/subculture-collective/theme-tender`) is the primary repository.

## License

Code: GPL-3.0-only, see `LICENSE`. Templates (`templates/`) and the app registry (`registry/`): MIT, see `LICENSES/MIT.txt`. Themes in the catalog keep their own licences, recorded per theme.
