package remediation

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// ungovernedRolePDF builds a tagged document whose legacy /RoleMap maps /Para
// to /P through /MyP and /Aside2 to the PDF 1.7-only /Note. Object 5 is the
// root Document, object 6 the element under test (child supplies its /S and
// /NS), object 7 the PDF 2.0 namespace, object 8 the PDF 1.7 one and object 9
// a custom one; ns17 and custom extend the last two, and extra objects follow
// from 10.
func ungovernedRolePDF(child, ns17, custom string, extra ...string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R] /RoleMap << /Para /MyP /MyP /P /Aside2 /Note >> /Namespaces [7 0 R 8 0 R 9 0 R] >>",
		"<< /Type /StructElem /S /Document /NS 7 0 R /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem " + child + " /P 5 0 R >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf/ssn) " + ns17 + " >>",
		"<< /Type /Namespace /NS (http://litho.example/probe) " + custom + " >>",
	}
	return buildStructPDF(append(objs, extra...)...)
}

// remediatePDFUA2Output remediates data to PDF/UA-2, rewrites it and returns
// the reopened output.
func remediatePDFUA2Output(t *testing.T, data []byte) *document.Document {
	t.Helper()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, data)
	if _, err := RemediatePDFUA(ctx, doc, alloc, models.PDFUA2, Options{}); err != nil {
		t.Fatalf("RemediatePDFUA: %v", err)
	}
	var out bytes.Buffer
	if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}
	reread, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	return reread
}

// roleMapRulesFailed returns the ISO 14289-2 §8.2.4 findings of doc at PDF/UA-2.
func roleMapRulesFailed(t *testing.T, doc *document.Document) []compliance.Violation {
	t.Helper()
	errs, err := compliance.ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	var out []compliance.Violation
	for _, v := range toViolations(errs) {
		if v.Rule.Specification == "ISO_14289_2" && v.Rule.Clause == "8.2.4" {
			out = append(out, v)
		}
	}
	return out
}

// elementUnderTest returns the single kid of the root Document of a rewritten
// [ungovernedRolePDF], which a full rewrite renumbers.
func elementUnderTest(t *testing.T, doc *document.Document) ast.PdfDict {
	t.Helper()
	ctx := context.Background()
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("resolving the catalog: %v", err)
	}
	node, err := document.ResolveDict(ctx, doc, catalog.Dict["StructTreeRoot"])
	if err != nil {
		t.Fatalf("resolving the structure tree root: %v", err)
	}
	for range 2 {
		kids, err := document.ResolveArray(ctx, doc, node["K"])
		if err != nil || len(kids) != 1 {
			t.Fatalf("/K is %v, want one kid", node["K"])
		}
		if node, err = document.ResolveDict(ctx, doc, kids[0]); err != nil {
			t.Fatalf("resolving a kid: %v", err)
		}
	}
	return node
}

// TestRemediatePDFUA2RepairsAnUngovernedRoleMappedType pins the repair of a type
// the legacy /RoleMap maps in a namespace that map does not govern. The map
// governs a type in no namespace, or in the PDF 1.7 namespace when that
// namespace declares no /RoleMapNS (ISO 32000-2:2020, 14.8.6), so a /Para
// declared anywhere else is unmapped: ISO 14289-2 §8.2.4 t1. An element of a
// standard namespace is renamed to the type the map gives it, and a type of a
// custom namespace is mapped through that namespace's /RoleMapNS into a
// standard namespace, which renames nothing.
func TestRemediatePDFUA2RepairsAnUngovernedRoleMappedType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const otherMap = "/RoleMapNS << /Other [/Span 7 0 R] >>"
	tests := []struct {
		name                string
		child, ns17, custom string
		extra               []string
		wantS               ast.PdfName
		// mapsNS is set when the repair maps the type through the element's
		// namespace, and indirect when that /RoleMapNS stays an indirect
		// object.
		mapsNS, indirect bool
	}{
		{name: "pdf 2.0 namespace", child: "/S /Para /NS 7 0 R", wantS: "P"},
		{name: "pdf 1.7 namespace with its own role map", child: "/S /Para /NS 8 0 R", ns17: otherMap, wantS: "P"},
		{name: "custom namespace", child: "/S /Para /NS 9 0 R", wantS: "Para", mapsNS: true},
		{name: "custom namespace with a direct role map", child: "/S /Para /NS 9 0 R", custom: otherMap, wantS: "Para", mapsNS: true},
		{
			name: "custom namespace with an indirect role map", child: "/S /Para /NS 9 0 R", custom: "/RoleMapNS 10 0 R",
			extra: []string{"<< /Other [/Span 7 0 R] >>"}, wantS: "Para", mapsNS: true, indirect: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := remediatePDFUA2Output(t, ungovernedRolePDF(tc.child, tc.ns17, tc.custom, tc.extra...))
			for _, v := range roleMapRulesFailed(t, out) {
				t.Errorf("the output fails %s test %d: %s", v.Rule.Clause, v.Rule.TestNumber, v.Context)
			}
			elem := elementUnderTest(t, out)
			if s, _ := document.ResolveName(ctx, out, elem["S"]); s != tc.wantS {
				t.Errorf("the element is %s, want %s", s, tc.wantS)
			}
			if !tc.mapsNS {
				return
			}
			nsDict, err := document.ResolveDict(ctx, out, elem["NS"])
			if err != nil {
				t.Fatalf("resolving the element's namespace: %v", err)
			}
			if _, isRef := nsDict["RoleMapNS"].(ast.PdfIndirectRef); isRef != tc.indirect {
				t.Errorf("/RoleMapNS is %T, want an indirect object: %v", nsDict["RoleMapNS"], tc.indirect)
			}
			rmns, err := document.ResolveDict(ctx, out, nsDict["RoleMapNS"])
			if err != nil {
				t.Fatalf("resolving the custom namespace's /RoleMapNS: %v", err)
			}
			entry, err := document.ResolveArray(ctx, out, rmns["Para"])
			if err != nil || len(entry) != 2 {
				t.Fatalf("/RoleMapNS /Para is %v, want a [name namespace] pair", rmns["Para"])
			}
			if name, _ := document.ResolveName(ctx, out, entry[0]); name != "P" {
				t.Errorf("/RoleMapNS maps /Para to /%s, want /P", name)
			}
			ns, err := document.ResolveDict(ctx, out, entry[1])
			if err != nil {
				t.Fatalf("resolving the target namespace: %v", err)
			}
			if uri, _ := document.ResolveString(ctx, out, ns["NS"]); string(uri) != pdf2NamespaceURI {
				t.Errorf("/RoleMapNS maps /Para into %q, want %q", uri, pdf2NamespaceURI)
			}
			if tc.custom != "" {
				if _, kept := rmns["Other"]; !kept {
					t.Errorf("the repair dropped the namespace's own /Other entry: %v", rmns)
				}
			}
		})
	}
}

// TestFixRoleMapAddsTheNamespaceItMapsInto pins that a /RoleMapNS entry names a
// namespace the structure tree root lists: a custom namespace mapped into the
// PDF 2.0 one gets that namespace added to /Namespaces when the root lacks it.
func TestFixRoleMapAddsTheNamespaceItMapsInto(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R] /RoleMap << /Para /P >> /Namespaces [6 0 R] >>",
		"<< /Type /StructElem /S /Para /NS 6 0 R /P 4 0 R >>",
		"<< /Type /Namespace /NS (http://litho.example/probe) >>",
	))
	fixes, err := fixRoleMap(ctx, alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixRoleMap: %v", err)
	}
	if !anyFixContains(fixes, "/RoleMapNS") {
		t.Errorf("fixes = %v, want the /RoleMapNS repair reported", fixes)
	}
	st := pendingDict(t, alloc, 4)
	nsArr, ok := st["Namespaces"].(ast.PdfArray)
	if !ok || len(nsArr) != 2 {
		t.Fatalf("/Namespaces is %v, want the custom namespace and the PDF 2.0 one", st["Namespaces"])
	}
	rmns, ok := pendingDict(t, alloc, 6)["RoleMapNS"].(ast.PdfDict)
	if !ok {
		t.Fatalf("the custom namespace carries no /RoleMapNS")
	}
	entry, ok := rmns["Para"].(ast.PdfArray)
	if !ok || len(entry) != 2 || entry[1] != nsArr[1] {
		t.Errorf("/RoleMapNS /Para is %v, want /P in the namespace %v the root lists", rmns["Para"], nsArr[1])
	}
	if pendingDict(t, alloc, 4)["RoleMap"] == nil {
		t.Errorf("the repair dropped the legacy /RoleMap")
	}
}

// TestFixRoleMapLeavesAnUngovernedTypeTheMapDoesNotDeclare pins the refusals:
// the repair gives a type only the meaning the legacy /RoleMap declares for
// it, in a form the element's namespace admits, so it invents nothing.
func TestFixRoleMapLeavesAnUngovernedTypeTheMapDoesNotDeclare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		child, ns17, custom string
	}{
		// The map says nothing about /Gadget.
		{"type the map does not map", "/S /Gadget /NS 9 0 R", "", ""},
		// /Note is a PDF 1.7 type: the PDF 2.0 namespace does not define it.
		{"type the namespace does not define", "/S /Aside2 /NS 7 0 R", "", ""},
		// The namespace states its own mapping for /Para.
		{"type the namespace maps itself", "/S /Para /NS 9 0 R", "", "/RoleMapNS << /Para [/Gadget 9 0 R] >>"},
		// A /RoleMapNS that is no dictionary is the file's to keep.
		{"namespace role map that is no dictionary", "/S /Para /NS 9 0 R", "", "/RoleMapNS /Bogus"},
		// The map governs a type in no namespace, so nothing is unmapped.
		{"type in no namespace", "/S /Para", "", ""},
		// The map governs the PDF 1.7 namespace when it declares no role map.
		{"pdf 1.7 namespace", "/S /Para /NS 8 0 R", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, alloc := newTestDoc(t, ungovernedRolePDF(tc.child, tc.ns17, tc.custom))
			fixes, err := fixRoleMap(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
			if err != nil {
				t.Fatalf("fixRoleMap: %v", err)
			}
			if len(fixes) != 0 || len(alloc.Pending()) != 0 {
				t.Errorf("fixes = %v, pending objects %v, want nothing written", fixes, alloc.Pending())
			}
		})
	}
}
