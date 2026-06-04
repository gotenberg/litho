# PDF/X, PDF/E, PDF/VT conformance corpus

Test-corpus and oracle strategy shared by the three print/technical conformance families: [PDF-X.md](PDF-X.md), [PDF-E.md](PDF-E.md), [PDF-VT.md](PDF-VT.md). Read those first; this spec assumes their validators and profile enums exist.

## The problem: there is no oracle

litho's [integration matrix](test/integration/README.md) scores its verdicts against external ground-truth tools captured at corpus-init time: **PDF.js** for text/metadata, **VeraPDF** for PDF/A and PDF/UA compliance + remediation, **pdfcpu** for bookmarks. The model works because each is a redistributable tool that can be baked into the provisioning Docker image and re-run in CI.

That model breaks for PDF/X, PDF/E, and PDF/VT, because **no redistributable tool validates them**:

| Candidate oracle                  | PDF/X                          | PDF/E | PDF/VT | Redistributable | Notes                                                                            |
| --------------------------------- | ------------------------------ | ----- | ------ | --------------- | -------------------------------------------------------------------------------- |
| VeraPDF                           | no                             | no    | no     | yes             | PDF/A + PDF/UA only; no shipped X/E/VT profiles                                  |
| Ghostscript                       | partial (create, not validate) | no    | no     | yes             | can _produce_ PDF/X via `PDFX_def.ps`; no per-rule verdict output                |
| JHOVE                             | no                             | no    | no     | yes             | PDF module is well-formedness / PDF-1.x only                                     |
| Apache PDFBox / preflight         | partial (A-1b-oriented)        | no    | no     | yes             | not an X/E/VT validator                                                          |
| MuPDF / pdfcpu                    | no                             | no    | no     | yes             | no conformance validation                                                        |
| callas pdfToolbox                 | yes                            | yes   | yes    | **no**          | industry-standard preflight engine (GWG member tool); commercial, license-locked |
| Adobe Preflight / Enfocus PitStop | yes                            | yes   | yes    | **no**          | authoritative; commercial                                                        |

The redistributable column has no validator; the validators are exactly the tools we cannot bake into CI. So the corpus cannot follow the `remediation` model (shell out to a live tool); it must follow — and extend — the **captured-golden** model (`compliance`, `text`, `metadata`), where the catch is that the golden's _source_ is no longer a tool the image can run.

## Strategy: by-construction backbone + blessed canary

Two fixture layers, in priority order. The first needs no oracle at all; the second records provenance for the verdicts it cannot derive.

### Layer 1 — Synthesized, by-construction fixtures (the backbone)

Hand-build minimal fixtures whose verdict is _self-evident from how they are constructed_, a matched pass/fail pair per rule. This is how the PDF/A ecosystem bootstrapped before the VeraPDF corpus existed: the **Isartor test suite** (PDF/A Competence Center) shipped ~200 deliberately-failing files organized by PDF/A-1 clause. litho should ship a "litho PDF/X (and E, VT) test suite" in the same spirit, improving on Isartor by committing a matched **pass/fail pair** per rule (Isartor is fail-only).

- For rule R (e.g. PDF/X-1a "no RGB color"): commit `x1a-rgb-image-FAIL.pdf` (a CMYK-valid file with one RGB image) and `x1a-rgb-image-PASS.pdf` (identical, image converted to CMYK). The verdict is true _by construction_ and carries an ISO 15930 clause citation in the golden.
- Build them from a small Go fixture-builder under each corpus's `download.sh` (or commit them directly — they are tiny). Building on litho's own writer keeps them pure-Go and reproducible.

This layer is the load-bearing oracle. It is independent of litho's validator, so it catches the failure mode pure self-validation cannot: **the validator and the remediator agreeing on a wrong answer.** It doubles as the per-rule unit-test corpus.

**Golden + profile model (resolve before authoring).** The existing harness has two relevant `profile_mode`s: `expand` cartesian-products every fixture over _all_ declared profiles, and `auto` routes each fixture to the _one_ profile it claims. Neither fits a by-construction suite cleanly:

- Under `expand`, `x1a-rgb-image-FAIL.pdf` becomes ten fixtures (one per X level), and its golden must then state the verdict under all ten levels — an RGB image is forbidden under X-1a but _allowed_ under X-3/X-4 with an RGB output intent. So "mismatched: 0" is not free; it requires authoring a full per-level verdict matrix per fixture. The golden schema supports it (`{ "<fixture>": { "<profile>": { "compliant": bool, "rule": "15930-x §y", "why": "..." } } }`), but the authoring burden is real and must be owned.
- The cleaner alternative is **one target profile per fixture** (a fixture is built to exercise exactly one level). The harness has no such mode for a non-`auto` corpus today — adding a `single`/targeted `profile_mode` (profile read from a `claims.csv` or the directory name) is a small, called-out harness addition. Recommended.

Pick one and make the manifest match it. The recommendation is the targeted mode; it makes the by-construction backbone genuinely definitional.

### Layer 2 — Real-world canary fixtures (blessed, with provenance)

Real files exercise interactions the synthetic minimals miss. Named, obtainable sources (verify license per source — none may be vendored without checking):

- **PDF/X:** the **Ghent Workgroup (GWG) Output Suite** and the **Altona Test Suite** — the print industry's reference PDF/X-1a/X-3/X-4 files.
- **PDF/VT:** the **Cal Poly Graphic Communications "PDF/VT Test File Suite"** (ISO 16612-2 PDF/VT-1 files), indexed by the PDF Association's `pdf-corpora`.
- **PDF/E:** **no public sample corpus is known.** PDF/E Layer-2 will likely be empty; E coverage leans on Layer-1 synthesis, with vendor/CAD exports as an optional later addition. State this so the reader does not hunt for a corpus that does not exist.
- **PDF Association** sample sets and **vendor-blessed exports** (callas/Adobe/Enfocus declaring a file conformant) as known-good. Note: the `pdf20examples` set already vendored as the `pdf20` corpus is **CC-BY-SA-4.0** (share-alike, attribution required), _not_ BSD — so re-check every PDF Association set's license individually and honor the share-alike/attribution terms when vendoring.

Each Layer-2 fixture's golden records **who blessed the verdict and how** (`"provenance": "callas pdfToolbox 15, GWG2022 profile"` or `"Cal Poly PDF/VT suite, declared PDF/VT-1"`), so a reviewer can audit it. Because the blessing tool is not in CI, these goldens are **frozen artifacts**: re-blessed manually only when the standard or fixture changes, never regenerated by the build.

## Harness integration

The matrix runner's per-cell mechanics are reused, but this is **not** a zero-engine-change addition. Be precise about what each piece costs:

- **Adding a corpus** _is_ free per the README ("drop `download.sh` + `manifest.json`; nothing else changes") — true for the corpus directories.
- **Adding the `pdfx`/`pdfe`/`pdfvt` features is an engine change.** Each needs a `feature_<name>.go` implementing the `Feature` interface plus a `featureRegistry` entry in `test/integration/cmd/corpus/run.go`, the same shape as `feature_compliance.go`. Each also needs its own dir→profile map (mirroring `dirToPDFAProfile`) and a `ValidatePDFX`-calling equivalent of `validateWithLitho`.
- **Extending claim-detection is an engine change.** See [auto-mode](#auto-mode-and-nested-profiles).

### New features

`pdfx`, `pdfe`, `pdfvt` validity features, each a **captured-golden** feature (like `compliance`, not the live `remediation`):

| Feature | Golden source                       | Compares                                                    |
| ------- | ----------------------------------- | ----------------------------------------------------------- |
| `pdfx`  | curated (by-construction + blessed) | litho's PDF/X verdict per profile against the frozen golden |
| `pdfe`  | curated                             | litho's PDF/E verdict against the frozen golden             |
| `pdfvt` | curated                             | litho's PDF/VT verdict against the frozen golden            |

A later `pdfx-remediation` feature (remediate, then re-validate with litho and re-diff the golden's post-fix expectation) is possible, but note its honest limit: without a third-party oracle it is **self-referential**, so it is trustworthy only over the Layer-1 by-construction fixtures, where the post-remediation expected verdict is also known by construction.

### Auto-mode and nested profiles

The auto-mode detector (`detectClaimedProfile` → `profileFromXMP` in `test/integration/cmd/corpus/fixture.go`) reads **only the XMP packet** via `r.XMPMetadata`, and `isProfileDir` hardcodes `PDF_A-`/`PDF_UA-` prefixes. Two consequences for Layer-2 print canaries:

1. **X-1a/X-3 carry their identifier in the Info dictionary, not XMP** (see [PDF-X.md](PDF-X.md)). Auto-mode as built cannot detect them at all; scan-claims needs a new Info-dict reader for `/GTS_PDFXVersion`, and `isProfileDir`/`profileFromXMP` need `PDF_X-`/`PDF_E-`/`PDF_VT-` branches.
2. **The families nest, so one claim per file is insufficient.** A PDF/VT-1 file is _also_ a valid PDF/X-4 file and should pass both `PDF_X-4` and `PDF_VT-1`; auto-mode routes to a single profile. And `GTS_PDFXVersion` alone does not distinguish X-4 from X-4p or X-5g/pg/n — the sub-level comes from structure (`DestOutputProfile` vs `DestOutputProfileRef`, referenced XObjects), not the version string. Specify multi-profile claim emission (a fixture per claimed profile) and structural sub-level disambiguation, or restrict Layer-2 to explicit per-profile fixture directories.

### Baseline, CI gating, and Layer-2 triage

Run `make corpus-baseline` once the features are green to capture per-cell counts into `test/integration/baseline.json`; `make corpus-check` then regression-gates PDF/X-E-VT like PDF/A. The Layer-1 backbone should reach `mismatched: 0` (verdicts are definitional); a non-zero count there is a real validator bug.

Layer-2 needs an adjudication path the auto-oracle features do not, because a frozen blessing can be _wrong_ (or a litho change can be more correct than the original commercial verdict) with no live oracle to settle it. Adopt the same discipline as the Factur-X known-divergences list: a committed, provenance-tagged **allowed-divergence** registry, and a documented triage for any print-canary mismatch (litho bug? stale blessing? genuine tool disagreement?). Decide explicitly whether print-canary cells are baseline-gated or report-only, so a blessing-vs-litho disagreement cannot silently fail CI without an adjudication path.

## Phasing

| Phase | Deliverable                                                                           | Oracle dependency        |
| ----- | ------------------------------------------------------------------------------------- | ------------------------ |
| A     | Layer-1 by-construction suite per family + `pdfx`/`pdfe`/`pdfvt` features             | none (self-evident)      |
| B     | Layer-2 canary corpora with blessed, provenance-tagged goldens                        | one-time manual blessing |
| C     | (optional) wire a live oracle if VeraPDF ships X profiles or an OSS validator emerges | future tool              |

Phase A is shippable alongside each validator PR and is the part that actually proves correctness — subject to the same standing dependency the validators have: Layer-1 clause citations require the licensed ISO 15930/24517/16612 text in hand, so "immediately" means "as soon as that text (already a validator prerequisite) is obtained." Phase B adds real-world breadth. Phase C is opportunistic.

## Honest risks

- **Goldens are only as good as their blessing.** Layer 2 inherits the judgment of a commercial tool CI cannot re-run; provenance tagging makes that auditable but not self-correcting. Layer 1 is the defense: independent, citable, definitional.
- **Self-reference in remediation testing.** litho validating its own remediated output proves internal consistency, not conformance. Only the Layer-1 fixtures break the loop — so remediation features must run over them, not only over real-world files.
- **Fixture licensing.** GWG, Altona, Cal Poly, and `pdf20examples` (CC-BY-SA-4.0) each carry their own terms; prefer `download.sh` fetches and synthesized fixtures over committing third-party binaries, and honor attribution/share-alike where required.
- **Standard drift.** New ISO 15930/24517/16612 revisions (X-6, VT-3, a published E-2) change verdicts; Layer-2 frozen goldens must be re-blessed on each, which is manual by design.
