# ADR 0005: Rename Theme Tender to Pult

## Status

Accepted, 2026-11-07, by the owner during the brand review in SUBCULT Studio (`subcult-tv/studio/studies/theme-tender-directions/`).

## Context

The brand review settled on a broadcast and remote-control identity: applying a theme is changing the channel, undo is the previous-channel button, and one remote drives every app. The old name carried none of that. Version 0.1.0 had been published to the `subcult` pacman repository, but nobody had installed it, so a rename needed no migration for users.

## Decision

The product, repository, Go module, binary and packages are named Pult (`pult`). Pult is the Russian word for a TV remote. Tagline: "a universal remote for your colors."

| Before | After |
|---|---|
| `subculture-collective/theme-tender` (Gitea and GitHub mirror) | `subculture-collective/pult`; the old URLs redirect |
| Go module `git.subcult.tv/subculture-collective/theme-tender` | `git.subcult.tv/subculture-collective/pult` |
| `tender` command, `cmd/tender` | `pult`, `cmd/pult` |
| `TENDER_*` environment variables | `PULT_*` |
| `theme-tender` config, data and state directories and owned files such as `theme-tender.conf` | `pult` |
| Omarchy and framework theme names `tender-<id>` | `pult-<id>` |
| Packages `theme-tender`, `theme-tender-bin`, formula `theme-tender` | `pult`, `pult-bin`, formula `pult` |

ADRs 0001 to 0004 keep their original wording as a record of the decisions at the time.

## Consequences

- The Gitea Actions secrets keep their names (`TENDER_SIGNING_KEY`, `TENDER_SIGNING_PASSWORD`); the workflows map them to `PULT_*` variables. Renaming a secret would mean re-entering its value.
- The Gitea push mirror still targets `github.com/subculture-collective/theme-tender.git` and reaches the renamed repository through GitHub's redirect. Re-create the mirror with the new address if GitHub ever drops the redirect.
- `pult update` reads releases from the renamed repository. Bundles published before the rename still verify, but they name owned files `theme-tender.*` until a new bundle is tagged.
- The `subcult` pacman repository keeps the unused `theme-tender` 0.1.0 package until it is deleted; `pult` appears there with the next release tag.
