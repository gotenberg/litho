# Remediation Engine

Applies targeted fixes to a parsed PDF so it satisfies PDF/A or PDF/UA compliance. Consumes a `[]compliance.Violation` from the compliance engine and produces a report of what was fixed and what remains.

## Architecture

A linear registry of **fixers** in `fixer.go`. Each fixer:

1. **Matches** violations by clause prefix (via `clausePrefix`) or description substring.
2. **Repairs** the document by mutating `*writer.ObjAllocator` (queues replacement objects).
3. **Reports** short human-readable descriptions of applied fixes.

The orchestrator (`remediate.go`) classifies every violation against every fixer, runs them in registry order, and collects residuals.

## Fixer contract

```go
type fixer struct {
    name    string
    matches func(compliance.Violation) bool
    apply   func(ctx, alloc, doc, opts, violations, profile) ([]string, error)
}
```

Rules:

- Check `ctx.Err()` at loop boundaries.
- Log skipped resolution failures at Debug via `doc.Logger`, never swallow silently.
- Copy dicts before mutating (`maps.Copy` then modify, then `AllocAt`).
- Return stable, grep-friendly fix descriptions.

## Registry ordering

Order is priority. Content-preserving fixers (metadata, structure, single-dict edits) run before content-modifying fixers (LZW re-encoding, operator stripping). The split is gated by `Options.PreserveContent`.

The `file-structure` fixer is a sentinel: it never mutates, only signals `Report.NeedsFullRewrite`. It must run last among Tier 1 fixers.

## Adding a fixer

1. Create `fixer_<name>.go` with `match<Name>` and `fix<Name>` functions.
2. Prefer `clausePrefix(v, "6.7.3")` over `descriptionContains`.
3. Register in `fixer.go::registry` in the appropriate tier.
4. Write a unit test in `fixer_<name>_test.go` (see `fixer_structtree_test.go` for the pattern).
5. Run `go run ./test/integration/cmd/remediate` before and after.

## Testing

Unit tests: `*_test.go` files in this package. Shared helpers in `shared_helpers_test.go`.

Integration: `go run ./test/integration/cmd/remediate`. See [test/integration/README.md](../../../test/integration/README.md) for flags.
