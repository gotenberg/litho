package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// nsLoopPDF builds a tagged PDF 2.0 document with no legacy /RoleMap. Object 5
// is the root Document in the PDF 2.0 namespace, object 6 the element under
// test (child supplies its /S, /NS and /K), object 7 the PDF 2.0 namespace and
// objects 8 and 9 two custom ones; pdf2, one and two extend the three.
func nsLoopPDF(child, pdf2, one, two string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K [5 0 R] /Namespaces [7 0 R 8 0 R 9 0 R] >>",
		"<< /Type /StructElem /S /Document /NS 7 0 R /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem "+child+" /P 5 0 R >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) "+pdf2+" >>",
		"<< /Type /Namespace /NS (http://litho.example/one) "+one+" >>",
		"<< /Type /Namespace /NS (http://litho.example/two) "+two+" >>",
	)
}

// nsRoleMapEntry returns the /RoleMapNS entry for name in the namespace whose
// URI is uri, as the target type and the URI of its namespace, and false when
// that namespace maps no such type.
func nsRoleMapEntry(t *testing.T, doc *document.Document, uri string, name ast.PdfName) (ast.PdfName, string, bool) {
	t.Helper()
	ctx := context.Background()
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("resolving the catalog: %v", err)
	}
	st, err := document.ResolveDict(ctx, doc, catalog.Dict["StructTreeRoot"])
	if err != nil {
		t.Fatalf("resolving the structure tree root: %v", err)
	}
	namespaces, err := document.ResolveArray(ctx, doc, st["Namespaces"])
	if err != nil {
		t.Fatalf("resolving /Namespaces: %v", err)
	}
	for _, item := range namespaces {
		ns, err := document.ResolveDict(ctx, doc, item)
		if err != nil {
			t.Fatalf("resolving a namespace: %v", err)
		}
		if got, _ := document.ResolveString(ctx, doc, ns["NS"]); string(got) != uri {
			continue
		}
		rmns, _ := document.ResolveDict(ctx, doc, ns["RoleMapNS"])
		value, ok := rmns[name]
		if !ok {
			return "", "", false
		}
		arr, err := document.ResolveArray(ctx, doc, value)
		if err != nil || len(arr) != 2 {
			t.Fatalf("/RoleMapNS %s is %v, want [name namespace]", name, value)
		}
		target, _ := document.ResolveName(ctx, doc, arr[0])
		targetNS, err := document.ResolveDict(ctx, doc, arr[1])
		if err != nil {
			t.Fatalf("resolving the target namespace: %v", err)
		}
		targetURI, _ := document.ResolveString(ctx, doc, targetNS["NS"])
		return target, string(targetURI), true
	}
	t.Fatalf("no namespace %q", uri)
	return "", "", false
}

// TestRemediatePDFUA2BreaksNamespaceRoleLoops pins the repair of a /RoleMapNS
// loop an element closes (ISO 14289-2, 8.2.4 t1, t2 and t3). A loop through a
// standard type loses that type's entry; a loop of private types has its
// closing entry sent to a standard namespace the chain has not left, naming
// the standard type the chain resolves to when there is one, and otherwise,
// for a cycle, Div, or P for an element that holds content. A chain that ends
// on a private type without cycling is left.
func TestRemediatePDFUA2BreaksNamespaceRoleLoops(t *testing.T) {
	t.Parallel()

	const (
		pdf2 = "http://iso.org/pdf2/ssn"
		pdf1 = "http://iso.org/pdf/ssn"
		one  = "http://litho.example/one"
		two  = "http://litho.example/two"
	)
	type entry struct {
		uri        string
		name       ast.PdfName
		target     ast.PdfName
		targetURI  string
		wantAbsent bool
	}
	tests := []struct {
		name           string
		child          string
		pdf2, one, two string
		want           []entry
		// keepsFindings marks a case left to the residual.
		keepsFindings bool
	}{
		{
			name:  "a loop through a second namespace closes on its last entry",
			child: "/S /Callout /NS 8 0 R /K []",
			one:   "/RoleMapNS << /Callout [/Box 9 0 R] >>",
			two:   "/RoleMapNS << /Box [/Callout 8 0 R] >>",
			want: []entry{
				{uri: one, name: "Callout", target: "Box", targetURI: two},
				{uri: two, name: "Box", target: "Div", targetURI: pdf2},
			},
		},
		{
			name:  "a loop within one namespace closes on its first entry",
			child: "/S /Panel /NS 8 0 R /K []",
			one:   "/RoleMapNS << /Panel [/Frame 8 0 R] /Frame [/Panel 8 0 R] >>",
			want: []entry{
				{uri: one, name: "Panel", target: "Div", targetURI: pdf2},
				{uri: one, name: "Frame", target: "Panel", targetURI: one},
			},
		},
		{
			name:  "an element holding content is given P",
			child: "/S /Panel /NS 8 0 R /K 0",
			one:   "/RoleMapNS << /Panel [/Frame 8 0 R] /Frame [/Panel 8 0 R] >>",
			want:  []entry{{uri: one, name: "Panel", target: "P", targetURI: pdf2}},
		},
		{
			name:  "a chain back into the PDF 2.0 namespace keeps its standard type in the PDF 1.7 one",
			child: "/S /Q /NS 7 0 R /K []",
			pdf2:  "/RoleMapNS << /Q [/T 8 0 R] >>",
			one:   "/RoleMapNS << /T [/P 7 0 R] >>",
			want:  []entry{{uri: one, name: "T", target: "P", targetURI: pdf1}},
		},
		{
			name:  "a loop through a standard type loses that type's entry",
			child: "/S /Sect /NS 7 0 R /K []",
			pdf2:  "/RoleMapNS << /Sect [/Book 8 0 R] >>",
			one:   "/RoleMapNS << /Book [/Sect 7 0 R] >>",
			want: []entry{
				{uri: pdf2, name: "Sect", wantAbsent: true},
				{uri: one, name: "Book", target: "Sect", targetURI: pdf2},
			},
		},
		{
			name:  "a standard type mapped to itself loses its entry",
			child: "/S /Aside /NS 7 0 R /K []",
			pdf2:  "/RoleMapNS << /Aside [/Aside 7 0 R] >>",
			want:  []entry{{uri: pdf2, name: "Aside", wantAbsent: true}},
		},
		{
			name:  "a same-namespace hop keeps the standard type the chain resolves to",
			child: "/S /Q /NS 8 0 R /K []",
			one:   "/RoleMapNS << /Q [/P 8 0 R] /P [/P 7 0 R] >>",
			want:  []entry{{uri: one, name: "Q", target: "P", targetURI: pdf2}},
		},
		{
			name:          "a same-namespace hop to a type nothing maps is left",
			child:         "/S /Alpha /NS 8 0 R /K []",
			one:           "/RoleMapNS << /Alpha [/Beta 8 0 R] >>",
			want:          []entry{{uri: one, name: "Alpha", target: "Beta", targetURI: one}},
			keepsFindings: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := nsLoopPDF(tc.child, tc.pdf2, tc.one, tc.two)
			in, _ := newTestDoc(t, data)
			if len(roleMapRulesFailed(t, in)) == 0 {
				t.Fatal("the input reports no 8.2.4 finding; the case tests nothing")
			}
			out := remediatePDFUA2Output(t, data)
			if got := roleMapRulesFailed(t, out); len(got) != 0 && !tc.keepsFindings {
				t.Errorf("8.2.4 findings after remediation: %v", got)
			}
			for _, e := range tc.want {
				target, targetURI, ok := nsRoleMapEntry(t, out, e.uri, e.name)
				if e.wantAbsent {
					if ok {
						t.Errorf("%s in %s still maps to %s in %s", e.name, e.uri, target, targetURI)
					}
					continue
				}
				if !ok || target != e.target || targetURI != e.targetURI {
					t.Errorf("%s in %s maps to %s in %q (present %v), want %s in %q",
						e.name, e.uri, target, targetURI, ok, e.target, e.targetURI)
				}
			}
		})
	}
}

// TestRemediatePDFUA2LeavesAnUnusedNamespaceLoop pins that a /RoleMapNS loop
// no element reaches is left as the file wrote it: ISO 14289-2, 8.2.4 judges
// structure elements, so it has nothing to answer for.
func TestRemediatePDFUA2LeavesAnUnusedNamespaceLoop(t *testing.T) {
	t.Parallel()
	data := nsLoopPDF("/S /P /NS 7 0 R /K []", "",
		"/RoleMapNS << /Callout [/Box 9 0 R] >>", "/RoleMapNS << /Box [/Callout 8 0 R] >>")
	out := remediatePDFUA2Output(t, data)
	target, targetURI, ok := nsRoleMapEntry(t, out, "http://litho.example/two", "Box")
	if !ok || target != "Callout" || targetURI != "http://litho.example/one" {
		t.Errorf("Box maps to %s in %q (present %v), want Callout in the first namespace", target, targetURI, ok)
	}
}
