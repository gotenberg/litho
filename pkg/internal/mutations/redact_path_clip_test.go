package mutations

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// clipFixture builds a one-page document whose content stream is body and
// whose resource dictionary is resources.
func clipFixture(t *testing.T, resources, body string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"5 0 obj\n" + resources + "\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	return doc, pages[0]
}

// paintedObject is one painted path as a minimal renderer sees it: flattened
// subpaths in page space and the even-odd clips in force.
type paintedObject struct {
	subpaths [][]bezPt
	closed   []bool
	clips    [][][]bezPt
	op       string
	half     float64
	// miterLimit is the miter limit of a miter-joined stroke, 0 for any
	// other join.
	miterLimit float64
}

// renderModel reads a content stream into the painted paths it draws. It
// models only what these tests write: q, Q, cm, w, path construction, W*, n
// and the paint operators.
func renderModel(t *testing.T, data []byte) []paintedObject {
	t.Helper()
	ops, err := content.ParseStreamData(context.Background(), data)
	if err != nil {
		t.Fatalf("parsing content: %v", err)
	}
	type gstate struct {
		ctm   [6]float64
		width float64
		join  int
		limit float64
		clips [][][]bezPt
	}
	stack := []gstate{{ctm: [6]float64{1, 0, 0, 1, 0, 0}, width: 1, limit: 10}}
	var subs [][]bezPt
	var closed []bool
	var pen, start bezPt
	clipPending := false
	var out []paintedObject

	num := func(op content.Operation, i int) float64 { return document.Float(op.Operands[i]) }
	for _, op := range ops {
		g := &stack[len(stack)-1]
		pt := func(x, y float64) bezPt { return apply(g.ctm, bezPt{x, y}) }
		switch op.Operator {
		case "q":
			cp := *g
			cp.clips = append([][][]bezPt(nil), g.clips...)
			stack = append(stack, cp)
		case "Q":
			stack = stack[:len(stack)-1]
		case "cm":
			m := [6]float64{num(op, 0), num(op, 1), num(op, 2), num(op, 3), num(op, 4), num(op, 5)}
			c := g.ctm
			g.ctm = [6]float64{
				m[0]*c[0] + m[1]*c[2], m[0]*c[1] + m[1]*c[3],
				m[2]*c[0] + m[3]*c[2], m[2]*c[1] + m[3]*c[3],
				m[4]*c[0] + m[5]*c[2] + c[4], m[4]*c[1] + m[5]*c[3] + c[5],
			}
		case "w":
			g.width = num(op, 0)
		case "j":
			g.join = int(num(op, 0))
		case "M":
			g.limit = num(op, 0)
		case "m":
			pen = bezPt{num(op, 0), num(op, 1)}
			start = pen
			subs = append(subs, []bezPt{pt(pen.x, pen.y)})
			closed = append(closed, false)
		case "l":
			pen = bezPt{num(op, 0), num(op, 1)}
			subs[len(subs)-1] = append(subs[len(subs)-1], pt(pen.x, pen.y))
		case "c":
			p0 := pen
			c1, c2 := bezPt{num(op, 0), num(op, 1)}, bezPt{num(op, 2), num(op, 3)}
			pen = bezPt{num(op, 4), num(op, 5)}
			seg := pathSeg{cubic: true, p: [4]bezPt{p0, c1, c2, pen}}
			// Fine enough that two flattenings of the same curve agree to
			// well within the tolerance assertClearedExactly allows.
			length := math.Hypot(c1.x-p0.x, c1.y-p0.y) + math.Hypot(c2.x-c1.x, c2.y-c1.y) + math.Hypot(pen.x-c2.x, pen.y-c2.y)
			steps := int(math.Min(math.Max(length/0.1, 8), 20000))
			for k := 1; k <= steps; k++ {
				q := evalSeg(seg, float64(k)/float64(steps))
				subs[len(subs)-1] = append(subs[len(subs)-1], pt(q.x, q.y))
			}
		case "re":
			x, y, w, h := num(op, 0), num(op, 1), num(op, 2), num(op, 3)
			subs = append(subs, []bezPt{pt(x, y), pt(x+w, y), pt(x+w, y+h), pt(x, y+h)})
			closed = append(closed, true)
			pen, start = bezPt{x, y}, bezPt{x, y}
		case "h":
			closed[len(closed)-1] = true
			pen = start
		case "W*":
			clipPending = true
		case "W":
			t.Fatalf("model handles only W*")
		case "n", "f", "f*", "S", "s", "B", "B*", "b", "b*":
			if op.Operator == "s" || op.Operator == "b" || op.Operator == "b*" {
				closed[len(closed)-1] = true
			}
			if op.Operator != "n" {
				obj := paintedObject{
					subpaths: subs, closed: closed, op: op.Operator,
					clips: append([][][]bezPt(nil), g.clips...),
					half:  g.width / 2 * maxStretch(g.ctm),
				}
				if g.join == 0 {
					obj.miterLimit = g.limit
				}
				out = append(out, obj)
			}
			if clipPending {
				g.clips = append(g.clips, subs)
				clipPending = false
			}
			subs, closed = nil, nil
		}
	}
	return out
}

// windingAt returns the winding number of the subpaths, each closed, around p.
func windingAt(subs [][]bezPt, p bezPt) int {
	w := 0
	for _, s := range subs {
		for i := range s {
			a, b := s[i], s[(i+1)%len(s)]
			if a.y <= p.y {
				if b.y > p.y && (b.x-a.x)*(p.y-a.y)-(p.x-a.x)*(b.y-a.y) > 0 {
					w++
				}
			} else if b.y <= p.y && (b.x-a.x)*(p.y-a.y)-(p.x-a.x)*(b.y-a.y) < 0 {
				w--
			}
		}
	}
	return w
}

// segDistance returns the distance from p to the segment a-b.
func segDistance(p, a, b bezPt) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 0 {
		t = math.Max(0, math.Min(1, ((p.x-a.x)*dx+(p.y-a.y)*dy)/l2))
	}
	return math.Hypot(p.x-(a.x+t*dx), p.y-(a.y+t*dy))
}

// paints reports whether any object paints p, with round caps and joins.
func paints(objs []paintedObject, p bezPt) bool {
	return paintsWidened(objs, p, 0)
}

// paintsWidened is [paints] with every stroke's half width widened by delta.
func paintsWidened(objs []paintedObject, p bezPt, delta float64) bool {
	for _, o := range objs {
		inClip := true
		for _, c := range o.clips {
			if windingAt(c, p)%2 == 0 {
				inClip = false
				break
			}
		}
		if !inClip {
			continue
		}
		evenOdd := o.op == "f*" || o.op == "B*" || o.op == "b*"
		if o.op != "S" && o.op != "s" {
			if w := windingAt(o.subpaths, p); (evenOdd && w%2 != 0) || (!evenOdd && w != 0) {
				return true
			}
		}
		if o.op == "f" || o.op == "F" || o.op == "f*" {
			continue
		}
		for si, s := range o.subpaths {
			n := len(s) - 1
			if o.closed[si] {
				n = len(s)
			}
			for i := 0; i < n; i++ {
				if segDistance(p, s[i], s[(i+1)%len(s)]) <= o.half+delta {
					return true
				}
			}
			if o.miterLimit > 0 && inMiter(s, o.closed[si], o.half+delta, o.miterLimit, p) {
				return true
			}
		}
	}
	return false
}

// inMiter reports whether p lies in the miter a join of s adds beyond the
// round join the distance test already covers (ISO 32000-1, 8.4.3.5).
func inMiter(s []bezPt, closed bool, half, limit float64, p bezPt) bool {
	n := len(s)
	for i := range n {
		if !closed && (i == 0 || i == n-1) {
			continue
		}
		a, v, b := s[(i+n-1)%n], s[i], s[(i+1)%n]
		u, okU := unit(a, v)
		w, okW := unit(v, b)
		if !okU || !okW {
			continue
		}
		sinHalf := math.Sqrt(math.Max((1+u.x*w.x+u.y*w.y)/2, 0))
		if sinHalf == 0 || 1/sinHalf > limit {
			continue
		}
		// The outer side is the one the path turns away from.
		side := 1.0
		if u.x*w.y-u.y*w.x > 0 {
			side = -1
		}
		n1 := bezPt{v.x - side*u.y*half, v.y + side*u.x*half}
		n2 := bezPt{v.x - side*w.y*half, v.y + side*w.x*half}
		bis, ok := unit(bezPt{}, bezPt{(n1.x+n2.x)/2 - v.x, (n1.y+n2.y)/2 - v.y})
		if !ok {
			continue
		}
		tip := bezPt{v.x + bis.x*half/sinHalf, v.y + bis.y*half/sinHalf}
		if windingAt([][]bezPt{{v, n1, tip, n2}}, p) != 0 {
			return true
		}
	}
	return false
}

// unit returns the unit vector from a to b.
func unit(a, b bezPt) (bezPt, bool) {
	l := math.Hypot(b.x-a.x, b.y-a.y)
	if l == 0 {
		return bezPt{}, false
	}
	return bezPt{(b.x - a.x) / l, (b.y - a.y) / l}, true
}

// assertClearedExactly compares what before and after paint over a grid: the
// same marks outside every region, none inside any.
func assertClearedExactly(t *testing.T, before, after []byte, regions [][4]float64) {
	t.Helper()
	orig, got := renderModel(t, before), renderModel(t, after)
	for x := 0.37; x < 612; x += 2.9 {
		for y := 0.41; y < 792; y += 2.9 {
			p := bezPt{x, y}
			inside := false
			for _, r := range regions {
				if x >= r[0] && x <= r[2] && y >= r[1] && y <= r[3] {
					inside = true
				}
			}
			// A point on the edge of a stroke reads differently under two
			// flattenings of one curve, so it decides nothing.
			want := paintsWidened(orig, p, -0.01)
			if want != paintsWidened(orig, p, 0.01) {
				continue
			}
			switch {
			case inside && paints(got, p):
				t.Fatalf("(%g, %g) is inside a region and still painted:\n%s", x, y, after)
			case !inside && paints(got, p) != want:
				t.Fatalf("(%g, %g) is outside every region and changed: before %v, after %v:\n%s",
					x, y, paints(orig, p), paints(got, p), after)
			}
		}
	}
}

// assertNoGeometryInside fails when a painted path's own geometry, not a clip,
// runs strictly inside any of the cuts.
func assertNoGeometryInside(t *testing.T, after []byte, cuts []cutRect) {
	t.Helper()
	for _, o := range renderModel(t, after) {
		for _, s := range o.subpaths {
			for _, p := range s {
				for _, c := range cuts {
					const eps = 1e-6
					if p.x > c.x0+eps && p.x < c.x1-eps && p.y > c.y0+eps && p.y < c.y1-eps {
						t.Fatalf("path point (%g, %g) survives inside %v:\n%s", p.x, p.y, c, after)
					}
				}
			}
		}
	}
}

// assertNoStrokeInside is [assertNoGeometryInside] for a stroke, whose
// segments must not cross the cuts either: a stroke is broken at a cut rather
// than bridged by a chord.
func assertNoStrokeInside(t *testing.T, after []byte, cuts []cutRect) {
	t.Helper()
	assertNoGeometryInside(t, after, cuts)
	for _, o := range renderModel(t, after) {
		for si, s := range o.subpaths {
			n := len(s) - 1
			if o.closed[si] {
				n = len(s)
			}
			for i := 0; i < n; i++ {
				a, b := s[i], s[(i+1)%len(s)]
				mid := bezPt{(a.x + b.x) / 2, (a.y + b.y) / 2}
				for _, c := range cuts {
					const eps = 1e-6
					if mid.x > c.x0+eps && mid.x < c.x1-eps && mid.y > c.y0+eps && mid.y < c.y1-eps {
						t.Fatalf("a stroked segment crosses %v at (%g, %g):\n%s", c, mid.x, mid.y, after)
					}
				}
			}
		}
	}
}

func redactAndRead(t *testing.T, resources, body string, regions [][4]float64) (RedactReport, []byte) {
	t.Helper()
	doc, page := clipFixture(t, resources, body)
	alloc := writer.NewObjAllocator(20)
	report, err := RedactPageRegions(context.Background(), alloc, doc, page, regions)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	return report, newPageContents(t, alloc, page.Ref.ObjectNum)
}

func TestRedactPageRegions_ClipsAFilledPathStraddlingTheRegion(t *testing.T) {
	body := "100 100 100 100 re f"
	regions := [][4]float64{{90, 90, 160, 160}}
	report, got := redactAndRead(t, "<< >>", body, regions)

	if report.ClippedPaths != 1 || report.RemovedPaths != 0 {
		t.Errorf("clipped %d, removed %d; want 1 and 0", report.ClippedPaths, report.RemovedPaths)
	}
	assertClearedExactly(t, []byte(body), got, regions)
	// The rectangle's corner at (100,100) sat inside the region: it is gone,
	// not merely hidden.
	assertNoGeometryInside(t, got, []cutRect{{90 + fillCutInset, 90 + fillCutInset, 160 - fillCutInset, 160 - fillCutInset}})
}

func TestRedactPageRegions_SplitsAFilledRuleIntoRectangles(t *testing.T) {
	// A hairline rule drawn as a thin filled rectangle, as producers draw
	// table borders. What survives stays two rectangles: a renderer snaps an
	// re rule to whole pixels and anti-aliases a polygon, so the rule outside
	// the region would come back fainter as anything else.
	body := "92.039 335 0.12 153 re f"
	regions := [][4]float64{{61.2, 356.4, 550.8, 435.6}}
	_, got := redactAndRead(t, "<< >>", body, regions)

	assertClearedExactly(t, []byte(body), got, regions)
	_, painted, ok := bytes.Cut(got, []byte("W* n"))
	if n := bytes.Count(painted, []byte(" re")); !ok || n != 2 || bytes.Contains(painted, []byte(" l ")) {
		t.Errorf("want the rule painted as two rectangles, got %d re:\n%s", n, got)
	}
	// One path holding two rectangles is no rectangle to poppler either, so
	// each piece takes its own paint operator.
	if n := bytes.Count(painted, []byte(" f")); n != 2 {
		t.Errorf("want each piece filled on its own, got %d f:\n%s", n, got)
	}
}

func TestRedactPageRegions_PaintsAThinRuleAlongTheRegionEdgeAsSeparateRectangles(t *testing.T) {
	// A table rule under one unit thick along the region's top edge, reaching past
	// it on both sides: it splits into a piece either side and a sliver above
	// the cut. Painted as one path, renderers stop snapping it to whole pixels
	// and draw the whole rule fainter outside the region (govdocs
	// thread_400_400756), so each piece takes its own paint operator.
	body := "78.6 593.52 384.84 -0.96 re f"
	regions := [][4]float64{{153, 198, 459, 594}}
	_, got := redactAndRead(t, "<< >>", body, regions)

	assertClearedExactly(t, []byte(body), got, regions)
	_, painted, ok := bytes.Cut(got, []byte("W* n"))
	if n := bytes.Count(painted, []byte(" re")); !ok || n != 3 {
		t.Fatalf("want the rule painted as three rectangles, got %d re:\n%s", n, got)
	}
	if n := bytes.Count(painted, []byte(" f")); n != 3 {
		t.Errorf("want each piece filled on its own, got %d f:\n%s", n, got)
	}
}

func TestRedactPageRegions_StrokesARectangleWithoutBridgingTheRegion(t *testing.T) {
	// The outline's corner sits in the region. The stroke is broken at the
	// region rather than bridged, so no segment of it crosses the region and
	// no diagonal makes a renderer treat the outline as anything but lines.
	body := "1 w 20 400 150 150 re S"
	regions := [][4]float64{{60, 360, 540, 440}}
	_, got := redactAndRead(t, "<< >>", body, regions)

	assertClearedExactly(t, []byte(body), got, regions)
	reach := 0.5*math.Sqrt2 + strokeReachSlack
	assertNoStrokeInside(t, got, []cutRect{{60 + reach, 360 + reach, 540 - reach, 440 - reach}})
}

func TestRedactPageRegions_KeepsARectangleWindingWithItsDirection(t *testing.T) {
	// The narrow rectangle runs against the outer one, so under the nonzero
	// rule it punches a hole in it. Its parts outside the region must keep
	// punching it.
	body := "100 100 300 300 re 150 150 m 150 350 l 350 350 l 350 150 l h 110 340 30 -150 re f"
	regions := [][4]float64{{105, 120, 330, 230}}
	_, got := redactAndRead(t, "<< >>", body, regions)

	assertClearedExactly(t, []byte(body), got, regions)
}

func TestRedactPageRegions_ClipsAStrokedLineCrossingTheRegion(t *testing.T) {
	body := "2 w 50 400 m 500 400 l S"
	regions := [][4]float64{{200, 300, 300, 500}}
	report, got := redactAndRead(t, "<< >>", body, regions)

	if report.ClippedPaths != 1 {
		t.Errorf("clipped %d, want 1", report.ClippedPaths)
	}
	assertClearedExactly(t, []byte(body), got, regions)
	reach := 1*math.Sqrt2 + strokeReachSlack
	assertNoStrokeInside(t, got, []cutRect{{200 + reach, 300 + reach, 300 - reach, 500 - reach}})
}

func TestRedactPageRegions_ClipsACurveLikeASignature(t *testing.T) {
	// A pen stroke looping through the region, as a vector signature draws.
	body := "1.5 w 1 J 1 j 120 380 m 180 520 260 260 330 420 c 380 530 430 300 520 410 c S"
	regions := [][4]float64{{220, 330, 360, 470}}
	_, got := redactAndRead(t, "<< >>", body, regions)

	assertClearedExactly(t, []byte(body), got, regions)
	reach := 0.75*math.Sqrt2 + strokeReachSlack
	assertNoStrokeInside(t, got, []cutRect{{220 + reach, 330 + reach, 360 - reach, 470 - reach}})
}

func TestRedactPageRegions_ClipsAClosedStrokeWhoseStartIsInside(t *testing.T) {
	// The subpath starts at (100,100), inside the region, so the rewritten loop
	// has to start elsewhere and still close.
	body := "1 w 100 100 m 300 100 l 300 300 l 100 300 l h S"
	regions := [][4]float64{{50, 50, 180, 180}}
	_, got := redactAndRead(t, "<< >>", body, regions)

	assertClearedExactly(t, []byte(body), got, regions)
	if bytes.Contains(got, []byte("100 100 m")) {
		t.Errorf("the start point inside the region survived:\n%s", got)
	}
}

func TestRedactPageRegions_ClipsUnderARotatedCTM(t *testing.T) {
	body := "q 0.8 0.6 -0.6 0.8 300 300 cm 0 0 200 30 re f Q"
	regions := [][4]float64{{340, 300, 420, 420}}
	_, got := redactAndRead(t, "<< >>", body, regions)

	assertClearedExactly(t, []byte(body), got, regions)
}

func TestRedactPageRegions_KeepsTheAreaARingOfRegionsEncloses(t *testing.T) {
	// The square's whole outline lies inside four regions framing its middle.
	// Dropping it would clear the middle too, which no region covers.
	body := "100 100 m 300 100 l 300 300 l 100 300 l f"
	regions := [][4]float64{{90, 90, 310, 110}, {90, 290, 310, 310}, {90, 90, 110, 310}, {290, 90, 310, 310}}
	_, got := redactAndRead(t, "<< >>", body, regions)

	assertClearedExactly(t, []byte(body), got, regions)
	if !paints(renderModel(t, got), bezPt{200, 200}) {
		t.Errorf("the middle no region covers is no longer filled:\n%s", got)
	}
}

func TestRedactPageRegions_ClearsStrokeInkFromGeometryOutsideTheRegion(t *testing.T) {
	// The line runs 6 units below the region, but its 20-unit width paints 4
	// units into it. Selecting by geometry alone left that ink in place.
	body := "20 w 50 400 m 500 400 l S"
	regions := [][4]float64{{200, 406, 300, 500}}
	_, got := redactAndRead(t, "<< >>", body, regions)

	assertClearedExactly(t, []byte(body), got, regions)
}

func TestRedactPageRegions_RemovesAStrokeWhoseInkIsInside(t *testing.T) {
	body := "1 w 120 120 m 140 140 l S"
	report, got := redactAndRead(t, "<< >>", body, [][4]float64{{100, 100, 160, 160}})

	if report.RemovedPaths != 1 || report.ClippedPaths != 0 {
		t.Errorf("removed %d, clipped %d; want 1 and 0", report.RemovedPaths, report.ClippedPaths)
	}
	if bytes.Contains(got, []byte(" S")) {
		t.Errorf("the stroke survived:\n%s", got)
	}
}

func TestRedactPageRegions_ReadsTheLineWidthAGraphicsStateSets(t *testing.T) {
	// /LW 20 makes the line reach into the region, as 20 w does.
	body := "/GS1 gs 50 400 m 500 400 l S"
	regions := [][4]float64{{200, 406, 300, 500}}
	_, got := redactAndRead(t, "<< /ExtGState << /GS1 << /LW 20 >> >> >>", body, regions)

	// The render model reads no gs, so it is told the width the same way.
	modelled := bytes.ReplaceAll(got, []byte("/GS1 gs"), []byte("20 w"))
	assertClearedExactly(t, []byte("20 w 50 400 m 500 400 l S"), modelled, regions)
}

func TestRedactPageRegions_RefusesAStrokeWhoseGraphicsStateIsMissing(t *testing.T) {
	doc, page := clipFixture(t, "<< >>", "/GS9 gs 50 400 m 500 400 l S")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page, [][4]float64{{200, 300, 300, 500}})
	if !errors.Is(err, ErrRegionCoversAStrokedPath) {
		t.Errorf("err = %v, want ErrRegionCoversAStrokedPath", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

func TestRedactPageRegions_RefusesADashedStrokeThatWouldBeCut(t *testing.T) {
	// The chord would be shorter than the run, moving every dash after it.
	doc, page := clipFixture(t, "<< >>", "1 w [3 2] 0 d 50 400 m 250 450 l 500 400 l S")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page, [][4]float64{{200, 300, 300, 500}})
	if !errors.Is(err, ErrRegionCoversAStrokedPath) {
		t.Errorf("err = %v, want ErrRegionCoversAStrokedPath", err)
	}
}

func TestRedactPageRegions_RefusesAFilledAndStrokedOpenSubpathStartingInside(t *testing.T) {
	// The fill closes the subpath back to its start and the stroke does not,
	// so moving the start would move the fill's closing edge.
	doc, page := clipFixture(t, "<< >>", "1 w 150 150 m 400 150 l 400 400 l B")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page, [][4]float64{{100, 100, 200, 200}})
	if !errors.Is(err, ErrPathStraddlesRegion) {
		t.Errorf("err = %v, want ErrPathStraddlesRegion", err)
	}
}

func TestRedactPageRegions_ClipsAFilledAndStrokedClosedPath(t *testing.T) {
	body := "3 w 100 100 m 400 100 l 400 400 l 100 400 l b"
	regions := [][4]float64{{50, 200, 250, 300}}
	_, got := redactAndRead(t, "<< >>", body, regions)

	assertClearedExactly(t, []byte(body), got, regions)
}

func TestRedactPageRegions_KeepsSharpJoinsOutsideTheRegion(t *testing.T) {
	// A narrow spike: its miter reaches far past the vertex. The chord the cut
	// adds makes a join of its own, which must not reach out of the region.
	body := "4 w 100 400 m 300 410 l 100 420 l S"
	regions := [][4]float64{{200, 380, 330, 450}}
	_, got := redactAndRead(t, "<< >>", body, regions)

	assertClearedExactly(t, []byte(body), got, regions)
}

func TestRedactPageRegions_KeepsAMiterTipOutsideTheRegion(t *testing.T) {
	// The vertex at (250,430) is 5 units inside the region, and its miter tip
	// reaches about 10 units past it, out of the region. Removing the vertex
	// would take that tip, which the page shows outside.
	body := "4 w 100 400 m 250 430 l 100 460 l S"
	regions := [][4]float64{{200, 380, 255, 480}}
	_, got := redactAndRead(t, "<< >>", body, regions)

	assertClearedExactly(t, []byte(body), got, regions)
}

func TestRedactPageRegions_KeepsAStateOperatorARemovedPathHeld(t *testing.T) {
	// The rg inside the path sets the colour the later rectangle fills with,
	// so removing the path must not take it.
	body := "110 110 m 1 0 0 rg 120 110 l 120 120 l f 300 300 50 50 re f"
	report, got := redactAndRead(t, "<< >>", body, [][4]float64{{100, 100, 130, 130}})

	if report.RemovedPaths != 1 {
		t.Errorf("removed %d paths, want 1", report.RemovedPaths)
	}
	if bytes.Contains(got, []byte("110 110 m")) || !bytes.Contains(got, []byte("1 0 0 rg")) {
		t.Errorf("want the path gone and its colour kept:\n%s", got)
	}
}

func TestRedactPageRegions_RemovesAPathAroundATextObject(t *testing.T) {
	// A producer drew a text object between a path's construction and its
	// paint. The path and the text are removed by their own edits, which
	// removing the run whole would have made overlap.
	doc, page := redactPathFixture(t, "1 w 100 100 20 20 re BT /F1 12 Tf 100 105 Td (SECRET) Tj ET 1 0 0 RG S")
	alloc := writer.NewObjAllocator(20)

	report, err := RedactPageRegions(context.Background(), alloc, doc, page, [][4]float64{{50, 50, 300, 300}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	got := newPageContents(t, alloc, page.Ref.ObjectNum)
	if report.RemovedPaths != 1 || bytes.Contains(got, []byte("re")) || bytes.Contains(got, []byte("SECRET")) {
		t.Errorf("removed %d paths; want the path and the text gone:\n%s", report.RemovedPaths, got)
	}
	if !bytes.Contains(got, []byte("1 0 0 RG")) {
		t.Errorf("the colour the path run set was removed with it:\n%s", got)
	}
}

func TestRedactPageRegions_HoistsAStateOperatorAClippedPathHeld(t *testing.T) {
	// Inside the q the clip adds, the rg would be undone by its Q, and the
	// content after the path would lose the colour.
	body := "100 100 m 0 0 1 rg 200 100 l 200 200 l 100 200 l f 300 300 50 50 re f"
	regions := [][4]float64{{90, 90, 160, 160}}
	_, got := redactAndRead(t, "<< >>", body, regions)

	assertClearedExactly(t, []byte(body), got, regions)
	rg, q := bytes.Index(got, []byte("0 0 1 rg")), bytes.Index(got, []byte(" q "))
	if rg < 0 || q < 0 || rg > q {
		t.Errorf("the colour has to be set before the clip's q:\n%s", got)
	}
}

func TestRedactPageRegions_RefusesAPathInsideATextObject(t *testing.T) {
	doc, page := clipFixture(t, "<< >>", "BT 100 100 100 100 re f ET")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page, [][4]float64{{90, 90, 160, 160}})
	if !errors.Is(err, ErrPathStraddlesRegion) {
		t.Errorf("err = %v, want ErrPathStraddlesRegion", err)
	}
}

func TestJoinFactor_ReadsTheSharpestMiter(t *testing.T) {
	right := []subpath{{start: bezPt{0, 0}, segs: []pathSeg{
		{p: [4]bezPt{{0, 0}, {}, {}, {10, 0}}},
		{p: [4]bezPt{{10, 0}, {}, {}, {10, 10}}},
	}}}
	if got := joinFactor(right, false, strokeParams{lineWidth: 1, miterLimit: 10, known: true}); math.Abs(got-math.Sqrt2) > 1e-9 {
		t.Errorf("right angle: factor %g, want √2", got)
	}
	spike := []subpath{{start: bezPt{0, 0}, segs: []pathSeg{
		{p: [4]bezPt{{0, 0}, {}, {}, {100, 5}}},
		{p: [4]bezPt{{100, 5}, {}, {}, {0, 10}}},
	}}}
	if got := joinFactor(spike, false, strokeParams{lineWidth: 1, miterLimit: 10, known: true}); got != math.Sqrt2 {
		t.Errorf("a spike past the miter limit is bevelled: factor %g, want √2", got)
	}
	if got := joinFactor(spike, false, strokeParams{lineWidth: 1, miterLimit: 100, known: true}); got < 10 {
		t.Errorf("a spike within the miter limit is mitred: factor %g, want over 10", got)
	}
}
