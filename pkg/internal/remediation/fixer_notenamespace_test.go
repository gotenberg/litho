package remediation

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

func TestMatchNoteNamespace(t *testing.T) {
	t.Parallel()
	if !matchNoteNamespace(compliance.Violation{Rule: compliance.Rule_14289_2_8_2_5_14_t01}) {
		t.Error("must match ISO 14289-2 8.2.5.14 t1")
	}
	for _, r := range []compliance.Rule{compliance.Rule_14289_2_8_2_5_14_t02, compliance.Rule_14289_2_8_2_5_14_t04, compliance.Rule_14289_1_7_9_t01} {
		if matchNoteNamespace(compliance.Violation{Rule: r}) {
			t.Errorf("must not claim %s %s t%d", r.Specification, r.Clause, r.TestNumber)
		}
	}
}

// notePDF builds a tagged PDF/UA-2 candidate: object 5 is the root Document in
// the PDF 2.0 namespace (object 7), objects 6 and 9 are its two kids, and
// object 8 is the PDF 1.7 namespace. root extends the structure tree root and
// ns17 the PDF 1.7 namespace.
func notePDF(kid6, kid9, root, ns17 string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R] /Namespaces [7 0 R 8 0 R] "+root+" >>",
		"<< /Type /StructElem /S /Document /NS 7 0 R /P 4 0 R /K [6 0 R 9 0 R] >>",
		"<< /Type /StructElem /P 5 0 R "+kid6+" >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf/ssn) "+ns17+" >>",
		"<< /Type /StructElem /P 5 0 R "+kid9+" >>",
	)
}

// structRulesFailed returns the ISO 14289-2 8.2.4 and 8.2.5.14 findings of doc
// at PDF/UA-2.
func structRulesFailed(t *testing.T, doc *document.Document) map[string]int {
	t.Helper()
	errs, err := compliance.ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	out := make(map[string]int)
	for _, v := range toViolations(errs) {
		if v.Rule.Specification == "ISO_14289_2" && (v.Rule.Clause == "8.2.4" || v.Rule.Clause == "8.2.5.14") {
			out[fmt.Sprintf("%s t%d", v.Rule.Clause, v.Rule.TestNumber)]++
		}
	}
	return out
}

// TestRemediatePDFUA2MapsTheStandardNoteIntoThePDF20Namespace pins the repair of
// ISO 14289-2, 8.2.5.14 t1: a PDF 1.7 Note keeps its /S, and the PDF 1.7
// namespace maps Note to the PDF 2.0 FENote. The controls are the constructs
// the repair must leave alone because mapping would create another failure.
func TestRemediatePDFUA2MapsTheStandardNoteIntoThePDF20Namespace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name            string
		kid6, kid9      string
		root, ns17      string
		wantFixed       bool
		wantAfter       map[string]int
		wantKid6Bound17 bool
		// noteType is the /S of the repaired kid 6, Note when empty.
		noteType ast.PdfName
	}{
		{
			name: "note in no namespace and note in the pdf 1.7 namespace",
			kid6: "/S /Note", kid9: "/S /Note /NS 8 0 R",
			wantFixed: true, wantKid6Bound17: true,
		},
		{
			name: "type the legacy role map leads to note",
			kid6: "/S /Footnote", kid9: "/S /P /NS 7 0 R", root: "/RoleMap << /Footnote /Note >>",
			wantFixed: true, wantKid6Bound17: true, noteType: "Footnote",
		},
		{
			name: "pdf 1.7 namespace already declaring a role map",
			kid6: "/S /Note /NS 8 0 R", kid9: "/S /P /NS 7 0 R",
			ns17:      "/RoleMapNS << /Chapter [/Sect 7 0 R] >>",
			wantFixed: true,
		},
		{
			// A /RoleMapNS on the PDF 1.7 namespace ends the legacy map's
			// governance of every type declared in it (ISO 32000-2:2020,
			// 14.8.6), so the /Para bound there would lose its mapping.
			name: "pdf 1.7 element relying on the legacy role map",
			kid6: "/S /Note", kid9: "/S /Para /NS 8 0 R", root: "/RoleMap << /Para /P >>",
			wantAfter: map[string]int{"8.2.5.14 t1": 1},
		},
		{
			// As an FENote the half-built /Ref association would fail t2.
			name: "note another element refers to",
			kid6: "/S /Note", kid9: "/S /P /NS 7 0 R /Ref [6 0 R]",
			wantAfter: map[string]int{"8.2.5.14 t1": 1},
		},
		{
			name: "note carrying an FENote NoteType",
			kid6: "/S /Note /A << /O /FENote /NoteType /Sidenote >>", kid9: "/S /P /NS 7 0 R",
			wantAfter: map[string]int{"8.2.5.14 t1": 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := notePDF(tc.kid6, tc.kid9, tc.root, tc.ns17)
			if before := structRulesFailed(t, inputDoc(t, in)); before["8.2.5.14 t1"] == 0 {
				t.Fatalf("the input does not fail 8.2.5.14 t1: %v", before)
			}
			out := remediatePDFUA2Output(t, in)
			got := structRulesFailed(t, out)
			want := tc.wantAfter
			if tc.wantFixed {
				want = map[string]int{}
			}
			if len(got) != len(want) {
				t.Errorf("the output fails %v, want %v", got, want)
			}
			for k, n := range want {
				if got[k] != n {
					t.Errorf("the output fails %v, want %v", got, want)
				}
			}
			if !tc.wantFixed {
				return
			}
			catalog, err := document.ResolveCatalog(ctx, out)
			if err != nil {
				t.Fatalf("resolving the catalog: %v", err)
			}
			root, err := document.ResolveDict(ctx, out, catalog.Dict["StructTreeRoot"])
			if err != nil {
				t.Fatalf("resolving the structure tree root: %v", err)
			}
			docElem := firstKid(t, out, root)
			kid := firstKid(t, out, docElem)
			noteType := tc.noteType
			if noteType == "" {
				noteType = "Note"
			}
			if s, _ := document.ResolveName(ctx, out, kid["S"]); s != noteType {
				t.Errorf("the note is %s, want it to keep /S %s", s, noteType)
			}
			nsDict, _ := document.ResolveDict(ctx, out, kid["NS"])
			uri, _ := document.ResolveString(ctx, out, nsDict["NS"])
			if tc.wantKid6Bound17 && string(uri) != pdf1NamespaceURI {
				t.Errorf("the unbound note declares namespace %q, want the PDF 1.7 one", uri)
			}
			rmns, _ := document.ResolveDict(ctx, out, nsDict["RoleMapNS"])
			entry, _ := document.ResolveArray(ctx, out, rmns[noteType])
			if len(entry) != 2 {
				t.Fatalf("the PDF 1.7 /RoleMapNS maps %s to %v, want [FENote ns]", noteType, rmns[noteType])
			}
			if n, _ := document.ResolveName(ctx, out, entry[0]); n != "FENote" {
				t.Errorf("%s maps to %s, want FENote", noteType, n)
			}
			target, _ := document.ResolveDict(ctx, out, entry[1])
			if u, _ := document.ResolveString(ctx, out, target["NS"]); string(u) != pdf2NamespaceURI {
				t.Errorf("Note maps into namespace %q, want the PDF 2.0 one", u)
			}
			if len(rmns) > 1 {
				if _, kept := rmns["Chapter"]; !kept {
					t.Error("an existing /RoleMapNS entry was dropped")
				}
			}
		})
	}
}

// inputDoc opens data unremediated.
func inputDoc(t *testing.T, data []byte) *document.Document {
	t.Helper()
	doc, _ := newTestDoc(t, data)
	return doc
}

// firstKid returns the first kid of node's /K as a dictionary.
func firstKid(t *testing.T, doc *document.Document, node ast.PdfDict) ast.PdfDict {
	t.Helper()
	kids, err := document.ResolveArray(context.Background(), doc, node["K"])
	if err != nil || len(kids) == 0 {
		t.Fatalf("/K is %v, want kids", node["K"])
	}
	d, err := document.ResolveDict(context.Background(), doc, kids[0])
	if err != nil {
		t.Fatalf("resolving a kid: %v", err)
	}
	return d
}
