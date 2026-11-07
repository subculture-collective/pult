# Palette export (v1)

`tender palette` and `tender render` give other tools a theme's colors without applying anything. Nothing is written to the change record. Subarchy uses them for root-owned targets (SDDM, Plymouth, Limine) that it applies through polkit.

## tender palette

```bash
tender palette [-format json|toml] <theme-id>
```

Prints every role in `color.AllRoles` order: the 26 Catppuccin roles, `ansi0` … `ansi15`, then `background`, `foreground`, `cursor`, `selection_bg`, `selection_fg`. Roles the theme doesn't define are filled in by the 26-role derivation, as for `apply`. Colors are lowercase `#rrggbb`. An unbundled theme is downloaded first, as for `apply`.

```json
{
  "schema": 1,
  "theme": {"id": "catppuccin-mocha", "name": "Catppuccin Mocha", "author": "Catppuccin", "appearance": "dark", "license": "MIT"},
  "colors": {
    "rosewater": "#f5e0dc",
    "…": "…",
    "selection_fg": "#cdd6f4"
  }
}
```

TOML has the same content: `schema = 1`, a `[theme]` table and a `[colors]` table. `schema` changes only if fields are removed or change meaning.

## tender render

```bash
tender render [-o file] <theme-id> <template>
```

Renders one Mustache template with the variables described in [registry-schema.md](registry-schema.md#templates). `<template>` is a file path, or else the name of a bundled template such as `kitty/theme-tender.conf.mustache`. Output goes to stdout, or to `-o`, which is overwritten without a backup.
