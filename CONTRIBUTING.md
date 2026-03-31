# Contributing to litho

**litho** is a pure-Go, non-visual PDF Conforming Reader. It extracts text, metadata, forms, signatures, images, and structural data from PDF files across all versions (1.0 through 2.0). It also validates PDF/A (ISO 19005) and PDF/UA (ISO 14289) compliance. Visual rasterization is out of scope.

- Module: `github.com/gotenberg/litho`
- Go version: 1.26.0

## Getting Started

### Prerequisites

- Go 1.26.0+
- Docker (for integration tests)
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
make test-integration                      # All integration/corpus tests (requires Docker)
make test-integration TAGS=pdf.js          # PDF.js corpus only
make test-integration TAGS=verapdf         # VeraPDF corpus only
make test-integration TAGS=pdf.js,verapdf  # Explicit list (same as default)
make clean-corpus                          # Remove all corpus data (forces regeneration)
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
cmd/litho/              CLI binary
pkg/pdf/                Public API facade (io.ReaderAt + size)
pkg/models/             Public model types returned to callers
pkg/plugins/            Filter plugins (jpx, jbig2)
pkg/internal/
  core/ast/             PDF object AST types
  core/document/        Document resolution, object access
  core/parser/          PDF lexer/parser
  compliance/           PDF/A and PDF/UA compliance engine
  graphics/             Content stream parsing
  typography/           Font program parsing (TrueType, CFF, Type1)
  text/                 Text extraction
test/integration/       Corpus-based integration tests (Docker)
```

## Formatting and Imports

Run `make fmt` to enforce formatting (gofmt, gofumpt, goimports, gci). Import order enforced by gci: standard library, third-party, then `github.com/gotenberg/litho/...`.

## Testing

### Unit Tests

Tests use `_test.go` convention in the same package. Live under `pkg/` and `cmd/` alongside the code they test. Run with `make test-unit`. The compliance package uses a `mockDocBuilder` pattern to construct test PDF documents. Fuzz tests exist for parser and lexer (`_fuzz_test.go`).

### Integration Tests

Live in `test/integration/`. Run with `make test-integration` (requires Docker for corpus provisioning via testcontainers). Corpus data is auto-generated on first run and cached in `test/integration/testdata/` (not checked into Git).

Use `TAGS=` to select specific corpus: `make test-integration TAGS=pdf.js` or `TAGS=verapdf`. Set `LITHO_REGEN_CORPUS=1` to force corpus regeneration.

- **PDF.js corpus:** validates text extraction, metadata, and coordinates against Mozilla's PDF.js test suite (~1000 PDFs).
- **VeraPDF corpus:** validates PDF/A and PDF/UA compliance against the VeraPDF test corpus, comparing litho's pass/fail verdicts against VeraPDF's reference results.

## Dependencies

Minimal by design, pure Go with few direct dependencies:

| Dependency | Purpose |
|---|---|
| `github.com/dop251/goja` | JavaScript VM for dynamic form field evaluation |
| `github.com/andybalholm/brotli` | Brotli stream decompression |
| `golang.org/x/text` | Unicode text processing |

## Scoped Guidelines

- [Compliance engine](pkg/internal/compliance/README.md): PDF/A and PDF/UA validation architecture.
