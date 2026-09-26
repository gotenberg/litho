package remediation

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// longKey is an over-long resource name: 150 bytes, past the 127-byte cap of
// ISO 19005-2, 6.1.13 t4, written with a #20 escape as issue6151.pdf writes
// its font name.
var longKey = strings.Repeat("R", 70) + " " + strings.Repeat("S", 79)

// longKeySpelled is longKey as a PDF name token.
var longKeySpelled = "/" + strings.ReplaceAll(longKey, " ", "#20")

// streamBody renders a stream object with dict entries and data.
func streamBody(dict, data string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

// liveStreamOps parses the content stream at objNum as the writer will
// serialize it, the replacement queued in alloc first.
func liveStreamOps(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, objNum int) []content.Operation {
	t.Helper()
	ctx := context.Background()
	stream, ok := liveStream(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
	if !ok {
		t.Fatalf("object %d is no stream", objNum)
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		t.Fatalf("decoding object %d: %v", objNum, err)
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		t.Fatalf("parsing object %d: %v", objNum, err)
	}
	return ops
}

// resourceUse returns the resource category and name an operation addresses
// (ISO 32000-1, Table 33), or no category.
func resourceUse(op content.Operation) (ast.PdfName, ast.PdfName) {
	nameAt := func(i int) ast.PdfName {
		if i < 0 || i >= len(op.Operands) {
			return ""
		}
		name, _ := op.Operands[i].(ast.PdfName)
		return name
	}
	switch op.Operator {
	case "Tf":
		return "Font", nameAt(0)
	case "cs", "CS":
		if name := nameAt(0); name != "Pattern" && !strings.HasPrefix(string(name), "Device") {
			return "ColorSpace", name
		}
	case "gs":
		return "ExtGState", nameAt(0)
	case "Do":
		return "XObject", nameAt(0)
	case "sh":
		return "Shading", nameAt(0)
	case "scn", "SCN":
		return "Pattern", nameAt(len(op.Operands) - 1)
	case "BDC", "DP":
		return "Properties", nameAt(1)
	case "BI":
		if dict, ok := op.Operands[0].(ast.PdfDict); ok {
			name, _ := dict["CS"].(ast.PdfName)
			return "ColorSpace", name
		}
	}
	return "", ""
}

// checkResolves fails unless every resource name the stream at objNum
// addresses resolves in the live resource dictionary res and is within the
// cap. It returns how many it checked.
func checkResolves(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, objNum int, res ast.PdfDict) int {
	t.Helper()
	return checkResolvesCapped(t, alloc, doc, objNum, res, true)
}

// checkResolvesCapped is [checkResolves] with the cap checked only when capped
// is set, for a fixture read before the rename.
func checkResolvesCapped(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, objNum int, res ast.PdfDict, capped bool) int {
	t.Helper()
	ctx := context.Background()
	checked := 0
	for _, op := range liveStreamOps(t, alloc, doc, objNum) {
		category, name := resourceUse(op)
		if category == "" || name == "" {
			continue
		}
		checked++
		if _, ok := liveDictValue(ctx, alloc, doc, res[category])[name]; !ok {
			t.Errorf("object %d: %s addresses /%s… which /%s does not hold", objNum, op.Operator, string(name)[:min(len(name), 12)], category)
		}
		if capped && len(name) > pdfANameMaxLen {
			t.Errorf("object %d: %s addresses a name of %d bytes, want at most %d", objNum, op.Operator, len(name), pdfANameMaxLen)
		}
	}
	return checked
}

// liveResourcesOf returns the live /Resources dictionary of the object at
// objNum, a dictionary or a stream.
func liveResourcesOf(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, objNum int) ast.PdfDict {
	t.Helper()
	ctx := context.Background()
	ref := ast.PdfIndirectRef{ObjectNum: objNum}
	if stream, ok := liveStream(ctx, alloc, doc, ref); ok {
		return liveDictValue(ctx, alloc, doc, stream.Dict["Resources"])
	}
	return liveDictValue(ctx, alloc, doc, resolveFromAllocOrDoc(ctx, alloc, doc, ref)["Resources"])
}

// assembleObjStmPDF renders objs as a PDF whose objects listed in compressed
// are stored in one object stream, so their cross-reference entries are type
// 2 (ISO 32000-1, 7.5.7, Table 18), as real producers store page and resource
// dictionaries. Object len(objs)+1 is the object stream and len(objs)+2 the
// cross-reference stream.
func assembleObjStmPDF(objs []string, compressed map[int]bool) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n")
	offsets := make(map[int]int)
	var header, body strings.Builder
	for i, o := range objs {
		num := i + 1
		if compressed[num] {
			fmt.Fprintf(&header, "%d %d ", num, body.Len())
			body.WriteString(o + "\n")
			continue
		}
		offsets[num] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, o)
	}
	stmNum, xrefNum := len(objs)+1, len(objs)+2
	payload := header.String() + "\n" + body.String()
	offsets[stmNum] = buf.Len()
	fmt.Fprintf(&buf, "%d 0 obj\n<< /Type /ObjStm /N %d /First %d /Length %d >>\nstream\n%s\nendstream\nendobj\n",
		stmNum, len(compressed), header.Len()+1, len(payload), payload)

	offsets[xrefNum] = buf.Len()
	var entries []byte
	put := func(entryType byte, field2 uint32, field3 uint16) {
		entries = append(entries, entryType,
			byte(field2>>24), byte(field2>>16), byte(field2>>8), byte(field2),
			byte(field3>>8), byte(field3))
	}
	put(xref.XRefTypeFree, 0, 65535)
	index := 0
	for num := 1; num <= xrefNum; num++ {
		if compressed[num] {
			put(xref.XRefTypeCompressed, uint32(stmNum), uint16(index))
			index++
			continue
		}
		put(xref.XRefTypeNormal, uint32(offsets[num]), 0)
	}
	fmt.Fprintf(&buf, "%d 0 obj\n<< /Type /XRef /Size %d /W [1 4 2] /Root 1 0 R /Length %d >>\nstream\n%s\nendstream\nendobj\n",
		xrefNum, xrefNum+1, len(entries), entries)
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", offsets[xrefNum])
	return buf.Bytes()
}

// TestFixNameUTF8_RenamesOverLongResourceNamesInEveryScope pins the rename of
// an over-long resource name (ISO 19005-2, 6.1.13 t4) in each resource
// dictionary a content stream resolves names through (ISO 32000-1, 7.8.3):
// the key and every use change together, so each use still resolves and
// neither is past the cap. The fixer used to leave every such name in place.
func TestFixNameUTF8_RenamesOverLongResourceNamesInEveryScope(t *testing.T) {
	t.Parallel()

	const catalog = "<< /Type /Catalog /Pages 2 0 R >>"
	pages := "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
	page := func(extra string) string {
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " + extra + " >>"
	}
	const font = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"

	for _, tc := range []struct {
		name       string
		objs       []string
		compressed map[int]bool
		// stream is the content stream checked, and resOf the object whose
		// /Resources it resolves names through.
		stream, resOf int
	}{
		{
			name: "page",
			objs: []string{
				catalog, pages,
				page(fmt.Sprintf("/Resources << /Font << %s 5 0 R >> >>", longKeySpelled)),
				streamBody("", fmt.Sprintf("BT %s 12 Tf (x) Tj ET", longKeySpelled)),
				font,
			},
			stream: 4, resOf: 3,
		},
		{
			name: "inherited",
			objs: []string{
				catalog,
				fmt.Sprintf("<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << /ColorSpace << %s /DeviceRGB >> >> >>", longKeySpelled),
				page(""),
				streamBody("", fmt.Sprintf("%s cs 1 0 0 sc 0 0 10 10 re f", longKeySpelled)),
			},
			stream: 4, resOf: 2,
		},
		{
			name: "form",
			objs: []string{
				catalog, pages,
				page("/Resources << /XObject << /Fm0 5 0 R >> >>"),
				streamBody("", "/Fm0 Do"),
				streamBody(fmt.Sprintf("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /Font << %s 6 0 R >> >>", longKeySpelled),
					fmt.Sprintf("BT %s 12 Tf (x) Tj ET", longKeySpelled)),
				font,
			},
			stream: 5, resOf: 5,
		},
		{
			name: "tiling pattern",
			objs: []string{
				catalog, pages,
				page("/Resources << /Pattern << /P0 5 0 R >> >>"),
				streamBody("", "/Pattern cs /P0 scn 0 0 10 10 re f"),
				streamBody(fmt.Sprintf("/PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 1 1] /XStep 1 /YStep 1 /Resources << /ExtGState << %s << /LW 1 >> >> >>", longKeySpelled),
					fmt.Sprintf("%s gs 0 0 1 1 re f", longKeySpelled)),
			},
			stream: 5, resOf: 5,
		},
		{
			name: "type 3 glyph",
			objs: []string{
				catalog, pages,
				page("/Resources << /Font << /F3 5 0 R >> >>"),
				streamBody("", "BT /F3 12 Tf (a) Tj ET"),
				fmt.Sprintf("<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1 1] /FontMatrix [1 0 0 1 0 0] /CharProcs << /a 6 0 R >>"+
					" /Encoding << /Differences [97 /a] >> /FirstChar 97 /LastChar 97 /Widths [1] /Resources << /XObject << %s 7 0 R >> >> >>", longKeySpelled),
				streamBody("", fmt.Sprintf("1 0 d0 %s Do", longKeySpelled)),
				streamBody("/Type /XObject /Subtype /Form /BBox [0 0 1 1]", ""),
			},
			stream: 6, resOf: 5,
		},
		{
			name: "appearance",
			objs: []string{
				catalog, pages,
				page("/Annots [5 0 R] /Resources << >>"),
				streamBody("", ""),
				"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /AP << /N 6 0 R >> >>",
				streamBody(fmt.Sprintf("/BBox [0 0 10 10] /Resources << /Font << %s 7 0 R >> >>", longKeySpelled),
					fmt.Sprintf("BT %s 10 Tf (a) Tj ET", longKeySpelled)),
				font,
			},
			stream: 6, resOf: 6,
		},
		{
			name: "soft mask group",
			objs: []string{
				catalog, pages,
				page("/Resources << /ExtGState << /GS0 << /SMask << /S /Luminosity /G 5 0 R >> >> >> >>"),
				streamBody("", "/GS0 gs 0 0 10 10 re f"),
				streamBody(fmt.Sprintf("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Group << /S /Transparency /CS /DeviceGray >>"+
					" /Resources << /Shading << %s 6 0 R >> >>", longKeySpelled),
					fmt.Sprintf("%s sh", longKeySpelled)),
				"<< /ShadingType 2 /ColorSpace /DeviceGray /Coords [0 0 10 0] /Function << /FunctionType 2 /Domain [0 1] /N 1 >> >>",
			},
			stream: 5, resOf: 5,
		},
		{
			name: "properties",
			objs: []string{
				catalog, pages,
				page(fmt.Sprintf("/Resources << /Properties << %s << /Type /OCG /Name (Layer) >> >> >>", longKeySpelled)),
				streamBody("", fmt.Sprintf("/OC %s BDC 0 0 10 10 re f EMC", longKeySpelled)),
			},
			stream: 4, resOf: 3,
		},
		{
			name: "object stream",
			objs: []string{
				catalog, pages,
				page(fmt.Sprintf("/Resources << /Font << %s 5 0 R >> >>", longKeySpelled)),
				streamBody("", fmt.Sprintf("BT %s 12 Tf (x) Tj ET", longKeySpelled)),
				font,
			},
			compressed: map[int]bool{2: true, 3: true, 5: true},
			stream:     4, resOf: 3,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			pdf := assemblePDF(tc.objs)
			if tc.compressed != nil {
				pdf = assembleObjStmPDF(tc.objs, tc.compressed)
			}
			doc, alloc := newTestDoc(t, pdf)
			if tc.compressed != nil {
				if got, _ := doc.XRefEntry(tc.resOf); got.Type != xref.XRefTypeCompressed {
					t.Fatalf("fixture: object %d has xref type %d, want it in an object stream", tc.resOf, got.Type)
				}
			}
			if checkResolvesCapped(t, alloc, doc, tc.stream, liveResourcesOf(t, alloc, doc, tc.resOf), false) == 0 {
				t.Fatal("fixture: the stream addresses no resource")
			}

			fixes, err := fixNameUTF8(ctx, alloc, doc, Options{}, nameLengthViolations, "PDF/A-2b")
			if err != nil {
				t.Fatalf("fixNameUTF8: %v", err)
			}
			if !slices.ContainsFunc(fixes, func(s string) bool { return strings.HasPrefix(s, "renamed 1 over-long resource name") }) {
				t.Errorf("fixes %q report no rename", fixes)
			}
			if checkResolves(t, alloc, doc, tc.stream, liveResourcesOf(t, alloc, doc, tc.resOf)) == 0 {
				t.Fatal("the stream addresses no resource after the rename")
			}
			for name := range liveDictValue(ctx, alloc, doc, liveResourcesOf(t, alloc, doc, tc.resOf)) {
				for key := range liveDictValue(ctx, alloc, doc, liveResourcesOf(t, alloc, doc, tc.resOf)[name]) {
					if len(key) > pdfANameMaxLen {
						t.Errorf("/%s keeps a key of %d bytes", name, len(key))
					}
				}
			}
		})
	}
}

// TestFixNameUTF8_KeepsInlineImageData pins that renaming the colour space an
// inline image names (ISO 32000-1, 8.9.7) splices its dictionary and carries
// its data across byte for byte: the data holds bytes no token reading keeps.
func TestFixNameUTF8_KeepsInlineImageData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const data = "\x00\xff/(\n"
	stream := fmt.Sprintf("q 4 0 0 1 0 0 cm BI /W 4 /H 1 /CS %s /BPC 8 ID %s EI Q", longKeySpelled, data)
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /ColorSpace << %s /DeviceGray >> >> >>", longKeySpelled),
		streamBody("", stream),
	}))

	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nameLengthViolations, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	if checkResolves(t, alloc, doc, 4, liveResourcesOf(t, alloc, doc, 3)) != 1 {
		t.Fatal("want the inline image's colour space checked")
	}
	live, _ := liveStream(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 4})
	body, err := doc.DecodeStream(ctx, live)
	if err != nil {
		t.Fatalf("decoding the content: %v", err)
	}
	if !bytes.Contains(body, []byte(" ID "+data+" EI")) {
		t.Errorf("the image data did not survive byte for byte: %q", body)
	}
	if bytes.Contains(body, []byte(longKeySpelled)) {
		t.Errorf("the image dictionary keeps the over-long name: %q", body)
	}
}

// TestFixNameUTF8_ShortensNamesThatAreNoResourceKey pins that an over-long
// name no resource dictionary holds, a marked-content tag here, is shortened
// where it stands, to one spelling at every place.
func TestFixNameUTF8_ShortensNamesThatAreNoResourceKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tag := "/" + strings.Repeat("T", 200)
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		streamBody("", fmt.Sprintf("%s BMC 0 0 1 1 re f EMC %s <</Alt (x)>> BDC EMC", tag, tag)),
	}))

	fixes, err := fixNameUTF8(ctx, alloc, doc, Options{}, nameLengthViolations, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	if !slices.Contains(fixes, "shortened 2 over-long name(s) in content streams") {
		t.Errorf("fixes %q, want the two tags reported", fixes)
	}
	var tags []ast.PdfName
	for _, op := range liveStreamOps(t, alloc, doc, 4) {
		if op.Operator == "BMC" || op.Operator == "BDC" {
			tag, _ := op.Operands[0].(ast.PdfName)
			tags = append(tags, tag)
		}
	}
	if len(tags) != 2 || tags[0] != tags[1] || len(tags[0]) > pdfANameMaxLen || !strings.HasPrefix(string(tags[0]), "TTT") {
		t.Errorf("tags %q, want one shortened spelling of the tag twice", tags)
	}
}

// TestFixNameUTF8_RenamesDefaultAppearanceFonts pins that an over-long /DR
// font key is renamed in the /DA strings that name it (ISO 32000-1, 12.7.3.3),
// which name it from inside a string.
func TestFixNameUTF8_RenamesDefaultAppearanceFonts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	da := fmt.Sprintf("(%s 12 Tf 0 g)", longKeySpelled)
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		fmt.Sprintf("<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] /DR << /Font << %s 6 0 R >> >> /DA %s >> >>", longKeySpelled, da),
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Annots [5 0 R] >>",
		streamBody("", ""),
		fmt.Sprintf("<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /Rect [0 0 10 10] /P 3 0 R /DA %s >>", da),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}))

	fixes, err := fixNameUTF8(ctx, alloc, doc, Options{}, nameLengthViolations, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	if !slices.Contains(fixes, "renamed over-long resource names in 2 default-appearance string(s)") {
		t.Errorf("fixes %q, want the two /DA strings reported", fixes)
	}
	acroForm := liveDictValue(ctx, alloc, doc, resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})["AcroForm"])
	fonts := liveDictValue(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, acroForm["DR"])["Font"])
	widget := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 5})
	for _, holder := range []ast.PdfDict{acroForm, widget} {
		s, _ := holder["DA"].(ast.PdfString)
		ops, err := content.ParseStreamData(ctx, []byte(s))
		if err != nil || len(ops) != 2 {
			t.Fatalf("/DA %q parses to %v, %v", s, ops, err)
		}
		name, _ := ops[0].Operands[0].(ast.PdfName)
		if _, ok := fonts[name]; !ok || len(name) > pdfANameMaxLen {
			t.Errorf("/DA %q names a font /DR does not hold within the cap", s)
		}
	}
}

// TestFixNameUTF8_RenamesAKeyEveryDictionaryHolds pins the one spelling for
// the whole document: two dictionaries that hold the same over-long key for
// different fonts both take the new spelling, so each stream still finds its
// own font.
func TestFixNameUTF8_RenamesAKeyEveryDictionaryHolds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << %s 6 0 R >> /XObject << /Fm0 5 0 R >> >> >>", longKeySpelled),
		streamBody("", fmt.Sprintf("BT %s 12 Tf (x) Tj ET /Fm0 Do", longKeySpelled)),
		streamBody(fmt.Sprintf("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources << /Font << %s 7 0 R >> >>", longKeySpelled),
			fmt.Sprintf("BT %s 12 Tf (y) Tj ET", longKeySpelled)),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>",
	}))

	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nameLengthViolations, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	checkResolves(t, alloc, doc, 4, liveResourcesOf(t, alloc, doc, 3))
	checkResolves(t, alloc, doc, 5, liveResourcesOf(t, alloc, doc, 5))
	for objNum, want := range map[int]int{3: 6, 5: 7} {
		fonts := liveDictValue(ctx, alloc, doc, liveResourcesOf(t, alloc, doc, objNum)["Font"])
		for _, ref := range fonts {
			if ref.(ast.PdfIndirectRef).ObjectNum != want {
				t.Errorf("object %d's font now addresses object %v, want %d", objNum, ref, want)
			}
		}
	}
}

// TestFixNameUTF8_LeavesAKeyAStreamCannotBeReadFor pins the residual: a
// content stream the token walk cannot read to its end may write the key past
// the failure, so no key is renamed and the stream keeps its bytes. The control
// closes the hex string, and the key is renamed.
func TestFixNameUTF8_LeavesAKeyAStreamCannotBeReadFor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, stream string
		renamed      bool
	}{
		{name: "unreadable", stream: fmt.Sprintf("BT %s 12 Tf <4142 ET", longKeySpelled)},
		{name: "control", stream: fmt.Sprintf("BT %s 12 Tf <4142> Tj ET", longKeySpelled), renamed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, alloc := newTestDoc(t, assemblePDF([]string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << %s 5 0 R >> >> >>", longKeySpelled),
				streamBody("", tc.stream),
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
			}))
			if _, complete := contentTokenSplices(ctx, []byte(tc.stream), func(lexer.Token, tokenPlace) (string, bool) { return "", false }); complete != tc.renamed {
				t.Fatalf("fixture: the token walk reads the stream to its end: %v", complete)
			}

			if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nameLengthViolations, "PDF/A-2b"); err != nil {
				t.Fatalf("fixNameUTF8: %v", err)
			}
			if _, pending := alloc.Pending()[4]; pending != tc.renamed {
				t.Errorf("stream rewritten: %v, want %v", pending, tc.renamed)
			}
			fonts := liveDictValue(ctx, alloc, doc, liveResourcesOf(t, alloc, doc, 3)["Font"])
			if _, kept := fonts[ast.PdfName(longKey)]; kept == tc.renamed {
				t.Errorf("key kept: %v, want %v; /Font = %v", kept, !tc.renamed, fonts)
			}
		})
	}
}

// TestFixNameUTF8_WithdrawsARenameADictionaryRefuses pins the retry: a
// dictionary that already holds the new spelling as a key refuses the rename,
// and the repair starts again without it, so the key and its uses stay
// together rather than part. The control holds no such key, and the key is
// renamed.
func TestFixNameUTF8_WithdrawsARenameADictionaryRefuses(t *testing.T) {
	t.Parallel()
	spelled := "/" + strings.ReplaceAll(sanitizeName(longKey), " ", "#20")
	for _, tc := range []struct {
		name, private string
		renamed       bool
	}{
		{name: "refused", private: fmt.Sprintf("%s 1 %s 2", longKeySpelled, spelled)},
		{name: "control", private: fmt.Sprintf("%s 1 /Other 2", longKeySpelled), renamed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, alloc := newTestDoc(t, assemblePDF([]string{
				fmt.Sprintf("<< /Type /Catalog /Pages 2 0 R /PieceInfo << %s >> >>", tc.private),
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << %s 5 0 R >> >> >>", longKeySpelled),
				streamBody("", fmt.Sprintf("BT %s 12 Tf (x) Tj ET", longKeySpelled)),
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
			}))

			if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nameLengthViolations, "PDF/A-2b"); err != nil {
				t.Fatalf("fixNameUTF8: %v", err)
			}
			fonts := liveDictValue(ctx, alloc, doc, liveResourcesOf(t, alloc, doc, 3)["Font"])
			if _, kept := fonts[ast.PdfName(longKey)]; kept == tc.renamed {
				t.Fatalf("key kept: %v, want %v; /Font = %v", kept, !tc.renamed, fonts)
			}
			if checkResolvesCapped(t, alloc, doc, 4, liveResourcesOf(t, alloc, doc, 3), tc.renamed) != 1 {
				t.Fatal("want the Tf checked")
			}
		})
	}
}

// TestFixNameUTF8_KeepsOutputIntentsDefaultColourSpace is the clobber guard
// against the real peer that writes the same dictionary first: output-intent
// (registry ahead of name-utf8) injects /DefaultRGB into the /ColorSpace
// dictionary that holds the over-long key. The rename copies that dictionary
// from the allocator, so the injection survives.
func TestFixNameUTF8_KeepsOutputIntentsDefaultColourSpace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources 5 0 R >>",
		streamBody("", fmt.Sprintf("%s cs 1 0 0 sc 0 0 10 10 re f 0 1 0 rg", longKeySpelled)),
		"<< /ColorSpace 6 0 R >>",
		fmt.Sprintf("<< %s /DeviceRGB >>", longKeySpelled),
	}))
	if injectTestDefaultRGB(t, alloc, doc) == 0 {
		t.Fatal("control: output-intent injected no /DefaultRGB")
	}
	if cs := liveDictValue(ctx, alloc, doc, liveResourcesOf(t, alloc, doc, 3)["ColorSpace"]); cs["DefaultRGB"] == nil || cs[ast.PdfName(longKey)] == nil {
		t.Fatalf("control: output-intent did not write /DefaultRGB beside the over-long key; /ColorSpace = %v", cs)
	}

	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nameLengthViolations, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	checkResolves(t, alloc, doc, 4, liveResourcesOf(t, alloc, doc, 3))
	cs := liveDictValue(ctx, alloc, doc, liveResourcesOf(t, alloc, doc, 3)["ColorSpace"])
	if _, kept := cs["DefaultRGB"]; !kept {
		t.Errorf("/DefaultRGB is gone: the rename copied the document's /ColorSpace; = %v", cs)
	}
	if _, old := cs[ast.PdfName(longKey)]; old {
		t.Errorf("the key was not renamed: %v", cs)
	}
}

// TestFixNameUTF8_KeepsTransparencysFormWrite is the clobber guard against
// the real peer that rewrites the same form XObject first: transparency
// (PDF/A-1, registry ahead of name-utf8) drops the form's /Group. The rename
// writes the form's content back over the allocator's copy, so the dropped
// group stays dropped.
func TestFixNameUTF8_KeepsTransparencysFormWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /Fm0 5 0 R >> >> >>",
		streamBody("", "/Fm0 Do"),
		streamBody(fmt.Sprintf("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Group << /S /Transparency >> /Resources << /Font << %s 6 0 R >> >>", longKeySpelled),
			fmt.Sprintf("BT %s 12 Tf (x) Tj ET", longKeySpelled)),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}))
	if _, err := fixTransparency(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixTransparency: %v", err)
	}
	if stream, ok := alloc.Pending()[5].(ast.PdfStream); !ok || stream.Dict["Group"] != nil {
		t.Fatal("control: transparency did not drop the form's /Group")
	}

	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nameLengthViolations, "PDF/A-1b"); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	if checkResolves(t, alloc, doc, 5, liveResourcesOf(t, alloc, doc, 5)) != 1 {
		t.Fatal("want the form's Tf checked")
	}
	if stream, _ := alloc.Pending()[5].(ast.PdfStream); stream.Dict["Group"] != nil {
		t.Errorf("the form's /Group is back: the rename rebuilt the form from the document; dict = %v", stream.Dict)
	}
}

// TestFixUnknownOps_BuildsOnTheRenamedContent is the clobber guard the other
// way: unknown-ops (a content fixer, after name-utf8) rewrites the stream the
// rename wrote, and must build on it, so both repairs land.
func TestFixUnknownOps_BuildsOnTheRenamedContent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << %s 5 0 R >> >> >>", longKeySpelled),
		streamBody("", fmt.Sprintf("BT %s 12 Tf zz (x) Tj ET", longKeySpelled)),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}))

	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nameLengthViolations, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	if checkResolves(t, alloc, doc, 4, liveResourcesOf(t, alloc, doc, 3)) != 1 {
		t.Fatal("want the Tf checked")
	}
	for _, op := range liveStreamOps(t, alloc, doc, 4) {
		if op.Operator == "zz" {
			t.Error("the undefined operator survived")
		}
	}
}
