# PDF/VT support in litho

Plan to add ISO 16612 (PDF/VT) validation and remediation across every conformance level, plus a test corpus.

PDF/VT is the variable- and transactional-printing conformance family: the format behind high-volume personalized print (statements, bills, direct mail) where one logical document holds thousands of per-recipient records and the RIP must cache reused graphics to keep up. **PDF/VT is layered on PDF/X**, so it is the last of the three families to build: a PDF/VT file is a conforming PDF/X file plus variable-data structure. Implement [PDF-X.md](PDF-X.md) first — PDF/VT validation calls straight into the PDF/X validator and adds a thin VT-specific layer on top. Shared corpus strategy: [PDF-X-E-VT-CORPUS.md](PDF-X-E-VT-CORPUS.md).

## What PDF/VT is

PDF/VT takes a conforming PDF/X file and adds three things:

1. **A document-part hierarchy** (`/DPart` tree, rooted at the catalog `/DPartRoot`) that partitions the pages into records — typically one record per recipient.
2. **Document Part Metadata** (`/DPM` dictionaries on DPart nodes) carrying per-record data the RIP and finishing equipment consume (recipient, mailpiece, postage sort).
3. **Encapsulated groups of graphic objects** — graphics carried via form XObjects / transparency groups marked `/GTS_Encapsulated true`, self-contained and side-effect-free so the RIP can render them once and cache them, then reuse across pages and records. This caching is the entire performance reason PDF/VT exists. (Confirm in ISO 16612 exactly which object dictionaries may carry the flag so the self-containment check covers the right set.)

Plus the conformance identifier and the PDF/X-inherited print constraints.

## Versions

| Level     | ISO part     | Built on (PDF/X)                  | Base PDF | Distribution                                                            |
| --------- | ------------ | --------------------------------- | -------- | ----------------------------------------------------------------------- |
| PDF/VT-1  | 16612-2:2010 | PDF/X-4                           | 1.6      | single self-contained file                                              |
| PDF/VT-2  | 16612-2:2010 | PDF/X-4p **or** X-5g **or** X-5pg | 1.6      | multi-file: external output intent and/or page contents                 |
| PDF/VT-2s | 16612-2:2010 | same as VT-2                      | 1.6      | streaming: a multipart MIME package of VT-1 files and/or VT-2 file sets |
| PDF/VT-3  | 16612-3:2020 | PDF/X-6                           | 2.0      | PDF 2.0 generation; single/multi per the X-6 base                       |

Three corrections from spec review, to confirm against the ISO texts:

1. **PDF/VT-3 exists.** ISO 16612-3:2020 ("Using PDF/X-6 (PDF/VT-3)") is a published, distinct part built on PDF 2.0 / PDF/X-6 — not an open question and not a revision of 16612-2. Its rules cite 16612-3, and it depends on litho's PDF/X-6 support.
2. **VT-2 is not "PDF/X-5" generically.** ISO 16612-2's title is "Using PDF/X-4 and PDF/X-5"; VT-2/VT-2s are based on one of PDF/X-4p, PDF/X-5g, or PDF/X-5pg. The validator must accept any of those underlying levels for VT-2, not dispatch unconditionally to a single X-5 validator.
3. **VT-2s is a MIME package**, not "a stream of VT-2 documents": a multipart container holding one or more PDF/VT-1 files and/or PDF/VT-2 file sets (with their referenced files) for interleaved generation and rendering.

> The DPM schema, the `/GTS_Encapsulated` object set, scalability/environment-colorant requirements, and the exact identifier property names must be transcribed from ISO 16612-2 / 16612-3. No veraPDF profile exists; rules are hand-authored and ISO-cited.

**Conformance set:** `VT-1`, `VT-2`, `VT-2s` (16612-2:2010, PDF 1.6); `VT-3` (16612-3:2020, PDF 2.0) gated on PDF/X-6 support landing first.

### Identifier

Conformance is declared primarily in **XMP** (the `pdfvtid:` namespace; transcribe the exact property names from the standard). Treat any `GTS_PDFVTVersion` Info-dictionary key as the legacy mechanism, not the headline identifier — mirroring the PDF/X migration where the Info `GTS_PDFXVersion` key gave way to `pdfxid:` XMP. Confirm whether an Info-dict key is required or merely permitted.

## Requirement families mapped to litho's check groups

| Requirement                                                                       | litho check group          | Reuse vs new            |
| --------------------------------------------------------------------------------- | -------------------------- | ----------------------- |
| Underlying file is valid PDF/X: X-4 (VT-1), X-4p/X-5g/X-5pg (VT-2/2s), X-6 (VT-3) | **PDF/X validator**        | reuse (hard dependency) |
| Conformance identifier present (XMP `pdfvtid:`)                                   | `xmp*.go` / new `pdfvt.go` | new                     |
| Catalog `/DPartRoot` → `/DPart` tree well-formed, covers all pages                | new `pdfvt.go`             | new                     |
| `/DPM` metadata dictionaries valid where present                                  | new `pdfvt.go`             | new                     |
| Encapsulated groups (`/GTS_Encapsulated`) self-contained, side-effect-free        | `xobjects.go` + `pdfvt.go` | new                     |
| Referenced XObjects + external files resolvable (VT-2/2s, via X-4p/X-5)           | `xobjects.go`              | reuse from PDF/X work   |
| Scalability / environment colorant constraints                                    | new `pdfvt.go`             | new                     |

The dependency graph is the whole story: **everything above the line in the table is the PDF/X validator**; PDF/VT adds only the `pdfvt.go` group (DPart tree, DPM, encapsulation, identifier). If PDF/X is solid, PDF/VT is a small, well-bounded addition.

## What exists in litho today

| Capability                                     | Status                                                |
| ---------------------------------------------- | ----------------------------------------------------- |
| Document-part (`/DPart`) hierarchy parsing     | Missing                                               |
| Encapsulated-group detection                   | Missing                                               |
| `pdfvtid:` identifier                          | Missing                                               |
| Form-XObject resolution + inspection           | Done (`xobjects.go`) — basis for encapsulation checks |
| Everything PDF/X-4 / X-4p / X-5 / X-6 requires | Pending [PDF-X.md](PDF-X.md) (hard prerequisite)      |

## Plan

Same PR shape, but PR2 leans entirely on the PDF/X validator existing.

### PR1 — Models + API

- `pkg/models/compliance.go` — `type PDFVTProfile string` with `PDFVT1`, `PDFVT2`, `PDFVT2s` (and `PDFVT3` gated on X-6), `IsValid()`, `String()`.
- `pkg/pdf/reader.go` — `ValidatePDFVT(ctx, profile)` returning `[]error` + `ValidatePDFVTReport(ctx, profile)` returning `[]Violation`.
- `pkg/pdf/writer.go` — `RemediatePDFVT(ctx, profile, opts...)` (no-op until PR3).
- `compliance.go` dispatcher + `cmd/litho` wiring.

### PR2 — Validator

- `ValidatePDFVT` first runs the PDF/X validator for the underlying level — X-4 for VT-1; **whichever of X-4p/X-5g/X-5pg the file declares** for VT-2/VT-2s; X-6 for VT-3 — and inherits every violation.
- `profiles/PDF_VT/PDFVT-1.xml … PDFVT-3.xml` — hand-authored, ISO 16612-cited; reference the PDF/X profiles for the inherited rules; appended to the `cmd/gen-rules` `profileFiles` table, local-only.
- `pkg/internal/compliance/pdfvt.go` — DPart-tree well-formedness + page coverage, DPM validity, encapsulated-group self-containment, `pdfvtid:` identifier.
- `pkg/internal/core/document` — `/DPart` tree resolver (new), with a `maxDPartDepth` anti-DoS cap per the resource-limits rule.
- `check.go` mapping; unit + corpus tests.

### PR3 — Remediator (very bounded)

PDF/VT remediation is almost entirely the underlying PDF/X remediation plus identifier injection:

- `fixer_pdfvt_identifier.go` — write the `pdfvtid:` XMP identifier.
- Delegate structural print fixes to the PDF/X-4 / X-5 / X-6 fixers.
- **Cannot author a DPart hierarchy, DPM, or encapsulation structure.** Variable-data structure is a generation-time concern; a PDF that lacks it is not "broken VT," it is "not VT." litho reports the absence; it does not synthesize the record tree. This is the hardest remediation limit of the three families and the spec states it plainly: **PDF/VT support is validation-first; remediation is confined to identifier + inherited PDF/X structural fixes.**

### PR4 — Corpus

See [PDF-X-E-VT-CORPUS.md](PDF-X-E-VT-CORPUS.md). PDF/VT has a citable public source (the Cal Poly PDF/VT Test File Suite); the corpus section covers it plus the encapsulation/DPart edge cases worth pinning.

## Out of scope

- Authoring variable-data documents (the DPart tree, DPM, encapsulated graphics). That is a composition engine, not a conforming processor.
- RIP-side caching behavior. litho validates that encapsulation is _declared correctly_; it does not model the cache.
- VT-2 / VT-2s multi-file and MIME-package (un)assembly. litho validates the references resolve; it does not package external-file sets or MIME containers.
