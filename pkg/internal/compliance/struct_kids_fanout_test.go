package compliance

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// sharedKArrayObjs are the objects of a tagged one-page document whose
// structure tree root, object 4, lists its kids in object 5: an array of two
// direct Div elements that each name object 5 again as their /K, beside a P
// element, object 6. Walked anew wherever it is reached, the array fans out
// 2^depth up to the walk's depth cap.
var sharedKArrayObjs = []string{
	"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /Lang (en) /StructTreeRoot 4 0 R >>",
	"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
	"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
	"<< /Type /StructTreeRoot /K 5 0 R >>",
	"[<< /Type /StructElem /S /Div /K 5 0 R >> << /Type /StructElem /S /Div /K 5 0 R >> 6 0 R]",
	"<< /Type /StructElem /S /P /P 4 0 R >>",
}

// selfKArrayObjs are [sharedKArrayObjs] with object 5 an array that lists
// itself twice beside the P element.
var selfKArrayObjs = []string{
	sharedKArrayObjs[0], sharedKArrayObjs[1], sharedKArrayObjs[2], sharedKArrayObjs[3],
	"[5 0 R 5 0 R 6 0 R]",
	sharedKArrayObjs[5],
}

// divChainObjs returns the objects of a tagged one-page document whose
// structure tree root lists a Div, object 5, heading a chain of n Div
// elements that each list the next twice, the last a P. The pass-through
// lookups of ISO/TS 32005 Table 5 expand each Div in its parent's place, and
// expanded anew wherever it is reached the chain fans out 2^n.
func divChainObjs(n int) []string {
	objs := []string{
		sharedKArrayObjs[0], sharedKArrayObjs[1], sharedKArrayObjs[2],
		"<< /Type /StructTreeRoot /K [5 0 R] >>",
	}
	for i := range n {
		next := 6 + i
		objs = append(objs, fmt.Sprintf("<< /Type /StructElem /S /Div /K [%d 0 R %d 0 R] >>", next, next))
	}
	return append(objs, "<< /Type /StructElem /S /P >>")
}

// structRoot returns the structure tree root of doc.
func structRoot(t *testing.T, doc *document.Document) ast.PdfDict {
	t.Helper()
	root, err := document.ResolveDict(context.Background(), doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil || root == nil {
		t.Fatalf("resolving the structure tree root: %v", err)
	}
	return root
}

// structWalkState returns the state the PDF/UA structure walk starts from.
func structWalkState(ctx context.Context, doc *document.Document, root ast.PdfDict, profile string, results *[]Violation) *walkState {
	return &walkState{
		doc:            doc,
		profile:        profile,
		results:        results,
		roleMap:        buildRoleMapIndex(ctx, doc, root),
		classMap:       structClassMap(ctx, doc, root),
		iso32005Parent: "StructTreeRoot",
		ua1Parent:      ua1ParentRef{root: true},
		seen:           make(map[int]bool),
	}
}

// TestStructWalks_SharedKArrayFanOut pins that every structure tree walk of
// the compliance checks reads a /K array written as an object of its own
// once, so direct elements naming it again cannot fan out 2^depth, and that
// the walks still reach the element beside them.
func TestStructWalks_SharedKArrayFanOut(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		objs []string
		run  func(ctx context.Context, doc *document.Document, root ast.PdfDict)
	}{
		{"walkStructureElements", sharedKArrayObjs, func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			walkStructureElements(ctx, doc, root, func(ast.PdfDict, string) {})
		}},
		{"walkStructureElementsWithLang", sharedKArrayObjs, func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			walkStructureElementsWithLang(ctx, doc, root, false, func(ast.PdfDict, string, int, bool) {})
		}},
		{"walkStructureTree PDF/UA-1", sharedKArrayObjs, func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			var results []Violation
			walkStructureTree(ctx, doc, root, string(models.PDFUA1), &results)
		}},
		{"walkStructureTree PDF/UA-2", sharedKArrayObjs, func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			var results []Violation
			walkStructureTree(ctx, doc, root, string(models.PDFUA2), &results)
		}},
		{"walkNSCheck", sharedKArrayObjs, func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			var results []Violation
			walkNSCheck(ctx, doc, root, defaultStandardNamespaceURI, nil, nil, &results, make(map[int]bool), 0)
		}},
		{"significantChildren", sharedKArrayObjs, func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			newNSRoleResolver(ctx, doc, root).significantChildren(ctx, doc, root, 0)
		}},
		{"collectFENoteRefGraph", selfKArrayObjs, func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			collectFENoteRefGraph(ctx, doc, newNSRoleResolver(ctx, doc, root), root)
		}},
		{"structElemContainsRef", sharedKArrayObjs, func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			structElemContainsRef(ctx, doc, ast.PdfDict{"K": root["K"]}, 0, make(map[int]bool))
		}},
		{"structTreeHasActualText", sharedKArrayObjs, func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			structTreeHasActualText(ctx, doc, root, 0)
		}},
		{"structValueWalk", sharedKArrayObjs, func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			w := structValueWalk{doc: doc, seen: make(map[int]bool)}
			w.children(ctx, propertyListSite{}, root, 0)
		}},
		{"iso32005Kids", sharedKArrayObjs, func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			var results []Violation
			iso32005Kids(ctx, structWalkState(ctx, doc, root, string(models.PDFUA2), &results), root)
		}},
		{"iso32005Kids Div chain", divChainObjs(40), func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			var results []Violation
			iso32005Kids(ctx, structWalkState(ctx, doc, root, string(models.PDFUA2), &results), root)
		}},
		{"tableKids", sharedKArrayObjs, func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			var results []Violation
			tableKids(ctx, structWalkState(ctx, doc, root, string(models.PDFUA1), &results), root)
		}},
		{"tableKids Div chain", divChainObjs(40), func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			var results []Violation
			tableKids(ctx, structWalkState(ctx, doc, root, string(models.PDFUA1), &results), root)
		}},
		{"listContainsLabels", sharedKArrayObjs, func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			var results []Violation
			listContainsLabels(ctx, structWalkState(ctx, doc, root, string(models.PDFUA1), &results), root, 0)
		}},
		{"listContainsLabels Div chain", divChainObjs(40), func(ctx context.Context, doc *document.Document, root ast.PdfDict) {
			var results []Violation
			listContainsLabels(ctx, structWalkState(ctx, doc, root, string(models.PDFUA1), &results), root, 0)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			doc, _ := objsDocPDF(t, tt.objs...)
			tt.run(ctx, doc, structRoot(t, doc))
			if ctx.Err() != nil {
				t.Fatal("the walk did not end before the deadline")
			}
		})
	}
}

// TestStructWalks_SharedKArrayReachesElements pins that the walks reading a
// shared /K array once still report every element it holds: the two direct
// Div elements and the P beside them.
func TestStructWalks_SharedKArrayReachesElements(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc, _ := objsDocPDF(t, sharedKArrayObjs...)
	root := structRoot(t, doc)
	var types []ast.PdfName
	walkStructureElements(ctx, doc, root, func(elem ast.PdfDict, _ string) {
		s, _ := elem["S"].(ast.PdfName)
		types = append(types, s)
	})
	if fmt.Sprint(types) != "[/Div /Div /P]" {
		t.Errorf("walked %v, want [Div Div P]", types)
	}
}

// TestValidateUA_SharedKArrayFanOut pins that PDF/UA and WTPDF validation end
// on a structure tree whose /K array its direct elements name again, and on
// one whose Div elements each list the next twice.
func TestValidateUA_SharedKArrayFanOut(t *testing.T) {
	t.Parallel()
	for _, shape := range []struct {
		name string
		objs []string
	}{
		{"shared array", sharedKArrayObjs},
		{"self array", selfKArrayObjs},
		{"Div chain", divChainObjs(40)},
	} {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			doc, _ := objsDocPDF(t, shape.objs...)
			for _, profile := range []models.PDFUAProfile{models.PDFUA1, models.PDFUA2} {
				if _, err := ValidatePDFUA(ctx, doc, profile); err != nil && ctx.Err() == nil {
					t.Fatalf("ValidatePDFUA %s: %v", profile, err)
				}
			}
			if _, err := ValidateWTPDF(ctx, doc, models.WTPDF1Accessibility); err != nil && ctx.Err() == nil {
				t.Fatalf("ValidateWTPDF: %v", err)
			}
			if _, err := ValidatePDFA(ctx, doc, models.PDFA2a); err != nil && ctx.Err() == nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if ctx.Err() != nil {
				t.Fatal("validation did not end before the deadline")
			}
		})
	}
}

// TestCollectPageObjects_SharedKidsArrayFanOut pins that the compliance page
// tree walk reads a /Kids array written as an object of its own once, as the
// reader does, and still finds the page it holds.
func TestCollectPageObjects_SharedKidsArrayFanOut(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids 4 0 R /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"[3 0 R << /Type /Pages /Kids 4 0 R /Count 1 >> << /Type /Pages /Kids 4 0 R /Count 1 >>]",
	)
	pages := collectPageObjects(ctx, doc, cat)
	if ctx.Err() != nil {
		t.Fatal("the walk did not end before the deadline")
	}
	if len(pages) != 1 || pages[0].objNum != 3 {
		t.Errorf("collected %d pages, want the one page, object 3", len(pages))
	}
}
