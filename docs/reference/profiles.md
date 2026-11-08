# Profiles and palette files (v1)

Decisions: [ADR 0003](../adr/0003-profiles-fonts.md). Implemented by `internal/profile`.

## Profiles

A profile is a TOML file with `schema = 1` and these optional fields:

| Field | Meaning |
|---|---|
| `theme` | Catalog theme ID, or a palette file path. A relative path is relative to the profile file |
| `apps` | Registry IDs to theme. Omitted or empty means every app |
| `skip` | Registry IDs to leave alone, even if `apps` lists them |
| `[font]` `family`, `size` | Terminal font for kitty, Ghostty, Alacritty and foot. `size` in points; omit it to keep the terminal's size. The family may not contain `"`, `\` or a line break |

Unknown fields are an error, so a typo does not go unnoticed.

```bash
pult apply -profile desk                 # $XDG_CONFIG_HOME/pult/profiles/desk.toml
pult apply -profile ./subarchy.toml       # any file
pult apply -profile desk dracula          # the profile, with another theme
pult profiles                             # list the user's profiles
```

Apps a profile leaves out are listed as `skip: not in the profile`. The font goes into the same file Pult owns for the terminal's colors, after them, so `pult uninstall` removes it with the colors. On Linux, `apply` warns when `fc-list` does not know the font family; Pult never installs fonts.

## Palette files

A palette file has the format [`pult palette`](palette-export.md) prints, in JSON or TOML (by extension):

```json
{
  "schema": 1,
  "theme": {"id": "subcult-minimal", "name": "SUBCULT Minimal", "author": "SUBCULT", "appearance": "dark"},
  "colors": {"ansi0": "#100b19", "…": "…", "ansi15": "#f0ece4", "background": "#100b19", "foreground": "#f0ece4"}
}
```

- `colors` needs `ansi0` … `ansi15`, `background` and `foreground`; any role in `color.AllRoles` may be given and the rest are derived as for catalog themes. Unknown roles are an error.
- `theme` is optional. Without an `id`, the name (or the file name) is turned into one, which is what the change record and `pult doctor` show. Without `appearance`, it follows the background's lightness.
- A palette file works anywhere a theme ID does: `apply`, `palette`, `render`, and a profile's `theme`. An argument counts as a file when it contains `/` or ends in `.json` or `.toml`.
