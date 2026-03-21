# Implementation Plan: JBIG2Decode & JPXDecode (JPEG2000)

## Problem Statement

JBIG2Decode and JPXDecode filters are not implemented in litho. Any PDF stream using
these filters triggers `ErrUnsupportedFilter`, breaking image extraction, compliance
validation, and causing all JBIG2/JPX corpus tests to be skipped.

These are the last two major missing filters in the pipeline.

## Research Summary

### How Others Handle It

| | **JBIG2Decode** | **JPXDecode (JPEG2000)** |
|---|---|---|
| **pdf.js** | Pure JS (~2K lines, ITU-T T.88) + WASM accelerator | OpenJPEG→WASM (gave up on pure JS) |
| **pdf_oxide** | Stub — returns compressed bytes unchanged | Not implemented |
| **pdfcpu** | Stub constant, no decode | Stub constant, no decode |

### Go Libraries Available

| Library | Format | Type | License | Usable? |
|---|---|---|---|---|
| unidoc/unipdf | JBIG2 | Pure Go | AGPL/Commercial | No — `internal/`, AGPL |
| Apache PDFBox JBIG2 | JBIG2 | Java | **Apache-2.0** | Reference for Go port |
| segmed/openjpeg | JPEG2000 | CGo | BSD-ish | Possible, low activity |
| linxlib/openjpeg | JPEG2000 | CGo | Unknown | Possible |

**No usable pure Go library exists for either format.**

---

## Phase 1: JBIG2Decode (Pure Go)

### Approach

Port from [apache/pdfbox-jbig2](https://github.com/apache/pdfbox-jbig2) (Apache-2.0).
The Java code is clean, well-documented, and maps naturally to Go.

### Spec

ITU-T T.88 / ISO/IEC 14492

### Key Components

1. **Arithmetic decoder** (QM coder) — core entropy decoder
2. **Huffman decoder** — standard tables B.1–B.15 + custom tables
3. **MMR decoder** — Modified Modified READ (reuses CCITT Group 4 logic)
4. **Segment parser** — symbol dictionaries, text regions, halftone regions,
   generic regions, refinement regions, page info, end-of-page/stripe/file
5. **Bitmap** — 2D bitmap with OR/AND/XOR/XNOR composition operators
6. **Global segments** — `/JBIG2Globals` stream from `/DecodeParms`

### Estimated Size

~5,000–8,000 lines of Go

### File Structure

```
pkg/internal/filter/jbig2/
├── jbig2.go              # Public Decode() entry point
├── arithmetic.go         # Arithmetic (MQ/QM) decoder
├── huffman.go            # Huffman decoder + standard tables
├── mmr.go                # MMR (CCITT Group 4 variant) decoder
├── bitmap.go             # 2D bitmap type + composition ops
├── segment.go            # Segment header parser + dispatcher
├── region_generic.go     # Generic region decoding (Type 36/38/39)
├── region_text.go        # Text region decoding (Type 4/6/7)
├── region_halftone.go    # Halftone region decoding (Type 16/20/22/23)
├── region_refinement.go  # Refinement region decoding (Type 40/42/43)
├── symbol_dictionary.go  # Symbol dictionary segment (Type 0)
├── pattern_dictionary.go # Pattern dictionary segment (Type 16)
├── page.go               # Page information + result assembly
├── errors.go             # Sentinel errors
└── *_test.go             # Tests for each component
```

### Integration Point

In `pkg/internal/filter/filter.go`, add:

```go
case "JBIG2Decode":
    return decodeJBIG2(data, parms)
```

The `decodeJBIG2` function must:
1. Check `/DecodeParms` for `/JBIG2Globals` (a stream with shared symbol dictionaries)
2. If globals exist, decode the globals stream first
3. Decode the main segment data
4. Return the page bitmap as raw pixel bytes

### Testing Strategy

- Unit tests per component (arithmetic decoder, huffman, bitmap ops, each region type)
- Integration test with real JBIG2-compressed PDF streams from the corpus
- Remove JBIG2 skip entries from `pdfjs_corpus_test.go`

---

## Phase 2: JPXDecode (JPEG2000)

### Approach

**Option A (recommended)**: Pure Go port from OpenJPEG (BSD-2-Clause) — Part 1 baseline only.

**Option B (fallback)**: CGo bindings to OpenJPEG behind a build tag
(`//go:build cgo && openjpeg`) so litho stays usable without it.

### Spec

ISO/IEC 15444-1 (JPEG 2000 Part 1)

### Key Components

1. **JP2 container parser** — box structure
2. **Codestream parser** — SIZ, COD, QCD, SOT, SOD markers
3. **Tier-1 decoder** — MQ arithmetic coder + bit-plane decoding
4. **Tier-2 decoder** — packet parsing, code-block ordering
5. **DWT** — Discrete Wavelet Transform (5/3 reversible + 9/7 irreversible)
6. **MCT** — Multi-Component Transform (RGB ↔ YCbCr)

### Estimated Size

~15,000–25,000 lines of Go (decoder path only)

### Complexity Warning

JPEG2000 is the most complex image codec in common use. pdf.js abandoned their
pure JS implementation in favor of WASM OpenJPEG. A pure Go port is a major
undertaking (2–4 months estimate).

### Lightweight Alternative

If litho only needs structural validation (not pixel decoding), a JP2 header/marker
parser (~500 lines) could cover PDF/A compliance checks without the full codec.

---

## Execution Order

1. **JBIG2Decode** — Start here. More commonly encountered, tractable complexity,
   clean Apache-2.0 reference.
2. **JPXDecode** — Evaluate after JBIG2 ships. Decide between full codec vs.
   structural validation based on real-world needs.
