# ADR 0004: Hyprland framework backends

## Status

Accepted, 2026-11-07. Picks up ADR 0002's "full backends come later, one at a time" (#42); the owner asked for them and left the test setup to the agent.

## Context

Each of the six frameworks Theme Tender detects themes itself in its own way. The research (sources and commits in [framework-backends.md](../reference/framework-backends.md)) found:

| Framework | What it accepts from outside |
|---|---|
| Noctalia v5 | A custom palette file with all terminal roles, switched with `noctalia msg color-scheme-set` |
| DankMaterialShell (Dux Modular Shell) | A custom theme file, switched over IPC; terminal ANSI colors are always derived from it |
| HyDE | A theme directory, switched with `hydectl theme set`; needs a wallpaper and a real GTK theme name |
| Caelestia | Only its packaged schemes |
| end-4 (illogical-impulse) | One accent color, through its own script, inside a Hyprland session |
| ML4W | Nothing: colors only come from matugen on a wallpaper, and every login regenerates them |

## Decision

- A backend never edits files the framework generates. It writes files Theme Tender owns, where the framework has a place for them, and switches with the framework's own command, after the files are written through the change record.
- Before its first switch, Theme Tender records the framework's selection (`$XDG_STATE_HOME/theme-tender/frameworks/<name>.json`, itself in the change record). Uninstall switches back first, and only if Theme Tender's selection is still showing, then removes the files.
- While a backend is active, app files the framework owns are left to it ("<framework> themes it") instead of asking.
- A framework that cannot be reached (shell not running, not in a session) is left alone with a note; apply goes on for everything else.
- Backends: Noctalia, DankMaterialShell, HyDE, Caelestia (catalog themes it packages; others are left alone with a note), end-4 (the palette's accent). ML4W has none: it stays detect-and-ask, because anything written into its outputs is overwritten at the next login.

## Consequences

- Two backends are partial by the frameworks' design: DankMaterialShell's terminals do not get the palette's ANSI colors, and end-4 gets only the accent. Theme Tender says so in the plan.
- HyDE's switch rewrites GTK, Qt and Xresources settings and links GTK themes, as it does for any HyDE theme. Theme Tender's HyDE theme copies the GTK and icon themes from the theme in use, because an empty GTK theme name would make HyDE's switch delete `~/.themes`.
- Noctalia, DankMaterialShell and Caelestia were run in Arch containers; HyDE and end-4 need a Hyprland session, so they are checked by unit tests against formats read from their source only.
