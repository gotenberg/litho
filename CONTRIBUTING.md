# Contributing to litho

litho is a pure-Go PDF Conforming Processor. Two rules override everything else: **correctness** (every output must be a valid PDF that viewers can open) and **thread safety** (every public type documents its concurrency contract and honors it under `-race`).

- Module: `github.com/gotenberg/litho`
- Go: 1.27.0+
- Optional: Docker (corpus provisioning)

## Quick start

```bash
go build ./cmd/litho
make fmt && make lint # zero warnings policy
make test-unit        # go test -race ./...
make bench            # go test -bench=. -benchmem ./...
```

## Coding rules

### Encapsulation

`pkg/pdf` and `pkg/models` are the public surface. No type from `pkg/internal/` may appear in a public signature, not even through a type alias. Translate at the boundary; use adapters for interfaces.

### Error handling

- Never assign errors to `_` without a justification comment. If justified, log at Debug via `doc.Logger`.
- Do not use `_ = variable` to suppress unused warnings. If a variable is unused, remove it. If a function parameter is unused, prefix it with `_` in the signature. If a multi-return value is unused, capture it as `_` directly in the assignment (e.g., `_, b, err := f()`).
- Every named parameter must be used in the function body. Never leave a named-but-unused parameter: Go compiles it, but it misleads readers into thinking the value matters. The default fix is to remove the parameter and update callers. Prefix with `_` only when the signature is constrained by an interface, a function type, or a uniform calling convention (see `pkg/internal/compliance/doc.go`). Run `unparam` to catch these before submitting.
- Wrap and propagate every error: `fmt.Errorf("resolving font on page %d: %w", pageNum, err)`.
- Match errors with `errors.Is`, never `strings.Contains`.
- Sentinel errors live in `errors.go` per package.
- Every `//nolint` directive carries a trailing justification on the same line; bare `//nolint` is rejected. Prefer fixing the finding over suppressing it. A `gosec` suppression on an integer conversion (`uintN(x)`) states the bound that makes the conversion safe.

### Context

Every public method that does I/O, loops, or recursion takes `context.Context` as its first parameter. Never store `ctx` in a struct field.

Check `ctx.Err()` at natural checkpoints (per ~100 content-stream operators, per page-tree node, per xref entry, per filter buffer). Do NOT check in the lexer's per-token loop.

Trivial accessors returning cached scalars (page count, PDF version) omit `ctx`.

### Thread safety

`Document` is safe for concurrent reads after construction. Mutable caches use `sync.RWMutex`. Validation runs serialize via `Document.LockValidationRun`. The `Authenticate` call is part of the setup phase and must complete before concurrent use begins.

Document the concurrency contract on every public type.

### Resource streaming

Never load embedded files, images, or binary streams into memory during parsing. The public API returns `Open func(ctx context.Context) (io.ReadCloser, error)`, not `[]byte`. Filters apply lazily inside the `Open` path.

### Parser safety

Validate token types with `Expect`. Handle `io.EOF` gracefully. Never panic. Track visited nodes in `ResolveObject` to break circular references. Prefer typed resolvers (`ResolveDict`, `ResolveArray`, `ResolveName`) over `ResolveObject`.

### Naming

- No `Get` prefix on accessors: `Metadata()`, not `GetMetadata()`. Go convention. This applies to unexported functions too: `resolveResources`, not `getResources`.
- Use `Resolve` for functions that chase indirect references and build typed objects: `ResolveCatalog`, `ResolvePages`, `ResolveFont`, `ResolveOutline`.
- Use `Extract` for page-scoped data retrieval: `ExtractText`, `ExtractImages`, `ExtractAnnotations`.
- Use `Set` for setters: `SetMetadata`, `SetCachedCMap`.
- For simple dict/struct field accessors that do no ref-chasing, drop the prefix entirely: `dictInt`, not `getInt`.

### No duplication of core logic

`ResolveDict`, `DecodePdfString`, `Float`, and friends live in `pkg/internal/core/document`. Do not reimplement them in other packages.

### Spec-grounded behavior

Any code that produces a compliance verdict, accepts or rejects a construct, or hardcodes a spec table cites the governing clause (ISO 32000-1/2, ISO 19005-x, ISO 14289-x, EN 16931, Factur-X) at the decision site. Deliberate leniency toward real-world producers names the specific clause it relaxes and the empirical reason (which producer, why), so a divergence is auditable rather than supposed. Hardcoded spec tables carry an inline citation to the table they mirror and, where licensing permits, are generated.

## Safety and robustness

### Untrusted input sizes

Any length, count, or offset read from the PDF is untrusted. Before `make([]T, n)`, allocating a buffer, or looping `n` times, validate `n` against a documented bound or against `Document.FileSize()`. Reject negative and overflowing values explicitly: a panicking `make` is itself a denial of service. Prefer a natural bound (page count vs trailer `/Size`, stream `/Length` vs file size) over an arbitrary constant.

### Panic boundary

Internal code never panics (see [Parser safety](#parser-safety)) and never calls `recover()`, except in a goroutine worker (see [Goroutine lifecycle](#goroutine-lifecycle)). As defense-in-depth against crafted input, every exported `pkg/pdf` entry point that processes document bytes installs a top-level `recover()` that converts an unexpected panic into a wrapped sentinel logged at Error via `doc.Logger` with the recovered value and a stack. The boundary is a safety net, not a license to panic.

### Resource limits

Every defensive cap (recursion depth, buffer size, decompression output, iteration count) is a named const at package scope with a comment stating it is an anti-DoS guard, not spec-mandated, and why the chosen value is safe for real documents. See `maxPageTreeDepth` in `catalog.go` and `maxNameTreeDepth` in `nametree.go`. No bare numeric literals as limits. The same conceptual limit must not be defined with divergent values across packages without a documented cross-reference.

Five caps are resource budgets a caller configures through `pdf.Limits` (`pdf.WithLimits`, and the `--max-*` CLI flags): the decoded bytes of one stream, the pixels of one image, the embedded PDF compliance opens, the nesting of form XObjects, and the object cache. Their defaults are the named consts of `pkg/internal/limits`, and code reads the document's value at run time through `Document.Limits`, never the const, so a lowered budget reaches every walk. A codec cap that bounds pixels or samples scales with the image budget (`Limits.ScaleImageCap`). Four caps derive from the stream budget: the decoded content of one page across its `/Contents` array is a quarter of it (`Limits.MaxPageContentBytes`), the object-stream cache is equal to it (`Limits.MaxObjStmCacheBytes`), and the font-program cache and the colour-transform cache are a quarter of it each (`Limits.MaxFontProgCacheBytes`, `Limits.MaxColorCacheBytes`). A cap that bounds what a budget bounds, a sum of it or a cache of it, derives from that budget in a documented relation that keeps its default, rather than staying fixed when a caller moves the budget.

A new cap stays an internal const, unless it bounds a resource a caller has reason to trade against what litho reads: then it joins `Limits`, with a default equal to the const it replaces, a ceiling, a CLI flag, and a test that lowers it through the public API. A recursion guard that protects litho's own stack never joins it, since a Go stack overflow is fatal and no recover catches it.

### Goroutine lifecycle

Every goroutine has a defined exit condition and cannot outlive its parent call. `ctx` propagates into every goroutine, and result collection selects on `ctx.Done()` so cancellation short-circuits. Each worker recovers panics into an error result wrapping `document.ErrInternalPanic` (re-exported as `pdf.ErrInternalPanic`) and logs them, so one bad worker cannot crash the host: the entry point's `recover()` runs on the calling goroutine and never sees a worker's panic. Fan-out is bounded by a fixed count or a worker pool, never one goroutine per untrusted-input element.

### Deterministic output

Given identical input bytes and identical options, the writer produces identical output bytes, modulo the documented `/ID` and date seams. Never iterate a Go map to emit ordered output: sort keys or object numbers first. Confine unavoidable nondeterministic inputs (`/ID` generation, current-date metadata) so reproducible-build callers can pin them.

## Documentation rules

### Tone

- Short, declarative sentences. Say what it does, then stop.
- Lead with the action. "Validates font embedding", not "This function validates font embedding".
- Active voice. "litho checks the profile", not "The profile is checked by litho".
- No em dashes. Use a period, colon, or comma.
- No "we" hedging. "Don't...", not "We do not recommend...".

### Godoc

Every exported type and function has a Godoc comment starting with its identifier name:

```go
// Violation records a single rule violation with context.
type Violation struct { ... }
```

Test, fuzz, benchmark and example functions are exempt, since go doc never renders them. Give one a doc comment when it carries corpus evidence or a reason the code does not show, and start it with the function name.

Every package has a `doc.go` with a `// Package foo ...` comment.

Reference identifiers with `[Name]` brackets for pkg.go.dev linking:

```go
// ValidatePDFA returns violations as []error where each element
// is a [Violation] value. See [Rule] for the structured fields.
```

### Code comments

- Explain _why_, not _what_.
- No numbered step comments. No noise comments that restate the code.
- No section-separator comments (`// --- Foo ---`). Use whitespace and function grouping instead.
- Reference spec clauses: `// Per ISO 32000-2, Table 116...`.
- Mark debt with `// TODO: [context]`.

### Corpus evidence

Corpus measurements back a decision. They are not the decision. Keep them out of the prose and collect them in a `# Corpus evidence` section, the last block of the declaration's doc comment:

```go
// maxPageTreeDepth caps recursion so a self-referential /Kids chain cannot
// exhaust the goroutine stack. Anti-DoS guard, not spec-mandated: ISO
// 32000-1, 7.7.3.2 makes the page tree a strict tree.
//
// # Corpus evidence
//
//   - All corpora: 5170 of 5171 files parse, and the deepest page tree is 4.
//   - All corpora: no file nests deeper than 100, so the gap with the
//     compliance and remediation caps is never reached.
```

- One bullet per finding. Each bullet starts with the corpus it was measured on (`All corpora`, `govdocs`, `pdfjs`, `verapdf`, `pdf20`, `pdfcpu`, `facturx`, `litho`), then the file names when the finding is file-specific, then a colon.
- A corpus mention is the word corpus, a corpus name, a corpus file, or a count measured over corpus files. PDF.js, veraPDF, and pdfcpu as tools are not corpus mentions.
- Evidence behind a comment inside a function body, a struct field, or one entry of a `const` or `var` group moves to the section of the enclosing declaration. The comment keeps the reason.
- The prose above the section reads complete without it.

### Formatting non-Go files

Run `make prettify && make lint-prettier` for YAML, Markdown, and JSON.

## Testing

Tests live in `_test.go` files alongside the code.

Behavior-focused tests assert observable contracts (non-nil output, expected text present, page count matches) rather than internal state. Exact-value assertions that would lock the implementation belong in dedicated regression tests, not in smoke tests.

### Fuzzing

Fuzz tests exist for the lexer and parser. Any function whose leading argument is attacker-controlled `[]byte` (stream filter `Decode`, predictors, font/CMap table parsers, image decoders) ships a `Fuzz*` target in a `*_fuzz_test.go` beside it, seeded with one valid and one truncated input, asserting it never panics.

See:

- [test/integration/README.md](test/integration/README.md): corpus CLI tool usage, veraPDF and PDF.js corpora.
- [pkg/internal/compliance/README.md](pkg/internal/compliance/README.md): compliance engine architecture.
- [pkg/internal/remediation/README.md](pkg/internal/remediation/README.md): remediation fixer contract.

## Pull requests

Plan non-trivial changes before coding. Present problem, approach, affected files, and test strategy.

### Checklist

- [ ] Encapsulation: no `pkg/internal` types in public signatures.
- [ ] Error handling: all errors wrapped; silenced errors justified and logged.
- [ ] Context: I/O and recursive functions accept and check `context.Context`.
- [ ] Thread safety: concurrency contract documented and honored under `-race`.
- [ ] Resource streaming: no eager materialization of binary resources.
- [ ] Safety: file-derived sizes bounded before allocation; defensive caps are named consts; new byte decoders have a fuzz target.
- [ ] Spec-grounded: verdicts, accept/reject decisions, and hardcoded tables cite their clause; deliberate leniency names the clause it relaxes.
- [ ] Linting: `make fmt && make lint` passes with zero warnings.
- [ ] Documentation: Godoc on all exports; `doc.go` per package.
- [ ] Tests: new behavior covered; no regressions; corpora unchanged.

### Commits

[Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`.

Stage specific files. Never `git add -A`.

## Dependencies

| Dependency                      | Purpose                                               |
| ------------------------------- | ----------------------------------------------------- |
| `github.com/dop251/goja`        | JavaScript VM for dynamic form fields                 |
| `github.com/andybalholm/brotli` | Brotli stream decompression                           |
| `golang.org/x/text`             | Unicode text processing                               |
| `github.com/antchfx/xpath`      | XPath 1.0 evaluator backing the Schematron engine     |
| `github.com/antchfx/xmlquery`   | XML DOM + XPath navigator paired with `antchfx/xpath` |

### Filter plugins

litho decodes every standard stream filter in pure Go, JBIG2Decode and JPXDecode included, and ships no plugin. `pkg/plugins` defines `FilterDecoder`, which a caller implements to decode a filter litho does not know or to replace a built-in decoder; `pdf.WithFilterPlugin` registers one, and it takes precedence over the built-in decoder for its filter name.
