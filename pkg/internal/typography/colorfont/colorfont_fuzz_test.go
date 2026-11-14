package colorfont

import "testing"

// FuzzParse reads every colour glyph of an arbitrary program: Parse, then
// each glyph's bitmap at two sizes, its layers and every palette, must
// return or fail without panicking.
func FuzzParse(f *testing.F) {
	cblc, cbdt := cbTables(f, 20)
	colrData, cpalData := colrTables()
	valid := sfnt(f, 1000, 8, map[string][]byte{
		"CBLC": cblc, "CBDT": cbdt, "sbix": sbixTable(f, 40), "COLR": colrData, "CPAL": cpalData,
	})
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Fuzz(func(_ *testing.T, data []byte) {
		font, err := Parse(data)
		if err != nil {
			return
		}
		for gid := range uint16(16) {
			for _, ppem := range []float64{12, 200} {
				_, _, _ = font.Bitmap(gid, ppem) // only a panic is a finding
			}
			_, _, _ = font.Layers(gid) // only a panic is a finding
		}
		for i := range font.NumPalettes() {
			font.Palette(i)
		}
		font.PaletteForLight()
		font.PaletteForDark()
	})
}
