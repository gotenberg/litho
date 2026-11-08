# gotenberg/xlsx corpus fixtures

SpreadsheetML workbooks scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Calc as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. The sources are Gotenberg's own test workbooks, committed under `sources/`. The references are built on the host and never committed. LibreOffice core's own Calc and chart test workbooks are the `libreoffice/xlsx` corpus, scored by the same cell: its `FIXTURES.md` documents the reference checks and the fonts, which apply here.

| Path                                           | What it holds                                                                                                                                                                 |
| ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `sources/<name>/`                              | One document each and an optional `options.json`                                                                                                                              |
| `download.sh`                                  | Installs the references from the testdata volume as the corpus PDFs the provisioning image derives the diff goldens from                                                      |
| `../../lib/libreoffice.sh`                     | `make office-references SOURCES=gotenberg FORMATS=xlsx`: converts every source with the pinned image into `testdata/gotenberg/xlsx/references/<name>.pdf` and `manifest.json` |
| `../../../fixture/`, `../../../fixture/build/` | Read a source and its `options.json` (the `diff` feature, the builder); `make office-fixtures FORMATS=xlsx` writes `pdfs/<name>.pdf`                                          |

A source's reference is named by its fixture ID: `<name>.pdf`.

## Provisioning

```bash
make office-references SOURCES=gotenberg FORMATS=xlsx             # the references (Docker); NAMES="a b" for some
go run ./test/integration/cmd/corpus init --corpus=gotenberg/xlsx # the diff goldens, in the corpus image
go run ./test/integration/cmd/corpus run --corpus=gotenberg/xlsx --feature=diff
```

## Sources

From Gotenberg (MIT licence): `test/corpus/testdata/office/` for `budget.xlsx` and `figures-lo.xlsx`, `test/integration/testdata/` for `singlepagesheets-scrolled.xlsx`.

| Source           | File                             | Origin                                                                                                                                                                                        | Options            |
| ---------------- | -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------ |
| `budget`         | `budget.xlsx`                    | `budget.xlsx`: three sheets (Summary, Monthly, Notes), "FY2026 Budget" on page 1, the first column clipping long cells ("Cloud host"), the Monthly sheet in landscape over two pages: 4 pages | none               |
| `budget-single`  | `budget.xlsx`                    | `budget.xlsx` again, each sheet on one page the size of its content: 3 pages                                                                                                                  | `singlePage: true` |
| `figures-lo`     | `figures-lo.xlsx`                | `figures-lo.xlsx`: LibreOffice's XLSX export of two sheets, Revenue and Costs, on A4: 2 pages                                                                                                 | none               |
| `scrolled`       | `singlepagesheets-scrolled.xlsx` | `singlepagesheets-scrolled.xlsx`: a sheet saved scrolled down to row 37, whose single page still starts at row 1 ("Meteor" on page 1, Gotenberg's `libreoffice_convert.feature`)              | `singlePage: true` |
| `scrolled-pages` | `singlepagesheets-scrolled.xlsx` | The same workbook (24 KiB) cut into pages of its paper: 6 pages                                                                                                                               | none               |

`budget` and `budget-single` hold the same bytes, and so do `scrolled` and `scrolled-pages`; their options differ.

## Per-fixture options

A source converts at the route's defaults unless its directory holds `options.json`, a JSON object whose keys are form fields of Gotenberg's LibreOffice route (`pkg/modules/libreoffice/routes.go`), spelt as the route spells them. `libreoffice.sh` posts each key as the form field of the same name (with `jq`); the `diff` feature and the builder map it to `pdf.With*` options through `fixture.LoadOffice`. A key outside the table, or one spelt in another case, fails all three: the route's former `singlePageSheets` is refused. The fetched workbooks take no options. The ods and csv corpora read the same table.

| Key               | Type           | Default                            | litho option                                                                      |
| ----------------- | -------------- | ---------------------------------- | --------------------------------------------------------------------------------- |
| `password`        | string         | none                               | `WithConvertPassword`                                                             |
| `landscape`       | boolean        | `false`                            | `WithLandscape`                                                                   |
| `nativePages`     | string         | every page                         | `WithPageRanges`                                                                  |
| `nativeBookmarks` | boolean        | `true`, the route's default        | `WithOutline`                                                                     |
| `paperWidth`      | number, inches | each sheet's own paper             | `WithPaperSize`, in points (inches times 72), with `paperHeight`; both or neither |
| `paperHeight`     | number, inches | each sheet's own paper             | as `paperWidth`                                                                   |
| `singlePage`      | boolean        | `false`: each sheet cut into pages | `WithSinglePage`: each sheet on one page the size of its content                  |
