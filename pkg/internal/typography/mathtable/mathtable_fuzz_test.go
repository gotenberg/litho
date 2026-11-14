package mathtable

import "testing"

// FuzzParseTable reads every part of an arbitrary MATH table: ParseTable,
// then each constant, and the glyph information and constructions of the
// first glyphs at a few heights, must return or fail without panicking.
func FuzzParseTable(f *testing.F) {
	valid := buildTable()
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Fuzz(func(_ *testing.T, data []byte) {
		tab, err := ParseTable(data)
		if err != nil {
			return
		}
		for c := range numConstants + 1 {
			tab.Constant(c)
		}
		for gid := range uint16(48) {
			tab.ItalicsCorrection(gid)
			tab.TopAccentAttachment(gid)
			tab.IsExtendedShape(gid)
			for corner := range BottomLeft + 2 {
				for _, h := range []int{-1000, 0, 300, 1 << 20} {
					tab.Kern(gid, corner, h)
				}
			}
			tab.Construction(gid, true)
			tab.Construction(gid, false)
		}
		tab.MinConnectorOverlap()
	})
}
