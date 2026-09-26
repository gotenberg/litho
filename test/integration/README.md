# Integration Tests

Matrix runner that scores litho across (corpus, feature) cells. Every corpus supplies PDF fixtures; every feature either loads a captured golden or runs live against an external tool.

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
│   │   ├── gen-images.mjs      PDF.js image signature generator (shared)
│   │   ├── gen-bookmarks.sh    pdfcpu bookmark exporter (shared)
│   │   ├── gen-merges.sh       pdfcpu merge oracle (shared)
│   │   ├── gen-merge-empty.sh  pdfcpu merge-with-empty oracle (shared)
│   │   ├── run-verapdf.sh      veraPDF per-profile runner (shared)
│   │   ├── run-mustang.sh      Mustang Factur-X validity runner (shared)
│   │   └── run-kosit.sh        KoSIT Schematron runner (shared)
│   ├── pdfjs/
│   │   ├── download.sh
│   │   └── manifest.json
│   ├── verapdf/
│   │   ├── download.sh
│   │   └── manifest.json
│   ├── govdocs/
│   │   ├── download.sh
│   │   └── manifest.json
│   ├── pdf20/
│   │   ├── download.sh
│   │   └── manifest.json
│   ├── pdfcpu/
│   │   ├── download.sh
│   │   └── manifest.json
│   ├── facturx/
│   │   ├── download.sh
│   │   └── manifest.json
│   └── litho/                  Self-authored fixtures, committed (not downloaded)
│       ├── download.sh         Copies pdfs/ into the output dir
│       ├── manifest.json
│       ├── build/*.py          pikepdf builders that regenerate the fixtures
│       ├── pdfs/*.pdf
│       └── README.md           Per-fixture expected firing set
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

# Compare the local verapdf, node, java, qpdf, pdfsig and pikepdf with the pins.
make corpus-tools

# Wipe provisioned data to force re-init on the next run.
make clean-corpus

# Capture the current matrix scores as the regression baseline.
# Writes to test/integration/baseline.json (commit it).
make corpus-baseline

# Fail if any (corpus, feature) cell regressed against the baseline.
# The merge gate: the repo has no CI, so run it before pushing.
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

`corpus/versions.env` pins every tool and corpus source provisioning uses, one commented `KEY=VALUE` per line: veraPDF (the `verapdf/cli` image tag and the digest of the one build the goldens came from), Node (the `node` base image, by digest), the Temurin JRE veraPDF, Mustang and KoSIT run on, the PDF.js commit the image builds, pdfcpu, Mustang, KoSIT with its XRechnung bundle and EN 16931 XSLT, and the revision of each downloaded corpus (a commit SHA or a tag, never a branch; govdocs by its fixed thread list). A last group pins the tools the host runs for live checks and fixture builds (qpdf, poppler's `pdfsig`, pikepdf), which the image does not install.

`corpus init` passes every entry to `docker build` as a `--build-arg`. The Dockerfile carries no default of its own, so a bare `docker build` fails rather than drifting; build by hand with the arguments `corpus versions` prints:

```bash
docker build $(go run ./test/integration/cmd/corpus versions --build-args) \
  -t litho-corpus-builder test/integration/corpus
```

An environment variable of the same name overrides a pin for one invocation, and every command that reads the pins announces the override before anything else:

```bash
VERAPDF_VERSION=1.30.3 make corpus-init CORPUS_NAME=litho
```

Overriding `VERAPDF_VERSION` (or `NODE_VERSION`, `NODE_DEBIAN_RELEASE`) without its `*_IMAGE_DIGEST` drops the digest, and the image is pulled by tag: a reference carrying both resolves by the digest alone, which would install the pinned build under the new name. The build also fails when the veraPDF it installed reports another version than `VERAPDF_VERSION`.

Provisioning records what each stage ran with in `testdata/<corpus>/versions.json`, keyed by the directory the stage filled (`pdfs`, `goldens/text`, `goldens/compliance`, ...): the resolved pins, what the tools report about themselves, and for compliance the build header of the veraPDF reports. `corpus run`, `baseline` and `check` compare the record of every golden they read with the pins it depends on (a text golden with the PDF.js commit and Node, a compliance golden with veraPDF and the JRE, the PDFs with the corpus revision) and refuse to score a golden recorded with another value. The refusal names the command that re-provisions exactly the stale directories. `--allow-version-drift` (`CORPUS_ALLOW_VERSION_DRIFT=1`) scores them anyway, for a local experiment; never capture a baseline with it.

Testdata provisioned before `versions.json` existed has no record: the tools its goldens came from are unknown, and each run warns once per corpus without refusing, since only a re-provision can learn them. The one exception is compliance: a veraPDF report names its own build in its header, so a compliance golden whose veraPDF core differs from `VERAPDF_VERSION` is refused with or without a record.

To bump a pin:

1. Edit `corpus/versions.env`. For an image, look up the new digest (`docker buildx imagetools inspect verapdf/cli:v<version>`); for a corpus, pin the new commit SHA.
2. Re-provision what depends on it. `make corpus-init` names the stale directories and prints the command that removes and regenerates them; run that command.
3. Recapture the baseline (`make corpus-baseline`) and commit it with the pin, so the gate measures litho against the new goldens rather than the tool change.

The live `remediation` cells shell out to the `verapdf` on your PATH, and live checks use the local qpdf and pdfsig: `make corpus-tools` prints each one against its pin. It reads veraPDF's core version from a report, since `verapdf --version` prints the apps version, which a Homebrew build sets to 1.30.0 over a 1.30.2 core.

## Regression baseline

`test/integration/baseline.json` captures one `(matched, mismatched, skipped)` count per `(corpus, feature)` cell, plus named metrics for the cells that emit them (21 of 28 cells, 24 distinct names, across `text`, `imagedecode`, `compliance`, `remediation`, `remediation-aggressive`, `facturx-*`, `split`, `merge` and `merge-catalog`). The file is committed; `make corpus-check` runs the matrix and fails when any cell's `matched` count drops below the captured value, or when a metric moves the wrong way, which is **not always downwards**. `costMetrics` (`baseline.go`) registers eleven metrics as good-when-lower, so a **rise** in `false_positive`, `false_negative`, `ruleset_mismatch`, `disagreement`, `parse_failed`, `decode_failed`, `litho_regressed`, `verapdf_regressed`, `acroform_invented`, `portfolio_dropped` or `lexicons_dropped` is the regression. A further six (`litho_pre`/`post`/`fixed`, `verapdf_pre`/`post`/`fixed`) are raw inventory totals listed in `uncomparedMetrics`, and the check does not gate them at all. Improvements are non-fatal: they surface as `improvement:` lines so the operator can decide whether to refresh the baseline with `make corpus-baseline`.

Cells with `[no golden]` (typically corpora not provisioned for a feature) are excluded so capturing the baseline on a partially provisioned tree does not pin missing-golden states. The check is silent about corpora that exist in the current run but not the baseline; new corpora become regression-gated only after the next `make corpus-baseline` commit.

> **`make corpus-baseline` rewrites the whole file, including cells it did not measure.** The narrowing variables above flow into it, so `make corpus-baseline CORPUS_NAME=pdfjs` scores one corpus and writes a baseline containing only that corpus, silently dropping the other six. To refresh part of the matrix, call the tool directly with `-merge`, which replaces only the cells the run measured and preserves the rest:
>
> ```bash
> go run ./test/integration/cmd/corpus baseline --corpus=pdfjs --feature=merge-catalog --merge
> ```
>
> Verify afterwards that the cell count did not shrink. Refreshing the bar is also the only way to protect a gain: improvements are non-fatal, so a cell that improved stays unprotected until the baseline is recaptured and committed.

## Features

| Feature                  | Golden source              | What it compares                                                                                                                                                                                                                                                                                 |
| ------------------------ | -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `text`                   | PDF.js (captured at init)  | Per-page text extraction against PDF.js's reference text                                                                                                                                                                                                                                         |
| `metadata`               | PDF.js (captured at init)  | Info dict + linearization/form flags                                                                                                                                                                                                                                                             |
| `imagedecode`            | PDF.js (captured at init)  | Decodes every image `ExtractImages` lists on a page through its `Decode` function and matches it against the images PDF.js paints on that page, by size and by the mean colour of a grid of at most 4x4 cells; a cell may differ by 3 levels, the rounding two decoders legitimately disagree on |
| `compliance`             | veraPDF (captured at init) | litho's validation verdict against veraPDF's, per profile, which is what `matched` counts; `ruleset_mismatch` counts the cells whose set of failed rules (clause and test) differs from veraPDF's, whether or not the verdicts agree                                                             |
| `remediation`            | veraPDF (live at run time) | Remediates with litho, then validates the output with both litho and veraPDF; reports the diff                                                                                                                                                                                                   |
| `remediation-aggressive` | veraPDF (live at run time) | Same as `remediation` with `WithAggressiveFixes` enabled                                                                                                                                                                                                                                         |
| `bookmarks`              | pdfcpu (captured at init)  | Outline tree (title, page, children) against pdfcpu's `bookmarks export`                                                                                                                                                                                                                         |
| `split`                  | litho-self (live)          | Round-trips Split([1,last]); compares output text + bookmarks against litho's input extraction, then asserts the document-level catalog entries survived                                                                                                                                         |
| `merge`                  | litho-self + pdfcpu (live) | Merges each PDF with itself; checks page count, per-page text, and bookmark leaf multiset against the derived oracle AND pdfcpu's merge result, then the document-level catalog entries                                                                                                          |
| `merge-empty`            | litho-self + pdfcpu (live) | Two-input companion: merges each PDF with `empty.pdf`; exercises the real cross-document resource-merging path same as `merge` checks                                                                                                                                                            |
| `merge-catalog`          | litho-self (live)          | Merges each PDF with itself and asserts only the document-level catalog entries; no text or bookmark comparison, so it is cheap enough to run on a large corpus                                                                                                                                  |
| `facturx-validity`       | Mustang (captured at init) | litho's `ValidateFacturX` verdict against Mustang's. Scores the hybrid-invoice carrier rules (filename, `AFRelationship`, `fx:*` XMP, extension schema, embedded CII XSD); `compliance` scores the PDF/A-3 base layer against veraPDF                                                            |
| `facturx-schematron`     | KoSIT (captured at init)   | litho's Schematron rule firings against KoSIT's. KoSIT routes each fixture to XRechnung Schematron, generic EN 16931 CII Schematron, or "no scenario applicable"; a skip surfaces as `[not applicable]` rather than counting against the score                                                   |

The provisioning Docker image produces every captured golden under the corpus's `testdata/goldens/` directory. `remediation` has no captured golden: it shells out to `verapdf` at run time on the remediated buffer. Needs `verapdf` on PATH.

Not every corpus runs every feature. Each corpus is the oracle for the feature it ships goldens for; running the others on it would be redundant.

| Corpus    | text | metadata | imagedecode | compliance | remediation | bookmarks | split | merge | merge-empty | merge-catalog | role                                                                                                                            |
| --------- | ---- | -------- | ----------- | ---------- | ----------- | --------- | ----- | ----- | ----------- | ------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `pdfjs`   | ✓    | ✓        | ✓           |            |             |           |       |       |             | ✓             | text + metadata + image oracle (PDF.js renderer is the ground truth)                                                            |
| `verapdf` |      |          |             | ✓          | ✓           |           |       |       |             |               | compliance + remediation oracle (veraPDF is the ground truth)                                                                   |
| `govdocs` | ✓    | ✓        | ✓           | ✓          | ✓           |           |       |       |             |               | real-world canary; compliance/remediation use each file's claim                                                                 |
| `pdf20`   | ✓    | ✓        |             | ✓          | ✓           |           |       |       |             |               | PDF 2.0 canary (claims fall under PDF/A-4\* or PDF/UA-2)                                                                        |
| `pdfcpu`  | ✓    | ✓        |             |            |             | ✓         | ✓     | ✓     | ✓           |               | bookmark/merge/split canary; sourced from pdfcpu's sample tree                                                                  |
| `facturx` | ✓    | ✓        |             | ✓          | ✓           |           |       |       |             |               | Factur-X / ZUGFeRD hybrid-invoice oracle; the only corpus running `facturx-validity` (Mustang) and `facturx-schematron` (KoSIT) |
| `litho`   |      |          |             | ✓          | ✓           |           |       |       |             |               | rule-masking oracle; hand-built multi-violation fixtures                                                                        |

`merge-catalog` runs on `pdfjs` because that is where the annotation shapes it
guards against actually occur: measured across all 5171 corpus fixtures, 49
pdfjs files carry an annotation that a field collector can mistake for a form
field, against 2 in `pdfcpu`. It is deliberately absent from `litho` and
`verapdf`, whose manifests set `profile_mode: expand`: a catalog cannot vary
by profile, so those corpora would score the same document 8 and 15 times over.

### Comparison notes

Cross-tool comparisons live behind small normalisations so equivalent inputs do not register as mismatches. The non-obvious ones:

- **Compliance: the verdict and the rule set.** `matched` compares verdicts only. A fixture that breaks many rules, which every `litho` fixture does, stays non-compliant on both sides whatever rule class litho drops or invents, so a detection change reaches `matched` only when it flips a verdict. `ruleset_mismatch` closes that gap: it compares the distinct `clause-tNN` ids litho's violations fail with the ids veraPDF's rule summaries fail, as sets, so instance counts do not count (see the litho corpus README for why they differ). Every scored cell emits the key, zero included, so the baseline keeps a bar on a corpus where no cell diverges. The text scoreboard lists each diverging cell with the ids only one side reports, and the JSON mismatch record carries them as `litho_only_rules` and `verapdf_only_rules`.

- **Bookmarks: C1 control fold.** PDFDocEncoding (ISO 32000-1 Table D.2) maps bytes `0x80-0x9F` to typographic glyphs (en dash, em dash, smart quotes, etc.). litho applies this mapping when decoding outline titles, matching PDF.js's `PDFStringTranslateTable`. pdfcpu emits the raw byte as a C1 control rune (`U+0085`, `U+0090`, ...). The bookmarks feature folds C1 runes through the same table on both sides before diffing so an "Annex A – Foo" / "Annex A `` Foo" pair compares equal. The fold is a no-op on the litho side (no C1 runes remain after decode).

- **Bookmarks: tab inside a UTF-16BE title is a known difference.** pdfcpu strips horizontal tabs from outline titles ("1`\t`Preface" → "1Preface"); litho preserves them. Both are pointing at the same source byte; the canary leaves this case visible (1 fixture in the pdfcpu corpus, `adobe_supplement_iso32000_1.pdf`) because collapsing whitespace symmetrically would mask genuine title differences.

- **Text on pdfcpu skews low for layout reasons, not extraction quality.** The pdfcpu corpus runs around 62% word-match against PDF.js (32 of 51 fixtures in the baseline), far below the pdfjs corpus (1,027 of 1,190, about 86%) and close to govdocs (623 of 1,091, about 57%). Triage shows the gap to pdfjs is dominated by layout-style disagreements rather than litho bugs:
  - Right-margin page numbers separated from the left-margin doc number by 375 pt on the same Y line: litho inserts a space (correct), PDF.js concatenates.
  - Line-wrap hyphenation: litho preserves `Edi-` + newline + `tion`, PDF.js joins.
  - Subscript / superscript Y-drops in technical notation: both tools fire heuristics differently around the small Y offset.
  - Occasional PDF.js bugs where a single element is split mid-content (`(Extract from the Blue Book)` → `theBlue`).

  Treat the pdfcpu `text` score as a layout-style canary, not a regression signal. Real-world corpora (govdocs, pdfjs) are the load-bearing word-match oracle.

## Corpora

| Corpus    | Fixtures                          | Source                                                                                                                                                                  |
| --------- | --------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `pdfjs`   | ~1,200 PDFs (1 fixture each)      | Mozilla's PDF.js test suite                                                                                                                                             |
| `verapdf` | ~2,600 PDFs × 15 profiles         | veraPDF corpus, flattened from the per-profile directories                                                                                                              |
| `govdocs` | ~1,100 PDFs (1 fixture per claim) | Govdocs1 random-sample of five fixed thread archives (real-world PDFs)                                                                                                  |
| `pdf20`   | 7 PDFs (1 fixture per claim)      | PDF Association's `pdf20examples` (CC-BY-SA-4.0 PDF 2.0 samples)                                                                                                        |
| `pdfcpu`  | ~50 PDFs (1 fixture each)         | pdfcpu's `pkg/samples/bookmarks` + `pkg/testdata` (Apache-2.0)                                                                                                          |
| `facturx` | ~150 PDFs (1 fixture per claim)   | ZUGFeRD/corpus (Apache-2.0): real, sample and intentionally-broken Factur-X / ZUGFeRD invoices, flattened from the `ZUGFeRDv2`, `XML-Rechnung` and `ZUGFeRDv1` subtrees |
| `litho`   | 92 PDFs × 8 profiles              | Self-authored, committed in-tree; see `corpus/litho/README.md`                                                                                                          |

Fixture ID format depends on the corpus's `profile_mode` (see manifest schema):

- `expand` (verapdf): every PDF yields one fixture per declared profile, ID = `<profile>/<basename>`.
- `none` (pdfjs): one fixture per PDF, ID = `<basename>`.
- `auto` (govdocs, pdf20, facturx): one fixture per PDF, profile read from the document's XMP claim. Files claiming `PDF_A-Nx` or `PDF_UA-N` get ID = `<profile>/<basename>` and run compliance/remediation. Files with no claim get ID = `<basename>` and run only the profile-agnostic features (text, metadata).

Auto-mode provisioning runs the in-tree `corpus scan-claims` helper after download, writing `<testdata_root>/claims.csv` keyed `<basename>,<profile>`. `run-verapdf.sh` consumes that file and validates each PDF only against the profile it claims (one veraPDF invocation per profile, hard-linked staging buckets), so cost scales with corpus size, not corpus size × profile count. The runner picks up the same CSV on `corpus run` to skip the live XMP scan. Delete the CSV (or `make clean-corpus`) to force a re-scan.

Known limitation: the current govdocs sample (1,091 files) and the pdf20 examples contain zero PDF/A or PDF/UA claims (verified by byte-scanning for `pdfaid`), so their `claims.csv` files are legitimately empty and the compliance/remediation features run no fixtures there. Compliance is scored on three corpora instead: `verapdf` and `litho`, whose fixtures are built to exercise specific rules, and `facturx`, whose invoices claim PDF/A-3. veraPDF is the ground truth for all three. A general real-world sample of documents that claim PDF/A or PDF/UA is still missing.

Adding a new corpus means dropping `corpus/<name>/download.sh` + `manifest.json`, then adding the matching `COPY <name>/ ./<name>/` and `chmod +x <name>/download.sh` lines to `corpus/Dockerfile` so the provisioning image can reach the script. Nothing else changes: the CLI discovers corpora by scanning for `manifest.json`.

A new corpus becomes regression-gated only after the next `make corpus-baseline` commit; until then it runs and scores but cannot fail `make corpus-check`.

### Coverage: craft a litho fixture

When a change touches a construct no corpus file reaches, add a fixture to the litho corpus so the gate covers it. A fix measured only on throwaway files is protected by nothing once it lands: a later change can undo it and the gate stays green.

- Census first. Run a harness over the provisioned corpora and look for a file that reaches the construct. If one does, name it and build no fixture.
- Build the fixture with a pikepdf builder in `corpus/litho/build/` that uses `_common.py` (deterministic `/ID`, `stream_decode_level=none`), so a rebuild reproduces the PDF byte for byte. Commit the builder, the PDF in `corpus/litho/pdfs/`, and its rows in `corpus/litho/README.md`: the Fixtures table, the expected firing set, a section, and a Known divergences entry when litho and veraPDF disagree.
- Pack several independent constructs into one fixture. The corpus exists to catch a rule that masks another, which a one-construct file cannot show.
- Confirm the counts with veraPDF at the fixture's home profile before committing, and compare litho's rule set with veraPDF's at every profile.
- Provisioning captures the goldens with `corpus/lib/run-verapdf.sh`. On an already provisioned checkout, copy the PDF into `testdata/litho/pdfs/` and add its veraPDF jobs to each `testdata/litho/goldens/compliance/<profile>.json`, made with the pinned veraPDF (`make corpus-tools` shows whether the local one matches), then recapture `baseline.json`.
- A fixture only protects what a feature runs on it. The litho corpus runs `compliance`, `remediation` and `remediation-aggressive`, so a construct only a Writer mutation reaches (a stamp, a page deletion, a signature) needs a feature that runs that mutation, not a fixture.

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
- `skips`: maps fixture ID to a reason; the runner omits skipped fixtures from every feature.

## See also

- [pkg/internal/compliance/README.md](../../pkg/internal/compliance/README.md): compliance engine architecture.
- [pkg/internal/remediation/README.md](../../pkg/internal/remediation/README.md): remediation fixer contract.
