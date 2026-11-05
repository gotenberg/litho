package mutations

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// flattenDoc builds a one-page document carrying the given widget and returns
// what FlattenForm needs.
func flattenDoc(t *testing.T, widget string, extra ...string) (context.Context, *document.Document, *writer.ObjAllocator) {
	t.Helper()

	objs := append([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /DA (/Helv 9 Tf 0 g) >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] /Contents 5 0 R >>",
		widget,
		"<< /Length 0 >>\nstream\n\nendstream",
	}, extra...)

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)

	data := buf.Bytes()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return ctx, doc, writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
}

// flattenedPage returns the rewritten page and the content of every XObject the
// flatten added to it.
func flattenedPage(t *testing.T, alloc *writer.ObjAllocator) (ast.PdfDict, []string) {
	t.Helper()

	page, ok := alloc.Pending()[3].(ast.PdfDict)
	if !ok {
		t.Fatal("the page was not rewritten")
	}
	res, _ := page["Resources"].(ast.PdfDict)
	xobjs, _ := res["XObject"].(ast.PdfDict)

	var drawn []string
	for _, obj := range xobjs {
		ref, isRef := obj.(ast.PdfIndirectRef)
		if !isRef {
			continue
		}
		stream, isStream := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
		if !isStream {
			continue
		}
		raw, err := stream.Bytes(context.Background())
		if err != nil {
			t.Fatalf("reading the generated appearance: %v", err)
		}
		drawn = append(drawn, string(raw))
	}
	return page, drawn
}

// TestFlattenDrawsATextFieldWithNoAppearance pins that a field a viewer would
// draw from its value is drawn, rather than left behind.
//
// ISO 32000-1, 12.7.3.3 has a viewer build the appearance of a variable-text
// field from /V and /DA when the widget stores no /AP. Flattening removes the
// widget, so litho kept such a widget rather than lose what it showed, and
// flattening left the form in place. `set-form` produces the same shape,
// writing /V and asking for regeneration without building an /AP, so set-form
// followed by flatten-form was a no-op.
//
// # Corpus evidence
//
//   - pdfjs annotation-tx.pdf: holds a text field with a value and no /AP,
//     and flattening it left the form in place.
func TestFlattenDrawsATextFieldWithNoAppearance(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (Ada Lovelace) /Rect [10 10 130 30] >>")
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	page, drawn := flattenedPage(t, alloc)
	if annots, _ := page["Annots"].(ast.PdfArray); len(annots) != 0 {
		t.Errorf("the widget survived the flatten: %v", annots)
	}
	if len(drawn) != 1 {
		t.Fatalf("the flatten drew %d appearances, want 1", len(drawn))
	}
	if !strings.Contains(drawn[0], "(Ada Lovelace) Tj") {
		t.Errorf("the generated appearance does not draw the value: %q", drawn[0])
	}
	if !strings.Contains(drawn[0], "Tf") {
		t.Errorf("the generated appearance selects no font: %q", drawn[0])
	}
}

// TestFlattenDrawsACheckedButton pins that a checked button leaves a mark.
func TestFlattenDrawsACheckedButton(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (agree) /V /Yes /Rect [10 10 30 30] >>")
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	_, drawn := flattenedPage(t, alloc)
	if len(drawn) != 1 {
		t.Fatalf("the flatten drew %d appearances, want 1", len(drawn))
	}
	// The tick is stroked rather than drawn from a font, so it needs no
	// resources of its own.
	if !strings.Contains(drawn[0], " l\n") || !strings.Contains(drawn[0], "S\n") {
		t.Errorf("the button appearance strokes nothing: %q", drawn[0])
	}
}

// TestFlattenDropsAnEmptyField is the control: a field holding nothing has
// nothing to draw, so no appearance is generated for it, and the widget goes
// with the form rather than outlive it as a field (see widgetDrawsNothing).
func TestFlattenDropsAnEmptyField(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [10 10 130 30] >>")
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	page, ok := alloc.Pending()[3].(ast.PdfDict)
	if !ok {
		t.Fatal("the page was left alone, so the empty field survived the flatten")
	}
	if annots, _ := page["Annots"].(ast.PdfArray); len(annots) != 0 {
		t.Errorf("an empty field survived the flatten: %v", annots)
	}
	if _, drawn := flattenedPage(t, alloc); len(drawn) != 0 {
		t.Errorf("the flatten drew %d appearances for a field holding nothing", len(drawn))
	}
}

// TestFlattenDrawsAnEmptyFieldsCharacteristics is the control: an empty field
// whose /MK states a background or a border colour still paints them when a
// viewer builds its appearance (ISO 32000-1, Table 189), so the flatten paints
// them into the page, the background filled and the border stroked at the
// /BS width, and the widget leaves with the form.
func TestFlattenDrawsAnEmptyFieldsCharacteristics(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [10 10 130 30] /MK << /BG [1 1 0] /BC [0] >> /BS << /W 2 >> >>")
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}
	page, drawn := flattenedPage(t, alloc)
	if annots, _ := page["Annots"].(ast.PdfArray); len(annots) != 0 {
		t.Errorf("an empty field with a background colour was kept: %v", annots)
	}
	if len(drawn) != 1 {
		t.Fatalf("the flatten drew %d appearances, want the box", len(drawn))
	}
	for _, want := range []string{"1 1 0 rg\n0 0 120 20 re\nf", "0 G\n2 w\n1 1 118 18 re\nS"} {
		if !strings.Contains(drawn[0], want) {
			t.Errorf("the appearance %q does not paint %q", drawn[0], want)
		}
	}
}

// TestFlattenPrefersAStoredAppearance is the control: a widget that already
// carries an /AP is drawn from it, not from a freshly generated one.
func TestFlattenPrefersAStoredAppearance(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (ignored) /Rect [10 10 130 30] /AP << /N 6 0 R >> >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 120 20] /Length 26 >>\nstream\n1 0 0 RG 0 0 10 10 re S\nendstream")
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	_, drawn := flattenedPage(t, alloc)
	for _, d := range drawn {
		if strings.Contains(d, "(ignored) Tj") {
			t.Errorf("the stored appearance was replaced by a generated one: %q", d)
		}
	}
}

// TestParseDefaultAppearance covers the /DA forms ISO 32000-1, 12.7.3.3 allows.
func TestParseDefaultAppearance(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		da     string
		font   string
		size   float64
		colour string
	}{
		{"/Helv 9 Tf 0 g", "Helv", 9, "0 g"},
		{"/Helv 0 Tf 0 g", "Helv", 0, "0 g"},
		{"0 0 1 rg /F1 11 Tf", "F1", 11, "0 0 1 rg"},
		{"/F2 8 Tf 0 0 0 1 k", "F2", 8, "0 0 0 1 k"},
		{"/Helv#20Bold 9 Tf 0 g", "Helv Bold", 9, "0 g"},
		{"/F#231 9 Tf 0 g", "F#1", 9, "0 g"},
		{"//Helvetica 12 Tf 0 g", "Helvetica", 12, "0 g"},
		{"", "", 0, ""},
	} {
		font, size, colour := parseDefaultAppearance(tc.da)
		if font != tc.font || size != tc.size || colour != tc.colour {
			t.Errorf("parseDefaultAppearance(%q) = (%q, %v, %q), want (%q, %v, %q)",
				tc.da, font, size, colour, tc.font, tc.size, tc.colour)
		}
	}
}

// TestTextFieldAppearanceSpellsTheFontName pins that the generated appearance
// selects the /DR font its /DA names when that name needs escaping.
//
// A /DA is content-stream syntax (ISO 32000-1, 12.7.3.3), so "/Helv#20Bold"
// names the /DR key "Helv Bold" (ISO 32000-1, 7.3.5). The name used to be kept
// as spelled, which matched no /DR key and fell back to Helvetica, and once
// decoded it has to be escaped again on the way out: written raw, "/Helv Bold
// 9 Tf" gives Tf the operands Bold and 9 and selects no font.
func TestTextFieldAppearanceSpellsTheFontName(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (Ada) /Rect [10 10 130 30] >>")
	font := ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1"), "BaseFont": ast.PdfName("Helvetica-Bold")}
	catalog := ast.PdfDict{"AcroForm": ast.PdfDict{"DR": ast.PdfDict{"Font": ast.PdfDict{"Helv Bold": font}}}}
	annot := ast.PdfDict{"DA": ast.PdfString("/Helv#20Bold 9 Tf 0 g")}

	stream, ok, err := textFieldAppearance(ctx, doc, alloc, catalog, annot, nil, newTextFonts(nil), "", "Ada", 120, 20)
	if err != nil || !ok {
		t.Fatal("no appearance was generated")
	}
	raw, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the appearance: %v", err)
	}
	ops, err := content.ParseStreamData(ctx, raw)
	if err != nil {
		t.Fatalf("parsing the appearance %q: %v", raw, err)
	}
	var tf []ast.PdfObject
	for _, op := range ops {
		if op.Operator == "Tf" {
			tf = op.Operands
		}
	}
	if len(tf) != 2 || tf[0] != ast.PdfName("Helv Bold") {
		t.Fatalf("the appearance %q selects the font with %v, want [/Helv#20Bold 9]", raw, tf)
	}
	res, _ := stream.Dict["Resources"].(ast.PdfDict)
	fonts, _ := res["Font"].(ast.PdfDict)
	if got, _ := fonts["Helv Bold"].(ast.PdfDict); got["BaseFont"] != ast.PdfName("Helvetica-Bold") {
		t.Errorf("the appearance resources map Helv Bold to %v, want the /DR font", fonts["Helv Bold"])
	}
}

// TestTextFieldAppearanceRefusesANullFontName pins the one font name no
// spelling carries: ISO 32000-1, 7.3.5 excludes NUL from every name, so the
// value is refused rather than drawn through a name the writer refuses.
func TestTextFieldAppearanceRefusesANullFontName(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (Ada) /Rect [10 10 130 30] >>")
	font := ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1"), "BaseFont": ast.PdfName("Helvetica")}
	catalog := ast.PdfDict{"AcroForm": ast.PdfDict{"DR": ast.PdfDict{"Font": ast.PdfDict{"F\x001": font}}}}
	annot := ast.PdfDict{"DA": ast.PdfString("/F#001 9 Tf 0 g")}

	if _, ok, err := textFieldAppearance(ctx, doc, alloc, catalog, annot, nil, newTextFonts(nil), "", "Ada", 120, 20); ok || err == nil {
		t.Error("an appearance was generated through a font name holding NUL")
	}
}

// TestEscapePdfString pins that a value cannot break out of the literal string
// it is drawn from (ISO 32000-1, 7.3.4.2).
func TestEscapePdfString(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{"plain", "(plain)"},
		{"a(b)c", `(a\(b\)c)`},
		{`back\slash`, `(back\\slash)`},
		{"two\nlines", "(two lines)"},
	} {
		if got := escapePdfString([]byte(tc.in)); got != tc.want {
			t.Errorf("escapePdfString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestFlattenRefusesAValueNoFontCovers pins that a value the form's font
// cannot express, with no other font offered, fails the flatten rather than
// leaving the widget behind or drawing mojibake.
//
// The generated appearance draws through a WinAnsi-encoded font (ISO 32000-1,
// Annex D.2), which covers Latin text and nothing else. Writing the value's
// UTF-8 bytes anyway would reach a viewer decoded one byte per character, so a
// Cyrillic value would be drawn as mojibake. Keeping the widget preserved the
// value, but left a form field in a document the flatten promised had none,
// and the widget outlived the form that gave it meaning.
//
// # Corpus evidence
//
//   - pdfjs issue12963.pdf: carries the Cyrillic value "СУВОРОВ", which would
//     have been drawn as mojibake.
func TestFlattenRefusesAValueNoFontCovers(t *testing.T) {
	t.Parallel()
	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (СУВОРОВ) /Rect [10 10 130 30] >>")
	err := FlattenForm(ctx, alloc, doc)
	if !errors.Is(err, ErrFlattenIncomplete) || !errors.Is(err, ErrNoFontCovers) {
		t.Fatalf("FlattenForm returned %v, want ErrFlattenIncomplete wrapping ErrNoFontCovers", err)
	}
	for _, want := range []string{`"name"`, `'С'`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error %q does not name %s", err, want)
		}
	}
}

// cyrillicGlyphs returns one glyph per distinct letter of "СУВОРОВ".
func cyrillicGlyphs() []fontbuild.Glyph {
	var glyphs []fontbuild.Glyph
	seen := map[rune]bool{}
	for _, r := range "СУВОРОВ" {
		if seen[r] {
			continue
		}
		seen[r] = true
		glyphs = append(glyphs, fontbuild.Glyph{Name: fmt.Sprintf("uni%04X", r), Unicode: r, Advance: 600})
	}
	return glyphs
}

// cyrillicTrueType builds a TrueType program whose glyphs cover the Cyrillic
// letters of "СУВОРОВ" and nothing Latin.
func cyrillicTrueType() []byte {
	return fontbuild.TrueType{PostScriptName: "TestSans", Family: "Test Sans", Subfamily: "Regular", Glyphs: cyrillicGlyphs(), NotdefAdvance: 500}.Build()
}

// offering returns a [TextFontSource] offering programs in that order.
func offering(programs ...[]byte) TextFontSource {
	return func(context.Context, TextFontRequest) ([]TextFontCandidate, error) {
		out := make([]TextFontCandidate, len(programs))
		for i, p := range programs {
			out[i] = func(context.Context) ([]byte, error) { return p, nil }
		}
		return out, nil
	}
}

// TestFlattenDrawsAValueWithAnOfferedFont pins the other way out: a program
// offered for the text draws it, embedded as an Identity-H Type0 font whose
// codes are glyph IDs, subset to the glyphs the value uses, with a ToUnicode
// CMap, so the value shows and extracts. A program that does not cover the
// text gives way to one that does.
func TestFlattenDrawsAValueWithAnOfferedFont(t *testing.T) {
	t.Parallel()
	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (СУВОРОВ) /Rect [10 10 130 30] >>")
	latin := fontbuild.TrueType{PostScriptName: "LatinOnly", Family: "Latin Only", Subfamily: "Regular",
		Glyphs: []fontbuild.Glyph{{Name: "A", Unicode: 'A', Advance: 600}}, NotdefAdvance: 500}.Build()
	if err := FlattenFormWith(ctx, alloc, doc, FlattenOptions{Fonts: offering(latin, cyrillicTrueType())}); err != nil {
		t.Fatalf("FlattenFormWith: %v", err)
	}
	page, drawn := flattenedPage(t, alloc)
	if annots, _ := page["Annots"].(ast.PdfArray); len(annots) != 0 {
		t.Errorf("the widget was kept: %v", annots)
	}
	if len(drawn) != 1 {
		t.Fatalf("the flatten drew %d appearances, want 1", len(drawn))
	}
	// Five distinct letters, GIDs 1 to 5 in first-use order: С У В О Р, the
	// repeated О and В mapped back to their glyphs.
	if want := "<0001000200030004000500040003> Tj"; !strings.Contains(drawn[0], want) {
		t.Errorf("the value was not drawn as glyph IDs: %q", drawn[0])
	}
	font := embeddedType0(t, alloc)
	if font["Encoding"] != ast.PdfName("Identity-H") {
		t.Errorf("the font's encoding is %v, want Identity-H", font["Encoding"])
	}
	base, _ := font["BaseFont"].(ast.PdfName)
	if !strings.HasSuffix(string(base), "+TestSans") || len(base) != len("ABCDEF+TestSans") {
		t.Errorf("the font is %q, want a subset of TestSans with a six-letter tag", base)
	}
	if _, ok := font["ToUnicode"].(ast.PdfIndirectRef); !ok {
		t.Error("the font carries no ToUnicode CMap")
	}
	desc := descendantOf(t, alloc, font)
	if desc["Subtype"] != ast.PdfName("CIDFontType2") {
		t.Errorf("the descendant is %v, want a CIDFontType2", desc)
	}
	// Five codes, each mapped to a glyph of the renumbered subset: .notdef
	// then the five letters, in the order the subset numbers them.
	ref, _ := desc["CIDToGIDMap"].(ast.PdfIndirectRef)
	cidToGID, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("CIDToGIDMap is %v, want a stream", desc["CIDToGIDMap"])
	}
	if mapping, err := cidToGID.Bytes(ctx); err != nil || len(mapping) != 12 || mapping[3] != 1 || mapping[11] != 5 {
		t.Errorf("the CIDToGIDMap holds %x (%v), want codes 1 to 5 mapped to glyphs 1 to 5", mapping, err)
	}
	fd := flattenPendingDict(t, alloc, desc["FontDescriptor"])
	if _, ok := fd["FontFile2"]; !ok {
		t.Error("the descriptor embeds no FontFile2")
	}
}

// TestFlattenDrawsAValueWithAnOpenTypeCFFFont pins the CFF path: an OpenType
// program with CFF outlines is embedded as a CIDFontType0 over its CFF table
// (ISO 32000-1, 9.9, Table 126), its codes the glyph IDs a name-keyed program
// takes as CIDs (9.7.4.2).
func TestFlattenDrawsAValueWithAnOpenTypeCFFFont(t *testing.T) {
	t.Parallel()
	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (СУВОРОВ) /Rect [10 10 130 30] >>")
	if err := FlattenFormWith(ctx, alloc, doc, FlattenOptions{Fonts: offering(cyrillicOpenTypeCFF())}); err != nil {
		t.Fatalf("FlattenFormWith: %v", err)
	}
	_, drawn := flattenedPage(t, alloc)
	if len(drawn) != 1 || !strings.Contains(drawn[0], "<0001000200030004000500040003> Tj") {
		t.Fatalf("the value was not drawn as glyph IDs: %v", drawn)
	}
	font := embeddedType0(t, alloc)
	desc := descendantOf(t, alloc, font)
	if desc["Subtype"] != ast.PdfName("CIDFontType0") {
		t.Errorf("the descendant is a %v, want CIDFontType0", desc["Subtype"])
	}
	fd := flattenPendingDict(t, alloc, desc["FontDescriptor"])
	ref, _ := fd["FontFile3"].(ast.PdfIndirectRef)
	ff, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok || ff.Dict["Subtype"] != ast.PdfName("CIDFontType0C") {
		t.Fatalf("the descriptor embeds no CIDFontType0C FontFile3: %v", fd["FontFile3"])
	}
	program, err := ff.Bytes(ctx)
	if err != nil || len(program) < 4 || program[0] != 1 {
		t.Errorf("the FontFile3 holds no CFF program: %v", err)
	}
}

// cyrillicOpenTypeCFF wraps the CFF twin of [cyrillicTrueType] in an OpenType
// program: the TrueType's tables but glyf and loca, with the CFF table in
// their place (OpenType specification, CFF table).
func cyrillicOpenTypeCFF() []byte {
	glyphs := cyrillicGlyphs()
	ttf := fontbuild.TrueType{PostScriptName: "TestSerif", Family: "Test Serif", Subfamily: "Regular", Glyphs: glyphs, NotdefAdvance: 500}.Build()
	cffData := fontbuild.CFF{FontName: "TestSerif", Glyphs: glyphs, NotdefAdvance: 500}.Build()
	records, err := truetype.ParseTableDirectory(ttf)
	if err != nil {
		panic(err)
	}
	tables := map[string][]byte{"CFF ": cffData}
	for _, rec := range records {
		if rec.Tag == "glyf" || rec.Tag == "loca" {
			continue
		}
		data, err := truetype.TableData(ttf, records, rec.Tag)
		if err != nil {
			panic(err)
		}
		tables[rec.Tag] = data
	}
	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	var out, body bytes.Buffer
	out.WriteString("OTTO")
	_ = binary.Write(&out, binary.BigEndian, uint16(len(tags))) //nolint:gosec // a handful of tables
	_ = binary.Write(&out, binary.BigEndian, [3]uint16{0, 0, 0})
	offset := 12 + 16*len(tags)
	for _, tag := range tags {
		data := tables[tag]
		out.WriteString(tag)
		_ = binary.Write(&out, binary.BigEndian, uint32(0))
		_ = binary.Write(&out, binary.BigEndian, uint32(offset+body.Len())) //nolint:gosec // a test program of a few hundred bytes
		_ = binary.Write(&out, binary.BigEndian, uint32(len(data)))         //nolint:gosec // a test program of a few hundred bytes
		body.Write(data)
		for body.Len()%4 != 0 {
			body.WriteByte(0)
		}
	}
	out.Write(body.Bytes())
	return out.Bytes()
}

// embeddedType0 returns the one Type0 font pending in alloc.
func embeddedType0(t *testing.T, alloc *writer.ObjAllocator) ast.PdfDict {
	t.Helper()
	for _, obj := range alloc.Pending() {
		if dict, ok := obj.(ast.PdfDict); ok && dict["Subtype"] == ast.PdfName("Type0") {
			return dict
		}
	}
	t.Fatal("no Type0 font was allocated")
	return nil
}

// descendantOf returns the descendant CIDFont of a Type0 font.
func descendantOf(t *testing.T, alloc *writer.ObjAllocator, font ast.PdfDict) ast.PdfDict {
	t.Helper()
	desc, _ := font["DescendantFonts"].(ast.PdfArray)
	if len(desc) != 1 {
		t.Fatalf("the Type0 font has %d descendants, want 1", len(desc))
	}
	return flattenPendingDict(t, alloc, desc[0])
}

// flattenPendingDict returns the dictionary ref names in alloc.
func flattenPendingDict(t *testing.T, alloc *writer.ObjAllocator, ref ast.PdfObject) ast.PdfDict {
	t.Helper()
	r, _ := ref.(ast.PdfIndirectRef)
	dict, ok := alloc.Pending()[r.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("%v is not a pending dictionary", ref)
	}
	return dict
}

// TestFlattenDrawsAccentedLatinCorrectly pins that a value WinAnsi does cover
// is written in that encoding rather than as raw UTF-8.
func TestFlattenDrawsAccentedLatinCorrectly(t *testing.T) {
	t.Parallel()

	ctx, doc, alloc := flattenDoc(t,
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (Café) /Rect [10 10 130 30] >>")
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	_, drawn := flattenedPage(t, alloc)
	if len(drawn) != 1 {
		t.Fatalf("the flatten drew %d appearances, want 1", len(drawn))
	}
	// The accented character is one byte in WinAnsi (0xE9) and two in UTF-8
	// (0xC3 0xA9). Build both explicitly: an escape written in the source would
	// be at the mercy of whatever wrote the source.
	winAnsi := string([]byte{'(', 'C', 'a', 'f', 0xE9, ')'})
	utf8Bytes := string([]byte{0xC3, 0xA9})
	if !strings.Contains(drawn[0], winAnsi) {
		t.Errorf("the value was not written in WinAnsi: %q", drawn[0])
	}
	if strings.Contains(drawn[0], utf8Bytes) {
		t.Errorf("the value reached the stream as raw UTF-8: %q", drawn[0])
	}
}

// TestWinAnsiEncode covers the encoder the appearance depends on.
func TestWinAnsiEncode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   string
		want []byte
		ok   bool
	}{
		{"plain", []byte("plain"), true},
		{"Café", []byte{'C', 'a', 'f', 0xE9}, true},
		{"€uro", []byte{0x80, 'u', 'r', 'o'}, true},
		{"СУВОРОВ", nil, false},
		{"日本語", nil, false},
	} {
		got, ok := winAnsiEncode(tc.in)
		if ok != tc.ok {
			t.Errorf("winAnsiEncode(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if ok && string(got) != string(tc.want) {
			t.Errorf("winAnsiEncode(%q) = % x, want % x", tc.in, got, tc.want)
		}
	}
}
