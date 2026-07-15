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

// TestStructTreeFanoutBomb pins that a crafted structure tree whose every
// element lists the same /K child twice cannot force exponential work. The
// structure-tree walkers cap recursion depth but, without a visited-set, a DAG
// fan-out explodes 2^depth. The guard bounds it to one visit per element, so a
// PDF/UA validation over the tree completes promptly.
func TestStructTreeFanoutBomb(t *testing.T) {
	t.Parallel()

	const levels = 60
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Type /StructTreeRoot /K [6 0 R 6 0 R] >>",
	}
	for i := range levels {
		objn := 6 + i
		child := objn + 1
		if i == levels-1 {
			objs = append(objs, "<< /Type /StructElem /S /P /P 5 0 R >>")
			continue
		}
		objs = append(objs, fmt.Sprintf("<< /Type /StructElem /S /Div /K [%d 0 R %d 0 R] /P 5 0 R >>", child, child))
	}

	data := cyclePDF(objs...)

	done := make(chan struct{})
	go func() {
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err == nil {
			_, _ = ValidatePDFUA(context.Background(), doc, models.PDFUA1)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("structure-tree validation did not complete: fan-out not bounded")
	}
}
