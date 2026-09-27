package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// fieldObjsPDF writes objs as objects 1..n of a classic-xref document.
func fieldObjsPDF(objs []string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for _, off := range offs {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// sharedFieldKidsPDF builds a form whose field p lists the /Kids array 4, and
// whose array 4 holds the direct fields a and d that name it again as their own
// /Kids, beside the direct signed signature field b and the direct text field
// c.
// Walked anew wherever it is reached, the array fans out 2^depth.
func sharedFieldKidsPDF() []byte {
	return fieldObjsPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] /SigFlags 3 >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"[<< /T (a) /Kids 4 0 R >> << /T (b) /FT /Sig /V 6 0 R >> << /T (c) /FT /Tx /V (x) >> << /T (d) /Kids 4 0 R >>]",
		"<< /T (p) /Kids 4 0 R >>",
		"<< /Type /Sig /ByteRange [0 1 2 3] /Contents <00> >>",
	})
}

// distinctNamesFieldsPDF builds a form of levels levels, each a nameless node
// listing two named fields whose /Kids both name the next level: a field below
// it has 2^levels fully qualified names in a file that grows linearly.
func distinctNamesFieldsPDF(levels int) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
	}
	for i := range levels {
		node := 4 + 3*i
		next := node + 3
		objs = append(objs,
			fmt.Sprintf("<< /Kids [%d 0 R %d 0 R] >>", node+1, node+2),
			fmt.Sprintf("<< /T (a) /Kids [%d 0 R] >>", next),
			fmt.Sprintf("<< /T (b) /Kids [%d 0 R] >>", next),
		)
	}
	objs = append(objs, "<< /T (leaf) /FT /Tx /V (v) >>")
	return fieldObjsPDF(objs)
}

// TestFieldWalks_SharedKidsArrayFanOut pins that the mutation walks over the
// form field tree read a /Kids array written as an object of its own once, so
// the direct fields that name it again cannot fan out 2^depth, and that each
// walk still reaches the field beside them.
func TestFieldWalks_SharedKidsArrayFanOut(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
		run  func(ctx context.Context, t *testing.T, data []byte) error
	}{
		{"SetFormField", sharedFieldKidsPDF(), func(ctx context.Context, t *testing.T, data []byte) error {
			doc, alloc := newMutationDoc(t, data)
			return SetFormField(ctx, alloc, doc, "p.c", "y")
		}},
		{"SetFormField distinct names", distinctNamesFieldsPDF(40), func(ctx context.Context, t *testing.T, data []byte) error {
			doc, alloc := newMutationDoc(t, data)
			if err := SetFormField(ctx, alloc, doc, "absent", "y"); err == nil {
				return fmt.Errorf("setting an absent field succeeded")
			}
			return nil
		}},
		{"SetFormField distinct names found", distinctNamesFieldsPDF(3), func(ctx context.Context, t *testing.T, data []byte) error {
			doc, alloc := newMutationDoc(t, data)
			return SetFormField(ctx, alloc, doc, "b.a.b.leaf", "y")
		}},
		{"signedFieldWalk", sharedFieldKidsPDF(), func(ctx context.Context, t *testing.T, data []byte) error {
			doc, _ := newMutationDoc(t, data)
			walk := newSignedFieldWalk(ctx, nil, doc, false)
			walk.list(ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 5}}, "", "", nil, 0)
			if len(walk.found) != 1 || walk.found[0].name != "p.b" {
				return fmt.Errorf("found %v, want the one signed field p.b", walk.found)
			}
			return nil
		}},
		{"dropSignedFields", sharedFieldKidsPDF(), func(ctx context.Context, t *testing.T, data []byte) error {
			doc, alloc := newMutationDoc(t, data)
			catalog, err := LiveCatalogDict(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})
			if err != nil {
				return err
			}
			dropSignedFields(ctx, alloc, doc, catalog)
			p, _ := alloc.Pending()[5].(ast.PdfDict)
			if kids, _ := p["Kids"].(ast.PdfArray); len(kids) != 3 {
				return fmt.Errorf("field p = %v, want its /Kids without the signed field b", p)
			}
			return nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := tt.run(ctx, t, tt.data); err != nil {
				t.Error(err)
			}
			if ctx.Err() != nil {
				t.Fatal("the walk did not end before the deadline")
			}
		})
	}
}
