package xsd

import "testing"

// orderedBases lists every built-in [compareRange] orders, one per
// comparator family and date/time kind.
var orderedBases = []string{
	"decimal", "int", "float", "double", "duration", "dateTime", "time",
	"date", "gYearMonth", "gYear", "gMonthDay", "gDay", "gMonth",
}

// FuzzCompareRange asserts the range comparators never panic on a crafted
// value and stay antisymmetric: swapping the operands mirrors the order.
// The value is instance text, as attacker-controlled as the payload it
// comes from, and the parsers index into it by hand.
func FuzzCompareRange(f *testing.F) {
	f.Add(uint8(0), "1.5", "2")
	f.Add(uint8(2), "NaN", "-0")
	f.Add(uint8(4), "-P1Y2M3DT4H5M6.7S", "P400D")
	f.Add(uint8(5), "2000-01-01T24:00:00", "1999-12-31T23:00:00Z")
	f.Add(uint8(6), "24:00:00+14:00", "00:00:00")
	f.Add(uint8(7), "-0001-02-29-05:00", "2000-01-01")
	f.Add(uint8(7), "2000-01-0", "2000-01-01") // Truncated.
	f.Add(uint8(9), "1234567890", "-2000Z")
	f.Add(uint8(12), "--1", "--12")

	f.Fuzz(func(t *testing.T, pick uint8, a, b string) {
		base := builtin(orderedBases[int(pick)%len(orderedBases)])
		ab, statusAB := compareRange(a, b, base)
		ba, statusBA := compareRange(b, a, base)
		if statusAB != rangeCompared || statusBA != rangeCompared {
			return
		}
		mirror := map[order]order{
			orderLess: orderGreater, orderGreater: orderLess,
			orderEqual: orderEqual, orderIncomparable: orderIncomparable,
		}
		if mirror[ab] != ba {
			t.Errorf("xs:%s: %q vs %q is %d, but %q vs %q is %d", base.Name.Local, a, b, ab, b, a, ba)
		}
	})
}
