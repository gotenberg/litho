# wpt corpus fixtures

Reftests from [web-platform-tests](https://github.com/web-platform-tests/wpt) at the commit `WPT_COMMIT` in `corpus/versions.env` pins, scored by the `reftest` feature (see `FEATURES.md`). Nothing from WPT is committed but the list of tests and the hashes of the files they read; `make wpt-fetch` downloads the files into `test/integration/testdata/wpt/` (gitignored) and Ahem into `test/integration/testdata/fonts/ahem/`.

| File                 | What it holds                                                                                                                                               |
| -------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `reftests.json`      | The 569 tests: each test's WPT path, whether it is a print reftest, its pages compared, its resources, and each reference with its relation and fuzziness   |
| `files.sha256`       | Every file the tests read (tests, references, their stylesheets and images, `LICENSE.md`, `fonts/Ahem.ttf`): 1009 lines of `<sha256>  <WPT path>`           |
| `list-candidates.py` | The selection script: reads a WPT checkout and writes `reftests.json` (standard library only)                                                               |
| `fetch.sh`           | Downloads each listed file from `raw.githubusercontent.com` at the pinned commit, refuses one whose hash differs, and records the commit in `versions.json` |

## How the tests were chosen

`list-candidates.py` walks these WPT directories and keeps a reftest when the test and every reference it links are plain HTML and CSS a static converter can draw:

- CSS 2.1 chapters: `css/CSS2/` `box-display`, `normal-flow`, `floats`, `floats-clear`, `margin-padding-clear`, `borders`, `text`, `lists`, `visuren`, `visudet`, `page-box`, `pagination`, `colors`, `backgrounds`. WPT has no `css/CSS2/page`; `page-box` and `pagination` hold its tests.
- CSS modules: `css/css-backgrounds/`, `css/css-text/`, `css/css-lists/`, `css/css-page/`, `css/css-break/`, without their `flexbox`, `grid`, `table`, `multicol` and `writing-mode` subdirectories (layout modes outside the converter's first wave), and without `support`, `reference`, `resources`, `crashtests`, `animations`, `parsing`, `inheritance` and tentative tests.

A test qualifies when:

- it links one or more references with `<link rel="match">` or `<link rel="mismatch">`, and no reference links one of its own (11 chained references excluded);
- neither it nor a reference holds a script, SVG (inline or as an image), MathML, a form control, an embedded document, canvas or media, an `@font-face` rule, a `data:` or remote URL, an event handler attribute, a vertical writing mode, a `reftest-wait` class, or a CSS 2.1 `flags` value of `interact`, `userstyle`, `dom`, `font`, `svg`, `animated`, `nonHTML`, `speech` or `scroll`;
- an XHTML file (`.xht`) parses alike as HTML: no CDATA section, no self-closed element other than a void one. CSS 2.1 tests wrap their style sheets in `<![CDATA[ ]]>`, which an HTML parser hands to CSS as text that swallows the whole sheet; that rule excludes 2318 files, most of the CSS 2.1 suite;
- every stylesheet and image a file names resolves inside that file's own directory and exists. The feature converts each document with the file system rooted there, so a `../support/` image (145 CSS 2.1 tests use `../support/black96x96.png`) cannot be read. The one absolute URL allowed is `/fonts/ahem.css`, whose `@font-face` the converter cannot load: the corpus installs Ahem as a font instead.

That leaves 2860 candidates. Each area then keeps at most its cap, the candidates whose path hashes lowest (SHA-256), so the choice stays put when WPT adds or removes other tests: 24 per CSS 2.1 chapter, 80 from `css-backgrounds`, 120 from `css-text`, 40 each from `css-lists`, `css-page` and `css-break`.

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
| Total                           | 569   | 70             |

17 tests have a mismatch reference, 14 more than one reference, and 18 declare a fuzziness. Every fuzziness of the selection allows from 0 differences, so the cell, which checks only the upper bounds, decides no test otherwise than WPT would on them. One test, `css/css-page/subpixel-page-size-001-print.html`, declares `<meta name="reftest-pages" content="2">`: `list-candidates.py` writes it as the entry's `pages`, and the cell compares only that page of the test with the reference, as WPT's runner does. 167 are XHTML files that parse as HTML.

To list the candidates again, at the pinned commit or a new one:

```bash
git init -q wpt && cd wpt
git remote add origin https://github.com/web-platform-tests/wpt.git
git config core.sparseCheckout true
printf '/css/\n/fonts/Ahem.ttf\n/LICENSE.md\n' > .git/info/sparse-checkout
git fetch -q --depth 1 --filter=blob:none origin "$WPT_COMMIT" && git checkout -q FETCH_HEAD
cd .. && python3 -I test/integration/corpus/wpt/list-candidates.py wpt > test/integration/corpus/wpt/reftests.json
npx prettier --write test/integration/corpus/wpt/reftests.json
```

The script prints the candidate count and the commonest rejection reasons on stderr. `files.sha256` is the SHA-256 of every path `reftests.json` names, plus `LICENSE.md` and `fonts/Ahem.ttf`, computed in the same checkout (`shasum -a 256`), sorted by path; `TestWPTListAndHashesAgree` checks that the two files agree.

## Licence

web-platform-tests is under the 3-Clause BSD License (its `LICENSE.md`, fetched with the tests); Ahem comes from the same tree, `fonts/Ahem.ttf`. No WPT file is committed.
