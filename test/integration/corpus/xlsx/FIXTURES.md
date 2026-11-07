# xlsx corpus fixtures

SpreadsheetML workbooks scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Calc as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. Two kinds of source: Gotenberg's own test workbooks, committed under `sources/`, and a selection of LibreOffice's Calc and chart test workbooks at the commit `LIBREOFFICE_CORE_COMMIT` pins, of which only the list and the hashes are committed. The references are built on the host and never committed. The corpus works as the docx corpus does, and its `FIXTURES.md` documents the reference checks and the drawing without annotations, which apply here with `xlsx` for `docx`; this file documents the spreadsheet options the ods and csv corpora share.

| Path                               | What it holds                                                                                                                        |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `sources/<name>/`                  | The 5 committed sources: one `.xlsx` each and an optional `options.json`                                                             |
| `documents.json`                   | The 280 LibreOffice test workbooks, each by its ID, `<module>/<file>.xlsx` (`sc/tdf123.xlsx`, `chart2/tdf456.xlsx`)                  |
| `files.sha256`                     | Every fetched file, `<sha256>  <path in LibreOffice core>`: the 280 workbooks and `COPYING.MPL`                                      |
| `../lib/list-office-candidates.py` | Selects the workbooks from a LibreOffice core checkout and writes the two files above (standard library only)                        |
| `../lib/fetch-office.sh`           | `make xlsx-fetch`: downloads each listed file at the pinned commit into `testdata/xlsx/sources/`, refusing one whose hash differs    |
| `../lib/libreoffice.sh`            | `make xlsx-libreoffice`: converts every source with the pinned image into `testdata/xlsx/libreoffice/<name>.pdf` and `manifest.json` |
| `download.sh`                      | Installs the references from the testdata volume as the corpus PDFs the provisioning image derives the diff goldens from             |
| `../fixture/`, `../fixture/build/` | Read a source and its `options.json` (the `diff` feature, the builder); `make xlsx-fixtures` writes `pdfs/<name>.pdf`                |

A fetched workbook's ID names the module whose tests it comes from: `sc/<file>` is `sc/qa/unit/data/xlsx/<file>`, `chart2/<file>` is `chart2/qa/extras/data/xlsx/<file>`. Its reference is named by its ID with `/` written `__`: `budget.pdf`, `sc__tdf123.xlsx.pdf`.

## Provisioning

```bash
make xlsx-fetch                                         # the LibreOffice test workbooks and the fixture fonts, hash-checked
make xlsx-libreoffice                                   # the references (Docker); NAMES="budget sc__tdf123.xlsx" for some
go run ./test/integration/cmd/corpus init --corpus=xlsx # the diff goldens, in the corpus image
go run ./test/integration/cmd/corpus run --corpus=xlsx --feature=diff
```

## Committed sources

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

## How the documents were chosen

`list-office-candidates.py` reads the `.xlsx` files at the top of LibreOffice core's `sc/qa/unit/data/xlsx` and `chart2/qa/extras/data/xlsx` at the pinned commit (the commit of the `libreoffice-26.8.2.2` tag, the release the image runs) and keeps one when:

- it is at most 150 KiB;
- its name is plain ASCII (letters, digits, dot, underscore, hyphen), so every shell and form field carries it intact;
- it is a zip package holding `xl/workbook.xml`, and none of `xl/embeddings/`, `xl/activeX/`, `xl/vbaProject.bin`, or an EMF, WMF, SVM or TIFF picture: embedded objects, ActiveX controls, macros and the picture formats litho does not decode are out of scope. Charts are kept: the chart2 directory is there for them;
- the route converts it: every candidate did on the first run, so the script's `REFUSED` names none of this corpus.

The candidates of each directory are sorted by name and the first ones kept, up to its cap:

| Directory                    | Workbooks | Qualifying | Cap |
| ---------------------------- | --------- | ---------- | --- |
| `sc/qa/unit/data/xlsx`       | 294       | 279        | 220 |
| `chart2/qa/extras/data/xlsx` | 151       | 147        | 60  |
| Total                        | 445       | 426        | 280 |

`sc/qa/unit/data/xlsx` holds 352 files with its subdirectories (`pass/`, `fail/`, `indeterminate/` and the pivot table, data table, shared formula and track changes tests), which the script does not read.

LibreOffice's test documents are under the Mozilla Public License 2.0 (some also under the LGPL 3.0 or later, `COPYING.MPL` is fetched beside them): they are downloaded for testing and never committed or redistributed.

## Fonts

The cell converts with the fixture fonts `compose/fetch-fonts.sh` installs (see the docx corpus's `FIXTURES.md`), which hold what the image draws most workbooks with: Carlito for Calibri, Excel's default (180 of the 285 references draw it), Liberation Sans for Arial and Calc's default (59), Liberation Serif (38), Caladea for Cambria (4), DejaVu and Noto Sans CJK, and Noto Sans (`NOTO_SANS_CORE_*`, Debian's `fonts-noto-core`, the image's package): the face the image's fontconfig gives a family it does not install, Tahoma, Century Gothic, Comic Sans MS, Segoe UI and the like, which 27 references draw. The image also has OpenSymbol (4 references, check boxes and bullets), Noto Sans Symbols 2 (1) and URW base 35 (P052, 1), which the fixture fonts lack.

## How the rig was proven

Each reference handed to the cell as litho's conversion, scored against the goldens made from it, passes on 258 of 285 workbooks. Pages, links, outline and the strict rule score 1 on all; text F1 is below 1 on 60 and under the bar on 26, where text crosses a page edge or a clipping path, which PDF.js reads and litho's text extraction does not: a chart Calc cuts across two pages (`chart2/column-style.xlsx` at 0.236), a cell overflowing its single page (`budget-single`'s notes at 0.933), rotated cell text (`sc/tdf119292.xlsx` at 0.571). Layout averages 0.968 and falls below 0.9 on 4 (`chart2/chart-hatch-fill.xlsx` at 0.584, a hatch litho draws otherwise than poppler). `TestDiffFeature_LibreOfficeReferencesScoreOneAgainstTheirGoldens` checks the committed sources so on every run with the corpus provisioned.

## Accepted disagreements
