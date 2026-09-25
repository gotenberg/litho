package remediation

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// longName is an over-long PDF name: 200 bytes, well past the 127-byte
// cap of ISO 19005-2 §6.1.13 test 4.
var longName = strings.Repeat("L", 200)

// resourceRefPDF builds a single-page PDF whose page resources address two
// over-long names, one through a direct /ColorSpace sub-dictionary and one
// through an indirect /XObject sub-dictionary, and whose content stream
// references both. It also plants an over-long /BaseFont value, which no
// content stream addresses by name and which therefore stays repairable.
func resourceRefPDF(t *testing.T) []byte {
	t.Helper()
	stream := fmt.Sprintf("/%s cs\n/%s Do\n", longName, longName+"X")
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R "+
			"/Resources << /ColorSpace << /%s /DeviceGray >> /XObject 5 0 R /Font 6 0 R >> >>", longName),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		fmt.Sprintf("<< /%s 7 0 R >>", longName+"X"),
		"<< /F1 8 0 R >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 1 1] /Length 0 >>\nstream\n\nendstream",
		fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /%s >>", longName+"Font"),
	)
}

// liveDict returns the object as the writer will serialize it: the
// replacement queued in alloc when the fixer rewrote it, otherwise the
// parsed original.
func liveDict(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, objNum int) ast.PdfDict {
	t.Helper()
	if pending, ok := alloc.Pending()[objNum]; ok {
		dict, ok := pending.(ast.PdfDict)
		if !ok {
			t.Fatalf("object %d: pending is %T, want ast.PdfDict", objNum, pending)
		}
		return dict
	}
	resolved, err := doc.Resolve(context.Background(), ast.PdfIndirectRef{ObjectNum: objNum})
	if err != nil {
		t.Fatalf("resolving object %d: %v", objNum, err)
	}
	dict, ok := resolved.(ast.PdfDict)
	if !ok {
		t.Fatalf("object %d: got %T, want ast.PdfDict", objNum, resolved)
	}
	return dict
}

// TestFixNameUTF8_KeepsContentStreamReferencesResolvable is the regression
// guard for the dangling-resource-reference bug: the fixer used to truncate
// an over-long /Resources key to 127 bytes while leaving the content-stream
// operand that names it untouched, so the operand resolved to nothing. The
// name-length violation survived anyway (the long name is still in the
// stream), so the rewrite only broke the document. Every name operand must
// still resolve to a key of the resource dictionary it addresses.
func TestFixNameUTF8_KeepsContentStreamReferencesResolvable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, resourceRefPDF(t))

	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}

	page := liveDict(t, alloc, doc, 3)
	resources, ok := page["Resources"].(ast.PdfDict)
	if !ok {
		t.Fatalf("page /Resources: got %T, want ast.PdfDict", page["Resources"])
	}
	colorSpace, ok := resources["ColorSpace"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/ColorSpace: got %T, want ast.PdfDict", resources["ColorSpace"])
	}
	xObject := liveDict(t, alloc, doc, 5)

	stream, err := document.ResolveStream(ctx, doc, page["Contents"])
	if err != nil {
		t.Fatalf("resolving /Contents: %v", err)
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		t.Fatalf("decoding /Contents: %v", err)
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		t.Fatalf("parsing content stream: %v", err)
	}

	categories := map[string]ast.PdfDict{"cs": colorSpace, "Do": xObject}
	checked := 0
	for _, op := range ops {
		category, watched := categories[op.Operator]
		if !watched || len(op.Operands) != 1 {
			continue
		}
		name, ok := op.Operands[0].(ast.PdfName)
		if !ok {
			continue
		}
		if _, present := category[name]; !present {
			t.Errorf("%s operand /%s… does not resolve to any resource key", op.Operator, string(name)[:16])
		}
		checked++
	}
	if checked != len(categories) {
		t.Fatalf("checked %d name operands, want %d", checked, len(categories))
	}
}

// TestFixNameUTF8_RepairsUnreferencedName pins the other half of the
// contract: declining to rename resource keys must stay narrow. A name no
// other object addresses by name, here a /BaseFont value, is still
// truncated to the 127-byte cap of ISO 19005-2 §6.1.13 test 4.
func TestFixNameUTF8_RepairsUnreferencedName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, resourceRefPDF(t))

	fixes, err := fixNameUTF8(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	if len(fixes) == 0 {
		t.Fatal("fixNameUTF8 reported no fix, want the /BaseFont value repaired")
	}

	font := liveDict(t, alloc, doc, 8)
	baseFont, ok := font["BaseFont"].(ast.PdfName)
	if !ok {
		t.Fatalf("/BaseFont: got %T, want ast.PdfName", font["BaseFont"])
	}
	if len(baseFont) > pdfANameMaxLen {
		t.Errorf("/BaseFont length %d, want <= %d", len(baseFont), pdfANameMaxLen)
	}
}

// TestSanitizeDictNames_DisambiguatesTruncationCollision guards against
// silent data loss: two distinct over-long keys that share a 127-byte
// prefix truncate onto the same name, and without disambiguation the
// second entry overwrites the first.
func TestSanitizeDictNames_DisambiguatesTruncationCollision(t *testing.T) {
	t.Parallel()
	prefix := strings.Repeat("K", pdfANameMaxLen)
	dict := ast.PdfDict{
		ast.PdfName(prefix + "alpha"): ast.PdfInt(1),
		ast.PdfName(prefix + "beta"):  ast.PdfInt(2),
	}

	patched, changed := sanitizeDictNames(dict, nil)
	if !changed {
		t.Fatal("sanitizeDictNames reported no change")
	}
	if len(patched) != len(dict) {
		t.Fatalf("dict lost entries: got %d keys, want %d", len(patched), len(dict))
	}
	for key := range patched {
		if len(key) > pdfANameMaxLen {
			t.Errorf("key length %d, want <= %d", len(key), pdfANameMaxLen)
		}
	}
}

// TestSanitizeDictNames_Deterministic pins deterministic output: the
// disambiguating suffix must not depend on Go's randomized map iteration
// order, or two runs over identical input produce different bytes.
func TestSanitizeDictNames_Deterministic(t *testing.T) {
	t.Parallel()
	prefix := strings.Repeat("K", pdfANameMaxLen)
	build := func() ast.PdfDict {
		return ast.PdfDict{
			ast.PdfName(prefix + "alpha"):   ast.PdfInt(1),
			ast.PdfName(prefix + "beta"):    ast.PdfInt(2),
			ast.PdfName(prefix + "gamma"):   ast.PdfInt(3),
			ast.PdfName(prefix + "delta"):   ast.PdfInt(4),
			ast.PdfName(prefix + "epsilon"): ast.PdfInt(5),
		}
	}

	first, _ := sanitizeDictNames(build(), nil)
	for range 8 {
		next, _ := sanitizeDictNames(build(), nil)
		if len(next) != len(first) {
			t.Fatalf("key count drifted: got %d, want %d", len(next), len(first))
		}
		for key, val := range first {
			if next[key] != val {
				t.Fatalf("key %q mapped to %v, want %v", string(key)[pdfANameMaxLen-1:], next[key], val)
			}
		}
	}
}

// TestSanitizeName_TruncatesOnRuneBoundary guards the UTF-8 contract:
// cutting a name at byte 127 can land inside a multi-byte sequence, and
// the resulting name would be invalid UTF-8, re-violating the very rule
// (ISO 19005-2 §6.1.8, ISO 19005-4 §6.1.7) the fixer exists to satisfy.
func TestSanitizeName_TruncatesOnRuneBoundary(t *testing.T) {
	t.Parallel()
	// "€" is three bytes, so 60 of them span 180 bytes and byte 127 falls
	// mid-sequence.
	got := sanitizeName(strings.Repeat("€", 60))
	if len(got) > pdfANameMaxLen {
		t.Errorf("length %d, want <= %d", len(got), pdfANameMaxLen)
	}
	if !utf8.ValidString(got) {
		t.Errorf("sanitizeName returned invalid UTF-8: %q", got)
	}
}

// TestSanitizeName_ReplacesInvalidBytes keeps the original repair
// behaviour: bytes that are not valid UTF-8 become '_' so the name stays
// visible and distinct.
func TestSanitizeName_ReplacesInvalidBytes(t *testing.T) {
	t.Parallel()
	got := sanitizeName("A\xffB")
	if got != "A_B" {
		t.Errorf("got %q, want %q", got, "A_B")
	}
	if !utf8.ValidString(got) {
		t.Errorf("sanitizeName returned invalid UTF-8: %q", got)
	}
}

// TestCollectResourceKeys_FindsDirectAndIndirectCategories pins the scan
// that decides which names are addressed by name from a content stream:
// a resource category is a direct sub-dictionary in one place and an
// indirect reference in another, and both must be seen.
func TestCollectResourceKeys_FindsDirectAndIndirectCategories(t *testing.T) {
	t.Parallel()
	doc, alloc := newTestDoc(t, resourceRefPDF(t))

	pinned, err := collectResourceKeys(context.Background(), alloc, doc)
	if err != nil {
		t.Fatalf("collectResourceKeys: %v", err)
	}
	for _, want := range []string{longName, longName + "X", "F1"} {
		if !pinned[want] {
			t.Errorf("resource key %q… not collected", want[:min(len(want), 8)])
		}
	}
	if pinned[longName+"Font"] {
		t.Error("/BaseFont value must not be pinned as a resource key")
	}
}

// TestResourceRefPDF_Parses is a fixture guard: an assertion about a
// dangling reference is worthless if the fixture never parsed.
func TestResourceRefPDF_Parses(t *testing.T) {
	t.Parallel()
	data := resourceRefPDF(t)
	if !bytes.Contains(data, []byte(longName)) {
		t.Fatal("fixture lost its over-long name")
	}
	doc, _ := newTestDoc(t, data)
	if n := doc.XRefEntryCount(); n < 8 {
		t.Fatalf("fixture parsed %d objects, want at least 8", n)
	}
}
