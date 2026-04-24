# Integration Tests

Matrix runner that scores litho across (corpus, feature) cells. Each corpus supplies PDF fixtures; each feature pairs a litho entry point with a golden reference.

## Layout

```
test/integration/
├── corpus/<name>/
│   ├── Dockerfile           Provisioning image (emits PDFs + goldens)
│   ├── manifest.json        Fixture source, docker image tag, golden directories
│   └── setup.sh             Docker entrypoint
├── testdata/<name>/         Provisioned output (gitignored)
└── cmd/corpus/              CLI
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
```

| Variable             | Effect                                           | Example                          |
| -------------------- | ------------------------------------------------ | -------------------------------- |
| `CORPUS_NAME`        | run one corpus only (also narrows the auto-init) | `CORPUS_NAME=pdfjs`              |
| `CORPUS_FEATURE`     | comma-separated feature filter                   | `CORPUS_FEATURE=text,compliance` |
| `CORPUS_PROFILE`     | restrict to one VeraPDF profile directory        | `CORPUS_PROFILE=PDF_A-2b`        |
| `CORPUS_JSON_OUTPUT` | emit the scoreboard as JSON                      | `CORPUS_JSON_OUTPUT=1`           |
| `CORPUS_WORKERS`     | override the parallel worker pool size           | `CORPUS_WORKERS=16`              |

Direct CLI invocation (equivalent, no auto-init):

- `go run ./test/integration/cmd/corpus init [--corpus=NAME]`
- `go run ./test/integration/cmd/corpus run [--corpus=...] [--feature=...] [--profile=...] [--format=text|json] [--workers=N]`

## Features

| Feature                  | Golden source | What it compares                                             |
| ------------------------ | ------------- | ------------------------------------------------------------ |
| `text`                   | PDF.js        | Per-page text extraction (char-level + word-level match)     |
| `metadata`               | PDF.js        | Info dict + linearization/form flags                         |
| `compliance`             | VeraPDF       | PDF/A and PDF/UA validation verdict                          |
| `remediation`            | litho self    | Pre/post violation counts for non-aggressive fixers          |
| `remediation-aggressive` | litho self    | Pre/post violation counts with `WithAggressiveFixes` enabled |

The matrix displays `[no golden]` when a (corpus, feature) cell has no stored reference.

## Corpora

| Corpus    | Fixtures | Source                                                                                |
| --------- | -------- | ------------------------------------------------------------------------------------- |
| `pdfjs`   | ~1,100   | Mozilla's PDF.js test suite; fixture list in `test_manifest.json`                     |
| `verapdf` | ~2,600   | VeraPDF corpus organised by profile → clause → test                                   |
| `govdocs` | ~15,000  | Govdocs1 sample from five fixed thread archives × 13 VeraPDF profiles (pdf × profile) |

## Manifest schema

```json
{
  "name": "pdfjs",
  "source": "pdf.js",
  "docker_image": "litho-pdfjs-corpus",
  "fixture_source": "pdfjs-manifest",
  "fixture_manifest": "test_manifest.json",
  "pdf_root": "pdfs",
  "goldens": {
    "text": "references",
    "metadata": "references"
  },
  "skips": {
    "bug1393476": "infinite loop in xref parsing"
  }
}
```

Fields:

- `name`: identifier used in `--corpus` flags. Need not match the source directory.
- `source`: directory under `corpus/` holding the Dockerfile. Defaults to `name`.
- `fixture_source`: one of `pdfjs-manifest` (read `test_manifest.json`), `walk-profiles` (walk VeraPDF profile directories), or `walk-flat-x-profiles` (cartesian product of a flat `pdfs/` tree and the `profiles` list, used by real-world corpora like Govdocs where each PDF is validated against every conformance target).
- `profiles`: VeraPDF profile directory names used by `walk-flat-x-profiles`; each PDF yields one fixture per profile.
- `goldens`: maps feature name to the directory (relative to the corpus testdata root) that holds the reference files.
- `skips`: maps fixture ID to a reason; the runner omits skipped fixtures from every feature.

## See also

- [pkg/internal/compliance/README.md](../../pkg/internal/compliance/README.md): compliance engine architecture.
- [pkg/internal/remediation/README.md](../../pkg/internal/remediation/README.md): remediation fixer contract.
