# litho

A pure-Go PDF Conforming Processor. Read, validate, remediate, and produce PDF documents with no C dependencies.

litho implements ISO 32000 (PDF 1.4 through 2.0) in pure Go. It extracts text and structured content, validates documents against PDF/A, PDF/UA, Factur-X, and ZUGFeRD, repairs non-conformant files, and applies transformations such as rotate, split, merge, stamp, encrypt, and sign. Two rules govern the codebase: every output is a valid PDF that viewers can open, and every public type honors its documented concurrency contract under `-race`.

- Module: `github.com/gotenberg/litho`
- Go: 1.26.0+
- No CGO required. Optional CGO plugins add JPXDecode and JBIG2Decode.

## Features

### Read and extract

- Text, with spatial layout inference and an optional OCR hand-off.
- Positioned text elements, tables, and stroked lines.
- Images, annotations, form fields, and field values.
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
- Build Factur-X and ZUGFeRD hybrid invoices by embedding a Cross-Industry Invoice payload and remediating the PDF/A-3 base.

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

`Reader` is safe for concurrent use after construction. `Writer` is single-use and not safe for concurrent use.

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

# Encrypt and sign.
litho encrypt --user-password secret -o encrypted.pdf document.pdf
litho sign --cert cert.pem --key key.pem -o signed.pdf document.pdf
```

Run `litho` with no arguments to list every subcommand, or `litho <command>` to see its flags.

## Optional CGO plugins

litho decodes the standard PDF stream filters in pure Go. The two image filters that need an external C library are opt-in plugins:

| Plugin              | C library                             | Filter      |
| ------------------- | ------------------------------------- | ----------- |
| `pkg/plugins/jpx`   | libopenjp2 (`brew install openjpeg`)  | JPXDecode   |
| `pkg/plugins/jbig2` | libjbig2dec (`brew install jbig2dec`) | JBIG2Decode |

Without CGO or the library, litho skips the filter and reports the stream through `Reader.UndecodableStreams`, or `litho info --streams`. Register a plugin when opening a document:

```go
import "github.com/gotenberg/litho/pkg/plugins/jbig2"

r, err := pdf.OpenFile(ctx, "doc.pdf", pdf.WithFilterPlugin(jbig2.New()))
```

## Development

```bash
make fmt && make lint # zero-warnings policy
make test-unit        # go test -race ./...
make bench            # go test -bench=. -benchmem ./...
make corpus           # score against the VeraPDF, PDF.js, and pdfcpu corpora
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the coding, safety, and documentation rules.

## Acknowledgments

litho's text extraction, CMap handling, and font resolution draw on Mozilla's [PDF.js](https://github.com/mozilla/pdf.js). Its PDF/A and PDF/UA rule set and clause numbering follow [veraPDF](https://github.com/veraPDF). Grateful acknowledgment to both communities.
