# gotenberg/csv corpus fixtures

Comma-separated values files scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Calc as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. The sources are Gotenberg's own CSV files, committed under `sources/`. The references are built on the host and never committed. LibreOffice core's own Calc CSV tests are the `libreoffice/csv` corpus, scored by the same cell: its `FIXTURES.md` documents the reference checks and the fonts, which apply here.

| Path                                           | What it holds                                                                                                                                                               |
| ---------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `sources/<name>/`                              | One document each and an optional `options.json`                                                                                                                            |
| `corpus references`                            | `make office-references SOURCES=gotenberg FORMATS=csv`: converts every source with the pinned image into `testdata/gotenberg/csv/references/<name>.pdf` and `manifest.json` |
| `../../../fixture/`, `../../../fixture/build/` | Read a source and its `options.json` (the `diff` feature, the builder); `make office-fixtures FORMATS=csv` writes `pdfs/<name>.pdf`                                         |

A source's reference is named by its fixture ID: `<name>.pdf`.

## Provisioning

```bash
make office-references SOURCES=gotenberg FORMATS=csv             # the references (Docker); NAMES="a b" for some
go run ./test/integration/cmd/corpus init --corpus=gotenberg/csv # the diff goldens, in the corpus image
go run ./test/integration/cmd/corpus run --corpus=gotenberg/csv --feature=diff
```

## Sources

From Gotenberg (MIT licence): `test/corpus/testdata/office/sales.csv` and `test/integration/testdata/sheet.csv`. Neither takes options.

| Source  | File        | Origin                                                                                                                         |
| ------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `sales` | `sales.csv` | `sales.csv`: 25 sales rows of eight columns, names outside ASCII (`Straße GmbH`, `Élodie & Fils`): 1 page                      |
| `sheet` | `sheet.csv` | `sheet.csv`: a header row and two rows, "Alice" on the one page and no page header (Gotenberg's `libreoffice_convert.feature`) |
