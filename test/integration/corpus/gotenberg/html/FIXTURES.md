# gotenberg/html corpus fixtures

HTML and Markdown sources from Gotenberg's own test documents (MIT License, Copyright Julien Neuhart), converted twice: by Chromium, through the Chromium route of the Gotenberg image `GOTENBERG_IMAGE` in `corpus/versions.env` pins, and by litho. The `diff` feature scores litho's conversion against Chromium's (see `FEATURES.md`). The corpus works as `litho/html` does: its `FIXTURES.md` documents the source layout, the per-fixture options, the Chromium references (`make html-references`) and litho's PDFs (`make html-fixtures`), which apply here.

| Path          | What it holds                                                                                       |
| ------------- | --------------------------------------------------------------------------------------------------- |
| `sources/`    | The 15 sources                                                                                      |
| `references/` | Chromium's PDF of each source, the reference (`make html-references`, committed)                    |
| `pdfs/`       | litho's PDF of each source (`make html-fixtures`, committed once the converter writes them)         |
| `download.sh` | Installs `references/*.pdf` as the corpus PDFs the provisioning image derives the diff goldens from |

## Sources

| Source       | Pages (Chromium) | Origin                                         | Exercises                                                                                                   |
| ------------ | ---------------- | ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `invoice`    | 2                | Gotenberg `test/corpus/testdata/web/invoice`   | An external stylesheet, a PNG logo, tables with collapsed borders and header fills, `break-before: page`    |
| `landing`    | 4                | Gotenberg `landing`                            | A gradient hero, `@media print` hiding the nav, a `min-width` media query, a button, long paragraphs        |
| `i18n`       | 1                | Gotenberg `i18n`                               | Arabic and Hebrew (`dir="rtl"`), Chinese, Japanese, Korean, Thai, Hindi, Greek, Russian, emoji, mathematics |
| `print-css`  | 4                | Gotenberg `print-css`                          | `@page { size: A5; margin }` (preferred: an A5 page), forced page breaks                                    |
| `wide-table` | 1                | Gotenberg `wide-table`                         | A 30-column table wider than the page, `white-space: nowrap`                                                |
| `markdown`   | 2                | Gotenberg `markdown`, rendered to HTML by hand | Headings, a table, a code block, a strikethrough, task and definition lists, a link, an image, a rule       |

The sources below mirror the scenarios of Gotenberg's integration tests for the Chromium route (`test/integration/features/chromium_convert_html.feature`) on the documents those tests convert; each sets the options in the third column.

| Source                   | Pages (Chromium) | Options                                                       | Origin                                                       | Exercises                                                                                                                                                                      |
| ------------------------ | ---------------- | ------------------------------------------------------------- | ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `page-ranges`            | 2                | `nativePages: "2-3"`                                          | Gotenberg `pages-12-html`                                    | Page ranges: pages 2 and 3 of twelve                                                                                                                                           |
| `single-page`            | 1                | `singlePage`                                                  | Gotenberg `pages-12-html`                                    | One page as tall as the content; the forced break after the first heading still ends it (595.92 by 599.04 points)                                                              |
| `single-page-landscape`  | 1                | `singlePage`, `landscape`                                     | Gotenberg `wide-table-html`                                  | One page widened to a table wider than the paper (1087.92 by 384 points), Gotenberg issue 1390                                                                                 |
| `single-page-header`     | 1                | `singlePage`, `header`, `footer`, `marginTop`, `marginBottom` | Gotenberg `pages-12-html` with the `header-footer` templates | The single page prints the header and footer in its 0.6 inch margins (595.92 by 629.04 points); `totalPages` counts the 12 pages the document would print without `singlePage` |
| `landscape`              | 1                | `landscape`                                                   | Gotenberg `page-1-html`                                      | The paper's sides swapped (841.92 by 595.92 points)                                                                                                                            |
| `header-footer`          | 12               | `header`, `footer`                                            | Gotenberg `pages-12-html` and `header-footer-html`           | Header and footer templates: the title class in the header, "pageNumber of totalPages" in the footer                                                                           |
| `header-default-classes` | 12               | `header`, `footer`                                            | Gotenberg `pages-12-html`, self-authored templates           | Templates using the classes of Chromium's own header template, `text left`, `text center`, `text right`, with `title`, `pageNumber` and `totalPages`                           |
| `outline`                | 1                | `nativeBookmarks`                                             | Gotenberg `page-outline-html`                                | The headings as a two-level outline                                                                                                                                            |
| `print-css-fit`          | 4                | `preferCssPageSize: false`                                    | `print-css` (linked)                                         | An `@page size: A5` laid out at A5 and fitted to the A4 paper                                                                                                                  |

The Gotenberg sources are copied from its repository at commit `77fa381241480e8c2555497d150035418231e72a`, the last to touch `test/corpus/testdata/web`. What changed in the copy:

- `invoice`: `header.html` and `footer.html` are left out. The Chromium route reads files of those names as the page header and footer templates, which litho's converter has no equivalent of; sent along, they would draw on Chromium's pages only.
- `markdown`: the source is a template (`{{ toHTML "release-notes.md" }}`) the Markdown route fills in. The copy holds the two Markdown files rendered by hand as GitHub-flavoured Markdown renders them, with `style.css` and `screenshot.png`. The `<script>` element `release-notes.md` carries to test sanitising is stripped; its marker text stays.
- `console-error` and `wait` are not copied: each exists to run a script.

The scenario sources (`pages-12-html`, `wide-table-html`, `page-1-html`, `page-outline-html`, `header-footer-html`) are copied from Gotenberg's `test/integration/testdata` (MIT License, Copyright Julien Neuhart), unchanged but for `header-footer`'s header, which drops the `date` element: Chromium prints the date of the conversion there, which no committed reference can hold.

`header-default-classes` pairs Gotenberg's `pages-12-html` with templates litho authored; `print-css-fit` links `print-css`'s files.
