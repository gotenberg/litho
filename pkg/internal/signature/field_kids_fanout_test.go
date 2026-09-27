package signature

import (
	"bytes"
	"context"
	"slices"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestExtractSignatures_SharedKidsArrayFanOut pins that both field walks of
// [ExtractSignatures] visit a /Kids array written as an object of its own
// once. Its direct fields each name it again as their /Kids, and walked anew
// wherever it is reached it fans out 2^depth up to maxFieldTreeDepth, and
// reports the direct signature field once per reach.
func TestExtractSignatures_SharedKidsArrayFanOut(t *testing.T) {
	t.Parallel()

	data := buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"[<< /T (a) /FT /Sig /V 6 0 R /Kids 4 0 R >> << /T (b) /Kids 4 0 R >>]",
		"<< /T (p) /Kids 4 0 R >>",
		"<< /Type /Sig /ByteRange [0 100 200 50] /Contents <30820101> /M (D:20240101000000Z) >>",
	}, "")

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	done := make(chan []string, 1)
	go func() {
		sigs, err := ExtractSignatures(ctx, doc)
		if err != nil {
			t.Errorf("ExtractSignatures: %v", err)
		}
		names := make([]string, len(sigs))
		for i, s := range sigs {
			names[i] = s.FieldName
		}
		done <- names
	}()
	select {
	case names := <-done:
		if !slices.Equal(names, []string{"p.a"}) {
			t.Errorf("ExtractSignatures reported %v, want [p.a]", names)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the walk did not end before the deadline")
	}
}
