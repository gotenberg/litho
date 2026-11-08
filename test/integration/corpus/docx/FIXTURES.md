# docx corpus fixtures

DOCX documents scored by the `diff` feature (see `FEATURES.md`) against LibreOffice as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. Two kinds of source: Gotenberg's own test documents, committed under `sources/`, and a selection of LibreOffice's Writer test documents at the commit `LIBREOFFICE_CORE_COMMIT` pins, of which only the list and the hashes are committed. The references are built on the host and never committed.

| Path                               | What it holds                                                                                                                        |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `sources/<name>/`                  | The 10 committed sources: one `.docx` each and an optional `options.json`                                                            |
| `documents.json`                   | The 350 LibreOffice test documents, each by its ID, `<dir>/<file>.docx` (`ooxmlexport/tdf123.docx`)                                  |
| `files.sha256`                     | Every fetched file, `<sha256>  <path in LibreOffice core>`: the 350 documents and `COPYING.MPL`                                      |
| `../lib/list-office-candidates.py` | Selects the documents from a LibreOffice core checkout and writes the two files above (standard library only)                        |
| `../lib/fetch-office.sh`           | `make docx-fetch`: downloads each listed file at the pinned commit into `testdata/docx/sources/`, refusing one whose hash differs    |
| `../lib/libreoffice.sh`            | `make docx-libreoffice`: converts every source with the pinned image into `testdata/docx/libreoffice/<name>.pdf` and `manifest.json` |
| `download.sh`                      | Installs the references from the testdata volume as the corpus PDFs the provisioning image derives the diff goldens from             |
| `../fixture/`, `../fixture/build/` | Read a source and its `options.json` (the `diff` feature, the builder); `make docx-fixtures` writes `pdfs/<name>.pdf`                |

A source's reference is named by its fixture ID with `/` written `__`: `contract.pdf`, `ooxmlexport__tdf123.docx.pdf`.

## Provisioning

```bash
make docx-fetch                                         # the LibreOffice test documents and the fixture fonts, hash-checked
make docx-libreoffice                                   # the references (Docker); NAMES="contract ooxmlexport__tdf123.docx" for some
go run ./test/integration/cmd/corpus init --corpus=docx # the diff goldens, in the corpus image
go run ./test/integration/cmd/corpus run --corpus=docx --feature=diff
```

`libreoffice.sh` records, per reference, the image, the LibreOffice core commit and the SHA-256 of the source, of its `options.json` and of the PDF, and lists the sources the route refused under `failed`. Before scoring, the cell checks every fixture against that record and refuses the run, naming `make docx-libreoffice`, when a reference was built from another pin, from a source or options that changed since, or is missing; and, naming the directories to remove and re-provision, when the provisioned `pdfs/` the goldens were made from are not those references. The goldens are drawn without annotations (`pdftoppm -hide-annotations`), and litho's pages too: LibreOffice exports comments as notes without an appearance, whose icon is the viewer's own, and form fields with appearances in fonts it does not embed. The text goldens never held them.

## Committed sources

From Gotenberg (MIT licence): `test/corpus/testdata/office/` for the first four, `test/integration/testdata/` for the rest.

| Source             | File                    | Origin                                                                                                                                                                      | Options                           |
| ------------------ | ----------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------- |
| `contract`         | `contract.docx`         | `contract.docx`: Master Services Agreement, title, numbered headings, a page break, Calibri and Cambria                                                                     |                                   |
| `i18n`             | `i18n.docx`             | `i18n.docx`: Arabic and Hebrew paragraphs, Chinese, Japanese, Korean, Thai, Hindi, Greek, Russian, emoji, mathematical symbols                                              |                                   |
| `memo-lo`          | `memo-lo.docx`          | `memo-lo.docx`: LibreOffice's DOCX export of the memo (headings, a table, a footnote, a header and footer)                                                                  | `nativeBookmarks: false`          |
| `tracked`          | `tracked.docx`          | `tracked.docx`: tracked insertions and deletions                                                                                                                            |                                   |
| `page_1`           | `page_1.docx`           | `page_1.docx`: one page of text                                                                                                                                             |                                   |
| `pages_3`          | `pages_3.docx`          | `pages_3.docx`: three pages, the route's page range scenario                                                                                                                | `nativePages: "2-3"`              |
| `pages_12`         | `pages_12.docx`         | `pages_12.docx`: twelve pages                                                                                                                                               |                                   |
| `protected_page_1` | `protected_page_1.docx` | `protected_page_1.docx`: `page_1` encrypted (MS-OFFCRYPTO), password `foo` (Gotenberg's `libreoffice_convert.feature`, "Protected")                                         | `password: "foo"`                 |
| `special-chars`    | `Special_Chars_ß.docx`  | `Special_Chars_ß.docx`: `page_1` under a file name outside ASCII, here in landscape as the route's "Landscape" scenario converts `page_1`                                   | `landscape: true`                 |
| `long-name`        | `longitudinell.docx`    | `Longitudinell_jämförelse_mellan_laserkirurgi_och_strålbehandling_…_försök.docx` (230 bytes of name), renamed: `page_1` again, on the route's "Paper Size" scenario's paper | `paperWidth: 4`, `paperHeight: 6` |

`page_1`, `special-chars` and `long-name` hold the same bytes; their options differ.

## Per-fixture options

A source converts at the route's defaults unless its directory holds `options.json`, a JSON object whose keys are form fields of Gotenberg's LibreOffice route (`pkg/modules/libreoffice/routes.go`), spelt as the route spells them. `libreoffice.sh` posts each key as the form field of the same name (with `jq`); the `diff` feature and the builder map it to `pdf.With*` options through `fixture.LoadOffice`. A key outside the table, or one spelt in another case, fails all three. The fetched documents take no options.

| Key               | Type           | Default                                   | litho option                                                                      |
| ----------------- | -------------- | ----------------------------------------- | --------------------------------------------------------------------------------- |
| `password`        | string         | none                                      | `WithConvertPassword`                                                             |
| `landscape`       | boolean        | `false`                                   | `WithLandscape`                                                                   |
| `nativePages`     | string         | every page                                | `WithPageRanges`                                                                  |
| `nativeBookmarks` | boolean        | `true`, the route's default               | `WithOutline`                                                                     |
| `paperWidth`      | number, inches | the document's own page size, per section | `WithPaperSize`, in points (inches times 72), with `paperHeight`; both or neither |
| `paperHeight`     | number, inches | the document's own page size, per section | as `paperWidth`                                                                   |

## How the documents were chosen

`list-office-candidates.py` reads LibreOffice core's `sw/qa/extras/ooxmlexport/data`, `sw/qa/extras/ooxmlimport/data` and `sw/qa/extras/layout/data` at the pinned commit (the commit of the `libreoffice-26.8.2.2` tag, the release the image runs) and keeps a `.docx` when:

- it is at most 150 KiB;
- its name is plain ASCII (letters, digits, dot, underscore, hyphen), so every shell and form field carries it intact;
- it is a zip package holding `word/document.xml`, and none of `word/embeddings/`, `word/charts/`, `word/diagrams/`, `word/activeX/`, a `vbaProject.bin`, or an EMF, WMF, SVM or TIFF picture: embedded objects, charts, SmartArt, ActiveX controls, macros and the picture formats litho does not decode are out of scope;
- the route converts it: `math-malformed_xml.docx` (malformed on purpose, answered 400) is left out by name.

The candidates of each directory are sorted by name and the first ones kept, up to its cap:

| Directory     | Documents | Qualifying | Cap |
| ------------- | --------- | ---------- | --- |
| `ooxmlexport` | 1348      | 1222       | 250 |
| `ooxmlimport` | 161       | 142        | 60  |
| `layout`      | 107       | 74         | 40  |
| Total         | 1616      | 1438       | 350 |

LibreOffice's test documents are under the Mozilla Public License 2.0 (some also under the LGPL 3.0 or later, `COPYING.MPL` is fetched beside them): they are downloaded for testing and never committed or redistributed.

## Fonts

The cell converts with the fixture fonts `compose/fetch-fonts.sh` installs, which hold what the image draws most documents with: Carlito and Caladea for Calibri and Cambria (the Debian packages' orig tarballs, byte for byte the image's files), Liberation for Arial, Times New Roman and Courier New, DejaVu, Noto Sans CJK, Noto Sans Thai and Devanagari, and Noto Sans (`fonts-noto-core`'s face, byte for byte the image's, which draws a family the image does not install). The image also has the rest of Noto Core (Noto Serif and the script families), URW base 35 (C059, Nimbus), OpenSymbol (LibreOffice's bullet and symbol font), Noto Color Emoji and Noto Serif CJK, which the fixture fonts lack. Of the committed sources, `contract` draws its list bullets in OpenSymbol, and `i18n` draws some characters in Noto Serif and its emoji in Noto Color Emoji; of the 350 fetched references, 15 draw Noto Sans, 21 Noto Serif, 9 OpenSymbol, 3 a URW face and 1 Noto Serif CJK. The standard 14 fonts the form fields' appearances name (Times-Roman, Helvetica, unembedded) are never drawn: the annotations are left out.

## How the rig was proven

Each reference handed to the cell as litho's conversion, scored against the goldens made from it, passes on 348 of 360 documents. Pages, links and outline score 1 on all; text F1 is below 1 on 13, where litho's text extraction and PDF.js split words apart (`i18n`'s Thai and Devanagari at 0.786, `dkvert.docx`'s vertical text and `layout/tdf137025.docx` at 0); layout averages 0.966 and falls below 0.75 on 4, where litho draws hairlines and shapes otherwise than poppler (`ooxmlexport/fdo67737.docx` at 0.32); strict fails on 1. `TestDiffFeature_LibreOfficeReferencesScoreOneAgainstTheirGoldens` checks the committed sources so on every run with the corpus provisioned.

## Accepted disagreements

- `ooxmlexport/fdo59638.docx`: LibreOffice keeps the Symbol font's private-use bullet U+F0B7 in its text layer; litho maps it to the bullet it stands for, U+2022, so the text is searchable.
- `ooxmlimport/numbering-circle.docx`: LibreOffice's text layer reads the circled list number as ".①" because of its bidi ordering; litho reads "①.".
- Content controls (`ooxmlexport/2-id.docx`, `date-control.docx`, `Cool15788_symbolContentControl.docx`, `cell-sdt-redline.docx`, `author-property.docx`, `content-control-shape.docx`): LibreOffice exports a `w:sdt` as a PDF form field, so its text is a widget's value and not page text, and the reference's extracted text is empty where litho prints the control's content in the page. The text F1 of those pages cannot match.
- `ooxmlexport/del-then-format.docx`: LibreOffice prints the deleted run "BBB" because a formatting change is stacked on the deletion; litho drops deleted text as a printed document does.
- `ooxmlimport/n751077.docx`: LibreOffice prints the second text box of a page-anchored group on a page of its own although the group's geometry fits the first page; litho keeps the group on its anchor page, as the engine places every page-anchored box.
