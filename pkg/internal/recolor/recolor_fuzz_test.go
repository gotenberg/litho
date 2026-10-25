package recolor

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// fuzzSession is a session over a one-page document whose resources name an
// RGB space and a graphics state that sets the black point compensation and
// the rendering intent, converting to CMYK with the naive engine.
func fuzzSession(t testing.TB) (*session, *contentRewriter) {
	t.Helper()
	doc, pages := open(t, onePage("/Resources << /ColorSpace << /CS0 /DeviceRGB /P [/Pattern /DeviceGray] >>"+
		" /ExtGState << /GS0 << /UseBlackPtComp /OFF /RI /Perceptual >> >> >>", "").bytes())
	ti, err := cmyk.info()
	if err != nil {
		t.Fatal(err)
	}
	s, err := newSession(doc, writer.NewObjAllocator(doc.HighestObjectNumber(context.Background())), ti, Options{Engine: naiveEngine{}}, &Report{})
	if err != nil {
		t.Fatal(err)
	}
	return s, s.newContentRewriter(context.Background(), pages[0].Resources)
}

func FuzzContentRewrite(f *testing.F) {
	f.Add([]byte("1 0 0 rg /CS0 cs 0 1 0 sc q 0.5 G Q /P cs 1 /X scn BI /W 1 /H 1 /CS /G /BPC 8 ID \x80 EI 0 0 1 RG"))
	f.Add([]byte("1 0 0 r"))
	f.Add([]byte("1 0 0 rg 0 0 1 RG /GS0 gs 0 0 1 1 re f /Saturation ri 0 0 1 1 re B"))
	f.Add([]byte("1 0 0 rg /GS0 g"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, cr := fuzzSession(t)
		out, changed, err := cr.rewrite(context.Background(), data)
		if err == nil && changed && len(out) == 0 {
			t.Error("a changed stream came back empty")
		}
	})
}

func FuzzMeshCopy(f *testing.F) {
	valid := []byte{0, 0, 0, 0xff, 0, 0, 0, 0x28, 0, 0, 0xff, 0, 0, 0, 0x28, 0, 0, 0xff}
	f.Add(4, valid)
	f.Add(6, valid[:7])
	f.Fuzz(func(t *testing.T, kind int, data []byte) {
		if kind < 4 || kind > 7 {
			return
		}
		s, _ := fuzzSession(t)
		p := s.planDefinition(context.Background(), ast.PdfName("DeviceRGB"), nil)
		l := meshLayout{kind: kind, coordBits: 8, compBits: 8, flagBits: 8, decode: []float64{0, 40, 0, 40, 0, 1, 0, 1, 0, 1}}
		if _, err := s.copyMesh(context.Background(), data, l, p); err != nil {
			t.Errorf("copyMesh: %v", err)
		}
	})
}
