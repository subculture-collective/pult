# Built-in bundle data

The release job writes `catalog.db.zst` here before building `tender`, so release binaries carry the catalog as their built-in fallback. Source builds leave the directory empty except for this file; they then need a catalog from a signed bundle, `-catalog` or `$TENDER_CATALOG`.

`catalog.db.zst` is ignored by git. Release builds also set the built-in data's bundle version (`-X git.subcult.tv/subculture-collective/theme-tender.BuiltinVersion=YYYY.MM.DD.0`, the tagged commit's date), so an older installed bundle does not replace it.
