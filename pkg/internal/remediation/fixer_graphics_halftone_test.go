package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// halftoneDoc parses a document whose object 1 is the halftone body and
// object 2 the object it may name.
func halftoneDoc(t *testing.T, body, second string) *document.Document {
	t.Helper()
	objs := []string{body, second}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	data := b.Bytes()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// TestNormalizeHalftoneReplacesWhatCompliancePasses pins that the halftone
// repair clears what ISO 19005-2, 6.2.5 t4 to t6 now read: a halftone named
// by a name other than /Default and a type 6 halftone written as a stream
// become /Default, and the halftone a graphics state names loses its own
// /TransferFunction, a /Default included, while /Default itself stays.
func TestNormalizeHalftoneReplacesWhatCompliancePasses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const t1 = "/Type /Halftone /HalftoneType 1 /Frequency 60 /Angle 45 /SpotFunction /Round"
	tests := []struct {
		name, body string
		wantName   ast.PdfName
		wantTF     bool
		changed    bool
	}{
		{"a name other than Default", "/Foo", "Default", false, true},
		{"Default", "/Default", "", false, false},
		{"a type 6 stream", "<< /Type /Halftone /HalftoneType 6 /Width 1 /Height 1 /Length 1 >>\nstream\n\x00\nendstream", "Default", false, true},
		{"a transfer function named Default", "<< " + t1 + " /TransferFunction /Default >>", "", false, true},
		{"no transfer function", "<< " + t1 + " >>", "", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := halftoneDoc(t, tt.body, "null")
			patched, changed := normalizeHalftone(ctx, doc, ast.PdfIndirectRef{ObjectNum: 1})
			if changed != tt.changed {
				t.Fatalf("changed = %v, want %v", changed, tt.changed)
			}
			if !changed {
				return
			}
			if tt.wantName != "" {
				if patched != tt.wantName {
					t.Errorf("patched = %v, want /%s", patched, tt.wantName)
				}
				return
			}
			dict, ok := patched.(ast.PdfDict)
			if !ok {
				t.Fatalf("patched = %T, want a dictionary", patched)
			}
			if _, has := dict["TransferFunction"]; has != tt.wantTF {
				t.Errorf("TransferFunction kept = %v, want %v", has, tt.wantTF)
			}
		})
	}
}
