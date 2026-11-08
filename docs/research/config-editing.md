# Editing config files without losing comments

Spike for M1 work package 6, 2026-11-06. Question: how does Pult change a few keys in a user's TOML or YAML config and leave every other byte as it was?

## Options

1. **A parser that round-trips comments.** Parse, change the tree, serialize. Go has no maintained TOML library that writes comments back: `pelletier/go-toml/v2` and `BurntSushi/toml` drop them on encode. `go.yaml.in/yaml/v3` keeps most comments through its `Node` API, but re-encoding normalises indentation, quoting and blank lines, so untouched lines still change.
2. **Targeted text edits at parser-reported positions.** Parse only to find where a key's value, a table or a block starts and ends, then splice new text into the original bytes. Nothing outside the splice is rewritten.

## Findings

- `go-toml/v2`'s `unstable` parser reports byte ranges. A key/value expression covers `key = value` and excludes any trailing comment. Scalar values and every key part have their own range. Table headers, arrays and inline tables have none, but their key parts and the enclosing key/value locate them. Node handles are reused on each `NextExpression`, so anything needed later (array elements) must be copied while the expression is current; the first prototype crashed on exactly that.
- yaml.v3 nodes carry line and column but no end position. A block's end is the first later non-blank line indented at or left of the key, with one exception: a block sequence may start at the key's own column.
- Edits that only splice text keep comments, quoting, blank lines and key order by construction. Each edit is checked afterwards by parsing the result: the TOML must still decode, and the YAML value at the path must equal the fragment's.

## Decision

Use targeted text edits (option 2), in `internal/configedit`:

| Function | Used by | What it changes |
|---|---|---|
| `IncludeLine` | `include` with `line` (kitty, ghostty, tmux) | Adds one line at the start or end, unless it is already there |
| `TOMLAppend` | `include` with `key` (Alacritty `general.import`) | Adds one string to an array |
| `TOMLSet` | `select`, `ansi` in TOML (yazi `flavor.dark`) | Scalar values in place; new keys next to their siblings, at the end of their table, or in a new table |
| `TOMLKeys` | `keys` in TOML (starship) | Replaces a whole owned table (`[palettes.pult]`) and sets keys (`palette`) from the rendered template |
| `YAMLKeys` | `keys` in YAML (lazygit `gui.theme`) | Replaces the key's block, or adds it under the nearest parent, re-indented to the file's own step |
| `INISet` | `select`, `ansi` in INI (btop `color_theme`) | `key = value` lines, keeping spacing and quoting |

## Limits

These are refused with an error that asks the user to edit by hand, rather than guessed at:

- a TOML key inside an inline table, or a path defined both as a table and as dotted keys elsewhere;
- arrays with nested arrays or inline tables when appending;
- flow-style YAML mappings on the path, or a scalar where a mapping is needed.

JSON and JSONC have no editor yet; no M1 app needs one.

## Evidence

`internal/configedit` tests use exact expected outputs. Two round-trip tests use real files: starship's `catppuccin-powerline` preset and lazygit's full default config. Only the edited lines differ, and a second apply changes nothing.
