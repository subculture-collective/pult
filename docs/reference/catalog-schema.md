# Catalog schema (v1)

The catalog is a SQLite database built in CI from upstream theme sources. It ships zstd-compressed inside the signed data bundle together with the registry and templates.

## Sources

`cmd/catalog-build` reads the sources pinned in [`catalog/sources.json`](../../catalog/sources.json): repository URL, commit, commit date, and the paths it needs.

| Source ID | Repository | Input | Licence recorded |
|---|---|---|---|
| `catppuccin` | [catppuccin/palette](https://github.com/catppuccin/palette) | `palette.json`: 4 flavours, 26 roles, ANSI 0–15 | MIT |
| `catppuccin-ports` | [catppuccin/catppuccin](https://github.com/catppuccin/catppuccin) | `resources/ports.porcelain.json`: 457 ports | (ports only) |
| `dracula` | [dracula/draculatheme.com](https://github.com/dracula/draculatheme.com) | `content/spec.mdx`: Dracula and Alucard palettes and ANSI tables | MIT, the licence of Dracula's theme repositories |
| `tinted` | [tinted-theming/schemes](https://github.com/tinted-theming/schemes) | `base16/`, `base24/` YAML: 362 and 243 schemes; a scheme in both is read from `base24/` | MIT, the repository licence |
| `mbadolato` | [mbadolato/iTerm2-Color-Schemes](https://github.com/mbadolato/iTerm2-Color-Schemes) | `ghostty/`, generated for 907 of the 912 schemes; `licenses.yml` per scheme | per scheme; no entry means `unknown` |
| `gogh` | [Gogh-Co/Gogh](https://github.com/Gogh-Co/Gogh) | `themes/*.yml`: 1,251 themes | MIT OR Apache-2.0, the repository licence |

Counts were taken on 2026-11-06. The 2021 `PatrickFanella/Terminal-Color-Schemes` fork is not used. Dracula's ~500 port repositories are not imported yet: listing them needs the GitHub API, and M1 only needs the ports in the registry entries.

### Licences

- A theme is bundled with its colors when its licence is a redistributable SPDX ID (the allowlist in `internal/catalog/license.go`). An `OR` expression qualifies if any part does, an `AND` expression only if every part does.
- `unknown`, and any SPDX ID not on the allowlist, is listed without colors and downloaded from upstream on first use.
- `nonfree` is left out entirely.
- Copies: themes with identical terminal colors (`content_hash`) are compared. If any of them is `nonfree`, all are left out. An `unknown` theme makes copies of it in later sources (the table order above) `unknown` too, so Gogh's repository licence does not cover a scheme Gogh copied from mbadolato without a known licence. An earlier source keeps its own licence: the official Dracula stays MIT although mbadolato's copy is `unknown`.

## Bundle layout

```text
bundle-2026.11.06.1.tar.zst            # deterministic tar: manifest.toml first, then files by name
bundle-2026.11.06.1.tar.zst.minisig    # minisign signature (BLAKE2b-prehashed Ed25519)
  manifest.toml
  catalog.db
  registry/
  templates/
```

`manifest.toml` holds `schema = 1`, `bundle_version`, `catalog_schema`, `registry_schema`, the `[[source]]` revisions the catalog was built from, and `[files]`, the SHA-256 of every other file. `internal/bundle` builds, signs, verifies and reads bundles. A bundle is used only if its signature verifies against a key built into the binary, every file matches the manifest, and its `catalog_schema` and `registry_schema` are no newer than the binary reads (`bundle.CatalogSchema`, `bundle.RegistrySchema`; registry format 2 added `$MAC_CONFIG_HOME`); otherwise Theme Tender warns and uses the data built into the binary. Release builds embed the catalog there too (`builtin/`). Publishing is in [the bundle signing runbook](../runbooks/bundle-signing.md).

## Tables

```sql
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
-- schema_version, built_at (latest source commit date, UTC)

CREATE TABLE sources (
  id TEXT PRIMARY KEY,          -- 'catppuccin', 'catppuccin-ports', 'dracula', 'tinted', 'mbadolato', 'gogh'
  url TEXT NOT NULL,
  revision TEXT NOT NULL,       -- upstream commit
  fetched_at TEXT NOT NULL      -- commit date of revision, so builds are reproducible
);

CREATE TABLE themes (
  id TEXT PRIMARY KEY,          -- 'catppuccin-mocha'
  name TEXT NOT NULL,
  family TEXT,                  -- 'catppuccin'
  variant TEXT,                 -- 'mocha'
  author TEXT,
  source_id TEXT NOT NULL REFERENCES sources(id),
  source_path TEXT NOT NULL,
  license TEXT NOT NULL,        -- SPDX ID, 'unknown'
  bundled INTEGER NOT NULL,     -- 1 colors included, 0 fetch from upstream
  appearance TEXT NOT NULL,     -- 'dark' | 'light'
  content_hash TEXT NOT NULL    -- for deduplication across sources
);

CREATE TABLE colors (
  theme_id TEXT NOT NULL REFERENCES themes(id),
  role TEXT NOT NULL,           -- 26 roles, ansi0..ansi15, background, foreground, cursor, selection_bg, selection_fg
  hex TEXT NOT NULL,
  provenance TEXT NOT NULL,     -- 'official' (as the source gives it) | 'port' | 'base24' (mapped from Base16/Base24 slots) | 'derived'
  PRIMARY KEY (theme_id, role)
);

CREATE TABLE attributes (       -- computed in OKLCH by the builder
  theme_id TEXT NOT NULL REFERENCES themes(id),
  key TEXT NOT NULL,            -- see Attributes below
  value REAL NOT NULL,
  PRIMARY KEY (theme_id, key)
);

CREATE TABLE tags (
  theme_id TEXT NOT NULL REFERENCES themes(id),
  tag TEXT NOT NULL,            -- 'dark', 'pastel', 'neon', 'warm', 'high-contrast', ...
  origin TEXT NOT NULL,         -- 'computed' | 'curated'
  PRIMARY KEY (theme_id, tag)
);

CREATE TABLE relations (
  theme_id TEXT NOT NULL REFERENCES themes(id),
  other_id TEXT NOT NULL REFERENCES themes(id),
  kind TEXT NOT NULL,           -- 'light-dark' | 'variant' | 'duplicate'
  PRIMARY KEY (theme_id, other_id, kind)
);

CREATE TABLE vectors (          -- OKLab L, a, b of background, foreground, ansi0..ansi15, little-endian float32 (216 bytes)
  theme_id TEXT PRIMARY KEY REFERENCES themes(id),
  oklab BLOB NOT NULL
);

CREATE TABLE ports (
  family TEXT NOT NULL,         -- 'catppuccin', 'dracula'
  app_id TEXT NOT NULL,         -- registry id
  url TEXT NOT NULL,
  kind TEXT NOT NULL,           -- 'official' | 'community'
  PRIMARY KEY (family, app_id, url)
);

CREATE VIRTUAL TABLE themes_fts USING fts5(name, family, author, tags, content='');
```

Similarity search computes distances over `vectors` in Go. A few thousand themes is small enough to scan without an index.

## Build rules

- **IDs.** The slug of the theme name (`+` becomes `plus`); Catppuccin flavours are `catppuccin-<flavour>`. If an earlier source already took the ID, the source ID is appended (`gruvbox-dark-gogh`), then a number.
- **Colors.** Bundled themes get all 47 roles: what the source gives, then the 26-role derivation below for the rest. Base16 schemes map to ANSI as base16-shell does (brights repeat the normal colors); Base24 schemes use the Base24 styling guide. Catppuccin's official cursor is rosewater.
- **Appearance.** `light` if the background's OKLab lightness is at least 0.5, else `dark`.
- **Relations.** `duplicate` points a copy at the first theme with the same `content_hash`. Within one source, themes whose names differ only by the words "dark" and "light" form a family (`solarized` from "Solarized Dark"); every pair in a family is a `variant`, and pairs of different appearance are also `light-dark`. Catppuccin and Dracula are one family each.
- **Unknown-licence themes** have a `themes` row, appearance tag and FTS row, but no colors, attributes or vector.
- **Reproducibility.** Rows are inserted in sorted order, the database is vacuumed, and zstd runs single-threaded at a fixed level. Attributes are rounded to six decimals. Two builds from the same source revisions produce the same file on the same architecture; on 2026-11-06 the pinned sources gave sha256 `c05372aa5f627d8875d6be499babe73abe30afa909b3557f9d520b356e1c4cc3` (2,671 themes: 1,810 bundled, 861 listed, 28 nonfree left out).

Run `make catalog` to fetch the pinned sources into `.cache/catalog-src` and build `.cache/catalog.db.zst`.

### Attributes

Computed in OKLCH on the full palette. "Accents" are ANSI 1–6 and 9–14.

| Key | Meaning |
|---|---|
| `bg_lightness` | OKLab lightness of the background |
| `text_contrast` | WCAG contrast of foreground on background |
| `min_contrast` | Lowest WCAG contrast of the foreground or any accent on the background |
| `mean_chroma` | Mean chroma of the accents |
| `mean_accent_lightness` | Mean lightness of the accents |
| `warmth` | Mean of C·cos(h − 60°) over background, foreground and ANSI 0, 7, 8, 15: positive leans orange, negative blue |
| `dominant_hue` | Hue of the summed OKLab a/b of the 18 terminal colors |

### Computed tags

First thresholds, to be tuned against the TUI:

| Tag | Rule |
|---|---|
| `dark`, `light` | Appearance |
| `high-contrast` / `low-contrast` | `text_contrast` ≥ 12 / < 7 |
| `neon` | `mean_chroma` ≥ 0.20 |
| `muted` | `mean_chroma` < 0.08 |
| `pastel` | 0.08 ≤ `mean_chroma` < 0.14 and `mean_accent_lightness` ≥ 0.75 |
| `warm` / `cool` | `warmth` ≥ 0.01 / ≤ −0.01 |

## 26-role derivation

Used only when no official palette, port or Base24 scheme supplies a role. Implemented in `internal/color` (`Derive`). Input: ANSI 0–15, background and foreground. Roles the input already has are kept.

1. Convert the ANSI 16 colors, background and foreground to OKLCH. `base` defaults to background and `text` to foreground.
2. Surfaces and text (`crust` … `text`): place each role on the lightness axis at a fixed fraction of the distance from `base` to `text`. `mantle` and `crust` are darker than `base` in light and dark palettes alike, so they use a fraction of that distance below `base`. Chroma and hue are interpolated between `base` and `text`; `mantle` and `crust` keep `base`'s chroma and hue.
3. Accents with an ANSI source (`red`, `green`, `yellow`, `blue`, `teal` from cyan, `pink` from magenta) take the normal or bright ANSI color, whichever contrasts better with `base`. The bright color counts only if it is within 30° of the normal hue and has at least half its chroma; Solarized, for one, puts grays in the bright slots. Catppuccin's own ANSI mapping sends magenta to `pink`, not `mauve`.
4. Remaining accents (`rosewater`, `flamingo`, `maroon`, `peach` between red and yellow; `sky`, `sapphire` between teal and blue; `lavender`, `mauve` between blue and pink) take a hue at a fixed fraction of the shorter arc between their two neighbors. Lightness and chroma are the median of the six ANSI-sourced accents, times a per-role factor.
5. Move a step-4 accent away from `base` in lightness until its WCAG contrast with `base` reaches a floor: the weakest ANSI-sourced accent's contrast, capped at 3:1.
6. Terminal specials: `cursor` and `selection_fg` default to foreground, `selection_bg` to `surface2`.

The fractions and factors in steps 2 and 4 are averages measured from Catppuccin's four flavours. A test recomputes them from the vendored palette, so the constants can't drift from their source.

### Test oracle and threshold

Feed each Catppuccin flavour's ANSI colors, `base` and `text` through the derivation and compare the 26 roles with the official values. ΔE is the Euclidean distance in OKLab on a 0–1 scale; about 0.02 is a just-noticeable difference. To keep a flavour from being graded on its own data, each flavour is derived with constants fitted on the other three (leave-one-out).

First measurement, palette 1.8.0 (`07d02aa`):

| Derivation | Mean ΔE | Max ΔE |
|---|---|---|
| As implemented, leave-one-out | 0.0155 | 0.1057 (Latte `peach`) |
| As implemented, fitted on all four | 0.0118 | 0.0800 (Latte) |
| First draft of this section (magenta to `mauve`, midpoint hues, median lightness and chroma), fitted surface steps | 0.0276 | 0.2384 (Latte) |

Latte is the only light flavour, so its leave-one-out constants come from dark flavours alone; it has the largest errors. The step-5 floor changes no Catppuccin role.

Acceptance threshold for M1 criterion 6, approved by the owner on 2026-11-06 and checked by `TestCatppuccinDerivation`: leave-one-out mean ΔE ≤ 0.020 and max ΔE ≤ 0.110.
