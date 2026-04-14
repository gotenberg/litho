# Integration Tests

Corpus-based tests comparing litho against VeraPDF and PDF.js reference results. Requires Docker for initial provisioning.

## VeraPDF

Compares PDF/A and PDF/UA pass/fail verdicts against 2,601 VeraPDF test PDFs.

```bash
go run ./test/integration/cmd/corpus verapdf --init # provision + compare
go run ./test/integration/cmd/corpus verapdf        # compare only
go run ./test/integration/cmd/corpus verapdf --format json --profile PDF_A-4
go run ./test/integration/cmd/corpus verapdf --max-fn 3 --max-fp 0 # CI thresholds
```

| Flag        | Default | Description                         |
| ----------- | ------- | ----------------------------------- |
| `--init`    | false   | Provision corpus via Docker first   |
| `--format`  | text    | `text` or `json`                    |
| `--profile` | (all)   | Filter to one profile directory     |
| `--max-fn`  | -1      | Max false negatives (-1 = no limit) |
| `--max-fp`  | 0       | Max false positives                 |
| `--corpus`  | auto    | Override testdata path              |

## PDF.js

Compares text extraction, metadata, and coordinates against ~1,000 PDF.js test PDFs.

```bash
go run ./test/integration/cmd/corpus pdfjs --init
go run ./test/integration/cmd/corpus pdfjs --format json
go run ./test/integration/cmd/corpus pdfjs --max-text-errors 50
```

| Flag                | Default | Description                                  |
| ------------------- | ------- | -------------------------------------------- |
| `--init`            | false   | Provision corpus via Docker first            |
| `--format`          | text    | `text` or `json`                             |
| `--max-text-errors` | -1      | Max text mismatch percentage (-1 = no limit) |
| `--corpus`          | auto    | Override testdata path                       |

## Remediation

Applies the remediation engine to every failing VeraPDF corpus file and reports before/after violation counts.

```bash
go run ./test/integration/cmd/corpus remediate
go run ./test/integration/cmd/corpus remediate --format json --profile PDF_A-4
go run ./test/integration/cmd/corpus remediate --max-regressions 0 --min-fixed-pct 85
```

| Flag                | Default | Description                          |
| ------------------- | ------- | ------------------------------------ |
| `--format`          | text    | `text` or `json`                     |
| `--profile`         | (all)   | Filter to one profile directory      |
| `--aggressive`      | false   | Enable content-modifying fixers      |
| `--detail`          | false   | Print residual-violation breakdown   |
| `--max-regressions` | -1      | Max regressed files (-1 = no limit)  |
| `--min-fixed-pct`   | 0       | Min aggregate fix percentage (0-100) |
| `--corpus`          | auto    | Override testdata path               |

## Corpus data

Cached in `testdata/` (gitignored). Delete to force re-provisioning:

```bash
make clean-corpus
```

Exit codes: 0 = within thresholds, 1 = exceeded or error.
