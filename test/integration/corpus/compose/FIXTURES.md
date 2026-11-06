# compose corpus fixtures

PDFs litho writes itself: each fixture is a plain text source in `sources/` that `pdf.Convert` lays out, so the corpus scores the converter's output with the oracles the matrix already runs. The PDFs are committed, so provisioning and scoring never depend on the host's fonts; only a rebuild does.

## Oracles

The manifest expands every PDF over `PDF_A-2b` and `PDF_UA-1`, the two profiles the converter plan (`pkg/internal/compose/README.md`, wave 0) holds its output to.

| Cell                     | What it checks on a converted PDF                                                                                                                                                   |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `text`                   | PDF.js reads back the source text: the ToUnicode CMap the backend writes from `Glyph.Text` (ligatures included) and the reading order of the content stream. Once per PDF.          |
| `render`                 | poppler and litho draw every page alike under the render cell's pixel rule: glyph placement, line breaks, page breaks and the embedded subsets. Every page, once per PDF.           |
| `determinism`            | litho draws the first three pages twice, in fresh readers, to the same pixels (the render pass alone, as on pdfjs). The builder holds the conversion itself to identical bytes.     |
| `compliance`             | litho's PDF/A-2b and PDF/UA-1 verdict and failed-rule set on the raw output equal veraPDF's. The output claims neither, so both report it non-conformant; the rule sets must agree. |
| `remediation`            | litho remediates the output to the profile, and neither litho nor veraPDF finds a violation left.                                                                                   |
| `remediation-aggressive` | The same with `WithAggressiveFixes`.                                                                                                                                                |

`remediation-pair` stays out: it reads each PDF's home profile from a file name suffix (`_2b`, `_ua1`) only the litho corpus carries. The `mutate-*` cells, `metadata` and the merge and split cells add nothing the converter decides.

## Fixtures

| Fixture                 | Pages | Exercises                                                                                                                                            | Fonts embedded                                                                                     |
| ----------------------- | ----- | ---------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `ascii_paragraphs.pdf`  | 1     | English paragraphs longer than the line, blank lines between, punctuation, an address that must not split oddly, a last line with no newline         | DejaVu Serif                                                                                       |
| `long_lines_wrap.pdf`   | 2     | One 5,000-character line of words: UAX #14 wrapping over many lines and onto a second page                                                           | DejaVu Serif                                                                                       |
| `tabs_and_formfeed.pdf` | 2     | Tab stops every 8 characters, leading tabs, trailing spaces, a form feed that starts page 2                                                          | DejaVu Serif                                                                                       |
| `latin_accents.pdf`     | 1     | French, German and Spanish accents, guillemets and German quotes, and the `fi`, `fl`, `ff`, `ffi`, `ffl` pairs the shaper turns into ligature glyphs | DejaVu Serif                                                                                       |
| `greek_cyrillic.pdf`    | 1     | A Greek and a Russian paragraph, with tonos and ё                                                                                                    | DejaVu Serif                                                                                       |
| `cjk.pdf`               | 1     | A Japanese and a Chinese paragraph: CFF-flavoured OpenType subsets (CIDFontType0C), lines broken between ideographs                                  | Noto Sans JP, Noto Sans SC                                                                         |
| `arabic_hebrew.pdf`     | 1     | An Arabic and a Hebrew paragraph: right-to-left paragraphs, Arabic joining forms, neutral punctuation between runs, fallback past the serif family   | DejaVu Sans (Arabic), Liberation Serif (Hebrew), DejaVu Serif (the Latin punctuation between runs) |
| `empty_lines_pages.pdf` | 3     | 150 short lines, one empty in ten and a run of five empty lines: line height and the page breaks across three pages                                  | DejaVu Serif                                                                                       |

Each PDF embeds only the subset of each face it draws, as Type0 fonts over CIDFontType2 (TrueType) or CIDFontType0C (CFF), with Identity-H and a ToUnicode CMap (`pdffonts`). `cjk.pdf` weighs 0.9 MB, nearly all of it the two CFF subsets: the open finding that a CJK CFF subset embeds far more than the glyphs drawn.

## Known divergences

- `arabic_hebrew` fails the `text` cell, on its one page. PDF.js and litho read the same glyphs but in a different order: litho extracts each right-to-left line in logical order, as the source has it, and PDF.js lists the runs of each line from left to right, each run in logical order, so the end of a wrapped line comes first and the neutral punctuation between runs moves. The first line reads:
  - PDF.js: `تضع آلة .الطباعة فن قديم، وقد غيرت الطريقة التي يتعلم بها الناس ويتبادلون المعرفة :العربية`
  - litho: `العربية: الطباعة فن قديم، وقد غيرت الطريقة التي يتعلم بها الناس ويتبادلون المعرفة. تضع آلة`

  The Hebrew lines differ the same way (PDF.js `מכונת הדפוס .הדפוס הוא ... ידע :עברית`, litho `עברית: הדפוס הוא ... ידע. מכונת הדפוס`). No override corrects it: which order an extractor owes for right-to-left text is the open question, and the cell keeps it visible.

## Regenerating

```bash
make compose-fixtures
```

The target runs `fetch-fonts.sh`, which downloads the font releases `corpus/versions.env` pins into `test/integration/testdata/fonts/` (gitignored), refuses any download whose SHA-256 is not the pinned one, and keeps only the font files and licences. It then runs `go run ./test/integration/corpus/compose/build`, which converts every `sources/*.txt` with that directory alone (`pdf.WithConvertFontDirs`), at A4, 2 cm margins and the `serif` family at 12 points, converts each source twice, and fails when the two runs differ or the conversion reports a warning. A rebuild that changes nothing leaves `pdfs/` byte for byte as committed. After a converter change, rebuild, delete `test/integration/testdata/compose`, re-provision (`make corpus-init CORPUS_NAME=compose`) and recapture the compose cells of the baseline.

The fonts the committed PDFs were built with:

| Family        | Release                                    | Pins in `versions.env`                                                            | Files used                                         |
| ------------- | ------------------------------------------ | --------------------------------------------------------------------------------- | -------------------------------------------------- |
| DejaVu        | 2.37, `dejavu-fonts-ttf-2.37.tar.bz2`      | `DEJAVU_FONTS_VERSION`, `DEJAVU_FONTS_URL`, `DEJAVU_FONTS_SHA256`                 | `DejaVuSerif.ttf`, `DejaVuSans.ttf`                |
| Liberation    | 2.1.5, `liberation-fonts-ttf-2.1.5.tar.gz` | `LIBERATION_FONTS_VERSION`, `LIBERATION_FONTS_URL`, `LIBERATION_FONTS_SHA256`     | `LiberationSerif-Regular.ttf`                      |
| Noto Sans CJK | `Sans2.004`, the region subset OTFs        | `NOTO_CJK_SANS_VERSION`, `NOTO_SANS_{JP,KR,SC,TC}_SHA256` (shared with the image) | `NotoSansJP-Regular.otf`, `NotoSansSC-Regular.otf` |

The directory holds every face of the three releases (DejaVu's 22, Liberation's 12, the four Noto Sans CJK regions), and the files above are the ones the converter matched or fell back to. All three are under the SIL Open Font License 1.1 or, for DejaVu, the Bitstream Vera licence; no font file is committed.
