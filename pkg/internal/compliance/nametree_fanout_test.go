package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestNameTreeFanoutBomb pins that an EmbeddedFiles name tree whose every node
// lists the same /Kids child twice (with no pruning /Limits) cannot fan out
// 2^depth during the PDF/UA-2 embedded-file checks. Without a visited-set the
// walk explored every path and hung.
func TestNameTreeFanoutBomb(t *testing.T) {
	t.Parallel()

	const levels = 60
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 5 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
	}
	for i := range levels {
		objn := 5 + i
		child := objn + 1
		if i == levels-1 {
			objs = append(objs, "<< /Names [] >>")
			continue
		}
		objs = append(objs, fmt.Sprintf("<< /Kids [%d 0 R %d 0 R] >>", child, child))
	}

	data := cyclePDF(objs...)
	runWithinDeadline(t, data, func(doc *document.Document) {
		_, _ = ValidatePDFUA(context.Background(), doc, models.PDFUA2)
	})
}
