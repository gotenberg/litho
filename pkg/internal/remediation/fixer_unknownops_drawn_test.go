package remediation

import (
	"bytes"
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// unknownOpsReachPDF builds a page whose own content is well formed and draws
// the undefined operator zz through every other kind of content stream, laid
// out as [hexReachPDF] lays its objects out: a form XObject (object 5,
// ASCIIHex-encoded, naming itself and an inner form), the inner form (6), a
// tiling pattern (7), the glyph of a Type 3 font (9) and one state of an
// annotation appearance (11). Its other state (13) is well formed, and the
// page also names an image (12) whose samples read like the operator, which is
// no content stream.
func unknownOpsReachPDF() []byte {
	form := "0 0 1 1 re zz f /X1 Do"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Annots [10 0 R]"+
			" /Resources << /XObject << /X0 5 0 R /Im0 12 0 R >> /Pattern << /P0 7 0 R >> /Font << /T3 8 0 R >> >> >>",
		hexStreamObj("", "q /X0 Do /Im0 Do /Pattern cs /P0 scn 0 0 10 10 re f BT /T3 1 Tf <01> Tj ET Q"),
		hexStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Filter /ASCIIHexDecode"+
			" /Resources << /XObject << /X1 6 0 R /X0 5 0 R >> >>", hex.EncodeToString([]byte(form))+">"),
		hexStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", "0 0 2 2 re zz f"),
		hexStreamObj("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10]"+
			" /XStep 10 /YStep 10 /Resources << >>", "0 0 5 5 re zz f"),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0]"+
			" /CharProcs << /g1 9 0 R >> /Encoding << /Type /Encoding /Differences [1 /g1] >>"+
			" /FirstChar 1 /LastChar 1 /Widths [1] >>",
		hexStreamObj("", "1 0 0 0 1 1 d1 zz 0 0 1 1 re f"),
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AS /On /AP << /N << /On 11 0 R /Off 13 0 R >> >> >>",
		hexStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", "zz 0 0 1 1 re f"),
		hexStreamObj("/Type /XObject /Subtype /Image /Width 2 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray", "zz"),
		hexStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", "0 0 1 1 re f"),
	)
}

// withoutOperator returns ops without the operations whose operator is op.
func withoutOperator(ops []content.Operation, op string) []content.Operation {
	out := make([]content.Operation, 0, len(ops))
	for _, o := range ops {
		if o.Operator != op {
			out = append(out, o)
		}
	}
	return out
}

// TestFixUnknownOps_ReachesEveryDrawnStream pins that the undefined-operator
// repair reaches every kind of content stream compliance judges the tests over
// besides the page's own: a form XObject, a form it draws, a tiling pattern, a
// Type 3 glyph and an annotation appearance state (ISO 19005-2, 6.2.2 t1
// states the test for every content stream). Each stays a stream, keeps its
// dictionary but its filters, and parses to the operations of the source but
// the undefined one; a well-formed stream and an image are not written.
//
// The repair used to read page content, the forms page resources name and
// appearance streams alone, so an undefined operator in a tiling pattern or a
// Type 3 glyph stayed a residual after every remediation.
func TestFixUnknownOps_ReachesEveryDrawnStream(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, unknownOpsReachPDF())
	fixes, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	if got, want := strings.Join(fixes, "\n"), "removed 5 unknown content-stream operator(s)"; got != want {
		t.Errorf("fixes = %q, want %q", got, want)
	}

	for _, tc := range []struct {
		name    string
		obj     int
		keepKey ast.PdfName
	}{
		{"form XObject", hexReachForm, "BBox"},
		{"form a form draws", hexReachInnerForm, "BBox"},
		{"tiling pattern", hexReachPattern, "PatternType"},
		{"Type 3 glyph", hexReachGlyph, ""},
		{"appearance state", hexReachAppearance, "BBox"},
	} {
		got, ok := pendingStream(t, alloc, tc.obj)
		if !ok {
			t.Errorf("%s (object %d) was not rewritten", tc.name, tc.obj)
			continue
		}
		body, err := got.Bytes(ctx)
		if err != nil {
			t.Fatalf("%s: reading the rewrite: %v", tc.name, err)
		}
		if _, filtered := got.Dict["Filter"]; filtered {
			t.Errorf("%s keeps /Filter over a decoded body: %v", tc.name, got.Dict)
		}
		if tc.keepKey != "" && got.Dict[tc.keepKey] == nil {
			t.Errorf("%s lost /%s: %v", tc.name, tc.keepKey, got.Dict)
		}
		if length, _ := got.Dict["Length"].(ast.PdfInt); int(length) != len(body) {
			t.Errorf("%s: /Length %v for a %d-byte body", tc.name, got.Dict["Length"], len(body))
		}
		original, err := doc.DecodeStream(ctx, mustStream(t, doc, tc.obj))
		if err != nil {
			t.Fatalf("%s: decoding the source: %v", tc.name, err)
		}
		source, err := content.ParseStreamData(ctx, original)
		if err != nil {
			t.Fatalf("%s: parsing the source: %v", tc.name, err)
		}
		after, err := content.ParseStreamData(ctx, body)
		if err != nil {
			t.Fatalf("%s: parsing the rewrite: %v", tc.name, err)
		}
		if want := withoutOperator(source, "zz"); !sameOperations(after, want) {
			t.Errorf("%s reads as\n  %v\nwant the source without zz\n  %v", tc.name, after, want)
		}
	}
	for _, obj := range []int{hexReachPage, hexReachImage, hexReachCleanState} {
		if _, rewritten := alloc.Pending()[obj]; rewritten {
			t.Errorf("object %d holds no undefined operator and was rewritten: %v", obj, alloc.Pending()[obj])
		}
	}
}

// TestRewriteNonFiniteNumbers pins which tokens [rewriteNonFiniteNumbers]
// takes for a number with no finite value and rewrites as 0: a formatter's
// spelling of one where an operand stands, and nothing else.
func TestRewriteNonFiniteNumbers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name, src, want string
		n               int
	}{
		{"an empty glyph box", "602 0 inf inf -inf -inf d1 f ", "602 0 0 0 0 0 d1 f ", 4},
		{"other spellings", "NaN w +Infinity 0 m -nan 0 l INF 1 re", "0 w 0 0 m 0 0 l 0 1 re", 4},
		{"inside an array", "[inf 2] 0 d", "[inf 2] 0 d", 0},
		{"a name, a string and longer keywords", "/inf gs (inf) Tj infinite nanx", "/inf gs (inf) Tj infinite nanx", 0},
		{"two signs", "--inf w", "--inf w", 0},
		{"inline image data", "BI /W 1 /H 1 /BPC 8 /CS /G ID inf EI inf w", "BI /W 1 /H 1 /BPC 8 /CS /G ID inf EI 0 w", 1},
	} {
		got, n := rewriteNonFiniteNumbers(ctx, []byte(tc.src))
		if string(got) != tc.want || n != tc.n {
			t.Errorf("%s: rewriteNonFiniteNumbers(%q) = %q, %d; want %q, %d", tc.name, tc.src, got, n, tc.want, tc.n)
		}
	}
}

// nonFiniteGlyphPDF builds a page showing text with a Type 3 font (object 5)
// whose one glyph (object 6) is body.
func nonFiniteGlyphPDF(body string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		hexStreamObj("", "BT /F1 20 Tf 10 20 Td ( ) Tj ET"),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 -236 602 800] /FontMatrix [0.001 0 0 0.001 0 0]"+
			" /CharProcs << /space 6 0 R >> /Encoding << /Type /Encoding /Differences [32 /space] >>"+
			" /FirstChar 32 /LastChar 32 /Widths [602] >>",
		hexStreamObj("", body),
	)
}

// TestFixUnknownOps_KeepsTheWidthOfAGlyphWithNoFiniteBox pins the repair of a
// glyph whose bounding box a producer accumulated from no point and printed as
// infinities. The glyph keeps its d1 with the six operands Table 113 requires,
// its width the one /Widths gives, and runs no operator the producer did not
// write.
//
// Reached as other undefined operators are, "inf" was read as the three
// operators PDF.js splits it into, i n f, ahead of the d1 a glyph starts with
// (ISO 32000-1, 9.6.5), and "-inf" went with no operands, which left d1 with
// none.
//
// # Corpus evidence
//
//   - pdfjs issue5039.pdf: the space glyph of its Type 3 font is this body.
func TestFixUnknownOps_KeepsTheWidthOfAGlyphWithNoFiniteBox(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, nonFiniteGlyphPDF("602 0 inf inf -inf -inf d1 f "))
	fixes, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	if got, want := strings.Join(fixes, "\n"), "rewrote 4 number(s) written with no finite value in content streams as 0"; got != want {
		t.Errorf("fixes = %q, want %q", got, want)
	}
	glyph, ok := pendingStream(t, alloc, 6)
	if !ok {
		t.Fatal("the glyph was not rewritten")
	}
	body, err := glyph.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the glyph: %v", err)
	}
	ops, err := content.ParseStreamData(ctx, body)
	if err != nil {
		t.Fatalf("parsing the glyph %q: %v", body, err)
	}
	want := []content.Operation{
		{Operator: "d1", Operands: []ast.PdfObject{ast.PdfInt(602), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)}},
		{Operator: "f"},
	}
	if !sameOperations(ops, want) {
		t.Errorf("the glyph %q reads as\n  %v\nwant\n  %v", body, ops, want)
	}
}

// type3PatternTransparencyPDF builds a PDF/A-1 page drawing a Type 3 glyph
// (object 6) typed as a form with a transparency group, and a tiling pattern
// (object 7) whose inline graphics state sets a constant alpha, each holding
// the undefined operator zz.
func type3PatternTransparencyPDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"+
			" /Resources << /Font << /T3 5 0 R >> /Pattern << /P0 7 0 R >> >> >>",
		hexStreamObj("", "/Pattern cs /P0 scn 0 0 10 10 re f BT /T3 1 Tf <01> Tj ET"),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0]"+
			" /CharProcs << /g1 6 0 R >> /Encoding << /Type /Encoding /Differences [1 /g1] >>"+
			" /FirstChar 1 /LastChar 1 /Widths [1] >>",
		hexStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 1 1] /Group << /S /Transparency >>",
			"1 0 0 0 1 1 d1 zz 0 0 1 1 re f"),
		hexStreamObj("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10"+
			" /Resources << /ExtGState << /GS0 << /ca 0.5 >> >> >>", "/GS0 gs zz 0 0 5 5 re f"),
	)
}

// TestFixUnknownOps_KeepsTheTransparencyRepairOfAGlyphAndAPattern runs the
// real earlier fixer that writes the same glyph and pattern: transparency,
// ahead of unknown-ops in the registry, drops the glyph's transparency group
// and sets the pattern's alpha to 1 (ISO 19005-1, 6.4), and the undefined
// operator repair must build on those streams, not the document's.
func TestFixUnknownOps_KeepsTheTransparencyRepairOfAGlyphAndAPattern(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, type3PatternTransparencyPDF())
	patternAlpha := func(s ast.PdfStream) float64 {
		res := liveDictValue(ctx, alloc, doc, s.Dict["Resources"])
		gs := liveDictValue(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, res["ExtGState"])["GS0"])
		return document.Float(gs["ca"])
	}
	if _, err := fixTransparency(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixTransparency: %v", err)
	}
	if glyph, ok := pendingStream(t, alloc, 6); !ok || glyph.Dict["Group"] != nil {
		t.Fatal("control: transparency did not drop the glyph's group, so there is no earlier edit to lose")
	}
	if pattern, ok := pendingStream(t, alloc, 7); !ok || patternAlpha(pattern) != 1 {
		t.Fatal("control: transparency did not repair the pattern's alpha, so there is no earlier edit to lose")
	}

	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	for _, obj := range []int{6, 7} {
		got, _ := pendingStream(t, alloc, obj)
		body, err := got.Bytes(ctx)
		if err != nil {
			t.Fatalf("reading object %d: %v", obj, err)
		}
		if bytes.Contains(body, []byte("zz")) {
			t.Fatalf("object %d holds %q: the operator was not removed, so the checks below prove nothing", obj, body)
		}
		if obj == 6 && got.Dict["Group"] != nil {
			t.Errorf("the glyph's /Group is back: the repair rebuilt it from the document; dict = %v", got.Dict)
		}
		if obj == 7 && patternAlpha(got) != 1 {
			t.Errorf("the pattern's /ca is %v again: the repair rebuilt it from the document; dict = %v", patternAlpha(got), got.Dict)
		}
	}
}

// TestFixUnknownOps_KeepsTheHexStringRepairOfAGlyphAndAPattern runs the real
// earlier fixer that writes the same glyph and pattern bodies: hex-string,
// ahead of unknown-ops in the registry, respells their malformed strings, and
// the rewrite of their numbers with no finite value must splice into the
// respelled bodies, not the document's.
func TestFixUnknownOps_KeepsTheHexStringRepairOfAGlyphAndAPattern(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"+
			" /Resources << /Font << /T3 5 0 R >> /Pattern << /P0 7 0 R >> >> >>",
		hexStreamObj("", "/Pattern cs /P0 scn 0 0 10 10 re f BT /T3 1 Tf <01> Tj ET"),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0]"+
			" /CharProcs << /g1 6 0 R >> /Encoding << /Type /Encoding /Differences [1 /g1] >>"+
			" /FirstChar 1 /LastChar 1 /Widths [1] >>",
		hexStreamObj("", "1 0 inf inf -inf -inf d1 BT /F0 1 Tf <414> Tj ET"),
		hexStreamObj("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10"+
			" /Resources << >>", "BT /F0 1 Tf <424> Tj ET inf w 0 0 5 5 re S"),
	))
	if _, err := fixHexString(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixHexString: %v", err)
	}
	for _, obj := range []int{6, 7} {
		if _, ok := pendingStream(t, alloc, obj); !ok {
			t.Fatalf("control: the hex fixer did not rewrite object %d", obj)
		}
	}

	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	for obj, respelled := range map[int]string{6: "<4140>", 7: "<4240>"} {
		got, _ := pendingStream(t, alloc, obj)
		body, err := got.Bytes(ctx)
		if err != nil {
			t.Fatalf("reading object %d: %v", obj, err)
		}
		if bytes.Contains(body, []byte("inf")) {
			t.Fatalf("object %d holds %q: the number was not rewritten, so the check below proves nothing", obj, body)
		}
		if n := streamHexViolations(t, body); n != 0 || !bytes.Contains(body, []byte(respelled)) {
			t.Errorf("object %d holds %q: the repair rebuilt it from the document and discarded the respelling", obj, body)
		}
	}
}

// TestFixUnknownOps_AggressiveLimitsKeepsTheRepairOfAPattern runs the real
// later fixer that writes the same tiling pattern: aggressive-limits, after
// unknown-ops in the registry, trims the DeviceN space the pattern's inline
// resources declare (ISO 19005-1, 6.1.12 t9), and must write the trimmed
// dictionary over the repaired body, not the document's.
func TestFixUnknownOps_AggressiveLimitsKeepsTheRepairOfAPattern(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const patternObj = 4
	doc, alloc := newTestDoc(t, deviceNFixture(deviceNCatalog, deviceNPages,
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Pattern << /P0 4 0 R >> >> >>",
		deviceNStreamObj("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10"+
			" /Resources << /ColorSpace << /CS0 DEVN >> >>", "/CS0 cs 1 1 1 1 1 1 1 1 1 sc zz 0 0 5 5 re f"),
	))
	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	if _, ok := pendingStream(t, alloc, patternObj); !ok {
		t.Fatalf("control: unknown-ops did not rewrite the pattern, object %d", patternObj)
	}

	fixes := runDeviceNTrim(t, ctx, alloc, doc)

	got, _ := pendingStream(t, alloc, patternObj)
	res := liveDictValue(ctx, alloc, doc, got.Dict["Resources"])
	space := liveArray(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, res["ColorSpace"])["CS0"])
	if len(space) < 2 || len(liveArray(ctx, alloc, doc, space[1])) != pdfA1DeviceNComponents {
		t.Fatalf("the pattern's /CS0 is %v: the trim did not rewrite the pattern, so the check below proves nothing; fixes = %v", space, fixes)
	}
	body, err := got.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the pattern: %v", err)
	}
	if bytes.Contains(body, []byte("zz")) {
		t.Errorf("the pattern holds %q again: the trim rebuilt it from the document and discarded the repair", body)
	}
}
