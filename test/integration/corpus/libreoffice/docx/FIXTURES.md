# libreoffice/docx corpus fixtures

DOCX documents scored by the `diff` feature (see `FEATURES.md`) against LibreOffice as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. A selection of LibreOffice's Writer test documents at the commit `LIBREOFFICE_CORE_COMMIT` pins, of which only the list and the hashes are committed; the references are built by `corpus references` into the testdata and never committed.

| Path                | What it holds                                                                                                                                                            |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `documents.json`    | The 350 LibreOffice test documents, each by its ID, `<dir>/<file>.docx` (`ooxmlexport/tdf123.docx`)                                                                      |
| `files.sha256`      | Every fetched file, `<sha256>  <path in LibreOffice core>`: the 350 documents and `COPYING.MPL`                                                                          |
| `corpus update`     | Selects the documents from a LibreOffice core checkout and writes the two files above                                                                                    |
| `corpus fetch`      | `make corpus-fetch CORPUS=libreoffice/docx`: downloads each listed file at the pinned commit into `testdata/libreoffice/docx/sources/`, refusing one whose hash differs  |
| `corpus references` | `make corpus-references CORPUS=libreoffice/docx`: converts every source with the pinned image into `testdata/libreoffice/docx/references/<name>.pdf` and `manifest.json` |

A source's reference is named by its ID with `/` written `__`: `ooxmlexport__tdf123.docx.pdf`.

## Provisioning

```bash
make corpus CORPUS=libreoffice/docx FEATURE=diff # every missing step, then the score
```

`make corpus-prepare CORPUS=libreoffice/docx` runs the steps alone: `corpus-fetch` downloads the documents and the fixture fonts, hash-checked; `corpus-references` converts them with the pinned image (Docker; `FIXTURES=a__b.ext` rebuilds some); `corpus-goldens` derives the diff goldens in the corpus image.

`corpus references` records, per reference, the image, the LibreOffice core commit and the SHA-256 of the source, of its `options.json` and of the PDF, and lists the sources the route refused under `failed`. Before scoring, the cell checks every fixture against that record and refuses the run, naming `make corpus-references CORPUS=libreoffice/docx`, when a reference was built from another pin, from a source or options that changed since, or is missing; and, naming the directories to remove and re-provision, when the provisioned `pdfs/` the goldens were made from are not those references. The goldens are drawn without annotations (`pdftoppm -hide-annotations`), and litho's pages too: LibreOffice exports comments as notes without an appearance, whose icon is the viewer's own, and form fields with appearances in fonts it does not embed. The text goldens never held them.

## How the documents were chosen

`corpus update` reads LibreOffice core's `sw/qa/extras/ooxmlexport/data`, `sw/qa/extras/ooxmlimport/data` and `sw/qa/extras/layout/data` at the pinned commit (the commit of the `libreoffice-26.8.2.2` tag, the release the image runs) and keeps a `.docx` when:

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

The cell converts with the fixture fonts `corpus fetch --corpus=fonts` installs, which hold what the image draws most documents with: Carlito and Caladea for Calibri and Cambria (the Debian packages' orig tarballs, byte for byte the image's files), Liberation for Arial, Times New Roman and Courier New, DejaVu, Noto Sans CJK, Noto Sans Thai and Devanagari, Noto Sans and Noto Serif (`fonts-noto-core`'s faces, byte for byte the image's, which draw a sans or a serif family the image does not install), and OpenSymbol (LibreOffice's bullet and symbol font). The image also has the rest of Noto Core (the script families), URW base 35 (C059, Nimbus), Noto Color Emoji and Noto Serif CJK, which the fixture fonts lack. Of the committed sources, `contract` draws its list bullets in OpenSymbol, and `i18n` draws some characters in Noto Serif and its emoji in Noto Color Emoji; of the 350 fetched references, 15 draw Noto Sans, 21 Noto Serif, 9 OpenSymbol, 3 a URW face and 1 Noto Serif CJK. The standard 14 fonts the form fields' appearances name (Times-Roman, Helvetica, unembedded) are never drawn: the annotations are left out.

## How the rig was proven

Each reference handed to the cell as litho's conversion, scored against the goldens made from it, passes on 348 of 360 documents. Pages, links and outline score 1 on all; text F1 is below 1 on 13, where litho's text extraction and PDF.js split words apart (`i18n`'s Thai and Devanagari at 0.786, `dkvert.docx`'s vertical text and `layout/tdf137025.docx` at 0); layout averages 0.966 and falls below 0.75 on 4, where litho draws hairlines and shapes otherwise than poppler (`ooxmlexport/fdo67737.docx` at 0.32); strict fails on 1. `TestDiffFeature_LibreOfficeReferencesScoreOneAgainstTheirGoldens` checked the gotenberg corpus's sources so on every run; it now checks the litho Office corpora. The counts cover this corpus and the gotenberg/docx corpus, removed on 2026-11-09, together.

## Accepted disagreements

- `ooxmlexport/fdo59638.docx`: LibreOffice keeps the Symbol font's private-use bullet U+F0B7 in its text layer; litho maps it to the bullet it stands for, U+2022, so the text is searchable.
- `ooxmlimport/numbering-circle.docx`: LibreOffice's text layer reads the circled list number as ".①" because of its bidi ordering; litho reads "①.".
- Content controls (`ooxmlexport/2-id.docx`, `date-control.docx`, `Cool15788_symbolContentControl.docx`, `cell-sdt-redline.docx`, `author-property.docx`, `content-control-shape.docx`, `FDO76312.docx`, `ShapeOverlappingWithSdt.docx`, `content-control-grab-bag.docx`, `customxml-two-data-stores.docx`, `dml-charheight-default.docx`, `dml-groupshape-sdt.docx`, `groupshape-relsize.docx`, `n820504.docx`, and `dkvert.docx` among its other misses): LibreOffice exports a `w:sdt` as a PDF form field, so its text is a widget's value and not page text, and the reference's extracted text is empty where litho prints the control's content in the page; the widget's appearance is drawn in Times, so the layout of those pages differs too. The text F1 of those pages cannot match. Writing form fields for content controls would be a feature of its own, not taken.
- `tdf139336_ColumnsWithFootnoteDoNotOccupyEntirePage.docx`: LibreOffice prints two pages, moving the last section's second column to page 2 although page 1 has room for it; litho balances the columns on page 1 with the ten footnotes under them. The sections carry no setting that asks for the move.
- `ooxmlexport/del-then-format.docx`: LibreOffice prints the deleted run "BBB" because a formatting change is stacked on the deletion; litho drops deleted text as a printed document does.
- `ooxmlimport/n751077.docx`: LibreOffice prints the second text box of a page-anchored group on a page of its own although the group's geometry fits the first page; litho keeps the group on its anchor page, as the engine places every page-anchored box.
