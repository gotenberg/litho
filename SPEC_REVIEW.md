# Spec-correctness review

Findings from a multi-pass audit of litho against its authoritative
specifications. The audit asks one question per file/line: does this code
derive its behaviour from a spec clause, or from a supposition (oracle
mimicry, "real-world producers do X", convenience defaults)?

Each finding ranks as:

- **HIGH** — produces or accepts spec-noncompliant output; risks a wrong
  validation verdict.
- **MEDIUM** — unbacked leniency or unbacked default; behaviour may
  silently diverge from the spec on edge cases.
- **LOW** — clarity-only; behaviour is fine but rationale is undocumented
  or misnamed.

Authoritative specs referenced:

- ISO 32000-1:2008 (PDF 1.7), ISO 32000-2:2020 (PDF 2.0).
- ISO 19005-1:2005 / 19005-2:2011 / 19005-3:2012 / 19005-4:2020 (PDF/A).
- ISO 14289-1:2014 / 14289-2:2024 (PDF/UA).
- ISO 16684-1:2019 (XMP).
- ISO/IEC 19757-3:2016 (Schematron).
- W3C XPath 1.0 (1999) and XPath 2.0 (2010).
- Factur-X 1.0.07 (FNFE-MPE, 2023).
- EN 16931-1:2017 + EN 16931-3-3:2020 (CEN).
- ZUGFeRD 1.0 (FeRD, 2014), ZUGFeRD 2.0 / 2.1+ (2018 / 2020).
- XRechnung (KoSIT).
- PEPPOL BIS 3.0 Billing (OpenPEPPOL).
- ICC.1:2010 (ICC v4), RFC 5646 / RFC 3066 (BCP 47).

VeraPDF, Mustang, and KoSIT validators are descriptive references only;
they cite the spec, they are not the spec.

## Theme A — Crypto and parser correctness bugs

Spec-noncompliant output or failed spec-mandated operations.

| Finding                                                                                                        | File:line                                               | Spec                                      |
| -------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- | ----------------------------------------- |
| `aesCBCEncryptZeroIV` omits PKCS7 padding when plaintext length is a multiple of 16, breaking OE/UE generation | `pkg/internal/crypto/encrypt.go:419-435`                | ISO 32000-2 §7.6.4.4 Algorithm 2.A step e |
| V5 password truncation asymmetric: encrypt truncates to 127 bytes, decrypt does not; long-password auth fails  | `pkg/internal/crypto/crypto.go:124` vs `encrypt.go:144` | ISO 32000-2 §7.6.5.3                      |
| `/P` defaults to `0` (all permissions denied) when absent; spec default is `0xFFFFFFFC`                        | `pkg/internal/crypto/crypto.go:62`                      | ISO 32000-1 Table 22                      |
| `/R` defaults to `/V` without consistency check; `/Length` default `40` mishandled if dict omits it            | `pkg/internal/crypto/crypto.go:60-61`                   | ISO 32000-1 §7.6.2                        |
| Lexer accepts scientific notation (`1e2`); a parsed token will round-trip into noncompliant output             | `pkg/internal/core/lexer/lexer.go:147-168`              | ISO 32000-1 §7.3.3                        |
| Odd-length hex string padded with `0` in-place; round-trip emits the padded form                               | `pkg/internal/core/parser/parser.go:961-966`            | ISO 32000-2 §7.3.4.3                      |
| Missing `endobj` silently tolerated with no spec basis                                                         | `pkg/internal/core/parser/parser.go:222-227`            | ISO 32000-1 §7.5.6                        |
| CCITT `/K=0` (Group3-1D) and `/K>0` (Group3-mixed) collapse into one ccitt.SubFormat                           | `pkg/internal/filter/filter.go:336-339`                 | ISO 32000-1 Table 11                      |

## Theme B — Remediation injects synthetic defaults the spec does not sanction

Fixer side. Each value below passes a validator but masks a real metadata
gap in the source document.

| Finding                                                                              | File:line                                                |
| ------------------------------------------------------------------------------------ | -------------------------------------------------------- |
| Injects `/Lang = "en"` when missing; spec does not mandate a default                 | `pkg/internal/remediation/fixer_lang.go:32-35`           |
| Hardcodes OutputIntent `/S = "GTS_PDFA1"` / `"GTS_PDFA"`                             | `pkg/internal/remediation/fixer_outputintent.go:243`     |
| Picks sRGB default colour space without inspecting page content                      | `pkg/internal/remediation/fixer_outputintent.go:213-214` |
| Replaces missing `/DestOutputProfile` with sRGB regardless of document colour space  | `pkg/internal/remediation/fixer_icc.go:118-120`          |
| Injects `dc:title = "Untitled"`, `xmp:CreatorTool = "litho"` when missing            | `pkg/internal/remediation/fixer_xmp.go:157-166`          |
| `PreserveContent` gating preserves a Separation spec violation when asserted         | `pkg/internal/remediation/fixer_sepcanon.go:31-37`       |
| Hardcodes `pdfaid:rev = "2020"` for every PDF/A-4 file regardless of actual revision | `pkg/internal/remediation/fixer_xmp.go:337-339`          |

## Theme C — Lenient acceptance on the Factur-X consumer path

Each tolerance below ships with a "real-world producers do X" comment.
No spec authority is cited. Either drop the tolerance or gate it behind an
opt-in `WithLenient(true)`-style option.

| Finding                                                    | File:line                                              | Spec mandates                                                                                                                                      |
| ---------------------------------------------------------- | ------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `XRECHNUNG` listed as Factur-X allowed level               | `pkg/internal/compliance/facturx.go:243-245`           | Factur-X 1.0.07 §6.2 lists MIN / BASIC WL / BASIC / EN 16931 / EXTENDED; XRECHNUNG arrives via ZUGFeRD 2.1+ — boundary needs explicit confirmation |
| `/AFRelationship = Alternative` accepted for Factur-X 1.0+ | `pkg/internal/compliance/facturx.go:222`               | Factur-X 1.0.07 mandates `/Data`                                                                                                                   |
| Embedded stream `/Subtype = application/xml` accepted      | `pkg/internal/compliance/facturx.go:506-511`           | Factur-X 1.0.07 mandates `text/xml` (`text#2Fxml`)                                                                                                 |
| `fx:Version = "2.1"` accepted alongside `"1.0"`            | `pkg/internal/compliance/facturx.go:242`               | Factur-X 1.0.07 fixes `fx:Version` at `"1.0"`                                                                                                      |
| XML and XMP decoders configured `Strict = false`           | `pkg/internal/compliance/cii.go:184`, `facturx.go:630` | No spec authorises non-strict XML parsing                                                                                                          |
| `/AFRelationship = Undefined` accepted for embedded files  | `pkg/internal/compliance/embedded.go:26`               | ISO 32000-2 §14.13 does not define this value                                                                                                      |

## Theme D — Schematron engine semantic gaps

Internals of the rule evaluator. Each item can cause verdict drift on
edge cases the bundled EN 16931 / Factur-X rule sets happen not to hit.

| Finding                                                                                                                    | File:line                                                          |
| -------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| Phase-scoped `sch:let` bindings loaded but never substituted; ISO Schematron §5.4.6 requires application                   | `pkg/internal/schematron/load.go:104`, `evaluate.go:151-158`       |
| Boolean coercion implements XPath 1.0 semantics; heterogeneous rewritten 2.0 sequences may diverge                         | `pkg/internal/schematron/evaluate.go:1019-1031`                    |
| Rewrite-failure fallthrough goes to `xpath.CompileWithNS()` with no compile-time panic guard around antchfx                | `pkg/internal/schematron/evaluate.go:995-1007`                     |
| `round()` rewritten as `floor(x + 0.5)`; negative halves diverge one ULP from XPath 1.0                                    | `pkg/internal/schematron/xpath_rewrite.go:138-149`                 |
| `upper-case` / `lower-case` rewritten via `translate()` over ASCII only; non-ASCII left untouched                          | `pkg/internal/schematron/xpath_rewrite.go:1535-1578`               |
| Quantifier variable bound via `node.InnerText()` loses node identity; sub-path tests against `$item` fail                  | `pkg/internal/schematron/evaluate.go:395`                          |
| `decimalEqualityEpsilon = 1e-6` not derived from EN 16931's two-decimal precision rule (conservative, but unbacked)        | `pkg/internal/schematron/xpath_rewrite.go:859`                     |
| Overlay XRechnung Fatal → Warning demotion is litho policy; ISO Schematron has no such rule                                | `pkg/internal/compliance/cii.go:586-591`                           |
| `augmentProfileWithBaseline()` layers ~600 baseline rules onto Factur-X EN 16931 profile; spec does not prescribe layering | `pkg/internal/compliance/cii_schematron/cii_schematron.go:233-295` |
| PEPPOL overlay routing keyed off exact `urn:cen.eu:en16931:2017` string; spec does not prescribe this routing              | `pkg/internal/compliance/cii.go:467-481`                           |

## Theme E — Unbacked depth limits and magic numbers

Defensive coding. Specs do not prescribe these limits but do not forbid
them. Behaviour is fine; the comments should label them as anti-DoS
guards, not imply spec backing.

`pkg/internal/compliance/actions.go:79` (200),
`check.go:237` (100),
`structure.go:55` (200),
`embedded.go:430` (2),
`accessibility_files.go:273` (30),
`accessibility_files.go:302` (10),
`accessibility_ua2.go:162` (200),
`pkg/internal/filter/filter.go:25` (256 MB).

## Theme F — Hardcoded tables to verify against spec tables

Each list below should carry an inline citation to the spec table it
mirrors, and ideally be auto-generated from the spec where licensing
permits.

- `pkg/internal/compliance/fonts_helpers.go:16-31` — Standard 14 fonts →
  ISO 32000-1 §9.6.2.2.
- `pkg/internal/compliance/color.go:65-76` — ICC device classes / colour
  spaces → ICC.1:2010 Annex A.
- `pkg/internal/compliance/content.go:15-46` — PDF operator set → ISO
  32000-2 Annex A.
- `pkg/internal/compliance/embedded.go:17-27` — `/AFRelationship` values
  → ISO 32000-2 §14.13.

## Counts

- HIGH: 32
- MEDIUM: 29
- LOW: 16

## Suggested ordering

1. Theme A. Real bugs producing wrong output.
2. Theme C. Pick a single Factur-X philosophy: strict by default, opt-in
   lenient. The current "tolerate by default" stance ships verdicts that
   diverge from the spec.
3. Theme D1 (phase lets). Silent semantic gap in the Schematron engine.
4. Theme B. Decide which synthetic defaults stay vs. become
   caller-supplied.
5. Theme E / F. Annotate, then move on.

## Out of scope for this audit

- VeraPDF rule descriptions copied verbatim into `pkg/internal/compliance/rules.go`
  retain their em-dashes because golden comparisons key off them.
- LICENSE text under `pkg/internal/compliance/cii_schemas/` and
  `cii_schematron/` is preserved verbatim per the upstream redistribution
  clauses.
- Plugins under `pkg/plugins/` were not audited; they wrap external C
  libraries whose conformance lives with the library, not litho.
