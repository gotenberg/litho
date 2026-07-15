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

// TestPageTreeFanoutBomb pins that a crafted page tree whose every node lists
// the same child twice (obj N /Kids [N+1 N+1]) cannot force exponential work.
// The core page-tree walk detects cycles only along the current path, so without
// a global "already traversed" guard a DAG fan-out explodes 2^depth in both the
// document page resolver and the compliance page collector. The guards bound
// both to one visit per node, so building the document and validating it finish
// promptly and see exactly one page.
func TestPageTreeFanoutBomb(t *testing.T) {
	t.Parallel()

	const levels = 60
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 3 0 R] /Count 1 >>",
	}
	for i := range levels {
		objn := 3 + i
		child := objn + 1
		if i == levels-1 {
			objs = append(objs, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>")
			continue
		}
		objs = append(objs, fmt.Sprintf("<< /Type /Pages /Kids [%d 0 R %d 0 R] /Count 1 /Parent 2 0 R >>", child, child))
	}

	data := cyclePDF(objs...)

	done := make(chan int, 1)
	go func() {
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			done <- -1
			return
		}
		vs, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
		if err != nil {
			done <- -2
			return
		}
		done <- len(vs)
	}()

	select {
	case n := <-done:
		if n == -1 || n == -2 {
			t.Fatalf("document build/validation errored on the page-tree bomb (code %d)", n)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("page-tree validation did not complete: fan-out not bounded")
	}
}
