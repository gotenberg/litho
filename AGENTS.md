# AGENTS.md

## Project Identity

**litho** is an enterprise-grade, non-visual PDF Conforming Reader written in pure Go. It extracts text, metadata, forms, signatures, images, and structural data from PDF files across all versions (1.0 through 2.0). Visual rasterization is out of scope.

Module: `github.com/gotenberg/litho`
Go version: 1.26.0

## Mandatory Workflow

Every non-trivial task **must** follow these five steps in order. No step may be skipped.

### Step 1 — Plan

Before writing any code, present:

- **Problem statement:** What is broken or missing, and why it matters.
- **Proposed solution:** The approach you will take.
- **Alternatives considered:** When pertinent, explain what else was considered and why it was rejected.
- **Scope:** Which files will be created or modified.
- **Testing strategy:** What tests will be added or updated to verify the change.

**Stop and wait for user approval before proceeding to Step 2.**

### Step 2 — Implement

- Code the approved plan.
- Verify the build passes (`go build ./...`).
- Do not deviate from the approved plan without flagging the deviation and getting approval.

### Step 3 — Test

- Write or update tests as described in the plan's testing strategy.
- Run `make test-unit` (and `make test-integration` when relevant).
- All new and existing tests must pass.

### Step 4 — Review

Self-review against this checklist and present findings to the user:

- [ ] **Backward compatibility:** No breaking changes to the public API (`pkg/pdf`, `pkg/models`) unless explicitly approved.
- [ ] **Encapsulation:** No `pkg/internal` types leak into public signatures.
- [ ] **Error handling:** All errors wrapped and propagated; no silenced errors without justification.
- [ ] **Context:** I/O-bound or recursive functions accept and check `context.Context`.
- [ ] **Resource streaming:** No eager materialization of binary resources.
- [ ] **Linting:** `make fmt && make lint` passes with zero warnings.
- [ ] **Code quality:** No dead code, no commented-out code, no TODO without context.
- [ ] **Documentation:** Public types and functions have Godoc comments.
- [ ] **Tests:** New behavior is covered; no regressions.
- [ ] **Scope:** Changes are limited to what the plan specified.

### Step 5 — Commit

**Stop and wait for explicit user approval before committing.**

- Use [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`).
- Stage specific files only — never `git add -A` or `git add .`.
- Do not push unless the user explicitly asks.

## Build & Quality Commands

```bash
make fmt               # gofmt + gofumpt + gci + goimports + go mod tidy
make lint              # golangci-lint (strict, see .golangci.yml)
make lint-todo         # Find TODO comments via godox
make test-unit         # Run unit tests only (go test -race ./...)
make test-integration  # Run integration/corpus tests (go test -timeout 5m -tags=integration -v github.com/gotenberg/litho/test/integration)
go build ./cmd/litho   # Build the binary
```

Always run `make fmt && make lint` before submitting code.

## Coding Rules

### Error Handling
- Never assign errors to `_` unless explicitly justified (e.g., safe hash writes).
- Always wrap and propagate errors up the call stack.
- Use `errors.Is()` with sentinel variables — never `strings.Contains()`.
- Sentinel errors live in `errors.go` per package.

### Context and Cancellation (Critical)
- **Every public method that performs I/O, loops, or recursion must accept `context.Context` as its first parameter.** This is the only mechanism for callers to impose deadlines and cancel runaway parsing. litho is designed for server use (Gotenberg) where HTTP request deadlines govern processing time.
- **Never store `ctx` in a struct field.** Pass it through function parameters, per Go convention.
- **Check `ctx.Err()` at natural checkpoints in internal loops:**
    - Content stream operator dispatch (every ~100 operators).
    - Page tree traversal (before processing each node).
    - Object resolution (`resolveObject` — on each step, alongside cycle detection).
    - CMap parsing (per entry or per block).
    - Font resolution (at the top of `resolveFont` and inside encoding table iteration).
    - XRef table parsing and rebuild (per entry).
    - Filter pipeline (per read buffer during decompression).
    - JavaScript VM execution (use goja's interrupt mechanism with `ctx`).
- **Do NOT add context checks in the lexer's per-token loop.** The lexer is the hottest path; checking one level up (per content stream operator) is sufficient.
- **Wrap context errors with location info** so the caller knows where cancellation occurred: `fmt.Errorf("extracting text from page %d: %w", pageNum, err)`.
- **Never return partial results on context cancellation.** Return an error. The caller cannot trust incomplete output. If a future use case requires partial results, that would be a separate streaming API.
- **Do not introduce litho-specific timeout sentinels.** The standard `context.DeadlineExceeded` and `context.Canceled` are sufficient; callers check with `errors.Is()`.
- **Trivial accessors that return cached scalar values** (e.g., page count, PDF version) do not need `ctx`.

### Encapsulation (Critical)
- Public packages (`pkg/pdf`, `pkg/models`) must **never** accept or return types from `pkg/internal/`.
- The public API accepts `io.ReaderAt` + `size int64`. Internal document parsing happens behind the facade.
- Map all internal types to `pkg/models` structs before returning to callers.

### Parser Safety
- Never blindly advance the lexer. Validate token types using explicit `Expect` logic.
- Handle `io.EOF` gracefully — never panic.

### Centralized Core Utilities (Anti-Duplication)
- NEVER duplicate core PDF operational logic (e.g., `resolveObject`, `decodePdfString`, `getFloat`) across internal domains.
- PDF strings are often encrypted; decryption must be handled consistently through the crypto pipeline.
- `resolveObject` must track visited nodes to prevent infinite loops from malicious circular references. Context cancellation adds a wall-clock bound on top of this structural bound.
- Rely on centralized, thread-safe methods provided by the `internal/core/document` package to ensure security and prevent stack overflows.

### Resource Streaming (Critical — No Eager Materialization)
- **Never load embedded files, images, or other binary resources into memory during parsing.** The reader must collect metadata (byte offset, length, filters, MIME type) without decoding or buffering the resource content.
- The public API must return a **lazy accessor** for every resource — a value that lets the caller read the content on demand. In `pkg/models`, expose a function field (e.g., `Open func(ctx context.Context) (io.ReadCloser, error)`) rather than a `[]byte` field. The underlying implementation reads from the original `io.ReaderAt` at the recorded offset only when `Open` is called.
- **Rationale:** A single PDF may contain hundreds of megabytes of embedded files and images. Eagerly decoding them would make the reader unusable on large documents and would force callers to pay the I/O and memory cost for resources they may never need.
- Stream filters (Flate, ASCII85, DCT, etc.) must be applied lazily inside the `Open` implementation, chaining the decoded stream through the filter pipeline on read — not at parse time.
- Callers are responsible for closing the reader returned by `Open`. Document this clearly in Godoc.
- Within `pkg/internal`, the same rule applies: internal helpers that enumerate resources (attachments, image XObjects, portfolio files, 3D streams) must store stream coordinates, not decoded bytes.
- **Test guidance:** Unit tests that verify resource content should call `Open`, read, and close. Tests that only verify metadata (filename, MIME type, dimensions) must **not** call `Open` — this ensures the lazy contract is maintained and tests stay fast.

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

#### Unit tests
- Tests use `_test.go` convention in the same package.
- Live under `pkg/` and `cmd/` alongside the code they test.
- Run with `make test-unit`.
- The internal compliance package uses a `mockDocBuilder` pattern to construct test PDF documents.
- Fuzz tests exist for parser and lexer (`_fuzz_test.go`).

#### Integration tests
- Live in `test/integration/`.
- Run with `make test-integration` (requires corpus setup first).
- The PDF.js corpus tests validate the public API against hundreds of real-world PDFs from Mozilla's test suite. See `PDFJS_CORPUS_TESTING.md` for setup and details.
- Integration test data lives in `test/integration/testdata/` and is **not checked into Git**.

## Dependencies

Minimal by design — pure Go with few direct dependencies:
- `github.com/dop251/goja` — JavaScript VM for dynamic form field evaluation.
- `github.com/andybalholm/brotli` — Brotli stream decompression.
- `golang.org/x/text` — Unicode text processing.

## What Is NOT Implemented

- **JBIG2Decode / JPXDecode (JPEG2000):** Scanned image filters are not yet supported.
- **DeviceN / Separation color spaces:** Complex print industry spot colors (e.g., PANTONE) are not yet supported.