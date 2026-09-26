package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestColourOffStreamLooksUpNoColourName pins that a content stream whose
// colour operators are off looks up none of the names cs, CS and sh give, nor
// the pattern scn selects, for ISO 19005-2/4, 6.2.2 t2 and ISO 19005-4, 6.2.2
// t3 (see [checkContentStreamResourcesOps]). Every count was measured with
// veraPDF 1.30 at PDF/A-2b and PDF/A-4 on the same fixture.
func TestColourOffStreamLooksUpNoColourName(t *testing.T) {
	t.Parallel()

	const res = "/Font << /F1 5 0 R >> /ColorSpace << /CS0 [/CalRGB << /WhitePoint [0.9505 1 1.089] >>] >> " +
		"/Shading << /Sh0 7 0 R >> /Pattern << /P0 8 0 R >>"
	shading := "<< /ShadingType 2 /ColorSpace [/CalRGB << /WhitePoint [0.9505 1 1.089] >>] /Coords [0 0 1 0] " +
		"/Function << /FunctionType 2 /Domain [0 1] /C0 [0 0 0] /C1 [1 1 1] /N 1 >> >>"
	pattern := namesTiling(1, "<< >>", "0 0 5 5 re f")
	glyph := func(body string) []byte {
		return defaultSpacePDF("", res, "BT /F1 10 Tf (a) Tj ET", namesType3(6, "<< >>"), defaultStream("", body), shading, pattern)
	}
	inherited := func(want int) []namesCount {
		return []namesCount{{models.PDFA2b, "6.2.2", 2, want}, {models.PDFA4, "6.2.2", 2, want}}
	}
	undefined := func(want int) []namesCount { return []namesCount{{models.PDFA4, "6.2.2", 3, want}} }
	const d1 = "10 0 0 0 10 10 d1 "
	runNamesCases(t, []struct {
		name   string
		data   []byte
		counts []namesCount
	}{
		{
			name:   "a cs past a d1 naming an entry only the page defines",
			data:   glyph(d1 + "/CS0 cs " + namesFill),
			counts: inherited(0),
		},
		{
			name:   "an sh past a d1 naming a shading only the page defines",
			data:   glyph(d1 + "/Sh0 sh"),
			counts: inherited(0),
		},
		{
			name:   "an scn past a d1 selecting a pattern only the page defines",
			data:   glyph(d1 + "/Pattern cs /P0 scn " + namesFill),
			counts: inherited(0),
		},
		{
			name:   "a cs, a CS, an sh and an scn past a d1 naming nothing defined",
			data:   glyph(d1 + "/CS9 cs /CS9 CS /Sh9 sh /Pattern cs /P9 scn " + namesFill),
			counts: undefined(0),
		},
		{
			name:   "a cs before a d0 naming an entry only the page defines",
			data:   glyph("10 0 d0 /CS0 cs " + namesFill),
			counts: inherited(1),
		},
		{
			name:   "a cs past the Q closing a d1",
			data:   glyph("q " + d1 + "Q /CS0 cs " + namesFill),
			counts: inherited(1),
		},
		{
			name: "a cs in the cell of an uncoloured pattern",
			data: defaultSpacePDF("", "/Pattern << /P0 5 0 R >> /ColorSpace << /CS0 [/CalRGB << /WhitePoint [0.9505 1 1.089] >>] /PS [/Pattern /DeviceGray] >>",
				"/PS cs 0.5 /P0 scn "+namesFill, namesTiling(2, "<< >>", "/CS0 cs 1 0 0 sc 0 0 5 5 re f")),
			counts: inherited(0),
		},
		{
			name: "a cs in the cell of a coloured pattern",
			data: defaultSpacePDF("", "/Pattern << /P0 5 0 R >> /ColorSpace << /CS0 [/CalRGB << /WhitePoint [0.9505 1 1.089] >>] >>",
				"/Pattern cs /P0 scn "+namesFill, namesTiling(1, "<< >>", "/CS0 cs 1 0 0 sc 0 0 5 5 re f")),
			counts: inherited(1),
		},
	})
}
