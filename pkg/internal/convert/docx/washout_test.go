package docx

import (
	imgcolor "image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/layout"
)

// TestWashout checks Word's washout of a picture, written as DrawingML lum
// bright 70% and contrast -70% or as VML gain 0.3 and blacklevel 0.35: a
// black pixel prints 205 of 255.
func TestWashout(t *testing.T) {
	t.Parallel()
	dml := `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="127000" cy="127000"/><wp:docPr id="1" name="p"/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic>` +
		`<pic:nvPicPr><pic:cNvPr id="1" name="a.png"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="rIda"><a:lum bright="70000" contrast="-70000"/></a:blip></pic:blipFill>` +
		`<pic:spPr/></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
	vml := `<w:p><w:r><w:pict><v:shape style="width:10pt;height:10pt"><v:imagedata r:id="rIda" gain="19661f" blacklevel="22938f"/></v:shape></w:pict></w:r></w:p>`
	for name, body := range map[string]string{"lum": dml, "gain": vml} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pics := find(build(t, body, map[string]string{"word/media/a.png": pngData(t, 2, 2)}).root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
			if len(pics) != 1 || pics[0].Picture.Source == nil {
				t.Fatalf("%d pictures", len(pics))
			}
			img, err := pics[0].Picture.Source.Decode(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if c := imgcolor.NRGBAModel.Convert(img.At(0, 0)).(imgcolor.NRGBA); c.R != 205 || c.G != 205 || c.B != 205 {
				t.Errorf("black prints %v, want 205", c)
			}
		})
	}
}
