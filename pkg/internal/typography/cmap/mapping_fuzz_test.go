package cmap

import (
	"context"
	"testing"
)

// unicodeCMap is a ToUnicode CMap with a one-byte and a two-byte codespace and
// both a bfchar and a bfrange, so the mapper has to choose a code length rather
// than always consuming the same number of bytes.
const unicodeCMap = "/CIDInit /ProcSet findresource begin\n" +
	"12 dict begin\nbegincmap\n" +
	"2 begincodespacerange\n<00> <80>\n<8140> <9ffc>\nendcodespacerange\n" +
	"1 beginbfchar\n<41> <0041>\nendbfchar\n" +
	"1 beginbfrange\n<8140> <8150> <3000>\nendbfrange\n" +
	"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"

// cidCMap is an embedded CID CMap with the same mixed code lengths.
const cidCMap = "/CIDInit /ProcSet findresource begin\n" +
	"12 dict begin\nbegincmap\n" +
	"2 begincodespacerange\n<00> <80>\n<8140> <9ffc>\nendcodespacerange\n" +
	"1 begincidchar\n<41> 65\nendcidchar\n" +
	"1 begincidrange\n<8140> <8150> 633\nendcidrange\n" +
	"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"

// baseNotdefCMap is an embedded CID CMap that builds on a predefined base and
// declares notdef mappings, so decoding walks the base and the notdef list.
const baseNotdefCMap = "/CIDInit /ProcSet findresource begin\n" +
	"12 dict begin\nbegincmap\n" +
	"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" +
	"1 begincidchar\n<0041> 7\nendcidchar\n" +
	"/Identity-H usecmap\n" +
	"1 beginnotdefrange\n<0100> <01FF> 3\nendnotdefrange\n" +
	"1 beginnotdefchar\n<0042> 1\nendnotdefchar\n" +
	"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"

// FuzzCMapMapping feeds arbitrary strings through arbitrary CMaps and asserts
// crash-freedom.
//
// The existing targets beside this one parse a CMap and stop. Mapping is the
// other half and the half that runs per shown string: [CMap.Map] walks the
// codespace ranges to decide how many bytes the next code occupies, then looks
// the code up, and falls back to a longest-prefix search when no range matches.
// Both the CMap and the string come from the document (the CMap from a
// /ToUnicode or /Encoding stream, the string from a text-showing operator), and
// they are independent: nothing makes the string's code lengths agree with the
// ranges the CMap declares.
//
// Two inputs rather than one, because reusing the CMap source as the string
// would only ever map bytes that happen to look like PostScript.
func FuzzCMapMapping(f *testing.F) {
	f.Add([]byte(unicodeCMap), []byte("A\x81\x40"))
	f.Add([]byte(unicodeCMap), []byte{})
	f.Add([]byte(unicodeCMap), []byte{0x81})       // truncated two-byte code
	f.Add([]byte(unicodeCMap), []byte{0xFF, 0xFF}) // outside every range
	f.Add([]byte(cidCMap), []byte("A\x81\x45"))
	f.Add([]byte(baseNotdefCMap), []byte("\x00\x41\x01\x50\xFF"))
	f.Add([]byte(baseNotdefCMap[:len(baseNotdefCMap)/2]), []byte("\x00\x41")) // truncated inside a block
	f.Add([]byte("begincmap\nendcmap\n"), []byte("abc"))                      // no codespace at all
	f.Add([]byte(zeroWidthCMapSrc), []byte("AB"))                             // "<>" declares no code at all
	f.Add([]byte{}, []byte{})
	f.Add(rangeBomb(maxCMapRangeCodes>>16+1, true), []byte("\x00\x41"))
	f.Add(useCMapChain(), []byte("\x00\x41\x30\x42"))

	f.Fuzz(func(t *testing.T, cmapSrc, codes []byte) {
		if len(cmapSrc) > 1<<20 || len(codes) > 1<<16 {
			return
		}
		ctx := context.Background()

		if cm, err := Parse(ctx, cmapSrc); err == nil && cm != nil {
			// Walk the string the way a caller does. A match reports at least
			// one byte, so assert that rather than step over a zero: skipping
			// it keeps this target from hanging but also hides the very defect
			// that hangs a real caller, which is what walking the string is
			// meant to expose.
			for pos := 0; pos < len(codes); {
				n, _, ok := cm.Map(codes[pos:])
				if ok && n <= 0 {
					t.Fatalf("Map reported a match consuming %d bytes at offset %d", n, pos)
				}
				if !ok {
					pos++
					continue
				}
				pos += n
			}
		}

		if cid := ParseEmbeddedCIDMap(ctx, cmapSrc); cid != nil {
			_ = cid.Decode(codes)
			_ = cid.Codes(codes)
		}
	})
}

// TestCMapMappingSeedsMap pins that the seeds reach the mapper rather than
// failing to parse: a CMap that does not parse leaves [CMap.Map] unreached
// and the target inert.
func TestCMapMappingSeedsMap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	cm, err := Parse(ctx, []byte(unicodeCMap))
	if err != nil {
		t.Fatalf("the ToUnicode seed did not parse: %v", err)
	}
	n, got, ok := cm.Map([]byte("A"))
	if !ok || n != 1 || got != "A" {
		t.Errorf("Map(\"A\") = %d, %q, %v; want 1, \"A\", true", n, got, ok)
	}
	if n, _, ok = cm.Map([]byte{0x81, 0x40}); !ok || n != 2 {
		t.Errorf("Map of a two-byte code consumed %d bytes (ok %v), want 2", n, ok)
	}

	cid := ParseEmbeddedCIDMap(ctx, []byte(cidCMap))
	if cid == nil {
		t.Fatal("the CID seed did not parse")
	}
	if codes := cid.Codes([]byte("A")); len(codes) != 1 {
		t.Errorf("Codes(\"A\") returned %d codes, want 1", len(codes))
	}
}
