# Catalog builder fixtures

One directory per source ID, laid out like the upstream checkout.

- `catppuccin/palette.json`: the Mocha and Latte flavours from catppuccin/palette 1.8.0 (MIT; licence in `internal/color/testdata/catppuccin/LICENSE`).
- `dracula/content/spec.mdx`: a cut-down copy of the layout of Dracula's spec, with Dracula's published colors.
- `mbadolato/ghostty/Dracula`: Dracula's colors again, to test that an `unknown` copy leaves the original alone.
- Everything else is made up for the tests: licence cases (`Free One`, `Unknown One`, `Nonfree One`, `Unlisted One`), copies across sources, and Base16/Base24 mapping.
