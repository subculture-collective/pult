# minisign 0.12 compatibility fixture

Generated with the minisign 0.12 C tool in an `archlinux` container on 2026-11-07:

```bash
minisign -G -p t.pub -s t.key      # passphrase: testpass
minisign -S -s t.key -m data       # writes data.minisig
```

This is a test key with a published passphrase. It is not trusted anywhere and must never sign a release.
