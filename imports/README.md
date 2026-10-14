# Imports

One record per body of upstream material taken into this repository, as `<import_id>.json`. [The import record contract](../spec/imports.md) says when a record is required and what it holds; [the schema](../spec/schemas/import-record.v1.schema.json) fixes its shape.

The first record is `tasks-axi.json`, the translation of tasks-axi into the Pool. The root `LICENSE` is already in place, as its own change; each import's record arrives with its source or ahead of it, and source never arrives ahead of its record.
