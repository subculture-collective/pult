# Bundle signing key and data releases

Theme Tender accepts a downloaded data bundle only if it carries a minisign signature from a key listed in `internal/bundle/keys.go`. That list is empty until the owner creates the key, so until then every downloaded bundle is ignored and the built-in data is used (ADR 0002).

## Create the key (owner, once)

On a trusted machine with [minisign](https://jedisct1.github.io/minisign/):

```bash
minisign -G -p theme-tender.pub -s theme-tender.key
```

Choose a passphrase when asked. Then:

1. Store `theme-tender.key` and its passphrase in the secret store, with an offline copy.
2. In Gitea, open `subculture-collective/theme-tender` → Settings → Actions → Secrets and add:
   - `TENDER_SIGNING_KEY`: the full contents of `theme-tender.key`;
   - `TENDER_SIGNING_PASSWORD`: the passphrase.
3. Open a PR that adds the second line of `theme-tender.pub` (the base64 line, not the comment) to `TrustedKeys` in `internal/bundle/keys.go`.

Don't commit `theme-tender.key`.

## Publish a bundle

```bash
git tag bundle-2026.11.07.1
git push origin bundle-2026.11.07.1
```

The `bundle-release` workflow runs `make verify` and `make catalog`, builds `dist/bundle-<version>.tar.zst` with `cmd/bundle-build`, signs it, and attaches the bundle and its `.minisig` to a release for the tag. The workflow has not run yet; it needs the secrets above.

To check a bundle by hand: `minisign -V -p theme-tender.pub -m bundle-<version>.tar.zst`.

## Install a bundle

Until `tender update` exists, copy both files to `$XDG_DATA_HOME/theme-tender/bundle.tar.zst` and `bundle.tar.zst.minisig`. `tender doctor` shows which bundle is in use.

## Rotate the key

Add the new public key to `TrustedKeys` next to the old one and release a binary. Switch the Gitea secrets to the new key, publish a bundle, and remove the old key in a later release.
