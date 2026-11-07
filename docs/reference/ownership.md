# Config ownership (v1)

Each config file has one owner. `internal/owner` finds it, and Theme Tender acts on it: write the file, ask first, or hand off to the owner's own theme mechanism. Policy is in [ADR 0002](../adr/0002-theme-tender-foundations.md); the owner is stored in each change-record entry.

## Owners

Checked in this order; the first that matches is the owner.

| Owner | Record value | Detected when | Action |
|---|---|---|---|
| Omarchy | `omarchy` | Omarchy is installed (`~/.local/share/omarchy`, `/usr/share/omarchy` or `$OMARCHY_PATH`) and the file lies in, or refers to, the install directory, `~/.config/omarchy` or `$XDG_STATE_HOME/omarchy`; or a hook in `~/.config/omarchy/hooks/theme-set.d/` names the file; or its first ten lines say it is generated from Omarchy | Hand off to the Omarchy backend |
| Hyprland framework | `framework:<name>` | The framework's marker directory exists and the file lies in, or refers to, one of its directories | Ask |
| Palette generator | `generator:matugen`, `generator:wallust`, `generator:pywal` | The file is a matugen `output_path` or wallust `target`, refers to one, or refers to `~/.cache/wal/` | Ask |
| chezmoi | `chezmoi` | `chezmoi source-path <file>` succeeds | Ask; the source is `Owner.Source` |
| stow | `stow` | A symlinked prefix of the path points at `<stow dir>/<package>/<same path relative to home>`, as stow lays it out | Ask; writing changes the dotfiles repository |
| Other symlink | `symlink` | Any other symlink on the path | Ask |
| Nobody | `none` | None of the above | Write |

"Refers to" means the file's text contains the path written absolute, with `~/`, `$HOME/` or `${HOME}/`, or, for a file in the same directory, its bare name (`include matugen.conf`). Files over 1 MiB are not read.

A framework detected on the machine but not linked to a particular file is reported by `System()`, so the apply pipeline can ask once before changing anything.

## Framework markers

| Name | Directories | Source |
|---|---|---|
| `caelestia` | `~/.config/caelestia`, `~/.config/quickshell/caelestia`, `$XDG_STATE_HOME/caelestia` | caelestia-dots/shell README |
| `dms` | `~/.config/DankMaterialShell`, `~/.config/dms`, `~/.config/quickshell/dms` | DankMaterialShell README and `SettingsData.qml` |
| `end-4` | `~/.config/quickshell/ii`, `~/.config/illogical-impulse` | end-4/dots-hyprland `dots/.config/quickshell/ii` |
| `hyde` | `~/.config/hyde`, `$XDG_STATE_HOME/hyde`, `$XDG_CACHE_HOME/hyde` | HyDE `Configs/.config/hyde` |
| `ml4w` | `~/.config/ml4w`, `~/.config/ml4w-dotfiles-settings` | mylinuxforwork/dotfiles `dotfiles/.config/ml4w` |
| `noctalia` | `~/.config/noctalia`, `~/.config/quickshell/noctalia-shell` | Earlier Noctalia releases; not confirmed for the current C++ rewrite |

Checked on 2026-11-06. `~/.config` means `$XDG_CONFIG_HOME`.

## Omarchy theme

`System().OmarchyTheme` is read from `$XDG_STATE_HOME/omarchy/current/theme.name`, or for older releases from the target of the `~/.config/omarchy/current/theme` symlink.
