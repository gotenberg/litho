# libreoffice/ods corpus fixtures

OpenDocument spreadsheets scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Calc as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.4.1`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. A selection of LibreOffice's Calc and chart test spreadsheets at the commit `LIBREOFFICE_CORE_COMMIT` pins, of which only the list and the hashes are committed; the references are built by `corpus references` into the testdata and never committed. The corpus works as the libreoffice/xlsx corpus does: its `FIXTURES.md` documents the scripts, the provisioning and the fonts, and the libreoffice/docx corpus's the reference checks, which apply here with `ods` for `xlsx` (`make corpus-fetch CORPUS=libreoffice/ods`, `make corpus-references CORPUS=libreoffice/ods`).

| Path             | What it holds                                                                                                                     |
| ---------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| `documents.json` | The 256 LibreOffice test spreadsheets, each by its ID, `<module>/<file>` (`sc/tdf123.ods`, `sc/tdf456.fods`, `chart2/tdf789.ods`) |
| `files.sha256`   | Every fetched file, `<sha256>  <path in LibreOffice core>`: the 256 spreadsheets and `COPYING.MPL`                                |

A fetched spreadsheet's ID names the module whose tests it comes from and its extension the directory: `sc/<file>.ods` is `sc/qa/unit/data/ods/<file>.ods`, `sc/<file>.fods` is `sc/qa/unit/data/fods/<file>.fods`, `chart2/<file>.ods` is `chart2/qa/extras/data/ods/<file>.ods`.

## How the documents were chosen

`corpus update` reads the files at the top of LibreOffice core's `sc/qa/unit/data/ods`, `sc/qa/unit/data/fods` and `chart2/qa/extras/data/ods` at the pinned commit, each directory for its own extension (the one flat file `sc/qa/unit/data/ods` holds is left out), and keeps a spreadsheet when:

- it is at most 150 KiB;
- its name is plain ASCII (letters, digits, dot, underscore, hyphen);
- a `.ods` is a zip package whose `content.xml` holds an `office:spreadsheet` body, with no entry under `Basic/` (macros), no WMF, EMF or SVM picture, and every `Object*/` directory a chart by the manifest's media type (`application/vnd.oasis.opendocument.chart`); the pictures LibreOffice stores in `ObjectReplacements/` in place of the charts are not drawn. A `.fods` is a flat file holding an `office:spreadsheet` body whose embedded documents, if any, are charts. An encrypted package, whose `content.xml` is not readable, does not qualify;
- the route converts it: every candidate did on the first run, so the script's `REFUSED` names none of this corpus.

The candidates of each directory are sorted by name and the first ones kept, up to its cap:

| Directory                   | Spreadsheets | Qualifying | Cap |
| --------------------------- | ------------ | ---------- | --- |
| `sc/qa/unit/data/ods`       | 298          | 286        | 200 |
| `sc/qa/unit/data/fods`      | 16           | 16         | 16  |
| `chart2/qa/extras/data/ods` | 59           | 58         | 40  |
| Total                       | 373          | 360        | 256 |

`sc/qa/unit/data/ods` holds 345 files with its subdirectories (the OpenCL and shared formula tests), which the script does not read.

LibreOffice's test documents are under the Mozilla Public License 2.0 (some also under the LGPL 3.0 or later, `COPYING.MPL` is fetched beside them): they are downloaded for testing and never committed or redistributed.

## Fonts

Of the 258 references, 240 draw Liberation Sans, Calc's default, 22 Carlito, 17 Liberation Serif and 7 Noto Sans, the image's face for a family it does not install; OpenSymbol draws 1 and URW base 35 (Z003) 1. The cell converts with the image's fonts, which hold all of them. Two references name an unembedded Helvetica in their form controls' appearances, which are annotations the cell leaves out.

## How the rig was proven

Each reference handed to the cell as litho's conversion, scored against the goldens made from it, passes on 247 of 258 spreadsheets. Pages, links, outline and the strict rule score 1 on all; text F1 is below 1 on 58 and under the bar on 11, where text crosses a page edge or a clipping path, which PDF.js reads and litho's text extraction does not: a chart cut across pages (`chart2/secondary_axis.ods` at 0.642), long cells running off their page (`sc/lookup_target.fods` at 0.438). Layout averages 0.972 and falls below 0.9 on 1. `TestDiffFeature_OfficeReferencesScoreOneAgainstTheirGoldens` checked the gotenberg corpus's sources so on every run; it now checks the litho Office corpora. The counts cover this corpus and the gotenberg/ods corpus, removed on 2026-11-09, together.

## Accepted disagreements

- `sc/lookup_target.fods`: LibreOffice prints the path of an external link's source as it resolved it on the route, `file:///tmp/<uuid>/...`, which changes on every conversion; litho prints the document's text.
- `sc/sheet_name_with_dots.ods`: two links to a cell of the sheet `1.1.1.1`, which LibreOffice cannot resolve and writes as URIs of the upload itself; the cell reads them as links to the first page (`FEATURES.md`, "Links to the upload"), and litho writes no link for them. Link recall is 0. `sc/external_hyperlink.ods`, `sc/fdo79305.ods` and `sc/rich-text-cells.ods` link files beside the upload, which the reference names under the route's folders and the cell reads as the relative links the documents state.
- `sc/hard-recalc.ods`, `sc/tdf130583.ods`, `sc/cachedValue.ods`, `sc/dependencyTree.ods`, `sc/tdf130556.ods`: LibreOffice recalculates formulas on load, those without a saved result and the volatile ones (`TODAY()` prints the day of the conversion), and inherits number formats into their results; litho shows the value each cell saved, and never evaluates a formula.
- `chart2/tdf128432.ods`: a 3D bar chart LibreOffice draws in perspective with its walls and bar depth; litho draws every chart flat, so the bars sit elsewhere while the axes and labels agree.

## Expected text

One reference holds text its pages do not show, which PDF.js reads. It keeps, under `options/<fixture ID with / written __>/`, an `options.json` naming `expectedText` and the file `expected-text.txt` it names: the page's text in reading order, written from the source's runs and read against the reference drawn at 72 dpi. `corpus update` rewrites the lists alone and leaves the directory, and a directory that names no listed fixture fails the corpus's load. The `diff` cell scores litho's text against the file and reads the reference for the pages, layout, links and outline alone; the reference is the one built without the file (`FEATURES.md`, "Expected text").

| Fixture                    | What the reference holds                                                                                                                                                                                                                                                                   |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `sc/LargeAnchorOffset.ods` | The label `79.99 mm` of its dimension line on page 1, and on page 2 the label again from 24.55 points left of the page, its last glyphs `9 mm` inside the page's edge but under the clip of the page's area, which shows only the line's end there; litho writes the label on page 1 alone |
