# Change record (draft v1)

Every file Theme Tender creates, edits or deletes is recorded before it is touched, so any change can be undone, even by hand if Theme Tender itself is broken.

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

[[change]]
app = "kitty"
path = "/home/u/.config/kitty/kitty.conf"         # path the app reads
resolved = "/home/u/dotfiles/kitty/kitty.conf"    # after following symlinks
owner = "stow"                                    # none | stow | chezmoi | omarchy | generator:<name> | framework:<name>
method = "include"
action = "edit"                                   # create | edit | delete
before = "sha256:9f2c..."                         # "absent" when the file did not exist
after = "sha256:41aa..."
mode = "0644"
backup = "blobs/sha256/9f/2c....zst"              # omitted when before = "absent"
detail = { line = "include theme-tender.conf", line_no = 88 }
```

## Write protocol

1. Take `lock`.
2. Write the transaction with `status = "pending"` and every planned change.
3. Store each `before` file as a blob, if that blob doesn't already exist.
4. Write each target file atomically: temporary file in the same directory, `fsync`, rename. Keep the original mode and owner.
5. Run the app's `validate` command. On failure, restore that app's files and mark those changes failed.
6. Set `status = "complete"` (or `failed`), update `current.toml`, release `lock`.

If Theme Tender stops part-way, the next run and `tender doctor` find the pending transaction and offer to finish or roll it back.

## Undo rules

For each change, newest first:

- Current file hash equals `after`: restore the `before` blob, or delete the file if `before = "absent"`.
- Current file hash differs from `after`: the user has edited the file since. Show a diff against `before` and change nothing without confirmation.
- File missing: report it and skip it.

`tender uninstall` undoes every transaction since the first one, which returns every file to its state before Theme Tender first touched it.

## Retention

The first backup of each path ("pristine") is kept until uninstall. Other blobs are pruned once no transaction among the last 20 refers to them. Blobs are only deleted after the transactions that refer to them.

## Restoring by hand

```bash
zstd -d < ~/.local/state/theme-tender/blobs/sha256/9f/2c....zst > ~/.config/kitty/kitty.conf
```

The transaction files list which blob belongs to which path.
