package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// embeddedFilePDF builds a document carrying both constructs ISO 19005-1
// forbids: a /Names/EmbeddedFiles name tree and a file specification with /EF.
// When namesIndirect is true the name dictionary is its own object, which is
// the shape real producers write and a different code path from the inline one.
func embeddedFilePDF(namesIndirect bool) []byte {
	names := "<< /EmbeddedFiles 5 0 R >>"
	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R /Names " + names + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 6 0 R >> >>",
		"<< /Names [(a.txt) 4 0 R] >>",
		"<< /Length 3 >>\nstream\nabc\nendstream",
	}
	if namesIndirect {
		bodies[0] = "<< /Type /Catalog /Pages 2 0 R /Names 7 0 R >>"
		bodies = append(bodies, "<< /EmbeddedFiles 5 0 R >>")
	}
	return buildStructPDF(bodies...)
}

// runCatalogCleanup drives the whole fixer, not the repair alone, because the
// wiring is what was broken: clause 6.1.11 reached a fixer that only knew the
// PDF/A-4 Perms meaning of it.
func runCatalogCleanup(t *testing.T, data []byte, profile string, preserve bool) (*ast.PdfDict, ast.PdfDict, []string) {
	t.Helper()

	doc, alloc := newTestDoc(t, data)
	fixes, err := fixCatalogCleanup(context.Background(), alloc, doc,
		Options{PreserveContent: preserve}, nil, profile)
	if err != nil {
		t.Fatalf("fixCatalogCleanup: %v", err)
	}
	var catalog *ast.PdfDict
	if d, ok := alloc.Pending()[1].(ast.PdfDict); ok {
		catalog = &d
	}
	spec, _ := alloc.Pending()[4].(ast.PdfDict)
	return catalog, spec, fixes
}

// TestRemovePDFA1EmbeddedFiles_StripsBothConstructs pins the repair of
// ISO 19005-1 6.1.11: t2 forbids the /EmbeddedFiles name entry and t1 forbids
// /EF on a file specification. 70 corpus cells reported these with litho and
// veraPDF in complete agreement, unrepaired even in aggressive mode.
func TestRemovePDFA1EmbeddedFiles_StripsBothConstructs(t *testing.T) {
	t.Parallel()

	catalog, spec, fixes := runCatalogCleanup(t, embeddedFilePDF(false), "PDF/A-1b", false)
	if catalog == nil {
		t.Fatalf("the catalog was not rewritten; fixes=%v", fixes)
	}
	names, ok := (*catalog)["Names"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Names is not an inline dictionary after the repair: %v", (*catalog)["Names"])
	}
	if _, present := names["EmbeddedFiles"]; present {
		t.Error("/EmbeddedFiles survived in the catalog name dictionary")
	}
	if spec == nil {
		t.Fatalf("the file specification was not rewritten; fixes=%v", fixes)
	}
	if _, present := spec["EF"]; present {
		t.Error("/EF survived on the file specification")
	}
	// The specification itself must remain: the rule forbids the key, not the
	// reference to an external file.
	if _, present := spec["F"]; !present {
		t.Error("the repair removed more than /EF from the file specification")
	}
}

// TestRemovePDFA1EmbeddedFiles_HandlesAnIndirectNameDictionary covers the other
// branch: when /Names is its own object it is that object which must be
// rewritten, not the catalog.
func TestRemovePDFA1EmbeddedFiles_HandlesAnIndirectNameDictionary(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, embeddedFilePDF(true))
	if _, err := fixCatalogCleanup(context.Background(), alloc, doc,
		Options{PreserveContent: false}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixCatalogCleanup: %v", err)
	}
	names, ok := alloc.Pending()[7].(ast.PdfDict)
	if !ok {
		t.Fatal("the indirect name dictionary was not rewritten")
	}
	if _, present := names["EmbeddedFiles"]; present {
		t.Error("/EmbeddedFiles survived in the indirect name dictionary")
	}
}

// TestRemovePDFA1EmbeddedFiles_PreserveContentRefuses is the first negative
// control. Deleting an attachment destroys content the author put there, so
// standard mode must leave it alone.
func TestRemovePDFA1EmbeddedFiles_PreserveContentRefuses(t *testing.T) {
	t.Parallel()

	catalog, spec, _ := runCatalogCleanup(t, embeddedFilePDF(false), "PDF/A-1b", true)
	if catalog != nil {
		if names, ok := (*catalog)["Names"].(ast.PdfDict); ok {
			if _, present := names["EmbeddedFiles"]; !present {
				t.Error("/EmbeddedFiles was removed while preserving content")
			}
		}
	}
	if spec != nil {
		if _, present := spec["EF"]; !present {
			t.Error("/EF was removed while preserving content")
		}
	}
}

// TestRemovePDFA1EmbeddedFiles_LeavesPDFA3Alone is the control that matters
// most. Parts 2 and later permit embedded files, and PDF/A-3 is the basis of
// Factur-X, whose invoice payload IS an embedded file. Removing it would
// destroy the document's reason for existing, so the part check is load-bearing.
//
// The profile spelling is exact on purpose: [compliance.PDFAPart] matches whole
// identifiers, and the level-less "PDF/A-1" returns 0, which would make this
// control pass while testing nothing.
func TestRemovePDFA1EmbeddedFiles_LeavesPDFA3Alone(t *testing.T) {
	t.Parallel()

	for _, profile := range []string{"PDF/A-3b", "PDF/A-2b", "PDF/A-4f"} {
		catalog, spec, _ := runCatalogCleanup(t, embeddedFilePDF(false), profile, false)
		if catalog != nil {
			if names, ok := (*catalog)["Names"].(ast.PdfDict); ok {
				if _, present := names["EmbeddedFiles"]; !present {
					t.Errorf("%s: /EmbeddedFiles was removed from a profile that permits it", profile)
				}
			}
		}
		if spec != nil {
			if _, present := spec["EF"]; !present {
				t.Errorf("%s: /EF was removed from a profile that permits embedded files", profile)
			}
		}
	}
}
