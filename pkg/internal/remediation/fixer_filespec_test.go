package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// fileSpecPDF builds a single-page PDF whose object 4 is a /Filespec with the
// given body and object 5 is the embedded-file stream it points at.
func fileSpecPDF(filespecBody string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		filespecBody,
		"<< /Type /EmbeddedFile /Length 3 >>\nstream\nabc\nendstream",
	)
}

func TestMatchFileSpecFUF(t *testing.T) {
	t.Parallel()
	for _, r := range []compliance.Rule{
		compliance.Rule_19005_2_6_8_t02,
		compliance.Rule_19005_3_6_8_t02,
		compliance.Rule_19005_4_6_9_t02,
		compliance.Rule_14289_1_7_11_t01,
	} {
		if !matchFileSpecFUF(compliance.Violation{Rule: r}) {
			t.Errorf("must match the F/UF rule (clause %s)", r.Clause)
		}
	}
	if matchFileSpecFUF(compliance.Violation{Rule: compliance.Rule_19005_3_6_8_t03}) {
		t.Error("must not match the AFRelationship rule")
	}
}

func TestFixFileSpecFUF_AddsUF(t *testing.T) {
	pdf := fileSpecPDF("<< /Type /Filespec /F (test.txt) /EF << /F 5 0 R >> >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFileSpecFUF(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixFileSpecFUF: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	fs := pendingDict(t, alloc, 4)
	uf, ok := fs["UF"].(ast.PdfString)
	if !ok || string(uf) != "test.txt" {
		t.Errorf("/UF: got %v, want (test.txt)", fs["UF"])
	}
}

func TestFixFileSpecFUF_AddsF(t *testing.T) {
	pdf := fileSpecPDF("<< /Type /Filespec /UF (doc.bin) /EF << /F 5 0 R >> >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFileSpecFUF(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixFileSpecFUF: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}
	fs := pendingDict(t, alloc, 4)
	f, ok := fs["F"].(ast.PdfString)
	if !ok || string(f) != "doc.bin" {
		t.Errorf("/F: got %v, want (doc.bin)", fs["F"])
	}
}

func TestFixFileSpecFUF_NoopBothPresent(t *testing.T) {
	pdf := fileSpecPDF("<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 5 0 R >> >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFileSpecFUF(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixFileSpecFUF: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a conformant file spec needs no fix: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixFileSpecFUF_SkipsWithoutEF(t *testing.T) {
	// A file spec with neither /EF (not an embedded file) nor both keys is left
	// alone: the F/UF requirement applies only to embedded-file specs.
	pdf := fileSpecPDF("<< /Type /Filespec /F (ext.txt) >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFileSpecFUF(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixFileSpecFUF: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a non-embedded file spec must be skipped: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixFileSpecFUF_SkipsBothMissing(t *testing.T) {
	// /EF present but neither /F nor /UF: there is no name to derive, so leave it.
	pdf := fileSpecPDF("<< /Type /Filespec /EF << /F 5 0 R >> /Desc (x) >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFileSpecFUF(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixFileSpecFUF: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a file spec with no name must be left alone: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixFileSpecFUF_SkipsEmptySource(t *testing.T) {
	// An empty /F cannot satisfy the non-empty requirement, so it is not copied.
	pdf := fileSpecPDF("<< /Type /Filespec /F () /EF << /F 5 0 R >> >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFileSpecFUF(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixFileSpecFUF: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("an empty source name must not be copied: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}
