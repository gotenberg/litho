package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// annotDocPDF builds a minimal single-page PDF whose page carries one annotation
// of the given subtype, with correct xref offsets so it parses cleanly.
func annotDocPDF(subtype string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /" + subtype + " /Rect [0 0 10 10] >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

func mediaAnnotViolations(t *testing.T, subtype, profile string) []Violation {
	t.Helper()
	data := annotDocPDF(subtype)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	cat, err := document.ResolveCatalog(context.Background(), doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	return checkPDFUA2ForbiddenMediaAnnots(context.Background(), doc, cat.Dict, profile)
}

func TestForbiddenMediaAnnots_UA2(t *testing.T) {
	if v := mediaAnnotViolations(t, "Sound", "PDF/UA-2"); len(v) != 1 || v[0].Rule != Rule_14289_2_8_9_2_4_11_t01 {
		t.Errorf("Sound under UA-2: got %v, want one §8.9.2.4.11 t01", v)
	}
	if v := mediaAnnotViolations(t, "Movie", "PDF/UA-2"); len(v) != 1 || v[0].Rule != Rule_14289_2_8_9_2_4_11_t02 {
		t.Errorf("Movie under UA-2: got %v, want one §8.9.2.4.11 t02", v)
	}
}

func TestForbiddenMediaAnnots_AllowedAndOtherProfiles(t *testing.T) {
	// A benign annotation is never flagged.
	if v := mediaAnnotViolations(t, "Text", "PDF/UA-2"); len(v) != 0 {
		t.Errorf("Text annotation must not be flagged: %v", v)
	}
	// The rule is PDF/UA-2 only: a Sound annotation under UA-1 is this check's
	// no-op (PDF/UA-1 handles media annotations under different rules).
	if v := mediaAnnotViolations(t, "Sound", "PDF/UA-1"); len(v) != 0 {
		t.Errorf("UA-2-only check must not fire for UA-1: %v", v)
	}
}
