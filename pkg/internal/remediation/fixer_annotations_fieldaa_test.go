package remediation

import (
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// permittedFieldAAPDF builds a one-page document whose form fields carry an
// /AA holding only a GoTo action, which no PDF/A part forbids as an action
// type, so the javascript fixer's reduction leaves every one of them:
//
//   - object 4, the non-terminal field "parent", whose kid widget (object 5)
//     is the page's only annotation. The page walk never reaches object 4.
//   - an inline field in the inline AcroForm's /Fields, written back only
//     through the catalog.
//   - object 6, a field whose /AA is the null object, an absent entry (ISO
//     32000-1, 7.3.7) and so no violation.
func permittedFieldAAPDF() []byte {
	const goTo = "/AA << /K << /S /GoTo /D [3 0 R /Fit] >> >>"
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R << /T (inl) /FT /Tx " + goTo + " >> 6 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] >>",
		"<< /T (parent) /FT /Tx /Kids [5 0 R] " + goTo + " >>",
		"<< /Type /Annot /Subtype /Widget /Parent 4 0 R /Rect [0 0 10 10] /F 4 >>",
		"<< /T (nullaa) /FT /Tx /AA null >>",
	})
}

// TestFixAnnotations_RemovesFormFieldAA pins that the annotations fixer removes
// a form field's /AA whatever it holds where the profile forbids the key: ISO
// 19005-1 §6.6.2 t2 and ISO 19005-2/3 §6.4.1 t2. The fixer used to strip /AA
// from widget annotations only, and the javascript fixer removes forbidden
// action types alone, so a field /AA holding a GoTo stayed and kept the
// violation. veraPDF 1.30 reports formFields[0](n 0 obj PDTextField) under
// either rule for such a field and nothing once it is gone, measured at
// PDF/A-1b and PDF/A-2b.
//
// PDF/A-4 permits a restricted /AA on a field (ISO 19005-4 §6.6.3) and PDF/UA
// states no such rule, so neither loses it.
func TestFixAnnotations_RemovesFormFieldAA(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		profile string
		removed bool
	}{
		{"PDF/A-1b", true},
		{"PDF/A-2b", true},
		{"PDF/A-3b", true},
		{"PDF/A-4", false},
		{"PDF/UA-1", false},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, permittedFieldAAPDF())
			fixes, err := fixAnnotations(ctx, alloc, doc, Options{}, nil, tc.profile)
			if err != nil {
				t.Fatalf("fixAnnotations: %v", err)
			}

			parent := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 4})
			catalog := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})
			fields := liveArray(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, catalog["AcroForm"])["Fields"])
			if len(fields) != 3 {
				t.Fatalf("/Fields has %d entries, want 3: %v", len(fields), fields)
			}
			inline, ok := fields[1].(ast.PdfDict)
			if !ok {
				t.Fatalf("/Fields[1] is %T, want the inline field", fields[1])
			}

			_, parentHas := parent["AA"]
			_, inlineHas := inline["AA"]
			if parentHas == tc.removed {
				t.Errorf("field 4 /AA present = %v, want %v", parentHas, !tc.removed)
			}
			if inlineHas == tc.removed {
				t.Errorf("the inline field's /AA present = %v, want %v", inlineHas, !tc.removed)
			}
			if _, rewritten := alloc.Pending()[6]; rewritten {
				t.Errorf("field 6, whose /AA is null, was rewritten: %v", alloc.Pending()[6])
			}

			want := []string{"removed /AA from form field 4", "removed /AA from inline form field"}
			for _, fix := range want {
				if slices.Contains(fixes, fix) != tc.removed {
					t.Errorf("fix %q reported = %v, want %v; fixes = %q", fix, !tc.removed, tc.removed, fixes)
				}
			}
		})
	}
}

// fieldAAPeersPDF builds a document on which every fixer that writes the
// catalog, the inline AcroForm or a form field has work to do at PDF/A-2b:
//
//   - the catalog: a JavaScript /OpenAction (javascript).
//   - the inline AcroForm: /NeedAppearances true (need-appearances) and /XFA
//     (xfa), plus an inline field whose GoTo /AA only the catalog write can
//     remove (annotations).
//   - object 4, the field "parent", whose /AA holds a GoTo and a JavaScript
//     action (annotations removes the key, javascript would reduce it) and
//     which has no /TU (form-tooltip).
//   - object 5, its kid widget, with no /F (annot-flags).
//   - object 6, a field merged with its widget, whose /AA holds a GoTo
//     (annotations), with no /F (annot-flags) and no /TU (form-tooltip).
func fieldAAPeersPDF() []byte {
	const goTo = "<< /S /GoTo /D [3 0 R /Fit] >>"
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OpenAction << /S /JavaScript /JS (o) >> " +
			"/AcroForm << /NeedAppearances true /XFA 8 0 R " +
			"/Fields [4 0 R 6 0 R << /T (inl) /FT /Tx /AA << /K " + goTo + " >> >>] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R 6 0 R] >>",
		"<< /T (parent) /FT /Tx /Kids [5 0 R] /AA << /K " + goTo + " /F << /S /JavaScript /JS (f) >> >> >>",
		"<< /Type /Annot /Subtype /Widget /Parent 4 0 R /Rect [0 0 10 10] /AP << /N 7 0 R >> >>",
		"<< /Type /Annot /Subtype /Widget /T (merged) /FT /Tx /Rect [20 0 30 10] /AP << /N 7 0 R >> /AA << /K " + goTo + " >> >>",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Length 0 >>\nstream\n\nendstream",
		"<< /Length 0 >>\nstream\n\nendstream",
	})
}

// TestFixAnnotations_FieldAAComposesWithItsPeers pins that the field /AA
// removal and every fixer writing the same catalog, AcroForm or field keep one
// another's edits, run through the real fixers in registry order.
//
// need-appearances precedes annotations and writes the inline AcroForm through
// the catalog, and annotations writes the catalog again for the inline field.
// annot-flags, form-tooltip, javascript and xfa follow it and write the widget,
// the fields and the catalog. Each copies what it reads and writes the copy
// back whole, so a stale read on either side of annotations drops an edit:
// /NeedAppearances or the /AA coming back, or a field losing its /TU or /F.
// javascript also walks the field /AA, which annotations has already removed.
func TestFixAnnotations_FieldAAComposesWithItsPeers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, fieldAAPeersPDF())
	const profile = "PDF/A-2b"

	run := func(name string, fix func() ([]string, error)) []string {
		t.Helper()
		fixes, err := fix()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(fixes) == 0 {
			t.Fatalf("control: %s fixed nothing, so its composition is untested", name)
		}
		return fixes
	}
	run("fixNeedAppearances", func() ([]string, error) {
		return fixNeedAppearances(ctx, alloc, doc, Options{}, nil, profile)
	})
	annotFixes := run("fixAnnotations", func() ([]string, error) {
		return fixAnnotations(ctx, alloc, doc, Options{}, nil, profile)
	})
	run("fixAnnotFlags", func() ([]string, error) {
		return fixAnnotFlags(ctx, alloc, doc, Options{}, nil, profile)
	})
	run("fixFormTooltip", func() ([]string, error) {
		return fixFormTooltip(ctx, alloc, doc, Options{}, nil, profile)
	})
	jsFixes := run("fixJavaScript", func() ([]string, error) {
		return fixJavaScript(ctx, alloc, doc, Options{}, nil, profile)
	})
	run("fixXFA", func() ([]string, error) {
		return fixXFA(ctx, alloc, doc, Options{}, nil, profile)
	})

	for _, fix := range []string{"removed /AA from form field 4", "removed /AA from form field 6", "removed /AA from inline form field"} {
		if !slices.Contains(annotFixes, fix) {
			t.Errorf("annotations did not report %q: %q", fix, annotFixes)
		}
	}
	for _, fix := range jsFixes {
		if fix != "removed forbidden /OpenAction from catalog" {
			t.Errorf("javascript reported %q: the field /AA annotations removed came back to it", fix)
		}
	}

	catalog := pendingDict(t, alloc, 1)
	if _, has := catalog["OpenAction"]; has {
		t.Error("the catalog kept /OpenAction: a later catalog write rebuilt it from the document")
	}
	acroForm, ok := catalog["AcroForm"].(ast.PdfDict)
	if !ok {
		t.Fatalf("the catalog's /AcroForm is %T, want the inline dictionary", catalog["AcroForm"])
	}
	for _, key := range []ast.PdfName{"NeedAppearances", "XFA"} {
		if _, has := acroForm[key]; has {
			t.Errorf("the AcroForm kept %s: a later catalog write rebuilt it from the document", key)
		}
	}
	fields := liveArray(ctx, alloc, doc, acroForm["Fields"])
	if len(fields) != 3 {
		t.Fatalf("/Fields has %d entries, want 3: %v", len(fields), fields)
	}
	if inline, ok := fields[2].(ast.PdfDict); !ok {
		t.Errorf("/Fields[2] is %T, want the inline field", fields[2])
	} else if _, has := inline["AA"]; has {
		t.Errorf("the inline field kept /AA: a later catalog write rebuilt the AcroForm from the document")
	}

	parent := pendingDict(t, alloc, 4)
	if _, has := parent["AA"]; has {
		t.Errorf("field 4 kept /AA: %v", parent)
	}
	if _, has := parent["TU"]; !has {
		t.Errorf("field 4 lost the /TU form-tooltip wrote: %v", parent)
	}
	if f := pendingDict(t, alloc, 5)["F"]; f != ast.PdfInt(4) {
		t.Errorf("widget 5 carries /F %v, want the 4 annot-flags wrote", f)
	}
	merged := pendingDict(t, alloc, 6)
	if _, has := merged["AA"]; has {
		t.Errorf("merged field 6 kept /AA: a later fixer rebuilt it from the document; %v", merged)
	}
	if merged["F"] != ast.PdfInt(4) {
		t.Errorf("merged field 6 carries /F %v, want the 4 annot-flags wrote", merged["F"])
	}
	if _, has := merged["TU"]; !has {
		t.Errorf("merged field 6 lost the /TU form-tooltip wrote: %v", merged)
	}
}
