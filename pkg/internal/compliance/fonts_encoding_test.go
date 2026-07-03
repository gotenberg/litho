package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// TestSymbolicCmapViolation pins the part-aware symbolic TrueType cmap rule
// against the veraPDF test expressions: PDF/A-1 requires exactly one subtable;
// PDF/A-2/3 and PDF/UA-1 also accept a Microsoft Symbol (3,0); PDF/A-4 and
// PDF/UA-2 accept a (3,0) or a Mac (1,0) regardless of count.
func TestSymbolicCmapViolation(t *testing.T) {
	sub := func(p, e uint16) truetype.CmapSubtable {
		return truetype.CmapSubtable{PlatformID: p, EncodingID: e}
	}
	macAndSymbol := []truetype.CmapSubtable{sub(1, 0), sub(3, 0)} // Wingdings shape
	symbolOnly := []truetype.CmapSubtable{sub(3, 0)}
	macOnly := []truetype.CmapSubtable{sub(1, 0)}
	unicodeOnly := []truetype.CmapSubtable{sub(3, 1)}
	twoNonSymbol := []truetype.CmapSubtable{sub(3, 1), sub(0, 3)}

	cases := []struct {
		name    string
		profile string
		subs    []truetype.CmapSubtable
		want    bool
	}{
		// Wingdings (Mac + Symbol): only PDF/A-1 rejects it (count != 1).
		{"A1 mac+symbol", "PDF/A-1b", macAndSymbol, true},
		{"A2 mac+symbol", "PDF/A-2b", macAndSymbol, false},
		{"A3 mac+symbol", "PDF/A-3b", macAndSymbol, false},
		{"A4 mac+symbol", "PDF/A-4", macAndSymbol, false},
		{"UA1 mac+symbol", "PDF/UA-1", macAndSymbol, false},
		{"UA2 mac+symbol", "PDF/UA-2", macAndSymbol, false},

		// A single non-symbol Unicode subtable: accepted by the count-based
		// levels, rejected by PDF/A-4 / PDF/UA-2 which demand (3,0) or (1,0).
		{"A1 unicode-only", "PDF/A-1b", unicodeOnly, false},
		{"A2 unicode-only", "PDF/A-2b", unicodeOnly, false},
		{"A4 unicode-only", "PDF/A-4", unicodeOnly, true},
		{"UA2 unicode-only", "PDF/UA-2", unicodeOnly, true},

		// Symbol-only and Mac-only single subtables are always fine.
		{"A2 symbol-only", "PDF/A-2b", symbolOnly, false},
		{"A4 symbol-only", "PDF/A-4", symbolOnly, false},
		{"A4 mac-only", "PDF/A-4", macOnly, false},
		{"UA2 mac-only", "PDF/UA-2", macOnly, false},

		// Two subtables, neither Symbol nor Mac: rejected everywhere.
		{"A1 two-nonsymbol", "PDF/A-1b", twoNonSymbol, true},
		{"A2 two-nonsymbol", "PDF/A-2b", twoNonSymbol, true},
		{"A4 two-nonsymbol", "PDF/A-4", twoNonSymbol, true},
		{"UA1 two-nonsymbol", "PDF/UA-1", twoNonSymbol, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := symbolicCmapViolation(tc.profile, tc.subs); got != tc.want {
				t.Errorf("symbolicCmapViolation(%q) = %v, want %v", tc.profile, got, tc.want)
			}
		})
	}
}
