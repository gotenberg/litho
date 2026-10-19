package tagging

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestAutoTag_ArtifactEndsBeforeADanglingToken pins that every EMC AutoTag
// writes is read as an operator. A page whose content stream ends inside a
// literal or hex string that never terminates swallows whatever follows it, so
// an EMC appended after the last byte lands inside the string and leaves the
// /Artifact sequence open, which ISO 32000-1, 14.6 does not make a
// marked-content sequence (ISO 14289-1, 7.1 t3).
func TestAutoTag_ArtifactEndsBeforeADanglingToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for name, tail := range map[string]string{
		"literal string": "(dangling ( string",
		"hex string":     "<0A1B",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			page := "BT /F1 12 Tf 72 700 Td (Body text on this page.) Tj ET\n" +
				"72 695 m 540 695 l S\n" + tail
			marked, _ := autoTagPages(t, []string{page})
			ops, err := content.ParseStreamData(ctx, []byte(marked[0]))
			if err != nil {
				t.Fatalf("parse marked content: %v", err)
			}
			depth := 0
			for _, op := range ops {
				switch op.Operator {
				case "BMC", "BDC":
					depth++
				case "EMC":
					depth--
				}
			}
			if depth != 0 {
				t.Errorf("marked content leaves %d sequence(s) open:\n%s", depth, marked[0])
			}
		})
	}
}
