# html corpus fixtures

HTML sources converted twice: by Chromium, through the Chromium route of the Gotenberg image `GOTENBERG_IMAGE` in `corpus/versions.env` pins, and by litho. The `diff` feature scores litho's conversion against Chromium's (see `FEATURES.md`). Each source is a self-contained directory, `sources/<name>/index.html` with the files it names beside it.

| Path            | What it holds                                                                                                                 |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `sources/`      | The 12 sources                                                                                                                |
| `chromium/`     | Chromium's PDF of each source, the reference (`make html-chromium`, committed)                                                |
| `pdfs/`         | litho's PDF of each source (`make html-fixtures`, committed once the converter writes them)                                   |
| `chromium.sh`   | Posts each source to the Gotenberg image and writes `chromium/<name>.pdf`                                                     |
| `build/main.go` | Converts each source with `pdf.Convert` twice and writes `pdfs/<name>.pdf`, failing on a difference and printing the warnings |
| `download.sh`   | Installs `chromium/*.pdf` as the corpus PDFs the provisioning image derives the diff goldens from                             |

## Sources

| Source        | Pages (Chromium) | Origin                                         | Exercises                                                                                                                                                        |
| ------------- | ---------------- | ---------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `invoice`     | 2                | Gotenberg `test/corpus/testdata/web/invoice`   | An external stylesheet, a PNG logo, tables with collapsed borders and header fills, `break-before: page`                                                         |
| `landing`     | 4                | Gotenberg `landing`                            | A gradient hero, `@media print` hiding the nav, a `min-width` media query, a button, long paragraphs                                                             |
| `i18n`        | 1                | Gotenberg `i18n`                               | Arabic and Hebrew (`dir="rtl"`), Chinese, Japanese, Korean, Thai, Hindi, Greek, Russian, emoji, mathematics                                                      |
| `print-css`   | 4                | Gotenberg `print-css`                          | `@page { size: A5; margin }` (ignored: the route does not prefer the CSS page size), forced page breaks                                                          |
| `wide-table`  | 1                | Gotenberg `wide-table`                         | A 30-column table wider than the page, `white-space: nowrap`                                                                                                     |
| `markdown`    | 2                | Gotenberg `markdown`, rendered to HTML by hand | Headings, a table, a code block, a strikethrough, task and definition lists, a link, an image, a rule                                                            |
| `floats`      | 1                | Self-authored                                  | Left and right floats holding floats of their own, text wrapping between them, `clear`, stacked right floats                                                     |
| `lists`       | 1                | Self-authored                                  | Nested ordered lists in decimal, alpha and roman, `start` and `reversed`, disc, circle and square markers, inside markers, `list-style: none`, a definition list |
| `borders`     | 1                | Self-authored                                  | The eight border styles, four widths and colours, radii on every corner, elliptical, thick and uneven-side radii                                                 |
| `backgrounds` | 1                | Self-authored                                  | Colours with alpha, a tiled PNG, repeat-x and -y, positions, origin, clip, size, a linear gradient, two layers                                                   |
| `article`     | 6                | Self-authored, generated text                  | A long article: 8 sections, 24 subsections, justified paragraphs, block quotes, internal and external links                                                      |
| `images`      | 1                | Self-authored                                  | A JPEG and a PNG at their own size, scaled by attributes, an inline image on the middle of a line, a full-width image                                            |

The Gotenberg sources are copied from its repository at commit `77fa381241480e8c2555497d150035418231e72a`, the last to touch `test/corpus/testdata/web`. What changed in the copy:

- `invoice`: `header.html` and `footer.html` are left out. The Chromium route reads files of those names as the page header and footer templates, which litho's converter has no equivalent of; sent along, they would draw on Chromium's pages only.
- `markdown`: the source is a template (`{{ toHTML "release-notes.md" }}`) the Markdown route fills in. The copy holds the two Markdown files rendered by hand as GitHub-flavoured Markdown renders them, with `style.css` and `screenshot.png`. The `<script>` element `release-notes.md` carries to test sanitising is stripped; its marker text stays.
- `console-error` and `wait` are not copied: each exists to run a script.

No other source holds a script. The six self-authored sources use the Liberation families by name, which both the Gotenberg image and the fixture fonts carry; their images (`tile.png`, `photo.jpg`, `chart.png`, `dot.png`) were drawn by a small Go program for this corpus.

## The Chromium references

```bash
make html-chromium
```

runs `chromium.sh`, which starts the pinned image on a free loopback port and posts each source directory, `index.html` and every sibling file, to `/forms/chromium/convert/html` with `paperWidth=210mm`, `paperHeight=297mm`, `emulatedMediaType=print` and `printBackground=true`, the route's margins left at their default (0.39 inch). Backgrounds are printed because a document converter paints them; the route's default leaves them out. Chromium writes the time into each PDF, so a rebuild changes the bytes: rebuild only when a source or the pin changes, then re-provision (`rm -rf test/integration/testdata/html && make corpus-init CORPUS_NAME=html`) and recapture the cell. Chromium's A4 page is 595.92 by 841.92 points, a rounding of 8.27 by 11.7 inches, and its text column starts 27.75 points in, 37 CSS pixels; litho converts on that measured geometry rather than the exact A4 so the two wrap and break pages alike.

## litho's PDFs

```bash
make html-fixtures
```

fetches the fixture fonts (`compose/fetch-fonts.sh`) and runs `build/main.go`, which converts each source on Chromium's measured A4 page (595.92 by 841.92 points, 27.75 point margins), print media, the source's directory as the resource root and the Liberation, DejaVu and Noto Sans CJK fixture fonts alone. It converts each twice, fails on a byte difference and prints each conversion's warnings, which name what litho does not draw yet. The diff feature converts the sources itself and does not read `pdfs/`, which is there for review and for the oracles that read committed PDFs.
