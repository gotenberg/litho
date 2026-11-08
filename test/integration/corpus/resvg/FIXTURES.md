# resvg corpus fixtures

SVG tests from [resvg](https://github.com/linebender/resvg) at the commit `RESVG_COMMIT` in `corpus/versions.env` pins (the v0.49.0 tag), scored by the `svgdraw` feature (see `FEATURES.md`). Nothing from resvg is committed but the list of tests and the hashes of the files they read; `make resvg-fetch` downloads the files into `test/integration/testdata/resvg/` (gitignored) and the fonts the text tests draw with into `test/integration/testdata/fonts/resvg/`. The downloaded files are data: nothing runs from there.

| File                 | What it holds                                                                                                                                                         |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `tests.json`         | The 246 tests: each SVG's path under `crates/resvg/tests/tests/`, its reference PNG and the PNG's size                                                                |
| `files.sha256`       | Every file the tests read (each SVG and PNG, the Noto Sans, Serif and Mono and Amiri fonts with their licences, resvg's MIT licence): 501 lines of `<sha256>  <path>` |
| `list-candidates.py` | The selection script: reads a resvg checkout and writes `tests.json` (standard library only)                                                                          |
| `fetch.sh`           | Downloads each listed file from `raw.githubusercontent.com` at the pinned commit, refuses one whose hash differs, and records the commit in `versions.json`           |

## How the tests were chosen

resvg's tests are SVG files with a PNG reference beside each, drawn by resvg scaled to 300 pixels wide (a few 500). `list-candidates.py` walks the directories of the features litho's SVG renderer draws: structure (`svg`, `g`, `defs`, `use`, `symbol`, `switch`, `systemLanguage`, `style`, `style-attribute`, `transform`), shapes (all seven), painting (fill and stroke with their properties, `opacity`, `display`, `visibility`, `color`), paint servers (`linearGradient`, `radialGradient`, `stop`, `stop-color`, `stop-opacity`), clipping (`clipPath`, `clip-rule`) and text basics (`text`, `tspan`, `text-anchor`, `font-size`). It leaves out the filters, masks, patterns, markers and the rest of text, which the renderer does not draw.

A test qualifies when it has a reference PNG and uses none of:

- a filter, a mask, a pattern, a marker, animation, a foreign object, text on a path, an image element, a script;
- an XML entity, an at-rule, a pseudo-class in a style element, a CSS function, a unit other than px, pt, pc, mm, cm, in, em, ex and %;
- context paint, blending, isolation, paint order, vector effects, CSS transforms, rendering hints, a gradient spread method other than pad;
- text length adjustment and rotation, spacing, decoration, baselines, font variants, stretch, kerning, features and variations, writing modes, direction, the font shorthand;
- a reference to a file outside itself;
- undefined behaviour: resvg marks a test whose rendering no specification decides with `(UB)` in its title.

That leaves 696 candidates. Each area then keeps at most its cap, the candidates whose path hashes lowest (SHA-256), so the choice stays put when resvg adds or removes other tests.

| Area                           | Tests |
| ------------------------------ | ----- |
| `masking/clip-rule`            | 1     |
| `masking/clipPath`             | 10    |
| `paint-servers/linearGradient` | 12    |
| `paint-servers/radialGradient` | 10    |
| `paint-servers/stop`           | 6     |
| `paint-servers/stop-color`     | 1     |
| `paint-servers/stop-opacity`   | 2     |
| `painting/color`               | 2     |
| `painting/display`             | 4     |
| `painting/fill`                | 10    |
| `painting/fill-opacity`        | 3     |
| `painting/fill-rule`           | 2     |
| `painting/opacity`             | 4     |
| `painting/stroke`              | 5     |
| `painting/stroke-dasharray`    | 6     |
| `painting/stroke-dashoffset`   | 3     |
| `painting/stroke-linecap`      | 3     |
| `painting/stroke-linejoin`     | 4     |
| `painting/stroke-miterlimit`   | 3     |
| `painting/stroke-opacity`      | 3     |
| `painting/stroke-width`        | 4     |
| `painting/visibility`          | 4     |
| `shapes/circle`                | 6     |
| `shapes/ellipse`               | 6     |
| `shapes/line`                  | 6     |
| `shapes/path`                  | 16    |
| `shapes/polygon`               | 4     |
| `shapes/polyline`              | 4     |
| `shapes/rect`                  | 12    |
| `structure/defs`               | 4     |
| `structure/g`                  | 2     |
| `structure/style`              | 10    |
| `structure/style-attribute`    | 3     |
| `structure/svg`                | 16    |
| `structure/switch`             | 6     |
| `structure/symbol`             | 8     |
| `structure/systemLanguage`     | 3     |
| `structure/transform`          | 12    |
| `structure/use`                | 10    |
| `text/font-size`               | 3     |
| `text/text`                    | 6     |
| `text/text-anchor`             | 3     |
| `text/tspan`                   | 4     |
| Total                          | 246   |

To list the candidates again, at the pinned commit or a new one:

```bash
git init -q resvg && cd resvg
git remote add origin https://github.com/linebender/resvg.git
git config core.sparseCheckout true
printf '/crates/resvg/tests/tests/\n/crates/resvg/tests/fonts/\n/LICENSE-MIT\n' > .git/info/sparse-checkout
git fetch -q --depth 1 --filter=blob:none origin "$RESVG_COMMIT" && git checkout -q FETCH_HEAD
cd .. && python3 -I test/integration/corpus/resvg/list-candidates.py resvg > test/integration/corpus/resvg/tests.json
npx prettier --write test/integration/corpus/resvg/tests.json
```

The script prints the candidate count and the commonest rejection reasons on stderr. `files.sha256` is the SHA-256 of every SVG and PNG `tests.json` names, plus `LICENSE-MIT` and the fonts `NotoSans-Regular.ttf`, `NotoSans-Bold.ttf`, `NotoSans-Italic.ttf`, `NotoSerif-Regular.ttf`, `NotoMono-Regular.ttf`, `Amiri-Regular.ttf` and their licences from `crates/resvg/tests/fonts/`, computed in the same checkout (`shasum -a 256`), sorted by path; `TestSVGListAndHashesAgree` checks that the two files agree.

## Known disagreements

The reference PNGs are resvg's own rasterisation, so these fixtures stay failing on purpose, recorded in `baseline.json`:

- `painting/fill/rgba-0-127-0-50percent.svg`: resvg's reference paints black, rejecting the percentage alpha of `rgba()` that litho honours (CSS Color 4).
- `painting/stroke-dasharray/negative-values.svg`, `structure/style/current-color-stroke-before-color.svg`, `structure/svg/preserveAspectRatio=xMinYMin.svg`: the differences are the anti-aliased edges of one- and two-unit strokes between the two rasterisers.
- `structure/svg/not-UTF-8-encoding.svg`: the reference draws a system font the fixture set lacks.

## Licence

resvg is under the MIT or the Apache 2.0 licence (its `LICENSE-MIT`, fetched with the tests). The Noto and Amiri fonts are under the SIL Open Font License 1.1 (`Noto-LICENSE-OFL.txt`, `Amiri-LICENSE-OFL.txt`). No resvg file is committed.
