# Contributing to litho

**litho** is a pure-Go, non-visual PDF Conforming Reader. It extracts text, metadata, forms, signatures, images, and structural data from PDF files across all versions (1.0 through 2.0). It also validates PDF/A (ISO 19005) and PDF/UA (ISO 14289) compliance. Visual rasterization is out of scope.

- Module: `github.com/gotenberg/litho`
- Go version: 1.26.0

## Getting Started

### Prerequisites

- Go 1.26.0+
- Docker (for corpus provisioning)
- CGO + libopenjp2 (optional, for JPXDecode filter support)

### Build and Run

```bash
go build ./cmd/litho
```

### Development Loop

```bash
make fmt                                   # gofmt + gofumpt + gci + goimports + go mod tidy
make lint                                  # golangci-lint (strict, see .golangci.yml)
make lint-todo                             # Find TODO comments via godox
make test-unit                             # Unit tests only (go test -race ./...)
make corpus-verapdf                        # VeraPDF corpus comparison (text output)
make corpus-verapdf-json                   # VeraPDF corpus comparison (JSON output)
make corpus-verapdf-init                   # Provision VeraPDF corpus via Docker, then compare
make corpus-pdfjs                          # PDF.js corpus comparison (text output)
make corpus-pdfjs-json                     # PDF.js corpus comparison (JSON output)
make corpus-pdfjs-init                     # Provision PDF.js corpus via Docker, then compare
make clean-corpus                          # Remove all corpus data (forces re-provisioning)
```

Run `make fmt && make lint` before submitting code. Zero warnings policy.

## Submitting a Pull Request

Non-trivial changes benefit from planning before coding. Present the problem statement, proposed approach, affected files, and testing strategy. Get alignment before investing in implementation.

### PR Checklist

- [ ] **Encapsulation:** no `pkg/internal` types leak into public signatures.
- [ ] **Error handling:** all errors wrapped and propagated; no silenced errors without justification.
- [ ] **Context:** I/O-bound or recursive functions accept and check `context.Context`.
- [ ] **Resource streaming:** no eager materialization of binary resources.
- [ ] **Linting:** `make fmt && make lint` passes with zero warnings.
- [ ] **Code quality:** no dead code, no commented-out code, no TODO without context.
- [ ] **Documentation:** public types and functions have Godoc comments.
- [ ] **Tests:** new behavior is covered; no regressions.
- [ ] **Scope:** changes are limited to what was planned.

### Commit Conventions

Follow [Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`.

Stage specific files only. Never `git add -A` or `git add .`. Do not push unless explicitly asked.

## Core Principles

### Error Handling

- Never assign errors to `_` unless explicitly justified (e.g., safe hash writes).
- Always wrap and propagate errors up the call stack.
- Use `errors.Is()` with sentinel variables, never `strings.Contains()`.
- Sentinel errors live in `errors.go` per package.

### Context and Cancellation

Every public method that performs I/O, loops, or recursion must accept `context.Context` as its first parameter. litho is designed for server use (Gotenberg) where HTTP request deadlines govern processing time.

Never store `ctx` in a struct field. Pass it through function parameters, per Go convention.

Check `ctx.Err()` at natural checkpoints in internal loops:
- Content stream operator dispatch (every ~100 operators).
- Page tree traversal (before processing each node).
- Object resolution (on each step, alongside cycle detection).
- CMap parsing (per entry or per block).
- Font resolution (at the top of `resolveFont` and inside encoding table iteration).
- XRef table parsing and rebuild (per entry).
- Filter pipeline (per read buffer during decompression).
- JavaScript VM execution (use goja's interrupt mechanism with `ctx`).

Do NOT add context checks in the lexer's per-token loop. The lexer is the hottest path; checking one level up (per content stream operator) is sufficient.

Wrap context errors with location info: `fmt.Errorf("extracting text from page %d: %w", pageNum, err)`.

Never return partial results on context cancellation. Return an error. If a future use case requires partial results, that would be a separate streaming API.

Do not introduce litho-specific timeout sentinels. `context.DeadlineExceeded` and `context.Canceled` are sufficient.

Trivial accessors that return cached scalar values (e.g., page count, PDF version) do not need `ctx`.

### Encapsulation

Public packages (`pkg/pdf`, `pkg/models`) must never accept or return types from `pkg/internal/`. The public API accepts `io.ReaderAt` + `size int64`. Internal document parsing happens behind the facade. Map all internal types to `pkg/models` structs before returning to callers.

### Parser Safety

Never blindly advance the lexer. Validate token types using explicit `Expect` logic. Handle `io.EOF` gracefully, never panic.

### Centralized Core Utilities

Never duplicate core PDF operational logic (e.g., `ResolveDict`, `DecodePdfString`, `GetFloat`) across internal domains. PDF strings are often encrypted; decryption must be handled consistently through the crypto pipeline.

Prefer typed resolve helpers over `ResolveObject`: use `ResolveDict`, `ResolveArray`, `ResolveName`, `ResolveStream`, `ResolveString`, `ResolveInt`, `ResolveBool` from `internal/core/document`. Reserve `ResolveObject` only for multi-type switches.

`ResolveObject` must track visited nodes to prevent infinite loops from malicious circular references. Context cancellation adds a wall-clock bound on top of this structural bound.

### Resource Streaming (No Eager Materialization)

Never load embedded files, images, or other binary resources into memory during parsing. The reader collects metadata (byte offset, length, filters, MIME type) without decoding or buffering the resource content.

The public API returns a lazy accessor for every resource: a function field like `Open func(ctx context.Context) (io.ReadCloser, error)` rather than a `[]byte` field. The underlying implementation reads from the original `io.ReaderAt` at the recorded offset only when `Open` is called.

Stream filters (Flate, ASCII85, DCT, etc.) are applied lazily inside the `Open` implementation, chaining the decoded stream through the filter pipeline on read, not at parse time.

Within `pkg/internal`, the same rule applies: internal helpers that enumerate resources store stream coordinates, not decoded bytes.

**Test guidance:** unit tests verifying resource content should call `Open`, read, and close. Tests verifying only metadata (filename, MIME type, dimensions) must not call `Open`.

## Project Layout

```
cmd/litho/                       CLI binary
cmd/gen-rules/                   Code generator for compliance rules
pkg/pdf/                         Public API facade (io.ReaderAt + size)
pkg/models/                      Public model types returned to callers
pkg/plugins/                     Filter plugins (jpx, jbig2)
pkg/internal/
  core/ast/                      PDF object AST types
  core/document/                 Document resolution, object access
  core/parser/                   PDF lexer/parser
  compliance/                    PDF/A and PDF/UA compliance engine
  graphics/                      Content stream parsing
  typography/                    Font program parsing (TrueType, CFF, Type1)
  text/                          Text extraction
test/integration/cmd/corpus/     Corpus comparison CLI (VeraPDF, PDF.js)
test/integration/corpus/         Docker setup for corpus provisioning
```

## Documentation

### Writing Style

- **Short, declarative sentences.** Say what it does, then stop.
- **Lead with the action.** "Validates font embedding" not "This function validates font embedding".
- **Active voice.** "litho checks the profile" not "The profile is checked by litho".
- **No em dashes.** Use a period, colon, or comma instead.
- **No "we" hedging.** "Don't..." not "We do not recommend...".

### Godoc

All exported types and functions require Godoc comments. Start with the identifier name:

```go
// Violation records a single rule violation with context.
type Violation struct { ... }

// ValidatePDFA audits the document against a PDF/A profile.
func ValidatePDFA(ctx context.Context, ...) ([]error, error)
```

Each package should have a `doc.go` with a `// Package foo ...` comment.

### Code Comments

- Explain *why*, not *what*. The code shows what; the comment explains the non-obvious reasoning.
- No numbered step comments (`// 1. Do X`, `// 2. Do Y`).
- No section dividers with numbers (`// --- 8. Foo ---`). Plain dividers are fine for major boundaries (`// --- VeraPDF ---`).
- No noise comments that restate the code (`// Check if err is nil`, `// Return results`).
- Reference spec clauses where relevant (`// Per ISO 32000-2, Table 116...`).
- Mark technical debt with `// TODO: [context]`.

## Formatting and Imports

Run `make fmt` to enforce formatting (gofmt, gofumpt, goimports, gci). Import order enforced by gci: standard library, third-party, then `github.com/gotenberg/litho/...`.

## Testing

### Unit Tests

Tests use `_test.go` convention in the same package. Live under `pkg/` and `cmd/` alongside the code they test. Run with `make test-unit`. Fuzz tests exist for parser and lexer (`_fuzz_test.go`).

### Corpus Testing

A CLI tool at `test/integration/cmd/corpus/` compares litho against reference corpus results. Corpus data is cached in `test/integration/testdata/` (not checked into Git). Provision via Docker on first use with `--init`.

- **VeraPDF corpus:** compares PDF/A and PDF/UA compliance verdicts against VeraPDF's reference results (2,601 test PDFs).
- **PDF.js corpus:** compares text extraction, metadata, and coordinates against Mozilla's PDF.js test suite (~1,000 PDFs).

```bash
# Provision and run VeraPDF corpus
go run ./test/integration/cmd/corpus verapdf --init

# Run comparison only (corpus already provisioned)
go run ./test/integration/cmd/corpus verapdf

# JSON output, filter to one profile
go run ./test/integration/cmd/corpus verapdf --format json --profile PDF_A-4

# CI threshold enforcement (exit 1 if exceeded)
go run ./test/integration/cmd/corpus verapdf --max-fn 3 --max-fp 0

# PDF.js corpus
go run ./test/integration/cmd/corpus pdfjs --init
go run ./test/integration/cmd/corpus pdfjs --format json
```

See [test/integration/README.md](test/integration/README.md) for full flag reference.

## Dependencies

Minimal by design, pure Go with few direct dependencies:

| Dependency | Purpose |
|---|---|
| `github.com/dop251/goja` | JavaScript VM for dynamic form field evaluation |
| `github.com/andybalholm/brotli` | Brotli stream decompression |
| `golang.org/x/text` | Unicode text processing |

## Scoped Guidelines

- [Compliance engine](pkg/internal/compliance/README.md): PDF/A and PDF/UA validation architecture.
- [Corpus testing](test/integration/README.md): corpus CLI tool usage and flag reference.
