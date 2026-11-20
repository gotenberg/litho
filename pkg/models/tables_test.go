package models

import "testing"

func TestTable_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, Table{
		Rows: [][]string{{"a", "b"}, {"c", "d"}},
		BBox: [4]float64{0, 0, 100, 100},
	})
}
