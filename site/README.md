# Pult website

The landing page and user docs, as static HTML with no build step and no backend. The intended hostname is `pult.subcult.tv`. It isn't deployed yet.

## Preview

```sh
python -m http.server 18784 --bind 127.0.0.1 --directory site
```

Open `http://127.0.0.1:18784/` and `/docs/`.

## Keeping it accurate

Commands and flags in `docs/index.html` come from `cmd/pult/main.go`, and TUI keys from `internal/tui/tui.go`. Change the page when those change. The theme counts on both pages are rounded from `make catalog` output.

The palettes in `app.js` are `pult palette <id>` output for the themes in the demo list. The list is a browser mock-up of `pult tui`, not the app itself.

While no bundle under the new name is published, the docs tell people to skip `pult update`. Remove that note, and shorten the install steps, when the first Pult release ships.

## Brand

The marks, `assets/og.png` and the fonts come from `studio/brands/pult` and `studio/brand-tools/fonts` in `subcult-tv`. Fonts are subset to Latin WOFF2 and keep their OFL texts in `fonts/`.

Patreon links use `https://support.subcult.tv`, per `studio/content/patreon/support-redesign-2026-10/site-guidelines.md`: one support block on the landing page and a quiet footer link, no prices or tiers, and no ask on the error page.
