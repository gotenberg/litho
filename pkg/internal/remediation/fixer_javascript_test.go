package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestIsForbiddenAction_NamedActions covers the extension that flags a /Named
// action whose /N is not one of the four standard named actions (§6.6.1). The
// forbidden-type behavior must stay intact.
func TestIsForbiddenAction_NamedActions(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))
	ctx := context.Background()
	cases := []struct {
		name string
		act  ast.PdfDict
		want bool
	}{
		{"named ExportData forbidden", ast.PdfDict{"S": ast.PdfName("Named"), "N": ast.PdfName("ExportData")}, true},
		{"named NextPage allowed", ast.PdfDict{"S": ast.PdfName("Named"), "N": ast.PdfName("NextPage")}, false},
		{"named LastPage allowed", ast.PdfDict{"S": ast.PdfName("Named"), "N": ast.PdfName("LastPage")}, false},
		{"named without N left alone", ast.PdfDict{"S": ast.PdfName("Named")}, false},
		{"launch still forbidden", ast.PdfDict{"S": ast.PdfName("Launch")}, true},
		{"goto allowed", ast.PdfDict{"S": ast.PdfName("GoTo")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isForbiddenAction(ctx, alloc, doc, tc.act, "PDF/A-2b"); got != tc.want {
				t.Errorf("isForbiddenAction(%v) = %v, want %v", tc.act, got, tc.want)
			}
		})
	}
}

func TestFixJavaScript_RemovesForbiddenNamedAction(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /OpenAction << /S /Named /N /ExportData >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixJavaScript(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixJavaScript: %v", err)
	}
	if len(descs) == 0 {
		t.Fatal("expected the forbidden Named /OpenAction to be removed")
	}
	if _, has := pendingDict(t, alloc, 1)["OpenAction"]; has {
		t.Error("forbidden Named /OpenAction should have been removed from the catalog")
	}
}

// TestFixJavaScript_RemovesNamesJavaScriptTree pins a swap bug where the
// rebuilt /Names dict was written into the discarded catalog copy, so the
// allocated catalog silently kept its /Names/JavaScript tree. JavaScript is a
// forbidden action subtype (ISO 19005-2 §6.5.1, §6.6.1), so the surviving
// /Names/JavaScript tree left the output non-conformant despite the fix.
func TestFixJavaScript_RemovesNamesJavaScriptTree(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /Names << /JavaScript << /Names [(init) << /S /JavaScript /JS (app.alert\\(1\\)) >>] >> /Dests << /Names [] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixJavaScript(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixJavaScript: %v", err)
	}
	if len(descs) == 0 {
		t.Fatal("expected /Names/JavaScript removal to report a fix")
	}

	namesObj, ok := pendingDict(t, alloc, 1)["Names"]
	if !ok {
		t.Fatal("catalog lost its /Names dict entirely; only the JavaScript entry should go")
	}
	ref, ok := namesObj.(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("expected rewritten /Names to be an indirect ref, got %T", namesObj)
	}
	names, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("rewritten /Names object %d not a pending dict", ref.ObjectNum)
	}
	if _, has := names["JavaScript"]; has {
		t.Error("allocated catalog still references a /Names dict containing /JavaScript")
	}
	if _, has := names["Dests"]; !has {
		t.Error("unrelated /Names entries must survive the rewrite")
	}
}

func TestFixJavaScript_KeepsAllowedNamedAction(t *testing.T) {
	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /OpenAction << /S /Named /N /NextPage >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	doc, alloc := newTestDoc(t, pdf)

	if _, err := fixJavaScript(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixJavaScript: %v", err)
	}
	// A standard navigation action is permitted. If the catalog was rewritten
	// at all, the /OpenAction must survive; an untouched catalog is also fine.
	if obj, ok := alloc.Pending()[1]; ok {
		if d, ok := obj.(ast.PdfDict); ok {
			if _, has := d["OpenAction"]; !has {
				t.Error("allowed Named /OpenAction must be preserved")
			}
		}
	}
}

// actionSitesPDF builds one page that carries an action of each kind the
// profiles disagree on, each at its own site so a test can tell which were
// stripped: a JavaScript /OpenAction (object 6), /Names/JavaScript tree, page
// /AA/O (object 12) and outline /A, a SetOCGState (object 4) and a GoTo3DView
// (object 5) link, plus a Launch (object 10) and a non-standard Named action
// (object 11) that every PDF/A part forbids.
func actionSitesPDF() []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OpenAction 6 0 R /Names << /JavaScript 7 0 R >> /Outlines 8 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R 10 0 R 11 0 R] /AA << /O 12 0 R >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A << /S /SetOCGState /State [] >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A << /S /GoTo3DView /V 0 >> >>",
		"<< /S /JavaScript /JS (open) >>",
		"<< /Names [(init) << /S /JavaScript /JS (init) >>] >>",
		"<< /Type /Outlines /First 9 0 R /Last 9 0 R /Count 1 >>",
		"<< /Title (item) /Parent 8 0 R /A << /S /JavaScript /JS (outline) >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A << /S /Launch /F (a.exe) >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A << /S /Named /N /Print >> >>",
		"<< /S /JavaScript /JS (page) >>",
	})
}

// actionSites records which actions of [actionSitesPDF] survive a
// fixJavaScript run.
type actionSites struct {
	openAction, namesJS, outline, pageAA bool
	setOCGState, goTo3DView              bool
	launch, named                        bool
}

// TestFixJavaScript_StripsOnlyWhatTheProfileForbids pins that each action type
// is stripped only where the target profile's action-type whitelist excludes
// it (ISO 19005-1 6.6.1 test 1, ISO 19005-2/3 6.5.1 test 1, ISO 19005-4 6.6.1
// test 1).
//
// The fixer used to strip one fixed list whatever the profile, so a PDF/A-4
// file lost the JavaScript that every PDF/A-4 conformance permits, and a
// PDF/A-4e file also lost the SetOCGState and GoTo3DView actions that PDF/A-4e
// admits. The Launch and Named sites are the positive control: every PDF/A part
// forbids them, so they prove the fixer ran on every PDF/A row.
func TestFixJavaScript_StripsOnlyWhatTheProfileForbids(t *testing.T) {
	t.Parallel()

	all := actionSites{true, true, true, true, true, true, true, true}
	partsOneToThree := actionSites{}
	pdfA4 := actionSites{openAction: true, namesJS: true, outline: true, pageAA: true}
	pdfA4e := actionSites{openAction: true, namesJS: true, outline: true, pageAA: true, setOCGState: true, goTo3DView: true}

	for _, tc := range []struct {
		profile string
		want    actionSites
	}{
		{"PDF/A-1b", partsOneToThree},
		{"PDF/A-2b", partsOneToThree},
		{"PDF/A-3u", partsOneToThree},
		{"PDF/A-4", pdfA4},
		{"PDF/A-4f", pdfA4},
		{"PDF/A-4e", pdfA4e},
		// PDF/UA states no action-type restriction.
		{"PDF/UA-1", all},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, actionSitesPDF())
			if _, err := fixJavaScript(ctx, alloc, doc, Options{}, nil, tc.profile); err != nil {
				t.Fatalf("fixJavaScript: %v", err)
			}

			live := func(objNum int) ast.PdfDict {
				return resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
			}
			has := func(d ast.PdfDict, key ast.PdfName) bool {
				_, ok := d[key]
				return ok
			}
			catalog := live(1)
			got := actionSites{
				openAction:  has(catalog, "OpenAction"),
				namesJS:     has(liveDictValue(ctx, alloc, doc, catalog["Names"]), "JavaScript"),
				outline:     has(live(9), "A"),
				pageAA:      has(liveDictValue(ctx, alloc, doc, live(3)["AA"]), "O"),
				setOCGState: has(live(4), "A"),
				goTo3DView:  has(live(5), "A"),
				launch:      has(live(10), "A"),
				named:       has(live(11), "A"),
			}
			if got != tc.want {
				t.Errorf("surviving actions under %s:\n got %+v\nwant %+v", tc.profile, got, tc.want)
			}
		})
	}
}

// TestFixJavaScript_StripsWhatTheValidatorFlags pins that the fixer strips an
// action type exactly where [compliance.IsForbiddenActionType] flags it, for
// every action type ISO 32000-2, Table 201 defines plus a non-standard one.
//
// A hand list used to nominate the stripped types, and it had drifted from the
// whitelists of ISO 19005-1 6.6.1 test 1, ISO 19005-2/3 6.5.1 test 1 and ISO
// 19005-4 6.6.1 test 1: GoToE at PDF/A-1, RichMediaExecute and GoToDp at parts
// 1 to 3 were flagged and kept. The explicit rows pin those three.
func TestFixJavaScript_StripsWhatTheValidatorFlags(t *testing.T) {
	t.Parallel()

	types := []ast.PdfName{
		"GoTo", "GoToR", "GoToE", "GoToDp", "Launch", "Thread", "URI", "Sound",
		"Movie", "Hide", "Named", "SubmitForm", "ResetForm", "ImportData",
		"SetOCGState", "Rendition", "Trans", "GoTo3DView", "JavaScript",
		"RichMediaExecute", "SetState", "NOP", "NotAnActionType",
	}
	profiles := []string{"PDF/A-1b", "PDF/A-2b", "PDF/A-3b", "PDF/A-4", "PDF/A-4e", "PDF/A-4f", "PDF/UA-1"}

	explicit := map[[2]string]bool{
		{"GoToE", "PDF/A-1b"}:            true,
		{"GoToE", "PDF/A-2b"}:            false,
		{"RichMediaExecute", "PDF/A-1b"}: true,
		{"RichMediaExecute", "PDF/A-3b"}: true,
		{"RichMediaExecute", "PDF/A-4"}:  false,
		{"GoToDp", "PDF/A-2b"}:           true,
		{"GoToDp", "PDF/A-4f"}:           false,
	}

	for _, profile := range profiles {
		for _, typ := range types {
			t.Run(profile+"/"+string(typ), func(t *testing.T) {
				t.Parallel()

				ctx := context.Background()
				doc, alloc := newTestDoc(t, assemblePDF([]string{
					"<< /Type /Catalog /Pages 2 0 R >>",
					"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
					"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
					"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A << /S /" + string(typ) + " >> >>",
				}))
				if _, err := fixJavaScript(ctx, alloc, doc, Options{}, nil, profile); err != nil {
					t.Fatalf("fixJavaScript: %v", err)
				}
				annot := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 4})
				_, kept := annot["A"]

				want := compliance.IsForbiddenActionType(typ, profile)
				if w, ok := explicit[[2]string{string(typ), profile}]; ok && w != want {
					t.Fatalf("the validator's verdict moved: IsForbiddenActionType(%s, %s) = %v, this test pins %v", typ, profile, want, w)
				}
				if kept == want {
					t.Errorf("%s under %s: kept = %v, but the validator flags it = %v", typ, profile, kept, want)
				}
			})
		}
	}
}
