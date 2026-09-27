package remediation

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// structParentLangPDF builds a tagged page whose structure tree reaches only
// the Document element 5. A square annotation 6 and a text field 8 name,
// through the /ParentTree 10, the elements 7 and 9, whose /Lang are the string
// tokens annotLang and fieldLang, or absent when empty: only the /ParentTree
// leads to them. extraAnnots lists further annotation objects on the page,
// written from object 11 on.
func structParentLangPDF(annotLang, fieldLang string, extraAnnots ...string) []byte {
	annots := "6 0 R 8 0 R"
	for i := range extraAnnots {
		annots += fmt.Sprintf(" %d 0 R", 11+i)
	}
	lang := func(token string) string {
		if token == "" {
			return ""
		}
		return " /Lang " + token
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /Lang (en) /MarkInfo << /Marked true >> /AcroForm << /Fields [8 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [" + annots + "] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree 10 0 R /ParentTreeNextKey 2 >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [] >>",
		"<< /Type /Annot /Subtype /Square /Rect [50 600 70 620] /P 3 0 R /StructParent 0 >>",
		"<< /Type /StructElem /S /Annot /P 5 0 R" + lang(annotLang) + " /K << /Type /OBJR /Obj 6 0 R >> >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f1) /Rect [100 600 200 620] /P 3 0 R /StructParent 1 >>",
		"<< /Type /StructElem /S /Form /P 5 0 R" + lang(fieldLang) + " /K << /Type /OBJR /Obj 8 0 R >> >>",
		"<< /Nums [0 7 0 R 1 9 0 R] >>",
	}
	return buildStructPDF(append(objs, extraAnnots...)...)
}

// TestFixCatalogLang_RepairsStructParentElementLang pins that the /Lang repair
// reaches the structure elements an annotation and a form field name by
// /StructParent, which the structure tree does not reach. ISO 14289-1, 7.2
// t29 judges them (veraPDF's GFPDAnnot.getLang and GFPDFormField.getLang), and
// a walk of the tree alone left both invalid tags in the output.
func TestFixCatalogLang_RepairsStructParentElementLang(t *testing.T) {
	t.Parallel()

	// The underscore makes "en_US" malformed, and PDF/UA refuses the empty tag
	// that ISO 32000-1, 14.9.2 reads as "unknown".
	doc, alloc := newTestDoc(t, structParentLangPDF("(en_US)", "()"))
	descs, err := fixCatalogLang(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixCatalogLang: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("descriptions = %v, want one", descs)
	}
	for _, objNum := range []int{7, 9} {
		elem := pendingDict(t, alloc, objNum)
		if _, ok := elem["Lang"]; ok {
			t.Errorf("element %d keeps /Lang %v", objNum, elem["Lang"])
		}
		if elem["K"] == nil || elem["S"] == nil {
			t.Errorf("element %d lost its other entries: %v", objNum, elem)
		}
	}
	for _, objNum := range []int{1, 4, 5, 10} {
		if _, ok := alloc.Pending()[objNum]; ok {
			t.Errorf("object %d was rewritten, but it carries no invalid /Lang", objNum)
		}
	}
}

// TestFixCatalogLang_KeepsValidStructParentElementLang pins the controls: a
// valid tag is kept, and the empty tag PDF/A admits (ISO 19005-2, 6.7.4 t1
// judges the tag against ISO 32000-1, 14.9.2, which reads it as "unknown")
// is kept under a PDF/A profile.
func TestFixCatalogLang_KeepsValidStructParentElementLang(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, profile, annotLang, fieldLang string
	}{
		{name: "valid tags", profile: "PDF/UA-1", annotLang: "(de-DE)", fieldLang: "(fr)"},
		{name: "empty tag under PDF/A", profile: "PDF/A-2a", annotLang: "(de-DE)", fieldLang: "()"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, alloc := newTestDoc(t, structParentLangPDF(tc.annotLang, tc.fieldLang))
			descs, err := fixCatalogLang(context.Background(), alloc, doc, Options{}, nil, tc.profile)
			if err != nil {
				t.Fatalf("fixCatalogLang: %v", err)
			}
			if len(descs) != 0 || len(alloc.Pending()) != 0 {
				t.Errorf("descriptions = %v, pending = %d objects, want nothing repaired", descs, len(alloc.Pending()))
			}
		})
	}
}

// TestFixCatalogLang_RepairsStructParentElementLangOnce pins that an element
// the tree and the /ParentTree both reach is repaired and counted once.
func TestFixCatalogLang_RepairsStructParentElementLangOnce(t *testing.T) {
	t.Parallel()

	pdf := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /Lang (en) >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 7 0 R] >> >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [7 0 R] >>",
		"<< /Type /Annot /Subtype /Square /Rect [50 600 70 620] /StructParent 0 >>",
		"<< /Type /StructElem /S /Annot /P 5 0 R /Lang (en_US) /K << /Type /OBJR /Obj 6 0 R >> >>",
	)
	doc, alloc := newTestDoc(t, pdf)
	descs, err := fixCatalogLang(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-1")
	if err != nil {
		t.Fatalf("fixCatalogLang: %v", err)
	}
	if len(descs) != 1 || descs[0] != "removed invalid /Lang from 1 structure element(s)" {
		t.Errorf("descriptions = %v, want the one element counted once", descs)
	}
	if _, ok := pendingDict(t, alloc, 7)["Lang"]; ok {
		t.Error("element 7 keeps its /Lang")
	}
}

// TestFixCatalogLang_KeepsAnnotNestingParentTree pins that the /Lang repair of
// the elements the /ParentTree maps keeps what annot-nesting (registry 19),
// which runs ahead of catalog-lang (28), queued: the structure tree root with
// the /ParentTree entry of the annotation it nested, and the element holding
// it. The repair reads the tree root and its /ParentTree as annot-nesting
// left them, and writes back only the elements whose /Lang it strips.
func TestFixCatalogLang_KeepsAnnotNestingParentTree(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, structParentLangPDF("(en_US)", "(de-DE)",
		"<< /Type /Annot /Subtype /Square /Rect [300 600 320 620] /P 3 0 R >>"))
	if _, err := fixAnnotNesting(ctx, alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixAnnotNesting: %v", err)
	}
	nested := pendingDict(t, alloc, 11)
	key, ok := nested["StructParent"].(ast.PdfInt)
	if !ok {
		t.Fatalf("control: annot-nesting gave annotation 11 no /StructParent: %v", nested)
	}

	if _, err := fixCatalogLang(ctx, alloc, doc, Options{}, nil, "PDF/UA-1"); err != nil {
		t.Fatalf("fixCatalogLang: %v", err)
	}
	if _, ok := pendingDict(t, alloc, 7)["Lang"]; ok {
		t.Error("element 7 keeps its /Lang")
	}
	root := pendingDict(t, alloc, 4)
	elemObj, err := document.LookupNumberTree(ctx, doc, root["ParentTree"], int(key))
	if err != nil || elemObj == nil {
		t.Fatalf("the /ParentTree lost the entry %d annot-nesting added: %v (%v)", key, root["ParentTree"], err)
	}
	elemRef, ok := elemObj.(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("/ParentTree entry %d = %v, want a reference", key, elemObj)
	}
	if elem := resolveFromAllocOrDoc(ctx, alloc, doc, elemRef); elem["S"] != ast.PdfName("Annot") {
		t.Errorf("the element annot-nesting created is gone: %v", elem)
	}
}
