package html

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"math"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// Font files of the repository the tests load: Ahem, public domain, and
// the same font as WOFF 2.0.
const (
	ahemTTF   = "../../layout/testdata/Ahem.ttf"
	ahemWOFF2 = "../../typography/woff/testdata/Ahem.woff2"
	ahemWOFF  = "../../typography/woff/testdata/Ahem.woff"
)

func readTestFile(t testing.TB, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fontResources is a resources root holding Ahem in each format.
func fontResources(t testing.TB) fstest.MapFS {
	return fstest.MapFS{
		"fonts/ahem.ttf":   {Data: readTestFile(t, ahemTTF)},
		"fonts/ahem.woff2": {Data: readTestFile(t, ahemWOFF2)},
		"fonts/ahem.woff":  {Data: readTestFile(t, ahemWOFF)},
		"fonts/bogus.ttf":  {Data: []byte("not a font")},
	}
}

// texts returns the text runs of doc in paint order, groups flattened.
func texts(doc *typeset.Document) []*typeset.Text {
	var out []*typeset.Text
	var walkFrags func([]typeset.Fragment)
	walkFrags = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Text:
				out = append(out, v)
			case *typeset.Group:
				walkFrags(v.Content)
			}
		}
	}
	for _, p := range doc.Pages {
		walkFrags(p.Content)
	}
	return out
}

// runText returns the characters a run stands for.
func runText(r *typeset.Text) string {
	var b strings.Builder
	for _, g := range r.Glyphs {
		b.WriteString(g.Text)
	}
	return b.String()
}

// convertFonts converts src with the font resources, failing on error.
func convertFonts(t *testing.T, src string) *typeset.Document {
	t.Helper()
	doc, err := Convert(context.Background(), strings.NewReader(src), testFonts(t), Options{Resources: fontResources(t)})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return doc
}

// faceOfText returns the key of the face drawing the run holding marker.
func faceOfText(t *testing.T, doc *typeset.Document, marker string) *typeset.Text {
	t.Helper()
	for _, r := range texts(doc) {
		if strings.Contains(runText(r), marker) {
			return r
		}
	}
	t.Fatalf("no run holds %q", marker)
	return nil
}

func TestFontFace_LoadsEachFormat(t *testing.T) {
	t.Parallel()
	data := base64.StdEncoding.EncodeToString(readTestFile(t, ahemTTF))
	src := `<style>
@font-face { font-family: AhemTTF; src: url(fonts/ahem.ttf) format("truetype"); }
@font-face { font-family: AhemWOFF2; src: url(fonts/ahem.woff2) format("woff2"); }
@font-face { font-family: AhemWOFF; src: url("fonts/ahem.woff") format("woff"); }
@font-face { font-family: AhemData; src: url(data:font/ttf;base64,` + data + `); }
p { font-size: 20px; margin: 0 }
</style>
<p style="font-family: AhemTTF">TTFX</p>
<p style="font-family: AhemWOFF2">WOFFTWOX</p>
<p style="font-family: AhemWOFF">WOFFONEX</p>
<p style="font-family: AhemData">DATAX</p>
<p>SERIFX</p>`
	doc := convertFonts(t, src)
	for _, w := range doc.Warnings {
		if w.Code == warn.FontNotLoaded {
			t.Errorf("warning: %s", w.Message)
		}
	}
	for _, marker := range []string{"TTFX", "WOFFTWOX", "WOFFONEX", "DATAX"} {
		r := faceOfText(t, doc, marker)
		if !strings.HasPrefix(r.Face.Key, "font-face:") {
			t.Errorf("%s drawn with %s", marker, r.Face.Key)
		}
		// Ahem's glyphs are one em wide: 20px is 15pt.
		if len(r.Glyphs) > 1 && math.Abs(r.Glyphs[1].X-r.Glyphs[0].X-15) > 1e-6 {
			t.Errorf("%s: advance %v, want 15", marker, r.Glyphs[1].X-r.Glyphs[0].X)
		}
	}
	if k := faceOfText(t, doc, "SERIFX").Face.Key; strings.HasPrefix(k, "font-face:") {
		t.Errorf("an undeclared family drew with %s", k)
	}
	// The same bytes, from a file or a data: URL, embed as one font.
	if faceOfText(t, doc, "TTFX").Face.Key != faceOfText(t, doc, "DATAX").Face.Key {
		t.Error("the file and the data: URL got different keys")
	}
}

func TestFontFace_SourcesInOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		src     string
		loads   bool
		warning string
	}{
		{"missing then file", `src: url(fonts/none.ttf), url(fonts/ahem.ttf)`, true, ""},
		{"unsupported format skipped", `src: url(fonts/ahem.ttf) format("embedded-opentype"), url(fonts/ahem.woff2) format(woff2)`, true, ""},
		{"several formats", `src: url(fonts/ahem.woff) format("svg", "woff")`, true, ""},
		{"local", `src: local(TestSerif-Regular)`, true, ""},
		{"local missing then url", `src: local("No Such Font"), url(fonts/ahem.ttf)`, true, ""},
		{"all fail", `src: url(fonts/none.ttf), local(Nope)`, false, "fonts/none.ttf"},
		{"unknown format", `src: url(fonts/ahem.ttf) format("svg")`, false, "format()"},
		{"unsupported tech", `src: url(fonts/ahem.ttf) tech(color-colrv1)`, false, "tech()"},
		{"colour techs", `src: url(fonts/ahem.ttf) tech(color-COLRv0, color-CBDT, color-sbix, palettes)`, true, ""},
		{"one tech unsupported", `src: url(fonts/ahem.ttf) tech(color-COLRv0, color-svg)`, false, "tech()"},
		{"not a font", `src: url(fonts/bogus.ttf)`, false, "not a TrueType"},
		{"outside the root", `src: url(../x.ttf)`, false, "leaves the resources root"},
		{"absolute url", `src: url(https://example.com/f.woff2)`, false, "no HTTP client"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := convertFonts(t, `<style>@font-face { font-family: Web; `+tt.src+` }
p { font-family: Web, "Test Serif"; font-size: 20px }</style><p>MARKX</p>`)
			r := faceOfText(t, doc, "MARKX")
			ahem := strings.HasPrefix(r.Face.Key, "font-face:") || tt.name == "local"
			if tt.name == "local" && !strings.HasSuffix(r.Face.Key, "test_serif.ttf#0") {
				t.Errorf("local() drew with %s", r.Face.Key)
			}
			if ahem != tt.loads {
				t.Errorf("drawn with %s, loads = %v", r.Face.Key, tt.loads)
			}
			var msgs []string
			for _, w := range doc.Warnings {
				if w.Code == warn.FontNotLoaded {
					msgs = append(msgs, w.Message)
				}
			}
			switch {
			case tt.warning == "" && len(msgs) > 0:
				t.Errorf("unexpected warnings: %v", msgs)
			case tt.warning != "" && (len(msgs) != 1 || !strings.Contains(msgs[0], tt.warning)):
				t.Errorf("warnings %v, want one naming %q", msgs, tt.warning)
			}
		})
	}
}

func TestFontFace_FailedFamilyFallsThrough(t *testing.T) {
	t.Parallel()
	// A declared family shadows the installed family of the same name even
	// when its rule loads nothing: Test Serif here resolves to the next
	// family, Ahem.
	doc := convertFonts(t, `<style>
@font-face { font-family: "Test Serif"; src: url(fonts/none.ttf) }
@font-face { font-family: Next; src: url(fonts/ahem.ttf) }
p { font-family: "Test Serif", Next; font-size: 20px }</style><p>MARKX</p>`)
	if k := faceOfText(t, doc, "MARKX").Face.Key; !strings.HasPrefix(k, "font-face:") {
		t.Errorf("drawn with %s, want the next family's Ahem", k)
	}
}

func TestFontFace_SyntheticBold(t *testing.T) {
	t.Parallel()
	doc := convertFonts(t, `<style>
@font-face { font-family: Web; src: url(fonts/ahem.ttf) }
@font-face { font-family: Web; src: url(fonts/ahem.woff2); font-weight: 700 }
@font-face { font-family: Light; src: url(fonts/ahem.ttf) }
p { font-size: 24px }</style>
<p style="font-family: Web; font-weight: bold">BOLDFACEX</p>
<p style="font-family: Light; font-weight: 600">SEMIX</p>
<p style="font-family: Light; font-style: italic">ITALX</p>`)
	if r := faceOfText(t, doc, "BOLDFACEX"); r.Stroke != 0 {
		t.Errorf("a bold rule was emboldened: stroke %v", r.Stroke)
	}
	if r := faceOfText(t, doc, "SEMIX"); r.Stroke <= 0 {
		t.Error("600 on a 400 rule was not emboldened")
	}
	if r := faceOfText(t, doc, "ITALX"); r.Skew <= 0 {
		t.Error("italic on a normal rule was not slanted")
	}
}

func TestFontFace_TooManyFaces(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("<style>")
	for range maxFontFaces + 1 {
		b.WriteString("@font-face { font-family: Web; src: url(fonts/ahem.ttf) }\n")
	}
	b.WriteString("</style><p style='font-family: Web'>MARKX</p>")
	doc := convertFonts(t, b.String())
	n := 0
	for _, w := range doc.Warnings {
		if w.Code == warn.FontNotLoaded && strings.Contains(w.Message, "more than") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d limit warnings, want 1", n)
	}
}

func TestFontLoader_Budget(t *testing.T) {
	t.Parallel()
	l := &fontLoader{res: newResources(fontResources(t)), fonts: testFonts(t), programs: map[string]programResult{}, total: maxTotalFontBytes - 10}
	if _, err := l.program(context.Background(), rootPrefix+"fonts/ahem.ttf"); !errors.Is(err, errFontBudget) {
		t.Errorf("program past the budget: %v", err)
	}
	l.total = 0
	if p, err := l.program(context.Background(), rootPrefix+"fonts/ahem.woff2"); err != nil || l.total != int64(len(p)) {
		t.Errorf("program: %v, total %d", err, l.total)
	}
}

func TestFontFace_Deterministic(t *testing.T) {
	t.Parallel()
	src := `<style>@font-face { font-family: Web; src: url(fonts/ahem.woff2) }
p { font-family: Web; font-size: 20px }</style><p>Same bytes</p>`
	var outs [2][]byte
	for i := range outs {
		var buf bytes.Buffer
		if err := typeset.Write(context.Background(), convertFonts(t, src), &buf, typeset.Options{}); err != nil {
			t.Fatal(err)
		}
		outs[i] = buf.Bytes()
	}
	if !bytes.Equal(outs[0], outs[1]) {
		t.Error("two conversions differ")
	}
}

func TestDescriptorOf_Styles(t *testing.T) {
	t.Parallel()
	d := descriptorOf(css.FontFace{Family: "F", Style: css.FontOblique, ObliqueMin: 5, ObliqueMax: 20, WeightMin: 100, WeightMax: 900})
	if d.ObliqueMin != 5 || d.ObliqueMax != 20 || d.WeightMax != 900 || d.Family != "F" {
		t.Errorf("descriptor %+v", d)
	}
}

// varFontTTF is a variable TrueType font of five axes, wght, wdth, slnt,
// opsz and TEST, made for the instancer's tests.
const varFontTTF = "../../typography/varfont/testdata/varfont.ttf"

// TestFontFace_VariableFont pins that a variable @font-face draws, and
// embeds, the instance its text asks for: one per weight, the rule's
// variations and the property's applied, the -variations format and the
// variations technology read, and the output the same bytes twice.
func TestFontFace_VariableFont(t *testing.T) {
	t.Parallel()
	res := fstest.MapFS{"var.ttf": {Data: readTestFile(t, varFontTTF)}}
	src := `<style>
@font-face { font-family: Var; src: url(var.ttf) format("truetype-variations") tech(variations); }
@font-face { font-family: VarRule; src: url(var.ttf); font-weight: 100 900; font-variation-settings: "wght" 800; }
p { font: 12px Var; margin: 0 }
</style>
<p style="font-weight: 400">AB AAAA</p>
<p style="font-weight: 900">AB BBBB</p>
<p style="font-weight: 900">AB ABAB</p>
<p style="font-variation-settings: 'wght' 900">AB BABA</p>
<p style="font-family: VarRule">AB AABB</p>`
	var outs [2][]byte
	for i := range outs {
		doc, err := Convert(context.Background(), strings.NewReader(src), testFonts(t), Options{Resources: res})
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range doc.Warnings {
			if w.Code == warn.CSSUnsupported || w.Code == warn.FontNotLoaded || w.Code == warn.ContentUnsupported {
				t.Errorf("warning: %s", w.Message)
			}
		}
		regular, bold := faceOfText(t, doc, "AAAA").Face, faceOfText(t, doc, "BBBB").Face
		if strings.Contains(regular.Key, "@") || !strings.Contains(bold.Key, "@") {
			t.Errorf("weight 400 drew with %q, 900 with %q: want the face and an instance", regular.Key, bold.Key)
		}
		if again := faceOfText(t, doc, "ABAB").Face; again.Key != bold.Key {
			t.Errorf("two runs at weight 900 drew with %q and %q", bold.Key, again.Key)
		}
		if v := faceOfText(t, doc, "BABA").Face; !bytes.Equal(v.Program, bold.Program) {
			t.Error(`font-variation-settings: "wght" 900 drew another instance than font-weight: 900`)
		}
		if r := faceOfText(t, doc, "AABB"); r.Stroke != 0 || !strings.Contains(r.Face.Key, "@") {
			t.Errorf("the rule's variations drew with %q, stroke %v", r.Face.Key, r.Stroke)
		}
		var buf bytes.Buffer
		if err := typeset.Write(context.Background(), doc, &buf, typeset.Options{}); err != nil {
			t.Fatal(err)
		}
		outs[i] = buf.Bytes()
	}
	if !bytes.Equal(outs[0], outs[1]) {
		t.Error("two conversions differ")
	}
}
