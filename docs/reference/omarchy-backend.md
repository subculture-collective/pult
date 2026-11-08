# Omarchy backend (v1)

On Omarchy, Pult doesn't write app configs. It writes an Omarchy theme and switches to it, and Omarchy renders kitty, ghostty, alacritty, btop, Hyprland and the rest from its own templates. Files Omarchy owns are never edited (ADR 0002). `internal/omarchy` implements this; the ownership detector hands such files off (see [ownership.md](ownership.md)).

## Theme directory

`$HOME/.config/omarchy/themes/pult-<slug>/`, with:

- `colors.toml`, the palette in Omarchy's keys;
- `README.md`, saying where the theme came from.

Omarchy hard-codes that path, so `$XDG_CONFIG_HOME` isn't used. The `pult-` prefix matters: a user theme named like a built-in one (`catppuccin`) is overlaid on the built-in theme, which would mix in its backgrounds and editor themes. No backgrounds are shipped.

Both files go through the change record like any other edit, so `pult uninstall` removes them.

## colors.toml

Omarchy uses `background`, `foreground`, `muted` and `bright_foreground` as ANSI 0, 7, 8 and 15, and derives `brown` and anything left out.

| Omarchy key | Role |
|---|---|
| `mode` | `dark` or `light`, from the theme's appearance |
| `accent` | `blue` |
| `selection`, `selection_background` / `selection_foreground` | `selection_bg` / `selection_fg` |
| `muted` | `ansi8` |
| `background`, `dark_background`, `darker_background`, `lighter_background` | `background`, `mantle`, `crust`, `surface0` |
| `foreground`, `dark_foreground`, `light_foreground`, `bright_foreground` | `foreground`, `overlay0`, `subtext1`, `ansi15` |
| `red`, `green`, `yellow`, `blue`, `magenta`, `cyan` | `ansi1` … `ansi6` |
| `orange` | `peach` |
| `bright_red` … `bright_magenta` | `ansi9` … `ansi14` |

Because Omarchy sets ANSI 0 to the background, a scheme whose ANSI 0 differs from its background loses that distinction under Omarchy.

## Switching

After the files are written, Pult runs `omarchy-theme-set pult-<slug>`. Before the first switch it records the active theme (`$XDG_STATE_HOME/omarchy/current/theme.name`), so that uninstall can switch back to it. That last step belongs to the apply pipeline.
