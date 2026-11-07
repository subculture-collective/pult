# Runbooks

Runbooks describe repeatable procedures that an operator or agent can follow safely.

Good runbooks include:

- Purpose
- Preconditions
- Commands
- Expected output
- Rollback or cleanup steps
- Verification

Add runbooks as procedures appear (releases, bundle signing, mirror setup).

- [bundle-signing.md](bundle-signing.md): create the release signing key, publish and install a data bundle.
- [release.md](release.md): tag a release, publish to the AUR and the Homebrew tap, check each channel.
- [upstream-watch.md](upstream-watch.md): the weekly report of upstream sources and app releases.
