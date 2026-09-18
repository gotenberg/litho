package graphics

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// imageResources holds one image and one form XObject, both embedded directly
// so the lookup needs no document: [document.ResolveObject] only dereferences
// an indirect reference.
func imageResources() ast.PdfDict {
	return ast.PdfDict{
		"XObject": ast.PdfDict{
			"Im1": ast.NewStream(ast.PdfDict{"Subtype": ast.PdfName("Image")}, nil),
			"Fm1": ast.NewStream(ast.PdfDict{"Subtype": ast.PdfName("Form")}, nil),
		},
	}
}

func doOp(name string) content.Operation {
	return content.Operation{Operator: "Do", Operands: []ast.PdfObject{ast.PdfName(name)}}
}

func TestImageBoxes_SilentByDefault(t *testing.T) {
	m := testMachine()
	m.Resources = imageResources()
	m.Current().CTM = [6]float64{100, 0, 0, 50, 10, 20}

	if err := m.Execute(context.Background(), []content.Operation{doOp("Im1")}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := m.ImageBoxes(); len(got) != 0 {
		t.Errorf("ImageBoxes() = %v, want none while recording is off", got)
	}
	if got := m.UnresolvedXObjects(); got != 0 {
		t.Errorf("UnresolvedXObjects() = %d, want 0 while recording is off", got)
	}
}

func TestImageBoxes_RecordsPlacementWhenAsked(t *testing.T) {
	m := testMachine()
	m.RecordImageBoxes = true
	m.Resources = imageResources()
	// An image fills the unit square in its own space, so this CTM puts it at
	// (10,20) and makes it 100 wide by 50 tall.
	m.Current().CTM = [6]float64{100, 0, 0, 50, 10, 20}

	if err := m.Execute(context.Background(), []content.Operation{doOp("Im1")}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	boxes := m.ImageBoxes()
	if len(boxes) != 1 {
		t.Fatalf("ImageBoxes() = %v, want one box", boxes)
	}
	got := boxes[0]
	want := ImageBox{Name: "Im1", MinX: 10, MinY: 20, MaxX: 110, MaxY: 70}
	if got != want {
		t.Errorf("box = %+v, want %+v", got, want)
	}
	if n := m.UnresolvedXObjects(); n != 0 {
		t.Errorf("UnresolvedXObjects() = %d, want 0: the image resolved", n)
	}
}

func TestImageBoxes_FlippedCTMReportsTheAreaCovered(t *testing.T) {
	m := testMachine()
	m.RecordImageBoxes = true
	m.Resources = imageResources()
	// Negative scale mirrors the image about its origin. The box still has to
	// name the area it covers, not the corners in the order they were computed.
	m.Current().CTM = [6]float64{-40, 0, 0, -30, 100, 200}

	if err := m.Execute(context.Background(), []content.Operation{doOp("Im1")}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	boxes := m.ImageBoxes()
	if len(boxes) != 1 {
		t.Fatalf("ImageBoxes() = %v, want one box", boxes)
	}
	want := ImageBox{Name: "Im1", MinX: 60, MinY: 170, MaxX: 100, MaxY: 200}
	if boxes[0] != want {
		t.Errorf("box = %+v, want %+v", boxes[0], want)
	}
}

func TestImageBoxes_InlineImageIsRecorded(t *testing.T) {
	m := testMachine()
	m.RecordImageBoxes = true
	// No resources at all: an inline image carries its data in the content
	// stream, so nothing has to be looked up for it.
	m.Current().CTM = [6]float64{80, 0, 0, 40, 5, 15}

	bi := content.Operation{
		Operator: "BI",
		Operands: []ast.PdfObject{ast.PdfDict{"W": ast.PdfInt(1), "H": ast.PdfInt(1)}},
	}
	if err := m.Execute(context.Background(), []content.Operation{bi}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	boxes := m.ImageBoxes()
	if len(boxes) != 1 {
		t.Fatalf("ImageBoxes() = %v, want one box", boxes)
	}
	want := ImageBox{Name: inlineImageName, MinX: 5, MinY: 15, MaxX: 85, MaxY: 55}
	if boxes[0] != want {
		t.Errorf("box = %+v, want %+v", boxes[0], want)
	}
	// Nothing was looked up, so nothing can have failed to resolve.
	if n := m.UnresolvedXObjects(); n != 0 {
		t.Errorf("UnresolvedXObjects() = %d, want 0 for an inline image", n)
	}
}

func TestImageBoxes_InlineImageSilentByDefault(t *testing.T) {
	m := testMachine()
	m.Current().CTM = [6]float64{80, 0, 0, 40, 5, 15}

	bi := content.Operation{Operator: "BI", Operands: []ast.PdfObject{ast.PdfDict{}}}
	if err := m.Execute(context.Background(), []content.Operation{bi}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := m.ImageBoxes(); len(got) != 0 {
		t.Errorf("ImageBoxes() = %v, want none while recording is off", got)
	}
}

func TestImageBoxes_FormXObjectIsNotRecorded(t *testing.T) {
	m := testMachine()
	m.RecordImageBoxes = true
	m.Resources = imageResources()
	m.Current().CTM = [6]float64{100, 0, 0, 50, 10, 20}

	if err := m.Execute(context.Background(), []content.Operation{doOp("Fm1")}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// A form paints through its own content stream, so this operator has no
	// geometry of its own to report. It resolved, so it is not unresolved
	// either: that is the distinction a caller needs.
	if got := m.ImageBoxes(); len(got) != 0 {
		t.Errorf("ImageBoxes() = %v, want none for a form", got)
	}
	if got := m.UnresolvedXObjects(); got != 0 {
		t.Errorf("UnresolvedXObjects() = %d, want 0: the form resolved, it just paints elsewhere", got)
	}
}

func TestImageBoxes_UnresolvableDoIsCounted(t *testing.T) {
	cases := []struct {
		name      string
		resources ast.PdfDict
		op        content.Operation
	}{
		{"no operand", imageResources(), content.Operation{Operator: "Do"}},
		{
			"operand is not a name",
			imageResources(),
			content.Operation{Operator: "Do", Operands: []ast.PdfObject{ast.PdfInt(7)}},
		},
		{"no resources at all", nil, doOp("Im1")},
		{"name absent from resources", imageResources(), doOp("Missing")},
		{"XObject entry is not a dict", ast.PdfDict{"XObject": ast.PdfInt(1)}, doOp("Im1")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := testMachine()
			m.RecordImageBoxes = true
			m.Resources = tc.resources

			if err := m.Execute(context.Background(), []content.Operation{tc.op}); err != nil {
				t.Fatalf("Execute: %v", err)
			}

			if got := m.ImageBoxes(); len(got) != 0 {
				t.Errorf("ImageBoxes() = %v, want none", got)
			}
			// The count is the whole point: a caller clearing a region must be
			// able to tell "nothing here" from "I could not tell".
			if got := m.UnresolvedXObjects(); got != 1 {
				t.Errorf("UnresolvedXObjects() = %d, want 1", got)
			}
		})
	}
}
