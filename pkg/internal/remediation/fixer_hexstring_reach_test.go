package remediation

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// hexStreamObj writes an indirect stream object body with dict and body.
func hexStreamObj(dict, body string) string {
	return "<< " + dict + " /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream"
}

// Objects of [hexReachPDF].
const (
	hexReachPage       = 4
	hexReachForm       = 5
	hexReachInnerForm  = 6
	hexReachPattern    = 7
	hexReachGlyph      = 9
	hexReachAppearance = 11
	hexReachImage      = 12
	hexReachCleanState = 13
)

// hexReachPDF builds a page whose own content is well formed and draws a
// malformed hex string through every other kind of content stream: a form
// XObject (object 5, ASCIIHex-encoded, naming itself and an inner form), the
// inner form (6), a tiling pattern (7), the glyph of a Type 3 font (9) and one
// state of an annotation appearance (11). Its other state (13) is well formed,
// and the page also names an image (12) whose samples read like a malformed hex
// string, which is no content stream.
func hexReachPDF() []byte {
	form := "BT /F0 1 Tf <414> Tj ET /X1 Do"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Annots [10 0 R]"+
			" /Resources << /XObject << /X0 5 0 R /Im0 12 0 R >> /Pattern << /P0 7 0 R >> /Font << /T3 8 0 R >> >> >>",
		hexStreamObj("", "q /X0 Do /Im0 Do /Pattern cs /P0 scn 0 0 10 10 re f BT /T3 1 Tf <01> Tj ET Q"),
		hexStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Filter /ASCIIHexDecode"+
			" /Resources << /XObject << /X1 6 0 R /X0 5 0 R >> >>", hex.EncodeToString([]byte(form))+">"),
		hexStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", "BT /F0 1 Tf <41!4> Tj ET"),
		hexStreamObj("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10]"+
			" /XStep 10 /YStep 10 /Resources << >>", "BT /F0 1 Tf <424> Tj ET 0 0 5 5 re f"),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0]"+
			" /CharProcs << /g1 9 0 R >> /Encoding << /Type /Encoding /Differences [1 /g1] >>"+
			" /FirstChar 1 /LastChar 1 /Widths [1] >>",
		hexStreamObj("", "1 0 0 0 1 1 d1 BT /F0 1 Tf <434> Tj ET 0 0 1 1 re f"),
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AS /On /AP << /N << /On 11 0 R /Off 13 0 R >> >> >>",
		hexStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", "BT /F0 1 Tf <444> Tj ET"),
		hexStreamObj("/Type /XObject /Subtype /Image /Width 5 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray", "<414>"),
		hexStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", "0 0 1 1 re f"),
	)
}

// streamHexViolations parses body as the hex string tests do and counts the
// malformed strings it records.
func streamHexViolations(t *testing.T, body []byte) int {
	t.Helper()
	_, violations := content.ParseStreamDataFormatChecked(context.Background(), body)
	n := 0
	for _, v := range violations {
		if errors.Is(v, parser.ErrHexStringOddDigits) || errors.Is(v, parser.ErrHexStringInvalidChar) {
			n++
		}
	}
	return n
}

// TestFixHexString_ReachesEveryDrawnStream pins that the repair reaches every
// kind of content stream the hex string tests judge besides the page's own: a
// form XObject, a form it draws, a tiling pattern, a Type 3 glyph and an
// annotation appearance state (ISO 19005-2, 6.1.6 t1 and t2 state the tests
// over every string a file writes). Each stays a stream, keeps its dictionary
// but its filters, and parses to the same operations with no malformed string
// left; a well-formed stream and an image are not written.
//
// The repair used to read the page's content alone, so compliance kept stating
// the tests for every other stream after remediation.
func TestFixHexString_ReachesEveryDrawnStream(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, hexReachPDF())
	fixes, err := fixHexString(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixHexString: %v", err)
	}
	if got, want := strings.Join(fixes, "\n"), "rewrote 5 content stream(s) holding a malformed hex string"; got != want {
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
		if n := streamHexViolations(t, body); n != 0 {
			t.Errorf("%s still holds %d malformed hex string(s): %q", tc.name, n, body)
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
		want, err := content.ParseStreamData(ctx, original)
		if err != nil {
			t.Fatalf("%s: parsing the source: %v", tc.name, err)
		}
		after, err := content.ParseStreamData(ctx, body)
		if err != nil {
			t.Fatalf("%s: parsing the rewrite: %v", tc.name, err)
		}
		if !sameOperations(after, want) {
			t.Errorf("%s reads as\n  %v\nwant what the source reads as\n  %v", tc.name, after, want)
		}
	}
	for _, obj := range []int{hexReachPage, hexReachImage, hexReachCleanState} {
		if _, rewritten := alloc.Pending()[obj]; rewritten {
			t.Errorf("object %d holds no malformed hex string and was rewritten: %v", obj, alloc.Pending()[obj])
		}
	}
}

// mustStream resolves the source stream at objNum.
func mustStream(t *testing.T, doc *document.Document, objNum int) ast.PdfStream {
	t.Helper()
	obj, err := doc.Resolve(context.Background(), ast.PdfIndirectRef{ObjectNum: objNum})
	if err != nil {
		t.Fatalf("resolving object %d: %v", objNum, err)
	}
	s, ok := obj.(ast.PdfStream)
	if !ok {
		t.Fatalf("object %d is %T, want a stream", objNum, obj)
	}
	return s
}

// TestFixHexString_KeepsAnInlineImage pins that respelling a string carries an
// inline image across byte for byte. Re-serialising the parsed operations, as
// the repair used to, wrote "<< ... >> BI" with no ID, data or EI (ISO
// 32000-1, 8.9.7), a page no reader can parse.
func TestFixHexString_KeepsAnInlineImage(t *testing.T) {
	t.Parallel()

	image := "BI /W 1 /H 1 /BPC 8 /CS /G ID \x80 EI"
	got := rewrittenContent(t, hexStringPDF("<414> Tj ET q "+image+" Q BT"))
	if got == "" {
		t.Fatal("the content stream was not rewritten")
	}
	if !strings.Contains(got, image) {
		t.Errorf("the inline image did not survive the rewrite:\n%q", got)
	}
	if !strings.Contains(got, "<4140> Tj") {
		t.Errorf("the malformed hex string was not respelled:\n%q", got)
	}
}

// transparentHexFormPDF builds a PDF/A-1 page drawing a form XObject (object
// 5) that carries a transparency group and shows body.
func transparentHexFormPDF(body string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /X0 5 0 R >> >> >>",
		hexStreamObj("", "/X0 Do"),
		hexStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Group << /S /Transparency >>", body),
	)
}

// TestFixHexString_KeepsTheTransparencyRepair runs the real earlier fixer that
// writes the same form: transparency, ahead of hex-string in the registry,
// drops the form's transparency group (ISO 19005-1, 6.4), and the respelling
// must build on that stream, not the document's.
func TestFixHexString_KeepsTheTransparencyRepair(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, transparentHexFormPDF("BT /F0 1 Tf <414> Tj ET"))
	if _, err := fixTransparency(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixTransparency: %v", err)
	}
	seeded, ok := pendingStream(t, alloc, hexReachForm)
	if !ok || seeded.Dict["Group"] != nil {
		t.Fatalf("control: transparency did not drop the group of object %d, so there is no earlier edit to lose", hexReachForm)
	}

	if _, err := fixHexString(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixHexString: %v", err)
	}
	got, _ := pendingStream(t, alloc, hexReachForm)
	body, err := got.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the form: %v", err)
	}
	if !bytes.Contains(body, []byte("<4140>")) {
		t.Fatalf("the form holds %q: the string was not respelled, so the check below proves nothing", body)
	}
	if got.Dict["Group"] != nil {
		t.Errorf("/Group is back: the respelling rebuilt the form from the document and discarded "+
			"transparency's write; dict = %v", got.Dict)
	}
}

// TestFixHexString_LaterFixersKeepTheRepair runs the real later fixers that
// write the same streams, unknown-ops and nested-resources, and pins that each
// builds on the respelled body instead of the document's.
func TestFixHexString_LaterFixersKeepTheRepair(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		form  string
		later func(context.Context, *testing.T, *document.Document, *writer.ObjAllocator)
		check func(*testing.T, ast.PdfStream, []byte)
	}{
		{
			name: "unknown-ops rewrites an exponent in the form",
			form: "BT /F0 1 Tf <414> Tj ET 1.5e1 w",
			later: func(ctx context.Context, t *testing.T, doc *document.Document, alloc *writer.ObjAllocator) {
				t.Helper()
				if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
					t.Fatalf("fixUnknownOps: %v", err)
				}
			},
			check: func(t *testing.T, _ ast.PdfStream, body []byte) {
				t.Helper()
				if bytes.Contains(body, []byte("1.5e1")) {
					t.Fatalf("the form holds %q: the exponent was not rewritten, so the check proves nothing", body)
				}
			},
		},
		{
			name: "nested-resources gives the form its resources",
			form: "q /CS0 cs 0.5 sc BT /F0 1 Tf <414> Tj ET Q",
			later: func(ctx context.Context, t *testing.T, doc *document.Document, alloc *writer.ObjAllocator) {
				t.Helper()
				if _, err := fixNestedResources(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
					t.Fatalf("fixNestedResources: %v", err)
				}
			},
			check: func(t *testing.T, got ast.PdfStream, _ []byte) {
				t.Helper()
				if got.Dict["Resources"] == nil {
					t.Fatalf("nested-resources gave the form no /Resources, so the check proves nothing: %v", got.Dict)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, alloc := newTestDoc(t, exponentPagePDF("/X0 Do", tc.form, "", ""))
			if _, err := fixHexString(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
				t.Fatalf("fixHexString: %v", err)
			}
			if _, ok := pendingStream(t, alloc, hexReachForm); !ok {
				t.Fatalf("control: the hex fixer did not rewrite object %d", hexReachForm)
			}
			tc.later(ctx, t, doc, alloc)

			got, _ := pendingStream(t, alloc, hexReachForm)
			body, err := got.Bytes(ctx)
			if err != nil {
				t.Fatalf("reading the form: %v", err)
			}
			tc.check(t, got, body)
			if n := streamHexViolations(t, body); n != 0 || !bytes.Contains(body, []byte("<4140>")) {
				t.Errorf("the form holds %q: the later fixer rebuilt it from the document and discarded the respelling", body)
			}
		})
	}
}
