# libreoffice/xlsx corpus fixtures

SpreadsheetML workbooks scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Calc as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. A selection of LibreOffice's Calc and chart test workbooks at the commit `LIBREOFFICE_CORE_COMMIT` pins, of which only the list and the hashes are committed; Gotenberg's own test workbooks are the `gotenberg/xlsx` corpus. The references are built on the host and never committed. The corpus works as the libreoffice/docx corpus does, and its `FIXTURES.md` documents the reference checks and the drawing without annotations, which apply here with `xlsx` for `docx`; this file documents the spreadsheet options the ods and csv corpora share.

| Path                       | What it holds                                                                                                                                                                     |
| -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `documents.json`           | The 280 LibreOffice test workbooks, each by its ID, `<module>/<file>.xlsx` (`sc/tdf123.xlsx`, `chart2/tdf456.xlsx`)                                                               |
| `files.sha256`             | Every fetched file, `<sha256>  <path in LibreOffice core>`: the 280 workbooks and `COPYING.MPL`                                                                                   |
| `../list-candidates.py`    | Selects the workbooks from a LibreOffice core checkout and writes the two files above (standard library only)                                                                     |
| `../fetch.sh`              | `make office-fetch SOURCES=libreoffice FORMATS=xlsx`: downloads each listed file at the pinned commit into `testdata/libreoffice/xlsx/sources/`, refusing one whose hash differs  |
| `../../lib/libreoffice.sh` | `make office-references SOURCES=libreoffice FORMATS=xlsx`: converts every source with the pinned image into `testdata/libreoffice/xlsx/references/<name>.pdf` and `manifest.json` |
| `download.sh`              | Installs the references from the testdata volume as the corpus PDFs the provisioning image derives the diff goldens from                                                          |

A fetched workbook's ID names the module whose tests it comes from: `sc/<file>` is `sc/qa/unit/data/xlsx/<file>`, `chart2/<file>` is `chart2/qa/extras/data/xlsx/<file>`. Its reference is named by its ID with `/` written `__`: `sc__tdf123.xlsx.pdf`.

## Provisioning

```bash
make office-fetch SOURCES=libreoffice FORMATS=xlsx                  # the LibreOffice test documents and the fixture fonts, hash-checked
make office-references SOURCES=libreoffice FORMATS=xlsx             # the references (Docker); NAMES="a__b.xlsx" for some
go run ./test/integration/cmd/corpus init --corpus=libreoffice/xlsx # the diff goldens, in the corpus image
go run ./test/integration/cmd/corpus run --corpus=libreoffice/xlsx --feature=diff
```

## How the documents were chosen

`list-candidates.py` reads the `.xlsx` files at the top of LibreOffice core's `sc/qa/unit/data/xlsx` and `chart2/qa/extras/data/xlsx` at the pinned commit (the commit of the `libreoffice-26.8.2.2` tag, the release the image runs) and keeps one when:

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

The cell converts with the fixture fonts `lib/fetch-fonts.sh` installs (see the docx corpus's `FIXTURES.md`), which hold what the image draws most workbooks with: Carlito for Calibri, Excel's default (180 of the 285 references draw it), Liberation Sans for Arial and Calc's default (59), Liberation Serif (38), Caladea for Cambria (4), DejaVu and Noto Sans CJK, and Noto Sans (`NOTO_SANS_CORE_*`, Debian's `fonts-noto-core`, the image's package): the face the image's fontconfig gives a family it does not install, Tahoma, Century Gothic, Comic Sans MS, Segoe UI and the like, which 27 references draw. Noto Serif and OpenSymbol (4 references, check boxes and bullets) are pinned too; the image also has Noto Sans Symbols 2 (1) and URW base 35 (P052, 1), which the fixture fonts lack.

## How the rig was proven

Each reference handed to the cell as litho's conversion, scored against the goldens made from it, passes on 258 of 285 workbooks. Pages, links, outline and the strict rule score 1 on all; text F1 is below 1 on 60 and under the bar on 26, where text crosses a page edge or a clipping path, which PDF.js reads and litho's text extraction does not: a chart Calc cuts across two pages (`chart2/column-style.xlsx` at 0.236), a cell overflowing its single page (`budget-single`'s notes at 0.933), rotated cell text (`sc/tdf119292.xlsx` at 0.571). Layout averages 0.968 and falls below 0.9 on 4 (`chart2/chart-hatch-fill.xlsx` at 0.584, a hatch litho draws otherwise than poppler). `TestDiffFeature_LibreOfficeReferencesScoreOneAgainstTheirGoldens` checks the committed sources so on every run with the corpus provisioned. The counts cover this corpus and `gotenberg/xlsx` together.

## Accepted disagreements

- `sc/pivottable_tabular_mode.xlsx`: LibreOffice applies a date page field's hidden items by their display name, and several timestamps share each name, so which records it keeps depends on the item each name lands on (it shows a total of 13 where Excel shows 9); litho hides every value under a hidden name.
- `sc/pivot_dark1.xlsx`: the pivot output matches LibreOffice's, but "Total Result" crosses a page edge, where PDF.js reads the clipped text and litho's extraction leaves it out.
- `sc/129969-min.xlsx`, `sc/tdf111876.xlsx`, `sc/hyperlink_export.xlsx`: LibreOffice resolves relative hyperlinks against the route's upload directory, so the reference's link targets are `file:///tmp/<uuid>/...` URIs no converter can know; litho keeps the targets the workbook names. Link recall is 0 on each; `hyperlink_export` also prints the resolved paths as cell text.
- `sc/tdf161301.xlsx`: LibreOffice prints 2024-05-28 with the `[$]ggge` era code as "CE784年2月20日", a misread era, and its page count differs; litho prints the date the code asks for.
- `sc/cell-value.xlsx`: a 10,987 pt text overflows over 8 pages in both, but LibreOffice caches the text width in whole pixels at a resolution the page count only hints at, so its last page cuts the text one column earlier than litho's.
- `sc/orderOfCNumFmtElements.xlsx`: its pie charts are 3D, which LibreOffice draws in perspective with their walls and depth; litho draws every chart flat, so the slices sit elsewhere while the labels and legend agree.
- `sc/hyperlinks.xlsx`: an empty sheet holds a hidden VML comment shape 750 pt tall, which LibreOffice counts in the print range and prints as four blank pages after the two of content; litho prints the two pages the cells fill.
- `sc/XlStartupExternal.xlsx`: LibreOffice recalculates a formula over an external `[1]!` defined name on load and prints `Err:509` where Excel cached a value; litho prints the cached value.
