# weasyprint corpus fixtures

WeasyPrint's draw tests, extracted from the release `WEASYPRINT_VERSION` in `corpus/versions.env` pins and scored by the `draw` feature (see `FEATURES.md`). Each test of WeasyPrint's `tests/draw/` calls `assert_pixels(expected, html)`: it renders the HTML and compares every pixel of the result with a grid of characters, one per pixel, each standing for a colour. The fixtures carry both, so litho's converter is held to the pixels WeasyPrint's authors wrote down by hand.

| Path         | What it holds                                                                                                                                                     |
| ------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `fixtures/`  | 315 JSON fixtures, `<module>__<test>.json`: the test name and source, whether WeasyPrint marks it `xfail`, the HTML, the expected rows, and the colour map        |
| `resources/` | The files of WeasyPrint's `tests/resources/` the fixtures name (images, SVG, stylesheets), and `tests_ua.css`, the user agent stylesheet the tests are written to |
| `ua.css`     | Prepended to every test by the feature: takes the margins a browser's user agent stylesheet adds back out (see the file and `FEATURES.md`)                        |
| `LICENSE`    | WeasyPrint's licence, copied from the release                                                                                                                     |

The colour map is `PIXELS_BY_CHAR` of `tests/draw/__init__.py`, read from the release rather than restated, copied into each fixture so one file says all a test expects. A character whose colour is `null` (`z`) marks a pixel the test does not check.

## Extraction

`make weasyprint-fixtures` runs `corpus select --corpus=weasyprint`, which downloads the GitHub source tarball of tag `v$WEASYPRINT_VERSION`, refuses it unless it hashes to `WEASYPRINT_SHA256`, and reads the release from it (`--checkout` names an unpacked release instead).

The command reads each `tests/draw/test_*.py`, enough of Python's grammar to find its assignments, test functions and calls, and keeps an `assert_pixels` call when both arguments are literal: a string constant, or a name bound once, in the module or the test function, to a string constant (WeasyPrint shares some grids between tests that way). A test with several calls yields one fixture per call, suffixed `_2`, `_3`. The test font, `weasyprint.otf`, goes to the gitignored font directory, checked against `WEASYPRINT_FONT_SHA256`; the other two formats of the same font are not used. The target then formats the fixtures with Prettier, which the repository's `make lint-prettier` holds JSON to.

Of the 372 `assert_pixels` calls of release 70.0, 315 became fixtures:

| Left out                                                                                                     | Calls | Modules                                                                                                                                                                   |
| ------------------------------------------------------------------------------------------------------------ | ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Computed grid or HTML: an f-string, a `%` format, a parameter of a parametrized test, a string built in code | 53    | `test_table` 24, `test_text` 8, `test_image` 6, `test_background` 5, `test_visibility` 4, `test_list` 2, `test_absolute`, `test_box`, `test_overflow`, `test_page` 1 each |
| Names a resource over 256 KiB (the CMYK ICC profile and the JPEG that embeds it)                             | 4     | `test_cmyk_color_profiles`                                                                                                                                                |

19 of the 315 fixtures are tests WeasyPrint itself marks `xfail`; their grids say what the authors expect, not what WeasyPrint draws, and they are scored like the others.

| Module                     | Fixtures | xfail |
| -------------------------- | -------- | ----- |
| `test_absolute`            | 24       | 1     |
| `test_background`          | 19       |       |
| `test_box`                 | 34       |       |
| `test_cmyk_color_profiles` | 2        |       |
| `test_column`              | 4        |       |
| `test_current_color`       | 8        | 1     |
| `test_float`               | 35       | 4     |
| `test_footnote`            | 11       |       |
| `test_footnote_column`     | 10       | 1     |
| `test_gradient`            | 28       |       |
| `test_image`               | 23       |       |
| `test_leader`              | 18       | 3     |
| `test_note`                | 14       | 3     |
| `test_overflow`            | 5        |       |
| `test_table`               | 17       | 1     |
| `test_text`                | 40       | 5     |
| `test_transform`           | 14       |       |
| `test_whitespace`          | 9        |       |

Many fixtures reach past the converter's first wave (tables, columns, footnotes, transforms, gradients, SVG images): they score as failures until it covers them, and the baseline records which pass.

## Known disagreements with Chromium

Chromium is the reference where WeasyPrint and Chromium draw a test differently, so these fixtures stay failing on purpose, recorded in `baseline.json`:

- `test_tables_20`: WeasyPrint and Chromium disagree on the table's layout.
- `test_background_repeat_round_2` and `_4`: Chromium positions the rounded tiles where litho does.
- `test_absolute_rtl_2`: an absolutely positioned box whose border box reaches past the first page takes a second page in Chromium (it prints two pages); WeasyPrint expects one.
- `test_text_float_text_decoration`, `test_text_nested_block_text_decoration`, `test_text_nested_text_decoration_color`: WeasyPrint marks these expected grids as failing in its own suite, and they contradict `test_text_nested_text_decoration`, which passes; litho follows the passing one.
- `test_text_underline`, `test_text_multiple_text_decoration`, `test_text_nested_text_decoration`: litho's HTML output draws an automatic underline as Chromium does, floor(size / 10) px thick with its top ceil(size / 20) px below the baseline (measured over 23 rows of 4 fonts at 13 to 64 px), whatever the font's own underline metrics say; WeasyPrint draws the font's metrics.
- `test_absolute_split_8`, `test_absolute_split_9`, `test_absolute_pages_counter`, `test_absolute_pages_counter_orphans`: an absolutely positioned box fragments where it lands, like in-flow content, as Chromium paginates (3, 2 and 3 pages measured for the first three); WeasyPrint moves the box whole (1, 1 and 2 pages).

## Licence

WeasyPrint is under the 3-Clause BSD License (`LICENSE`, copied from the release). The fixtures copy its tests' HTML and expected grids, and `resources/` copies files of its test suite, under that licence. The test font is not committed.
