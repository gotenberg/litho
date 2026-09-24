package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// sigRefPDF builds a PDF whose catalog /Perms /DocMDP (object 4) references one
// signature-reference dictionary (object 5) carrying the three digest keys
// ISO 19005-2/3 §6.1.12 test 2 forbids. sigExtra is spliced into the signature
// dictionary, so a test either makes it a real signed signature or leaves it
// signing nothing.
func sigRefPDF(sigExtra string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /Perms << /DocMDP 4 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Sig /Reference [5 0 R]"+sigExtra+" >>",
		"<< /Type /SigRef /TransformMethod /DocMDP /DigestMethod /SHA1 "+
			"/DigestValue (abc) /DigestLocation [0 0] >>",
	)
}

// signedSig makes the signature dictionary of [sigRefPDF] a real one.
const signedSig = " /Contents <00112233> /ByteRange [0 100 200 300]"

func permsOf(t *testing.T, doc *document.Document) ast.PdfObject {
	t.Helper()
	catalog, err := document.ResolveCatalog(context.Background(), doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	perms, ok := catalog.Dict["Perms"]
	if !ok {
		t.Fatal("fixture has no /Perms")
	}
	return perms
}

// TestFixSignatureReferences_StandardModeLeavesASignedSignature pins the repair
// of a swap litho used to make: stripping the forbidden digest keys rewrites a
// reference dictionary inside the revision the signature covers, which moves the
// /ByteRange and reports ISO 19005-2/3 §6.4.3 test 1 in place of §6.1.12 test 2.
// The violation count stays the same, so a count-based gate sees nothing.
//
// # Corpus evidence
//
//   - verapdf PDF_A-2b__veraPDF_test_suite_6-1-12-t02-fail-a/b/c: the count
//     stayed at one violation and a count-based gate saw nothing.
func TestFixSignatureReferences_StandardModeLeavesASignedSignature(t *testing.T) {
	t.Parallel()
	doc, alloc := newTestDoc(t, sigRefPDF(signedSig))

	fixes := fixSignatureReferences(context.Background(), alloc, doc,
		Options{PreserveContent: true}, permsOf(t, doc))
	if len(fixes) != 0 {
		t.Errorf("standard mode must leave a signed signature alone, got fixes %v", fixes)
	}
	if _, queued := alloc.Pending()[5]; queued {
		t.Error("the signature reference dictionary was rewritten, which invalidates the signature")
	}
}

// TestFixSignatureReferences_AggressiveModeStripsASignedSignature keeps the
// chain aggressive mode relies on. There the invalidation is intended: the strip
// breaks the coverage, §6.4.3 t1 fires, and [fixSignatureStrip] removes the
// invalidated signature, resolving both rules. Guarding unconditionally severed
// that chain.
//
// # Corpus evidence
//
//   - All corpora: guarding unconditionally cost 18 fixtures their fully-fixed
//     verdict in the remediation-aggressive corpus cell.
func TestFixSignatureReferences_AggressiveModeStripsASignedSignature(t *testing.T) {
	t.Parallel()
	doc, alloc := newTestDoc(t, sigRefPDF(signedSig))

	fixes := fixSignatureReferences(context.Background(), alloc, doc,
		Options{PreserveContent: false}, permsOf(t, doc))
	if len(fixes) != 1 {
		t.Fatalf("aggressive mode must still strip a signed signature, got %v", fixes)
	}
	ref := pendingDict(t, alloc, 5)
	if _, present := ref["DigestValue"]; present {
		t.Error("/DigestValue survived in aggressive mode")
	}
}

// TestFixSignatureReferences_StripsAnUnsignedSignature keeps the repair working
// where there is no signature to invalidate. Without this arm the standard-mode
// guard would be indistinguishable from disabling the fixer.
func TestFixSignatureReferences_StripsAnUnsignedSignature(t *testing.T) {
	t.Parallel()
	doc, alloc := newTestDoc(t, sigRefPDF(""))

	fixes := fixSignatureReferences(context.Background(), alloc, doc,
		Options{PreserveContent: true}, permsOf(t, doc))
	if len(fixes) != 1 {
		t.Fatalf("expected one fix for an unsigned /DocMDP, got %v", fixes)
	}

	ref := pendingDict(t, alloc, 5)
	for _, forbidden := range []ast.PdfName{"DigestLocation", "DigestMethod", "DigestValue"} {
		if _, present := ref[forbidden]; present {
			t.Errorf("/%s survived on an unsigned signature reference", forbidden)
		}
	}
	// The repair is surgical: the rest of the reference dictionary stays.
	if ref["TransformMethod"] != ast.PdfName("DocMDP") {
		t.Errorf("/TransformMethod: got %v, want /DocMDP", ref["TransformMethod"])
	}
}
