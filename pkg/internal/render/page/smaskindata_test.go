package page

import (
	"os"
	"testing"
)

// smaskInDataFixture draws pkg/pdf's rgba8.jp2 (96 x 64, opacity 0 left of
// column 24 and 255 from column 72) at its size at the top left of the page,
// over a red page, under the image entries given, through transfer gs when
// set.
func smaskInDataFixture(t *testing.T, entries, gs string) fixture {
	t.Helper()
	data, err := os.ReadFile("../../../pdf/testdata/jpx/rgba8.jp2")
	if err != nil {
		t.Fatal(err)
	}
	return fixture{
		page:    "/Resources << /XObject << /Im 5 0 R >> /ExtGState << /Inv << /TR 6 0 R >> >> >>",
		content: "1 0 0 rg 0 0 100 100 re f q " + gs + " 96 0 0 64 0 36 cm /Im Do Q",
		objs: []string{
			stream("/Type /XObject /Subtype /Image /Width 96 /Height 64 /Filter /JPXDecode "+entries, string(data)),
			invert,
		},
	}
}

// TestImageSMaskInData pins that the renderer composites the soft mask a
// JPXDecode image carries in its data (ISO 32000-1, Table 89 /SMaskInData):
// the page shows through where the opacity is 0, and the image paints where it
// is 255. Device row 50 is image row 50.
func TestImageSMaskInData(t *testing.T) {
	t.Parallel()
	// Image pixel (85, 50): 85*255/95, 50*255/63, 135*37 mod 256.
	opaque := rgb{228, 202, 131}
	img, _ := draw(t, smaskInDataFixture(t, "/SMaskInData 1", ""))
	expect(t, img, 6, map[[2]int]rgb{{5, 50}: red, {85, 50}: opaque, {98, 50}: red})

	// Without the entry the data's opacity is ignored (Table 89, value 0):
	// the transparent columns paint their colour.
	img, _ = draw(t, smaskInDataFixture(t, "", ""))
	expect(t, img, 6, map[[2]int]rgb{{5, 50}: {13, 202, 243}, {85, 50}: opaque})

	// A transfer function maps the colour, not the opacity.
	img, _ = draw(t, smaskInDataFixture(t, "/SMaskInData 1", "/Inv gs"))
	expect(t, img, 6, map[[2]int]rgb{{5, 50}: red, {85, 50}: {255 - 228, 255 - 202, 255 - 131}})
}
