package compliance

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// sweepFixture returns a document whose objects fail every check the object
// sweep collects for: a stream with /F, one with LZWDecode, one with a
// misspelt filter, a stream whose /Length is wrong, a type 4 function past the
// integer limit that a Separation the page draws reaches, a name past its
// length limit, a metadata stream another object references, and file
// specifications written as objects of their own and inline.
func sweepFixture(t *testing.T) (*document.Document, ast.PdfDict) {
	t.Helper()
	longName := "/" + strings.Repeat("N", 200)
	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?><x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"></rdf:RDF></x:xmpmeta><?xpacket end="w"?>`
	return objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(a) 9 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R"+
			" /Resources << /ColorSpace << /CS0 [/Separation /Spot /DeviceGray 10 0 R] >> >>"+
			" /Annots [<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 1 1] /FS << /Type /Filespec /F (x.txt) /EF << /F 8 0 R >> >> >>]"+
			" /Metadata 11 0 R >>",
		"<< /Length 13 >>\nstream\n/CS0 cs 1 scn\nendstream",
		"<< /Length 4 /F (external.bin) >>\nstream\ndata\nendstream",
		"<< /Length 4 /Filter /LZWDecode >>\nstream\ndata\nendstream",
		"<< /Length 4 /Filter [/Flatedecode] "+longName+" 1 >>\nstream\ndata\nendstream",
		"<< /Type /EmbeddedFile /Length 99 >>\nstream\ndata\nendstream",
		"<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 8 0 R >> >>",
		"<< /FunctionType 4 /Domain [0 1] /Range [0 1] /Length 20 >>\nstream\n{ 99999999999 pop }\nendstream",
		"<< /Type /Metadata /Subtype /XML /Length "+strconv.Itoa(len(xmp))+" >>\nstream\n"+xmp+"\nendstream",
	)
}

// TestObjectSweep_FindsWhatEachWalkFinds pins that every check the object
// sweep of a validation run feeds returns what it returns when it walks the
// objects itself, order included, at every PDF/A part and at PDF/UA: the
// groups that read the sweep, run once on a context carrying it and once on
// one without, return the same violations.
func TestObjectSweep_FindsWhatEachWalkFinds(t *testing.T) {
	t.Parallel()
	groups := map[string]checkGroup{
		"file structure": checkFileStructureGroup,
		"xmp":            checkXMPGroup,
		"embedded":       checkEmbeddedGroup,
		"document":       checkDocumentGroup,
		"accessibility":  checkAccessibilityGroup,
	}
	for _, profile := range []string{"PDF/A-1b", "PDF/A-2b", "PDF/A-3b", "PDF/A-4", "PDF/A-4f", "PDF/UA-1", "PDF/UA-2"} {
		t.Run(profile, func(t *testing.T) {
			t.Parallel()
			doc, catalog := sweepFixture(t)
			ctx := document.OpenedObjectsOnly(context.Background())
			doc.SetComplianceMode(true)
			sweep := newObjectSweep(doc, catalog, profile)
			resolveAllObjects(ctx, doc, sweep)
			if !sweep.complete {
				t.Fatal("the sweep did not visit every object")
			}
			if len(sweep.fileSpecs) == 0 {
				t.Fatal("the sweep found no file specification")
			}
			vctx := buildValidationCtx(ctx, doc, catalog, profile)
			swept := withSweptObjects(ctx, sweep)
			for name, group := range groups {
				got := group(swept, vctx)
				want := group(ctx, vctx)
				if !slices.EqualFunc(got, want, func(a, b Violation) bool { return a.Error() == b.Error() }) {
					t.Errorf("%s group: with the sweep %d violations %v, walking %d %v", name, len(got), got, len(want), want)
				}
			}
			if !IsPDFUA(profile) {
				f := sweep.structure
				for name, n := range map[string]int{
					"external keys":   len(f.externalKeys),
					"LZW":             len(f.lzw),
					"numeric limits":  len(f.numericLimits),
					"stream lengths":  len(f.streamLengths),
					"type 4 function": len(f.type4),
				} {
					if n == 0 {
						t.Errorf("the sweep recorded no %s finding", name)
					}
				}
				if PDFAPart(profile) > 1 && len(f.nonStandardFilters) == 0 {
					t.Error("the sweep recorded no non-standard filter")
				}
				if len(sweep.metadata) == 0 {
					t.Error("the sweep found no metadata stream")
				}
			}
		})
	}
}

// TestObjectSweep_IncompleteIsNotHandedOn pins that a sweep ctx cut short is
// not handed to the check groups, which then walk the objects themselves
// rather than read a part of the document as the whole.
func TestObjectSweep_IncompleteIsNotHandedOn(t *testing.T) {
	t.Parallel()
	doc, catalog := sweepFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sweep := newObjectSweep(doc, catalog, "PDF/A-2b")
	resolveAllObjects(ctx, doc, sweep)
	if sweep.complete {
		t.Fatal("a sweep under a done context reads as complete")
	}
	if got := sweptObjectsOf(withSweptObjects(context.Background(), sweep), doc); got != nil {
		t.Error("an incomplete sweep is handed on")
	}
	complete := newObjectSweep(doc, catalog, "PDF/A-2b")
	resolveAllObjects(context.Background(), doc, complete)
	other, _ := sweepFixture(t)
	if got := sweptObjectsOf(withSweptObjects(context.Background(), complete), other); got != nil {
		t.Error("a sweep of one document is read for another")
	}
}
