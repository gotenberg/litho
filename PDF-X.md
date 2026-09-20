# PDF/X support in litho

Plan to add ISO 15930 (PDF/X) validation and remediation across every conformance level, plus a test corpus.

PDF/X is the print-production conformance family. It sits beside the three families litho already ships (PDF/A archival, PDF/UA accessibility, Factur-X e-invoicing) and is the largest single gap in litho's "Conforming Processor" mandate. PDF/X is also the structural base for PDF/VT (see [PDF-VT.md](PDF-VT.md)) and shares its color/output-intent machinery with PDF/E (see [PDF-E.md](PDF-E.md)); ship it first. The shared test-corpus strategy lives in [PDF-X-E-VT-CORPUS.md](PDF-X-E-VT-CORPUS.md).

## What PDF/X is

PDF/X constrains a PDF so a print provider can process it "blind": every resource needed to render the intended print is self-contained and unambiguous. The constraints are mostly subtractive (no live transparency below X-4, no encryption, no forbidden actions, no uncontrolled RGB) plus a few additive requirements (a print OutputIntent, page geometry boxes, the version identifier, embedded fonts).

There is no separate file body as with Factur-X. PDF/X is a profile of plain PDF: same objects, narrower rules.

## Versions and conformance levels

"All versions" spans four generations across the ISO 15930 parts. The conformance identifier migrated location between generations (see the [identifier note](#identifier-and-trapped-migrate-to-xmp-at-x-4)): `Info /GTS_PDFXVersion` + `/GTS_PDFXConformance` for X-1a/X-3, the XMP `pdfxid:` namespace (`http://www.npes.org/pdfx/ns/id/`) for X-4 onward.

| Level | ISO part                    | Base PDF  | Color model                          | Transparency | Output profile                      |
| ----- | --------------------------- | --------- | ------------------------------------ | ------------ | ----------------------------------- |
| X-1a  | 15930-1:2001 / 15930-4:2003 | 1.3 / 1.4 | CMYK + spot + gray only (no RGB/Lab) | forbidden    | embedded CMYK ICC                   |
| X-3   | 15930-3:2002 / 15930-6:2003 | 1.3 / 1.4 | + device/ICC RGB/Lab/CalRGB/CalGray¹ | forbidden    | embedded ICC                        |
| X-4   | 15930-7:2008 (+2010 corr.)  | 1.6       | managed/device RGB + CMYK + spot¹    | allowed      | embedded ICC                        |
| X-4p  | 15930-7                     | 1.6       | same as X-4                          | allowed      | external (`DestOutputProfileRef`)   |
| X-5g  | 15930-8:2010                | 1.6       | same as X-4 + referenced XObjects    | allowed      | embedded ICC                        |
| X-5pg | 15930-8:2010                | 1.6       | X-5g + external profile              | allowed      | external                            |
| X-5n  | 15930-8:2010                | 1.6       | X-4 + n-colorant output profile      | allowed      | embedded **or** external n-colorant |
| X-6   | 15930-9:2020                | 2.0       | managed/device RGB + CMYK + spot¹    | allowed      | embedded ICC                        |
| X-6p  | 15930-9:2020                | 2.0       | same as X-6                          | allowed      | external                            |
| X-6n  | 15930-9:2020                | 2.0       | same as X-6                          | allowed      | embedded or external n-colorant     |

¹ **Device color is conditional, not free.** In X-3/X-4/X-6 a device color space (DeviceRGB/DeviceGray/DeviceCMYK) is permitted only when the OutputIntent profile is of that same space — DeviceRGB requires an RGB output-intent profile, etc. The validator gates device color on the output-intent color space, it does not allow/forbid by name. X-1a permits only DeviceCMYK/DeviceGray + spot (the OutputIntent is CMYK).

PDF/X-2 (15930-5:2003) is intentionally excluded: it was a partial-exchange variant that saw effectively no adoption and is superseded by X-5. So "every conformance level" means the ten live levels above.

> Per-rule clause numbers must be transcribed from the ISO 15930 documents during implementation. They are licensed and, unlike PDF/A and PDF/UA, **have no veraPDF validation profile to generate from** (see [Rule sourcing](#rule-sourcing)). The 2008-vs-2010 X-4 corrigendum deltas also need checking against the source.

The conformance set litho should expose: `X-1a`, `X-3`, `X-4`, `X-4p`, `X-5g`, `X-5pg`, `X-5n`, `X-6`, `X-6p`, `X-6n`.

### Identifier and Trapped migrate to XMP at X-4

This is a profile-gated rule with real teeth, easy to get backwards:

- **X-1a / X-3** declare conformance in the **Info dictionary**: `/GTS_PDFXVersion` + `/GTS_PDFXConformance`.
- **X-4 and later** declare it **only** in XMP, `pdfxid:GTS_PDFXVersion`. A `GTS_PDFXVersion` key left in the Info dict is itself a violation; `GTS_PDFXConformance` is not used at all from X-4 on.
- The `/Trapped` declaration follows the same migration: Info `/Trapped` (`True`/`False`, never `Unknown`) for X-1a/X-3, and the XMP `Trapped` property for X-4+ (confirm against 15930-7/-9 whether Info `/Trapped` is forbidden or merely redundant for X-4+).

## Requirement families mapped to litho's check groups

PDF/X validates the same domains PDF/A already does, so most checks extend an existing [check group](pkg/internal/compliance/README.md). The PDF/X-only rules cluster in a new `pdfx.go` group.

| Requirement                                                                              | litho check group         | Reuse vs new                                        |
| ---------------------------------------------------------------------------------------- | ------------------------- | --------------------------------------------------- |
| Exactly one `GTS_PDFX` OutputIntent with `DestOutputProfile` (or `…Ref`)                 | `color_outputintent.go`   | extend (today: ignores GTS_PDFX for PDF/A coverage) |
| Identifier in the right place per level (Info for X-1a/X-3; `pdfxid:` XMP only for X-4+) | `document.go` / `xmp*.go` | new                                                 |
| Device color permitted only when the OutputIntent profile matches its space              | `color_device*.go`        | extend (output-intent-coupled)                      |
| ICC-managed RGB/Lab permitted only X-3+                                                  | `color_iccbased.go`       | extend                                              |
| No live transparency (X-1a, X-3)                                                         | `transparency.go`         | extend (profile-gated)                              |
| All fonts embedded (incl. Type3 glyph fonts)                                             | `fonts*.go`               | reuse (PDF/A already enforces)                      |
| Every page has MediaBox + (TrimBox xor ArtBox); BleedBox optional                        | `pdfx.go`                 | new                                                 |
| No encryption                                                                            | `file_structure.go`       | reuse                                               |
| No `/AA`, `/OpenAction` launch, JavaScript, or forbidden actions                         | `actions.go`              | extend                                              |
| `/Trapped` valid in the right place per level                                            | `pdfx.go` / `xmp*.go`     | new                                                 |
| Annotations inside BleedBox/TrimBox are Hidden/NoView or printing-safe                   | `annotations.go`          | extend                                              |
| Transfer functions / halftone restrictions; no OPI (X-1a/X-3)                            | `graphics.go`             | extend                                              |
| Referenced XObjects valid + resolvable (X-5g/pg)                                         | `xobjects.go`             | new (external refs)                                 |
| External output profile reference well-formed (X-4p/X-5n/X-6p/X-6n)                      | `color_outputintent.go`   | new                                                 |

The leverage: litho's color, font, transparency, and XObject groups were built for PDF/A and already resolve and inspect the constructs PDF/X regulates. PDF/X is mostly a new _rule set over existing machinery_ — but the dispatch layer is real work, not free (see [PR2](#pr2--validator)).

## Rule sourcing

PDF/A and PDF/UA rules are code-generated from veraPDF XML profiles by `cmd/gen-rules`. **No equivalent profile exists for PDF/X.** Two options:

1. **Hand-author veraPDF-format XML profiles** under `pkg/internal/compliance/profiles/PDF_X/` (one per level) and keep the existing generation + check-group pipeline. `cmd/gen-rules` reads local XML by default, so the profiles feed the generator — but their filenames must be appended to the hardcoded `profileFiles` table in `cmd/gen-rules/main.go`, and marked local-only so `--download` skips them (there is no upstream URL). Each rule cites its ISO 15930 clause inline. Preferred: it keeps PDF/X mechanically close to PDF/A and reuses `check.go` group wiring.
2. Hand-author Go rules in a `rules_pdfx.go` (the `rules_extra.go` pattern). Faster to start, diverges from the generated pipeline.

Recommendation: option 1. Per CONTRIBUTING ("hardcoded spec tables cite the governing clause… where licensing permits, are generated"), every rule carries an ISO 15930 clause citation at its decision site; the absence of a redistributable upstream profile is documented as the reason the table is hand-maintained rather than generated.

## What exists in litho today

| Capability                                                       | Status                                                                                                                                                                                                                                                          |
| ---------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| OutputIntent parsing + ICC validation                            | Done (`color_outputintent.go`, `color_iccbased.go`) — currently does **not** count `GTS_PDFX` as PDF/A coverage (ignored, not rejected); PDF/X must accept and require it                                                                                       |
| Device / ICCBased / Separation / DeviceN color inspection        | Done (`color*.go`)                                                                                                                                                                                                                                              |
| Font embedding enforcement                                       | Done (`fonts*.go`)                                                                                                                                                                                                                                              |
| Transparency group + soft-mask inspection                        | Done (`transparency.go`)                                                                                                                                                                                                                                        |
| XObject (image/form) + JPEG2000 header inspection                | Done (`xobjects.go`, `jpx.go`)                                                                                                                                                                                                                                  |
| Encryption / actions / XMP groups                                | Done                                                                                                                                                                                                                                                            |
| Page-box (Media/Trim/Art/Bleed) parsing + `models.Page` exposure | Done (`catalog.go` `resolveBoxArray`, surfaced on `models.Page` and `reader.go`). **Caveat:** Trim/Art/Bleed default to CropBox when absent, so the TrimBox-xor-ArtBox rule must inspect the raw `/TrimBox`/`/ArtBox` dict keys, not the resolved struct fields |
| `GTS_PDFXVersion` / `pdfxid:` parsing + a PDF/X profile enum     | Done — `semantics/pdfx.go` `DeclaredPDFX`, `models.PDFXProfile` (ten levels), `Reader.DeclaredPDFX`. Detection only: litho reports the level a document **declares**, it does not validate against it                                                           |
| Referenced (external) XObject resolution (X-5)                   | Missing                                                                                                                                                                                                                                                         |
| External output-profile reference handling (X-4p/X-5n/X-6\*)     | Missing                                                                                                                                                                                                                                                         |
| RGB→CMYK color conversion, transparency flattening               | Missing — deferred pending [COLOR-MANAGEMENT.md](COLOR-MANAGEMENT.md) + [TRANSPARENCY-FLATTENING.md](TRANSPARENCY-FLATTENING.md), not part of the initial PDF/X remediation (see [Remediation limits](#remediation-limits))                                     |

## Plan

Ship in four PRs. PR1–PR2 deliver the validator (the high-value, fully-achievable half); PR3 delivers remediation within its honest limits; PR4 wires the corpus.

### PR1 — Models + API surface

- `pkg/models/compliance.go` — **landed.** `type PDFXProfile string` with constants `PDFX1a`, `PDFX3`, `PDFX4`, `PDFX4p`, `PDFX5g`, `PDFX5pg`, `PDFX5n`, `PDFX6`, `PDFX6p`, `PDFX6n`, plus `IsValid()` and `String()` mirroring `PDFAProfile`, shipped with `semantics.DeclaredPDFX` and `Reader.DeclaredPDFX(ctx)`. Three things the corpus taught that the spec does not state: the LEVEL lives in `/GTS_PDFXConformance` (the version key carries only the base, `PDF/X-1:2001`); only the **catalog's** `/Metadata` is a document claim, since resource-level packets also carry `pdfxid:`; and a `pdfaProperty:name` extension-schema declaration is not a claim. **The rest of PR1 is NOT built**: `ValidatePDFX`, `ValidatePDFXReport` and `RemediatePDFX` are deliberately absent, because a validator with no rules returns an empty error slice — "conformant" — for every file.
- `pkg/pdf/reader.go` — `Reader.ValidatePDFX(ctx, profile)` returning `[]error` (mirrors `ValidatePDFA`) + `ValidatePDFXReport(ctx, profile)` returning `[]Violation` (mirrors `ValidatePDFAReport`).
- `pkg/pdf/writer.go` — `Writer.RemediatePDFX(ctx, profile, opts...)` signature (no-op body until PR3).
- `pkg/internal/compliance/compliance.go` — `ValidatePDFX(ctx, doc, profile)` dispatcher.
- `cmd/litho` — extend `validate`/`remediate` profile maps with the PDF/X levels (one line each, like the Factur-X wiring).

### PR2 — Validator

- `profiles/PDF_X/PDFX-1A.xml … PDFX-6N.xml` — hand-authored veraPDF-format profiles, ISO-cited; appended to the `cmd/gen-rules` `profileFiles` table, local-only.
- `pkg/internal/compliance/pdfx.go` — the PDF/X-only check group: OutputIntent presence/uniqueness, identifier location per level, page-box geometry, `/Trapped`, transfer-function/OPI restrictions.
- **Dispatch plumbing (not just predicates):** `validationCtx` and `check.go` are PDF/A-shaped (keyed on `models.PDFAProfile`, with `PDFAPart`/`PDFALevel`/`IsPDFUA`). PR2 must add a PDF/X family to the context (or a parallel `pdfx` context) and a `pdfxCheckGroups` map keyed by `models.PDFXProfile`, then teach each reused check (`color_device*`, `transparency`, `actions`, `annotations`) to recognise the PDF/X family. This is modest but is machinery.
- Box presence detection: add raw-key inspection for `/TrimBox`/`/ArtBox` (the resolved `models.Page` fields mask absence).
- `check.go` — map the PDF/X levels to their check groups.
- Tests: unit per check + the corpus feature from PR4.

### PR3 — Remediator (bounded)

Fixers (new `fixer_pdfx_*.go`, per the [fixer contract](pkg/internal/remediation/README.md)):

- `fixer_pdfx_outputintent.go` — inject a `GTS_PDFX` OutputIntent from a caller-supplied or bundled ICC output profile (e.g. a default CMYK profile) when missing.
- `fixer_pdfx_identifier.go` — **branch on target level.** For X-1a/X-3 write Info `/GTS_PDFXVersion` + `/GTS_PDFXConformance`. For X-4/X-5/X-6 write **only** the `pdfxid:` XMP entry and strip any `GTS_PDFXVersion` left in the Info dict; emit no `GTS_PDFXConformance`. Writing the Info key for an X-4+ target produces a non-conformant file, violating the correctness prime directive.
- `fixer_pdfx_boxes.go` — set TrimBox = CropBox/MediaBox when absent.
- `fixer_pdfx_trapped.go` — set the trapping declaration to `False` in the right place per level (Info for X-1a/X-3, XMP for X-4+).
- `fixer_pdfx_strip.go` (content-modifying tier) — remove forbidden actions/JavaScript/encryption, drop OPI.
- Reuse existing font-embedding and encryption-removal fixers.

### PR4 — Corpus

See [PDF-X-E-VT-CORPUS.md](PDF-X-E-VT-CORPUS.md). Adds a `pdfx` validity feature with curated, known-verdict fixtures (no live FOSS oracle exists).

## Remediation limits

This is the honest boundary, and it follows directly from the two capabilities litho does not have (color management and transparency flattening — see the project gap analysis):

- **Cannot convert color.** Remediating an RGB document to **X-1a** (CMYK + spot only) requires RGB→CMYK conversion through the output profile. litho has no color-management engine yet (planned in [COLOR-MANAGEMENT.md](COLOR-MANAGEMENT.md)), so initial X-1a/X-3 remediation can only _report_ the offending color and fix structure around it; it cannot recolor. A document that is already CMYK/spot but missing the OutputIntent and identifier _is_ fully remediable.
- **Cannot flatten transparency.** X-1a and X-3 forbid live transparency. litho cannot flatten, so it cannot down-convert an X-4 file to X-3.
- **X-4 / X-6 remediation is the achievable sweet spot.** Those levels permit managed/device RGB (output-intent-coupled) and live transparency, so the remaining gaps (OutputIntent, identifier, boxes, `/Trapped`, font embedding, stripping encryption/actions) are all structural and _are_ within litho's reach.

The spec therefore commits to: **validation for all levels; full remediation for X-4/X-6-class structural conformance; partial (report-only for color/transparency) remediation for X-1a/X-3.** `RemediatePDFX` returns the standard `RemediationReport` whose `Residual` lists exactly the color/transparency violations litho declined to fix, so the limit is auditable per call.

If color conversion and transparency flattening are later added (they would also unlock print-faithful PDF/A-2b/3b remediation), X-1a/X-3 remediation closes automatically.

## Out of scope

- Color-managed rendering / soft-proofing. litho validates color structure, it does not render.
- Preflight _fixups_ beyond the fixers above (image resampling, ink-density limits, overprint correction) — these are pdfToolbox-class features, not conformance remediation.
- Authoring referenced-XObject (X-5) external files. litho validates them; it does not generate multi-file X-5 sets.
- PDF/X-2 (15930-5) — obsolete, unused.
