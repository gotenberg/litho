package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestIsForbiddenAction_NamedActions covers the extension that flags a /Named
// action whose /N is not one of the four standard named actions (§6.6.1). The
// forbidden-type behavior must stay intact.
func TestIsForbiddenAction_NamedActions(t *testing.T) {
	doc, _ := newTestDoc(t, minimalCatalogPDF(""))
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
			if got := isForbiddenAction(ctx, doc, tc.act); got != tc.want {
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
// allocated catalog silently kept its /Names/JavaScript tree and litho's
// post-fix validation kept failing 6.5.1/6.6.1 while veraPDF passed.
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
