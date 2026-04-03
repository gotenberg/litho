# Integration Tests

Corpus-based integration tests for litho. Requires Docker for initial corpus provisioning.

## Corpus CLI Tool

A standalone binary at `cmd/corpus/` compares litho's output against reference results.

### VeraPDF Subcommand

Compares litho's PDF/A and PDF/UA pass/fail verdicts against the VeraPDF test corpus.

```bash
# Provision via Docker (first time only), then compare
go run ./test/integration/cmd/corpus verapdf --init

# Compare only (corpus already provisioned)
go run ./test/integration/cmd/corpus verapdf

# JSON output
go run ./test/integration/cmd/corpus verapdf --format json

# Filter to a single profile
go run ./test/integration/cmd/corpus verapdf --profile PDF_A-4

# CI mode: exit 1 if thresholds exceeded
go run ./test/integration/cmd/corpus verapdf --max-fn 3 --max-fp 0

# Custom corpus path
go run ./test/integration/cmd/corpus verapdf --corpus /path/to/testdata/verapdf
```

Flags:

| Flag | Default | Description |
|---|---|---|
| `--init` | false | Provision corpus via Docker before comparison |
| `--format` | text | Output format: `text` or `json` |
| `--profile` | (all) | Filter to a single profile directory |
| `--max-fn` | -1 | Maximum false negatives (-1 = no limit) |
| `--max-fp` | 0 | Maximum false positives |
| `--corpus` | auto | Path to corpus testdata directory |

Exit codes: 0 = within thresholds, 1 = exceeded or error.

### PDF.js Subcommand

Compares litho's text extraction, metadata, and coordinates against Mozilla's PDF.js test suite (~1000 PDFs).

```bash
# Provision via Docker (first time only), then compare
go run ./test/integration/cmd/corpus pdfjs --init

# Compare only (corpus already provisioned)
go run ./test/integration/cmd/corpus pdfjs

# JSON output
go run ./test/integration/cmd/corpus pdfjs --format json

# CI mode: exit 1 if text mismatch percentage exceeds threshold
go run ./test/integration/cmd/corpus pdfjs --max-text-errors 50

# Custom corpus path
go run ./test/integration/cmd/corpus pdfjs --corpus /path/to/testdata/pdf.js
```

Flags:

| Flag | Default | Description |
|---|---|---|
| `--init` | false | Provision corpus via Docker before comparison |
| `--format` | text | Output format: `text` or `json` |
| `--max-text-errors` | -1 | Maximum text mismatch percentage (-1 = no limit) |
| `--corpus` | auto | Path to corpus testdata directory |

Exit codes: 0 = within thresholds, 1 = exceeded or error.

## Corpus Data

Corpus data is cached in `testdata/` (gitignored). Delete the relevant directory to force regeneration:

```bash
make clean-corpus  # Remove all corpus data
```
