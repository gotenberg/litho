# Anti-Pattern Analysis

## ~~1. Errors Assigned to `_` — ~540 instances (CRITICAL)~~ FIXED

Fixed in commits `1f36118` (typed helpers migration, eliminated ~474 raw `ResolveObject` calls) and `4782f32` (debug logging for all ~115 remaining discarded resolve errors across 20 files). Every `ResolveX` error is now captured and logged at debug level with caller-specific context. `main.go` bugs (4 silently swallowed user-facing errors) also fixed.

Note: the original ~540 count included the `ResolveObject` + type assertion pattern which was eliminated in #8. The remaining ~115 were the typed helper errors that callers discarded — these now log at debug level as non-blocking best-effort behavior for broken PDFs.

## 2. Bare `return err` Without Wrapping — ~101 instances (HIGH) — PARTIALLY FIXED

Fixed ~60 instances in commit `7e10d56` across the worst offenders: postscript execution (41), annotation extraction (12), crypto authentication (4), xref parsing (9), catalog/page tree (4).

**Remaining (intentionally bare — thin wrappers or low-level I/O):**
- `ResolveDict/Array/Name/...` helpers — transparent pass-through by design
- `ioat/reader.go` — standard library I/O errors
- `parser.go` — lexer already embeds position info
- `filter/jbig2/` — skipped per request

## ~~3. Unsafe Type Assertions (no `ok` check)~~ FIXED

Fixed in commit `f7dc5c5`: all 9 direct type assertions that could panic on malformed input now use ok-checked patterns with proper error returns (`resolve.go`, `xref.go`, `threads.go`). The `val, _ := obj.(Type)` pattern (3 instances in `content_walker.go`) is safe — it yields a zero value on mismatch, never panics.

## ~~4. Context Stored in Struct Field — 1 instance (MEDIUM)~~ FIXED

Fixed in commit `3063dc0`: removed `ctx` from `contentWalker` struct, threaded through method parameters.

## ~~5. `context.Background()` in Library Code — 1 instance (HIGH)~~ FIXED

Fixed in commit `3063dc0`: `checkInfoDictPart4` now uses its `ctx` parameter instead of `context.Background()`.

## ~~6. Eager Materialization of Streams (HIGH)~~ MOSTLY FIXED

- ~~`pkg/internal/core/ast/ast.go:138-141` — `PdfStream.Data []byte` holds entire stream content in memory~~ **FIXED** in commit `36d9989`: `PdfStream` now uses lazy loading via `Bytes()` method. Data is read from the underlying `io.ReaderAt` only on first access and cached via `sync.Once`.
- ~~`pkg/internal/filter/filter.go:293` — `io.ReadAll(r)` in `decodeCCITTFax`~~ **FIXED** in commit `7eca029`: uses pooled buffer pattern.
- `pkg/internal/filter/jbig2/document.go:9,25` — `allData []byte` stores entire JBIG2 stream

## ~~7. Inline `errors.New()` Instead of Sentinels — 5 instances (MEDIUM)~~ FIXED

Fixed in commit `6eef73e`: all 5 inline `errors.New()` calls moved to sentinel variables in `ioat/errors.go` (`ErrUnreadByte`, `ErrSeekInvalidWhence`, `ErrSeekNegativePosition`) and `filter/errors.go` (`ErrLZWNonLiteralAfterClear`, `ErrLZWCodeOutOfRange`).

## ~~8. Duplicated Resolve+Assert Pattern — ~40+ instances (MEDIUM)~~ FIXED

Fixed in commit `1f36118`: migrated ~474 `ResolveObject` + type assertion sites to typed helpers (`ResolveDict`, `ResolveArray`, `ResolveName`, `ResolveStream`, `ResolveString`, `ResolveInt`, `ResolveBool`) across 31 files. Added 3 new helpers. 98 `ResolveObject` calls remain (justified multi-type switches).

## ~~9. Magic Numbers (LOW)~~ MOSTLY FIXED

Fixed in commit `fa045cd`: xref entry types replaced with `XRefTypeFree`/`XRefTypeNormal`/`XRefTypeCompressed` constants, UTF-16BE BOM bytes replaced with `utf16BEBom0`/`utf16BEBom1`. Remaining: bit manipulation constants in `filter/jbig2/` (inherent to the binary format, naming would reduce readability).

## 10. `init()` with Side Effects — 1 instance (LOW, acknowledged)

- `pkg/internal/compliance/rules.go:342` — documented as necessary to break a dependency cycle

## Summary

| # | Anti-Pattern | Count | Severity | Status |
|---|---|---|---|---|
| 1 | Errors assigned to `_` | ~540 | CRITICAL | **FIXED** |
| 2 | Bare `return err` (no wrap) | ~101 | HIGH | **PARTIALLY FIXED** (~60 wrapped) |
| 3 | Unsafe type assertions | 9 | HIGH | **FIXED** |
| 4 | Context in struct field | 1 | MEDIUM | **FIXED** |
| 5 | `context.Background()` in lib | 1 | HIGH | **FIXED** |
| 6 | Eager stream materialization | 1 | HIGH | **MOSTLY FIXED** (2/3 fixed, JBIG2 remaining) |
| 7 | Inline `errors.New()` | 5 | MEDIUM | **FIXED** |
| 8 | Duplicated resolve+assert | ~474 | MEDIUM | **FIXED** |
| 9 | Magic numbers | ~10 | LOW | **MOSTLY FIXED** |
| 10 | `init()` with side effects | 1 | LOW | Acknowledged |
