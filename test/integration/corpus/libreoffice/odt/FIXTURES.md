# libreoffice/odt corpus fixtures

OpenDocument text documents scored by the `diff` feature (see `FEATURES.md`) against LibreOffice as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. A selection of LibreOffice's Writer test documents at the commit `LIBREOFFICE_CORE_COMMIT` pins, of which only the list and the hashes are committed; the references are built by `corpus references` into the testdata and never committed. The corpus works as the libreoffice/docx corpus does: its `FIXTURES.md` documents the scripts, the provisioning, the reference checks and the fonts, which apply here with `odt` for `docx` (`make corpus-fetch CORPUS=libreoffice/odt`, `make corpus-references CORPUS=libreoffice/odt`).

| Path             | What it holds                                                                                                          |
| ---------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `documents.json` | The 195 LibreOffice test documents, each by its ID, `<dir>/<file>.odt` or `<dir>/<file>.fodt` (`odfexport/tdf123.odt`) |
| `files.sha256`   | Every fetched file, `<sha256>  <path in LibreOffice core>`: the 195 documents and `COPYING.MPL`                        |

## How the documents were chosen

`corpus update` reads LibreOffice core's `sw/qa/extras/odfexport/data`, `sw/qa/extras/odfimport/data` and `sw/qa/extras/layout/data` at the pinned commit and keeps a `.odt` or a flat `.fodt` when:

- it is at most 150 KiB;
- its name is plain ASCII (letters, digits, dot, underscore, hyphen);
- it is a zip package whose `content.xml` holds an `office:text` body, with no entry under `Object*/` (embedded objects and their replacement pictures) or `Basic/` (macros), and no WMF, EMF or SVM picture. An encrypted package, whose `content.xml` is not readable, does not qualify;
- or it is a flat XML document holding an `office:text` body, with no embedded `office:document` and no plug-in, WMF, EMF, SVM, audio or video.

Each directory is read once per extension, so the flat documents have caps of their own and never displace the packaged ones. The candidates are sorted by name and the first ones kept, up to the cap:

| Directory   | Extension | Documents | Qualifying | Cap |
| ----------- | --------- | --------- | ---------- | --- |
| `odfexport` | `.odt`    | 112       | 105        | 80  |
| `odfimport` | `.odt`    | 73        | 68         | 40  |
| `layout`    | `.odt`    | 91        | 70         | 30  |
| `odfexport` | `.fodt`   | 55        | 54         | 15  |
| `odfimport` | `.fodt`   | 21        | 21         | 10  |
| `layout`    | `.fodt`   | 177       | 176        | 20  |
| Total       |           | 529       | 494        | 195 |

LibreOffice's test documents are under the Mozilla Public License 2.0 (some also under the LGPL 3.0 or later, `COPYING.MPL` is fetched beside them): they are downloaded for testing and never committed or redistributed.

## Fonts

Beyond the fixture fonts (see the docx corpus), `form` and `form-flat` draw some glyphs in OpenSymbol; of the 150 fetched references, 7 draw Noto Sans, 5 OpenSymbol and 1 a URW face (`odfexport/table_styles_2.odt`, Nimbus Mono PS).

## How the rig was proven

Each reference handed to the cell as litho's conversion, scored against the goldens made from it, passes on 150 of 154 documents. Pages, links and outline score 1 on all; text F1 is below 1 on 5, where litho's text extraction and PDF.js split words apart (`odfexport/table_styles_2.odt` at 0.333); strict fails on 5, where litho draws hairlines, hatches and bitmap fills lighter than poppler (`odfexport/sw_hatch.odt`, layout 0.037); layout averages 0.966. `TestDiffFeature_LibreOfficeReferencesScoreOneAgainstTheirGoldens` checked the gotenberg corpus's sources so on every run; it now checks the litho Office corpora. The counts cover this corpus and the gotenberg/odt corpus, removed on 2026-11-09, together.

## Accepted disagreements

- `layout/tdf117187.odt`: LibreOffice resolves the `text:date` field when it converts and prints the day's date (7 November 2026 in the references); litho prints the value the document saved, 10 September 2018. A reference that changes with the day cannot be matched, and the text bar misses on that one field.
- `odfexport/tdf118502.odt`: the picture is an EPS whose text LibreOffice renders, so the reference carries it as extractable text; litho draws the EPS through its replacement picture, with no text to extract. The page agrees; text F1 is 0.
- `odfexport/embedded-pdf.odt`: the embedded PDF's text is extractable from LibreOffice's reference, which renders the PDF's content as text; litho draws the embedded object as its replacement picture.
- `odfexport/tdf139126.odt`: a table formula's saved result is a 300-digit number, which litho prints wrapped in its cell; LibreOffice recalculates the formula on load and prints "** Expression is faulty **". Formula evaluation is out of scope.
