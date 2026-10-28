# Contributing to litho

litho is a pure-Go PDF Conforming Processor. Two rules override everything else:

- **Correctness.** Every output is a valid PDF that viewers can open.
- **Thread safety.** Every public type documents its concurrency contract and honors it under `-race`.

## Toolchain

- Module: `github.com/gotenberg/litho`
- Go: 1.27.0+, no CGO
- Node.js, for Prettier (`package.json`)
- Docker, optional: provisions the integration corpora at the tool versions `test/integration/corpus/versions.env` pins

The Makefile runs golangci-lint and unparam at pinned versions through `go run`, never the copy on your `PATH`.

## Before you start

Plan non-trivial changes before coding: the problem, the approach, the affected files, and the test strategy. Open an issue or a draft PR to discuss it.

One thing per PR. Keep features, bug fixes, and refactoring in separate PRs.

## Project layout

```
cmd/litho/           -> The CLI. A thin front end to pkg/pdf.
cmd/gen-*/           -> Generators for committed tables (rules, CMaps, fonts, Schematron).
pkg/pdf/             -> Public API: Reader, Writer, Pipeline, options, sentinel errors.
pkg/models/          -> Public data types pkg/pdf returns.
pkg/plugins/         -> Public interfaces a caller implements.
pkg/internal/        -> Everything else: parser, filters, fonts, compliance, remediation, renderer.
examples/            -> Runnable programs, one per task.
test/integration/    -> Corpus matrix scoring litho against veraPDF, PDF.js and other oracles.
```

## Setup and Makefile

| Command              | Purpose                                                       | When to use                        |
| -------------------- | ------------------------------------------------------------- | ---------------------------------- |
| `make build`         | Build the `litho` CLI                                         | Manual testing                     |
| `make fmt`           | `go fix`, pinned formatters and `go mod tidy` over the tree   | Before committing                  |
| `make lint`          | Pinned golangci-lint, zero warnings                           | Before committing                  |
| `make lint-unparam`  | Pinned unparam: unused parameters and results                 | Before committing                  |
| `make prettify`      | Format Markdown, YAML and JSON                                | Before committing                  |
| `make lint-prettier` | Lint Markdown, YAML and JSON                                  | Before committing                  |
| `make test-unit`     | `go test -race ./...`                                         | Before committing                  |
| `make bench`         | `go test -bench=. -benchmem ./...`                            | Performance changes                |
| `make godoc`         | Serve the godoc locally                                       | To check documentation             |
| `make corpus-check`  | Fail if a (corpus, feature) cell regressed against a baseline | Before pushing: the repo has no CI |
| `make help`          | List every target, the golden regenerators included           | To find the rest                   |

`make fmt` rewrites the whole tree: stage only the files your change touches.

## Code conventions

### Encapsulation

`pkg/pdf`, `pkg/models` and `pkg/plugins` are the public surface. No type from `pkg/internal/` appears in a public signature, not even through a type alias. Translate at the boundary; use adapters for interfaces.

### Error handling

- Wrap and propagate every error: `fmt.Errorf("resolving font on page %d: %w", pageNum, err)`.
- Match errors with `errors.Is`, never `strings.Contains`.
- Sentinel errors live in `errors.go` per package.
- Never assign an error to `_` without a justification comment. If justified, log it at Debug via `doc.Logger`.
- Every named parameter is used. Remove an unused one and update the callers; prefix it with `_` only when an interface or a function type fixes the signature. Never write `_ = variable` to silence the compiler.
- Every `//nolint` carries a justification on the same line. A `gosec` suppression on an integer conversion states the bound that makes it safe.

### Context

Every public method that does I/O, loops, or recursion takes `context.Context` first. Never store `ctx` in a struct field. Trivial accessors of cached scalars (page count, PDF version) omit it.

Check `ctx.Err()` at natural checkpoints: per ~100 content-stream operators, per page-tree node, per xref entry, per filter buffer. Never in the lexer's per-token loop.

### Thread safety

`Document` is safe for concurrent reads after construction. Mutable caches use `sync.RWMutex`. Validation runs serialize via `Document.LockValidationRun`. `Authenticate` belongs to setup and completes before concurrent use begins.

Nothing that writes a document writes into the `Reader` it reads. `NewWriter`, `Split` and `Merge` work on `Document.Fork`, a copy with its own trailer, caches and logger. A field added to `Document` or `Reader` needs a fork decision (`TestForkDecidesEveryField`, `TestReaderForkDecidesEveryField`).

### Resource streaming

Never load embedded files, images, or binary streams into memory during parsing. The public API returns `Open func(ctx context.Context) (io.ReadCloser, error)`, not `[]byte`, and filters apply lazily inside `Open`.

### Parser safety

- Validate token types with `Expect`. Handle `io.EOF` gracefully. Never panic.
- Track visited nodes in `ResolveObject` to break circular references.
- Prefer typed resolvers (`ResolveDict`, `ResolveArray`, `ResolveName`) over `ResolveObject`.
- `ResolveDict`, `DecodePdfString`, `Float` and friends live in `pkg/internal/core/document`. Don't reimplement them.

### Untrusted input

- Any length, count, or offset read from the PDF is untrusted. Bound it before `make`, a buffer, or a loop, against a natural limit (page count vs `/Size`, `/Length` vs `Document.FileSize()`) or a documented cap. Reject negative and overflowing values: a panicking `make` is a denial of service.
- Every defensive cap is a named package-level const whose comment says it is an anti-DoS guard, not spec-mandated, and why its value is safe for real documents (see `maxPageTreeDepth`). No bare numeric limits, and no conceptual limit defined twice with divergent values.
- A cap that bounds a resource a caller trades against what litho reads joins `pdf.Limits`, with a default equal to the const it replaces, a ceiling, a CLI flag, and a test that lowers it through the public API. Code reads `Document.Limits` at run time, never the const. A cap derived from a budget keeps a documented relation to it (see `pdf.Limits`). A recursion guard that protects litho's own stack never joins it: no `recover` catches a Go stack overflow.

### Panics and goroutines

- Internal code never panics and never calls `recover()`. Every exported `pkg/pdf` entry point that processes document bytes installs a top-level `recover()` that logs the panic at Error and returns a wrapped sentinel.
- Every goroutine exits before its parent call returns. `ctx` reaches every goroutine, and result collection selects on `ctx.Done()`.
- Each worker recovers its own panics into an error wrapping `document.ErrInternalPanic`, since the entry point's `recover()` never sees them.
- Fan-out is bounded by a fixed count or a pool, never one goroutine per untrusted-input element.

### Deterministic output

Identical input bytes and options write identical output bytes. Encryption, whose keys, salts, IVs and `/ID` are random, is the one exception. Never iterate a Go map to emit ordered output.

### Spec-grounded behavior

Code that produces a compliance verdict, accepts or rejects a construct, or hardcodes a spec table cites the governing clause (ISO 32000-1/2, ISO 19005-x, ISO 14289-x, EN 16931, Factur-X) at the decision. Deliberate leniency toward real-world producers names the clause it relaxes and the producer that needs it. Hardcoded tables cite the table they mirror and, where licensing permits, are generated.

### Naming

- No `Get` prefix, exported or not: `Metadata()`, `resolveResources`.
- `Resolve` chases indirect references and builds typed objects: `ResolveCatalog`, `ResolveFont`.
- `Extract` retrieves page-scoped data: `ExtractText`, `ExtractImages`.
- `Set` for setters: `SetMetadata`.
- Plain field accessors that chase nothing take no prefix: `dictInt`.

## Documentation conventions

### Tone

- Short, declarative sentences. Say what it does, then stop.
- Lead with the action. "Validates font embedding", not "This function validates font embedding".
- Active voice. "litho checks the profile", not "The profile is checked by litho".
- No em dashes. Use a period, colon, or comma.
- No "we" hedging. "Don't...", not "We do not recommend...".

### Godoc

Every exported identifier has a doc comment starting with its name, and every package a `doc.go`. Reference identifiers with `[Name]` for pkg.go.dev links. Test, fuzz, benchmark and example functions are exempt.

A doc comment is a contract with the caller. Write down what the caller can rely on, and nothing they cannot act on.

- **The first sentence stands alone.** Package indexes show only it.
- **Document the contract, not the implementation.** Valid inputs, the result, what a zero value or nil means, the sentinel errors returned, what stays unchanged.
- **State ownership and lifetime.** Who closes the `io.ReadCloser`, whether a slice aliases internal state, whether the call mutates its argument.
- **State the concurrency contract on the type**, once, then only where a method deviates.
- **Options state their default and their range**, and what an out-of-range value does.
- **Name the cost of a trade-off**: size, speed or memory against fidelity, which way and roughly how much.
- **Cite the clause at the decision**: `(ISO 32000-1, 7.4.4.4)`. Name viewers only when matching them is the documented behavior.
- **One idea per paragraph.** Use short paragraphs, `# Headings` and `-` lists.
- **Say what is not supported.** `Only sequential JPEG qualifies.` saves the caller an experiment.

Public godoc never mentions the corpus, corpus files, gate cells, measurement counts, commit hashes, or how a behavior was found. `TestPublicGodocNeverMentionsCorpus` enforces it.

### Code comments

- Explain _why_, not _what_. Reference spec clauses: `// Per ISO 32000-2, Table 116...`.
- No numbered steps, no section separators, no comments that restate the code.
- Mark debt with `// TODO: [context]`.

### Corpus evidence

Corpus measurements back a decision; they are not the decision. Collect them in a `# Corpus evidence` section, the last block of the doc comment of the unexported declaration that decides, or of the test that locks the behavior. The prose above it reads complete without it.

```go
// maxPageTreeDepth caps recursion so a self-referential /Kids chain cannot
// exhaust the goroutine stack. Anti-DoS guard, not spec-mandated: ISO
// 32000-1, 7.7.3.2 makes the page tree a strict tree.
//
// # Corpus evidence
//
//   - All corpora: 5170 of 5171 files parse, and the deepest page tree is 4.
```

One bullet per finding, starting with the corpus (`All corpora`, `govdocs`, `pdfjs`, `verapdf`, `pdf20`, `pdfcpu`, `facturx`, `litho`), then the file names when file-specific, then a colon. Evidence behind a comment inside a function, a struct field, or a `const` group moves to the enclosing declaration.

## Testing

Tests live in `_test.go` files beside the code. Assert observable contracts (expected text present, page count matches), not internal state. Exact-value assertions belong in dedicated regression tests.

Any function whose leading argument is attacker-controlled `[]byte` (filter `Decode`, predictors, font and CMap parsers, image decoders) ships a `Fuzz*` target in a `*_fuzz_test.go`, seeded with one valid and one truncated input.

The integration matrix scores litho against oracles over several corpora. When no corpus file reaches a construct, craft a fixture in the litho corpus. See [test/integration/README.md](test/integration/README.md).

## Pull requests

### Commits

[Conventional Commits](https://www.conventionalcommits.org/): `<type>(<scope>): <description>`, with `feat`, `fix`, `refactor`, `test`, `docs` or `chore`.

Stage specific files. Never `git add -A`.

### Checklist

- [ ] No `pkg/internal` type in a public signature.
- [ ] Errors wrapped; silenced errors justified and logged.
- [ ] I/O and recursive functions take and check `context.Context`.
- [ ] Concurrency contract documented and honored under `-race`.
- [ ] No eager materialization of binary resources.
- [ ] File-derived sizes bounded before allocation; caps are named consts; new byte decoders have a fuzz target.
- [ ] Verdicts, accept/reject decisions and hardcoded tables cite their clause.
- [ ] `make fmt && make lint && make lint-unparam && make prettify && make lint-prettier` pass with zero warnings.
- [ ] Godoc on every export, `doc.go` per package, no corpus mention in public godoc.
- [ ] `make test-unit` and `make corpus-check` pass.

## Further reading

- [`test/integration/README.md`](test/integration/README.md): the corpus matrix, tool versions, the regression baseline.
- [`pkg/internal/compliance/README.md`](pkg/internal/compliance/README.md): the compliance engine.
- [`pkg/internal/remediation/README.md`](pkg/internal/remediation/README.md): the remediation fixer contract.
