# wpt corpus fixtures

Reftests from [web-platform-tests](https://github.com/web-platform-tests/wpt) at the commit `WPT_COMMIT` in `corpus/versions.env` pins, scored by the `reftest` feature (see `FEATURES.md`). Nothing from WPT is committed but the list of tests and the hashes of the files they read; `make corpus-fetch CORPUS=wpt` downloads the files into `test/integration/testdata/wpt/` (gitignored) and Ahem into `test/integration/testdata/fonts/ahem/`.

| File            | What it holds                                                                                                                                               |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `reftests.json` | The 1177 tests: each test's WPT path, whether it is a print reftest, its pages compared, its resources, and each reference with its relation and fuzziness  |
| `files.sha256`  | Every file the tests read (tests, references, their stylesheets and images, `LICENSE.md`, `fonts/Ahem.ttf`): 2082 lines of `<sha256>  <WPT path>`           |
| `corpus update` | The selection script: reads a WPT checkout and writes `reftests.json`                                                                                       |
| `corpus fetch`  | Downloads each listed file from `raw.githubusercontent.com` at the pinned commit, refuses one whose hash differs, and records the commit in `versions.json` |

## How the tests were chosen

`corpus update` walks these WPT directories and keeps a reftest when the test and every reference it links are plain HTML and CSS a static converter can draw:

- CSS 2.1 chapters: `css/CSS2/` `box-display`, `normal-flow`, `floats`, `floats-clear`, `margin-padding-clear`, `borders`, `text`, `lists`, `visuren`, `visudet`, `page-box`, `pagination`, `colors`, `backgrounds`. WPT has no `css/CSS2/page`; `page-box` and `pagination` hold its tests.
- CSS modules: `css/css-backgrounds/`, `css/css-text/`, `css/css-lists/`, `css/css-page/`, `css/css-break/`, `css/css-flexbox/`, `css/css-align/`, `css/css-grid/`, `css/css-multicol/`, `css/css-writing-modes/`, `css/css-overflow/`, `css/css-text-decor/` and `css/css-sizing/`, without their `flexbox`, `grid`, `table`, `multicol`, `writing-mode`, `subgrid`, `masonry`, `grid-lanes`, `aspect-ratio`, `contain-intrinsic-size`, `scroll-markers`, `responsive-iframe`, `forms`, `tools` and `test-plan` subdirectories (layout modes and features outside the converter's scope), and without `support`, `reference`, `resources`, `crashtests`, `animations`, `animation`, `invalidation`, `parsing`, `inheritance` and tentative tests.

The flexbox areas, `css-flexbox` and `css-align`, also leave out the tests that lean on what the converter does not lay out: grid, multi-column, the intrinsic sizing keywords, `aspect-ratio`, `visibility: collapse`, `display: contents`, containment, sticky positioning, `zoom`, the legacy `-webkit-box`, any `writing-mode`, anchor positioning, `<details>`, `<fieldset>`, and the balanced wrapping of CSS Flexbox 2 (`flex-wrap: balance`, `flex-line-count`). A `css-align` test qualifies only when it holds a flex container.

Six areas (`css-grid`, `css-multicol`, `css-writing-modes`, `css-overflow`, `css-text-decor`, `css-sizing`) leave out subgrid, masonry, `aspect-ratio`, `display: contents`, containment, sticky positioning, `zoom`, anchor positioning, `<details>`, `<fieldset>`, the balanced wrapping of CSS Flexbox 2, and any writing mode other than `horizontal-tb` (`text-orientation` and `text-combine-upright` with it). A `css-writing-modes` test qualifies only when it sets `direction: rtl`, `unicode-bidi` or a `dir` attribute of `rtl` or `auto`: the module's tests are nearly all vertical, so three remain.

A test qualifies when:

- it links one or more references with `<link rel="match">` or `<link rel="mismatch">`, and no reference links one of its own (11 chained references excluded);
- neither it nor a reference holds a script, SVG (inline or as an image), MathML, a form control, an embedded document, canvas or media, an `@font-face` rule, a `data:` or remote URL, an event handler attribute, a vertical writing mode, a `reftest-wait` class, or a CSS 2.1 `flags` value of `interact`, `userstyle`, `dom`, `font`, `svg`, `animated`, `nonHTML`, `speech` or `scroll`;
- an XHTML file (`.xht`) parses alike as HTML: no CDATA section, no self-closed element other than a void one. CSS 2.1 tests wrap their style sheets in `<![CDATA[ ]]>`, which an HTML parser hands to CSS as text that swallows the whole sheet; that rule excludes 2318 files, most of the CSS 2.1 suite;
- every stylesheet and image a file names resolves inside that file's own directory and exists. The feature converts each document with the file system rooted there, so a `../support/` image (145 CSS 2.1 tests use `../support/black96x96.png`) cannot be read. The one absolute URL allowed is `/fonts/ahem.css`, whose `@font-face` the converter cannot load: the corpus installs Ahem as a font instead.

That leaves 4324 candidates. Each area then keeps at most its cap, the candidates whose path hashes lowest (SHA-256), so the choice stays put when WPT adds or removes other tests: 24 per CSS 2.1 chapter, 80 from `css-backgrounds`, 120 from `css-text`, 40 each from `css-lists`, `css-page` and `css-break`, 200 from `css-flexbox`, 20 from `css-align`, 200 from `css-grid`, 80 from `css-multicol`, 60 from `css-writing-modes`, and 40 each from `css-overflow`, `css-text-decor` and `css-sizing`.

| Area                            | Tests | Print reftests |
| ------------------------------- | ----- | -------------- |
| `css/CSS2/backgrounds`          | 24    |                |
| `css/CSS2/borders`              | 24    |                |
| `css/CSS2/box-display`          | 24    |                |
| `css/CSS2/colors`               | 2     |                |
| `css/CSS2/floats`               | 24    |                |
| `css/CSS2/floats-clear`         | 24    |                |
| `css/CSS2/lists`                | 24    |                |
| `css/CSS2/margin-padding-clear` | 14    |                |
| `css/CSS2/normal-flow`          | 24    |                |
| `css/CSS2/pagination`           | 24    | 24             |
| `css/CSS2/text`                 | 24    |                |
| `css/CSS2/visudet`              | 4     |                |
| `css/CSS2/visuren`              | 13    |                |
| `css/css-backgrounds`           | 80    |                |
| `css/css-break`                 | 40    | 6              |
| `css/css-lists`                 | 40    |                |
| `css/css-page`                  | 40    | 40             |
| `css/css-text`                  | 120   |                |
| `css/css-flexbox`               | 200   | 2              |
| `css/css-align`                 | 5     |                |
| `css/css-grid`                  | 200   |                |
| `css/css-multicol`              | 80    | 2              |
| `css/css-writing-modes`         | 3     |                |
| `css/css-overflow`              | 40    |                |
| `css/css-text-decor`            | 40    |                |
| `css/css-sizing`                | 40    |                |
| Total                           | 1177  | 74             |

22 tests have a mismatch reference, 15 more than one reference, and 32 declare a fuzziness. Every fuzziness of the selection allows from 0 differences, so the cell, which checks only the upper bounds, decides no test otherwise than WPT would on them. One test, `css/css-page/subpixel-page-size-001-print.html`, declares `<meta name="reftest-pages" content="2">`: `corpus update` writes it as the entry's `pages`, and the cell compares only that page of the test with the reference, as WPT's runner does. 172 are XHTML files that parse as HTML.

To list the candidates again, at the pinned commit or a new one:

```bash
git init -q wpt && cd wpt
git remote add origin https://github.com/web-platform-tests/wpt.git
git config core.sparseCheckout true
printf '/css/\n/fonts/Ahem.ttf\n/LICENSE.md\n' > .git/info/sparse-checkout
git fetch -q --depth 1 --filter=blob:none origin "$WPT_COMMIT" && git checkout -q FETCH_HEAD
cd .. && make corpus-update CORPUS=wpt CHECKOUT=wpt
```

`corpus update` writes `reftests.json` and `files.sha256` and prints the candidate count on stderr. `files.sha256` is the SHA-256 of every path `reftests.json` names, plus `LICENSE.md` and `fonts/Ahem.ttf`, computed in the same checkout, sorted by path in byte order; `TestWPTListAndHashesAgree` checks that the two files agree.

## Known disagreements

Chromium is the reference where a test and Chromium draw differently, so these fixtures stay failing on purpose, recorded in `baseline.json`:

- `css/css-text/white-space/control-chars-00E`, `-00F`, `-080`, `-083`, `-089`, `-090`, `-095`, `-099`: the tests expect a control character to draw a visible glyph; Chromium's PDF draws nothing for one, and so does litho.
- Mismatch-reference tests whose own Chromium drawing differs from their `!=` reference by fewer pixels than the cell's 1% tolerance, so that no correct drawing can pass them (Chromium 9.0.0's test against its notref, in pixels): `css/CSS2/borders/ridge-default` (4400, the whole border), `css/CSS2/floats/float-in-nested-multicol-001` (140), `css/css-backgrounds/border-width-small-values-001-b` and `-e` (1604 each), `css/css-lists/list-style-position-001` (2209), `css/css-text-decor/text-decoration-inset-auto` (0: the pinned Chromium does not support the property), `css/css-text-decor/overline-wavy-covers-whole-line-length-001` (1164), `css/css-text-decor/text-decoration-skip-ink-004` (124), `css/css-text-decor/text-underline-offset-001` (320), `css/css-content/object-replacement-2` (3227). Where litho's drawing was wrong as well, it was fixed; the cell counts them as matching the mismatch reference.

## Licence

web-platform-tests is under the 3-Clause BSD License (its `LICENSE.md`, fetched with the tests); Ahem comes from the same tree, `fonts/Ahem.ttf`. No WPT file is committed.
