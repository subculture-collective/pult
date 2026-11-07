# Templates

One directory per registry app ID, holding the Mustache templates that app's registry entry names. The variables and the renderer's rules are in [docs/reference/registry-schema.md](../docs/reference/registry-schema.md#templates).

Every template is rendered in `internal/render`'s golden tests; a template no registry entry uses, or one that is missing, fails `make verify`.

Licence: MIT, see [LICENSES/MIT.txt](../LICENSES/MIT.txt). Generated config files belong to the user.
