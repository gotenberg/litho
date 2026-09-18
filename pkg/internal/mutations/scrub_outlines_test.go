package mutations

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// outlineFixture builds a one-page document whose catalog names object 20 as
// the outline root. outlines supplies object 20 and the items below it, so each
// test shapes the tree it needs.
func outlineFixture(t *testing.T, outlines string) *document.Document {
	t.Helper()
	ctx := context.Background()

	body := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET"
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /Outlines 20 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"6 0 obj\n<< /Font << /F1 5 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 6 0 R " +
		"/Contents 7 0 R >>\nendobj\n" +
		"7 0 obj\n<< /Length " + itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		outlines +
		"trailer\n<< /Size 40 /Root 1 0 R >>\n%%EOF\n")

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

func TestScrubOutlineTitles_EmptiesAQuotingTitle(t *testing.T) {
	// Positive control, and it checks the title is EMPTIED rather than removed:
	// /Title is required on an outline item (ISO 32000-1, Table 153), so
	// deleting it would leave an item the spec does not allow.
	doc := outlineFixture(t,
		"20 0 obj\n<< /Type /Outlines /First 21 0 R /Last 21 0 R /Count 1 >>\nendobj\n"+
			"21 0 obj\n<< /Title (the SECRET chapter) /Parent 20 0 R >>\nendobj\n")
	alloc := writer.NewObjAllocator(50)

	scrubbed, err := ScrubOutlineTitles(context.Background(), alloc, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScrubOutlineTitles: %v", err)
	}
	if !slices.Contains(scrubbed, 21) {
		t.Fatalf("scrubbed = %v, want item 21", scrubbed)
	}

	item, ok := alloc.Pending()[21].(ast.PdfDict)
	if !ok {
		t.Fatalf("object 21 = %T, want the rewritten outline item", alloc.Pending()[21])
	}
	title, present := item["Title"]
	if !present {
		t.Fatal("/Title was deleted: it is required on an outline item")
	}
	if got := document.DecodePdfString(title.(ast.PdfString)); got != "" {
		t.Errorf("/Title = %q, want empty", got)
	}
	// The bookmark survives and still points where it did.
	if _, present := item["Parent"]; !present {
		t.Error("the item lost its /Parent")
	}
}

func TestScrubOutlineTitles_ReachesANestedChild(t *testing.T) {
	// The child hangs off /First, so this fails if the walk only follows the
	// top-level /Next chain.
	doc := outlineFixture(t,
		"20 0 obj\n<< /Type /Outlines /First 21 0 R /Last 21 0 R /Count 2 >>\nendobj\n"+
			"21 0 obj\n<< /Title (an ordinary chapter) /Parent 20 0 R /First 22 0 R >>\nendobj\n"+
			"22 0 obj\n<< /Title (a SECRET section) /Parent 21 0 R >>\nendobj\n")
	alloc := writer.NewObjAllocator(50)

	scrubbed, err := ScrubOutlineTitles(context.Background(), alloc, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScrubOutlineTitles: %v", err)
	}
	if !slices.Contains(scrubbed, 22) {
		t.Errorf("scrubbed = %v, want the nested child 22", scrubbed)
	}
	if slices.Contains(scrubbed, 21) {
		t.Errorf("the parent was rewritten though its title quotes nothing: %v", scrubbed)
	}
}

func TestScrubOutlineTitles_LeavesUnrelatedTitlesAlone(t *testing.T) {
	doc := outlineFixture(t,
		"20 0 obj\n<< /Type /Outlines /First 21 0 R /Last 21 0 R /Count 1 >>\nendobj\n"+
			"21 0 obj\n<< /Title (an ordinary chapter) /Parent 20 0 R >>\nendobj\n")
	alloc := writer.NewObjAllocator(50)

	scrubbed, err := ScrubOutlineTitles(context.Background(), alloc, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScrubOutlineTitles: %v", err)
	}
	if len(scrubbed) != 0 {
		t.Errorf("scrubbed = %v, want none", scrubbed)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects, want none", n)
	}
}

func TestScrubOutlineTitles_CycleTerminates(t *testing.T) {
	// 21 and 22 name each other through /Next. Without the visited guard the
	// walk never returns.
	doc := outlineFixture(t,
		"20 0 obj\n<< /Type /Outlines /First 21 0 R /Last 22 0 R /Count 2 >>\nendobj\n"+
			"21 0 obj\n<< /Title (first) /Parent 20 0 R /Next 22 0 R >>\nendobj\n"+
			"22 0 obj\n<< /Title (a SECRET heading) /Parent 20 0 R /Next 21 0 R >>\nendobj\n")
	alloc := writer.NewObjAllocator(50)

	scrubbed, err := ScrubOutlineTitles(context.Background(), alloc, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScrubOutlineTitles: %v", err)
	}
	if !slices.Contains(scrubbed, 22) {
		t.Errorf("scrubbed = %v, want 22 found before the cycle ended the walk", scrubbed)
	}
}

func TestScrubOutlineTitles_DirectItemHidingALaterSiblingRefuses(t *testing.T) {
	// The regression. Item 21 is indirect and quotes nothing; its /Next is a
	// DIRECT dictionary quoting nothing either; and behind that sits indirect
	// item 23, whose title quotes the removed text.
	//
	// Checking the direct item and returning would end the chain silently and
	// report a clean outline while 23's title still spells the text. A direct
	// item cannot be rewritten and /Next lives inside it, so the only honest
	// answer is to refuse.
	doc := outlineFixture(t,
		"20 0 obj\n<< /Type /Outlines /First 21 0 R /Last 23 0 R /Count 3 >>\nendobj\n"+
			"21 0 obj\n<< /Title (first) /Parent 20 0 R /Next << /Title (middle) /Next 23 0 R >> >>\nendobj\n"+
			"23 0 obj\n<< /Title (a SECRET heading) /Parent 20 0 R >>\nendobj\n")
	alloc := writer.NewObjAllocator(50)

	_, err := ScrubOutlineTitles(context.Background(), alloc, doc, []string{"SECRET"})
	if !errors.Is(err, ErrOutlineItemNotAddressable) {
		t.Errorf("err = %v, want ErrOutlineItemNotAddressable", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing", n)
	}
}

func TestScrubOutlineTitles_RefusalAfterScrubbingRegistersNothing(t *testing.T) {
	// Item 21 quotes and is scrubbed first; the direct item behind it refuses.
	// Without the rollback, 21's rewrite stays queued.
	doc := outlineFixture(t,
		"20 0 obj\n<< /Type /Outlines /First 21 0 R /Last 21 0 R /Count 2 >>\nendobj\n"+
			"21 0 obj\n<< /Title (a SECRET chapter) /Parent 20 0 R /Next << /Title (direct) >> >>\nendobj\n")
	alloc := writer.NewObjAllocator(50)

	_, err := ScrubOutlineTitles(context.Background(), alloc, doc, []string{"SECRET"})
	if !errors.Is(err, ErrOutlineItemNotAddressable) {
		t.Fatalf("err = %v, want ErrOutlineItemNotAddressable", err)
	}
	if n := len(alloc.Pending()); n != 0 {
		t.Errorf("registered %d objects while refusing: the earlier scrub was not rolled back", n)
	}
}

func TestScrubOutlineTitles_NoOutlineTreeIsNotAnError(t *testing.T) {
	// A document with no /Outlines has no bookmark to quote anything, and the
	// pass must not refuse over its absence.
	doc, _ := redactFixture(t)
	alloc := writer.NewObjAllocator(50)

	scrubbed, err := ScrubOutlineTitles(context.Background(), alloc, doc, []string{"SECRET"})
	if err != nil {
		t.Fatalf("ScrubOutlineTitles: %v", err)
	}
	if len(scrubbed) != 0 {
		t.Errorf("scrubbed = %v, want none", scrubbed)
	}
}

func TestScrubOutlineTitles_RejectsAnEmptyToken(t *testing.T) {
	doc := outlineFixture(t,
		"20 0 obj\n<< /Type /Outlines /First 21 0 R /Last 21 0 R /Count 1 >>\nendobj\n"+
			"21 0 obj\n<< /Title (chapter) /Parent 20 0 R >>\nendobj\n")
	alloc := writer.NewObjAllocator(50)

	_, err := ScrubOutlineTitles(context.Background(), alloc, doc, []string{"SECRET", ""})
	if !errors.Is(err, ErrScanTokenEmpty) {
		t.Errorf("err = %v, want ErrScanTokenEmpty", err)
	}
}
