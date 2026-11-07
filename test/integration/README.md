# Integration Tests

Matrix runner that scores litho across (corpus, feature) cells. Every corpus supplies PDF fixtures, or, for the six converter corpora, the HTML, SVG, DOCX and ODT sources litho converts; every feature either loads a captured golden or runs live against an external tool.

## Layout

```
test/integration/
├── corpus/                     Shared provisioning image + per-corpus scripts
│   ├── versions.env            Pinned version of every tool and corpus source
│   ├── Dockerfile              One image: Node + PDF.js + JRE + veraPDF + pdfcpu + Mustang + KoSIT
│   ├── entrypoint.sh           Dispatches to <corpus>/download.sh then shared goldens
│   ├── lib/
│   │   ├── fetch-revision.sh   Checks out one pinned commit or tag of a git repository
│   │   ├── gen-references.mjs  PDF.js text + metadata generator (shared)
│   │   ├── join-lines.mjs      Joins PDF.js text items into golden lines (node --test join-lines.test.mjs)
│   │   ├── gen-images.mjs      PDF.js image signature generator (shared)
│   │   ├── gen-links.mjs       PDF.js link annotation lister (html diff goldens)
│   │   ├── gen-render.sh       poppler page renderer (render and diff goldens)
│   │   ├── gen-bookmarks.sh    pdfcpu bookmark exporter (shared)
│   │   ├── gen-merges.sh       pdfcpu merge oracle (shared)
│   │   ├── gen-merge-empty.sh  pdfcpu merge-with-empty oracle (shared)
│   │   ├── run-verapdf.sh      veraPDF per-profile runner (shared)
│   │   ├── run-mustang.sh      Mustang Factur-X validity runner (shared)
│   │   ├── run-kosit.sh        KoSIT Schematron runner (shared)
│   │   ├── list-office-candidates.py  Selects the docx and odt corpora's LibreOffice test documents
│   │   ├── fetch-office.sh     Downloads and hash-checks them (make docx-fetch, make odt-fetch)
│   │   ├── libreoffice.sh      Builds their LibreOffice references on the host (make docx-libreoffice)
│   │   └── install-references.sh  Installs those references as the corpus PDFs (docx, odt download.sh)
│   ├── pdfjs/
│   │   ├── download.sh
│   │   ├── flatten-scope.json  Widens the flatten cell to the whole corpus
│   │   └── manifest.json
│   ├── verapdf/
│   │   ├── download.sh
│   │   └── manifest.json
│   ├── govdocs/
│   │   ├── download.sh
│   │   ├── flatten-scope.json  Widens the flatten cell to the whole corpus
│   │   ├── manifest.json
│   │   └── text-overrides.json Corrections to the PDF.js text goldens, with reasons
│   ├── pdf20/
│   │   ├── download.sh
│   │   └── manifest.json
│   ├── pdfcpu/
│   │   ├── download.sh
│   │   └── manifest.json
│   ├── facturx/
│   │   ├── download.sh
│   │   └── manifest.json
│   ├── litho/                  Self-authored fixtures, committed (not downloaded)
│   │   ├── download.sh         Copies pdfs/ into the output dir
│   │   ├── manifest.json
│   │   ├── text-overrides.json Corrections to the PDF.js text goldens, with reasons
│   │   ├── build/*.py          pikepdf builders that regenerate the fixtures
│   │   ├── pdfs/*.pdf
│   │   └── FIXTURES.md         Per-fixture expected firing set
│   ├── compose/                PDFs litho converts from text sources, committed
│   │   ├── download.sh         Copies pdfs/ into the output dir
│   │   ├── fetch-fonts.sh      Downloads the pinned fixture fonts, hash-checked
│   │   ├── manifest.json
│   │   ├── sources/*.txt       The UTF-8 plain text inputs
│   │   ├── build/main.go       pdf.Convert builder (make compose-fixtures)
│   │   ├── pdfs/*.pdf
│   │   └── FIXTURES.md         Per-fixture focus, oracles and fonts
│   ├── wpt/                    WPT reftests (host-provisioned, no Docker)
│   │   ├── manifest.json
│   │   ├── reftests.json       The 569 tests, their references and fuzziness
│   │   ├── files.sha256        Every file they read, by WPT path and SHA-256
│   │   ├── list-candidates.py  Selects the tests from a WPT checkout
│   │   ├── fetch.sh            Downloads and hash-checks the files (make wpt-fetch)
│   │   └── FIXTURES.md         How the tests were chosen
│   ├── svg/                    resvg SVG tests (host-provisioned, no Docker)
│   │   ├── manifest.json
│   │   ├── tests.json          The 246 tests, their reference PNGs and sizes
│   │   ├── files.sha256        Every file they read, by resvg path and SHA-256
│   │   ├── list-candidates.py  Selects the tests from a resvg checkout
│   │   ├── fetch.sh            Downloads and hash-checks the files (make svg-fetch)
│   │   └── FIXTURES.md         How the tests were chosen
│   ├── weasyprint/             WeasyPrint draw tests (host-provisioned, no Docker)
│   │   ├── manifest.json
│   │   ├── fixtures/*.json     One test each: HTML, expected pixel grid, colours
│   │   ├── resources/          The images and stylesheets the tests name
│   │   ├── ua.css              Prepended to each test (WeasyPrint's test UA)
│   │   ├── extract.py          Extracts the fixtures (make weasyprint-fixtures)
│   │   ├── LICENSE             WeasyPrint's licence
│   │   └── FIXTURES.md         What was extracted and skipped
│   ├── html/                   HTML sources litho and Chromium convert
│   │   ├── download.sh         Copies chromium/ into the output dir
│   │   ├── manifest.json
│   │   ├── sources/<name>/     index.html or index.md, assets, options.json
│   │   ├── chromium/*.pdf      Chromium references (make html-chromium)
│   │   ├── chromium.sh         Converts the sources with the pinned Gotenberg image
│   │   ├── build/main.go       pdf.Convert builder (make html-fixtures)
│   │   ├── pdfs/*.pdf          litho's PDFs, once the converter writes them
│   │   └── FIXTURES.md         Per-source origin and focus
│   ├── docx/, odt/             DOCX and ODT sources litho and LibreOffice convert
│   │   ├── download.sh         Installs the host-built references from the testdata volume
│   │   ├── manifest.json
│   │   ├── sources/<name>/     Gotenberg's documents, one each, with options.json
│   │   ├── documents.json      The LibreOffice test documents fetched, by ID
│   │   ├── files.sha256        Every fetched file, by LibreOffice core path and SHA-256
│   │   ├── pdfs/*.pdf          litho's PDFs of the committed sources, once the converters write them
│   │   └── FIXTURES.md         How the documents were chosen, the options, the fonts
│   └── fixture/                Reads an html, docx or odt source and its options.json (Go side)
│       └── build/main.go       pdf.Convert builder of the Office sources (make docx-fixtures, make odt-fixtures)
├── testdata/fonts/             Fixture fonts, Ahem and the WeasyPrint test font (gitignored)
├── testdata/wpt/               The fetched WPT files, with versions.json (gitignored)
├── testdata/svg/               The fetched resvg tests, with versions.json (gitignored)
├── testdata/<docx|odt>/        sources/ fetched, libreoffice/ references and manifest.json, then pdfs/ and goldens/ (gitignored)
├── testdata/reports/           Failure images of the converter cells (gitignored)
├── testdata/<corpus>/          Provisioned output (gitignored)
│   ├── pdfs/*.pdf
│   ├── versions.json           Versions each provisioning stage ran with
│   ├── claims.csv              Auto-mode only: <basename>,<profile> per claimed PDF
│   └── goldens/
│       ├── text/<fixture>.json
│       ├── metadata/<fixture>.json
│       ├── images/<fixture>.json
│       └── compliance/<profile>.json
└── cmd/corpus/                 CLI (init, run, baseline, check, scan-claims, versions, tools)
```

## Running

```bash
# Full matrix; auto-provisions any missing corpus via Docker on first run.
make corpus

# Narrow the matrix with environment variables.
make corpus CORPUS_NAME=pdfjs
make corpus CORPUS_FEATURE=text,compliance
make corpus CORPUS_NAME=verapdf CORPUS_FEATURE=compliance CORPUS_PROFILE=PDF_A-2b
make corpus CORPUS_JSON_OUTPUT=1
make corpus CORPUS_WORKERS=16

# Provision without scoring, at the pinned versions (see "Tool versions").
make corpus-init
make corpus-init CORPUS_NAME=verapdf

# Compare the local verapdf, node, java, qpdf, pdfsig, pikepdf, fontTools and Pillow with the pins.
make corpus-tools

# Provision the converter corpora the host provisions (corpus-init skips them).
make wpt-fetch                                              # WPT files and Ahem, hash-checked
make svg-fetch                                              # resvg tests and their Noto fonts, hash-checked
make weasyprint-fixtures                                    # re-extract the committed WeasyPrint fixtures, fetch the test font
make html-fixtures                                          # fixture fonts, then litho's html corpus PDFs
make html-chromium                                          # rebuild the committed Chromium references (Docker; HTML_NAMES="a b" for some)
make docx-fetch                                             # the LibreOffice test documents of the docx corpus, hash-checked (odt-fetch alike)
make docx-libreoffice                                       # their and the committed sources' LibreOffice references, into the testdata (Docker; NAMES="a b")
make docx-fixtures                                          # litho's PDFs of the committed docx sources (odt-fixtures alike)
go run ./test/integration/cmd/corpus init --corpus=docx,odt # the diff goldens of the references

# Wipe provisioned data to force re-init on the next run.
make clean-corpus

# Capture the current matrix scores as the regression baseline.
# Writes to test/integration/baseline.json (commit it).
make corpus-baseline

# Fail if any (corpus, feature) cell regressed against the baseline.
# The merge gate: CI runs no corpus, so run it before pushing.
# Improvements are non-fatal but noted.
make corpus-check
```

| Variable                     | Effect                                               | Example                          |
| ---------------------------- | ---------------------------------------------------- | -------------------------------- |
| `CORPUS_NAME`                | run one corpus only (also narrows the auto-init)     | `CORPUS_NAME=pdfjs`              |
| `CORPUS_FEATURE`             | comma-separated feature filter                       | `CORPUS_FEATURE=text,compliance` |
| `CORPUS_PROFILE`             | restrict to one profile directory                    | `CORPUS_PROFILE=PDF_A-2b`        |
| `CORPUS_JSON_OUTPUT`         | emit the scoreboard as JSON                          | `CORPUS_JSON_OUTPUT=1`           |
| `CORPUS_WORKERS`             | override the parallel worker pool size               | `CORPUS_WORKERS=16`              |
| `CORPUS_ALLOW_VERSION_DRIFT` | score goldens made with other versions than the pins | `CORPUS_ALLOW_VERSION_DRIFT=1`   |

## Tool versions

`corpus/versions.env` pins every tool and corpus source provisioning uses, one commented `KEY=VALUE` per line: veraPDF (the `verapdf/cli` image tag and the digest of the one build the goldens came from), Node (the `node` base image, by digest), the Temurin JRE veraPDF, Mustang and KoSIT run on, the PDF.js commit the image builds, pdfcpu (with the SHA-256 of its Linux archive for each architecture the image builds for, amd64 and arm64), Mustang, KoSIT with its XRechnung bundle and EN 16931 XSLT, poppler (the release, the SHA-256 of its tarball, and the Debian base image it builds on, by digest), and the revision of each downloaded corpus (a commit SHA or a tag, never a branch; govdocs by its fixed thread list). A last group pins the tools the host runs for live checks, fixture builds, the outline goldens and the ICC transform goldens (qpdf, pikepdf, fontTools, Pillow), which the image does not install, and the Liberation and DejaVu font archives the compose fixtures are converted with, each by URL and SHA-256; the host's poppler `pdfsig` is checked against the poppler pin the image builds. The converter corpora add five host pins: the web-platform-tests commit (`WPT_COMMIT`), the resvg commit (`RESVG_COMMIT`), the WeasyPrint release with the SHA-256 of its source tarball and of its test font (`WEASYPRINT_VERSION`, `WEASYPRINT_SHA256`, `WEASYPRINT_FONT_SHA256`), the Gotenberg image the Chromium and LibreOffice references come from (`GOTENBERG_IMAGE`), and the LibreOffice core commit the docx and odt corpora's test documents come from (`LIBREOFFICE_CORE_COMMIT`), the commit of the release the image runs; the fixture fonts gain Carlito and Caladea (`CARLITO_FONTS_*`, `CALADEA_FONTS_*`), the faces the image draws Calibri and Cambria with. A docx or odt reference's own `manifest.json` records the image, the commit and the SHA-256 of its source and options, and the `diff` cell refuses a reference that disagrees with the pins or the sources. `wpt/fetch.sh` records `WPT_COMMIT` in `testdata/wpt/versions.json`, so the wpt cell refuses files fetched at another commit as the image-provisioned cells do; `svg/fetch.sh` records `RESVG_COMMIT` in `testdata/svg/versions.json` likewise.

`corpus init` passes every entry to `docker build` as a `--build-arg`. The Dockerfile carries no default of its own, so a bare `docker build` fails rather than drifting; build by hand with the arguments `corpus versions` prints:

```bash
docker build $(go run ./test/integration/cmd/corpus versions --build-args) \
  -t litho-corpus-builder test/integration/corpus
```

An environment variable of the same name overrides a pin for one invocation, and every command that reads the pins announces the override before anything else:

```bash
VERAPDF_VERSION=1.30.2 make corpus-init CORPUS_NAME=litho
```

Overriding `VERAPDF_VERSION` (or `NODE_VERSION`, `NODE_DEBIAN_RELEASE`) without its `*_IMAGE_DIGEST` drops the digest, and the image is pulled by tag: a reference carrying both resolves by the digest alone, which would install the pinned build under the new name. The build also fails when the veraPDF it installed reports another version than `VERAPDF_VERSION`.

The image installs the pdfcpu archive of its own architecture and fails the build when that archive's SHA-256 is not the one `PDFCPU_SHA256_AMD64` or `PDFCPU_SHA256_ARM64` pins, so overriding `PDFCPU_VERSION` needs the building architecture's hash overridden with it.

The image builds poppler's `pdftoppm` and `pdfinfo` from the release tarball and fails the build when its SHA-256 is not `POPPLER_SHA256`, so overriding `POPPLER_VERSION` needs the hash overridden with it. The release needs a newer toolchain and FreeType than the Node image's Debian release ships, so it builds in a `debian:<POPPLER_DEBIAN_RELEASE>-slim` stage pinned by `POPPLER_DEBIAN_IMAGE_DIGEST`, and the final stage runs the binaries through their own loader, libraries and fonts (`corpus/lib/bundle-poppler.sh`): URW base 35 for the standard 14 fonts and DejaVu for any other font a PDF does not embed. The stage also installs poppler-data (`POPPLER_DATA_VERSION`, checked against `POPPLER_DATA_SHA256`), the CMaps and CID-to-Unicode tables of the Adobe CJK character collections, without which pdftoppm draws no glyph of a font of Adobe-GB1, -CNS1, -Japan1 or -Korea1 that has no `/ToUnicode` or whose encoding is one of their CMaps, and the Regular face of each region's Noto Sans CJK subset font (`NOTO_CJK_SANS_VERSION`, each checked against its `NOTO_SANS_<region>_SHA256`, SIL Open Font License 1.1), which poppler draws an unembedded font of those collections with. `corpus/lib/70-noto-cjk.conf` makes fontconfig offer each language its own region's face first. Together they add about 36 MB to the image: 13 MB of poppler-data and 23 MB of fonts.

Provisioning records what each stage ran with in `testdata/<corpus>/versions.json`, keyed by the directory the stage filled (`pdfs`, `goldens/text`, `goldens/compliance`, ...): the resolved pins, what the tools report about themselves, and for compliance the build header of the veraPDF reports. `corpus run`, `baseline` and `check` compare the record of every golden they read with the pins it depends on (a text golden with the PDF.js commit and Node, a compliance golden with veraPDF and the JRE, the PDFs with the corpus revision) and refuse to score a golden recorded with another value. The refusal names the command that re-provisions exactly the stale directories. `--allow-version-drift` (`CORPUS_ALLOW_VERSION_DRIFT=1`) scores them anyway, for a local experiment; never capture a baseline with it.

Testdata provisioned before `versions.json` existed has no record: the tools its goldens came from are unknown, and each run warns once per corpus without refusing, since only a re-provision can learn them. The one exception is compliance: a veraPDF report names its own build in its header, so a compliance golden whose veraPDF core differs from `VERAPDF_VERSION` is refused with or without a record.

To bump a pin:

1. Edit `corpus/versions.env`. For an image, look up the new digest (`docker buildx imagetools inspect verapdf/cli:v<version>`); for pdfcpu, pin the hashes its release lists in `checksums.txt` for the `Linux_x86_64` and `Linux_arm64` archives; for a corpus, pin the new commit SHA.
2. Re-provision what depends on it. `make corpus-init` names the stale directories and prints the command that removes and regenerates them; run that command.
3. Recapture the baseline (`make corpus-baseline`) and commit it with the pin, so the gate measures litho against the new goldens rather than the tool change.

The live `remediation` cells shell out to the `verapdf` on your PATH, and live checks use the local qpdf and pdfsig: `make corpus-tools` prints each one against its pin. It reads veraPDF's core version from a report, since `verapdf --version` prints the apps version, which a Homebrew build sets to 1.30.0 over a 1.30.3 core.

## Regression baseline

`test/integration/baseline.json` captures one `(matched, mismatched, skipped)` count per `(corpus, feature)` cell, plus named metrics for the cells that emit them (45 of 52 cells, 88 distinct names, across `text`, `imagedecode`, `render`, `compliance`, `remediation`, `remediation-aggressive`, `remediation-pair`, `facturx-*`, `split`, `merge`, `merge-catalog`, `determinism` and the `mutate-*` cells). The file is committed; `make corpus-check` runs the matrix and fails when any cell's `matched` count drops below the captured value, or when a metric moves the wrong way, which is **not always downwards**. `costMetrics` (`baseline.go`) registers forty-seven metrics as good-when-lower, so a **rise** in `false_positive`, `false_negative`, `ruleset_mismatch`, `disagreement`, `parse_failed`, `decode_failed`, `litho_regressed`, `verapdf_regressed`, `acroform_invented`, `portfolio_dropped`, `lexicons_dropped`, `nondeterministic`, `render_pages_nondeterministic`, `render_failed`, `render_size_mismatch`, or one of the failure counts of the [mutation features](FEATURES.md#mutation-features), [`redact-render`](FEATURES.md#redaction-verifier), [`recolor`](FEATURES.md#colour-conversion) or `flatten` is the regression. A further twenty-two, listed in `uncomparedMetrics` (`litho_pre`/`post`/`fixed`, `verapdf_pre`/`post`/`fixed` among them), are raw inventory totals the check does not gate at all. Improvements are non-fatal: they surface as `improvement:` lines so the operator can decide whether to refresh the baseline with `make corpus-baseline`.

The converter cells record more than counts. `reftest`, `draw` and `svgdraw` record each fixture's outcome (`pass`, `fail` or `error`) under `outcomes`: a fixture that passed in the baseline and does not now is a regression, whatever the cell's count does, and one that passes now and did not is an improvement. `diff` records each fixture's five scores under `scores`, with the tolerance it forgives under `score_tolerance` (0.02): a score that drops by more is a regression. A fixture the baseline holds and the run did not score is reported missing. The outcomes and scores of a cell are replaced whole by a `--merge` capture, so new passes merge in with the cell.

Cells with `[no golden]` (typically corpora not provisioned for a feature) are excluded so capturing the baseline on a partially provisioned tree does not pin missing-golden states. The check is silent about corpora that exist in the current run but not the baseline; new corpora become regression-gated only after the next `make corpus-baseline` commit.

> **`make corpus-baseline` rewrites the whole file, including cells it did not measure.** The narrowing variables above flow into it, so `make corpus-baseline CORPUS_NAME=pdfjs` scores one corpus and writes a baseline containing only that corpus, silently dropping the other six. To refresh part of the matrix, call the tool directly with `-merge`, which replaces only the cells the run measured and preserves the rest:
>
> ```bash
> go run ./test/integration/cmd/corpus baseline --corpus=pdfjs --feature=merge-catalog --merge
> ```
>
> Verify afterwards that the cell count did not shrink. Refreshing the bar is also the only way to protect a gain: improvements are non-fatal, so a cell that improved stays unprotected until the baseline is recaptured and committed.

## Features

Each feature, its oracle and its comparison rules are in [FEATURES.md](FEATURES.md).

## Corpora

| Corpus       | Fixtures                             | Source                                                                                                                                                                  |
| ------------ | ------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `pdfjs`      | ~1,200 PDFs (1 fixture each)         | Mozilla's PDF.js test suite                                                                                                                                             |
| `verapdf`    | ~2,600 PDFs × 15 profiles            | veraPDF corpus, flattened from the per-profile directories                                                                                                              |
| `govdocs`    | ~1,100 PDFs (1 fixture per claim)    | Govdocs1 random-sample of five fixed thread archives (real-world PDFs)                                                                                                  |
| `pdf20`      | 7 PDFs (1 fixture per claim)         | PDF Association's `pdf20examples` (CC-BY-SA-4.0 PDF 2.0 samples)                                                                                                        |
| `pdfcpu`     | ~50 PDFs (1 fixture each)            | pdfcpu's `pkg/samples/bookmarks` + `pkg/testdata` (Apache-2.0)                                                                                                          |
| `facturx`    | ~150 PDFs (1 fixture per claim)      | ZUGFeRD/corpus (Apache-2.0): real, sample and intentionally-broken Factur-X / ZUGFeRD invoices, flattened from the `ZUGFeRDv2`, `XML-Rechnung` and `ZUGFeRDv1` subtrees |
| `litho`      | 215 PDFs × 13 profiles (2,795 cells) | Self-authored, committed in-tree; see `corpus/litho/README.md`                                                                                                          |
| `compose`    | 8 PDFs × 2 profiles (16 cells)       | Converted by litho from committed text sources; see `corpus/compose/FIXTURES.md`                                                                                        |
| `wpt`        | 569 HTML reftests                    | web-platform-tests (BSD-3) at a pinned commit, fetched by `make wpt-fetch`; see `corpus/wpt/FIXTURES.md`                                                                |
| `weasyprint` | 315 HTML draw tests                  | WeasyPrint's `tests/draw` (BSD-3), extracted and committed; see `corpus/weasyprint/FIXTURES.md`                                                                         |
| `svg`        | 246 SVG tests                        | resvg's `crates/resvg/tests` (MIT) at a pinned commit, fetched by `make svg-fetch`; see `corpus/svg/FIXTURES.md`                                                        |
| `html`       | 29 HTML and Markdown sources         | Gotenberg's web test corpus and self-authored pages, with Chromium's PDFs; see `corpus/html/FIXTURES.md`                                                                |
| `docx`       | 360 DOCX documents                   | 10 of Gotenberg's (committed) and 350 of LibreOffice's Writer tests (MPL-2.0, fetched by `make docx-fetch`), against LibreOffice's PDFs; see `corpus/docx/FIXTURES.md`  |
| `odt`        | 154 ODT and flat ODT documents       | 4 of Gotenberg's (committed) and 150 of LibreOffice's Writer tests (MPL-2.0, fetched by `make odt-fetch`), against LibreOffice's PDFs; see `corpus/odt/FIXTURES.md`     |

Fixture ID format depends on the corpus's `profile_mode` (see manifest schema):

- `expand` (verapdf, litho, compose): every PDF yields one fixture per declared profile, ID = `<profile>/<basename>`. A profile-agnostic feature (`text`) keeps each PDF's first fixture, under ID = `<basename>`.
- `none` (pdfjs): one fixture per PDF, ID = `<basename>`.
- `auto` (govdocs, pdf20, facturx): one fixture per PDF, profile read from the document's XMP claim. Files claiming `PDF_A-Nx` or `PDF_UA-N` get ID = `<profile>/<basename>` and run compliance/remediation. Files with no claim get ID = `<basename>` and run only the profile-agnostic features (text, metadata).

A converter corpus (`wpt`, `svg`, `weasyprint`, `html`, `docx`, `odt`) lists source documents instead of PDFs, through its manifest's `inputs`. Its fixture IDs are the WPT paths of the tests (`css/CSS2/floats/float-001.html`), the resvg paths of the SVG tests (`shapes/rect/simple-case.svg`), the fixture file names (`test_float__test_float`), the source directory names (`invoice`), and, for a fetched LibreOffice test document, its directory and file name (`ooxmlexport/tdf123.docx`).

Auto-mode provisioning runs the in-tree `corpus scan-claims` helper after download, writing `<testdata_root>/claims.csv` keyed `<basename>,<profile>`. `run-verapdf.sh` consumes that file and validates each PDF only against the profile it claims (one veraPDF invocation per profile, hard-linked staging buckets), so cost scales with corpus size, not corpus size × profile count. The runner picks up the same CSV on `corpus run` to skip the live XMP scan. Delete the CSV (or `make clean-corpus`) to force a re-scan.

Known limitation: the current govdocs sample (1,091 files) and the pdf20 examples contain zero PDF/A or PDF/UA claims (verified by byte-scanning for `pdfaid`), so their `claims.csv` files are legitimately empty and the compliance/remediation features run no fixtures there. Compliance is scored on three corpora instead: `verapdf` and `litho`, whose fixtures are built to exercise specific rules, and `facturx`, whose invoices claim PDF/A-3. `compose` scores it too, on what litho's converter writes, which claims nothing. veraPDF is the ground truth for all three. A general real-world sample of documents that claim PDF/A or PDF/UA is still missing.

Adding a new corpus means dropping `corpus/<name>/download.sh` + `manifest.json`, then adding the matching `COPY <name>/ ./<name>/` and `chmod +x <name>/download.sh` lines to `corpus/Dockerfile` so the provisioning image can reach the script. Nothing else changes: the CLI discovers corpora by scanning for `manifest.json`. A corpus the host provisions, as `wpt`, `svg` and `weasyprint` are, names its make target in `provisioned_by` instead and ships no `download.sh`; `corpus init` skips it.

A new corpus becomes regression-gated only after the next `make corpus-baseline` commit; until then it runs and scores but cannot fail `make corpus-check`.

### Coverage: craft a litho fixture

When a change touches a construct no corpus file reaches, add a fixture to the litho corpus so the gate covers it. A fix measured only on throwaway files is protected by nothing once it lands: a later change can undo it and the gate stays green.

- Census first. Run a harness over the provisioned corpora and look for a file that reaches the construct. If one does, name it and build no fixture.
- Build the fixture with a pikepdf builder in `corpus/litho/build/` that uses `_common.py` (deterministic `/ID`, `stream_decode_level=none`), so a rebuild reproduces the PDF byte for byte. Commit the builder, the PDF in `corpus/litho/pdfs/`, and its rows in `corpus/litho/README.md`: the Fixtures table, the expected firing set, a section, and a Known divergences entry when litho and veraPDF disagree.
- Pack several independent constructs into one fixture. The corpus exists to catch a rule that masks another, which a one-construct file cannot show.
- Confirm the counts with veraPDF at the fixture's home profile before committing, and compare litho's rule set with veraPDF's at every profile.
- Provisioning captures the goldens with `corpus/lib/run-verapdf.sh`. On an already provisioned checkout, copy the PDF into `testdata/litho/pdfs/` and add its veraPDF jobs to each `testdata/litho/goldens/compliance/<profile>.json`, made with the pinned veraPDF (`make corpus-tools` shows whether the local one matches), then recapture `baseline.json`.
- A fixture's text is scored too. Provisioning generates its PDF.js golden into `testdata/litho/goldens/text/`; where PDF.js reads the fixture wrong, add an entry to `corpus/litho/text-overrides.json` that records PDF.js's reading, gives the right one or marks the page unscored, and cites the clause (see the Scope section of `corpus/litho/README.md`).
- A fixture only protects what a feature runs on it. The litho corpus runs `compliance`, `remediation`, `remediation-aggressive`, `determinism`, `text` and the seven [mutation features](FEATURES.md#mutation-features), so a fixture protects a stamp, a page insertion and deletion, auto-tagging, form flattening, a signature, encryption and a redaction too. A construct only another Writer mutation reaches needs a spec in `mutationSpecs` (`cmd/corpus/feature_mutation_specs.go`) that runs it, not only a fixture.

## Manifest schema

```json
{
  "name": "govdocs",
  "source": "govdocs",
  "pdf_root": "pdfs",
  "profile_mode": "auto",
  "profiles": ["PDF_A-1a", "PDF_A-1b", "...", "PDF_UA-2"],
  "goldens": {
    "text": "goldens/text",
    "metadata": "goldens/metadata",
    "compliance": "goldens/compliance",
    "remediation": "goldens/compliance",
    "remediation-aggressive": "goldens/compliance"
  },
  "skips": {
    "bug1393476": "infinite loop in xref parsing"
  }
}
```

Fields:

- `name`: identifier used in `CORPUS_NAME`. Need not match the source directory.
- `source`: directory under `corpus/` holding the `download.sh`. Defaults to `name`.
- `pdf_root`: subdirectory of the corpus testdata root that holds the flat `pdfs/` tree.
- `profile_mode`: how PDFs become fixtures. `expand` (default) cartesian-products every PDF over `profiles`; `none` drops the cartesian and emits one fixture per PDF; `auto` reads each PDF's XMP claim and emits one fixture per PDF, keyed by the claimed profile when present.
- `profiles`: veraPDF profile directory names. Drives the cartesian under `expand`, acts as the allow-set for accepted XMP claims under `auto`, and is ignored under `none`.
- `goldens`: maps feature name to the directory (relative to the corpus testdata root) that holds the reference files. `remediation` points at the compliance directory because that is where the feature reads `verapdf_pre` counts from. The provisioning Docker image only generates goldens for features listed here, so dropping `text` + `metadata` from the veraPDF corpus skips the (slow) PDF.js generator entirely.
- `golden_overrides`: maps feature name to a committed file, relative to the corpus source directory, that corrects goldens the oracle reads wrong. `text` and `flatten` take one. A `text` override file (litho and govdocs carry one) corrects the PDF.js goldens: each entry records the oracle's reading it replaces, so it fails its fixture when the oracle changes, and gives the right reading or marks the page unscored, with the clause behind it; the goldens stay the oracle's raw output. A `flatten` scope file (pdfjs and govdocs carry one) widens the `flatten` cell from the render sample to the whole corpus (see [Transparency flattening](FEATURES.md#transparency-flattening)).
- `skips`: maps fixture ID to a reason; the runner omits skipped fixtures from every feature.
- `inputs`: makes the corpus a converter corpus, whose fixtures are source documents. `{"list": "reftests.json"}` reads a committed JSON array of objects with an `id`, each the path of its source under the testdata root (`wpt`, `svg`); `{"glob": "fixtures/*.json"}` matches sources under the corpus source directory, each keyed by what its one `*` matched (`weasyprint`; `html` globs `sources/*`, a directory per source, which the `diff` feature reads through `corpus/fixture`). `pdf_root`, `profile_mode` and `profiles` do not apply. Both may be set, as the `docx` and `odt` corpora set them: the committed sources are globbed and the fetched ones listed, with `list_root` the subdirectory of the testdata root the list's IDs resolve under (`sources`) and `fetched_by` the make target a missing one names.
- `references`: `{"dir": "libreoffice", "built_by": "make docx-libreoffice"}` names the reference PDFs the host builds into the testdata, one `<id with / written __>.pdf` each with a `manifest.json`, which `download.sh` installs as the corpus PDFs and the `diff` feature checks against the pins and the sources before it scores.
- `goldens_in_source`: resolves `goldens` against the corpus source directory instead of the testdata root, for references committed beside the manifest (`wpt`, `svg`, `weasyprint`).
- `provisioned_by`: names the make target that provisions the corpus on the host; `corpus init` skips the corpus and says so.

## See also

- [pkg/internal/compliance/README.md](../../pkg/internal/compliance/README.md): compliance engine architecture.
- [pkg/internal/remediation/README.md](../../pkg/internal/remediation/README.md): remediation fixer contract.
