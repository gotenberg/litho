package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestUA2LinkAnnotNesting pins ISO 14289-2 §8.2.5.20 t1, which requires a Link
// annotation to be enclosed by a Link or Reference element.
//
// The rule is per annotation. It was once approximated by a document-level
// count, firing only when a file held no Link element at all, which let a
// single well-placed Link mask every misplaced one; the mixed case below is the
// pin against that.
func TestUA2LinkAnnotNesting(t *testing.T) {
	ctx := context.Background()

	// Build a UA-2 document whose Link annotation is enclosed by a structure
	// element of type enclType. An empty enclType leaves it out of the tree.
	build := func(enclType string) []Violation {
		link := "<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /StructParent 0 >>"
		if enclType == "" {
			doc, cat := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [8 0 R] >>",
				"<< /Type /StructTreeRoot /Namespaces [5 0 R] /K 6 0 R >>",
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
				"<< /Type /StructElem /S /Document /NS 5 0 R /P 4 0 R /K 7 0 R >>",
				"<< /Type /StructElem /S /P /NS 5 0 R /P 6 0 R >>",
				link,
			)
			return checkPDFUA2AnnotStructNesting(ctx, doc, cat, "PDF/UA-2")
		}
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [8 0 R] >>",
			"<< /Type /StructTreeRoot /Namespaces [5 0 R] /K 6 0 R >>",
			"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			"<< /Type /StructElem /S /Document /NS 5 0 R /P 4 0 R /K 7 0 R >>",
			"<< /Type /StructElem /S /"+enclType+" /NS 5 0 R /P 6 0 R /K 9 0 R >>",
			link,
			"<< /Type /OBJR /Obj 8 0 R >>",
		)
		return checkPDFUA2AnnotStructNesting(ctx, doc, cat, "PDF/UA-2")
	}

	for _, tc := range []struct {
		enclType string
		want     bool
	}{
		{enclType: "", want: false},          // absent from the tree: an artifact
		{enclType: "Link", want: false},      // the type the clause requires
		{enclType: "Reference", want: false}, // the alternative it allows
		{enclType: "Artifact", want: false},  // veraPDF's isArtifact exemption
		{enclType: "P", want: true},
		{enclType: "Div", want: true},
	} {
		name := tc.enclType
		if name == "" {
			name = "outside the tree"
		}
		got := hasRule(build(tc.enclType), Rule_14289_2_8_2_5_20_t01)
		if got != tc.want {
			t.Errorf("Link enclosed by %s: fired=%v, want %v", name, got, tc.want)
		}
	}

	// The masking pin: a conforming Link beside a misplaced one. Counting Link
	// elements document-wide reports nothing here.
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [8 0 R 9 0 R] >>",
		"<< /Type /StructTreeRoot /Namespaces [5 0 R] /K 6 0 R >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		"<< /Type /StructElem /S /Document /NS 5 0 R /P 4 0 R /K [7 0 R 10 0 R] >>",
		"<< /Type /StructElem /S /Link /NS 5 0 R /P 6 0 R /K 11 0 R >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /StructParent 0 >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /StructParent 1 >>",
		"<< /Type /StructElem /S /P /NS 5 0 R /P 6 0 R /K 12 0 R >>",
		"<< /Type /OBJR /Obj 8 0 R >>",
		"<< /Type /OBJR /Obj 9 0 R >>",
	)
	got := checkPDFUA2AnnotStructNesting(ctx, doc, cat, "PDF/UA-2")
	var fired int
	for _, v := range got {
		if v.Rule == Rule_14289_2_8_2_5_20_t01 {
			fired++
		}
	}
	if fired != 1 {
		t.Errorf("one conforming Link beside one misplaced Link: fired %d times, want 1", fired)
	}
}

// TestLinkAnnotStructDest pins the target identity ISO 14289-2 §8.2.5.20 t2
// compares.
//
// Only a structure destination has a target to compare, and only the target's
// identity counts. Comparing whole destination arrays reported two links onto
// the same element with different zoom as disagreeing, and reported page
// destinations and URI actions that veraPDF never examines at all.
func TestLinkAnnotStructDest(t *testing.T) {
	ctx := context.Background()

	// Object 5 is a structure element, object 3 is the page. destExpr becomes
	// the annotation's destination-bearing entries.
	dest := func(t *testing.T, destExpr string) string {
		t.Helper()
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /Names << /Dests 6 0 R >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
			"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] "+destExpr+" >>",
			"<< /Type /StructElem /S /P >>",
			"<< /Names [(named) [5 0 R /Fit]] >>",
		)
		page, err := document.ResolveDict(ctx, doc, cat["Pages"])
		if err != nil {
			t.Fatalf("resolving /Pages: %v", err)
		}
		kids, err := document.ResolveArray(ctx, doc, page["Kids"])
		if err != nil {
			t.Fatalf("resolving /Kids: %v", err)
		}
		first, err := document.ResolveDict(ctx, doc, kids[0])
		if err != nil {
			t.Fatalf("resolving the page: %v", err)
		}
		annots, err := document.ResolveArray(ctx, doc, first["Annots"])
		if err != nil {
			t.Fatalf("resolving /Annots: %v", err)
		}
		annot, err := document.ResolveDict(ctx, doc, annots[0])
		if err != nil {
			t.Fatalf("resolving the annotation: %v", err)
		}
		return linkAnnotDest(ctx, doc, annot)
	}

	structTarget := "5 0"

	for _, tc := range []struct {
		name string
		expr string
		want string
	}{
		{
			name: "structure destination through /Dest",
			expr: "/Dest [5 0 R /Fit]",
			want: structTarget,
		},
		{
			name: "view parameters do not change identity",
			expr: "/Dest [5 0 R /XYZ 0 700 0]",
			want: structTarget,
		},
		{
			name: "structure destination through a GoTo /D",
			expr: "/A << /S /GoTo /D [5 0 R /Fit] >>",
			want: structTarget,
		},
		{
			name: "/SD takes precedence over /D",
			expr: "/A << /S /GoTo /SD [5 0 R /Fit] /D [3 0 R /Fit] >>",
			want: structTarget,
		},
		{
			name: "named destination resolves to its array",
			expr: "/Dest (named)",
			want: structTarget,
		},
		{
			name: "page destination names no structure element",
			expr: "/Dest [3 0 R /Fit]",
			want: "",
		},
		{
			name: "GoTo onto a page names no structure element",
			expr: "/A << /S /GoTo /D [3 0 R /XYZ 0 700 0] >>",
			want: "",
		},
		{
			name: "URI action has no destination",
			expr: "/A << /S /URI /URI (https://example.test/) >>",
			want: "",
		},
		{
			name: "no destination at all",
			expr: "/F 4",
			want: "",
		},
		{
			name: "empty destination array",
			expr: "/Dest []",
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dest(t, tc.expr); got != tc.want {
				t.Errorf("linkAnnotDest = %q, want %q", got, tc.want)
			}
		})
	}
}
