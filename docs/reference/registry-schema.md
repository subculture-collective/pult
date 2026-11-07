# Registry schema (draft v1)

The registry describes each supported app: how to find it, where its config lives, how to theme it, and how to reload and check it. Built-in entries live in `registry/apps/<id>.yaml` (MIT). User entries in `~/.config/theme-tender/registry.d/<id>.yaml` override built-in entries with the same `id`, field by field.

This is a draft contract. M1 turns it into a JSON Schema in `registry/schema.json` and validates every entry in CI.

## Example

The reload and validate commands below are illustrative. M1 confirms the real command for each app before the entry ships.

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
  env: KITTY_CONFIG_DIRECTORY   # when set, replaces the candidate directories
  file: kitty.conf
  dirs:
    linux: ["$XDG_CONFIG_HOME/kitty"]
    macos: ["$XDG_CONFIG_HOME/kitty", "$HOME/Library/Preferences/kitty"]

integrations:                 # first applicable entry wins
  - method: include
    versions: ">=0.26"
    template: kitty/theme.conf.mustache
    owned_file: theme-tender.conf        # written next to the config file
    line: "include theme-tender.conf"
    position: end

reload:
  - argv: [kitten, "@", load-config]
    when: running
validate:
  argv: [kitty, +runpy, "from kitty.config import load_config; load_config('{config}')"]

ports:
  catppuccin: { url: https://github.com/catppuccin/kitty, kind: official }
  dracula: { url: https://github.com/dracula/kitty, kind: official }
```

## Fields

| Field | Required | Meaning |
|---|---|---|
| `schema` | yes | Registry schema version. Unknown versions are rejected |
| `id` | yes | Lowercase, unique, stable. Used for overrides and the change record |
| `name`, `homepage` | yes | Display only |
| `platforms` | yes | Subset of `linux`, `macos` |
| `tier` | yes | `1` generate and reload, `2` install an upstream port, `3` manual instructions only |
| `detect.binaries` | one of | Names looked up on `PATH` |
| `detect.macos_apps` | one of | Bundle names under `/Applications` and `~/Applications` |
| `detect.packages` | one of | Package names per manager (`pacman`, `brew`, `dpkg`, `rpm`) |
| `detect.version` | no | `argv` to run and `regex` with one capture group. Result cached per binary path and modification time |
| `config.env` | no | Environment variable that overrides the config location |
| `config.file`, `config.dirs` | yes | Config file name and candidate directories per platform, first existing wins. `$XDG_CONFIG_HOME` defaults to `$HOME/.config` on both platforms |
| `integrations` | yes | Ordered list. The first entry whose `versions` range matches the installed version, and whose method the current owner allows, is used |
| `reload` | no | Commands run after apply. `when: running` runs only if the app has a running process |
| `validate` | no | Command run against the written config. Non-zero exit fails the apply and triggers rollback of that app |
| `ports` | no | Known upstream ports per palette family, with `kind: official` or `community` |
| `disabled` | no | User overrides only. `true` removes the app from detection and apply |

## Integration methods

| Method | What Theme Tender writes | Extra fields |
|---|---|---|
| `include` | A file it owns, plus one include line in the user's config | `template`, `owned_file`, `line`, `position` (`start` or `end`) |
| `owned` | A whole file in a theme directory the app reads (btop themes, yazi flavors, helix themes), plus a setting that selects it if needed | `template`, `target`, optional `select` (a `keys` edit) |
| `keys` | Specific keys in the user's config, keeping comments and formatting | `format` (`yaml`, `toml`, `json`, `jsonc`, `ini`), `template` (renders the key values), `paths` (key paths it may change) |
| `ansi` | Settings that make the app use the terminal's 16 colors | `format`, `set` (key/value map) |
| `port` | A clone of an upstream port into the app's plugin or theme directory | `url`, `target`, `select` |
| `manual` | Nothing. Shows instructions and a link | `url` |

`ansi` is listed first for apps where it looks acceptable, because the app then follows any terminal theme change with nothing to regenerate.

## Templates

Templates are Mustache files, compatible with the Base16 builder specification, so Tinted Theming templates can be reused. Variables available to every template:

- the 26 roles: `{{rosewater.hex}}`, `{{crust.rgb}}` … (`hex`, `hex_bare`, `rgb`, `r`, `g`, `b`)
- `{{ansi0.hex}}` … `{{ansi15.hex}}`, `{{background}}`, `{{foreground}}`, `{{cursor}}`, `{{selection_bg}}`, `{{selection_fg}}`
- Base16 and Base24 aliases `base00` … `base17`
- `{{theme.name}}`, `{{theme.slug}}`, `{{theme.appearance}}`

Whether Aether's template variables can be offered as aliases is an M1 research item.

## Command safety

- Commands are `argv` arrays. They never pass through a shell.
- The only substitutions are `{config}` (resolved config path) and `{theme_file}` (the owned file path).
- Built-in entries are trusted only inside a correctly signed bundle.
