# libreoffice/pptx corpus fixtures

PresentationML presentations scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Impress as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. A selection of LibreOffice's Impress, DrawingML and chart test presentations at the commit `LIBREOFFICE_CORE_COMMIT` pins, of which only the list and the hashes are committed; the references are built by `corpus references` into the testdata and never committed. The corpus works as the libreoffice/docx corpus does, and its `FIXTURES.md` documents the reference checks and the drawing without annotations, which apply here with `pptx` for `docx`; this file documents the presentation options the odp corpus shares.

| Path                | What it holds                                                                                                                                                            |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `documents.json`    | The 315 LibreOffice test presentations, each by its ID, `<module>/<file>.pptx` (`sd/tdf123.pptx`, `oox/tdf456.pptx`, `chart2/a.pptx`, `sd-data/b.pptx`)                  |
| `files.sha256`      | Every fetched file, `<sha256>  <path in LibreOffice core>`: the 315 presentations and `COPYING.MPL`                                                                      |
| `corpus update`     | Selects the presentations from a LibreOffice core checkout and writes the two files above                                                                                |
| `corpus fetch`      | `make corpus-fetch CORPUS=libreoffice/pptx`: downloads each listed file at the pinned commit into `testdata/libreoffice/pptx/sources/`, refusing one whose hash differs  |
| `corpus references` | `make corpus-references CORPUS=libreoffice/pptx`: converts every source with the pinned image into `testdata/libreoffice/pptx/references/<name>.pdf` and `manifest.json` |

A fetched presentation's ID names the directory whose tests it comes from: `sd/<file>` is `sd/qa/unit/data/pptx/<file>`, `sd-data/<file>` is `sd/qa/unit/data/<file>` (the Impress test directory itself, which holds a few documents of each format beside its format directories), `oox/<file>` is `oox/qa/unit/data/<file>`, `chart2/<file>` is `chart2/qa/extras/data/pptx/<file>`. Its reference is named by its ID with `/` written `__`: `sd__tdf123.pptx.pdf`.

## Provisioning

```bash
make corpus CORPUS=libreoffice/pptx FEATURE=diff # every missing step, then the score
```

`make corpus-prepare CORPUS=libreoffice/pptx` runs the steps alone: `corpus-fetch` downloads the documents and the fixture fonts, hash-checked; `corpus-references` converts them with the pinned image (Docker; `FIXTURES=a__b.ext` rebuilds some); `corpus-goldens` derives the diff goldens in the corpus image.

## How the documents were chosen

`corpus update` reads the `.pptx` files at the top of LibreOffice core's `sd/qa/unit/data/pptx`, `oox/qa/unit/data`, `chart2/qa/extras/data/pptx` and `sd/qa/unit/data` at the pinned commit (the commit of the `libreoffice-26.8.2.2` tag, the release the image runs) and keeps one when:

- it is at most 150 KiB;
- its name is plain ASCII (letters, digits, dot, underscore, hyphen), so every shell and form field carries it intact;
- it is a zip package holding `ppt/presentation.xml`, and none of `ppt/activeX/`, `ppt/vbaProject.bin`, an EMF, WMF, SVM or TIFF picture, or an audio or video file (`.mp4 .mov .avi .wmv .mp3 .wav .m4a`): ActiveX controls, macros, the picture formats litho does not decode and the media a PDF cannot play are out of scope. A part under `ppt/embeddings/` is allowed when no relationship but a chart's targets it: the workbook a chart keeps its data in (ECMA-376 Part 1, 21.2.2.63), which nothing draws. Charts are kept: the chart2 directory is there for them; an embedded object is not;
- the route converts it: every candidate did on the first run, so the script's `REFUSED` names none of this corpus.

The candidates of each directory are sorted by name and the first ones kept, up to its cap:

| Directory                    | Presentations | Qualifying | Cap |
| ---------------------------- | ------------- | ---------- | --- |
| `sd/qa/unit/data/pptx`       | 389           | 348        | 200 |
| `oox/qa/unit/data`           | 61            | 60         | 61  |
| `chart2/qa/extras/data/pptx` | 38            | 36         | 38  |
| `sd/qa/unit/data`            | 23            | 19         | 20  |
| Total                        | 511           | 463        | 315 |

`sd/qa/unit/data/pptx` holds 403 files with its subdirectories (`pass/`, `fail/`, `indeterminate/`) and its 4 `.odp` and 1 `.ppt` files, which the script does not read; `oox/qa/unit/data` holds 152 files, 61 of them `.pptx`, the DrawingML shape tests.

LibreOffice's test documents are under the Mozilla Public License 2.0 (some also under the LGPL 3.0 or later, `COPYING.MPL` is fetched beside them): they are downloaded for testing and never committed or redistributed.

## Fonts

The cell converts with the fixture fonts `corpus fetch --corpus=fonts` installs (see the docx corpus's `FIXTURES.md`). Of the 319 references, 141 draw Carlito (Calibri and Calibri Light, PowerPoint's defaults, and Aptos, which the image lacks), 50 Liberation Sans, 38 Noto Sans (the image's face for a family it does not install: Tahoma, Segoe UI, Trebuchet MS, Century Gothic), 21 OpenSymbol (Impress's bullets: `OPENSYMBOL_*`, Debian's `fonts-opensymbol`, the image's package), 6 Liberation Mono, 2 Liberation Serif, 2 Noto Sans CJK, and DejaVu Serif, DejaVu Sans Mono and Caladea 1 each; the fixture fonts hold all of them. The image also has URW base 35 (Nimbus Sans, 1 reference: `sd/slide-sections.pptx`), which the fixture fonts lack, and one reference draws the font its document embeds (`sd-data/BoldonseFontEmbedded.pptx`, Boldonse).

## How the rig was proven

Each reference handed to the cell as litho's conversion, scored against the goldens made from it, passes on 314 of 319 presentations. Pages, links and outline score 1 on all, strict on all but 1 (`sd-data/n820786.pptx`, a hairline litho draws lighter than poppler). Text F1 is below 1 on 3 and under the bar on 2, where text is drawn invisible, which PDF.js reads and litho's text extraction leaves out: white on a white slide (`sd/layout-clrmap-override.pptx` at 0.829), at 2% opacity (`sd/tdf118776.pptx` at 0). Rotated text (19 references) and text past the slide edge (2) score 1. Layout averages 0.975 and falls below 0.75 on 3, nearly empty slides whose few strokes litho and poppler draw apart (`sd/tdf112633.pptx` at 0.290, a faint rotated frame poppler draws and litho does not in `sd/tdf103473.pptx` at 0.473). 102 references hold no text PDF.js reads; 11 of them draw the text their slides hold as outlines, Fontwork and WordArt (`oox/tdf125582_TextOnCircle.pptx`, `sd/tdf116350-texteffects.pptx`): a conversion that writes that text as text scores text F1 0 there. `TestDiffFeature_LibreOfficeReferencesScoreOneAgainstTheirGoldens` checked the gotenberg corpus's sources so on every run; it now checks the litho Office corpora. The counts cover this corpus and the gotenberg/pptx corpus, removed on 2026-11-09, together.

## Accepted disagreements

- `sd/n828390_3.pptx`: subscripts sit where Impress puts them, so they share a line with the base text and litho's extraction joins the word where PDF.js splits LibreOffice's into items; the drawings agree.
- The 3D scenes of `oox/Scene3d_*.pptx` (13 fixtures: isometricRightUp, legacyObliqueBottomRight, legacyPerspectiveTopRight, the lightRig_* variants, material_highlight, material_wireframe, obliqueTopRight, perspectiveContrastingLeft, shape_rotation): LibreOffice extrudes and lights the shapes through its 3D renderer; litho prints each shape flat with its fill and a plain rotation where the camera is one.
- WordArt and other warped text (`oox/tdf125085_WordArtFontText.pptx`, `oox/tdf125085_WordArtFontTheme.pptx`, `oox/tdf125582_TextOnCircle.pptx`, `oox/tdf139618_ImportWordArtGradient.pptx`, `oox/tdf139618_WordArtBitmapFill.pptx`, `oox/tdf155327_WordArtDefaultColor.pptx`, `sd/ShapeTextInflateTop.pptx`, `sd/tdf116350-texteffects.pptx`, `sd/tdf125573_FontWorkScaleX.pptx`): LibreOffice draws a text warp preset (`prstTxWarp`) as outlines, so the reference holds no text PDF.js can read, where litho writes the text unwarped, selectable and searchable. Text F1 is 0 on each by construction.
- `sd/numfmt.pptx`: the slide's date and time fields print the moment of the conversion in the reference; litho prints the print date it is given, or the text the file saved.
- `sd/layout-clrmap-override.pptx`, `sd/tdf118776.pptx`: white text on a white slide and text at 2% opacity are read by PDF.js and left out by litho's text extraction, which reads what a reader sees; the drawings agree.
- `oox/import-characters.pptx`: an OMML equation LibreOffice renders through its formula editor; litho has no formula renderer.
- `oox/BoldonseFontEmbedded.pptx`: the deck embeds its own font, which LibreOffice installs and prints with; litho prints the text in the fallback face.
