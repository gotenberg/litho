package document

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// ocVisibleDoc returns a document whose default configuration turns group 5
// off and leaves group 6 on. Object 7 is a visibility expression that names
// itself, object 8 one shared by reference, and object 9 a dictionary of no
// optional content type.
func ocVisibleDoc(t *testing.T) *Document {
	t.Helper()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [5 0 R 6 0 R] /D << /OFF [5 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< >>",
		"<< /Type /OCG /Name (Off) >>",
		"<< /Type /OCG /Name (On) >>",
		"[/Not 7 0 R]",
		"[/Or 5 0 R 6 0 R]",
		"<< /Type /Font >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	start := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	data := buf.Bytes()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

func ocRef(n int) ast.PdfIndirectRef { return ast.PdfIndirectRef{ObjectNum: n} }

func ocmd(entries ...any) ast.PdfDict {
	d := ast.PdfDict{"Type": ast.PdfName("OCMD")}
	for i := 0; i+1 < len(entries); i += 2 {
		d[ast.PdfName(entries[i].(string))] = entries[i+1].(ast.PdfObject)
	}
	return d
}

// TestOCVisible pins the visibility of optional content groups and
// membership dictionaries (ISO 32000-1, 8.11.2.2, Table 99) under the
// default configuration.
func TestOCVisible(t *testing.T) {
	t.Parallel()
	doc := ocVisibleDoc(t)
	off, on := ocRef(5), ocRef(6)
	both := ast.PdfArray{off, on}
	for _, tc := range []struct {
		name string
		oc   ast.PdfObject
		want bool
	}{
		{"no entry", nil, true},
		{"group off", off, false},
		{"group on", on, true},
		{"another type", ocRef(9), true},
		{"unresolvable", ocRef(42), true},
		{"default policy AnyOn", ocmd("OCGs", both), true},
		{"AnyOn over the group off", ocmd("OCGs", off), false},
		{"single group as a dictionary", ocmd("OCGs", ocRef(5)), false},
		{"AllOn", ocmd("OCGs", both, "P", ast.PdfName("AllOn")), false},
		{"AnyOff", ocmd("OCGs", both, "P", ast.PdfName("AnyOff")), true},
		{"AllOff", ocmd("OCGs", both, "P", ast.PdfName("AllOff")), false},
		{"AllOff over the group off", ocmd("OCGs", ast.PdfArray{off}, "P", ast.PdfName("AllOff")), true},
		{"no group has no effect", ocmd("OCGs", ast.PdfArray{}), true},
		{"entries that are no group are ignored", ocmd("OCGs", ast.PdfArray{ast.PdfNull{}, ocRef(9), off}), false},
		{"VE Not", ocmd("VE", ast.PdfArray{ast.PdfName("Not"), on}), false},
		{"VE And", ocmd("VE", ast.PdfArray{ast.PdfName("And"), on, off}), false},
		{"VE nested", ocmd("VE", ast.PdfArray{ast.PdfName("Not"), ast.PdfArray{ast.PdfName("And"), on, off}}), true},
		{"VE shared by reference", ocmd("VE", ast.PdfArray{ast.PdfName("And"), ocRef(8), ocRef(8)}), true},
		{"VE takes precedence over OCGs", ocmd("OCGs", on, "VE", ast.PdfArray{ast.PdfName("Not"), on}), false},
		{"VE naming itself falls back to OCGs", ocmd("OCGs", off, "VE", ocRef(7)), false},
		{"malformed VE falls back to OCGs", ocmd("OCGs", on, "VE", ast.PdfArray{ast.PdfName("Xor"), on}), true},
	} {
		if got := OCVisible(context.Background(), doc, tc.oc); got != tc.want {
			t.Errorf("%s: OCVisible = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestOCVisibleDeepExpression pins that an expression nested past
// [maxVisibilityExpressionDepth] is read as malformed, not walked.
func TestOCVisibleDeepExpression(t *testing.T) {
	t.Parallel()
	doc := ocVisibleDoc(t)
	var ve ast.PdfObject = ocRef(6)
	for range maxVisibilityExpressionDepth + 1 {
		ve = ast.PdfArray{ast.PdfName("Not"), ast.PdfArray{ast.PdfName("Not"), ve}}
	}
	if !OCVisible(context.Background(), doc, ocmd("OCGs", ocRef(6), "VE", ve)) {
		t.Error("a too deep expression did not fall back to /OCGs")
	}
}

// TestPropertiesOCs pins that the /Properties names of membership
// dictionaries map to their visibility alongside those of groups.
func TestPropertiesOCs(t *testing.T) {
	t.Parallel()
	doc := ocVisibleDoc(t)
	res := ast.PdfDict{"Properties": ast.PdfDict{
		"G":  ocRef(5),
		"M":  ocmd("OCGs", ocRef(6), "P", ast.PdfName("AllOff")),
		"MC": ast.PdfDict{"MCID": ast.PdfInt(1)},
	}}
	got := PropertiesOCs(context.Background(), doc, res)
	var parts []string
	for _, k := range []string{"G", "M", "MC"} {
		v, ok := got[k]
		parts = append(parts, fmt.Sprintf("%s=%v/%v", k, v, ok))
	}
	if s := strings.Join(parts, " "); s != "G=false/true M=false/true MC=false/false" {
		t.Errorf("PropertiesOCs = %s", s)
	}
}
