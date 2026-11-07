# Registry schema (v1)

The registry describes each supported app: how to find it, where its config lives, how to theme it, and how to reload and check it. Built-in entries live in `registry/apps/<id>.yaml` (MIT). User entries in `~/.config/theme-tender/registry.d/<id>.yaml` override built-in entries with the same `id`.

[`registry/schema.json`](../../registry/schema.json) is the machine-readable contract; `internal/registry` loads and checks entries against it. This page explains it.

## Example

The kitty entry, [`registry/apps/kitty.yaml`](../../registry/apps/kitty.yaml):

```yaml
schema: 1
id: kitty
name: kitty
homepage: https://sw.kovidgoyal.net/kitty/
platforms: [linux, macos]
tier: 1                      # 1 generate+reload, 2 install upstream port, 3 manual link

detect:
  binaries: [kitty]
  macos_apps: [kitty.app]
  version:
    argv: [kitty, --version]
    regex: 'kitty ([0-9.]+)'

config:
  env:
    - { name: KITTY_CONFIG_DIRECTORY, kind: dir }
  paths:
    linux: ["$XDG_CONFIG_HOME/kitty/kitty.conf"]
    macos: ["$XDG_CONFIG_HOME/kitty/kitty.conf", "$HOME/Library/Preferences/kitty/kitty.conf"]

integrations:                 # first applicable entry wins
  - method: include
    template: kitty/theme-tender.conf.mustache
    owned_file: theme-tender.conf        # written next to the config file
    line: "include theme-tender.conf"
    position: end

reload:
  - argv: [pkill, -USR1, -x, kitty]
    when: running
validate:
  argv: [kitty, +runpy, "import sys; from kitty.config import load_config; load_config(sys.argv[-1])", "{theme_file}"]

ports:
  catppuccin: { url: https://github.com/catppuccin/kitty, kind: official }
  dracula: { url: https://github.com/dracula/kitty, kind: official }
```

Each built-in entry starts with a comment saying which commands and patterns were checked against an installed copy of the app, and which were not.

## Fields

| Field | Required | Meaning |
|---|---|---|
| `schema` | yes | Registry schema version, `1`. Other versions are rejected |
| `id` | yes | Lowercase letters, digits and `-`. Must match the file name. Used for overrides and the change record |
| `name`, `homepage` | yes | Display only. `homepage` is an `https://` URL |
| `platforms` | yes | Subset of `linux`, `macos` |
| `tier` | yes | `1` generate and reload, `2` install an upstream port, `3` manual instructions only |
| `detect.binaries` | one of | Names looked up on `PATH` |
| `detect.macos_apps` | one of | Bundle names under `/Applications` and `~/Applications` |
| `detect.packages` | one of | Package names per manager (`pacman`, `brew`, `dpkg`, `rpm`) |
| `detect.version` | no | `argv` to run and `regex` with exactly one capture group. Result cached per binary path and modification time |
| `config.env` | no | Environment variables that override the config location, in order. `kind: dir` names the directory that holds the config file, which keeps the file name of the first path; `kind: file` names the file |
| `config.paths` | yes | Candidate config paths per platform, one list for each platform in `platforms`. The first existing path wins; if none exists, Theme Tender creates the first |
| `integrations` | yes | Ordered list. The first entry whose `versions` range contains the installed version, and whose method the current owner allows, is used |
| `reload` | no | Commands run after apply. `when: running` runs only if the app has a running process; `when: always` runs every time. Optional `platforms` limits a command to some of the entry's platforms |
| `validate` | no | Command run against the written config. Non-zero exit fails the apply and triggers rollback of that app |
| `ports` | no | Known upstream ports per palette family, with `kind: official` or `community` |
| `disabled` | no | User overrides only. `true` removes the app from detection and apply |

Paths start with `$HOME/`, `$XDG_CONFIG_HOME/` or, in `macos` paths only, `$MAC_CONFIG_HOME/`. `$XDG_CONFIG_HOME` defaults to `$HOME/.config` on both platforms. `$MAC_CONFIG_HOME` is `$XDG_CONFIG_HOME` when that is set, and `$HOME/Library/Application Support` otherwise, which is where Go programs using `adrg/xdg` (lazygit) look on macOS.

When an app loads several config files and later ones win (Ghostty, tmux), list the candidates in reverse load order, so the first existing one is the file read last and Theme Tender's include is not overridden. The candidate created when none exists should be the one the app itself creates or reads first.

`versions` is a space-separated list of comparisons that must all hold, such as `>=0.14` or `>=1.2 <2`. Operators are `>=`, `<=`, `>`, `<` and `=`; a bare version means `=`. Only the leading dotted number of the installed version counts (`3.5a` is 3.5), and missing parts are zero (0.14 equals 0.14.0). No `versions` means any version.

## Integration methods

| Method | What Theme Tender writes | Fields |
|---|---|---|
| `include` | A file it owns, plus one line or one array entry in the user's config that loads it | `template`, `owned_file`, then either `line` and `position` (`start` or `end`), or `format` (`toml`, `yaml`) and `key`, an array the owned file's path is appended to |
| `owned` | A whole file in a theme directory the app reads (btop themes, yazi flavors, helix themes), plus a setting that selects it if needed | `template`, `target` (relative to the config file's directory), optional `select` |
| `keys` | Specific keys in the user's config, keeping comments and formatting | `format` (`yaml`, `toml`, `json`, `jsonc`, `ini`), `template` (renders the key values), `paths` (key paths it may change) |
| `ansi` | Settings that make the app use the terminal's 16 colors | `format`, `set` |
| `port` | A clone of an upstream port into the app's plugin or theme directory | `url`, `target`, optional `select` |
| `manual` | Nothing. Shows instructions and a link | `url` |

Every method also takes an optional `versions`.

`owned_file` is a file name written next to the config file, or a path from `$HOME/` or `$XDG_CONFIG_HOME/`. `select` and `set` are maps from dotted key paths to strings, numbers or booleans: `select: { format: toml, set: { flavor.dark: theme-tender } }`.

`ansi` is listed first for apps where it looks acceptable, because the app then follows any terminal theme change with nothing to regenerate.

## User overrides

A file in `registry.d` needs `schema` and `id`, and the `id` must match the file name. Its other fields are applied to the built-in entry with that `id`: mappings merge key by key, and every other value, lists included, replaces the built-in value. To change one integration, repeat the whole `integrations` list. A file for an `id` with no built-in entry must be a complete entry. `disabled: true` removes the app; disabling an `id` that does not exist is reported.

The merged entry must still match the schema. If a user file is unreadable, malformed or produces an invalid entry, Theme Tender skips that file, reports why, and keeps the built-in entry. A problem in a built-in entry stops loading, because it means the bundle is broken.

## Templates

Templates are Mustache files under `templates/<id>/` (MIT), rendered by `internal/render`. The renderer supports what theme templates need: variables, dotted names, sections, inverted sections and comments. It differs from full Mustache in three ways:

- Output is a config file, so nothing is HTML-escaped: `{{name}}`, `{{&name}}` and `{{{name}}}` are the same.
- A name that does not resolve is an error, so a typo fails the template's golden test instead of rendering empty.
- Partials and delimiter changes are rejected.

Variables available to every template:

- Every role in `color.AllRoles`: the 26 Catppuccin roles, `ansi0` … `ansi15`, `background`, `foreground`, `cursor`, `selection_bg`, `selection_fg`. Each has `.hex` (`#cdd6f4`), `.hex_bare` (`cdd6f4`), `.rgb` (`205, 214, 244`), `.r`, `.g` and `.b` (decimal). On its own, `{{mauve}}` is the same as `{{mauve.hex}}`.
- `{{theme.name}}`, `{{theme.slug}}`, `{{theme.author}}`, `{{theme.appearance}}` (`dark` or `light`).
- Base16/Base24 aliases `base00` … `base17`, with the same fields as roles. Slots map to roles as Tinted Theming's own Catppuccin Base24 schemes assign them (`base00` base, `base0E` mauve, `base11` crust, `base17` pink, …).
- Base16 builder specification names, so Tinted Theming templates can be reused: `{{base00-hex}}` (no `#`), `-hex-r`/`-g`/`-b`, `-rgb-r`/`-g`/`-b`, `-dec-r`/`-g`/`-b`, `-hex-bgr`, plus `scheme-name`, `scheme-author`, `scheme-slug`, `scheme-slug-underscored`, `scheme-system` (`base24`), `scheme-variant`, `scheme-is-dark-variant` and `scheme-is-light-variant`.

Every template has golden files in `internal/render/testdata/golden/` for Catppuccin Mocha, Catppuccin Latte and Dracula (derived roles). `go test ./internal/render -update` rewrites them; review the diff before committing.

Whether Aether's template variables can be offered as aliases is an M1 research item.

## Command safety

- Commands are `argv` arrays. They never pass through a shell.
- The only substitutions are `{config}` (resolved config path) and `{theme_file}` (the owned file path), in `reload` and `validate` arguments. An include `line` may use `{theme_file}`. Any other `{name}` is rejected when the entry is loaded.
- Pass a path to an interpreter as its own argument, not inside code: kitty's `validate` reads `sys.argv[-1]` instead of quoting `{theme_file}` into Python.
- Built-in entries are trusted only inside a correctly signed bundle.
