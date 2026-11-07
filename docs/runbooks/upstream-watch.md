# Upstream watcher

`.gitea/workflows/upstream-watch.yml` runs `cmd/upstream-check` every Monday at 06:17 UTC and on manual dispatch. It keeps one open issue, **Upstream watcher report**, opened by `gitea-actions`, whose body it replaces on every run; an issue with that title opened by anyone else is ignored. It never changes the repository.

## What it reports

- **Catalog sources**: each source in `catalog/sources.json` whose upstream `HEAD` differs from the pinned `revision`. Re-pin it and rebuild the catalog (`make catalog`) when the change is wanted.
- **Apps**: for each registry entry, the version in its `# Checked on <host>, <app> <version>, <date>:` header comment against the newest release tag upstream (`git ls-remote --tags`; prereleases and non-version tags are ignored). A newer release means the entry's commands and paths should be checked against it and the header updated. Entries without that comment are listed as never checked. The release repository comes from the entry's `homepage` when that is a GitHub repository, otherwise from `appRepos` in `cmd/upstream-check/main.go`; a new app whose homepage is elsewhere needs a line there.

## Run it by hand

```bash
go run ./cmd/upstream-check            # print the report; needs git and network access
```

To refresh the issue now, dispatch the workflow from the Actions tab, or:

```bash
tea api --login subcult-agent -X POST \
  /repos/subculture-collective/theme-tender/actions/workflows/upstream-watch.yml/dispatches -f ref=main
```

To stop it, close the issue and disable the workflow in the repository's Actions settings; the next run would otherwise open a new issue.
