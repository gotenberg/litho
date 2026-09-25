package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// nulPairDoc builds a structure tree whose role-map chains visit two
// (namespace, type) pairs that join to the same string on NUL. A "#00" escape
// puts a NUL in a name (ISO 32000-1, 7.3.5) and a "\000" escape puts one in
// the /NS string. Object numbers: 5 = the PDF 2.0 namespace; 6 -> 7 -> P is
// the chain whose namespace-first join collides ("http://x.test/a", "b\x00T"
// and "http://x.test/a\x00b", "T"); 8 -> 9 -> P is the chain whose name-first
// join collides ("T\x00v", "u" and "T", "v\x00u").
func nulPairDoc(t *testing.T) (*document.Document, ast.PdfDict) {
	t.Helper()
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /Namespaces [5 0 R 6 0 R 7 0 R 8 0 R 9 0 R] >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		"<< /Type /Namespace /NS (http://x.test/a) /RoleMapNS << /b#00T [/T 7 0 R] >> >>",
		"<< /Type /Namespace /NS (http://x.test/a\\000b) /RoleMapNS << /T [/P 5 0 R] >> >>",
		"<< /Type /Namespace /NS (u) /RoleMapNS << /T#00v [/T 9 0 R] >> >>",
		"<< /Type /Namespace /NS (v\\000u) /RoleMapNS << /T [/P 5 0 R] >> >>",
	)
	root, err := document.ResolveDict(context.Background(), doc, cat["StructTreeRoot"])
	if err != nil || root == nil {
		t.Fatalf("resolve StructTreeRoot: %v", err)
	}
	return doc, root
}

// TestRoleMapChainNulPairsStayApart pins that the role-map cycle detection
// keys a (namespace, type) pair by both parts, not by their NUL-joined
// string. Each chain below visits two different pairs that join to the same
// string and then reaches the standard type P, so a joined key reported a
// cycle on a chain that has none.
func TestRoleMapChainNulPairsStayApart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, root := nulPairDoc(t)
	r := newNSRoleResolver(ctx, doc, root)

	gotType, gotNS, ok := r.resolveStandardType(ctx, doc, "b\x00T", "http://x.test/a")
	if !ok || gotType != "P" || gotNS != pdf2NamespaceURI {
		t.Errorf("resolveStandardType = (%q, %q, %v), want (%q, %q, true)", gotType, gotNS, ok, "P", pdf2NamespaceURI)
	}

	if got := roleMapNSChainOutcome(ctx, doc, "b\x00T", "http://x.test/a", r.rmnsByURI); got != nsChainStandard {
		t.Errorf("roleMapNSChainOutcome = %v, want nsChainStandard", got)
	}

	ns, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 8})
	if err != nil || ns == nil {
		t.Fatalf("resolve namespace 8: %v", err)
	}
	start := iso32005Ref{name: "T\x00v", ns: ns, uri: "u"}
	std, ok := iso32005ResolveRef(ctx, &walkState{doc: doc}, start)
	if !ok || std.name != "P" {
		t.Errorf("iso32005ResolveRef = (%q, %v), want (%q, true)", std.name, ok, "P")
	}
}
