package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestFixTrailerID_AddsMissingID(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))
	if _, ok := doc.Index.Trailer["ID"]; ok {
		t.Fatal("fixture already has trailer /ID")
	}

	descs, err := fixTrailerID(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixTrailerID: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	idObj, ok := doc.Index.Trailer["ID"]
	if !ok {
		t.Fatal("trailer /ID not set after fix")
	}
	idArr, ok := idObj.(ast.PdfArray)
	if !ok {
		t.Fatalf("/ID: got %T, want PdfArray", idObj)
	}
	if len(idArr) != 2 {
		t.Fatalf("/ID array length: got %d, want 2", len(idArr))
	}
	first, ok1 := idArr[0].(ast.PdfString)
	second, ok2 := idArr[1].(ast.PdfString)
	if !ok1 || !ok2 {
		t.Fatalf("/ID entries: got %T, %T; want PdfString each", idArr[0], idArr[1])
	}
	if len(first) != 16 || len(second) != 16 {
		t.Errorf("/ID entries must be 16 bytes each, got %d/%d", len(first), len(second))
	}
	if string(first) != string(second) {
		t.Errorf("/ID entries should be identical, got %x vs %x", first, second)
	}
}

func TestFixTrailerID_SkipsIfPresent(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))
	doc.Index.Trailer["ID"] = ast.PdfArray{
		ast.PdfString("preexisting-id-bytes"),
		ast.PdfString("preexisting-id-bytes"),
	}

	descs, err := fixTrailerID(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixTrailerID: %v", err)
	}
	if len(descs) != 0 {
		t.Errorf("expected no descriptions when /ID is present, got %v", descs)
	}
}

// mintedID runs the trailer-id fixer on data at profile with opts, after
// setting the trailer /ID to trailerID when it is not nil, and returns the two
// /ID strings the fixer leaves.
func mintedID(t *testing.T, data []byte, trailerID ast.PdfObject, opts Options, profile string) (first, second string) {
	t.Helper()
	doc, alloc := newTestDoc(t, data)
	if trailerID != nil {
		doc.Index.Trailer["ID"] = trailerID
	}
	if _, err := fixTrailerID(context.Background(), alloc, doc, opts, nil, profile); err != nil {
		t.Fatalf("fixTrailerID: %v", err)
	}
	arr, ok := doc.Index.Trailer["ID"].(ast.PdfArray)
	if !ok || len(arr) != 2 {
		t.Fatalf("/ID: got %v, want a two-element array", doc.Index.Trailer["ID"])
	}
	a, okA := arr[0].(ast.PdfString)
	b, okB := arr[1].(ast.PdfString)
	if !okA || !okB {
		t.Fatalf("/ID entries: got %T, %T; want PdfString each", arr[0], arr[1])
	}
	return string(a), string(b)
}

// TestFixTrailerID_DerivesTheIDFromTheContent pins that a missing /ID is a
// hash of the document bytes and of the options that shape the output, not a
// random draw: the same input and options mint the same /ID, and a change to
// either mints another.
func TestFixTrailerID_DerivesTheIDFromTheContent(t *testing.T) {
	t.Parallel()

	src := minimalCatalogPDF("")
	opts := Options{Lang: "en", PreserveContent: true}
	base, second := mintedID(t, src, nil, opts, "PDF/A-1b")
	if len(base) != 16 || second != base {
		t.Fatalf("minted /ID [%x %x], want two identical 16-byte strings", base, second)
	}
	if again, _ := mintedID(t, src, nil, opts, "PDF/A-1b"); again != base {
		t.Fatalf("the same input and options minted %x, then %x", base, again)
	}

	variants := map[string]struct {
		data    []byte
		opts    Options
		profile string
	}{
		"other bytes":     {minimalCatalogPDF("/Lang (fr)"), opts, "PDF/A-1b"},
		"other profile":   {src, opts, "PDF/A-2b"},
		"aggressive":      {src, Options{Lang: "en"}, "PDF/A-1b"},
		"other language":  {src, Options{Lang: "de", PreserveContent: true}, "PDF/A-1b"},
		"figure alt text": {src, Options{Lang: "en", PreserveContent: true, FigureAlt: "A chart"}, "PDF/A-1b"},
	}
	for name, v := range variants {
		if got, _ := mintedID(t, v.data, nil, v.opts, v.profile); got == base {
			t.Errorf("%s: minted the same /ID %x as the base input", name, got)
		}
	}

	// A rolled-back round reruns the engine over the same document with
	// fixers disabled, and must mint the /ID the first round minted.
	if got, _ := mintedID(t, src, nil, opts.WithDisabled([]string{"hex-string"}), "PDF/A-1b"); got != base {
		t.Errorf("disabled fixers: minted %x, want the first round's %x", got, base)
	}

	pinned := []byte("0123456789abcdef")
	if got, _ := mintedID(t, src, nil, Options{PinnedID: pinned}, "PDF/A-1b"); got != string(pinned) {
		t.Errorf("PinnedID: minted %x, want %x", got, pinned)
	}
}

// TestFixTrailerID_KeepsAValidFirstElement pins ISO 32000-1, 14.4: the first
// /ID string is the file's permanent identifier, so an /ID whose second
// element is missing or empty keeps its first and gains a derived second.
func TestFixTrailerID_KeepsAValidFirstElement(t *testing.T) {
	t.Parallel()

	const permanent = "permanent-id-001"
	for name, id := range map[string]ast.PdfObject{
		"empty second": ast.PdfArray{ast.PdfString(permanent), ast.PdfString("")},
		"one element":  ast.PdfArray{ast.PdfString(permanent)},
	} {
		first, second := mintedID(t, minimalCatalogPDF(""), id, Options{}, "PDF/A-1b")
		if first != permanent {
			t.Errorf("%s: first /ID string %q, want the source's %q", name, first, permanent)
		}
		if len(second) != 16 || second == permanent {
			t.Errorf("%s: second /ID string %x, want 16 derived bytes", name, second)
		}
	}
}
