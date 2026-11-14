# litho

Pure-Go PDF Conforming Processor for ISO 32000 (PDF 1.4 through 2.0). It extracts text and structured content, validates against PDF/A, PDF/UA, WTPDF, Factur-X and ZUGFeRD, repairs non-conformant files, and edits, renders, encrypts and signs them, with no C dependencies.

> [!NOTE]
> litho powers the PDF operations of [Gotenberg](https://github.com/gotenberg/gotenberg) 9. Its API is pre-1.0 and may change between releases.

## Install

```bash
go get github.com/gotenberg/litho
```

Go 1.27.3+. No CGO.

## Quick start

```go
r, err := pdf.OpenFile(ctx, "input.pdf")
if err != nil {
	log.Fatal(err)
}
defer r.Close()

violations, err := r.ValidatePDFAReport(ctx, models.PDFA2b)
```

## Features

- **Extract** text with layout inference, positioned text, tables, images decoded to sRGB, annotations, form fields, bookmarks, attachments, and PDF 2.0 data (geospatial, measurements, 3D, formulas).
- **Validate** PDF/A (every part and level), PDF/UA-1 and UA-2, WTPDF 1.0, Factur-X and ZUGFeRD, with the clause, test number and severity of each violation.
- **Remediate** PDF/A, PDF/UA and WTPDF violations, to one profile or to a PDF/A and PDF/UA pair at once, embedding the fonts you supply, behind an integrity check that rolls back a fix that degrades the document.
- **Edit** pages (rotate, delete, insert, split, merge), stamps and watermarks, metadata, form fields, bookmarks and attachments, auto-tagging, redaction, colour conversion and transparency flattening.
- **Render** pages to images, on screen or for print, RGB or CMYK.
- **Protect** with RC4, AES-128 or AES-256 encryption, and PKCS#7 or PAdES signatures up to B-LTA.
- **Optimize** with object streams, stream deduplication, lossless image recompression and font subsetting.
- **Stay safe** on hostile input: configurable resource limits, no panics, deterministic output.

## Documentation

- [Library reference](https://pkg.go.dev/github.com/gotenberg/litho/pkg/pdf): `Reader` opens and extracts, `Writer` queues edits and writes, `Pipeline` chains them.
- [Examples](examples/): runnable programs for common tasks.

## Contributing

- Contributing guide: [CONTRIBUTING.md](CONTRIBUTING.md)
- Bug reports: [GitHub Issues](https://github.com/gotenberg/litho/issues)
- Security vulnerabilities: [SECURITY.md](SECURITY.md)

## License

litho is licensed under the [GNU Affero General Public License v3.0](LICENSE).

[Gotenberg](https://github.com/gotenberg/gotenberg) embeds litho and ships it under the MIT License, as its copyright owner allows. Contributors sign the [Contributor License Agreement](CLA.md), which grants that right for their contributions. Any other use of litho follows the AGPL-3.0.

## Acknowledgments

litho's text extraction, CMap handling and font resolution draw on Mozilla's [PDF.js](https://github.com/mozilla/pdf.js). Its PDF/A, PDF/UA and WTPDF rule set and clause numbering follow [veraPDF](https://github.com/veraPDF).
