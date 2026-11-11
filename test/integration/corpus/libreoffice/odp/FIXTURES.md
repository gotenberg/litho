# libreoffice/odp corpus fixtures

OpenDocument presentations scored by the `diff` feature (see `FEATURES.md`) against LibreOffice Impress as Gotenberg's LibreOffice route prints them: the image `GOTENBERG_IMAGE` in `corpus/versions.env` pins (`gotenberg/gotenberg:9.0.0`, LibreOffice 26.8.2.2), route `/forms/libreoffice/convert`. A selection of LibreOffice's Impress test presentations at the commit `LIBREOFFICE_CORE_COMMIT` pins, of which only the list and the hashes are committed; the references are built by `corpus references` into the testdata and never committed. The corpus works as the libreoffice/pptx corpus does: its `FIXTURES.md` documents the scripts, the provisioning and the fonts, and the libreoffice/docx corpus's the reference checks, which apply here with `odp` for `pptx` (`make corpus-fetch CORPUS=libreoffice/odp`, `make corpus-references CORPUS=libreoffice/odp`).

| Path             | What it holds                                                                                                                                 |
| ---------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `documents.json` | The 170 LibreOffice test presentations, each by its ID, `<dir>/<file>` (`sd/tdf123.odp`, `sd/tdf456.fodp`, `sd-data/a.odp`, `sd-data/b.fodp`) |
| `files.sha256`   | Every fetched file, `<sha256>  <path in LibreOffice core>`: the 170 presentations and `COPYING.MPL`                                           |

A fetched presentation's ID names the directory whose tests it comes from: `sd/<file>` is `sd/qa/unit/data/odp/<file>`, which holds the packaged and the flat presentations alike, and `sd-data/<file>` is `sd/qa/unit/data/<file>`, the Impress test directory itself.

## How the documents were chosen

`corpus update` reads the `.odp` and `.fodp` files at the top of LibreOffice core's `sd/qa/unit/data/odp` and `sd/qa/unit/data` at the pinned commit and keeps a presentation when:

- it is at most 150 KiB;
- its name is plain ASCII (letters, digits, dot, underscore, hyphen);
- a `.odp` is a zip package whose `content.xml` holds an `office:presentation` body, with no entry under `Basic/` (macros), no WMF, EMF or SVM picture, no audio or video file (`.mp4 .mov .avi .wmv .mp3 .wav .m4a`), and every `Object*/` directory a chart by the manifest's media type (`application/vnd.oasis.opendocument.chart`); the pictures LibreOffice stores in `ObjectReplacements/` in place of the objects are not drawn. A `.fodp` is a flat file holding an `office:presentation` body whose embedded documents, if any, are charts, with no `draw:plugin`, no audio or video media type and no link to a WMF, EMF, SVM or media file. An encrypted package, whose `content.xml` is not readable, does not qualify;
- the route converts it: every candidate did on the first run, so the script's `REFUSED` names none of this corpus;
- the cell can score its reference: the script's `UNSCORABLE` leaves out `sd/qa/unit/data/odp/canvas-slide.odp`, a slide 500 cm square, Impress's largest, which drawn at 72 dpi is 14174 pixels square, four times the render budget the cell draws a page within (`pdf.Limits`' `MaxRenderPixels`, 50 million).

The candidates of each directory are sorted by name and the first ones kept, up to its cap:

| Directory             | Presentations | Qualifying | Cap |
| --------------------- | ------------- | ---------- | --- |
| `sd/qa/unit/data/odp` | 187           | 157        | 150 |
| `sd/qa/unit/data`     | 30            | 26         | 20  |
| Total                 | 217           | 183        | 170 |

`sd/qa/unit/data/odp` holds 197 files with its subdirectories (`pass/`, `fail/`, `indeterminate/`) and a picture and an SVG its tests link, which the script does not read.

LibreOffice's test documents are under the Mozilla Public License 2.0 (some also under the LGPL 3.0 or later, `COPYING.MPL` is fetched beside them): they are downloaded for testing and never committed or redistributed.

## Fonts

Of the 172 references, 134 draw Liberation Serif and 83 Liberation Sans, Impress's defaults, 27 OpenSymbol (the bullets), 5 Carlito, 5 Noto Sans, 5 DejaVu Sans, 3 Noto Sans CJK and 1 DejaVu Serif; the fixture fonts hold all of them. The image also has Noto Sans Mongolian (1 reference: `sd/Tdf112594.fodp`), which the fixture fonts lack.

## How the rig was proven

Each reference handed to the cell as litho's conversion, scored against the goldens made from it, passes on 170 of 172 presentations. Pages, links and outline score 1 on all, strict on all but 3, hairlines and a bitmap fill litho draws otherwise than poppler (`sd/LineStylesOwn.odp`, `sd/fillbitmap2.odp`, `sd/tdf153161_FlushToSave.odp`). Text F1 is below 1 on 2 and under the bar on 1, `sd/tdf96708.odp` at 0.725, whose slide numbers are white on a white slide, which PDF.js reads and litho's text extraction leaves out. Rotated text (4 references) scores 1. Layout averages 0.977 and falls below 0.75 on 1 (`sd/tdf127267DashOnHairline.odp` at 0.736, dashed hairlines). 78 references hold no text PDF.js reads; 3 of them draw the Fontwork text their slides hold as outlines (`sd/tdf100348_Fontwork2TextWarp.odp`): a conversion that writes that text as text scores text F1 0 there. `TestDiffFeature_OfficeReferencesScoreOneAgainstTheirGoldens` checked the gotenberg corpus's sources so on every run; it now checks the litho Office corpora. The counts cover this corpus and the gotenberg/odp corpus, removed on 2026-11-09, together.

## Accepted disagreements

- `sd/tdf124781.odp`: superscripts sit where Impress puts them, within 0.2 pt, so they share a line with the base text and litho's extraction joins "H2O" into one word where PDF.js splits LibreOffice's into items; the drawings agree.
- `sd/repeatBitmapMode.odp`: the first page's tiled bitmap has a zero size, which LibreOffice does not print, as in `sd/slide-tile-background.odp`.
- `sd/tdf169675_3d_object_anim.odp`: a 3D object LibreOffice renders with its scene; litho has no 3D renderer.
- `sd/Math.fodp`, `sd/formula.fodp`: formula objects LibreOffice renders through its own formula editor; litho has no formula renderer and draws the object's replacement picture when the file holds one.
- `sd/tdf169952_multiple_OLEs.odp`, `sd/linked_ole.odp`: embedded and linked OLE objects LibreOffice draws itself; their replacement pictures are StarView metafiles, which litho does not draw.
- `sd/embedded-pdf.odp`: a PDF used as a picture, whose text LibreOffice renders; litho draws the replacement picture.
- `sd/tdf100348_Fontwork2TextWarp.odp`, `sd/tdf100348_FontworkBitmapFill.odp`, `sd/tdf100348_FontworkGradientGlow.odp`: Fontwork is drawn as outlines by LibreOffice, so the reference holds no text; litho writes the text unwarped.
- `sd/extfile_field.odp`: the file name field prints the route's temporary upload path in the reference, which changes on every conversion; litho prints nothing for it.
- `sd/numfmt.odp`: the date and time fields print the moment of the conversion in the reference.
- `sd/slide-tile-background.odp`: LibreOffice prints none of the slide's tiled background bitmap, with or without its zero size; litho tiles it.
- `sd/tdf96708.odp`: white text on a white slide is read by PDF.js and left out by litho's text extraction.
