package annot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// redForm is an appearance content stream that paints its whole box.
const redForm = "1 0 0 rg 0 0 1000 1000 re f"

// streamObj writes a stream object with extra dictionary entries.
func streamObj(dict, data string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

// buildPDF serialises objs as objects 1 to n with a classic xref table.
func buildPDF(objs []string) []byte {
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

// onePage builds a one-page document whose page carries pageExtra and the
// /Annots array annots, followed by objects 5 onwards.
func onePage(pageExtra, annots string, objs ...string) []byte {
	all := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Resources << >> /Contents 4 0 R /Annots %s %s >>", annots, pageExtra),
		streamObj("", ""),
	}
	return buildPDF(append(all, objs...))
}

// firstPage opens data and returns its first page.
func firstPage(t testing.TB, data []byte) (context.Context, *document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	cat, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, cat)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	return ctx, doc, pages[0]
}

// collect runs Collect on the first page of data.
func collect(t *testing.T, data []byte, opts Options) ([]Appearance, []Skip) {
	t.Helper()
	ctx, doc, page := firstPage(t, data)
	apps, skips, err := Collect(ctx, doc, page, opts)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return apps, skips
}

// annot writes an annotation dictionary.
func annot(subtype string, flags int, ap, extra string) string {
	return fmt.Sprintf("<< /Type /Annot /Subtype /%s /Rect [20 20 80 80] /F %d /AP %s %s >>", subtype, flags, ap, extra)
}

// form writes an appearance stream with the given dictionary entries.
func form(dict string) string {
	return streamObj("/Type /XObject /Subtype /Form "+dict, redForm)
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func matrixEqual(a, b geom.Matrix) bool {
	for i := range a {
		if !approx(a[i], b[i]) {
			return false
		}
	}
	return true
}

// TestAlgorithm81 checks the matrix A of ISO 32000-1, 12.5.5 against values
// worked by hand, and that A and the form matrix together put the box's
// corners on the rectangle's.
func TestAlgorithm81(t *testing.T) {
	tests := []struct {
		name   string
		bbox   geom.Rect
		matrix geom.Matrix
		rect   geom.Rect
		want   geom.Matrix
	}{
		{
			name:   "scale and translate",
			bbox:   geom.Rect{Max: geom.Point{X: 10, Y: 20}},
			matrix: geom.Identity,
			rect:   geom.Rect{Min: geom.Point{X: 20, Y: 20}, Max: geom.Point{X: 80, Y: 60}},
			want:   geom.Matrix{6, 0, 0, 2, 20, 20},
		},
		{
			name:   "quarter turn form matrix",
			bbox:   geom.Rect{Max: geom.Point{X: 10, Y: 20}},
			matrix: geom.Matrix{0, 1, -1, 0, 0, 0},
			rect:   geom.Rect{Min: geom.Point{X: 20, Y: 20}, Max: geom.Point{X: 80, Y: 50}},
			// The turned box spans x -20..0, y 0..10.
			want: geom.Matrix{3, 0, 0, 3, 80, 20},
		},
		{
			name:   "offset box",
			bbox:   geom.Rect{Min: geom.Point{X: 5, Y: 5}, Max: geom.Point{X: 15, Y: 15}},
			matrix: geom.Identity,
			rect:   geom.Rect{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 10, Y: 10}},
			want:   geom.Matrix{1, 0, 0, 1, -5, -5},
		},
		{
			name:   "degenerate box translates",
			bbox:   geom.Rect{},
			matrix: geom.Identity,
			rect:   geom.Rect{Min: geom.Point{X: 20, Y: 30}, Max: geom.Point{X: 80, Y: 80}},
			want:   geom.Matrix{1, 0, 0, 1, 20, 30},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := algorithm81(tt.bbox, tt.matrix, tt.rect)
			if !matrixEqual(got, tt.want) {
				t.Fatalf("A = %v, want %v", got, tt.want)
			}
			if tt.bbox.Max == tt.bbox.Min {
				return
			}
			total := tt.matrix.Mul(got)
			for _, c := range []geom.Point{tt.bbox.Min, tt.bbox.Max, {X: tt.bbox.Min.X, Y: tt.bbox.Max.Y}, {X: tt.bbox.Max.X, Y: tt.bbox.Min.Y}} {
				p := total.Apply(c)
				onX := approx(p.X, tt.rect.Min.X) || approx(p.X, tt.rect.Max.X)
				onY := approx(p.Y, tt.rect.Min.Y) || approx(p.Y, tt.rect.Max.Y)
				if !onX || !onY {
					t.Errorf("corner %v maps to %v, off the rectangle's corners %v", c, p, tt.rect)
				}
			}
		})
	}
}

// TestCollectAppearance checks the fields of one drawn appearance.
func TestCollectAppearance(t *testing.T) {
	data := onePage("/Rotate 90", "[5 0 R]",
		"<< /Type /Annot /Subtype /Square /Rect [80 60 20 20] /F 20 /OC 7 0 R /AP << /N 6 0 R >> >>",
		form("/BBox [0 0 10 20] /Matrix [0 1 -1 0 0 0] /Resources << /ProcSet [/PDF] >>"),
		"<< /Type /OCG /Name (Layer) >>",
	)
	apps, skips := collect(t, data, Options{})
	if len(apps) != 1 || len(skips) != 0 {
		t.Fatalf("got %d appearances, %d skips, want 1 and 0", len(apps), len(skips))
	}
	a := apps[0]
	if a.Index != 0 || a.Ref.ObjectNum != 5 || a.StreamRef.ObjectNum != 6 || a.Subtype != "Square" || a.State != "" {
		t.Errorf("identity fields: %+v", a)
	}
	wantRect := geom.Rect{Min: geom.Point{X: 20, Y: 20}, Max: geom.Point{X: 80, Y: 60}}
	if a.Rect != wantRect {
		t.Errorf("Rect = %v, want the normalised %v", a.Rect, wantRect)
	}
	if !matrixEqual(a.FormMatrix, geom.Matrix{0, 1, -1, 0, 0, 0}) {
		t.Errorf("FormMatrix = %v", a.FormMatrix)
	}
	// The page's /Rotate does not enter Algorithm 8.1: the caller applies it.
	if want := (geom.Matrix{3, 0, 0, 4, 80, 20}); !matrixEqual(a.A, want) {
		t.Errorf("A = %v, want %v", a.A, want)
	}
	if !a.NoRotate || a.NoZoom || a.Flags != 20 {
		t.Errorf("flags: NoRotate %v NoZoom %v Flags %d, want true false 20", a.NoRotate, a.NoZoom, a.Flags)
	}
	if ref, ok := a.OC.(ast.PdfIndirectRef); !ok || ref.ObjectNum != 7 {
		t.Errorf("OC = %v, want 7 0 R", a.OC)
	}
	if a.Resources == nil || a.PageResources {
		t.Errorf("Resources %v PageResources %v, want the stream's own", a.Resources, a.PageResources)
	}
}

// TestCollectOrderAndResources checks /Annots order, a direct annotation, and
// the page-resource fallback of ISO 32000-1, 7.8.3.
func TestCollectOrderAndResources(t *testing.T) {
	direct := annot("Square", 4, "<< /N 7 0 R >>", "")
	data := onePage("", "[5 0 R "+direct+" 6 0 R]",
		annot("Circle", 4, "<< /N 7 0 R >>", ""),
		annot("Ink", 4, "<< /N 8 0 R >>", ""),
		form("/BBox [0 0 10 10]"),
		form("/BBox [0 0 10 10] /Resources null /OC 9 0 R"),
		"<< /Type /OCG /Name (Layer) >>",
	)
	apps, _ := collect(t, data, Options{})
	var got []string
	for _, a := range apps {
		got = append(got, fmt.Sprintf("%d:%s", a.Index, string(a.Subtype)))
	}
	if want := "0:Circle 1:Square 2:Ink"; strings.Join(got, " ") != want {
		t.Fatalf("order = %q, want %q", strings.Join(got, " "), want)
	}
	if apps[1].Ref != (ast.PdfIndirectRef{}) {
		t.Errorf("a direct annotation has Ref %v", apps[1].Ref)
	}
	for _, a := range apps {
		if !a.PageResources || a.Resources != nil {
			t.Errorf("%s: PageResources %v Resources %v, want the page's", a.Subtype, a.PageResources, a.Resources)
		}
	}
	if apps[2].FormOC == nil || apps[0].FormOC != nil {
		t.Errorf("FormOC: %v %v", apps[0].FormOC, apps[2].FormOC)
	}
}

// TestCollectVisibility checks the flags of ISO 32000-1, 12.5.3, Table 165 and
// a pop-up's /Open in both modes.
func TestCollectVisibility(t *testing.T) {
	const ap = "<< /N 6 0 R >>"
	tests := []struct {
		name    string
		annot   string
		view    Reason
		print   Reason
		visible bool
	}{
		{name: "print flag", annot: annot("Square", FlagPrint, ap, "")},
		{name: "no flags", annot: annot("Square", 0, ap, ""), print: ReasonNotPrinted},
		{name: "hidden", annot: annot("Square", FlagHidden|FlagPrint, ap, ""), view: ReasonHidden, print: ReasonHidden},
		{name: "noview", annot: annot("Square", FlagNoView|FlagPrint, ap, ""), view: ReasonNoView},
		{name: "invisible standard", annot: annot("Square", FlagInvisible|FlagPrint, ap, "")},
		{name: "invisible non-standard", annot: annot("Foo", FlagInvisible|FlagPrint, ap, ""), view: ReasonInvisible, print: ReasonInvisible},
		{name: "non-standard", annot: annot("Foo", FlagPrint, ap, "")},
		{name: "closed popup", annot: annot("Popup", FlagPrint, ap, ""), view: ReasonClosedPopup, print: ReasonClosedPopup},
		{name: "open popup", annot: annot("Popup", FlagPrint, ap, "/Open true")},
		{name: "real flags ignored", annot: "<< /Subtype /Square /Rect [0 0 1 1] /F 2.0 /AP << /N 6 0 R >> >>", print: ReasonNotPrinted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := onePage("", "[5 0 R]", tt.annot, form("/BBox [0 0 10 10]"))
			for _, m := range []struct {
				mode Mode
				want Reason
			}{{View, tt.view}, {Print, tt.print}} {
				apps, skips := collect(t, data, Options{Mode: m.mode})
				var got Reason
				if len(skips) == 1 {
					got = skips[0].Reason
				}
				if got != m.want || len(apps)+len(skips) != 1 {
					t.Errorf("mode %d: %d appearances, skip %v, want skip %v", m.mode, len(apps), got, m.want)
				}
			}
		})
	}
}

// TestCollectStates checks the choice of the normal appearance by /AS (ISO
// 32000-1, 12.5.5) and the defaults poppler applies without one.
func TestCollectStates(t *testing.T) {
	tests := []struct {
		name      string
		n         string
		as        string
		wantState ast.PdfName
		wantRef   int
		wantSkip  Reason
	}{
		{name: "as picks", n: "<< /Yes 6 0 R /Off 7 0 R >>", as: "/AS /Yes", wantState: "Yes", wantRef: 6},
		{name: "as off", n: "<< /Yes 6 0 R /Off 7 0 R >>", as: "/AS /Off", wantState: "Off", wantRef: 7},
		{name: "no as two states", n: "<< /Yes 6 0 R /Off 7 0 R >>", wantState: "Off", wantRef: 7},
		{name: "no as one state", n: "<< /Yes 6 0 R >>", wantState: "Yes", wantRef: 6},
		{name: "as names nothing", n: "<< /Yes 6 0 R >>", as: "/AS /Off", wantSkip: ReasonNoAppearance},
		{name: "indirect states", n: "8 0 R", as: "/AS /Yes", wantState: "Yes", wantRef: 6},
		{name: "state not a stream", n: "<< /Yes 8 0 R >>", as: "/AS /Yes", wantSkip: ReasonNoAppearance},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := onePage("", "[5 0 R]",
				annot("Widget", FlagPrint, "<< /N "+tt.n+" >>", tt.as),
				form("/BBox [0 0 10 10]"),
				form("/BBox [0 0 10 10]"),
				"<< /Yes 6 0 R >>",
			)
			apps, skips := collect(t, data, Options{})
			if tt.wantSkip != 0 {
				if len(skips) != 1 || skips[0].Reason != tt.wantSkip {
					t.Fatalf("skips = %v, want %v", skips, tt.wantSkip)
				}
				return
			}
			if len(apps) != 1 {
				t.Fatalf("got %d appearances, skips %v", len(apps), skips)
			}
			if apps[0].State != tt.wantState || apps[0].StreamRef.ObjectNum != tt.wantRef {
				t.Errorf("state %q stream %d, want %q %d", apps[0].State, apps[0].StreamRef.ObjectNum, tt.wantState, tt.wantRef)
			}
		})
	}
}

// TestCollectSkips checks what is left out for want of an appearance, a
// /Rect or a /BBox.
func TestCollectSkips(t *testing.T) {
	tests := []struct {
		name  string
		annot string
		form  string
		want  Reason
	}{
		{name: "not a dictionary", annot: "42", form: form("/BBox [0 0 1 1]"), want: ReasonMalformed},
		{name: "no rect", annot: "<< /Subtype /Square /F 4 /AP << /N 6 0 R >> >>", form: form("/BBox [0 0 1 1]"), want: ReasonMalformed},
		{name: "short rect", annot: "<< /Subtype /Square /Rect [0 0 1] /F 4 /AP << /N 6 0 R >> >>", form: form("/BBox [0 0 1 1]"), want: ReasonMalformed},
		{name: "no ap", annot: "<< /Subtype /Square /Rect [0 0 1 1] /F 4 >>", form: form("/BBox [0 0 1 1]"), want: ReasonNoAppearance},
		{name: "no n", annot: annot("Square", 4, "<< /D 6 0 R >>", ""), form: form("/BBox [0 0 1 1]"), want: ReasonNoAppearance},
		{name: "n not a stream", annot: annot("Square", 4, "<< /N 42 >>", ""), form: form("/BBox [0 0 1 1]"), want: ReasonNoAppearance},
		{name: "no bbox", annot: annot("Square", 4, "<< /N 6 0 R >>", ""), form: form(""), want: ReasonBadForm},
		{name: "bbox not numbers", annot: annot("Square", 4, "<< /N 6 0 R >>", ""), form: form("/BBox [0 0 (a) 1]"), want: ReasonBadForm},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apps, skips := collect(t, onePage("", "[5 0 R]", tt.annot, tt.form), Options{})
			if len(apps) != 0 || len(skips) != 1 || skips[0].Reason != tt.want || skips[0].Index != 0 {
				t.Fatalf("apps %d skips %v, want one %v", len(apps), skips, tt.want)
			}
		})
	}
}

// TestCollectLenientForm checks the forms a viewer still draws: no /Subtype,
// a /Subtype other than /Form, and a /Matrix that is not six numbers.
func TestCollectLenientForm(t *testing.T) {
	forms := map[string]string{
		"no subtype":   streamObj("/BBox [0 0 10 10]", redForm),
		"image":        streamObj("/Subtype /Image /BBox [0 0 10 10]", redForm),
		"short matrix": form("/BBox [0 0 10 10] /Matrix [1 0 0 1]"),
	}
	for name, f := range forms {
		t.Run(name, func(t *testing.T) {
			apps, skips := collect(t, onePage("", "[5 0 R]", annot("Square", 4, "<< /N 6 0 R >>", ""), f), Options{})
			if len(apps) != 1 || len(skips) != 0 {
				t.Fatalf("apps %d skips %v, want one appearance", len(apps), skips)
			}
			if apps[0].FormMatrix != geom.Identity {
				t.Errorf("FormMatrix = %v, want the identity", apps[0].FormMatrix)
			}
		})
	}
}

// TestCollectNoAnnots checks pages with no, null or unreadable /Annots.
func TestCollectNoAnnots(t *testing.T) {
	for _, annots := range []string{"null", "42", "[]", "5 0 R"} {
		apps, skips := collect(t, onePage("", annots, "(not an array)"), Options{})
		if len(apps)+len(skips) != 0 {
			t.Errorf("/Annots %s: %d appearances %d skips, want none", annots, len(apps), len(skips))
		}
	}
	apps, skips, err := Collect(context.Background(), nil, nil, Options{})
	if apps != nil || skips != nil || err != nil {
		t.Errorf("nil page: %v %v %v", apps, skips, err)
	}
}

// TestCollectCancelled checks that a done context is reported.
func TestCollectCancelled(t *testing.T) {
	_, doc, page := firstPage(t, onePage("", "[5 0 R]", annot("Square", 4, "<< /N 6 0 R >>", ""), form("/BBox [0 0 1 1]")))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Collect(ctx, doc, page, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestCollectLithoCorpus runs Collect over litho's annotation and form
// fixtures, every page, both modes, and checks each appearance lands on its
// rectangle.
func TestCollectLithoCorpus(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "test", "integration", "corpus", "litho", "pdfs")
	names := []string{
		"annots_1b.pdf", "annothidden_2b.pdf", "annotkinds_ua2.pdf", "annotshare_ua1.pdf",
		"formflat_ua1.pdf", "formhidden_ua1.pdf", "formreuse_ua1.pdf", "formshare_ua1.pdf",
	}
	// annothidden_2b sets each visibility flag on purpose: its skips are
	// pinned per page, in View mode.
	wantSkips := map[string]string{
		"annothidden_2b.pdf 1": "0:hidden 1:closed-popup 3:noview 6:hidden",
		"annothidden_2b.pdf 2": "0:hidden 1:hidden 3:noview 4:hidden 5:hidden",
	}
	total := 0
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		ctx := context.Background()
		doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		cat, err := document.ResolveCatalog(ctx, doc)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		pages, err := document.ResolvePages(ctx, doc, cat)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for pi, page := range pages {
			for _, mode := range []Mode{View, Print} {
				apps, skips, err := Collect(ctx, doc, page, Options{Mode: mode})
				if err != nil {
					t.Fatalf("%s page %d: %v", name, pi+1, err)
				}
				last := -1
				for _, a := range apps {
					if a.Index <= last {
						t.Errorf("%s: appearance %d out of /Annots order", name, a.Index)
					}
					last = a.Index
					checkLands(t, name, a)
				}
				if mode != View {
					continue
				}
				total += len(apps)
				if want, ok := wantSkips[fmt.Sprintf("%s %d", name, pi+1)]; ok {
					var got []string
					for _, s := range skips {
						got = append(got, fmt.Sprintf("%d:%s", s.Index, s.Reason))
					}
					if strings.Join(got, " ") != want {
						t.Errorf("%s page %d: skips %q, want %q", name, pi+1, strings.Join(got, " "), want)
					}
				}
			}
		}
	}
	if total == 0 {
		t.Fatal("no fixture page yielded an appearance")
	}
}

// checkLands checks that the transformed /BBox covers exactly the /Rect.
func checkLands(t *testing.T, name string, a Appearance) {
	t.Helper()
	m := a.Transform()
	lo, hi := m.Apply(a.BBox.Min), m.Apply(a.BBox.Max)
	if a.BBox.Min.X == a.BBox.Max.X || a.BBox.Min.Y == a.BBox.Max.Y || a.FormMatrix != geom.Identity {
		return
	}
	const tol = 1e-6
	if math.Abs(min(lo.X, hi.X)-a.Rect.Min.X) > tol || math.Abs(max(lo.X, hi.X)-a.Rect.Max.X) > tol ||
		math.Abs(min(lo.Y, hi.Y)-a.Rect.Min.Y) > tol || math.Abs(max(lo.Y, hi.Y)-a.Rect.Max.Y) > tol {
		t.Errorf("%s annotation %d: box lands on %v %v, not on /Rect %v", name, a.Index, lo, hi, a.Rect)
	}
}
