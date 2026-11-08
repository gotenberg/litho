# gotenberg/ods corpus fixtures

OpenDocument spreadsheets scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Calc as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. The sources are Gotenberg's own test spreadsheets, committed under `sources/`. The references are built on the host and never committed. LibreOffice core's own Calc and chart test spreadsheets are the `libreoffice/ods` corpus, scored by the same cell: its `FIXTURES.md` documents the reference checks and the fonts, which apply here.

| Path                                           | What it holds                                                                                                                                                               |
| ---------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `sources/<name>/`                              | One document each and an optional `options.json`                                                                                                                            |
| `download.sh`                                  | Installs the references from the testdata volume as the corpus PDFs the provisioning image derives the diff goldens from                                                    |
| `../../lib/libreoffice.sh`                     | `make office-references SOURCES=gotenberg FORMATS=ods`: converts every source with the pinned image into `testdata/gotenberg/ods/references/<name>.pdf` and `manifest.json` |
| `../../../fixture/`, `../../../fixture/build/` | Read a source and its `options.json` (the `diff` feature, the builder); `make office-fixtures FORMATS=ods` writes `pdfs/<name>.pdf`                                         |

A source's reference is named by its fixture ID: `<name>.pdf`.

## Provisioning

```bash
make office-references SOURCES=gotenberg FORMATS=ods             # the references (Docker); NAMES="a b" for some
go run ./test/integration/cmd/corpus init --corpus=gotenberg/ods # the diff goldens, in the corpus image
go run ./test/integration/cmd/corpus run --corpus=gotenberg/ods --feature=diff
```

## Sources

From Gotenberg's `test/corpus/testdata/office/` (MIT licence). None takes options.

| Source         | File           | Origin                                                                                                                                                                                                                            |
| -------------- | -------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `figures`      | `figures.ods`  | `figures.ods`: two sheets, Revenue and Costs, each on its own Letter page with the sheet name in the header and the page number in the footer ("REVENUE-SH" on page 1, "COSTS-SHEE" on page 2, Gotenberg's `libreoffice.feature`) |
| `figures-flat` | `figures.fods` | `figures.fods`: the same spreadsheet as a flat ODS                                                                                                                                                                                |
