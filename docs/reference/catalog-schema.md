# Catalog schema (draft v1)

The catalog is a SQLite database built in CI from upstream theme sources. It ships zstd-compressed inside the signed data bundle together with the registry and templates.

## Sources

| Source | What it provides | Machine-readable input |
|---|---|---|
| [mbadolato/iTerm2-Color-Schemes](https://github.com/mbadolato/iTerm2-Color-Schemes) | ~900 terminal schemes | `yaml/`, licences in `licenses.yml` |
| [tinted-theming/schemes](https://github.com/tinted-theming/schemes) | 362 Base16, 243 Base24 | `base16/`, `base24/` YAML |
| [catppuccin](https://github.com/catppuccin/catppuccin) | Full 26-color palettes, 457 ports | `catppuccin/palette` JSON, `resources/ports.porcelain.json` |
| [Dracula](https://github.com/dracula) | Palette and ~500 port repositories | Palette spec, organization repo list |
| [Gogh](https://github.com/Gogh-Co/Gogh) | 1,000+ terminal themes | `themes/` |

Counts were taken on 2026-11-06. The 2021 `PatrickFanella/Terminal-Color-Schemes` fork is not used.

Licence handling: a theme whose licence is a known redistributable SPDX ID is bundled. `unknown` themes are listed with their colors left out and downloaded from upstream on first use. `nonfree` themes are left out entirely.

## Bundle layout

```text
bundle-2026.11.06.1.tar.zst
bundle-2026.11.06.1.tar.zst.minisig
  manifest.toml        # bundle version, schema versions, source revisions, file hashes
  catalog.db
  registry/
  templates/
```

## Tables

```sql
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
-- schema_version, built_at, bundle_version

CREATE TABLE sources (
  id TEXT PRIMARY KEY,          -- 'mbadolato', 'tinted', 'catppuccin', 'dracula', 'gogh'
  url TEXT NOT NULL,
  revision TEXT NOT NULL,       -- upstream commit
  fetched_at TEXT NOT NULL
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
  provenance TEXT NOT NULL,     -- 'official' | 'port' | 'base24' | 'derived'
  PRIMARY KEY (theme_id, role)
);

CREATE TABLE attributes (       -- computed in OKLCH by the builder
  theme_id TEXT NOT NULL REFERENCES themes(id),
  key TEXT NOT NULL,            -- bg_lightness, min_contrast, mean_chroma, warmth, dominant_hue
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

CREATE TABLE vectors (          -- OKLab values of the role colors in fixed order, float32, for similarity
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

## 26-role derivation

Used only when no official palette, port or Base24 scheme supplies a role.

1. Convert the ANSI 16 colors, background and foreground to OKLCH.
2. Surfaces and text (`crust` … `text`): step lightness from background to foreground, using the step ratios measured from Catppuccin's four flavours. Chroma and hue are interpolated between background and foreground.
3. Accents with an ANSI source (`red`, `green`, `yellow`, `blue`, `teal` from cyan, `mauve` from magenta) come from the normal or bright ANSI color, whichever contrasts better with `base`.
4. Remaining accents (`rosewater`, `flamingo`, `pink`, `maroon`, `peach`, `sky`, `sapphire`, `lavender`) interpolate hue between their nearest ANSI neighbors and use the palette's median accent lightness and chroma.
5. Raise lightness where needed to reach the minimum contrast against `base`.

Test oracle: feed each Catppuccin flavour's ANSI colors through the derivation and compare the result with the official roles (mean and maximum ΔE in OKLab). M1 records the first measurement and sets the acceptance threshold from it.
