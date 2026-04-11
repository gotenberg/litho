# Package `remediation`

Targeted fixes that take a PDF document plus a slice of
`compliance.Violation` values and produce a document that satisfies as
many of those violations as possible without altering visible content
(unless aggressive mode is enabled).

## Overview

The engine is a small dispatcher on top of a linear registry of fixers.
Each fixer has three responsibilities:

1. **Identify** the subset of violations it handles via a `matches` function
   (usually clause-prefix based).
2. **Repair** the document by mutating the `*writer.ObjAllocator`, which
   queues replacement objects for the writer to emit.
3. **Report** a slice of short human-readable descriptions of the fixes
   it applied. Nothing else.

The orchestrator (`remediate.go::remediate`) classifies every violation
against every fixer, then applies the matching fixers in registry order.
A violation is considered "handled" once any fixer has claimed it; any
violation left unclaimed after the pipeline runs appears in the final
report's `Residual` list.

## Fixer contract

Every registered fixer is a value of type `fixer`:

```go
type fixer struct {
    name    string
    matches func(compliance.Violation) bool
    apply   func(
        ctx context.Context,
        alloc *writer.ObjAllocator,
        doc *document.Document,
        opts Options,
        violations []compliance.Violation,
        profile string,
    ) ([]string, error)
}
```

**`name`** — a unique identifier used in log fields, error messages, and
`remediate.go`'s magic check that recognizes the `file-structure` fixer
as the one that forces a full rewrite.

**`matches`** — side-effect-free, fast, called once per violation per
fixer. Prefer matching on clause prefixes via the `clausePrefix` helper
in `fixer.go`; fall back to `descriptionContains` only when the clause
alone is ambiguous.

**`apply`** — the actual repair. Contracts:
- Context cancellation must be checked at loop boundaries.
- Resolution failures on optional paths should be logged and skipped via
  `doc.Logger.Debug`, never swallowed silently.
- Dict mutations must happen on a fresh copy (`maps.Copy` then modify) so
  that previously-allocated objects remain correct if the fixer errors
  mid-flight.
- The returned descriptions appear verbatim in the final
  `RemediationReport.Fixed` slice. Keep them short, stable, and
  grep-friendly.

## Registry ordering

`fixer.go::registry` is a slice; order is priority. A fixer earlier in
the slice runs before a fixer later in the slice. This matters in two
narrow but critical ways:

1. **Content-preserving before content-modifying.** Tier 1 (metadata,
   structure, single-dict edits) runs before Tier 2 (content-stream
   rewriting, LZW re-encoding, aggressive fixes). The split is visible
   as the blank line separator in the registry and is gated by
   `Options.PreserveContent`.

2. **`file-structure` must be last among Tier 1.** The `file-structure`
   fixer is a sentinel — it never mutates the allocator, only signals
   "the document needs a full rewrite". `remediate.go` detects it by
   name and sets `Report.NeedsFullRewrite = true`. Moving it earlier
   would cause downstream fixers to run against a document that has
   already been flagged for rewrite but not yet rewritten.

If you add a new fixer, think about where it belongs in the two tiers
and whether any later fixer reads state your fixer just modified. The
`clausePrefix` matching function means you can generally keep fixers
independent — they claim disjoint slices of violations — but catalog-
level keys like `/Metadata` and `/OutputIntents` can be touched by
multiple fixers, and later fixers must survive the mutations of earlier
ones.

## Adding a new fixer

1. Create `fixer_<name>.go` in this package.
2. Write a `match<Name>(compliance.Violation) bool` function. Prefer
   `clausePrefix(v, "6.7.3")` over `descriptionContains(v, "pdfaid")`.
3. Write `fix<Name>(ctx, alloc, doc, opts, violations, profile)` that
   mutates `alloc` and returns fix descriptions. Follow the "make a
   fresh copy of the dict, mutate the copy, `AllocAt` the result"
   pattern from existing fixers.
4. Register the fixer in `fixer.go::registry` in the appropriate tier.
5. Write a unit test in `fixer_<name>_test.go`:
   - Build a minimal input PDF (see `fixer_structtree_test.go` for the
     established pattern).
   - Open it with `document.NewDocument`.
   - Allocate a `*writer.ObjAllocator` sized to the input's object
     count.
   - Call the fixer directly.
   - Inspect `alloc.Pending()` to verify the expected object
     mutations.
6. Run `go run ./test/integration/cmd/remediate` before and after to
   confirm the corpus aggregate moved in the expected direction.

## Related packages

- [`compliance`](../compliance/) — produces the violations this package
  consumes. Profile parsing helpers (`PDFAPart`, `PDFAConformance`,
  `IsPDFUA`, etc.) live in `compliance/profile.go` and are the single
  source of truth; remediation fixers must use those rather than
  rolling their own parsers.
- [`core/writer`](../core/writer/) — the `ObjAllocator` and `Copier`
  abstractions every fixer mutates through.
- [`core/document`](../core/document/) — the read-side model fixers
  consult when they need to resolve existing objects.

## Testing

**Unit.** Per-fixer tests under `pkg/internal/remediation/*_test.go`.
See `fixer_structtree_test.go` for the pattern.

**Integration.** `test/integration/cmd/remediate` runs the full
remediation engine against the VeraPDF corpus (1,304 failing PDFs) and
reports aggregate before/after violation counts. Run before committing
any fixer change:

```bash
go run ./test/integration/cmd/remediate
```
