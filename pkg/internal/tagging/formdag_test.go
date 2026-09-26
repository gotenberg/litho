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

	// Scan every top-level form through one memo, the way textBearingXObjects
	// does, and count what it decided.
	answers := make(map[int]bool)
	for _, entry := range top {
		if paints, _ := formPaintsText(ctx, nil, doc, entry, make(map[int]bool), answers, 0); paints {
			t.Fatal("no form in this graph shows text")
		}
	}

	const forms = 10 * 4
	if len(answers) != forms {
		t.Errorf("the scan recorded %d decisions for %d distinct forms: a shared subtree is re-walked once per path, "+
			"which costs 4^10 visits instead of %d", len(answers), forms, forms)
	}

	if got := textBearingXObjects(ctx, nil, doc, pages[0]); len(got) != 0 {
		t.Errorf("no form in the graph shows text, got %v", got)
	}
}

// TestFormPaintsText_DepthCappedAnswerNotMemoised pins that a decision cut short
// by the depth cap is not cached and reused where more depth remains.
//
// A form deep in the graph can run out of budget and answer "no text" while the
// same form reached nearer the top has room to find it. Caching the first answer
// would hide real words from assistive technology, the failure the scan errs
// against.
func TestFormPaintsText_DepthCappedAnswerNotMemoised(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := nestedFormPDF()
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
	resources, err := document.ResolveDict(ctx, doc, pages[0].Resources["XObject"])
	if err != nil {
		t.Fatalf("resolving /XObject: %v", err)
	}
	outer := resources["X0"]

	answers := make(map[int]bool)

	// Starting past the cap answers false without looking, and says nothing
	// about the form itself.
	paints, complete := formPaintsText(ctx, nil, doc, outer, make(map[int]bool), answers, limits.DefaultMaxFormDepth)
	if paints || complete {
		t.Errorf("beyond the depth cap: paints=%v complete=%v, want false and incomplete", paints, complete)
	}
	if len(answers) != 0 {
		t.Errorf("an answer cut short by the depth cap was memoised: %v", answers)
	}

	// The same form from the top still finds the text it reaches through /Inner.
	if paints, _ := formPaintsText(ctx, nil, doc, outer, make(map[int]bool), answers, 0); !paints {
		t.Error("/X0 reaches text through /Inner but was reported as decoration")
	}
}
