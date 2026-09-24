# Remediation Engine

Applies targeted fixes to a parsed PDF so it satisfies PDF/A or PDF/UA compliance. Consumes a `[]compliance.Violation` from the compliance engine and produces a report of what was fixed and what remains.

## Architecture

A linear registry of **fixers** in `fixer.go`. Each fixer:

1. **Matches** violations by clause prefix (via `clausePrefix`), rule identity, or description substring.
2. **Repairs** the document by mutating `*writer.ObjAllocator` (queues replacement objects).
3. **Reports** short human-readable descriptions of applied fixes.

The orchestrator (`remediate.go`) classifies every violation against every fixer, runs them in registry order, and collects residuals.

The writer drives remediation to a bounded fixpoint: up to `maxRemediationRounds` passes, each extra round gated by an output-integrity probe, a regression check, and a revalidation that must strictly shrink the violation set. Trailer `/ID` regeneration is the only nondeterminism seam; `Options.PinnedID` (public `pdf.WithDocumentID`) pins it for byte-identical builds.

## Fixer contract

```go
type fixer struct {
    name    string
    safety  safety // structural | resource | content
    matches func(compliance.Violation) bool
    apply   func(ctx, alloc, doc, opts, violations, profile) ([]string, error)
}
```

Rules:

- Check `ctx.Err()` at loop boundaries.
- Log skipped resolution failures at Debug via `doc.Logger`, never swallow silently.
- Copy dicts before mutating (`maps.Copy` then modify, then `AllocAt`).
- Read through the allocator first (`resolveFromAllocOrDoc`, `livePageDict`, `liveArray`, `liveDictValue`, `resolveStreamFromAllocOrDoc`): a fixer that re-reads the source document clobbers fixes queued by earlier fixers. This holds for every container a fixer copies and writes back, parents included: a `/Resources` or `/ColorSpace` dictionary rebuilt around an inline repair is such a container.
- Return stable, grep-friendly fix descriptions.

## Registry ordering

Order is priority. Safety tiers run structural → resource → content. Content-tier fixes are tracked separately so the writer can roll back only that tier when the output regresses.

The `file-structure` fixer never mutates; it signals `Report.NeedsFullRewrite`, and the full rewrite re-serializes every object with clean formatting at the profile's pinned PDF version (`RequiredVersion`: 1.4 for A-1, 1.7 for A-2/3 and UA-1, 2.0 for A-4, UA-2 and WTPDF).

## Residuals

`Report.Residual` holds the violations no fixer matched. The public surface exposes them structured via `RemediationReport.ResidualViolations()`. Honest residuals beat silent ones: when a rule cannot be fixed (an A-4f target with no embedded file to invent), detection and the post-fix validation must still report it on both the litho and veraPDF side.

## Adding a fixer

1. Create `fixer_<name>.go` with `match<Name>` and `fix<Name>` functions.
2. Prefer `clausePrefix(v, "6.7.3")` or whole-`Rule` identity over `descriptionContains`.
3. Register in `fixer.go::registry` in the appropriate tier.
4. Write a unit test in `fixer_<name>_test.go` (see `fixer_structtree_test.go` for the pattern).
5. Run `make corpus CORPUS_FEATURE=remediation,remediation-aggressive` before and after. Corpus-unverified remediation changes regress; the `baseline.json` ratchet (`make corpus-check`) gates `parse_failed`, both `*_regressed` counters, and `disagreement`.

## Testing

Unit tests: `*_test.go` files in this package. Shared helpers in `shared_helpers_test.go`.

Integration: `make corpus CORPUS_FEATURE=remediation`. See [test/integration/README.md](../../../test/integration/README.md) for variables.
