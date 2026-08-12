package remediation

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// buildSimplePDF assembles a classic cross-reference PDF from object bodies
// numbered 1..N in order, so a fixture can pin exactly which object a fixer
// rewrites.
func buildSimplePDF(objs []string) []byte {
	var buf []byte
	buf = append(buf, "%PDF-1.7\n"...)
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = len(buf)
		buf = append(buf, fmt.Sprintf("%d 0 obj\n%s\nendobj\n", i+1, body)...)
	}
	start := len(buf)
	buf = append(buf, fmt.Sprintf("xref\n0 %d\n", len(objs)+1)...)
	buf = append(buf, "0000000000 65535 f \n"...)
	for i := 1; i <= len(objs); i++ {
		buf = append(buf, fmt.Sprintf("%010d 00000 n \n", offsets[i])...)
	}
	buf = append(buf, fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, start)...)
	return buf
}

// directAcroFormXFAPDF builds a document whose catalog carries an inline
// interactive form dictionary holding /XFA. Object 1 is the catalog, the only
// object that can be rewritten to drop the key.
func directAcroFormXFAPDF() []byte {
	return buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [] /XFA [(x) 4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Length 3 >>\nstream\nabc\nendstream",
	})
}

// TestFixXFARemovesKeyFromInlineAcroForm pins that /XFA is dropped when the
// interactive form dictionary is written inline in the catalog.
//
// ISO 32000-1, 12.7.2 does not require /AcroForm to be an indirect object, and
// 11 of the 20 corpus files carrying one write it inline. The repair only wrote
// through the allocator, which has no object number to address for a direct
// dictionary, so the rewrite was dropped while success was still reported:
// litho printed "removed /XFA from AcroForm" on every one of the fixpoint
// loop's three rounds and the output still carried /XFA.
func TestFixXFARemovesKeyFromInlineAcroForm(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, directAcroFormXFAPDF())

	fixes, err := fixXFA(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixXFA: %v", err)
	}
	if len(fixes) != 1 {
		t.Fatalf("fixes = %v, want one repair reported", fixes)
	}

	catalog := pendingDict(t, alloc, 1)
	acroForm, ok := catalog["AcroForm"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/AcroForm = %T, want the rewritten inline dictionary", catalog["AcroForm"])
	}
	if _, present := acroForm["XFA"]; present {
		t.Error("/XFA survived the repair that reported removing it")
	}
	// The rest of the form must be preserved.
	if _, hasFields := acroForm["Fields"]; !hasFields {
		t.Error("/Fields was lost while removing /XFA")
	}
}

// TestFixXFAIsIdempotentOnInlineAcroForm checks a second run reports nothing.
// A repair that never lands makes the fixpoint loop re-run it every round, which
// is what exposed the defect.
func TestFixXFAIsIdempotentOnInlineAcroForm(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, directAcroFormXFAPDF())

	if _, err := fixXFA(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	fixes, err := fixXFA(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("second pass reported %v, want no repair", fixes)
	}
}

// TestFixXFAStillRewritesIndirectAcroForm keeps the original path covered: an
// indirect form dictionary is rewritten in place, not through the catalog.
func TestFixXFAStillRewritesIndirectAcroForm(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildSimplePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Length 3 >>\nstream\nabc\nendstream",
		"<< /Fields [] /XFA [(x) 4 0 R] >>",
	}))

	if _, err := fixXFA(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixXFA: %v", err)
	}

	acroForm := pendingDict(t, alloc, 5)
	if _, present := acroForm["XFA"]; present {
		t.Error("/XFA survived on an indirect AcroForm")
	}
	if _, wroteCatalog := alloc.Pending()[1]; wroteCatalog {
		t.Error("the catalog was rewritten for an indirect AcroForm; only object 5 should change")
	}
}
