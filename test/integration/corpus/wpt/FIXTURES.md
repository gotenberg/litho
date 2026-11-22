# wpt corpus fixtures

Reftests from [web-platform-tests](https://github.com/web-platform-tests/wpt) at the commit `WPT_COMMIT` in `corpus/versions.env` pins, scored by the `reftest` feature (see `FEATURES.md`). A screen test is drawn as WPT's screenshot shows it: the 800 by 600 pixel viewport, without the document's `@page` rules, and from one page several viewports tall when its content reaches past the viewport, so nothing is cut at the viewport's edge; a print test is drawn page by page on WPT's 5 by 3 inch page. Nothing from WPT is committed but the list of tests and the hashes of the files they read; `make corpus-fetch CORPUS=wpt` downloads the files into `test/integration/testdata/wpt/` (gitignored), the math fonts the MathML tests name among them at their WPT paths (`fonts/math/`), and Ahem into `test/integration/testdata/fonts/ahem/`.

| File            | What it holds                                                                                                                                                  |
| --------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `reftests.json` | The 6494 tests: each test's WPT path, whether it is a print reftest, its pages compared, its resources, and each reference with its relation and fuzziness     |
| `files.sha256`  | Every file the tests read (tests, references, their stylesheets, images and math fonts, `LICENSE.md`, `fonts/Ahem.ttf`): 10101 lines of `<sha256>  <WPT path>` |
| `corpus update` | The selection script: reads a WPT checkout and writes `reftests.json`                                                                                          |
| `corpus fetch`  | Downloads each listed file from `raw.githubusercontent.com` at the pinned commit, refuses one whose hash differs, and records the commit in `versions.json`    |

## How the tests were chosen

`corpus update` walks these WPT directories and keeps a reftest when the test and every reference it links are plain HTML and CSS a static converter can draw:

- CSS 2.1 chapters: `css/CSS2/` `box-display`, `normal-flow`, `floats`, `floats-clear`, `margin-padding-clear`, `borders`, `text`, `lists`, `visuren`, `visudet`, `page-box`, `pagination`, `colors`, `backgrounds`, `tables`, `fonts`, `linebox`, `positioning`, `abspos`, `generated-content`, `values`, `selectors`. WPT has no `css/CSS2/page`; `page-box` and `pagination` hold its tests.
- CSS modules: `css/css-backgrounds/`, `css/css-text/`, `css/css-lists/`, `css/css-page/`, `css/css-break/`, `css/css-flexbox/`, `css/css-align/`, `css/css-grid/`, `css/css-multicol/`, `css/css-writing-modes/`, `css/css-overflow/`, `css/css-text-decor/`, `css/css-sizing/`, `css/css-tables/`, `css/css-fonts/`, `css/css-inline/`, `css/css-position/`, `css/css-transforms/`, `css/css-ruby/`, `css/css-content/`, `css/css-counter-styles/`, `css/css-values/`, `css/css-variables/`, `css/css-color/`, `css/css-images/`, `css/css-display/`, `css/css-pseudo/`, `css/css-masking/`, `css/compositing/`, `css/css-logical/`, `css/css-box/`, `css/css-animations/`, `css/css-transitions/`, `css/css-shapes/`, `css/filter-effects/`, `css/css-conditional/`, `css/css-anchor-position/`, `css/motion/`, `css/fill-stroke/` and `css/selectors/`, without their `flexbox`, `grid`, `table`, `multicol`, `writing-mode`, `subgrid`, `masonry`, `grid-lanes`, `aspect-ratio`, `contain-intrinsic-size`, `scroll-markers`, `responsive-iframe`, `forms`, `tools` and `test-plan` subdirectories (layout modes and features outside the converter's scope), and without `support`, `reference`, `resources`, `crashtests`, `animations`, `animation`, `invalidation`, `parsing`, `inheritance` and tentative tests.
- MathML: each sub-directory of `mathml/presentation-markup/` (`direction`, `fractions`, `menclose`, `mpadded`, `mrow`, `operators`, `radicals`, `scripts`, `spaces`, `tables`, `tokens`) and `mathml/relations/`, whose sub-directories (`css-styling`, `html5-tree`, `text-and-math`) are its areas, under the same exclusions.
- Compatibility: `compat/`, an area whose tests must set `-webkit-text-fill-color` or `-webkit-text-stroke`.
- Form controls: `html/rendering/widgets/`, the area `html/rendering`, whose tests must set `field-sizing` and may hold the form controls every other area forbids.

The flexbox areas, `css-flexbox` and `css-align`, also leave out the tests that lean on what the converter does not lay out: grid, multi-column, the intrinsic sizing keywords, `aspect-ratio`, `visibility: collapse`, `display: contents`, containment, sticky positioning, `zoom`, the legacy `-webkit-box`, any `writing-mode`, anchor positioning, `<details>`, `<fieldset>`, and the balanced wrapping of CSS Flexbox 2 (`flex-wrap: balance`, `flex-line-count`). A `css-align` test qualifies only when it holds a flex container.

Eight areas (`css-grid`, `css-multicol`, `css-writing-modes`, `css-overflow`, `css-text-decor`, `css-sizing`, `css-box`, `css-shapes`) leave out subgrid, masonry, `aspect-ratio`, `display: contents`, containment, sticky positioning, `zoom`, anchor positioning, `<details>`, `<fieldset>`, the balanced wrapping of CSS Flexbox 2, and any writing mode other than `horizontal-tb` (`text-orientation` and `text-combine-upright` with it). A `css-writing-modes` test qualifies only when it sets `direction: rtl`, `unicode-bidi` or a `dir` attribute of `rtl` or `auto`: the module's tests are nearly all vertical, so three remain. `css-logical` keeps the same rules but takes a test in any writing mode, since its properties map to their sides by it.

The eight CSS 2.1 chapters from `tables` to `selectors` and the fifteen modules from `css-tables` to `selectors`, `css-masking`, `compositing`, `filter-effects` and `css-conditional` take a test with an `@font-face` rule: its fonts must resolve inside the test's directory as its other resources do, and the converter loads them. `css-transforms` leaves out 3D transforms (a perspective, `preserve-3d`, `backface-visibility`, `matrix3d`, and the `translate`, `rotate` and `scale` functions in 3D).

The MathML areas take a test that holds MathML and `@font-face` rules, and its `@font-face` rules may name WPT's math fonts by their absolute URLs (`/fonts/math/<file>`): such a font must exist, and it is listed as a resource of the file at its WPT path. The `reftest` cell converts a document that names one from WPT's origin and serves it the font (see `FEATURES.md`). Every other rule holds: no script, no `reftest-wait`, no SVG, no embedded document. Of the 282 files of the two directories that link a reference, 20 are not walked (13 `.xhtml` or `.xml` files, 6 tentative tests, 1 in a `writing-mode` directory), 119 run a script (59 of them also wait with `reftest-wait`), 3 hold SVG and 1 an `iframe`; 139 qualify.

Two probes single out a part of an area the area's rules leave out, capped on their own: the `css-writing-modes` tests that set a vertical or sideways `writing-mode` (`text-orientation` and `text-combine-upright` still left out), every one of them, since the converter lays vertical flows out, and 10 `css-transforms` tests with a 3D transform. A third probe keeps the `::first-line` tests of the CSS 2.1 `selectors` chapter, which its own 40 would leave out: the chapter's rules forbid `::first-line`, and the probe takes its 10 candidates. `css-ruby` keeps 10 tests for the same reason: the converter draws a ruby annotation inline. A few tests say a feature is missing as well as many would. A fourth probe walks the `aspect-ratio` subdirectory of `css-sizing`, which the area skips, and keeps the tests that set `aspect-ratio`, which the area's rules forbid, under the area's other rules.

A test qualifies when:

- it links one or more references with `<link rel="match">` or `<link rel="mismatch">`, every reference lies in a directory walked or in `css/reference/` or `css/CSS2/reference/` (so the choice does not depend on what else a checkout holds: a checkout needs those directories, `fonts/math/`, `LICENSE.md` and `fonts/Ahem.ttf`), and no reference links one of its own (a chained reference keeps the test out);
- neither it nor a reference holds a script, SVG (inline or as an image), MathML (outside the MathML areas), a form control (outside `html/rendering`), an embedded document, canvas or media, an `@font-face` rule (outside the areas above that take one and the MathML areas), a `data:` or remote URL, an event handler attribute, a vertical writing mode (outside the probe and `css-logical`), a `reftest-wait` class, or a CSS 2.1 `flags` value of `interact`, `userstyle`, `dom`, `font`, `svg`, `animated`, `nonHTML`, `speech` or `scroll`;
- an XHTML file (`.xht`) parses alike as HTML once the markers of the CDATA sections in its style elements are stripped: a section anywhere else (a script, the title, the body), a marker that opens or closes nothing, a self-closed element other than a void one, text in the head (which an HTML parser moves into the body) or a `pre` element that opens on a line feed (which an HTML parser drops) keeps it out. CSS 2.1 tests wrap their style sheets in `<![CDATA[ ]]>`, which an HTML parser hands to CSS as text that swallows the whole sheet; the `reftest` cell strips those markers as it reads a test or a reference, an XML parser's reading of the sheet, and leaves the fetched files as WPT holds them;
- every stylesheet and image a file names resolves inside that file's own directory and exists. The feature converts each document with the file system rooted there, so a `../support/` image (145 CSS 2.1 tests use `../support/black96x96.png`) cannot be read. Two absolute URLs are allowed: `/fonts/ahem.css`, whose `@font-face` the converter does not load, since the corpus installs Ahem as a font instead, and, in the MathML areas, a math font under `/fonts/math/`.

That leaves 13014 candidates. Each area and each probe then keeps at most its cap, the candidates whose path hashes lowest (SHA-256), so the choice stays put when WPT adds or removes other tests: 24 per CSS 2.1 chapter but 100 from `tables`, 60 from `generated-content` and 40 each from `fonts`, `positioning` and `selectors`; 80 from `css-backgrounds`, 120 from `css-text`, 40 each from `css-lists`, `css-page` and `css-break`, 200 from `css-flexbox`, 20 from `css-align`, 200 from `css-grid`, 80 from `css-multicol`, 60 from `css-writing-modes`, 40 each from `css-overflow` and `css-text-decor`, 80 from `css-sizing`; 80 from `css-tables`, 100 from `css-transforms`, every candidate of `css-fonts`, 120 from `css-inline`, 60 each from `css-position`, `css-values`, `css-variables`, `css-color`, `css-images` and `css-display`, 100 each from `css-pseudo` and `selectors`, 40 each from `css-content`, `css-counter-styles`, `css-logical`, `css-box`, `css-animations` and `css-transitions`, 200 from `css-shapes`, 120 from `css-masking`, 60 from `compositing`, 400 from `filter-effects`, 200 from `css-conditional` 120 from `css-anchor-position` and 200 each from `css/motion`, `css/fill-stroke`, `compat` and `html/rendering`, which keep every candidate of theirs, and 10 from `css-ruby` and from each probe but `aspect-ratio`'s, which keeps 60, and the vertical writing modes', which keeps all 784 of its candidates; 100 from each MathML area, which keeps every candidate. A test whose file or a reference's had its markers stripped is capped apart, in a group of its own per area and probe, so that admitting such tests left the choice among the others as it was: 100 per CSS 2.1 chapter, every candidate of the `aspect-ratio` probe, and in any other area or probe as many as its own cap. `css-animations` keeps all 14 of its candidates; every reftest of `css-transitions` runs a script, and the area keeps none. `corpus update` prints the candidate count and the five rules that kept the most test files out.

| Area                                   | Tests | Print reftests |
| -------------------------------------- | ----- | -------------- |
| `css/CSS2/abspos`                      | 25    |                |
| `css/CSS2/backgrounds`                 | 124   |                |
| `css/CSS2/borders`                     | 124   |                |
| `css/CSS2/box-display`                 | 62    |                |
| `css/CSS2/colors`                      | 19    |                |
| `css/CSS2/floats`                      | 40    |                |
| `css/CSS2/floats-clear`                | 124   |                |
| `css/CSS2/fonts`                       | 76    |                |
| `css/CSS2/generated-content`           | 105   |                |
| `css/CSS2/linebox`                     | 124   |                |
| `css/CSS2/lists`                       | 30    |                |
| `css/CSS2/margin-padding-clear`        | 114   |                |
| `css/CSS2/normal-flow`                 | 124   |                |
| `css/CSS2/pagination`                  | 24    | 24             |
| `css/CSS2/positioning`                 | 140   |                |
| `css/CSS2/selectors`                   | 67    |                |
| `css/CSS2/tables`                      | 200   |                |
| `css/CSS2/text`                        | 91    |                |
| `css/CSS2/values`                      | 14    |                |
| `css/CSS2/visudet`                     | 19    |                |
| `css/CSS2/visuren`                     | 26    |                |
| `compat`                               | 6     |                |
| `css/compositing`                      | 36    |                |
| `css/css-align`                        | 7     |                |
| `css/css-anchor-position`              | 105   | 1              |
| `css/css-animations`                   | 14    |                |
| `css/css-backgrounds`                  | 123   |                |
| `css/css-box`                          | 36    |                |
| `css/css-break`                        | 80    | 6              |
| `css/css-color`                        | 107   |                |
| `css/css-conditional`                  | 139   |                |
| `css/css-content`                      | 40    |                |
| `css/css-counter-styles`               | 40    |                |
| `css/css-display`                      | 110   |                |
| `css/css-flexbox`                      | 302   | 2              |
| `css/css-fonts`                        | 260   | 1              |
| `css/css-grid`                         | 310   |                |
| `css/css-images`                       | 60    |                |
| `css/css-inline`                       | 115   | 3              |
| `css/css-lists`                        | 40    |                |
| `css/css-logical`                      | 5     |                |
| `css/css-masking`                      | 108   |                |
| `css/css-multicol`                     | 160   | 2              |
| `css/css-overflow`                     | 65    |                |
| `css/css-page`                         | 40    | 40             |
| `css/css-position`                     | 84    | 1              |
| `css/css-pseudo`                       | 83    |                |
| `css/css-ruby`                         | 11    |                |
| `css/css-shapes`                       | 160   |                |
| `css/css-sizing`                       | 313   |                |
| `css/css-tables`                       | 99    | 2              |
| `css/css-text`                         | 184   |                |
| `css/css-text-decor`                   | 50    |                |
| `css/css-transforms`                   | 126   |                |
| `css/css-values`                       | 80    |                |
| `css/css-variables`                    | 62    |                |
| `css/css-writing-modes`                | 797   |                |
| `css/fill-stroke`                      | 1     |                |
| `css/filter-effects`                   | 137   |                |
| `css/motion`                           | 77    |                |
| `css/selectors`                        | 110   |                |
| `html/rendering`                       | 1     |                |
| `mathml/presentation-markup/direction` | 2     |                |
| `mathml/presentation-markup/fractions` | 5     |                |
| `mathml/presentation-markup/mpadded`   | 5     |                |
| `mathml/presentation-markup/mrow`      | 5     |                |
| `mathml/presentation-markup/operators` | 32    |                |
| `mathml/presentation-markup/scripts`   | 33    |                |
| `mathml/presentation-markup/spaces`    | 4     |                |
| `mathml/presentation-markup/tables`    | 6     |                |
| `mathml/relations/css-styling`         | 38    |                |
| `mathml/relations/html5-tree`          | 7     |                |
| `mathml/relations/text-and-math`       | 2     |                |
| Total                                  | 6494  | 82             |

130 tests have a mismatch reference, 51 more than one reference, and 266 declare a fuzziness. The cell applies a fuzziness as WPT's runner does, lower bounds included: seven `css-images` gradient tests (`display-p3-linear-gradient`, `gradient-eval-001`, `-003`, `-006` and `-009`, `gradient-none-interpolation`, `gradient-powerless-hue-lch`) and `css-masking/mask-image/mask-opacity-1c` declare a maximum difference from 1, which a drawing within the range must reach; one identical to its reference passes, since their pixel ranges start at 0. Three print tests declare `<meta name="reftest-pages">`: `css/css-page/subpixel-page-size-001-print.html` (page 2) and `css/css-tables/collapsed-border-fragmentation-001-print.html` and `-002-print.html` (pages 1 and 2). `corpus update` writes it as the entry's `pages`, and the cell compares only those pages of the test with the reference, as WPT's runner does. 2507 are XHTML files that parse as HTML.

To list the candidates again, at the pinned commit or a new one:

```bash
git init -q wpt && cd wpt
git remote add origin https://github.com/web-platform-tests/wpt.git
git config core.sparseCheckout true
printf '/css/\n/mathml/\n/compat/\n/html/rendering/widgets/\n/fonts/Ahem.ttf\n/fonts/math/\n/LICENSE.md\n' > .git/info/sparse-checkout
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
- `compat/webkit-text-fill-color-property-005`: the test expects an underline of currentcolor in the element's color under a red `-webkit-text-fill-color`; the pinned Chromium draws it in the fill colour, on screen and in print, as it draws a currentcolor decoration in the stroke colour of stroked text, and so does litho.
- `css/CSS2/generated-content/content-counter-007`: the test expects a counter an element resets to reach its following siblings, as CSS 2.1, 12.4.1 scopes it; the pinned Chromium inherits counters as CSS Lists 3, 4.4.1 does, where a sibling's counter does not shadow the parent's, and so does litho.

## Licence

web-platform-tests is under the 3-Clause BSD License (its `LICENSE.md`, fetched with the tests); Ahem comes from the same tree, `fonts/Ahem.ttf`. No WPT file is committed.
