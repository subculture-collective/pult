# App registry

One YAML file per supported app in `apps/<id>.yaml`, checked against `schema.json`. The fields are explained in [docs/reference/registry-schema.md](../docs/reference/registry-schema.md).

`make verify` loads every entry through `internal/registry`, so an invalid entry fails CI.

Licence: MIT, see [LICENSES/MIT.txt](../LICENSES/MIT.txt).
