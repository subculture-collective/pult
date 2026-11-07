# Bundle signing key and data releases

Theme Tender accepts a downloaded data bundle only if it carries a minisign signature from a key listed in `internal/bundle/keys.go`. That list is empty until the owner creates the key, so until then every downloaded bundle is ignored and the built-in data is used (ADR 0002).

## Create the key (owner, once)

On a trusted machine with [minisign](https://jedisct1.github.io/minisign/):

```bash
minisign -G -p theme-tender.pub -s theme-tender.key
```

Choose a passphrase when asked. Then:

1. Store `theme-tender.key` and its passphrase in the secret store, with an offline copy.
2. In Gitea, open `subculture-collective/theme-tender` → Settings → Actions → Secrets and add `TENDER_SIGNING_KEY`. Use a passphrase-free copy of the key: the key and its passphrase would sit in the same secret store anyway, and pasted passphrases are easy to get subtly wrong. The offline copy keeps its passphrase.

   ```bash
   cp theme-tender.key theme-tender-ci.key
   minisign -C -s theme-tender-ci.key -W      # asks for the passphrase, removes it from the copy
   cat theme-tender-ci.key                    # paste both lines into TENDER_SIGNING_KEY, then delete the copy
   ```

   With an encrypted key instead, also add `TENDER_SIGNING_PASSWORD`. A trailing newline is ignored; any other difference fails with "Wrong password".
3. Open a PR that adds the second line of `theme-tender.pub` (the base64 line, not the comment) to `TrustedKeys` in `internal/bundle/keys.go`.

Don't commit `theme-tender.key`.

## Publish a bundle

```bash
git tag bundle-2026.11.07.1
git push origin bundle-2026.11.07.1
```

The `bundle-release` workflow runs `make verify` and `make catalog`, builds `dist/bundle-<version>.tar.zst` with `cmd/bundle-build`, signs it, and attaches the bundle and its `.minisig` to a release for the tag.

If signing fails, the run's error says why without showing the key: "expected one base64 line" or "not valid base64" means `TENDER_SIGNING_KEY` is incomplete or has extra text; "this is a public key" means the wrong file was pasted; "Wrong password" means the passphrase does not open the key.

To check a bundle by hand: `minisign -Vm bundle-<version>.tar.zst -P RWTsCn09xaMX6t72P+btoUcYSx43x4CV1lcejI8UxgdZRfVEmwzEvcP7`.

The first signed bundle, `bundle-2026.11.07.6`, was published on 2026-11-07 (2,487,430 bytes). A source build of `tender` from `main`, which has no built-in catalog, accepted it from `$XDG_DATA_HOME/theme-tender/bundle.tar.zst` and searched its catalog. The same file with one byte changed was rejected ("Invalid signature") and the built-in data was used. Tags `bundle-2026.11.07.1` to `.5` are failed runs from setting up the secrets and have no release.

## Install a bundle

Until `tender update` exists, copy both files to `$XDG_DATA_HOME/theme-tender/bundle.tar.zst` and `bundle.tar.zst.minisig`. `tender doctor` shows which bundle is in use.

## Rotate the key

Add the new public key to `TrustedKeys` next to the old one and release a binary. Switch the Gitea secrets to the new key, publish a bundle, and remove the old key in a later release.
