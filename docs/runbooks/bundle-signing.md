# Bundle signing key and data releases

Pult accepts a downloaded data bundle only if it carries a minisign signature from a key listed in `internal/bundle/keys.go`. That list is empty until the owner creates the key, so until then every downloaded bundle is ignored and the built-in data is used (ADR 0002).

## Create the key (owner, once)

On a trusted machine with [minisign](https://jedisct1.github.io/minisign/):

```bash
minisign -G -p pult.pub -s pult.key
```

Choose a passphrase when asked. Then:

1. Store `pult.key` and its passphrase in the secret store, with an offline copy.
2. In Gitea, open `subculture-collective/pult` → Settings → Actions → Secrets and add `PULT_SIGNING_KEY`. Use a passphrase-free copy of the key: the key and its passphrase would sit in the same secret store anyway, and pasted passphrases are easy to get subtly wrong. The offline copy keeps its passphrase.

   ```bash
   cp pult.key pult-ci.key
   minisign -C -s pult-ci.key -W      # asks for the passphrase, removes it from the copy
   cat pult-ci.key                    # paste both lines into PULT_SIGNING_KEY, then delete the copy
   ```

   With an encrypted key instead, also add `PULT_SIGNING_PASSWORD`. A trailing newline is ignored; any other difference fails with "Wrong password".
3. Open a PR that adds the second line of `pult.pub` (the base64 line, not the comment) to `TrustedKeys` in `internal/bundle/keys.go`.

Don't commit `pult.key`.

## Publish a bundle

```bash
git tag bundle-2026.11.07.1
git push origin bundle-2026.11.07.1
```

The `bundle-release` workflow first refuses a tag whose commit is not on `main`, whose version is not `YYYY.MM.DD.N`, or whose version is not newer than every published bundle (a bad version such as `bundle-2027` would otherwise stop every client from updating). It then runs `make verify` and `make catalog`, builds `dist/bundle-<version>.tar.zst` with `cmd/bundle-build`, signs it, and attaches the bundle and its `.minisig` to a release for the tag.

**Tags are protected.** The workflow hands the signing key to the code at the tagged commit, and the "on `main`" check lives in that same commit, so on its own it only guards against mistakes. Since 2026-11-07 Gitea tag protection allows only `onnwee` to push `bundle-*` and `v*` tags (repository Settings → Tags); a test push of a protected pattern as `subcult-agent` was rejected. Gitea only lists accounts with direct write access, so letting another account release means adding it as a collaborator with write access first, then to both rules.

If signing fails, the run's error says why without showing the key: "expected one base64 line" or "not valid base64" means `PULT_SIGNING_KEY` is incomplete or has extra text; "this is a public key" means the wrong file was pasted; "Wrong password" means the passphrase does not open the key.

To check a bundle by hand: `minisign -Vm bundle-<version>.tar.zst -P RWRhwce/U8MJcs1Nyku+9LlhMz8v6uqq38+IAObG19bBMF9mxMTJ+qdg`.

The first signed bundle, `bundle-2026.11.07.6`, was published on 2026-11-07 (2,487,430 bytes). A source build of `pult` from `main`, which has no built-in catalog, accepted it from `$XDG_DATA_HOME/pult/bundle.tar.zst` and searched its catalog. The same file with one byte changed was rejected ("Invalid signature") and the built-in data was used. Tags `bundle-2026.11.07.1` to `.5` are failed runs from setting up the secrets and have no release.

## Install a bundle

Release binaries carry a built-in data version, the tagged commit's date as `YYYY.MM.DD.0` (`pult.BuiltinVersion`, set with `-ldflags` by `scripts/dist.sh` and the AUR and Homebrew builds). An installed bundle older than that is not used, so upgrading the binary cannot be undone by an old bundle; source builds report `built-in`, older than any bundle.

`pult update` reads the release list (`-url`, default the Gitea API for this repository), picks the newest `bundle-*` release that has both assets, and installs it to `$XDG_DATA_HOME/pult/bundle.tar.zst` and `bundle.tar.zst.minisig` (or the `-bundle` path). It installs nothing unless the signature verifies with a built-in key, every file matches the manifest, the manifest's version matches the tag, and the release is newer than the bundle in use. The repository has been public since 2026-11-07; that day `pult update` installed `bundle-2026.11.07.6` from the default URL into an empty home without a login. `pult doctor` shows which bundle is in use.

## Rotation on 2026-11-13

The Gitea runner printed the multi-line `TENDER_SIGNING_KEY` secret and its passwords into public run logs between 2026-11-06 and 2026-11-12, so that key is treated as exposed. It was replaced by a new unencrypted key stored only as the single-line secret `PULT_SIGNING_KEY_B64` (base64 of the key file, which the runner masks); the workflow decodes it. The exposed key was removed from `TrustedKeys` at the same time instead of in a later release, so binaries from this release on accept only bundles signed with the new key and fall back to their built-in data until one is published. The logs that held the old key were deleted.

To set the secret: `base64 -w0 pult.key` and paste the single line into `PULT_SIGNING_KEY_B64`. Never put the multi-line key file itself into a secret that a step reads through `env:`.

## Rotate the key

Add the new public key to `TrustedKeys` next to the old one and release a binary. Switch the Gitea secrets to the new key, publish a bundle, and remove the old key in a later release.
