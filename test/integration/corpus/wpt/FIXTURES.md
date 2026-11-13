# wpt corpus fixtures

Reftests from [web-platform-tests](https://github.com/web-platform-tests/wpt) at the commit `WPT_COMMIT` in `corpus/versions.env` pins, scored by the `reftest` feature (see `FEATURES.md`). Nothing from WPT is committed but the list of tests and the hashes of the files they read; `make corpus-fetch CORPUS=wpt` downloads the files into `test/integration/testdata/wpt/` (gitignored) and Ahem into `test/integration/testdata/fonts/ahem/`.

| File            | What it holds                                                                                                                                               |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `reftests.json` | The 2854 tests: each test's WPT path, whether it is a print reftest, its pages compared, its resources, and each reference with its relation and fuzziness  |
| `files.sha256`  | Every file the tests read (tests, references, their stylesheets and images, `LICENSE.md`, `fonts/Ahem.ttf`): 4918 lines of `<sha256>  <WPT path>`           |
| `corpus update` | The selection script: reads a WPT checkout and writes `reftests.json`                                                                                       |
| `corpus fetch`  | Downloads each listed file from `raw.githubusercontent.com` at the pinned commit, refuses one whose hash differs, and records the commit in `versions.json` |

## How the tests were chosen

`corpus update` walks these WPT directories and keeps a reftest when the test and every reference it links are plain HTML and CSS a static converter can draw:

- CSS 2.1 chapters: `css/CSS2/` `box-display`, `normal-flow`, `floats`, `floats-clear`, `margin-padding-clear`, `borders`, `text`, `lists`, `visuren`, `visudet`, `page-box`, `pagination`, `colors`, `backgrounds`, `tables`, `fonts`, `linebox`, `positioning`, `abspos`, `generated-content`, `values`, `selectors`. WPT has no `css/CSS2/page`; `page-box` and `pagination` hold its tests.
- CSS modules: `css/css-backgrounds/`, `css/css-text/`, `css/css-lists/`, `css/css-page/`, `css/css-break/`, `css/css-flexbox/`, `css/css-align/`, `css/css-grid/`, `css/css-multicol/`, `css/css-writing-modes/`, `css/css-overflow/`, `css/css-text-decor/`, `css/css-sizing/`, `css/css-tables/`, `css/css-fonts/`, `css/css-inline/`, `css/css-position/`, `css/css-transforms/`, `css/css-ruby/`, `css/css-content/`, `css/css-counter-styles/`, `css/css-values/`, `css/css-variables/`, `css/css-color/`, `css/css-images/`, `css/css-display/`, `css/css-pseudo/`, `css/css-masking/`, `css/compositing/`, `css/css-logical/`, `css/css-box/` and `css/selectors/`, without their `flexbox`, `grid`, `table`, `multicol`, `writing-mode`, `subgrid`, `masonry`, `grid-lanes`, `aspect-ratio`, `contain-intrinsic-size`, `scroll-markers`, `responsive-iframe`, `forms`, `tools` and `test-plan` subdirectories (layout modes and features outside the converter's scope), and without `support`, `reference`, `resources`, `crashtests`, `animations`, `animation`, `invalidation`, `parsing`, `inheritance` and tentative tests.

The flexbox areas, `css-flexbox` and `css-align`, also leave out the tests that lean on what the converter does not lay out: grid, multi-column, the intrinsic sizing keywords, `aspect-ratio`, `visibility: collapse`, `display: contents`, containment, sticky positioning, `zoom`, the legacy `-webkit-box`, any `writing-mode`, anchor positioning, `<details>`, `<fieldset>`, and the balanced wrapping of CSS Flexbox 2 (`flex-wrap: balance`, `flex-line-count`). A `css-align` test qualifies only when it holds a flex container.

Seven areas (`css-grid`, `css-multicol`, `css-writing-modes`, `css-overflow`, `css-text-decor`, `css-sizing`, `css-box`) leave out subgrid, masonry, `aspect-ratio`, `display: contents`, containment, sticky positioning, `zoom`, anchor positioning, `<details>`, `<fieldset>`, the balanced wrapping of CSS Flexbox 2, and any writing mode other than `horizontal-tb` (`text-orientation` and `text-combine-upright` with it). A `css-writing-modes` test qualifies only when it sets `direction: rtl`, `unicode-bidi` or a `dir` attribute of `rtl` or `auto`: the module's tests are nearly all vertical, so three remain. `css-logical` keeps the same rules but takes a test in any writing mode, since its properties map to their sides by it.

The eight CSS 2.1 chapters from `tables` to `selectors` and the fifteen modules from `css-tables` to `selectors`, `css-masking` and `compositing` take a test with an `@font-face` rule: its fonts must resolve inside the test's directory as its other resources do, and the converter loads them. `css-transforms` leaves out 3D transforms (a perspective, `preserve-3d`, `backface-visibility`, `matrix3d`, and the `translate`, `rotate` and `scale` functions in 3D).

Two probes single out a part of an area the converter does not lay out, capped on their own: 10 `css-writing-modes` tests that set a vertical or sideways `writing-mode` (`text-orientation` and `text-combine-upright` still left out), and 10 `css-transforms` tests with a 3D transform. A third probe keeps the `::first-line` tests of the CSS 2.1 `selectors` chapter, which its own 40 would leave out: the chapter's rules forbid `::first-line`, and the probe takes its 10 candidates. `css-ruby` keeps 10 tests for the same reason: the converter draws a ruby annotation inline. A few tests say a feature is missing as well as many would. A fourth probe walks the `aspect-ratio` subdirectory of `css-sizing`, which the area skips, and keeps the tests that set `aspect-ratio`, which the area's rules forbid, under the area's other rules.

A test qualifies when:

- it links one or more references with `<link rel="match">` or `<link rel="mismatch">`, and no reference links one of its own (a chained reference keeps the test out);
- neither it nor a reference holds a script, SVG (inline or as an image), MathML, a form control, an embedded document, canvas or media, an `@font-face` rule (outside the areas above that take one), a `data:` or remote URL, an event handler attribute, a vertical writing mode (outside the probe and `css-logical`), a `reftest-wait` class, or a CSS 2.1 `flags` value of `interact`, `userstyle`, `dom`, `font`, `svg`, `animated`, `nonHTML`, `speech` or `scroll`;
- an XHTML file (`.xht`) parses alike as HTML: no CDATA section, no self-closed element other than a void one. CSS 2.1 tests wrap their style sheets in `<![CDATA[ ]]>`, which an HTML parser hands to CSS as text that swallows the whole sheet; that rule keeps out most of the CSS 2.1 suite;
- every stylesheet and image a file names resolves inside that file's own directory and exists. The feature converts each document with the file system rooted there, so a `../support/` image (145 CSS 2.1 tests use `../support/black96x96.png`) cannot be read. The one absolute URL allowed is `/fonts/ahem.css`, whose `@font-face` the converter cannot load: the corpus installs Ahem as a font instead.

That leaves 7602 candidates. Each area and each probe then keeps at most its cap, the candidates whose path hashes lowest (SHA-256), so the choice stays put when WPT adds or removes other tests: 24 per CSS 2.1 chapter but 100 from `tables`, 60 from `generated-content` and 40 each from `fonts`, `positioning` and `selectors`; 80 from `css-backgrounds`, 120 from `css-text`, 40 each from `css-lists`, `css-page` and `css-break`, 200 from `css-flexbox`, 20 from `css-align`, 200 from `css-grid`, 80 from `css-multicol`, 60 from `css-writing-modes`, 40 each from `css-overflow` and `css-text-decor`, 80 from `css-sizing`; 80 from `css-tables`, 100 from `css-transforms`, every candidate of `css-fonts`, 60 each from `css-inline`, `css-position`, `css-values`, `css-variables`, `css-color`, `css-images` and `css-display`, 100 each from `css-pseudo` and `selectors`, 40 each from `css-content`, `css-counter-styles`, `css-logical` and `css-box`, 120 from `css-masking` and 60 from `compositing`, which keep every candidate of theirs, and 10 from `css-ruby` and from each probe but `aspect-ratio`'s, which keeps 60. `corpus update` prints the candidate count and the five rules that kept the most test files out.

| Area                            | Tests | Print reftests |
| ------------------------------- | ----- | -------------- |
| `css/CSS2/abspos`               | 19    |                |
| `css/CSS2/backgrounds`          | 24    |                |
| `css/CSS2/borders`              | 24    |                |
| `css/CSS2/box-display`          | 24    |                |
| `css/CSS2/colors`               | 2     |                |
| `css/CSS2/floats`               | 24    |                |
| `css/CSS2/floats-clear`         | 24    |                |
| `css/CSS2/fonts`                | 40    |                |
| `css/CSS2/generated-content`    | 60    |                |
| `css/CSS2/linebox`              | 24    |                |
| `css/CSS2/lists`                | 24    |                |
| `css/CSS2/margin-padding-clear` | 14    |                |
| `css/CSS2/normal-flow`          | 24    |                |
| `css/CSS2/pagination`           | 24    | 24             |
| `css/CSS2/positioning`          | 40    |                |
| `css/CSS2/selectors`            | 50    |                |
| `css/CSS2/tables`               | 100   |                |
| `css/CSS2/text`                 | 24    |                |
| `css/CSS2/values`               | 7     |                |
| `css/CSS2/visudet`              | 4     |                |
| `css/CSS2/visuren`              | 13    |                |
| `css/compositing`               | 35    |                |
| `css/css-align`                 | 5     |                |
| `css/css-backgrounds`           | 80    |                |
| `css/css-box`                   | 13    |                |
| `css/css-break`                 | 40    | 6              |
| `css/css-color`                 | 60    |                |
| `css/css-content`               | 40    |                |
| `css/css-counter-styles`        | 40    |                |
| `css/css-display`               | 56    |                |
| `css/css-flexbox`               | 200   | 2              |
| `css/css-fonts`                 | 256   | 1              |
| `css/css-grid`                  | 200   |                |
| `css/css-images`                | 60    |                |
| `css/css-inline`                | 60    | 1              |
| `css/css-lists`                 | 40    |                |
| `css/css-logical`               | 5     |                |
| `css/css-masking`               | 107   |                |
| `css/css-multicol`              | 80    | 2              |
| `css/css-overflow`              | 40    |                |
| `css/css-page`                  | 40    | 40             |
| `css/css-position`              | 60    | 1              |
| `css/css-pseudo`                | 82    |                |
| `css/css-ruby`                  | 10    |                |
| `css/css-sizing`                | 83    |                |
| `css/css-tables`                | 75    | 2              |
| `css/css-text`                  | 120   |                |
| `css/css-text-decor`            | 40    |                |
| `css/css-transforms`            | 110   |                |
| `css/css-values`                | 60    |                |
| `css/css-variables`             | 60    |                |
| `css/css-writing-modes`         | 13    |                |
| `css/selectors`                 | 95    |                |
| Total                           | 2854  | 79             |

93 tests have a mismatch reference, 51 more than one reference, and 137 declare a fuzziness. The cell applies a fuzziness as WPT's runner does, lower bounds included: seven `css-images` gradient tests (`display-p3-linear-gradient`, `gradient-eval-001`, `-003`, `-006` and `-009`, `gradient-none-interpolation`, `gradient-powerless-hue-lch`) and `css-masking/mask-image/mask-opacity-1c` declare a maximum difference from 1, which a drawing within the range must reach; one identical to its reference passes, since their pixel ranges start at 0. Three print tests declare `<meta name="reftest-pages">`: `css/css-page/subpixel-page-size-001-print.html` (page 2) and `css/css-tables/collapsed-border-fragmentation-001-print.html` and `-002-print.html` (pages 1 and 2). `corpus update` writes it as the entry's `pages`, and the cell compares only those pages of the test with the reference, as WPT's runner does. 531 are XHTML files that parse as HTML.

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

- `css/css-text-decor/text-decoration-inset-auto`: the pinned Chromium does not support the property, so its drawing equals the test's `!=` reference, and so does litho's.
- `css/CSS2/selectors/first-letter-quote-002`: the test joins the open quote of a `::before` to the letter after it in `::first-letter`; the pinned Chromium takes the quote alone, and so does litho.
- `css/css-content/quotes-match-parent-001`: the pinned Chromium does not parse `quotes: match-parent`, a value only the editor's draft holds, and draws each level in the marks of its parent's language; so does litho.
- `css/css-counter-styles/armenian/css3-counter-styles-008` and `lower-armenian/css3-counter-styles-114`: the tests expect decimal past 9999, where CSS Counter Styles 3 ends the Armenian styles; the pinned Chromium spells 10000 to 99999999 with U+0302 marking ten thousands, and so does litho.
- `css/selectors/selectors-4/lang-002`, `-003`, `-004`, `-011`, `-012`, `-020`, `-022`, `-023`, `-024` and `-025`: the tests write `:lang()` with a string or a list of ranges, as Selectors 4 allows; the pinned Chromium parses one identifier alone and drops those rules, and so does litho.
- `css/CSS2/selectors/first-line-floats-002`: the test expects a float inside an inline box on the first line to inherit from `::first-line`; the pinned Chromium keeps the float's own style, and so does litho.
- `css/CSS2/generated-content/content-counter-007`: the test expects a counter an element resets to reach its following siblings, as CSS 2.1, 12.4.1 scopes it; the pinned Chromium inherits counters as CSS Lists 3, 4.4.1 does, where a sibling's counter does not shadow the parent's, and so does litho.

## Licence

web-platform-tests is under the 3-Clause BSD License (its `LICENSE.md`, fetched with the tests); Ahem comes from the same tree, `fonts/Ahem.ttf`. No WPT file is committed.
