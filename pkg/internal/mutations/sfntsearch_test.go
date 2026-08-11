package mutations

import "testing"

// TestSfntSearchParamsStayRepresentable pins that the three SFNT search fields
// never wrap.
//
// They are uint16 in the header, and searchRange is the table count rounded
// down to a power of two, times 16, so the arithmetic stops fitting past
// [maxSearchableTables]. The count comes from the source font's table
// directory, which is untrusted: a crafted font reaches 4096 tables with a
// 64 KB directory, and the multiply used to wrap. 5000 tables produced
// searchRange 0 and rangeShift 14464, a hint that points nowhere.
func TestSfntSearchParamsStayRepresentable(t *testing.T) {
	for _, numTables := range []int{-1, 0, 1, 2, 10, 4094, 4095, 4096, 5000, 65535} {
		searchRange, entrySelector, rangeShift := sfntSearchParams(numTables)

		// A power-of-two multiple of 16 is what the field means; zero is only
		// correct when there is nothing to search.
		if numTables > 0 && searchRange == 0 {
			t.Errorf("numTables=%d: searchRange wrapped to 0", numTables)
		}
		if searchRange%16 != 0 {
			t.Errorf("numTables=%d: searchRange=%d is not a multiple of 16", numTables, searchRange)
		}
		// rangeShift is defined as numTables*16 - searchRange, so it can never
		// exceed searchRange once both are clamped to the same ceiling.
		if numTables > 0 && rangeShift > searchRange {
			t.Errorf("numTables=%d: rangeShift=%d exceeds searchRange=%d (the subtraction underflowed)",
				numTables, rangeShift, searchRange)
		}
		if entrySelector > 15 {
			t.Errorf("numTables=%d: entrySelector=%d, want at most 15", numTables, entrySelector)
		}
	}
}

// TestSfntSearchParamsMonotonic checks the hints still grow with the table
// count below the ceiling, so clamping did not flatten ordinary fonts. Real
// fonts carry on the order of ten tables.
func TestSfntSearchParamsMonotonic(t *testing.T) {
	prev := uint16(0)
	for _, numTables := range []int{1, 2, 4, 8, 16, 64, 256, 1024, 4095} {
		searchRange, _, _ := sfntSearchParams(numTables)
		if searchRange <= prev {
			t.Errorf("numTables=%d: searchRange=%d did not grow past %d", numTables, searchRange, prev)
		}
		prev = searchRange
	}
}
