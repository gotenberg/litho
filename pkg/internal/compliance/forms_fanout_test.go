package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestFormFieldFanoutBomb pins that a crafted AcroForm field tree whose every
// node lists the same child twice (obj N /Kids [N+1 N+1]) cannot force
// exponential work. Without a visited-set the field walk fans out 2^depth and
// hangs (qpdf shared-unnamed-field.pdf). The visited-set bounds it to one visit
// per object, so validation completes promptly.
func TestFormFieldFanoutBomb(t *testing.T) {
	t.Parallel()

	// Build a chain of 60 field objects, each pointing at the next twice.
	const chain = 60
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Length 0 >>\nstream\n\nendstream", // object 4, unused placeholder
	}
	for i := range chain {
		objNum := 5 + i
		child := objNum + 1
		if i == chain-1 {
			objs = append(objs, "<< /FT /Tx /T (leaf) >>")
			continue
		}
		objs = append(objs, fmt.Sprintf("<< /Kids [%d 0 R %d 0 R] >>", child, child))
	}

	data := cyclePDF(objs...)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_, _ = ValidatePDFA(ctx, doc, models.PDFA2b)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("form field validation did not complete: fan-out bomb not bounded")
	}
}
