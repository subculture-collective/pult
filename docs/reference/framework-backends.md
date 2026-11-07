# Framework backends (v1)

Decision: [ADR 0004](../adr/0004-framework-backends.md). Implemented by `internal/framework`; wired into `apply` like the Omarchy backend.

When owner detection finds a framework with a backend, `tender apply` plans it before the apps. The plan shows the files and the switch command, or a note when the framework is left alone. After the change record writes the files, Theme Tender runs the switch. `tender uninstall` switches back to the selection recorded before Theme Tender's first switch, if Theme Tender's selection is still active, and then removes the files. `tender doctor` lists which detected frameworks are themed this way.

| Framework | Files Theme Tender owns | Switch | Selection read from | Checked |
|---|---|---|---|---|
| `noctalia` | `~/.config/noctalia/palettes/tender-<slug>.json`, palette format 2: 16 color roles plus `terminal` (ANSI, background, foreground, cursor, selection); `dark` only, which Noctalia uses for both modes | `noctalia msg color-scheme-set custom tender-<slug>` | `noctalia msg color-scheme-get` | Arch container, noctalia 5.4.1 under headless sway: `builtin Noctalia` → `custom tender-catppuccin-mocha` → back after uninstall, no palette warnings |
| `dms` | `~/.config/dms/theme-tender/tender-<slug>.json`, a flat theme (`primary`, `surface`, `surfaceContainer*`, …) | `dms ipc call settings set customThemeFile <file>`, then `… currentThemeName custom` | `dms ipc call settings get currentThemeName` and `customThemeFile` (JSON strings) | Arch container, dux-shell 2.1.0 under headless sway: `purple` → `custom` with the file → `purple` after uninstall |
| `hyde` | `~/.config/hyde/themes/tender-<slug>/`: `hypr.theme` (borders, `$COLOR_SCHEME`, GTK and icon themes copied from the theme in use), `kitty.theme` (exact terminal colors), `theme.dcol` (wallbash's 4 primaries, text colors and 9 shades each), `wallpapers/tender-<slug>.png` (plain background color) | `hydectl theme set tender-<slug>`, else `~/.local/lib/hyde/theme.switch.sh -q -s` | `HYDE_THEME` in `~/.local/state/hyde/staterc` | Unit tests; formats from HyDE `fa2fd57` (`theme.switch.sh`, `color.set.sh`, `wallbash.sh`) |
| `caelestia` | none | `caelestia scheme set -n <name> -f <flavour> -m <mode> -v <variant>`, for catalog themes Caelestia packages (Catppuccin flavors, Dracula, Nord, Gruvbox, Rosé Pine, Tokyo Night, Everforest, Everblush, One Dark); the user's variant is kept | `caelestia scheme get -n -f -m -v` | Arch container, caelestia CLI `6bde2df`: `gruvbox` → `catppuccin mocha` → `gruvbox` after uninstall |
| `end-4` | none | `~/.config/quickshell/ii/scripts/colors/switchwall.sh --noswitch --color <blue>`; back with the previous accent or `clear` (wallpaper colors). Needs `HYPRLAND_INSTANCE_SIGNATURE` and `ILLOGICAL_IMPULSE_VIRTUAL_ENV`, so run from the session | `appearance.palette.accentColor` in `~/.config/illogical-impulse/config.json` | Unit tests; script from dots-hyprland `2f2a376` |
| `ml4w` | — | none: ML4W regenerates every color output from the wallpaper at each login | — | Detected; its files are asked about |

Role mapping: the accent is Catppuccin's `blue` role (as for Omarchy), secondary `mauve`, tertiary `teal`, error `red`; surfaces follow `base`, `mantle`, `crust` and `surface0`–`2`; text `text` and `subtext0`.

## Limits

- **DankMaterialShell** derives terminal ANSI colors itself, so terminals it themes do not get the palette's ANSI colors.
- **end-4** derives everything from one accent color.
- **HyDE** applies theme colors only in theme mode (`enableWallDcol=0`). In wallpaper mode the plan notes it. HyDE's switch also rewrites GTK, Qt and Xresources settings, as it does for any theme.
- **Not running.** A shell that is not running, or a session that is missing, leaves the framework alone with a note. Apply again once it is up.
