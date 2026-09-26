package remediation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// TestResolveContentBytes_RefusesPastTheBound pins that the content-tagging
// fixer refuses a page whose /Contents parts sum past
// [limits.Limits.MaxPageContentBytes] with [limits.ErrPageContentTooLarge]:
// two 20 KiB parts pass a quarter of a 64 KiB stream budget, and read whole
// under the default one.
func TestResolveContentBytes_RefusesPastTheBound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	part := bytes.Repeat([]byte(" "), 20<<10)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents [4 0 R 5 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(part), part),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(part), part),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	data := b.Bytes()

	for _, tc := range []struct {
		name    string
		l       limits.Limits
		refused bool
	}{
		{"default", limits.Limits{}, false},
		{"64 KiB", limits.Limits{MaxStreamBytes: 64 << 10}, true},
	} {
		doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil, document.WithLimits(tc.l))
		if err != nil {
			t.Fatalf("%s: NewDocument: %v", tc.name, err)
		}
		page, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
		if err != nil {
			t.Fatalf("%s: resolving the page: %v", tc.name, err)
		}
		content, err := resolveContentBytes(ctx, nil, doc, page["Contents"])
		if tc.refused != errors.Is(err, limits.ErrPageContentTooLarge) || (!tc.refused && (err != nil || len(content) < 2*len(part))) {
			t.Errorf("%s: resolveContentBytes = %d bytes, %v; want the page refused %v", tc.name, len(content), err, tc.refused)
		}
	}
}
