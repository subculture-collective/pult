# Change record (v1)

Every file Theme Tender creates, edits or deletes is recorded before it is touched, so any change can be undone, even by hand if Theme Tender itself is broken. `internal/changes` implements this contract.

## Location

`$XDG_STATE_HOME/theme-tender/` on Linux and macOS. `$XDG_STATE_HOME` defaults to `$HOME/.local/state`.

```text
theme-tender/
  lock                                   # held during apply, uninstall and rollback
  current.toml                           # active theme and files Theme Tender owns (rebuilt from transactions if lost)
  transactions/
    20261106T182205Z-01JC7Q....toml      # one file per operation, never rewritten after it completes
  blobs/
    sha256/ab/cdef0123....zst            # zstd-compressed file contents, named by the hash of the uncompressed bytes
```

## Transaction file

```toml
schema = 1
id = "01JC7Q8K9W2X3Y4Z5A6B7C8D9E"   # ULID
kind = "apply"                        # apply | uninstall | rollback
status = "complete"                   # pending | complete | failed | rolled-back
started = 2026-11-06T18:22:05Z
finished = 2026-11-06T18:22:06Z
theme = "catppuccin-mocha"
tender_version = "0.1.0"
bundle_version = "2026.11.06.1"
# rolls_back = "<id>"                 # kind = "rollback": the transaction undone
# error = "kitty: invalid color"      # validation errors, when status = "failed"

[[change]]
app = "kitty"
path = "/home/u/.config/kitty/kitty.conf"         # path the app reads
resolved = "/home/u/dotfiles/kitty/kitty.conf"    # after following symlinks
owner = "stow"                                    # none | symlink | stow | chezmoi | omarchy | generator:<name> | framework:<name>; see ownership.md
method = "include"
action = "edit"                                   # create | edit | delete
before = "sha256:9f2c..."                         # "absent" when the file did not exist
after = "sha256:41aa..."
mode = "0644"
backup = "blobs/sha256/9f/2c....zst"              # omitted when before = "absent"
# status = "failed"                               # validation failed and the file was restored
detail = { app_version = "0.49.2", integration = "include" }
```

`detail` records, for app changes, the app version detected at apply (`app_version`, omitted when unknown) and the integration used (`integration`: the method, plus its version range in parentheses if it has one). `tender doctor` compares them with what it detects now: an app whose version changed is reported, along with whether an apply would now choose a different integration, or, if not, whether the app's validator still accepts the current files. Omarchy changes record `previous_theme` instead.

## Write protocol

1. Take `lock`, an exclusive `flock`. The kernel releases it if Theme Tender dies, so there is no stale lock to clean up. A second run gets "another Theme Tender run holds the lock".
2. Write the transaction with `status = "pending"` and every planned change. An edit that would not change a file is left out.
3. Store each `before` file as a blob, if that blob doesn't already exist.
4. Write each target file atomically: temporary file in the same directory, `fsync`, rename, `fsync` of the directory. Keep the original mode and owner; if the owner cannot be kept, the write fails instead of changing it. A symlinked path is written at `resolved`, so the link stays a link.
5. Run the app's `validate` command. On failure, restore that app's files, mark those changes `status = "failed"` and the transaction `failed`; other apps keep their changes.
6. Set `status = "complete"` (or `failed`), update `current.toml`, release `lock`.

If Theme Tender stops part-way, the next run and `tender doctor` find the pending transaction. Recovery rolls it back: each file that holds its `after` contents gets its `before` contents again, a file that still holds `before` is left alone, and anything else is reported as edited. The transaction is then `rolled-back`. Finishing an interrupted apply means running the apply again, because new contents are not stored. Tests stop the protocol after every step (lock, pending record, each backup, each write, validation, completion, `current.toml`) and check that recovery restores every file byte for byte.

`current.toml` is rebuilt from the transactions on every read; the file on disk is a copy for people and other tools.

## Undo rules

For each change, newest first:

- Current file hash equals `after`: restore the `before` blob, or delete the file if `before = "absent"`.
- Current file hash differs from `after`: the user has edited the file since. Show a diff against `before` and change nothing without confirmation.
- File missing: report it and skip it.

Rollback applies these rules to one transaction. `tender uninstall` applies them per file, comparing the current contents with the last recorded `after` and restoring the pristine `before`, which returns every file to its state before Theme Tender first touched it. Rollback and uninstall are transactions themselves (`kind = "rollback"`, `"uninstall"`). Directories Theme Tender created are left in place.

## Retention

Pruning runs at the end of every run, under the lock, after the transaction and `current.toml` are written. A blob is kept while any of these refers to it:

- one of the 20 newest transactions,
- a pending transaction, which recovery needs,
- a file Theme Tender still has a change to, as its pristine backup.

Every other blob is deleted, along with emptied fan-out directories. A transaction older than the newest 20 can then no longer be rolled back on its own, but uninstall still works. Pruning is best effort: if it stops part-way, the blobs left over are ones it would have deleted anyway.

A file whose contents are back to pristine, after an uninstall or a rollback, has no change outstanding and is dropped from `current.toml`. The next change to it records the contents it finds then as pristine, so edits the user made in between are what the next uninstall restores.

## Restoring by hand

```bash
zstd -d < ~/.local/state/theme-tender/blobs/sha256/9f/2c....zst > ~/.config/kitty/kitty.conf
```

The transaction files list which blob belongs to which path.
