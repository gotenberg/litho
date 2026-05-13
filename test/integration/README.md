# Integration Tests

Matrix runner that scores litho across (corpus, feature) cells. Every corpus supplies PDF fixtures; every feature either loads a captured golden or runs live against an external tool.

## Layout

```
test/integration/
├── corpus/                     Shared provisioning image + per-corpus scripts
│   ├── Dockerfile              One image: Node + PDF.js + VeraPDF
│   ├── entrypoint.sh           Dispatches to <corpus>/download.sh then shared goldens
│   ├── lib/
│   │   ├── gen-references.mjs  PDF.js text + metadata generator (shared)
│   │   └── run-verapdf.sh      VeraPDF per-profile runner (shared)
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
│   └── pdfcpu/
│       ├── download.sh
│       └── manifest.json
├── testdata/<corpus>/          Provisioned output (gitignored)
│   ├── pdfs/*.pdf
│   ├── claims.csv              Auto-mode only: <basename>,<profile> per claimed PDF
│   └── goldens/
│       ├── text/<fixture>.json
│       ├── metadata/<fixture>.json
│       └── compliance/<profile>.json
└── cmd/corpus/                 CLI (init, run, scan-claims)
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

# Wipe provisioned data to force re-init on the next run.
make clean-corpus

# Capture the current matrix scores as the regression baseline.
# Writes to test/integration/baseline.json (commit it).
make corpus-baseline

# Fail if any (corpus, feature) cell regressed against the baseline.
# Used by CI to gate merges; improvements are non-fatal but noted.
make corpus-check
```

| Variable             | Effect                                           | Example                          |
| -------------------- | ------------------------------------------------ | -------------------------------- |
| `CORPUS_NAME`        | run one corpus only (also narrows the auto-init) | `CORPUS_NAME=pdfjs`              |
| `CORPUS_FEATURE`     | comma-separated feature filter                   | `CORPUS_FEATURE=text,compliance` |
| `CORPUS_PROFILE`     | restrict to one profile directory                | `CORPUS_PROFILE=PDF_A-2b`        |
| `CORPUS_JSON_OUTPUT` | emit the scoreboard as JSON                      | `CORPUS_JSON_OUTPUT=1`           |
| `CORPUS_WORKERS`     | override the parallel worker pool size           | `CORPUS_WORKERS=16`              |

## Regression baseline

`test/integration/baseline.json` captures one `(matched, mismatched, skipped)` count per `(corpus, feature)` cell, plus named metrics for the `text` feature (`chars_match` and `words_match`). The file is committed; `make corpus-check` runs the matrix and fails when any cell's `matched` count or metric drops below the captured value. Improvements are non-fatal — they surface as `improvement:` lines so the operator can decide whether to refresh the baseline with `make corpus-baseline`.

Cells with `[no golden]` (typically corpora not provisioned for a feature) are excluded so capturing the baseline on a partially provisioned tree does not pin missing-golden states. The check is silent about corpora that exist in the current run but not the baseline; new corpora become regression-gated only after the next `make corpus-baseline` commit.

## Features

| Feature                  | Golden source              | What it compares                                                                                                                               |
| ------------------------ | -------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `text`                   | PDF.js (captured at init)  | Per-page text extraction against PDF.js's reference text                                                                                       |
| `metadata`               | PDF.js (captured at init)  | Info dict + linearization/form flags                                                                                                           |
| `compliance`             | VeraPDF (captured at init) | Litho's validation verdict against VeraPDF's, per profile                                                                                      |
| `remediation`            | VeraPDF (live at run time) | Remediates with litho, then validates the output with both litho and VeraPDF; reports the diff                                                 |
| `remediation-aggressive` | VeraPDF (live at run time) | Same as `remediation` with `WithAggressiveFixes` enabled                                                                                       |
| `bookmarks`              | pdfcpu (captured at init)  | Outline tree (title, page, children) against pdfcpu's `bookmarks export`                                                                       |
| `split`                  | litho-self (live)          | Round-trips Split([1,last]); compares output text + bookmarks against litho's input extraction                                                 |
| `merge`                  | litho-self + pdfcpu (live) | Merges each PDF with itself; checks page count, per-page text, and bookmark leaf multiset against the derived oracle AND pdfcpu's merge result |
| `merge-empty`            | litho-self + pdfcpu (live) | Two-input companion: merges each PDF with `empty.pdf`; exercises the real cross-document resource-merging path same as `merge` checks          |

All three captured goldens live under the corpus's `testdata/goldens/` directory and are produced by the provisioning Docker image. `remediation` has no captured golden: it shells out to `verapdf` at run time on the remediated buffer. Needs `verapdf` on PATH.

Not every corpus runs every feature. Each corpus is the oracle for the feature it ships goldens for; running the others on it would be redundant.

| Corpus    | text | metadata | compliance | remediation | bookmarks | split | merge | merge-empty | role                                                            |
| --------- | ---- | -------- | ---------- | ----------- | --------- | ----- | ----- | ----------- | --------------------------------------------------------------- |
| `pdfjs`   | ✓    | ✓        | —          | —           | —         | —     | —     | —           | text + metadata oracle (PDF.js renderer is the ground truth)    |
| `verapdf` | —    | —        | ✓          | ✓           | —         | —     | —     | —           | compliance + remediation oracle (VeraPDF is the ground truth)   |
| `govdocs` | ✓    | ✓        | ✓          | ✓           | —         | —     | —     | —           | real-world canary; compliance/remediation use each file's claim |
| `pdf20`   | ✓    | ✓        | ✓          | ✓           | —         | —     | —     | —           | PDF 2.0 canary (claims fall under PDF/A-4\* or PDF/UA-2)        |
| `pdfcpu`  | ✓    | ✓        | —          | —           | ✓         | ✓     | ✓     | ✓           | bookmark/merge/split canary; sourced from pdfcpu's sample tree  |

### Comparison notes

Cross-tool comparisons live behind small normalisations so equivalent inputs do not register as mismatches. The non-obvious ones:

- **Bookmarks: C1 control fold.** PDFDocEncoding (ISO 32000-1 Table D.2) maps bytes `0x80-0x9F` to typographic glyphs (en dash, em dash, smart quotes, etc.). litho applies this mapping when decoding outline titles, matching PDF.js's `PDFStringTranslateTable`. pdfcpu emits the raw byte as a C1 control rune (`U+0085`, `U+0090`, ...). The bookmarks feature folds C1 runes through the same table on both sides before diffing so an "Annex A – Foo" / "Annex A `` Foo" pair compares equal. The fold is a no-op on the litho side (no C1 runes remain after decode).

- **Bookmarks: tab inside a UTF-16BE title is a known difference.** pdfcpu strips horizontal tabs from outline titles ("1`\t`Preface" → "1Preface"); litho preserves them. Both are pointing at the same source byte; the canary leaves this case visible (1 fixture in the pdfcpu corpus, `adobe_supplement_iso32000_1.pdf`) because collapsing whitespace symmetrically would mask genuine title differences.

- **Text on pdfcpu skews low for layout reasons, not extraction quality.** The pdfcpu corpus runs around 62% word-match against PDF.js — much lower than govdocs or pdfjs corpora. Triage shows the gap is dominated by layout-style disagreements rather than litho bugs:
  - Right-margin page numbers separated from the left-margin doc number by 375 pt on the same Y line — litho inserts a space (correct), PDF.js concatenates.
  - Line-wrap hyphenation — litho preserves `Edi-` + newline + `tion`, PDF.js joins.
  - Subscript / superscript Y-drops in technical notation — both tools fire heuristics differently around the small Y offset.
  - Occasional PDF.js bugs where a single element is split mid-content (`(Extract from the Blue Book)` → `theBlue`).

  Treat the pdfcpu `text` score as a layout-style canary, not a regression signal. Real-world corpora (govdocs, pdfjs) are the load-bearing word-match oracle.

## Corpora

| Corpus    | Fixtures                          | Source                                                                 |
| --------- | --------------------------------- | ---------------------------------------------------------------------- |
| `pdfjs`   | ~1,100 PDFs (1 fixture each)      | Mozilla's PDF.js test suite                                            |
| `verapdf` | ~2,600 PDFs × 13 profiles         | VeraPDF corpus, flattened from the per-profile directories             |
| `govdocs` | ~1,500 PDFs (1 fixture per claim) | Govdocs1 random-sample of five fixed thread archives (real-world PDFs) |
| `pdf20`   | ~30 PDFs (1 fixture per claim)    | PDF Association's `pdf20examples` (BSD-licensed PDF 2.0 samples)       |
| `pdfcpu`  | ~50 PDFs (1 fixture each)         | pdfcpu's `pkg/samples/bookmarks` + `pkg/testdata` (Apache-2.0)         |

Fixture ID format depends on the corpus's `profile_mode` (see manifest schema):

- `expand` (verapdf): every PDF yields one fixture per declared profile, ID = `<profile>/<basename>`.
- `none` (pdfjs): one fixture per PDF, ID = `<basename>`.
- `auto` (govdocs, pdf20): one fixture per PDF, profile read from the document's XMP claim. Files claiming `PDF_A-Nx` or `PDF_UA-N` get ID = `<profile>/<basename>` and run compliance/remediation. Files with no claim get ID = `<basename>` and run only the profile-agnostic features (text, metadata).

Auto-mode provisioning runs the in-tree `corpus scan-claims` helper after download, writing `<testdata_root>/claims.csv` keyed `<basename>,<profile>`. `run-verapdf.sh` consumes that file and validates each PDF only against the profile it claims (one VeraPDF invocation per profile, hard-linked staging buckets), so cost scales with corpus size, not corpus size × profile count. The runner picks up the same CSV on `corpus run` to skip the live XMP scan. Delete the CSV (or `make clean-corpus`) to force a re-scan.

Adding a new corpus means dropping `corpus/<name>/download.sh` + `manifest.json`; nothing else changes.

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
- `profiles`: VeraPDF profile directory names. Drives the cartesian under `expand`, acts as the allow-set for accepted XMP claims under `auto`, and is ignored under `none`.
- `goldens`: maps feature name to the directory (relative to the corpus testdata root) that holds the reference files. `remediation` points at the compliance directory because that is where the feature reads `verapdf_pre` counts from. The provisioning Docker image only generates goldens for features listed here, so dropping `text` + `metadata` from the VeraPDF corpus skips the (slow) PDF.js generator entirely.
- `skips`: maps fixture ID to a reason; the runner omits skipped fixtures from every feature.

## See also

- [pkg/internal/compliance/README.md](../../pkg/internal/compliance/README.md): compliance engine architecture.
- [pkg/internal/remediation/README.md](../../pkg/internal/remediation/README.md): remediation fixer contract.
