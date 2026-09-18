package graphics

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// runPathOps executes src the way a caller walking a content stream does,
// naming each operator before it runs. The machine cannot know the index on
// its own, so a test that skipped this would record meaningless spans.
func runPathOps(t *testing.T, m *Machine, src string) {
	t.Helper()
	ctx := context.Background()

	ops, err := content.ParseStreamData(ctx, []byte(src))
	if err != nil {
		t.Fatalf("ParseStreamData: %v", err)
	}
	for i, op := range ops {
		m.OpIndex = i
		// Unsupported operators must not abort the walk, matching the caller.
		_ = m.ExecuteOne(ctx, op) //nolint:errcheck // Deliberate: mirrors the extraction caller, which ignores per-operator errors.
	}
}

func TestPathBoxes_SpanCoversTheWholeRun(t *testing.T) {
	t.Parallel()

	// re(0) f(1). The span has to reach back to the re: excising the f alone
	// would leave the rectangle to be painted by whatever operator came next.
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.RecordPathBoxes = true
	runPathOps(t, m, "10 20 100 50 re f")

	boxes := m.PathBoxes()
	if len(boxes) != 1 {
		t.Fatalf("PathBoxes() = %v, want exactly one box", boxes)
	}
	got := boxes[0]
	want := PathBox{MinX: 10, MinY: 20, MaxX: 110, MaxY: 70, StartOp: 0, EndOp: 1}
	if got != want {
		t.Errorf("box = %+v, want %+v", got, want)
	}
}

func TestPathBoxes_SpanStartsAtTheFirstConstructionOperator(t *testing.T) {
	t.Parallel()

	// q(0) cm(1) m(2) l(3) l(4) h(5) f(6). The run starts at the m, not at the
	// q or the cm, which set up state the rest of the page still relies on.
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.RecordPathBoxes = true
	runPathOps(t, m, "q 1 0 0 1 0 0 cm 10 10 m 30 10 l 30 30 l h f Q")

	boxes := m.PathBoxes()
	if len(boxes) != 1 {
		t.Fatalf("PathBoxes() = %v, want exactly one box", boxes)
	}
	got := boxes[0]
	want := PathBox{MinX: 10, MinY: 10, MaxX: 30, MaxY: 30, StartOp: 2, EndOp: 6}
	if got != want {
		t.Errorf("box = %+v, want %+v", got, want)
	}
}

func TestPathBoxes_ClipInTheRunMarksTheBox(t *testing.T) {
	t.Parallel()

	// re(0) W(1) f(2) both paints and narrows the clip, so a caller removing it
	// would widen the clip for everything drawn afterwards.
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.RecordPathBoxes = true
	runPathOps(t, m, "10 10 100 100 re W f")

	boxes := m.PathBoxes()
	if len(boxes) != 1 {
		t.Fatalf("PathBoxes() = %v, want exactly one box", boxes)
	}
	if !boxes[0].Clipped {
		t.Errorf("box = %+v, want Clipped: a W took part in the path", boxes[0])
	}
	if boxes[0].StartOp != 0 || boxes[0].EndOp != 2 {
		t.Errorf("span = [%d,%d], want [0,2]", boxes[0].StartOp, boxes[0].EndOp)
	}
}

func TestPathBoxes_ClipViaNDoesNotLeakIntoTheNextPath(t *testing.T) {
	t.Parallel()

	// re(0) W(1) n(2) re(3) f(4). The W n sets a clip and paints nothing, so it
	// is not a box at all. The path after it is an ordinary removable one: if
	// the n left the clip flag or the start offset behind, that path would be
	// reported as clipped and unremovable, and its span would start too early.
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.RecordPathBoxes = true
	runPathOps(t, m, "0 0 50 50 re W n 10 10 20 20 re f")

	boxes := m.PathBoxes()
	if len(boxes) != 1 {
		t.Fatalf("PathBoxes() = %v, want exactly one box: the W n paints nothing", boxes)
	}
	got := boxes[0]
	want := PathBox{MinX: 10, MinY: 10, MaxX: 30, MaxY: 30, StartOp: 3, EndOp: 4}
	if got != want {
		t.Errorf("box = %+v, want %+v", got, want)
	}
}

func TestPathBoxes_CarriesFormDepth(t *testing.T) {
	t.Parallel()

	// A path drawn while the caller is inside a form must say so: its operators
	// live in the form's stream, where no offset into the page's reaches them.
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.RecordPathBoxes = true
	m.FormDepth = 1
	runPathOps(t, m, "10 20 100 50 re f")

	boxes := m.PathBoxes()
	if len(boxes) != 1 {
		t.Fatalf("PathBoxes() = %v, want exactly one box", boxes)
	}
	if boxes[0].FormDepth != 1 {
		t.Errorf("FormDepth = %d, want 1", boxes[0].FormDepth)
	}
}

func TestPathBoxes_OffByDefault(t *testing.T) {
	t.Parallel()

	// The common caller extracts text and must not pay for geometry it never
	// reads.
	m := NewMachine([4]float64{0, 0, 612, 792})
	runPathOps(t, m, "10 20 100 50 re f")

	if boxes := m.PathBoxes(); len(boxes) != 0 {
		t.Errorf("PathBoxes() = %v, want none without RecordPathBoxes", boxes)
	}
}

func TestPathBoxes_PaintWithNoPathRecordsNothing(t *testing.T) {
	t.Parallel()

	// A paint operator with nothing constructed marks nothing, so it is not a
	// box. Recording one would hand a caller an empty span to excise.
	m := NewMachine([4]float64{0, 0, 612, 792})
	m.RecordPathBoxes = true
	runPathOps(t, m, "f S n")

	if boxes := m.PathBoxes(); len(boxes) != 0 {
		t.Errorf("PathBoxes() = %v, want none: no path was constructed", boxes)
	}
}
