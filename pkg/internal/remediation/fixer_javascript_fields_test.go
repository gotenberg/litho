package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// fieldActionsPDF builds a one-page document whose AcroForm field tree carries
// actions under form-field triggers, and whose page lists no annotation, so
// only a walk of the field tree can reach them:
//
//   - object 4, the root field "parent": a JavaScript /AA/K and a GoTo /AA/V.
//   - object 5, a kid with a partial name /T: a Launch /AA/K.
//   - object 6, a kid widget with no /T: a Launch /AA/K. It is no form field
//     (ISO 32000-1, 12.7.3.1), so its /AA is an annotation's and the field
//     walk leaves it to the page walk, as compliance's walkFormFields does.
//   - object 7, a root field whose /Kids lists itself and object 4, a cycle:
//     a Hide /AA/C.
func fieldActionsPDF() []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 7 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /T (parent) /FT /Tx /Kids [5 0 R 6 0 R] /AA << /K << /S /JavaScript /JS (k) >> /V << /S /GoTo /D [3 0 R /Fit] >> >> >>",
		"<< /T (named) /Parent 4 0 R /AA << /K << /S /Launch /F (a.exe) >> >> >>",
		"<< /Type /Annot /Subtype /Widget /Parent 4 0 R /Rect [0 0 10 10] /AA << /K << /S /Launch /F (b.exe) >> >> >>",
		"<< /T (cycle) /FT /Btn /Kids [7 0 R 4 0 R] /AA << /C << /S /Hide /T (x) >> >> >>",
	})
}

// fieldAASites records which /AA entries of [fieldActionsPDF] survive.
type fieldAASites struct {
	parentK, parentV, namedK, widgetK, cycleC bool
}

// TestFixJavaScript_StripsFormFieldActions pins that the fixer walks the
// AcroForm field tree, which the validator judges for forbidden action types
// (ISO 19005-1 6.6.1, ISO 19005-2/3 6.5.1, ISO 19005-4 6.6.1) apart from any
// page.
//
// The fixer walked pages, annotations, the catalog and outlines only, so a
// non-terminal field's /AA kept its JavaScript: litho annots_1b.pdf kept
// "6.5.1 test 1 /JavaScript at form field 1 /AA//K" at PDF/A-2b. The Launch
// and Hide sites are the positive control at PDF/A-4, which permits JavaScript
// and forbids both.
func TestFixJavaScript_StripsFormFieldActions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		profile string
		want    fieldAASites
	}{
		{"PDF/A-1b", fieldAASites{parentV: true, widgetK: true}},
		{"PDF/A-2b", fieldAASites{parentV: true, widgetK: true}},
		{"PDF/A-4", fieldAASites{parentK: true, parentV: true, widgetK: true}},
		// PDF/UA states no action-type restriction.
		{"PDF/UA-1", fieldAASites{true, true, true, true, true}},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, fieldActionsPDF())
			if _, err := fixJavaScript(ctx, alloc, doc, Options{}, nil, tc.profile); err != nil {
				t.Fatalf("fixJavaScript: %v", err)
			}

			aa := func(objNum int) ast.PdfDict {
				field := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
				return liveDictValue(ctx, alloc, doc, field["AA"])
			}
			has := func(d ast.PdfDict, key ast.PdfName) bool {
				_, ok := d[key]
				return ok
			}
			got := fieldAASites{
				parentK: has(aa(4), "K"),
				parentV: has(aa(4), "V"),
				namedK:  has(aa(5), "K"),
				widgetK: has(aa(6), "K"),
				cycleC:  has(aa(7), "C"),
			}
			if got != tc.want {
				t.Errorf("surviving field actions under %s:\n got %+v\nwant %+v", tc.profile, got, tc.want)
			}
		})
	}
}

// TestFixJavaScript_EmptiedFieldAAIsRemoved pins that a field /AA left empty
// is removed rather than written empty: ISO 19005-2/3 6.4.1 test 2 forbids the
// /AA key on a field whatever it holds, so an empty dictionary would keep that
// violation after the action is gone.
func TestFixJavaScript_EmptiedFieldAAIsRemoved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, fieldActionsPDF())
	if _, err := fixJavaScript(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixJavaScript: %v", err)
	}
	for _, objNum := range []int{5, 7} {
		field := pendingDict(t, alloc, objNum)
		if aa, has := field["AA"]; has {
			t.Errorf("field %d still carries /AA %v; its only action was forbidden", objNum, aa)
		}
	}
}

// inlineFieldTreePDF builds a document whose field tree lives at every inline
// level a field can: an inline AcroForm dictionary in the catalog whose
// /Fields array holds an inline field, and an indirect field whose /Kids
// array (object 5) holds another inline field. Each inline field carries a
// JavaScript /AA/K, so only a write through its container reaches the output.
func inlineFieldTreePDF() []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [<< /T (inl) /AA << /K << /S /JavaScript /JS (a) >> >> >> 4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /T (root) /Kids 5 0 R >>",
		"[<< /T (kid) /Parent 4 0 R /AA << /K << /S /JavaScript /JS (b) >> >> >>]",
	})
}

// TestFixJavaScript_WritesInlineFieldsThroughTheirContainers pins that a field
// with no object number of its own is written back where it lives: into the
// /Fields array of an inline AcroForm, hence into the catalog, and into an
// indirect /Kids array at that array's object number.
func TestFixJavaScript_WritesInlineFieldsThroughTheirContainers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, inlineFieldTreePDF())
	fixes, err := fixJavaScript(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixJavaScript: %v", err)
	}
	if len(fixes) != 2 {
		t.Errorf("got %d fixes, want 2 (one per inline field): %q", len(fixes), fixes)
	}

	acroForm := liveDictValue(ctx, alloc, doc, pendingDict(t, alloc, 1)["AcroForm"])
	fields := liveArray(ctx, alloc, doc, acroForm["Fields"])
	if len(fields) != 2 {
		t.Fatalf("/Fields has %d entries, want 2: %v", len(fields), fields)
	}
	top, ok := fields[0].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Fields[0] is %T, want the inline field", fields[0])
	}
	if _, has := top["AA"]; has {
		t.Errorf("the inline field in /Fields still carries /AA: %v", top)
	}
	if ref, ok := fields[1].(ast.PdfIndirectRef); !ok || ref.ObjectNum != 4 {
		t.Errorf("/Fields[1] = %v, want the untouched reference to object 4", fields[1])
	}

	kids, ok := alloc.Pending()[5].(ast.PdfArray)
	if !ok || len(kids) != 1 {
		t.Fatalf("the indirect /Kids array was not written at object 5: %v", alloc.Pending()[5])
	}
	kid, ok := kids[0].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Kids[0] is %T, want the inline field", kids[0])
	}
	if _, has := kid["AA"]; has {
		t.Errorf("the inline kid still carries /AA: %v", kid)
	}
	if _, rewritten := alloc.Pending()[4]; rewritten {
		t.Error("field 4 was rewritten although its /Kids reference did not change")
	}
}

// TestFixJavaScript_KeepsFormTooltipsFieldWrite pins that the field walk starts
// from each field as the earlier fixers left it. The peer is the real
// form-tooltip fixer, which precedes javascript in the registry and writes a
// field back at its own object number with the /TU it derives from /T (ISO
// 14289-1, 7.18.4). A copy of the document's field would drop that /TU.
func TestFixJavaScript_KeepsFormTooltipsFieldWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, fieldActionsPDF())

	if _, err := fixFormTooltip(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixFormTooltip: %v", err)
	}
	if _, has := pendingDict(t, alloc, 4)["TU"]; !has {
		t.Fatal("control: form-tooltip wrote no /TU on field 4, so this test proves nothing")
	}

	if _, err := fixJavaScript(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixJavaScript: %v", err)
	}
	field := pendingDict(t, alloc, 4)
	if _, has := liveDictValue(ctx, alloc, doc, field["AA"])["K"]; has {
		t.Fatalf("control: field 4 kept its JavaScript /AA/K, so the walk never ran: %v", field)
	}
	if _, has := field["TU"]; !has {
		t.Errorf("field 4 lost the /TU form-tooltip wrote: the walk rebuilt it from the document's copy; field = %v", field)
	}
}
