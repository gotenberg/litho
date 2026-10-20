package mutations

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestScanOutputForTokens_WalksPastTheReferenceBound pins that a reference
// chain longer than maxScanRefDepth is scanned to its end, not reported
// unreadable. The depth at which the walk first meets an object depends on the
// order Go ranges a dictionary in, so reporting what lay past the bound
// unreadable failed the same file closed on some runs and not others.
func TestScanOutputForTokens_WalksPastTheReferenceBound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const first, links = 10, 3 * maxScanRefDepth

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /PieceInfo << /Scan << /Private " + strconv.Itoa(first) + " 0 R >> >> >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")
	for i := range links {
		num := first + i
		body := "<< /Next " + strconv.Itoa(num+1) + " 0 R >>"
		if i == links-1 {
			body = "<< /Last (DEEPSECRET) >>"
		}
		buf.WriteString(strconv.Itoa(num) + " 0 obj\n" + body + "\nendobj\n")
	}
	buf.WriteString("trailer\n<< /Size " + strconv.Itoa(first+links) + " /Root 1 0 R >>\n%%EOF\n")
	data := buf.Bytes()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	res, err := ScanOutputForTokens(ctx, nil, doc, []string{"DEEPSECRET"})
	if err != nil {
		t.Fatalf("ScanOutputForTokens: %v", err)
	}
	if len(res.Unreadable) != 0 {
		t.Errorf("unreadable = %v, want none: every link resolves", res.Unreadable)
	}
	if len(res.Hits) != 1 || res.Hits[0].ObjectNum != first+links-1 {
		t.Errorf("hits = %+v, want the token at the end of the chain, object %d", res.Hits, first+links-1)
	}
}
