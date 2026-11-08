package pptx

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
)

// rgb returns the opaque colour of 0xRRGGBB.
func rgb(v uint32) css.Color { return hex(v) }

// sameColor reports whether two colours agree to a level of 255.
func sameColor(a, b css.Color) bool {
	d := func(x, y float64) bool { return math.Abs(x-y) < 1.01/255 }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}

func TestText_RunStyleResolution(t *testing.T) {
	t.Parallel()
	body := func(s string) string { return phShape("Content Placeholder 2", "", 1, 0, 0, 0, 0, s) }
	for _, tc := range []struct {
		name  string
		tree  string
		check func(t *testing.T, r slides.Run)
	}{
		{"master body style", body(para("x")), func(t *testing.T, r slides.Run) {
			if r.Style.Size != 32 || !slices.Equal(r.Style.Families, []string{"Test Sans"}) || !sameColor(r.Style.Color, rgb(0)) {
				t.Errorf("style %+v", r.Style)
			}
		}},
		{"shape list style over the master", body(`<a:bodyPr/><a:lstStyle><a:lvl1pPr><a:defRPr sz="2000" b="1"/></a:lvl1pPr></a:lstStyle><a:p><a:r><a:t>x</a:t></a:r></a:p>`), func(t *testing.T, r slides.Run) {
			if r.Style.Size != 20 || !r.Style.Bold {
				t.Errorf("style %+v", r.Style)
			}
		}},
		{"run over everything", body(`<a:bodyPr/><a:p><a:r><a:rPr sz="1000" i="1" u="sng" strike="sngStrike" baseline="30000" cap="small" spc="150" lang="fr-FR"><a:solidFill><a:schemeClr val="accent2"/></a:solidFill><a:latin typeface="Courier"/><a:ea typeface="+mn-lt"/></a:rPr><a:t>x</a:t></a:r></a:p>`), func(t *testing.T, r slides.Run) {
			s := r.Style
			if s.Size != 10 || !s.Italic || !s.Underline || !s.Strike || s.Script != slides.Superscript || s.Caps != slides.CapsSmall || s.Spacing != 1.5 || s.Lang != "fr-FR" {
				t.Errorf("style %+v", s)
			}
			if !slices.Equal(s.Families, []string{"Courier", "Test Sans"}) || !sameColor(s.Color, rgb(0xC0504D)) {
				t.Errorf("faces %q colour %+v", s.Families, s.Color)
			}
		}},
		{"level two of the body style", body(`<a:bodyPr/><a:p><a:pPr lvl="1"/><a:r><a:t>x</a:t></a:r></a:p>`), func(t *testing.T, r slides.Run) {
			if r.Style.Size != 28 {
				t.Errorf("size %v", r.Style.Size)
			}
		}},
		{"text box takes the other style", textBox("TB", 0, 0, 12700, 12700, para("x")), func(t *testing.T, r slides.Run) {
			if r.Style.Size != 18 {
				t.Errorf("size %v", r.Style.Size)
			}
		}},
		{"shape style font reference", `<p:sp><p:nvSpPr><p:cNvPr id="3" name="S"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="1" cy="1"/></a:xfrm><a:prstGeom prst="ellipse"/></p:spPr>` +
			`<p:style><a:lnRef idx="2"><a:schemeClr val="accent1"/></a:lnRef><a:fillRef idx="1"><a:schemeClr val="accent1"/></a:fillRef><a:effectRef idx="0"><a:schemeClr val="accent1"/></a:effectRef><a:fontRef idx="minor"><a:schemeClr val="lt1"/></a:fontRef></p:style>` +
			`<p:txBody>` + para("x") + `</p:txBody></p:sp>`, func(t *testing.T, r slides.Run) {
			if !sameColor(r.Style.Color, rgb(0xFFFFFF)) {
				t.Errorf("colour %+v", r.Style.Color)
			}
		}},
		{"hyperlink colour and underline", textBox("TB", 0, 0, 1, 1, `<a:bodyPr/><a:p><a:r><a:rPr><a:hlinkClick r:id="rIdH"/></a:rPr><a:t>x</a:t></a:r></a:p>`), func(t *testing.T, r slides.Run) {
			if r.Href != "https://example.com/" || !r.Style.Underline || !sameColor(r.Style.Color, rgb(0x0000FF)) {
				t.Errorf("run %+v", r)
			}
		}},
		{"fields", textBox("TB", 0, 0, 1, 1, `<a:bodyPr/><a:p><a:fld id="{1}" type="slidenum"><a:t>4</a:t></a:fld></a:p>`), func(t *testing.T, r slides.Run) {
			if r.Field != slides.FieldSlideNumber || r.Text != "4" {
				t.Errorf("run %+v", r)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := readDeck(t, testDeck{slides: []testSlide{{tree: tc.tree,
				rels: `<Relationship Id="rIdH" Type="` + tRelLink + `" Target="https://example.com/" TargetMode="External"/>`}}})
			shapes := doc.Slides[0].Shapes
			if len(shapes) != 1 || shapes[0].Text == nil || len(shapes[0].Text.Paragraphs) == 0 || len(shapes[0].Text.Paragraphs[0].Runs) == 0 {
				t.Fatalf("shapes %+v", shapes)
			}
			tc.check(t, shapes[0].Text.Paragraphs[0].Runs[0])
		})
	}
}

func TestText_ColorMapOverride(t *testing.T) {
	t.Parallel()
	doc, _ := readDeck(t, testDeck{slides: []testSlide{{
		tree:  textBox("TB", 0, 0, 1, 1, para("x")),
		after: `<p:clrMapOvr><a:overrideClrMapping bg1="dk1" tx1="lt1" bg2="dk2" tx2="lt2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/></p:clrMapOvr>`,
	}}})
	if c := doc.Slides[0].Shapes[0].Text.Paragraphs[0].Runs[0].Style.Color; !sameColor(c, rgb(0xFFFFFF)) {
		t.Errorf("tx1 maps to lt1: %+v", c)
	}
}

func TestText_ParagraphsAndBullets(t *testing.T) {
	t.Parallel()
	body := func(p string) string {
		return phShape("Content Placeholder 2", "", 1, 0, 0, 0, 0, `<a:bodyPr/><a:lstStyle/>`+p)
	}
	for _, tc := range []struct {
		name  string
		p     string
		check func(t *testing.T, p slides.Paragraph)
	}{
		{"master bullet and indents", `<a:p><a:r><a:t>x</a:t></a:r></a:p>`, func(t *testing.T, p slides.Paragraph) {
			b := p.Bullet
			if b == nil || b.Kind != slides.BulletChar || b.Char != "•" || !slices.Equal(b.Families, []string{"Arial"}) {
				t.Fatalf("bullet %+v", b)
			}
			if p.MarginLeft != 27 || p.Indent != -27 || p.SpaceBeforePct != 0.2 || p.Align != css.AlignLeft {
				t.Errorf("paragraph %+v", p)
			}
		}},
		{"buNone removes it", `<a:p><a:pPr><a:buNone/></a:pPr><a:r><a:t>x</a:t></a:r></a:p>`, func(t *testing.T, p slides.Paragraph) {
			if p.Bullet != nil {
				t.Errorf("bullet %+v", p.Bullet)
			}
		}},
		{"numbering with colour and size", `<a:p><a:pPr><a:buClr><a:srgbClr val="FF0000"/></a:buClr><a:buSzPct val="75000"/><a:buAutoNum type="romanUcPeriod" startAt="3"/></a:pPr><a:r><a:t>x</a:t></a:r></a:p>`, func(t *testing.T, p slides.Paragraph) {
			b := p.Bullet
			if b == nil || b.Kind != slides.BulletNumber || b.Format != "romanUcPeriod" || b.StartAt != 3 || b.Scale != 0.75 || !sameColor(b.Color, rgb(0xFF0000)) {
				t.Errorf("bullet %+v", b)
			}
		}},
		{"numbering without a start continues the count", `<a:p><a:pPr><a:buAutoNum type="arabicPeriod"/></a:pPr><a:r><a:t>x</a:t></a:r></a:p>`, func(t *testing.T, p slides.Paragraph) {
			if b := p.Bullet; b == nil || b.Kind != slides.BulletNumber || b.StartAt != 0 {
				t.Errorf("bullet %+v", b)
			}
		}},
		{"empty paragraph has no bullet", `<a:p><a:endParaRPr sz="1200"/></a:p>`, func(t *testing.T, p slides.Paragraph) {
			if p.Bullet != nil || p.Style.Size != 12 || len(p.Runs) != 0 {
				t.Errorf("paragraph %+v", p)
			}
		}},
		{"spacing, alignment, tabs, direction", `<a:p><a:pPr algn="ctr" rtl="1" marR="12700"><a:lnSpc><a:spcPct val="150000"/></a:lnSpc><a:spcBef><a:spcPts val="600"/></a:spcBef><a:spcAft><a:spcPct val="50%"/></a:spcAft><a:tabLst><a:tab pos="914400"/></a:tabLst></a:pPr><a:r><a:t>x</a:t></a:r><a:br/><a:r><a:t>y</a:t></a:r></a:p>`, func(t *testing.T, p slides.Paragraph) {
			if p.Align != css.AlignCenter || !p.RightToLeft || p.MarginRight != 1 || p.LineSpacing != 1.5 || p.SpaceBefore != 6 || p.SpaceAfterPct != 0.5 || !slices.Equal(p.TabStops, []float64{72}) {
				t.Errorf("paragraph %+v", p)
			}
			if len(p.Runs) != 3 || !p.Runs[1].Break {
				t.Errorf("runs %+v", p.Runs)
			}
		}},
		{"a bullet of several characters draws the first", `<a:p><a:pPr><a:buChar char="••"/></a:pPr><a:r><a:t>x</a:t></a:r></a:p>`, func(t *testing.T, p slides.Paragraph) {
			if p.Bullet == nil || p.Bullet.Char != "•" {
				t.Errorf("bullet %+v", p.Bullet)
			}
		}},
		{"a bullet no level gives a face draws in OpenSymbol", `<a:p><a:pPr><a:buChar char="-"/></a:pPr><a:r><a:t>x</a:t></a:r></a:p>`, func(t *testing.T, p slides.Paragraph) {
			if p.Bullet == nil || !slices.Equal(p.Bullet.Families, []string{"OpenSymbol"}) {
				t.Errorf("bullet %+v", p.Bullet)
			}
		}},
		{"exact line spacing", `<a:p><a:pPr><a:lnSpc><a:spcPts val="2400"/></a:lnSpc></a:pPr><a:r><a:t>x</a:t></a:r></a:p>`, func(t *testing.T, p slides.Paragraph) {
			if p.LineSpacingPoints != 24 {
				t.Errorf("paragraph %+v", p)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := oneSlide(t, title("T")+body(tc.p))
			sh := shapeNamed(s.Shapes, "Content Placeholder 2")
			if tc.name == "empty paragraph has no bullet" || strings.Contains(tc.name, "OpenSymbol") {
				// An empty placeholder prints nothing: read a text box.
				s = oneSlide(t, textBox("TB", 0, 0, 1, 1, `<a:bodyPr/>`+tc.p))
				sh = s.Shapes[0]
			}
			if sh == nil || sh.Text == nil || len(sh.Text.Paragraphs) != 1 {
				t.Fatalf("shape %+v", sh)
			}
			tc.check(t, sh.Text.Paragraphs[0])
		})
	}
}

func TestText_BodyProperties(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, bodyPr string
		check        func(t *testing.T, b *slides.TextBody)
	}{
		{"defaults", `<a:bodyPr/>`, func(t *testing.T, b *slides.TextBody) {
			if b.Insets != [4]float64{3.6, 7.2, 3.6, 7.2} || b.Anchor != slides.AnchorTop || !b.Wrap || b.Autofit != slides.AutofitNone {
				t.Errorf("body %+v", b)
			}
		}},
		{"set", `<a:bodyPr lIns="0" tIns="12700" rIns="25400" bIns="38100" anchor="b" wrap="none" numCol="2" spcCol="127000" vert="vert270" rtlCol="1"><a:normAutofit fontScale="62500" lnSpcReduction="20000"/></a:bodyPr>`, func(t *testing.T, b *slides.TextBody) {
			if b.Insets != [4]float64{1, 2, 3, 0} || b.Anchor != slides.AnchorBottom || b.Wrap || b.Columns != 2 || b.ColumnGap != 10 || b.Vertical != css.SidewaysLR || !b.RightToLeft {
				t.Errorf("body %+v", b)
			}
			if b.Autofit != slides.AutofitShrink || b.FontScale != 0.625 || b.SpacingReduction != 0.2 {
				t.Errorf("autofit %+v", b)
			}
		}},
		{"a clipped overflow", `<a:bodyPr vertOverflow="clip"/>`, func(t *testing.T, b *slides.TextBody) {
			if !b.Clip {
				t.Errorf("body %+v", b)
			}
		}},
		{"an overflow", `<a:bodyPr vertOverflow="overflow"/>`, func(t *testing.T, b *slides.TextBody) {
			if b.Clip {
				t.Errorf("body %+v", b)
			}
		}},
		{"a shape fit to its text keeps its box", `<a:bodyPr anchor="ctr"><a:spAutoFit/></a:bodyPr>`, func(t *testing.T, b *slides.TextBody) {
			if b.Autofit != slides.AutofitNone || b.Anchor != slides.AnchorMiddle {
				t.Errorf("body %+v", b)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := oneSlide(t, textBox("TB", 0, 0, 914400, 914400, tc.bodyPr+`<a:p><a:r><a:t>x</a:t></a:r></a:p>`))
			tc.check(t, s.Shapes[0].Text)
		})
	}
	// A placeholder inherits its master's body properties attribute by
	// attribute: the master title is anchored in the middle and shrinks.
	s := oneSlide(t, title("T"))
	if b := s.Shapes[0].Text; b.Anchor != slides.AnchorMiddle || b.Autofit != slides.AutofitShrink {
		t.Errorf("title body %+v", b)
	}
}

func TestText_OrphanPlaceholder(t *testing.T) {
	t.Parallel()
	doc, _ := readDeck(t, testDeck{
		layoutTree: phShape("Title 1", "title", -1, 0, 0, 0, 0, para("x")),
		slides:     []testSlide{{tree: phShape("Orphan", "body", 99, 0, 0, 12700, 12700, para("x"))}},
	})
	p := doc.Slides[0].Shapes[0].Text.Paragraphs[0]
	if p.Bullet != nil || p.MarginLeft != 0 || p.Runs[0].Style.Size != 18 {
		t.Errorf("a placeholder its layout lacks inherits nothing: %+v", p)
	}
}

func TestText_DateFieldFormats(t *testing.T) {
	// 2026-11-08 11:25:38, the moment the measurements were printed.
	serial := float64(time.Date(2026, 11, 8, 11, 25, 38, 0, time.UTC).Sub(time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC))) / float64(24*time.Hour)
	cases := []struct{ typ, lang, want string }{
		{"datetime", "en-US", "11/8/26"},
		{"datetime1", "en-US", "11/08/2026"},
		{"datetime2", "", "Sunday, November 8, 2026"},
		{"datetime3", "en-US", "November 8, 2026"},
		{"datetime5", "en-US", "Nov 8, 2026"},
		{"datetime8", "en-US", "11/08/2026 11:25 AM"},
		{"datetime9", "en-US", "11/08/2026 11:25:38 AM"},
		{"datetime10", "en-US", "11:25"},
		{"datetime11", "en-US", "11:25:38 AM"},
		{"datetime12", "en-US", "11:25 AM"},
		{"datetime1", "en-GB", "08/11/2026"},
		{"datetime2", "en-GB", "Sunday 8 November 2026"},
		{"datetime11", "en-GB", "11:25:38"},
		{"datetime1", "de-DE", "08.11.2026"},
		{"slidenum", "en-US", ""},
	}
	for _, c := range cases {
		code := dateFormat(c.typ, c.lang)
		if c.want == "" {
			if code != "" {
				t.Errorf("%s: code %q", c.typ, code)
			}
			continue
		}
		f, err := numfmt.Parse(code)
		if err != nil {
			t.Fatalf("%s %s: %v", c.typ, c.lang, err)
		}
		if got := f.Apply(numfmt.Value{Kind: numfmt.Number, Number: serial}).Text; got != c.want {
			t.Errorf("%s %s: %q prints %q, want %q", c.typ, c.lang, code, got, c.want)
		}
	}
}

func TestText_InsetsFitTheBox(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, bodyPr string
		want         [4]float64
	}{
		{"insets within the box stay", `<a:bodyPr lIns="12700" tIns="12700" rIns="12700" bIns="12700"/>`, [4]float64{1, 1, 1, 1}},
		{"top and bottom past the height lose half the excess each", `<a:bodyPr lIns="0" tIns="1270000" rIns="0" bIns="0"/>`, [4]float64{55, 0, -45, 0}},
		{"left and right past the width", `<a:bodyPr lIns="762000" tIns="0" rIns="762000" bIns="0"/>`, [4]float64{0, 55, 0, 55}},
		{"an inset the file leaves out keeps its default", `<a:bodyPr tIns="1270000"/>`, [4]float64{100, 7.2, 3.6, 7.2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := oneSlide(t, textBox("TB", 0, 0, 1397000, 127000, tc.bodyPr+`<a:p><a:r><a:t>x</a:t></a:r></a:p>`))
			got := s.Shapes[0].Text.Insets
			for i := range got {
				if d := got[i] - tc.want[i]; d > 1e-6 || d < -1e-6 {
					t.Fatalf("insets %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestText_DefaultTabs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		stops    []float64
		defTabSz string
		want     []float64
	}{
		{"the builder's 72 points add nothing", nil, "914400", nil},
		{"36 points from the start", nil, "457200", []float64{36, 72, 108}},
		{"after the last explicit stop", []float64{50}, "457200", []float64{50, 72, 108}},
		{"a distance that does not parse", []float64{50}, "x", []float64{50}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := defaultTabs(tc.stops, tc.defTabSz)
			if tc.want == nil {
				if len(got) != 0 {
					t.Fatalf("stops %v", got)
				}
				return
			}
			if n := len(got); (n != maxTabStops && n != len(tc.stops)) || !slices.Equal(got[:len(tc.want)], tc.want) {
				t.Errorf("stops %v, want %v first and %d in all", got[:min(len(got), 5)], tc.want, maxTabStops)
			}
		})
	}
}
