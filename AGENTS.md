# AGENTS.md

## Project Identity

**litho** is an enterprise-grade, non-visual PDF Conforming Reader written in pure Go. It extracts text, metadata, forms, signatures, images, and structural data from PDF files across all versions (1.0 through 2.0). Visual rasterization is out of scope.

Module: `github.com/gotenberg/litho`
Go version: 1.26.0

## Build & Quality Commands

```bash
make fmt          # gofmt + gofumpt + gci + goimports + go mod tidy
make lint         # golangci-lint (strict, see .golangci.yml)
make lint-todo    # Find TODO comments via godox
go test ./...     # Run all tests
go build ./cmd/litho  # Build the binary
```

Always run `make fmt && make lint` before submitting code.

## Project Structure

```
cmd/litho/                    # Binary entrypoint
pkg/
  pdf/                        # Public API (Facade). The ONLY entry point for consumers.
    reader.go                 # Document reader — accepts io.ReaderAt
  models/                     # Public data structs (Page, TextElement, Bookmark, etc.)
  compliance/                 # PDF/A and PDF/UA ISO validation (public API wraps internals)
  export/                     # Serialization layer (JSON, CSV, Markdown)
  internal/                   # Engine internals — NEVER expose types from here
    core/
      ast/                    # PDF object AST (PdfDict, PdfArray, PdfName, PdfStream, etc.)
      document/               # Document model, catalog, page tree, linearization, XRef rebuild
      ioat/                   # io.ReaderAt adapter
      lexer/                  # Byte-level PDF tokenizer
      parser/                 # Recursive descent PDF object parser
      xref/                   # Cross-reference table engine
    crypto/                   # RC4, AES-128, AES-256, password recovery
    filter/                   # Stream decode filters (Flate, ASCII85, predictors)
    graphics/
      content/                # Content stream operator lexer
      image/                  # Image extraction and decoding
      postscript/             # Type 4 PostScript function VM
      state.go                # Graphics state machine (CTM, colors, text state)
      lines.go                # Line/path detection
    heuristics/               # Table recognition, OCR hints, redaction detection
    interactive/
      acroform.go             # AcroForm field extraction
      annotation.go           # Annotations and links
      javascript.go           # JavaScript form field evaluation
      xfa.go                  # XFA XML Forms
    semantics/
      metadata.go             # Document info + XMP
      attachment.go           # Embedded files
      outline.go              # Bookmarks
      portfolio.go            # PDF Portfolios / Collections
      geospatial.go           # GeoJSON coordinates
      threedee.go             # 3D/U3D/PRC streams
      threads.go              # Article threads
      structure/              # Tagged PDF structure tree
    signature/
      signature.go            # PKCS#7 extraction
      verify.go               # X.509 certificate chain validation
      dss.go                  # Document Security Store
      timestamp.go            # PAdES / LTV timestamps
    typography/
      font.go                 # Font resolution (Simple, CID, Type 3)
      base14.go               # Standard 14 Adobe font metrics
      bidi.go                 # Bidirectional text + soft hyphens
      text.go                 # Text reconstruction (spatial layout)
      helpers.go              # Shared typography utilities
      cmap/                   # ToUnicode CMap parser
      encodings/              # MacRoman, WinAnsi, Differences, Adobe Glyph List
      truetype/               # Embedded TrueType cmap table parser
```

## Coding Rules

### Error Handling
- Never assign errors to `_` unless explicitly justified (e.g., safe hash writes).
- Always wrap and propagate errors up the call stack.
- Use `errors.Is()` with sentinel variables — never `strings.Contains()`.
- Sentinel errors live in `errors.go` per package.

### Encapsulation (Critical)
- Public packages (`pkg/pdf`, `pkg/models`, `pkg/compliance`, `pkg/export`) must **never** accept or return types from `pkg/internal/`.
- The public API accepts `io.ReaderAt` + `size int64`. Internal document parsing happens behind the facade.
- Map all internal types to `pkg/models` structs before returning to callers.

### Parser Safety
- Never blindly advance the lexer. Validate token types using explicit `Expect` logic.
- Handle `io.EOF` gracefully — never panic.

### Centralized Core Utilities (Anti-Duplication)
- NEVER duplicate core PDF operational logic (e.g., `resolveObject`, `decodePdfString`, `getFloat`) across internal domains.
- PDF strings are often encrypted; decryption must be handled consistently through the crypto pipeline.
- `resolveObject` must track visited nodes to prevent infinite loops from malicious circular references.
- Rely on centralized, thread-safe methods provided by the `internal/core/document` package to ensure security and prevent stack overflows.

### Formatting & Imports
- Run `make fmt` to enforce formatting (gofmt, gofumpt, goimports, gci).
- Import order enforced by gci: standard library, third-party, then `github.com/gotenberg/litho/...`.

### Linting
- The project uses strict `golangci-lint` with ~30 linters enabled (gosec, errcheck, prealloc, exhaustive, etc.).
- Run `make lint` — zero warnings policy.

### Documentation
- All public types and functions require Godoc comments.
- Each package should have a `doc.go` with `// Package foo ...` documentation.
- Technical debt must be marked with `// TODO: [Context]`.

### Testing
- Tests use `_test.go` convention in the same package.
- The compliance package uses a `mockDocBuilder` pattern to construct test PDF documents.
- Fuzz tests exist for parser and lexer (`_fuzz_test.go`).

## Dependencies

Minimal by design — pure Go with only two direct dependencies:
- `github.com/dop251/goja` — JavaScript VM for dynamic form field evaluation.
- `golang.org/x/text` — Unicode text processing.

## What Is NOT Implemented

- **JBIG2Decode / JPXDecode (JPEG2000):** Scanned image filters are not yet supported.
- **DeviceN / Separation color spaces:** Complex print industry spot colors (e.g., PANTONE) are not yet supported.
