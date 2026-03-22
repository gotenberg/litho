# compliance_2 — VeraPDF Corpus Analysis

**Date:** 2026-03-22
**VeraPDF version:** 1.28.2
**Corpus:** 2,601 test PDFs across 13 profiles

## Results (after all phases)

| Metric | Baseline | Current | Delta |
|--------|----------|---------|-------|
| **Pass** | 1,489 (57.3%) | **1,529 (58.8%)** | +40 |
| **False Positives** | 97 | **72** | -25 |
| **False Negatives** | 1,014 | **999** | -15 |

### Initial results (before improvements)

| Metric | Value |
|--------|-------|
| **Pass** | 1,489 (57.3%) |
| **Fail** | 1,111 (42.7%) |
| **Skip** | 1 |
| **False Positives** (litho wrongly rejects pass-expected) | 97 |
| **False Negatives** (litho wrongly accepts fail-expected) | 1,014 |

## False Positives — 97

Litho reports violations on files that should pass.

### By profile

| Profile | FP |
|---------|----|
| PDF/UA-1 | 49 |
| PDF/UA-2 | 20 |
| PDF/A-4 | 13 |
| PDF/A-2b | 5 |
| PDF/A-2u | 3 |
| PDF/A-1b | 3 |
| PDF/A-1a | 1 |
| PDF/A-3b | 1 |
| PDF/A-4e | 1 |
| PDF/A-4f | 1 |

### By violation (what litho wrongly flags)

| Count | Clause | Issue |
|-------|--------|-------|
| 14 | §7.2 t29 | Lang tag validation too strict — rejects valid truncated or regional tags |
| 13 | §7.2 t34 | Natural language determination — false flag on page content with valid lang |
| 8 | §8.4.4 t2 | PDF/UA-2 lang validation too strict — same root cause as §7.2 t29 |
| 6 | §7.18.1 t2 | Annotation alt text — wrongly flags Popup annotations (should be exempt) |
| 4 | §6.2.10.2 t3 | Font BaseFont missing — flags fonts where BaseFont is present but unresolvable |
| 3 | §7.5 t1 | TH Scope — flags TH elements when Headers/IDs structure is determinable |
| 3 | §7.2 t12 | Table structure — Caption position check too strict |
| 3 | §6.10 t1 | OCG config names — misfires rule (emitting wrong violation for AS check) |
| 2 | §6.2.11.7.2 t1 | ToUnicode — misses standard encoding exemption |
| 2 | §6.2.11.2 t3 | Font BaseFont — same root cause as §6.2.10.2 t3 |
| 1 | §6.2.9 t2 | Blending CS — false flag when OutputIntent covers the space |
| 1 | §6.2.4.2 t3 | ICCBased identical to OutputIntent — identity check too aggressive |
| 1 | §6.3.8 t1 | ToUnicode (part 1) — misses CJK collection exemption |
| 1 | §6.2.11.3.1 t1 | CIDSystemInfo — supplement comparison wrong direction |
| 1 | §6.2.10.3.1 t1 | CIDSystemInfo (part 4) — same root cause |
| 1 | §6.9 t2 | Form field appearance — flags field that has valid AP |
| 1 | §6.9 t4 | OCG AS — emits wrong rule variable |
| 1 | §6.8 t3 | AFRelationship — rejects "Undefined" which may be valid |
| 1 | §6.1.2 t1 | File header — rejects valid %PDF-2.0 header |
| 1 | §6.1.3 t4 | Linearized ID — false mismatch on valid linearized file |
| 1 | §6.1.12 t4 / §6.1.13 t4 | Name length — counts encoded length wrong |
| 1 | §8.2.5.29 t1 | Formula/MathML — flags valid Formula nesting |

### Root cause groups

1. **Lang validation too strict** (35 FP) — `isValidLangTag()` rejects truncated tags, regional variants, and edge cases that RFC 3066 / BCP 47 allows.
2. **Popup annotations not exempt** (6 FP) — Popup annotations should be skipped in annotation alt-text checks.
3. **Font BaseFont resolution** (6 FP) — Flags missing BaseFont when the name is present but cannot be resolved due to encoding.
4. **Structure containment checks** (6 FP) — Caption position and TH Scope checks are too strict.
5. **ToUnicode exemptions incomplete** (3 FP) — Missing CJK collection and standard encoding exemptions.
6. **CIDSystemInfo comparison** (2 FP) — Supplement ≤ check inverted or wrong when Identity CMap used.
7. **Color/ICC edge cases** (2 FP) — Blending CS and ICCBased identity false matches.
8. **OCG rule mismatch** (4 FP) — Wrong rule variable emitted for AS vs Name check.
9. **Misc** (remaining) — Scattered one-off issues in file structure, forms, embedded files.

## False Negatives — 1,014

Litho reports compliant on files that should fail.

### By profile

| Profile | FN | % of profile |
|---------|-----|-------------|
| PDF/A-2b | 435 | 44.2% of 986 |
| PDF/A-1b | 230 | 40.4% of 569 |
| PDF/A-4 | 201 | 41.3% of 487 |
| PDF/UA-1 | 71 | 24.0% of 296 |
| PDF/UA-2 | 33 | 23.9% of 138 |
| PDF/A-2a | 12 | 44.4% of 27 |
| PDF/A-1a | 12 | 35.3% of 34 |
| PDF/A-2u | 9 | 40.9% of 22 |
| PDF/A-4e | 5 | 26.3% of 19 |
| PDF/A-3b | 4 | 33.3% of 12 |
| PDF/A-4f | 2 | 18.2% of 11 |

### By area (top 25 sections litho misses)

| Area | FN | What's missing |
|------|-----|----------------|
| PDF/A-2b §6.6 | 269 | XMP metadata: schema validation, extension schemas, property types |
| PDF/A-1b §6.7 | 188 | XMP metadata: same issues under part 1 clause numbering |
| PDF/A-4 §6.2 | 152 | Color/fonts: device CS detection, ICC validation, font encoding |
| PDF/A-2b §6.2 | 122 | Color/fonts: same issues under part 2 clause numbering |
| PDF/UA-1 | 71 | Structure: role mapping, table/list containment, heading hierarchy |
| PDF/A-2b §6.1 | 34 | File structure: stream lengths, hex strings, object format |
| PDF/UA-2 | 33 | Structure: namespaces, link nesting, formula MathML |
| PDF/A-4 §6.1 | 26 | File structure: same issues under part 4 |
| PDF/A-1b §6.2 | 19 | Color: OutputIntent, device CS coverage |
| PDF/A-1b §6.1 | 16 | File structure: limits, stream format |
| PDF/A-2b §6.5 | 10 | Annotations: forbidden types, flag validation |
| PDF/A-4 §6.6 | 10 | XMP metadata (part 4 specific) |
| PDF/A-2u §6.2 | 8 | Color/fonts |
| PDF/A-4 §6.7 | 8 | XMP metadata |
| PDF/A-1a §6.8 | 7 | Structure tree: MarkInfo, StructTreeRoot |
| PDF/A-1b §6.3 | 7 | Fonts (part 1): encoding, embedding edge cases |
| PDF/A-2a §6.7 | 6 | XMP metadata |
| PDF/A-2a §6.2 | 5 | Color/fonts |

### Root cause groups

1. **XMP metadata validation gaps** (~460 FN) — The XMP checker doesn't catch many property-level violations: wrong value types, missing required properties, incorrect extension schema structure, container type mismatches for non-core namespaces.
2. **Color space / ICC profile gaps** (~300 FN) — Device-dependent CS detection misses usage in Shading, Pattern, and deeply nested XObject resources. ICC profile N-value validation incomplete. OutputIntent coverage check doesn't recurse into all resource types.
3. **File structure / limits gaps** (~100 FN) — Stream length validation doesn't check all streams. Hex string validation in content streams incomplete. Object header format checks too lenient.
4. **PDF/UA structure gaps** (~100 FN) — Role map validation misses indirect mappings. Table/list containment checks don't cover all nesting levels. Heading hierarchy skip detection incomplete for deeply nested trees.
5. **Font validation gaps** (~30 FN) — Font encoding checks don't validate Differences arrays. Glyph width comparison is a stub (needs font program parsing). .notdef detection needs content stream analysis.
6. **Annotation/action gaps** (~20 FN) — Some forbidden annotation subtypes not checked in all profiles. Action chain walking doesn't follow all /Next links.
