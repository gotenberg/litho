# Auto-tagging (structure-tree synthesis) in litho

Plan to synthesize a logical structure tree for an untagged PDF.

This is the engine that turns PDF/UA _validation_ into PDF/UA _remediation that can actually pass_. Today litho can fix a structure tree that already exists but is broken; it cannot populate one for a document that has none — and an untagged PDF is the overwhelmingly common PDF/UA failure. Tagging is a content-authoring capability (it invents logical structure from layout), which is why it sits outside the conformance specs even though PDF/UA is its reason to exist.

> **Highest-leverage item in the non-conformance set, and ship-first.** Auto-tagging closes the single most common real-world conformance gap, depends on **none** of the rendering/color/flatten bundle, and reuses an already-built analysis stack. It should be sequenced **ahead** of the rendering bundle. PR1 alone (the emitter, even with placeholder `/Alt`) materially lifts PDF/UA pass rate. Pair it with [REDACTION.md](REDACTION.md) as the cheap, high-value, rendering-independent work.

## What it is

Build the marked-content (`BDC … EMC`) and the structure tree that links it to page content: classify the page's text and graphics into a reading-ordered tree of `Document → H1…Hn / P / L→LI / Table→TR→TH/TD / Figure / Caption …`, assign `/Alt` where required, set a `/RoleMap`, and mark artifacts (headers/footers/page numbers) so they leave the reading order.

## What exists in litho today — more than you'd expect

litho already has most of the _analysis_ and even the tree _scaffold_; what is missing is populating and linking it.

| Capability                                                                                | Status                                                                                                                                        |
| ----------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| Heading **classification** (font-size histogram, section numbering, bold, script, scorer) | Done — `pkg/internal/bookmarks/{generate,histogram,numbering,language,cluster}.go`                                                            |
| Reading order / layout (positioned text elements, chunks)                                 | Done — `ExtractTextElements`, the `graphics` interpreter                                                                                      |
| Table detection (cells, rows) and stroked lines                                           | Done — `ExtractTables`, `ExtractLines`                                                                                                        |
| Figure/image discovery                                                                    | Done — `ExtractImages`                                                                                                                        |
| **`StructTreeRoot` + `/ParentTree` scaffold creation**                                    | **Done** — `remediation/fixer_structtree.go` already builds the root scaffold; `fixer_rolemap.go`, `fixer_contenttag.go` repair existing tags |
| Structure-tree _reader_                                                                   | Done — `pkg/internal/semantics/structure/`, `bookmarks/source_struct.go`                                                                      |
| **Populated `/K` tree (nested roles), `BDC/EMC` injection, per-page `/StructParents`**    | **Missing** — the real gap                                                                                                                    |
| **Reading-order inference across columns/regions**                                        | **Partial** — text-element order exists; multi-column/figure-flow needs work                                                                  |

So the gap is narrower than "build a tagger": the root/ParentTree scaffold and heading _classification_ exist. The new work is the **emitter** (populated `/K` with nested `P/L/LI/Table/...`, `/StructParents`, a `/RoleMap`) plus **injecting `BDC/EMC` marked-content into every content stream** — a pervasive content-stream rewrite. Heading classification supplies the H-levels; the emitter + marked-content injection are the bulk of the effort.

## Design

- **`pkg/internal/tagging` package.** Consume the layout analysis, produce an in-memory logical tree, then **populate** the existing `fixer_structtree.go` scaffold: `/K`, `/ParentTree`, `/RoleMap`, per-page `/StructParents`, and inject `BDC/EMC` into content streams.
- **Heading levels must be a no-skip hierarchy.** The bookmark engine ranks headings by font size (`cluster.go`, capped at 3 levels) — a _size rank_, not the valid `H1…Hn` no-skip hierarchy PDF/UA requires. Normalize size ranks into contiguous `H1…Hn`, raise the cap, and reconcile level gaps (no H1→H3 jumps).
- **Classification pipeline.** Reuse the heading scorer for H-levels; cluster body text into paragraphs by leading/indent; map detected tables to `Table/TR/TH/TD`; mark running headers/footers (the bookmark `SkipHeaderFooter` logic already detects them) as `/Artifact`; tag images `Figure` and request `/Alt`.
- **Reading order.** Order by column/region segmentation, not raw Y — the quality-determining step; extend the text-element chunking.
- **Alt text is the honest hole.** litho can create `Figure` structure and a _placeholder_ `/Alt` but cannot _describe_ an image. Default: structure-complete, `/Alt`-flagged (UA's alt-text rule may remain a residual until a human/AI fills it); optionally route images through the OCR/AI hand-off (`WithOCREngine`) to propose alt text.
- **Drive from remediation.** `RemediatePDFUA` gains an auto-tag fixer (content-modifying tier) that runs when the structure tree is empty; today's repair fixers continue to handle partially-tagged docs.

## Plan

- **PR1 — Logical-tree model + emitter** over the existing `fixer_structtree.go` scaffold: populated `/K` + `/ParentTree` + `/RoleMap` + marked content. Test by tagging a hand-built page and validating with the PDF/UA checker.
- **PR2 — Classification from layout.** Heading (no-skip-normalized)/paragraph/figure detection into the tree builder for single-column docs.
- **PR3 — Tables + reading order.** `ExtractTables`/`ExtractLines` → table structure; column/region reading-order segmentation.
- **PR4 — Remediation wiring.** Auto-tag fixer in `RemediatePDFUA`; measure PDF/UA pass-rate lift on the corpus; optional OCR-proposed `/Alt`.

## Limits and honest scope

- **Auto-tagging is heuristic, not authoritative.** Goal: "structurally valid + far closer to UA," not human-quality semantics. Complex layouts (nested tables, multi-column with floats, forms) degrade gracefully and are flagged.
- **Alt text cannot be invented faithfully** without OCR/AI; auto-tag leaves a human/AI step for `/Alt`.
- **Reading order is the hard part** and the main quality risk; invest review there.

## Out of scope

- Math/structured-content semantics beyond MathML pass-through (`ExtractFormulas` already extracts it).
- Re-flowing/re-laying-out the page; tagging annotates existing layout.
- Guaranteeing PDF/UA pass on arbitrary input — auto-tag raises the floor, it does not certify.
