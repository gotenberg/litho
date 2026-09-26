package tagging

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// formDAGPDF builds a page whose forms nest as a directed acyclic graph: each of
// the depth levels invokes every form on the level below it, and no form shows
// text. Following it as a tree costs branching^depth decodes; following it with
// a memo costs one per form.
//
// No cycle is involved, so the path-scoped visited set cannot help: each visit
// arrives by a distinct path, which is exactly the case that made the scan
// exponential.
func formDAGPDF(levels, branching int) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"", // page, filled once the top level's object numbers are known
		"<< /Length 8 >>\nstream\n/L0 Do\nendstream",
	}

	// Object numbers are 1-based and the four entries above occupy 1..4.
	const firstForm = 5
	formNum := func(level, index int) int { return firstForm + level*branching + index }

	for level := range levels {
		for range branching {
			var resources, body string
			if level+1 < levels {
				var refs bytes.Buffer
				for child := range branching {
					fmt.Fprintf(&refs, "/L%dx%d %d 0 R ", level+1, child, formNum(level+1, child))
				}
				resources = fmt.Sprintf("/Resources << /XObject << %s>> >> ", refs.String())
				var paint bytes.Buffer
				for child := range branching {
					fmt.Fprintf(&paint, "/L%dx%d Do ", level+1, child)
				}
				body = paint.String()
			} else {
				body = "0 0 1 1 re f"
			}
			objs = append(objs, fmt.Sprintf(
				"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] %s/Length %d >>\nstream\n%sendstream",
				resources, len(body), body))
		}
	}

	var top bytes.Buffer
	for index := range branching {
		fmt.Fprintf(&top, "/L0x%d %d 0 R ", index, formNum(0, index))
	}
	objs[2] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R "+
		"/Resources << /XObject << %s>> >> >>", top.String())

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
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

// TestTextBearingXObjects_SharedFormsDecodedOnce pins that a form reached by
// several paths is decided once.
//
// The scan tracked the forms on the current path to break cycles and removed
// each on the way out, which is right for a cycle and wrong for a DAG: every
// distinct path re-walked and re-inflated the same subtree.
//
// The fixture is 4 forms per level over 10 levels: 4^10 visits followed as a
// tree against 40 with a memo. The assertion counts memoised decisions rather
// than measuring elapsed time, because these streams are tiny and uncompressed,
// so a million trivial recursions still finish in under a second here while a
// real file re-inflates its Flate streams at every one of them. Wall clock
// would have made this test pass with the memo removed.
//
// # Corpus evidence
//
//   - pdfjs issue6961.pdf: nests ten levels wide enough that `litho remediate
//     -profile PDF/UA-1` ran over 50 minutes at 142% CPU without finishing,
//     and it never completed in any corpus run.
//   - pdfjs issue6961.pdf: re-inflates a 29 KB Flate stream at every one of
//     the tree walk's visits.
func TestTextBearingXObjects_SharedFormsDecodedOnce(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	data := formDAGPDF(10, 4)
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

	// Prove the graph is real before timing it. A fixture whose forms do not
	// parse as streams answers instantly and would pass this test while
	// exercising nothing: the first version of it closed one brace too few, so
	// /Length landed inside /Resources and every form resolved to a plain dict.
	top, err := document.ResolveDict(ctx, doc, pages[0].Resources["XObject"])
	if err != nil || len(top) != 4 {
		t.Fatalf("fixture is wrong: page names %d form resources (err %v), want 4", len(top), err)
	}
	for name, entry := range top {
		stream, serr := document.ResolveStream(ctx, doc, entry)
		if serr != nil || stream.Dict == nil {
			t.Fatalf("fixture is wrong: %s does not resolve to a stream: %v", name, serr)
		}
		nested, _ := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
		inner, _ := document.ResolveDict(ctx, doc, nested["XObject"])
		if len(inner) != 4 {
			t.Fatalf("fixture is wrong: %s invokes %d nested forms, want 4", name, len(inner))
		}
	}

	// Scan the page's forms the way textBearingXObjects does and count the
	// forms it read.
	g := scanForms(ctx, nil, doc, top)
	const forms = 10 * 4
	if len(g.nodes) != forms {
		t.Errorf("the scan read %d forms for %d distinct forms: a shared subtree is re-read once per path, "+
			"which costs 4^10 reads instead of %d", len(g.nodes), forms, forms)
	}
	for name, node := range g.entries {
		if g.paintsText(node) {
			t.Fatalf("no form in this graph shows text, but /%s was judged to paint some", name)
		}
	}

	if got := textBearingXObjects(ctx, nil, doc, pages[0]); len(got) != 0 {
		t.Errorf("no form in the graph shows text, got %v", got)
	}
}

// shortestChainPDF builds a page listing two forms that reach the same text at
// different depths. /X0 lists /Deep and /Inner; /Deep lists /Mid, /Mid lists
// /Inner, and /Inner shows text. /P lists only /Deep. /X0 reaches the text
// through a chain of two forms, itself and /Inner, and /P through four.
func shortestChainPDF() []byte {
	paint := "0 0 10 10 re f"
	text := "BT /F1 9 Tf 0 0 Td (Louise Anne Farrar) Tj ET"
	form := func(resources, body string) string {
		return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] %s/Length %d >>\nstream\n%s\nendstream",
			resources, len(body)+1, body)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /XObject << /X0 5 0 R /P 9 0 R >> >> >>",
		"<< /Length 14 >>\nstream\n/X0 Do /P Do\nendstream",
		form("/Resources << /XObject << /Deep 6 0 R /Inner 8 0 R >> >> ", paint),
		form("/Resources << /XObject << /Mid 7 0 R >> >> ", paint),
		form("/Resources << /XObject << /Inner 8 0 R >> >> ", paint),
		form("", text),
		form("/Resources << /XObject << /Deep 6 0 R >> >> ", paint),
	}

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
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

// TestTextBearingXObjects_ShortestChainDecides pins that a form is judged by
// the shortest chain of forms leading it to text, within the form budget, and
// not by the path the scan met a form on first.
//
// A form deep in the graph can run out of budget while the same form reached
// nearer the top has room to show its text. Judging it where it was met first
// would hide real words from assistive technology, the failure the scan errs
// against, and would make the answer depend on the order the resources are
// walked in.
func TestTextBearingXObjects_ShortestChainDecides(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := shortestChainPDF()
	for _, tc := range []struct {
		budget int
		want   map[string]bool
	}{
		// /X0 reaches the text in two forms and /P in four.
		{budget: 1, want: map[string]bool{}},
		{budget: 2, want: map[string]bool{"X0": true}},
		{budget: 3, want: map[string]bool{"X0": true}},
		{budget: 4, want: map[string]bool{"X0": true, "P": true}},
	} {
		doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil,
			document.WithLimits(limits.Limits{MaxFormDepth: tc.budget}))
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
		// Each run draws a fresh map order, so an answer that depends on the
		// path a form was first met on shows up as a run that differs.
		for run := range 50 {
			got := textBearingXObjects(ctx, nil, doc, pages[0])
			if len(got) != len(tc.want) || got["X0"] != tc.want["X0"] || got["P"] != tc.want["P"] {
				t.Fatalf("budget %d, run %d: text-bearing forms %v, want %v", tc.budget, run, got, tc.want)
			}
		}
	}
}
