<p align="center">
    <img src="https://raw.githubusercontent.com/gotenberg/art/main/litho-logo.svg" alt="litho Logo" width="600" />
    <h3 align="center">litho</h3>
    <p align="center">A PDF Conforming Processor</p>
    <p align="center">
        <a href="https://github.com/gotenberg/litho/releases/latest"><img alt="Latest version" src="https://img.shields.io/github/v/release/gotenberg/litho?label=latest"></a>
        <a href="https://github.com/gotenberg/litho/pkgs/container/litho"><img alt="GitHub Packages downloads" src="https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fipitio.github.io%2Fbackage%2Fgotenberg%2Flitho%2Flitho.json&query=%24.downloads&logo=github&label=downloads&color=blue"></a>
        <a href="https://github.com/gotenberg/litho/actions/workflows/continuous-integration.yml"><img alt="Continuous Integration" src="https://github.com/gotenberg/litho/actions/workflows/continuous-integration.yml/badge.svg"></a>
        <a href="https://pkg.go.dev/github.com/gotenberg/litho"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/gotenberg/litho.svg"></a>
    </p>
</p>

---

**litho** is a pure-Go PDF Conforming Processor. Read, validate, remediate, and produce PDF documents with no C dependencies.

litho implements ISO 32000 (PDF 1.4 through 2.0). It extracts text and structured content, validates against PDF/A, PDF/UA, WTPDF, Factur-X and ZUGFeRD, repairs non-conformant files, and edits, renders, encrypts and signs them. Every output is a valid PDF, and every public type honors its documented concurrency contract under `-race`.

## Install

```bash
go get github.com/gotenberg/litho
go install github.com/gotenberg/litho/cmd/litho@latest
```

Go 1.27.3+. No CGO.

Prebuilt `litho` binaries for Linux (amd64, arm64, 386, armv7, ppc64le, riscv64, s390x, loong64), macOS, FreeBSD and Windows (amd64, arm64) ship with each [GitHub release](https://github.com/gotenberg/litho/releases). On Linux, macOS and FreeBSD, the install script downloads one, verifies its checksum and installs it in `/usr/local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/gotenberg/litho/main/install.sh | sh
```

Set `LITHO_VERSION=v1.2.3` to pin a release and `LITHO_INSTALL_DIR` to install elsewhere.

## Quick start

```go
r, err := pdf.OpenFile(ctx, "input.pdf")
if err != nil {
	log.Fatal(err)
}
defer r.Close()

violations, err := r.ValidatePDFAReport(ctx, models.PDFA2b)
```

```bash
litho validate --profile PDF/A-2b document.pdf
litho remediate --profile PDF/A-2b --font-dir /usr/share/fonts -o fixed.pdf document.pdf
litho sign --cert cert.pem --key key.pem --reason Approved -o signed.pdf document.pdf
```

Run `litho` with no arguments to list every subcommand, or `litho <command> -h` to see its flags.

## Features

- **Extract** text with layout inference, positioned text, tables, images decoded to sRGB, annotations, form fields, bookmarks, attachments, and PDF 2.0 data (geospatial, measurements, 3D, formulas).
- **Validate** PDF/A (every part and level), PDF/UA-1 and UA-2, WTPDF 1.0, Factur-X and ZUGFeRD, with the clause, test number and severity of each violation.
- **Remediate** PDF/A, PDF/UA and WTPDF violations, embedding the fonts you supply, behind an integrity check that rolls back a fix that degrades the document.
- **Edit** pages (rotate, delete, insert, split, merge), stamps and watermarks, metadata, form fields, bookmarks and attachments, auto-tagging, redaction, colour conversion and transparency flattening.
- **Render** pages to images, on screen or for print, RGB or CMYK.
- **Protect** with RC4, AES-128 or AES-256 encryption, and PKCS#7 or PAdES signatures up to B-LTA.
- **Optimize** with object streams, stream deduplication, lossless image recompression and font subsetting.
- **Stay safe** on hostile input: configurable resource limits, no panics, deterministic output.

## Documentation

- [Library reference](https://pkg.go.dev/github.com/gotenberg/litho/pkg/pdf): `Reader` opens and extracts, `Writer` queues edits and writes, `Pipeline` chains them.
- [Examples](examples/): runnable programs for common tasks.
- [CLI reference](https://pkg.go.dev/github.com/gotenberg/litho/cmd/litho): global flags, exit status, encrypted documents.

## Contributing

- Contributing guide: [CONTRIBUTING.md](CONTRIBUTING.md)
- Bug reports: [GitHub Issues](https://github.com/gotenberg/litho/issues)
- Security vulnerabilities: [SECURITY.md](SECURITY.md)

## Acknowledgments

litho's text extraction, CMap handling and font resolution draw on Mozilla's [PDF.js](https://github.com/mozilla/pdf.js). Its PDF/A, PDF/UA and WTPDF rule set and clause numbering follow [veraPDF](https://github.com/veraPDF).
