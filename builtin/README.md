# Built-in bundle data

The release job writes `catalog.db.zst` here before building `tender`, so release binaries carry the catalog as their built-in fallback. Source builds leave the directory empty except for this file; they then need a catalog from a signed bundle, `-catalog` or `$TENDER_CATALOG`.

`catalog.db.zst` is ignored by git.
