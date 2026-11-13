package docx

import (
	"context"
	"image"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// effectImages returns the images of doc's first page in paint order, with
// the transform of the groups they sit in.
func effectImages(doc *typeset.Document) []*typeset.Image {
	var out []*typeset.Image
	var walk func(frags []typeset.Fragment)
	walk = func(frags []typeset.Fragment) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Image:
				out = append(out, v)
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(doc.Pages[0].Content)
	return out
}

// alphaColumn returns the soft mask of img down the column at page x, each
// sample with its page y.
func alphaColumn(t *testing.T, img *typeset.Image, x float64) (ys, as []float64) {
	t.Helper()
	pix, err := img.Source.Decode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n, ok := pix.(*image.NRGBA)
	if !ok {
		t.Fatalf("mask decodes to %T", pix)
	}
	k := float64(img.Source.Width) / img.Rect.Width
	col := int((x - img.Rect.X) * k)
	for y := range img.Source.Height {
		ys = append(ys, img.Rect.Y+(float64(y)+0.5)/k)
		as = append(as, float64(n.NRGBAAt(col, y).A))
	}
	return ys, as
}

// fitEdge fits a rising Gaussian edge, the normal distribution's integral
// scaled to the samples' largest value, to the samples (pos, a) whose pos
// lies in [lo, hi]: the position of its half and its deviation.
func fitEdge(pos, a []float64, lo, hi float64) (edge, sigma float64) {
	peak := 0.0
	for i, p := range pos {
		if p >= lo && p <= hi {
			peak = max(peak, a[i])
		}
	}
	best := math.Inf(1)
	for e := lo; e <= hi; e += 0.02 {
		for s := 0.05; s <= 6; s += 0.01 {
			sum := 0.0
			for i, p := range pos {
				if p < lo || p > hi {
					continue
				}
				d := 0.5*(1+math.Erf((p-e)/s/math.Sqrt2)) - a[i]/peak
				sum += d * d
			}
			if sum < best {
				best, edge, sigma = sum, e, s
			}
		}
	}
	return edge, sigma
}

// reverse returns the samples in the opposite order, positions negated,
// so a falling edge fits as a rising one.
func reverse(pos, a []float64) ([]float64, []float64) {
	p, b := make([]float64, len(pos)), make([]float64, len(a))
	for i := range pos {
		p[len(pos)-1-i], b[len(a)-1-i] = -pos[i], a[i]
	}
	return p, b
}

// runDoc returns a document of one 24 point run "X" with the w14 effects
// fx.
func runDoc(t *testing.T, fx string) *typeset.Document {
	t.Helper()
	body := `<w:p><w:r><w:rPr><w:rFonts w:ascii="Test Sans" w:hAnsi="Test Sans"/><w:sz w:val="48"/>` + fx + `</w:rPr><w:t>X</w:t></w:r></w:p>`
	body = strings.Replace(body, "<w:rPr>", `<w:rPr xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml">`, 1)
	return convertBytes(t, docx(t, body, nil), office.Options{})
}

// firstText returns the first text run of doc's first page that draws a
// glyph, the groups it sits in holding the identity transform.
func firstText(t *testing.T, doc *typeset.Document) *typeset.Text {
	t.Helper()
	var found *typeset.Text
	var walk func(frags []typeset.Fragment)
	walk = func(frags []typeset.Fragment) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Text:
				if found == nil && len(v.Glyphs) > 0 {
					found = v
				}
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(doc.Pages[0].Content)
	if found == nil {
		t.Fatal("no text")
	}
	return found
}

// glyphTop returns the page y of the top of the 24 point Test Sans glyph
// of doc's first text run, half an em above its baseline.
func glyphTop(t *testing.T, doc *typeset.Document) (x, top float64) {
	t.Helper()
	tx := firstText(t, doc)
	return tx.Origin.X + tx.Glyphs[0].X + tx.Size/4, tx.Origin.Y - tx.Size/2
}

// TestRunEffectsFollowWord converts one run with one w14 effect and fits
// the edge of the effect's soft mask above the glyph against the edges
// fitted on Word's text effects, within the spread of Word's samples.
//
// # Corpus evidence
//
//   - litho/docx run-w14-effects: Word's 8 point glow of 24 point glyphs
//     has its half 1.37 points out of the glyph and a Gaussian of 0.64
//     points; its 4 point shadow a Gaussian of 0.79 points.
//   - libreoffice/docx ooxmlexport/TextEffects_Glow_Shadow_Reflection:
//     the 5 and 18 point glows of 11 point glyphs have their halves 0.45
//     and 2.35 points out, Gaussians of 0.25 and 0.83 points.
func TestRunEffectsFollowWord(t *testing.T) {
	for _, tc := range []struct {
		name, fx   string
		out, sigma float64
	}{
		{"glow", `<w14:glow w14:rad="101600"><w14:srgbClr w14:val="ED7D31"><w14:alpha w14:val="60000"/></w14:srgbClr></w14:glow>`, 1.37, 0.64},
		{"shadow", `<w14:shadow w14:blurRad="50800" w14:dist="0" w14:dir="0" w14:algn="ctr"><w14:srgbClr w14:val="000000"><w14:alpha w14:val="60000"/></w14:srgbClr></w14:shadow>`, 0, 0.79},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := runDoc(t, tc.fx)
			imgs := effectImages(doc)
			if len(imgs) != 1 {
				t.Fatalf("%d images, want 1", len(imgs))
			}
			if imgs[0].Parent != nil {
				t.Errorf("effect image tagged %q, want an artifact", imgs[0].Parent.Role)
			}
			if !strings.Contains(docText(doc), "X") {
				t.Errorf("text %q, want the run's text", docText(doc))
			}
			x, top := glyphTop(t, doc)
			ys, as := alphaColumn(t, imgs[0], x)
			edge, sigma := fitEdge(ys, as, top-tc.out-4*tc.sigma, top-tc.out+4*tc.sigma)
			if got := top - edge; math.Abs(got-tc.out) > 0.35 {
				t.Errorf("half %.2f points out, want %.2f within 0.35", got, tc.out)
			}
			if math.Abs(sigma-tc.sigma) > 0.25 {
				t.Errorf("deviation %.2f points, want %.2f within 0.25", sigma, tc.sigma)
			}
		})
	}
}

// TestRunReflectionStartsAtBaseline checks that a run's reflection hangs
// from its baseline and fades to nothing at its end position of the
// font's height.
//
// # Corpus evidence
//
//   - litho/docx run-w14-effects: Word's reflection of a 24 point run
//     starts 0.6 point above the baseline at the start opacity and fades
//     to its end over 45.5% of the font's 24 points.
func TestRunReflectionStartsAtBaseline(t *testing.T) {
	doc := runDoc(t, `<w14:reflection w14:blurRad="6350" w14:stA="55000" w14:stPos="0" w14:endA="300" w14:endPos="45500" w14:dist="0" w14:dir="5400000" w14:fadeDir="5400000" w14:sx="100000" w14:sy="-100000" w14:kx="0" w14:ky="0" w14:algn="bl"/>`)
	imgs := effectImages(doc)
	if len(imgs) != 1 {
		t.Fatalf("%d images, want 1", len(imgs))
	}
	base := firstText(t, doc).Origin.Y
	x, _ := glyphTop(t, doc)
	ys, as := alphaColumn(t, imgs[0], x)
	if top := imgs[0].Rect.Y; top < base-1 || top > base {
		t.Errorf("reflection starts at %.2f, want within a point above the baseline %.2f", top, base)
	}
	for i, y := range ys {
		switch d := y - base; {
		case d > 1 && d < 2 && as[i] < 0.45*255:
			t.Errorf("opacity %.0f at %.1f points below the baseline, want about 0.55 of 255", as[i], d)
		case d > 0.455*24+1 && as[i] > 2:
			t.Errorf("opacity %.0f at %.1f points below the baseline, past the fade", as[i], d)
		}
	}
}
