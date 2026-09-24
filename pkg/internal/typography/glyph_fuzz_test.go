package typography

import (
	"context"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
	"github.com/gotenberg/litho/pkg/models"
)

// The glyph decode path had NO fuzz coverage, measured rather than assumed: a
// coverage profile over this package's existing targets reported 0 of 19 blocks
// in processText, 0 of 22 in decodeNextChar and 0 of 8 in resolveGlyphFallback.
// FuzzExtractFontProgram and FuzzReadUnitsPerEm both read a font PROGRAM and
// never reach the code that walks a shown string.
//
// That gap hid a real denial of service. A /ToUnicode CMap declaring a
// zero-width "<>" codespace made [cmap.CMap.Map] report a match consuming no
// bytes, decodeNextChar returned it through the one path that skips its own
// n == 0 floor, and processText advanced by zero forever: 600,499 elements
// appended in 100ms from a two-byte string, reachable from Reader.ExtractText.
//
// A package-level coverage census cannot see this. cmap's own FuzzCMapMapping
// drives the parser to 100% of cmap.go, and the typography package "has fuzz
// targets", yet the consumer between them was untouched.

// wellFormedToUnicode is a one-byte codespace with a single bfchar, so the
// mapper resolves rather than falling through every branch.
const wellFormedToUnicode = "begincmap\n" +
	"1 begincodespacerange\n<00> <FF>\nendcodespacerange\n" +
	"1 beginbfchar\n<41> <0041>\nendbfchar\n" +
	"endcmap\n"

// twoByteToUnicode declares a two-byte codespace so Map reports n == 2. The
// Type0 branches in [decodeNextChar] and the two-byte charCode in [processText] are
// reachable only through a CMap of this shape, and no mutation of a one-byte
// codespace produces one.
const twoByteToUnicode = "begincmap\n" +
	"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
	"1 beginbfchar\n<0041> <0041>\nendbfchar\n" +
	"endcmap\n"

// zeroWidthCMapSrcForFuzz is the shape that hung [ExtractText]. "<>" is a
// syntactically valid empty hex string, so the CMap parses; before the parse
// guard landed it produced a codespace of width zero and a mapping keyed by the
// empty string, which together made Map report a match consuming no bytes.
// Seeding it keeps a regression in that guard inside this target.
//
// # Corpus evidence
//
//   - All corpora: no corpus file carries such a CMap, so the corpus cannot
//     catch a regression in the guard.
const zeroWidthCMapSrcForFuzz = "begincmap\n" +
	"1 begincodespacerange\n<> <>\nendcodespacerange\n" +
	"1 beginbfchar\n<> <0041>\nendbfchar\n" +
	"endcmap\n"

// fuzzFont builds the font [decodeNextChar] consults. Every field it branches on
// comes from the fuzzer, and the two optional maps are behind flags rather than
// always present: LigatureOverrides is consulted FIRST, so setting it
// unconditionally hides the Encoding and fallback paths behind it, and a
// non-nil GIDToUnicode with CIDWidths forces [resolveGlyphFallback] down its GID
// branch and hides the CIDIdentityFallback one. Gating each keeps both sides
// of those decisions reachable.
func fuzzFont(cm *cmap.CMap, subtype string, code byte, r rune, cidFallback, suppress, withLigature, withGIDTable bool, cidOffset int) *Font {
	f := &Font{
		Subtype:             subtype,
		ToUnicode:           cm,
		Encoding:            map[byte]rune{code: r},
		CIDIdentityFallback: cidFallback,
		CIDOffset:           cidOffset,
		SuppressExtraction:  suppress,
		UnitsPerEm:          1000,
	}
	if withLigature {
		f.LigatureOverrides = map[byte]string{code: string(r)}
	}
	if withGIDTable {
		// resolveGlyphFallback takes its GID branch only when BOTH are
		// non-nil, which is how an embedded CIDFont presents.
		f.GIDToUnicode = map[uint16]rune{uint16(code): r}
		f.CIDWidths = map[int]float64{int(code): 500}
	}
	return f
}

// FuzzDecodeNextChar asserts the byte count [decodeNextChar] reports is always a
// usable stride. [processText] advances the string by it, so n < 1 loops forever
// and n > len(data) slices past the end: both are the caller's crash, not this
// function's, which is why the invariant is asserted here rather than left to
// the loop to discover.
func FuzzDecodeNextChar(f *testing.F) {
	f.Add([]byte(wellFormedToUnicode), "Type1", byte('A'), 'A', false, false, true, false, 0, []byte("AB"))
	f.Add([]byte(twoByteToUnicode), "Type0", byte(0), 'A', false, false, false, false, 0, []byte{0x00, 0x41, 0x00})
	f.Add([]byte(twoByteToUnicode), "Type0", byte(0), 'A', false, false, false, false, 29, []byte{0xFF, 0xFE})
	f.Add([]byte(zeroWidthCMapSrcForFuzz), "Type1", byte('A'), 'A', false, false, false, false, 0, []byte("AB"))
	f.Add([]byte(""), "Type0", byte(0), rune(0), true, false, false, false, 29, []byte{0x00, 0x41})
	f.Add([]byte(""), "Type0", byte(0), rune(0), false, false, false, true, 0, []byte{0x00, 0x41})
	f.Add([]byte(""), "Type0", byte(0), rune(0), false, true, false, false, 0, []byte{0xFF})
	f.Add([]byte("begincmap\nendcmap\n"), "TrueType", byte('Z'), 'Z', false, false, false, false, 0, []byte("Z"))

	f.Fuzz(func(t *testing.T, cmapSrc []byte, subtype string, code byte, r rune, cidFallback, suppress, withLigature, withGIDTable bool, cidOffset int, data []byte) {
		if len(cmapSrc) > 1<<20 || len(data) > 1<<16 {
			return
		}
		// decodeNextChar indexes data[0]; processText's loop condition
		// guarantees a non-empty string, so an empty one is not a case the
		// function is required to survive.
		if len(data) == 0 {
			return
		}

		font := fuzzFont(parseFuzzCMap(cmapSrc), subtype, code, r, cidFallback, suppress, withLigature, withGIDTable, cidOffset)

		_, n, _ := decodeNextChar(font, data)
		if n < 1 {
			t.Fatalf("decodeNextChar reported n=%d for %d bytes: processText would not advance", n, len(data))
		}
		if n > len(data) {
			t.Fatalf("decodeNextChar reported n=%d past the %d bytes available: data[n:] would panic", n, len(data))
		}
	})
}

// FuzzProcessText asserts the glyph loop terminates. The context carries a
// deadline because a stalled loop would otherwise hang the fuzz worker with no
// reproducer (go test -timeout does not govern the fuzzing loop), and the
// deadline firing is treated as the FAILURE rather than as a tolerated result:
// letting it return quietly would reproduce the blindness that kept the
// original hang out of the cmap mapper's own fuzz target for so long.
//
// The font is fuzzed here too, not pinned. Holding it at a simple Type1 leaves
// the skip path, the two-byte charCode and every Type0 branch unreachable, so a
// pinned font would report a healthy target that never enters most of the loop.
func FuzzProcessText(f *testing.F) {
	f.Add([]byte(wellFormedToUnicode), "Type1", byte('A'), 'A', false, false, false, false, 0, []byte("AB"))
	f.Add([]byte(twoByteToUnicode), "Type0", byte(0), 'A', false, false, false, false, 0, []byte{0x00, 0x41, 0x00, 0x42})
	f.Add([]byte(zeroWidthCMapSrcForFuzz), "Type1", byte('A'), 'A', false, false, false, false, 0, []byte("AB")) // The shape that hung.
	f.Add([]byte(""), "Type0", byte(0), rune(0), false, true, false, false, 0, []byte{0xFF, 0xFE})               // Suppressed: the skip path.
	f.Add([]byte(""), "Type0", byte(0), rune(0), true, false, false, true, 29, []byte{0x00, 0x41})
	f.Add([]byte("begincmap\nendcmap\n"), "Type1", byte('h'), 'h', false, false, true, false, 0, []byte("hello"))
	f.Add([]byte(wellFormedToUnicode), "Type1", byte('A'), 'A', false, false, false, false, 0, []byte{})

	f.Fuzz(func(t *testing.T, cmapSrc []byte, subtype string, code byte, r rune, cidFallback, suppress, withLigature, withGIDTable bool, cidOffset int, data []byte) {
		if len(cmapSrc) > 1<<20 {
			return
		}
		// A shown string is bounded by the content stream that carries it, and
		// processText appends one element per code, so this bounds the work per
		// execution rather than hiding a stall behind a smaller input.
		if len(data) > 4096 {
			return
		}

		font := fuzzFont(parseFuzzCMap(cmapSrc), subtype, code, r, cidFallback, suppress, withLigature, withGIDTable, cidOffset)
		machine := graphics.NewMachine([4]float64{0, 0, 612, 792})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var elements []models.TextElement
		if err := processText(ctx, ast.PdfString(data), font, machine, &elements); err != nil {
			t.Fatalf("processText did not finish %d bytes before the deadline (%v): the loop consumed no bytes", len(data), err)
		}
	})
}

// parseFuzzCMap returns the CMap the fuzzed bytes describe, or nil when they
// describe none. A nil ToUnicode is a real font shape, not a skipped case.
func parseFuzzCMap(src []byte) *cmap.CMap {
	if len(src) == 0 {
		return nil
	}
	parsed, err := cmap.Parse(context.Background(), src)
	if err != nil {
		return nil
	}
	return parsed
}

// TestGlyphFuzzSeedsReachTheLoop pins that the seeds exercise the decode path
// instead of returning early. A target whose seeds all bail is inert, and it
// would report the same clean result whether or not the code works.
func TestGlyphFuzzSeedsReachTheLoop(t *testing.T) {
	t.Parallel()

	cm, err := cmap.Parse(context.Background(), []byte(wellFormedToUnicode))
	if err != nil {
		t.Fatalf("the well-formed seed must parse, else every exec runs without a CMap: %v", err)
	}
	font := fuzzFont(cm, "Type1", 'A', 'A', false, false, false, false, 0)

	char, n, skip := decodeNextChar(font, []byte("AB"))
	if skip || n != 1 || char != "A" {
		t.Fatalf("decodeNextChar = (%q, %d, %v); want (\"A\", 1, false)", char, n, skip)
	}

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	var elements []models.TextElement
	if err := processText(context.Background(), ast.PdfString("AB"), font, machine, &elements); err != nil {
		t.Fatalf("processText: %v", err)
	}
	if len(elements) != 2 {
		t.Fatalf("elements = %d, want 2: the seed must reach the element append", len(elements))
	}
}

// TestTwoByteSeedReachesTheType0Paths pins the seed that exists solely to make
// Map report n == 2. Without it the Type0 branches in [decodeNextChar] and the
// two-byte charCode in [processText] are unreachable, and the target would look
// healthy while never entering them.
func TestTwoByteSeedReachesTheType0Paths(t *testing.T) {
	t.Parallel()

	cm, err := cmap.Parse(context.Background(), []byte(twoByteToUnicode))
	if err != nil {
		t.Fatalf("the two-byte seed must parse: %v", err)
	}
	if n, _, ok := cm.Map([]byte{0x00, 0x41}); !ok || n != 2 {
		t.Fatalf("Map of a two-byte code = (%d, ok=%v); want (2, true): the Type0 paths stay unreachable", n, ok)
	}
	// An unmapped code inside the two-byte codespace is what drives the
	// n > 1 unmapped branch decodeNextChar takes for Identity-H fonts.
	if n, _, ok := cm.Map([]byte{0xFF, 0xFE}); ok || n != 2 {
		t.Fatalf("Map of an unmapped two-byte code = (%d, ok=%v); want (2, false)", n, ok)
	}
}
