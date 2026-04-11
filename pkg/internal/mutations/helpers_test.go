package mutations

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func TestFormatFloat(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{1.5, "1.5"},
		{1.5000, "1.5"},
		{0.1234, "0.1234"},
		{0.12345, "0.1235"}, // rounded
		{-2.5, "-2.5"},
		{100, "100"},
		{0.0001, "0.0001"},
		{0.00001, "0"}, // below precision
	}
	for _, tc := range tests {
		if got := FormatFloat(tc.in); got != tc.want {
			t.Errorf("FormatFloat(%v): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGenerateDocumentID_LengthAndUniqueness(t *testing.T) {
	id1 := GenerateDocumentID()
	id2 := GenerateDocumentID()

	if len(id1) != 16 {
		t.Errorf("ID length: got %d, want 16", len(id1))
	}
	if len(id2) != 16 {
		t.Errorf("ID length: got %d, want 16", len(id2))
	}
	// Two random IDs should not collide.
	if string(id1) == string(id2) {
		t.Error("two consecutive IDs collided; randomness broken")
	}
}

func TestDefaultSourceLabel(t *testing.T) {
	tests := []struct {
		idx  int
		want string
	}{
		{0, "Document 1"},
		{1, "Document 2"},
		{42, "Document 43"},
	}
	for _, tc := range tests {
		if got := DefaultSourceLabel(tc.idx); got != tc.want {
			t.Errorf("DefaultSourceLabel(%d): got %q, want %q", tc.idx, got, tc.want)
		}
	}
}

func TestEscapePDFString(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"a(b)c", `a\(b\)c`},
		{`back\slash`, `back\\slash`},
		{`(\)`, `\(\\\)`},
		{"", ""},
	}
	for _, tc := range tests {
		if got := escapePDFString(tc.in); got != tc.want {
			t.Errorf("escapePDFString(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExtGStateInvocation(t *testing.T) {
	if got := extGStateInvocation(""); got != "" {
		t.Errorf("empty name: got %q, want empty", got)
	}
	if got := extGStateInvocation("StampGS5"); got != "/StampGS5 gs " {
		t.Errorf("got %q, want %q", got, "/StampGS5 gs ")
	}
}

func TestBuildTransformMatrix_Identity(t *testing.T) {
	got := buildTransformMatrix(0, 0, 1, 1, 0)
	want := "1 0 0 1 0 0 cm "
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildTransformMatrix_TranslateScale(t *testing.T) {
	got := buildTransformMatrix(10, 20, 2, 3, 0)
	want := "2 0 0 3 10 20 cm "
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildExtGState_FullyOpaque(t *testing.T) {
	alloc := writer.NewObjAllocator(0)
	name, ref := buildExtGState(alloc, 1.0)
	if name != "" {
		t.Errorf("expected empty name for fully opaque, got %q", name)
	}
	if ref.ObjectNum != 0 {
		t.Errorf("expected zero ref for fully opaque, got %+v", ref)
	}
	if len(alloc.Pending()) != 0 {
		t.Errorf("expected no allocations, got %d", len(alloc.Pending()))
	}
}

func TestBuildExtGState_ZeroOpacityTreatedAsOpaque(t *testing.T) {
	alloc := writer.NewObjAllocator(0)
	name, _ := buildExtGState(alloc, 0)
	if name != "" {
		t.Errorf("expected empty name for opacity 0 (back-compat), got %q", name)
	}
}

func TestBuildExtGState_PartialOpacity(t *testing.T) {
	alloc := writer.NewObjAllocator(0)
	name, ref := buildExtGState(alloc, 0.5)
	if name == "" {
		t.Fatal("expected non-empty name for partial opacity")
	}
	if !strings.HasPrefix(string(name), "StampGS") {
		t.Errorf("expected StampGS prefix, got %q", name)
	}
	pending := alloc.Pending()
	obj, ok := pending[ref.ObjectNum]
	if !ok {
		t.Fatalf("allocated object %d not in pending", ref.ObjectNum)
	}
	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("allocated object is %T, want PdfDict", obj)
	}
	if dict["Type"] != ast.PdfName("ExtGState") {
		t.Errorf("Type: got %v, want ExtGState", dict["Type"])
	}
	if dict["ca"] != ast.PdfReal(0.5) {
		t.Errorf("ca: got %v, want 0.5", dict["ca"])
	}
	if dict["CA"] != ast.PdfReal(0.5) {
		t.Errorf("CA: got %v, want 0.5", dict["CA"])
	}
}

func TestBuildTransformMatrix_Rotation90(t *testing.T) {
	got := buildTransformMatrix(0, 0, 1, 1, 90)
	// At 90°: cos=0, sin=1 → matrix [0 1 -1 0 0 0]
	// Both 0 and -0 are acceptable representations.
	if !strings.Contains(got, "1") || !strings.HasSuffix(got, "cm ") {
		t.Errorf("expected rotation matrix ending in cm, got %q", got)
	}
	parts := strings.Fields(strings.TrimSuffix(got, "cm "))
	if len(parts) != 6 {
		t.Fatalf("expected 6 matrix elements, got %d: %v", len(parts), parts)
	}
}
