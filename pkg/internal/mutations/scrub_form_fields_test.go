package mutations

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// widgetObj is a widget annotation with an optional /Parent and /AP, written so
// a test can place the value on the widget or on an ancestor.
func widgetObj(num int, extras string) string {
	return strconv.Itoa(num) + " 0 obj\n<< /Type /Annot /Subtype /Widget /Rect [0 0 20 10] " +
		extras + " >>\nendobj\n"
}

// fieldObj is a field dictionary stating a value.
func fieldObj(num int, value string) string {
	return strconv.Itoa(num) + " 0 obj\n<< /FT /Tx /T (field) /V (" + value + ") >>\nendobj\n"
}

func TestScrubFormFieldValues_ClearsAValueOnTheWidget(t *testing.T) {
	doc, _ := annotFixture(t, "[30 0 R]", "[]",
		widgetObj(30, "/FT /Tx /V (the SECRET value)"))
	alloc := writer.NewObjAllocator(50)

	scrubbed, err := ScrubFormFieldValues(context.Background(), alloc, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScrubFormFieldValues: %v", err)
	}
	if !slices.Contains(scrubbed, 30) {
		t.Fatalf("scrubbed = %v, want object 30", scrubbed)
	}
	newWidget, ok := alloc.Pending()[30].(ast.PdfDict)
	if !ok {
		t.Fatalf("object 30 = %T, want the rewritten widget", alloc.Pending()[30])
	}
	if _, present := newWidget["V"]; present {
		t.Error("/V survived: the field still holds the removed text")
	}
	if newWidget["Subtype"] != ast.PdfName("Widget") {
		t.Errorf("the widget lost its /Subtype: %v", newWidget)
	}
}

func TestScrubFormFieldValues_ClearsAValueOnAnAncestor(t *testing.T) {
	// The point of this pass. /V is inheritable, so the value the widget shows
	// is stated by a field dictionary that appears in no page's /Annots at all.
	// A scrub that only cleared what the annotation carries would leave it.
	doc, _ := annotFixture(t, "[31 0 R]", "[]",
		widgetObj(31, "/Parent 32 0 R")+fieldObj(32, "the SECRET value"))
	alloc := writer.NewObjAllocator(50)

	scrubbed, err := ScrubFormFieldValues(context.Background(), alloc, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScrubFormFieldValues: %v", err)
	}
	if !slices.Contains(scrubbed, 32) {
		t.Fatalf("scrubbed = %v, want the ancestor field 32", scrubbed)
	}
	owner, ok := alloc.Pending()[32].(ast.PdfDict)
	if !ok {
		t.Fatalf("object 32 = %T, want the rewritten field", alloc.Pending()[32])
	}
	if _, present := owner["V"]; present {
		t.Error("/V survived on the ancestor field")
	}
	// The field keeps its identity; only the value goes.
	if _, present := owner["T"]; !present {
		t.Error("the field lost its /T name")
	}
}

func TestScrubFormFieldValues_DirectParentAboveAnAddressableAncestor(t *testing.T) {
	// Regression: the walk passes THROUGH a direct dictionary on its way to an
	// indirect grandparent that states the value. Latching "not addressable" on
	// that hop refused a scrub that is perfectly possible, so the flag has to
	// describe the dictionary being examined rather than a link traversed.
	doc, _ := annotFixture(t, "[33 0 R]", "[]",
		widgetObj(33, "/Parent << /Parent 34 0 R >>")+fieldObj(34, "the SECRET value"))
	alloc := writer.NewObjAllocator(50)

	scrubbed, err := ScrubFormFieldValues(context.Background(), alloc, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScrubFormFieldValues: %v", err)
	}
	if !slices.Contains(scrubbed, 34) {
		t.Errorf("scrubbed = %v, want the grandparent field 34 reached through a direct parent", scrubbed)
	}
}

func TestScrubFormFieldValues_RefusesAValueStatedDirectly(t *testing.T) {
	// The dictionary stating /V is written straight into the widget, so there
	// is no object number for the replacement to take.
	doc, _ := annotFixture(t, "[35 0 R]", "[]",
		widgetObj(35, "/Parent << /FT /Tx /V (the SECRET value) >>"))
	alloc := writer.NewObjAllocator(50)

	_, err := ScrubFormFieldValues(context.Background(), alloc, doc, []string{"SECRET"})
	if !errors.Is(err, ErrFieldValueNotAddressable) {
		t.Errorf("err = %v, want ErrFieldValueNotAddressable", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

func TestScrubFormFieldValues_LeavesUnrelatedValuesAlone(t *testing.T) {
	doc, _ := annotFixture(t, "[36 0 R]", "[]",
		widgetObj(36, "/FT /Tx /V (an ordinary value)"))
	alloc := writer.NewObjAllocator(50)

	scrubbed, err := ScrubFormFieldValues(context.Background(), alloc, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScrubFormFieldValues: %v", err)
	}
	if len(scrubbed) != 0 {
		t.Errorf("scrubbed = %v, want none", scrubbed)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects, want none", n)
	}
}

func TestScrubFormFieldValues_DropsTheWidgetAppearance(t *testing.T) {
	// The appearance draws the old value, so clearing /V alone leaves it on
	// screen. A widget's appearance regenerates from the field value, which is
	// what makes dropping it recoverable here and not for a markup annotation.
	doc, _ := annotFixture(t, "[37 0 R]", "[]",
		widgetObj(37, "/FT /Tx /V (the SECRET value) /AP << /N 38 0 R >>")+
			"38 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 20 10] /Length 0 >>\nstream\n\nendstream\nendobj\n")
	alloc := writer.NewObjAllocator(50)

	if _, err := ScrubFormFieldValues(context.Background(), alloc, doc, []string{"SECRET"}); err != nil {
		t.Fatalf("ScrubFormFieldValues: %v", err)
	}
	newWidget, ok := alloc.Pending()[37].(ast.PdfDict)
	if !ok {
		t.Fatalf("object 37 = %T, want the rewritten widget", alloc.Pending()[37])
	}
	if _, present := newWidget["AP"]; present {
		t.Error("/AP survived: the appearance still draws the removed value")
	}
}

func TestScrubFormFieldValues_RefusalAfterScrubbingRegistersNothing(t *testing.T) {
	// Widget 30 is scrubbable and comes first; widget 35's value is stated
	// directly and refuses. Without the rollback, 30's rewrite stays queued.
	doc, _ := annotFixture(t, "[30 0 R 35 0 R]", "[]",
		widgetObj(30, "/FT /Tx /V (the SECRET value)")+
			widgetObj(35, "/Parent << /FT /Tx /V (another SECRET) >>"))
	alloc := writer.NewObjAllocator(50)

	_, err := ScrubFormFieldValues(context.Background(), alloc, doc, []string{"SECRET"})
	if !errors.Is(err, ErrFieldValueNotAddressable) {
		t.Fatalf("err = %v, want ErrFieldValueNotAddressable", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing: the earlier scrub was not rolled back", n)
	}
}

func TestScrubFormFieldValues_RejectsAnEmptyToken(t *testing.T) {
	doc, _ := annotFixture(t, "[30 0 R]", "[]", widgetObj(30, "/FT /Tx /V (value)"))
	alloc := writer.NewObjAllocator(50)

	_, err := ScrubFormFieldValues(context.Background(), alloc, doc, []string{"SECRET", ""})
	if !errors.Is(err, ErrScanTokenEmpty) {
		t.Errorf("err = %v, want ErrScanTokenEmpty", err)
	}
}
