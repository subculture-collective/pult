# ADR 0003: Profiles, palette files and fonts (M4)

## Status

Accepted, 2026-11-07. The owner delegated the M4 scope questions in #41 ("use your best judgement"); this ADR records the answers.

## Context

The M1 plan put "suite deploy profiles, fonts, opinionated tmux/terminal/prompt settings" in M4, and ADR 0002 says the Subarchy installer calls `tender apply` with a profile. Subarchy's ADR 0037 adds three facts:

- Its installer applies `subcult-minimal`, a palette that is not in the catalog.
- It describes Theme Tender as the tool that "changes only colours".
- Its layered defaults (Subarchy ADR 0048) own every other setting, and its fonts come from fontconfig defaults (Subarchy ADR 0039).

## Decision

| Question (#41) | Answer |
|---|---|
| What a profile contains | A theme (catalog ID or palette file), which apps to theme (`apps`, `skip`) and an optional terminal font. Nothing else |
| Where profiles live | A TOML file anywhere, passed by path, or a name in `$XDG_CONFIG_HOME/theme-tender/profiles/`. None are built in: Subarchy ships its own profile and palette with its package, so Theme Tender carries no distribution's choices |
| Themes outside the catalog | A palette file in the format `tender palette` prints (JSON or TOML, schema 1) works wherever a theme ID does: `apply`, `palette`, `render`, and a profile's `theme`. ANSI 0–15, background and foreground are required; other roles are derived as for catalog themes |
| Fonts | A profile may set a font family and size for terminals whose theme file Theme Tender owns: kitty, Ghostty, Alacritty and foot. Theme Tender never installs fonts; on Linux it warns when fontconfig does not know the family. The font lives in the same owned file as the colors, so undo and uninstall cover it |
| Opinionated settings | Not part of Theme Tender. They would make it edit behaviour, not appearance; they conflict with Subarchy's layered defaults, which own settings; and an undo of "your tmux settings" is not something a theme switch should carry. Distributions ship them as defaults; users keep them in their dotfiles |

A profile:

```toml
schema = 1
theme = "subcult-minimal.toml"   # relative to this file, or a catalog ID
apps = ["kitty", "ghostty", "tmux", "starship"]   # omit for every app
skip = ["btop"]
[font]
family = "JetBrains Mono"
size = 11
```

`tender apply -profile <name|file> [theme]` applies it; a theme argument replaces the profile's. `tender profiles` lists the user's profiles. Reference: [profiles.md](../reference/profiles.md).

## Consequences

- Subarchy's installer runs `tender apply -yes -profile /usr/share/subcult/theme-tender/subarchy.toml`, with `subcult-minimal` as a palette file next to it. Subarchy needs no catalog change and no Theme Tender release to change its default look.
- Fonts are limited to four terminals. iTerm2 wants PostScript names and WezTerm's config is Lua, so they are left out until someone needs them.
- M4 has no opinionated-settings work. If a later need appears, it should be a separate tool or Subarchy's defaults, not a profile field.
- Palette files are not signed. They contain only colors and names, which are escaped in templates (#63), so they cannot run anything.
