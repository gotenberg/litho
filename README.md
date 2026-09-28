# litho

A pure-Go PDF Conforming Processor. Read, validate, remediate, and produce PDF documents with no C dependencies.

litho implements ISO 32000 (PDF 1.4 through 2.0) in pure Go. It extracts text and structured content, validates documents against PDF/A, PDF/UA, Factur-X, and ZUGFeRD, repairs non-conformant files, and applies transformations such as rotate, split, merge, stamp, encrypt, and sign. Two rules govern the codebase: every output is a valid PDF that viewers can open, and every public type honors its documented concurrency contract under `-race`.

- Module: `github.com/gotenberg/litho`
- Go: 1.27.0+
- No CGO required.

## Features

### Read and extract

- Text, with spatial layout inference and an optional OCR hand-off.
- Positioned text elements, tables, and stroked lines.
- Images, decoded on request to sRGB pixels through their ICC profiles.
- Annotations, form fields, and field values.
- Bookmarks, embedded files, and digital signatures with LTV verification.
- PDF 2.0 data: geospatial tie points, measurements, 3D models, MathML formulas, and pronunciation lexicons.
- Document metadata from the Info dictionary and the raw XMP packet.

### Validate

- PDF/A: every part and level (1a/1b, 2a/2b/2u, 3a/3b/3u, 4/4e/4f).
- PDF/UA: UA-1 and UA-2.
- Factur-X 1.0+ and ZUGFeRD 1.0 / 2.0 / 2.1, including the embedded Cross-Industry Invoice spine, its XSD, and the EN 16931 and XRechnung Schematron rules.
- Each violation carries a structured clause, test number, and severity.

### Remediate

- Repair PDF/A and PDF/UA violations behind a regression-bounded integrity probe that rolls back a content-modifying fix if it degrades the document.
- Two modes: content-preserving by default, or aggressive via `WithAggressiveFixes`.
- Embed the fonts a document leaves unembedded from programs you supply (`WithFontProvider`, `WithFontFiles`, `--font-dir`). litho ships no font program: it embeds one only when it holds every glyph the document shows at the widths the document declares, subsets it to those glyphs, and reports every font it refuses.
- Build Factur-X and ZUGFeRD hybrid invoices by embedding a Cross-Industry Invoice payload and remediating the PDF/A-3 base.
- A remediation is the last edit on a document: it judges the document the edits queued before it produce, so its report describes the output. A PDF/A output is never encrypted, since ISO 19005 forbids it.

### Transform and compose

- Rotate, delete, insert, split, and merge pages.
- Stamp text, images, and PDF overlays for watermarks.
- Encrypt (RC4, AES-128, AES-256), decrypt, and sign (PKCS#7/CMS, incremental).
- Flatten forms, set metadata and form fields, and manage attachments and bookmarks.
- Chain operations through a `Pipeline` with managed intermediate storage.

## Install

```bash
go get github.com/gotenberg/litho
```

Build the CLI:

```bash
go build -o litho ./cmd/litho
```

## Library

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

func main() {
	ctx := context.Background()

	r, err := pdf.OpenFile(ctx, "input.pdf")
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	text, err := r.ExtractText(ctx, 1)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(text)

	// Validate against PDF/A-2b. Each violation reports its clause, test, and severity.
	violations, err := r.ValidatePDFAReport(ctx, models.PDFA2b)
	if err != nil {
		log.Fatal(err)
	}
	for _, v := range violations {
		fmt.Printf("[%s] clause %s test %d: %s\n", v.Severity, v.Clause, v.TestNumber, v.Description)
	}

	// Remediate, then write the result.
	w := pdf.NewWriter(r)
	defer w.Close()
	report, err := w.RemediatePDFA(ctx, models.PDFA2b)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("fixed %d, residual %d\n", len(report.Fixed), len(report.Residual))

	out, err := os.Create("output.pdf")
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	if err := w.WriteTo(ctx, out); err != nil {
		log.Fatal(err)
	}
}
```

The public surface lives in three types: `Reader` opens and extracts, `Writer` queues mutations and emits, and `Pipeline` chains operations. See the [godoc](https://pkg.go.dev/github.com/gotenberg/litho/pkg/pdf) for the full API.

`Reader` is safe for concurrent use after construction. `Writer` is single-use and not safe for concurrent use. A `Writer` never modifies the `Reader` it is built on, so several may share one `Reader` while other goroutines read it. Close a `Writer` when done: a remediated one holds its output in a temp file until `WriteTo` streams it or `Close` removes it.

A remediation is the last edit on a `Writer`. Queue every other edit before it: afterwards the `Writer` accepts `Sign`, `WriteTo` and `Close`, and refuses anything else, a second remediation included, with `ErrEditAfterRemediation`.

## CLI

`litho` exposes the same capabilities as subcommands.

```bash
# Inspect a document.
litho info -f json document.pdf

# Extract text and image metadata.
litho text document.pdf
litho images -f json document.pdf

# Validate compliance.
litho validate --profile PDF/A-2b document.pdf
litho validate --profile Factur-X invoice.pdf

# Remediate.
litho remediate --profile PDF/A-2b -o fixed.pdf document.pdf

# Transform.
litho rotate --angle 90 -o rotated.pdf document.pdf
litho merge -o merged.pdf a.pdf b.pdf
litho stamp-text --text "DRAFT" -o stamped.pdf document.pdf

# Chain operations in one command. A remediate stage runs last.
litho pipe document.pdf rotate --angle 90 stamp-text --text "DRAFT" -o out.pdf
litho pipe document.pdf rotate --angle 90 remediate --profile PDF/UA-1 -o out.pdf

# Redact and encrypt.
litho redact --region 1:72,700,200,24 -o redacted.pdf document.pdf
litho encrypt --user-password secret -o encrypted.pdf document.pdf
```

Run `litho` with no arguments to list every subcommand, or `litho <command> -h` to see its flags.

Signing is library-only. `Writer.Sign` takes a `pdf.Signer`, which the CLI cannot supply without additional crypto dependencies, so `litho sign` refuses with that explanation and exits `1`.

### Resource limits

Every document is read under five budgets that keep a crafted file from asking for unbounded memory or work. Global flags, placed before the command, change them for every document the command reads, each input of a multi-input command and each `pipe` stage included:

| Flag                        | Bounds                                              | Default     |
| --------------------------- | --------------------------------------------------- | ----------- |
| `--max-stream-bytes`        | Decoded bytes of one stream                         | `256MiB`    |
| `--max-image-pixels`        | Pixels of one image                                 | `100000000` |
| `--max-embedded-file-bytes` | Size of an embedded PDF that PDF/A validation opens | `50MiB`     |
| `--max-form-depth`          | Form XObjects nested below a page                   | `10`        |
| `--max-cached-objects`      | Parsed objects kept in memory                       | `50000`     |

A byte count takes a plain integer or a `KiB`, `MiB` or `GiB` suffix, the others a plain integer, and `0` keeps the default. A value that does not parse, or one past its ceiling, exits `1` before any document opens.

`--max-stream-bytes` also bounds the content of one page, summed across its content streams, at a quarter of its value: raising it is how to read a page heavier than `64MiB`.

```bash
litho --max-stream-bytes 64MiB --max-image-pixels 25000000 images document.pdf
```

The library takes the same budgets through `pdf.WithLimits`; `pdf.Limits` documents what each one bounds and the error a document past it returns.

### Encrypted documents

Editing an encrypted document produces an unprotected one. litho opens the file with the password supplied, writes out a decrypted object graph, and emits no `/Encrypt` dictionary unless one is requested. No command preserves the source's encryption. Every command that writes a document warns on stderr when the input carried protection the output will not.

`decrypt` is the deliberate exception and stays silent, because producing an unprotected document is what it was asked to do.

Re-protect the result in one of three ways. `split` and `merge` accept `--user-password` directly. `pipe` accepts an `encrypt` stage, which applies to the document it writes. Any other command can be followed by `litho encrypt`. A PDF/A output is the exception: ISO 19005 forbids encryption, so `pipe` refuses an `encrypt` stage beside a PDF/A `remediate` stage, and encrypting the result costs its conformance.

### Exit codes

| Code  | Meaning                                                                      |
| ----- | ---------------------------------------------------------------------------- |
| `0`   | Success.                                                                     |
| `1`   | The command failed: a missing file, an invalid flag, an unreadable document. |
| `2`   | The document did not conform.                                                |
| `124` | The `--timeout` deadline expired, the status `timeout(1)` reports.           |

`2` is deliberately distinct from `1`, so a caller can tell "litho could not run" from "the document is not conformant". `validate` reports it for a file with violations, and `remediate --strict` and `pipe --strict` for one they could not fully repair; those two still write their output, since the status is a verdict rather than a refusal to produce a document.

A command that reached a verdict before its deadline expired keeps that verdict: only a failure becomes `124`.

## Filter plugins

litho decodes the standard PDF stream filters in pure Go, JBIG2Decode and JPXDecode included. A stream behind a filter litho cannot decode, such as an unknown one, is reported through `Reader.UndecodableStreams`, or `litho info --streams`. To decode such a filter, or to replace a built-in decoder, implement `plugins.FilterDecoder` and register it when opening a document; it takes precedence over the built-in decoder for its filter name:

```go
r, err := pdf.OpenFile(ctx, "doc.pdf", pdf.WithFilterPlugin(myDecoder))
```

## Development

```bash
make fmt && make lint # zero-warnings policy
make test-unit        # go test -race ./...
make bench            # go test -bench=. -benchmem ./...
make corpus           # score against the veraPDF, PDF.js, and pdfcpu corpora
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the coding, safety, and documentation rules.

## Acknowledgments

litho's text extraction, CMap handling, and font resolution draw on Mozilla's [PDF.js](https://github.com/mozilla/pdf.js). Its PDF/A and PDF/UA rule set and clause numbering follow [veraPDF](https://github.com/veraPDF). Grateful acknowledgment to both communities.
