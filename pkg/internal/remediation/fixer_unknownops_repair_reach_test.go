package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// malformedTm is a text matrix whose first operand is a token the content
// parser cannot read as a number.
//
// ISO 32000-1, 7.3.3 writes a real as an optional sign, digits and one optional
// decimal point, so "0.0.0" is not a number and the parser surfaces it as an
// operator. That splits the operand run, and removing it as an operator
// undefined by ISO 19005-2, 6.2.2 takes the operands in front of it: Tm arrives
// with fewer than the six operands Table 108 requires and no text matrix is set.
//
// The token carries digits on purpose. The digit-less spellings ("-.", "-",
// "+", ".") no longer reach this fixer at all: the lexer resolves them to 0 the
// way Adobe Reader and pdf.js do, so they arrive as operands. See
// TestFixUnknownOps_DigitlessNumberNeedsNoRepair. Only a malformed token that
// still looks numeric gets this far, which makes the reachable input narrower
// than the digit-less case that motivated the repair, but the same defect.
//
// # Corpus evidence
//
//   - pdfjs issue15604: its digit-less "-." operands are the case that
//     motivated the repair.
const malformedTm = "0.0.0 0 0 1 59.64 760.026 Tm"

// repairReachPDF builds a document that puts the malformed text matrix in one
// of three places, so each of fixUnknownOps' three strip paths can be measured
// separately with everything else held equal.
//
// Object 7 is the annotation appearance stream, object 5 the form XObject the
// page invokes, and object 4 the page content itself.
func repairReachPDF(where string) []byte {
	const good = "BT /F1 12 Tf 20 700 Td (body) Tj ET\n"
	broken := "BT /F1 12 Tf " + malformedTm + " (text) Tj ET\n"

	page, form, appearance := good, good, good
	switch where {
	case "page":
		page = broken
	case "form":
		form = broken
	case "appearance":
		appearance = broken
	}
	page += "q /Fm0 Do Q\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 6 0 R >> /XObject << /Fm0 5 0 R >> >>" +
			" /Annots [8 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 50] /Length %d >>\nstream\n%s\nendstream",
			len(form), form),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 50] /Length %d >>\nstream\n%s\nendstream",
			len(appearance), appearance),
		"<< /Type /Annot /Subtype /Widget /Rect [100 100 200 150] /F 4 /AP << /N 7 0 R >> >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestFixUnknownOps_RepairsEveryStreamItStrips pins that the malformed-number
// repair reaches every stream the fixer rewrites, not only the page content.
//
// fixUnknownOps strips undefined operators from three places: the page content
// stream, the Form XObjects named in page resources, and the appearance streams
// hanging off /Annots[]/AP. repairMalformedNumbers guards the first against
// taking a real operator's operands with the token it removes. It was called
// from that path alone, so the same stream body lost its text matrix in a form
// or an appearance while surviving intact on a page.
//
// The premise the fixer's own godoc states, that removal is content-preserving
// because the operators are dead bytes at the viewer level, holds only for a
// real operator. A mangled operand is not dead bytes: it is part of the next
// operator's arguments, wherever the stream lives.
func TestFixUnknownOps_RepairsEveryStreamItStrips(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		where  string
		objNum int
	}{
		{"page", 4},
		{"form", 5},
		{"appearance", 7},
	} {
		t.Run("in the "+tc.where+" stream", func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := repairReachPDF(tc.where)
			doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))

			if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, ""); err != nil {
				t.Fatalf("fixUnknownOps: %v", err)
			}

			queued, ok := alloc.Pending()[tc.objNum].(ast.PdfStream)
			if !ok {
				t.Fatalf("object %d was not rewritten: %T", tc.objNum, alloc.Pending()[tc.objNum])
			}
			body, err := queued.Bytes(ctx)
			if err != nil {
				t.Fatalf("reading the rewritten stream: %v", err)
			}
			if !bytes.Contains(body, []byte("0 0 0 1 59.64 760.026 Tm")) {
				t.Errorf("Tm lost operands in the %s stream, so no text matrix is ever set:\n%s", tc.where, body)
			}
		})
	}
}
