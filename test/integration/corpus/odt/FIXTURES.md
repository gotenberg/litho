# odt corpus fixtures

OpenDocument text documents scored by the `diff` feature (see `FEATURES.md`) against LibreOffice as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. Two kinds of source: Gotenberg's own test documents, committed under `sources/`, and a selection of LibreOffice's Writer test documents at the commit `LIBREOFFICE_CORE_COMMIT` pins, of which only the list and the hashes are committed. The references are built on the host and never committed. The corpus works as the docx corpus does: its `FIXTURES.md` documents the scripts, the provisioning, the reference checks, the per-fixture options and the fonts, which apply here with `odt` for `docx` (`make odt-fetch`, `make odt-libreoffice`, `make odt-fixtures`).

| Path              | What it holds                                                                                         |
| ----------------- | ----------------------------------------------------------------------------------------------------- |
| `sources/<name>/` | The 4 committed sources: one `.odt` or flat `.fodt` each                                              |
| `documents.json`  | The 150 LibreOffice test documents, each by its ID, `<dir>/<file>.odt` (`odfexport/tdf123.odt`)       |
| `files.sha256`    | Every fetched file, `<sha256>  <path in LibreOffice core>`: the 150 documents and `COPYING.MPL`       |
| `download.sh`     | Installs the references from the testdata volume as the corpus PDFs the diff goldens are derived from |

## Committed sources

From Gotenberg's `test/corpus/testdata/office/` (MIT licence). None takes options.

| Source      | File        | Origin                                                                                                                                              |
| ----------- | ----------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `form`      | `form.odt`  | `form.odt`: a document with LibreOffice form controls (a text field, a checkbox, a list box, a text area) and the text `FORM-DOC-MARKER`            |
| `form-flat` | `form.fodt` | `form.fodt`: the same document as a flat ODT                                                                                                        |
| `memo`      | `memo.odt`  | `memo.odt`: an internal memo, headings of two levels, a 3 by 3 table, bold and italic spans, a footnote, a header and a footer with the page number |
| `memo-flat` | `memo.fodt` | `memo.fodt`: the same memo as a flat ODT                                                                                                            |

The form controls are form fields in LibreOffice's PDF, annotations the cell leaves out of both sides' drawings; their labels and the text around them are scored.

## How the documents were chosen

`list-office-candidates.py` reads LibreOffice core's `sw/qa/extras/odfexport/data`, `sw/qa/extras/odfimport/data` and `sw/qa/extras/layout/data` at the pinned commit and keeps a `.odt` when:

- it is at most 150 KiB;
- its name is plain ASCII (letters, digits, dot, underscore, hyphen);
- it is a zip package whose `content.xml` holds an `office:text` body, with no entry under `Object*/` (embedded objects and their replacement pictures) or `Basic/` (macros), and no WMF, EMF or SVM picture. An encrypted package, whose `content.xml` is not readable, does not qualify.

The candidates of each directory are sorted by name and the first ones kept, up to its cap:

| Directory   | Documents | Qualifying | Cap |
| ----------- | --------- | ---------- | --- |
| `odfexport` | 112       | 105        | 80  |
| `odfimport` | 73        | 68         | 40  |
| `layout`    | 91        | 70         | 30  |
| Total       | 276       | 243        | 150 |

LibreOffice's test documents are under the Mozilla Public License 2.0 (some also under the LGPL 3.0 or later, `COPYING.MPL` is fetched beside them): they are downloaded for testing and never committed or redistributed.

## Fonts

Beyond the fixture fonts (see the docx corpus), `form` and `form-flat` draw some glyphs in OpenSymbol; of the 150 fetched references, 7 draw Noto Sans, 5 OpenSymbol and 1 a URW face (`odfexport/table_styles_2.odt`, Nimbus Mono PS).

## How the rig was proven

Each reference handed to the cell as litho's conversion, scored against the goldens made from it, passes on 150 of 154 documents. Pages, links and outline score 1 on all; text F1 is below 1 on 5, where litho's text extraction and PDF.js split words apart (`odfexport/table_styles_2.odt` at 0.333); strict fails on 5, where litho draws hairlines, hatches and bitmap fills lighter than poppler (`odfexport/sw_hatch.odt`, layout 0.037); layout averages 0.966. `TestDiffFeature_LibreOfficeReferencesScoreOneAgainstTheirGoldens` checks the committed sources so on every run with the corpus provisioned.

## Accepted disagreements

- `layout/tdf117187.odt`: LibreOffice resolves the `text:date` field when it converts and prints the day's date (7 November 2026 in the references); litho prints the value the document saved, 10 September 2018. A reference that changes with the day cannot be matched, and the text bar misses on that one field.
- `odfexport/tdf118502.odt`: the picture is an EPS whose text LibreOffice renders, so the reference carries it as extractable text; litho draws the EPS through its replacement picture, with no text to extract. The page agrees; text F1 is 0.
- `odfexport/embedded-pdf.odt`: the embedded PDF's text is extractable from LibreOffice's reference, which renders the PDF's content as text; litho draws the embedded object as its replacement picture.
