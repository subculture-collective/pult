# ADR 0002: Theme Tender foundations

## Status

Accepted, 2026-11-06. Settled with the owner in a scope interview. The interview notes are kept outside this repository.

## Context

Theme Tender applies one palette across many apps without touching their other settings, and can undo every change. Several tools already cover parts of this: Aether and Omarchy themes (GUI, Omarchy only), tinty and Tinted Theming (CLI, no discovery or rollback), Stylix (Nix only), and matugen, wallust and pywal16 (palette generators). None of them combines app discovery, safe edits, rollback, a TUI and a large catalog on Linux and macOS.

## Decision

| Area | Decision |
|---|---|
| Product | Standalone tool, compatible with Omarchy. The Subarchy installer will call `tender apply` with a profile |
| Platforms | Linux and macOS. No Windows |
| Language | Go with Bubble Tea. Pure-Go SQLite driver, no cgo |
| Hosting | Gitea `subculture-collective/theme-tender` is primary. A push mirror publishes to GitHub `subculture-collective/theme-tender` once the repository is public |
| Contributions | GitHub issues and PRs are accepted. Accepted changes land on Gitea main with author credit and reach GitHub through the mirror |
| CI | Gitea Actions Linux jobs are the required check. Soyuz runs macOS jobs as a Gitea act_runner; those jobs are advisory and never block a merge |
| Licence | Go code GPL-3.0-only. Templates (`templates/`) and the app registry (`registry/`) are MIT, so generated config files belong to the user and templates can be shared with Catppuccin, Dracula and Tinted Theming |
| Config ownership | Each config file has one owner. Theme Tender hands off to, writes through, or asks before working around Omarchy, stow and other symlink setups, chezmoi, matugen/wallust/pywal hooks and Hyprland frameworks |
| Omarchy | Full backend: Theme Tender writes a theme directory under `~/.config/omarchy/themes/<name>` and switches with `omarchy-theme-set`. It never edits files Omarchy owns |
| Hyprland frameworks | HyDE, ML4W, end-4, Caelestia, DankMaterialShell and Noctalia are detected in v1. Theme Tender marks their files and passes the palette to their generator or asks. Full backends come later, one at a time |
| Storage | Catalog: SQLite with FTS5, built in CI and shipped zstd-compressed. User state: plain-text transaction files plus zstd backups named by content hash |
| Color model | 26 roles using Catppuccin's names, plus ANSI 0–15 and terminal specials. Lookup order: official palette, community port, Base24 scheme, then derivation in OKLCH |
| Catalog licensing | Bundle themes with known redistributable licences. Download unknown-licence themes from upstream on the user's machine. Never redistribute themes marked nonfree |
| Registry updates | The registry, templates and catalog ship as one data bundle, separate from the binary and signed with minisign (Ed25519). The public key is built into the binary. The private key is a Gitea Actions secret for the release job, with an offline copy in the owner's secret store |
| Distribution | Linux: release tarballs and AUR (`theme-tender`, `theme-tender-bin`). macOS: a Homebrew tap (`subculture-collective/homebrew-tap`, mirrored to GitHub) whose formula builds from source, so no Apple Developer ID or notarization is needed. Prebuilt notarized macOS binaries only if demand justifies the cost |

## Consequences

- Role IDs use Catppuccin's names (`rosewater` … `crust`). They are widely known among people who theme their setups, and Catppuccin's official ports map onto them directly. Other palettes are mapped onto the same roles.
- Registry entries contain commands Theme Tender runs (reload, validate, version). Commands are argument arrays, never shell strings, and an unsigned or wrongly signed bundle is rejected. A user's local registry overrides are trusted because the user wrote them.
- macOS regressions can reach main while Soyuz is offline. The advisory job reports them afterwards, and the release checklist requires a green macOS run before tagging.
- Building from source on Homebrew means users wait for a Go build and need Go as a build dependency. Homebrew handles this automatically.
- Writing into a stow or chezmoi source changes the user's dotfiles repository. Theme Tender always asks before doing so.
