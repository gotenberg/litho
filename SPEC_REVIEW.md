# Spec-correctness review

Findings from a multi-pass audit of litho against its authoritative
specifications. The audit asks one question per file/line: does this code
derive its behaviour from a spec clause, or from a supposition (oracle
mimicry, "real-world producers do X", convenience defaults)?

This revision folds in a second pass (Theme G) that swept the writer,
filters, signature, colour, and CMap paths the first pass treated
lightly, re-verified every first-pass finding against current source,
fixed line-number drift, and closed two first-pass crypto findings that
turned out to be spec-correct (see Closed).

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
- RFC 5652 (CMS), RFC 3161 (timestamps), TIFF 6.0 (Predictor 2).

VeraPDF, Mustang, and KoSIT validators are descriptive references only;
they cite the spec, they are not the spec.

## Theme A — Crypto and parser correctness bugs

Spec-noncompliant output or failed spec-mandated operations.

| Finding                                                                                             | File:line                                    | Spec                 |
| --------------------------------------------------------------------------------------------------- | -------------------------------------------- | -------------------- |
| `/P` defaults to `0` (all permissions denied) when absent; spec default is `0xFFFFFFFC`             | `pkg/internal/crypto/crypto.go:62`           | ISO 32000-1 Table 22 |
| `/R` defaults to `/V` without consistency check; `/Length` default `40` mishandled if dict omits it | `pkg/internal/crypto/crypto.go:60-61`        | ISO 32000-1 §7.6.2   |
| Lexer accepts scientific notation (`1e2`); a parsed token will round-trip into noncompliant output  | `pkg/internal/core/lexer/lexer.go:147-167`   | ISO 32000-1 §7.3.3   |
| Odd-length hex string padded with `0` in-place; round-trip emits the padded form                    | `pkg/internal/core/parser/parser.go:961-966` | ISO 32000-2 §7.3.4.3 |
| Missing `endobj` silently tolerated with no spec basis                                              | `pkg/internal/core/parser/parser.go:222-227` | ISO 32000-1 §7.5.6   |
| Resolved: CCITT `/K=0` (Group3-1D) and `/K>0` (Group3-mixed) collapsed into one ccitt.SubFormat     | `pkg/internal/filter/ccitt/decode.go`        | ISO 32000-1 Table 11 |

Re-verification notes (second pass):

- `/P`, `/R`/`/Length`, lexer scientific notation, hex odd-pad, and
  missing `endobj` were each re-read at the cited line and remain open.
- Lexer scientific-notation accept logic spans `147-167` (first pass
  cited `147-168`; line 168 is the closing brace).
- The CCITT `/K` finding is resolved: `pkg/internal/filter/ccitt` decodes
  Group 4, Group 3 one-dimensional and Group 3 mixed coding each as its `/K`
  names, with end-of-line codes optional as Table 11 makes them, in place of
  `golang.org/x/image/ccitt`, which had no mixed coding and demanded them.

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
opt-in `WithLenient(true)`-style option. Line numbers below were
re-verified against current source.

| Finding                                                    | File:line                                                            | Spec mandates                                                                                                                                      |
| ---------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `XRECHNUNG` listed as Factur-X allowed level               | `pkg/internal/compliance/facturx.go:245`                             | Factur-X 1.0.07 §6.2 lists MIN / BASIC WL / BASIC / EN 16931 / EXTENDED; XRECHNUNG arrives via ZUGFeRD 2.1+ — boundary needs explicit confirmation |
| `/AFRelationship = Alternative` accepted for Factur-X 1.0+ | `pkg/internal/compliance/facturx.go:234`                             | Factur-X 1.0.07 mandates `/Data` (live tolerance is the `altRelationships` field)                                                                  |
| Embedded stream `/Subtype = application/xml` accepted      | `pkg/internal/compliance/facturx.go:510-512`                         | Factur-X 1.0.07 mandates `text/xml` (`text#2Fxml`)                                                                                                 |
| `fx:Version = "2.1"` accepted alongside `"1.0"`            | `pkg/internal/compliance/facturx.go:242`                             | Factur-X 1.0.07 fixes `fx:Version` at `"1.0"`                                                                                                      |
| XML and XMP decoders configured `Strict = false`           | `pkg/internal/compliance/cii.go:184`, `cii.go:516`, `facturx.go:630` | No spec authorises non-strict XML parsing (also sets `xml.HTMLEntity`)                                                                             |
| `/AFRelationship = Undefined` accepted for embedded files  | `pkg/internal/compliance/embedded.go:26`                             | ISO 32000-2 §14.13 defines `Unspecified`, not `Undefined`                                                                                          |

## Theme D — Schematron engine semantic gaps

Internals of the rule evaluator. Each item can cause verdict drift on
edge cases the bundled EN 16931 / Factur-X rule sets happen not to hit.

| Finding                                                                                                                    | File:line                                                          |
| -------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| Phase-scoped `sch:let` bindings loaded but never substituted; ISO Schematron §5.4.6 requires application                   | `pkg/internal/schematron/load.go:104`, `evaluate.go:151-216`       |
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

Related consistency smell: the same conceptual limit, page-tree
recursion depth, is `256` in `pkg/internal/core/document/catalog.go:296`
but `100` in `pkg/internal/compliance/check.go:237`, with no
cross-reference explaining the divergence.

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

## Theme G — Second-pass findings: signatures, writer, filters, colour, CMap

New findings surfaced by the second pass. Each was read at the cited line
and confirmed against current source.

| Finding                                                                                                                                                                                                                                                                                       | File:line                                                      | Spec                                               | Sev  |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------- | ---- |
| `VerifySignatures` never verifies the PKCS#7/CMS SignerInfo: `HashValid` is set from "a hash was computed and ByteRange[1] > 0", with no digest comparison and no `rsa.Verify`/`ecdsa.Verify`. Cert chain, KeyUsage, RFC 3161 timestamp, and OCSP/CRL are checked; the signature math is not. | `pkg/internal/signature/verify.go:135`; `signature.go:144-154` | RFC 5652 §5.6; ISO 32000-2 §12.8.3.3               | HIGH |
| Document hash hardcodes SHA-256 regardless of the signer's declared `digestAlgorithm`; a SHA-1/384/512 signature gets a SHA-256 digest. Inert today (no comparison happens), load-bearing once the SignerInfo check above is added.                                                           | `pkg/internal/signature/signature.go:149-152`                  | ISO 32000-2 §12.8.3.3                              | HIGH |
| Cross-reference stream omits its own xref entry: the xref-stream object is allocated and serialized but never appended to the records table or the `/Index`, while `/Size` declares it exists. A reader following `/Index` cannot locate the xref stream by number.                           | `pkg/internal/core/writer/xref_writer.go:156-181`              | ISO 32000-1 §7.5.8.2                               | HIGH |
| TIFF Predictor 2 only handles `BitsPerComponent == 8` and sub-byte (1/2/4); for BPC 16 the mask truncates to `0xFF` and the bit shift goes negative, silently corrupting 16-bit-per-component image data. No error for unhandled BPC.                                                         | `pkg/internal/filter/predictor.go:213-245`                     | ISO 32000-1 §7.4.4.4; TIFF 6.0 Predictor 2         | HIGH |
| Image decode ignores the `/Decode` array: DeviceRGB/Gray/CMYK pixels are built directly from raw samples, so inverted/remapped samples (Adobe-inverted CMYK, `/Decode [1 0 ...]`, `/ImageMask`) render with wrong colour.                                                                     | `pkg/internal/graphics/image/decode.go:152-209`                | ISO 32000-1 §8.9.5.2                               | MED  |
| Real-number serializer uses fixed 6-decimal formatting, rounding any magnitude below ~`5e-7` to `0`; legitimate small reals (spec range down to ~`1.18e-38`) are lost on round-trip.                                                                                                          | `pkg/internal/core/writer/serializer.go:83-97`                 | ISO 32000-1 Annex C                                | MED  |
| Truncated/corrupt Flate and LZW streams return partial data as success with a nil error; the caller cannot distinguish a fully decoded stream from a salvaged partial one.                                                                                                                    | `pkg/internal/filter/filter.go:208-225`, `filter.go:42-46`     | No spec basis for silent partial success           | MED  |
| Skipping an unsupported filter mid-chain returns the still-encoded bytes as if decoded (e.g. `[ASCII85Decode, JBIG2Decode]` with JBIG2 unsupported hands back ASCII85-decoded but still-JBIG2-compressed bytes), with a nil error and no flag.                                                | `pkg/internal/filter/filter.go:89-101`                         | No spec basis                                      | MED  |
| ToUnicode `bfrange` increments the destination as raw UTF-16BE bytes (and NUL-pads odd-length destinations) rather than incrementing the decoded scalar value per the Adobe ToUnicode rule, producing wrong/spurious code points for non-BMP or odd-length destinations.                      | `pkg/internal/typography/cmap/cmap.go:185-201, 28-52`          | Adobe ToUnicode File Tutorial; ISO 32000-1 §9.10.3 | LOW  |
| CMap re-segmentation heuristics for mixed-width codespaces guess CJK code segmentation with no spec basis and no opt-in toggle.                                                                                                                                                               | `pkg/internal/typography/cmap/cmap.go:274-296`                 | ISO 32000-1 §9.7.6                                 | LOW  |
| `PdfStream.Bytes` documents a contradictory encryption contract (field comment "raw possibly-encrypted" vs method Godoc "after decryption"); a NewStream-built stream returns bytes as-given, so an un-wired lazy stream with a dropped `/Encrypt` could emit ciphertext.                     | `pkg/internal/core/ast/ast.go:157` vs `173-175`                | ISO 32000-2 §7.6                                   | LOW  |

## Closed — first-pass findings reclassified as not defects

Both re-read at the cited line in the second pass and found spec-correct.
Acting on the original recommendation would introduce a violation.

- **`aesCBCEncryptZeroIV` PKCS7 padding** (`pkg/internal/crypto/encrypt.go:419-435`).
  ISO 32000-2 §7.6.4.4 Algorithm 2.A computes UE/OE as AES-256-CBC **with
  no padding** over the 32-byte file key. The key is always a 16-multiple,
  so the no-padding branch is correct, and the decrypt path
  (`crypto.go:154-155`, `179-180`) expects raw 32 bytes. Adding PKCS7
  would itself violate the spec. Consider an inline citation that
  no-padding is intentional.
- **V5 password truncation asymmetry** (`pkg/internal/crypto/crypto.go:124-126`
  vs `encrypt.go:144`). Both encrypt and decrypt truncate to 127 bytes via
  `truncate(...)`; the "decrypt does not truncate" asymmetry does not exist.
  A genuinely open gap on this path is the absence of SASLprep/stringprep
  normalization before UTF-8 truncation (ISO 32000-2 §7.6.4.3.3), which is
  a different finding than the one recorded.

## Counts

First pass: HIGH 32, MEDIUM 29, LOW 16 (77).

Second pass: Theme G adds 4 HIGH, 4 MEDIUM, 3 LOW; two first-pass HIGH
crypto findings move to Closed. Revised totals: HIGH 34, MEDIUM 33,
LOW 19 (86).

## Suggested ordering

1. Theme G signature findings (G, HIGH). `VerifySignatures` currently
   reports a valid hash without verifying the signature, so callers get
   false assurance. The hardcoded-SHA-256 fix is a prerequisite for a
   correct comparison.
2. Theme G writer/filter findings (xref-stream self-entry, TIFF-16bit
   predictor, `/Decode` array). Wrong bytes in output or in decoded
   images.
3. Theme A. Remaining crypto/parser bugs producing wrong output or wrong
   defaults.
4. Theme C. Pick a single Factur-X philosophy: strict by default, opt-in
   lenient. The current "tolerate by default" stance ships verdicts that
   diverge from the spec.
5. Theme D1 (phase lets). Silent semantic gap in the Schematron engine.
6. Theme B. Decide which synthetic defaults stay vs. become
   caller-supplied.
7. Theme E / F. Annotate, then move on.

## Out of scope for this audit

- Non-spec-correctness findings from the same audit are tracked elsewhere,
  not here: a concurrent-map data race on `Document.Index.Entries` via
  lazy xref augmentation (thread-safety, not spec derivation) and several
  public-API footguns (`Encrypt` + `Sign` silently dropping encryption,
  unenforced single-use `Writer`, flat `[]error` validation results).
- VeraPDF rule descriptions copied verbatim into `pkg/internal/compliance/rules.go`
  retain their em-dashes because golden comparisons key off them.
- LICENSE text under `pkg/internal/compliance/cii_schemas/` and
  `cii_schematron/` is preserved verbatim per the upstream redistribution
  clauses.
- Plugins under `pkg/plugins/` were not audited; they wrap external C
  libraries whose conformance lives with the library, not litho.
